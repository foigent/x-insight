package hostctl

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/db"
)

const privateBackupSchema = "xinsight.private-database-backup.v1"
const privateBackupMaxBytes = maxBackupBytes + (2 << 20)

type BackupCustody struct {
	KeyID   string `json:"key_id"`
	Enabled bool   `json:"enabled"`
}
type PrivateBackupManifest struct {
	SchemaVersion string                 `json:"schema_version"`
	OperationID   string                 `json:"operation_id"`
	DeploymentID  string                 `json:"deployment_id"`
	Release       ReleaseIdentity        `json:"release"`
	Migrations    []db.MigrationChecksum `json:"migrations"`
	Dump          BackupResult           `json:"dump"`
	Fingerprint   EvidenceFingerprint    `json:"fingerprint"`
	Custody       *BackupCustody         `json:"custody,omitempty"`
}
type PrivateBackup struct {
	Manifest PrivateBackupManifest
	Key      []byte
	Path     string
	SHA256   string
	Size     int64
}

func (b *PrivateBackup) Clear() {
	if b != nil {
		clear(b.Key)
		b.Key = nil
	}
}
func equalSecret(a, b []byte) bool { return len(a) == len(b) && subtle.ConstantTimeCompare(a, b) == 1 }

// Open exact owner-only regular material without following a final symlink.
func openPrivateBackup(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, fmt.Errorf("private backup path must be absolute and clean")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("private backup unavailable")
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || st.Nlink != 1 || st.Uid != uint32(os.Geteuid()) || info.Size() <= 0 || info.Size() > privateBackupMaxBytes {
		f.Close()
		return nil, fmt.Errorf("private backup metadata is unsafe")
	}
	return f, nil
}

func (e *Engine) inspectPrivateBackup(ctx context.Context, path, expected string) (*PrivateBackup, error) {
	if !sha256ReferencePattern.MatchString(expected) {
		return nil, fmt.Errorf("exact private backup SHA256 required")
	}
	f, err := openPrivateBackup(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b := &PrivateBackup{Path: path, SHA256: expected}
	ok := false
	defer func() {
		if !ok {
			b.Clear()
		}
	}()
	all := sha256.New()
	r := tar.NewReader(io.TeeReader(f, all))
	seen := map[string]bool{}
	var dumpSHA string
	var dumpBytes int64
	for {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("private backup archive invalid")
		}
		if seen[h.Name] || h.Typeflag != tar.TypeReg || h.Mode != 0600 || h.Linkname != "" || h.Size < 0 || h.Uid != 0 || h.Gid != 0 {
			return nil, fmt.Errorf("private backup member unsafe")
		}
		seen[h.Name] = true
		switch h.Name {
		case "manifest.json":
			if h.Size > maxControlFileBytes {
				return nil, fmt.Errorf("private backup manifest too large")
			}
			raw, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}
			if err = decodePrivateManifest(raw, &b.Manifest); err != nil {
				return nil, fmt.Errorf("private backup manifest invalid")
			}
		case "database.dump":
			if h.Size <= 0 || h.Size > maxBackupBytes {
				return nil, fmt.Errorf("private backup dump size invalid")
			}
			hash := sha256.New()
			n, err := io.Copy(hash, r)
			if err != nil {
				return nil, err
			}
			dumpBytes = n
			dumpSHA = "sha256:" + hex.EncodeToString(hash.Sum(nil))
		case "review-body.key":
			if h.Size != 65 {
				return nil, fmt.Errorf("private backup key format invalid")
			}
			raw, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}
			defer clear(raw)
			if raw[64] != '\n' {
				return nil, fmt.Errorf("private backup key format invalid")
			}
			b.Key = make([]byte, 32)
			if _, err = hex.Decode(b.Key, raw[:64]); err != nil {
				return nil, fmt.Errorf("private backup key format invalid")
			}
		default:
			return nil, fmt.Errorf("private backup member is not supported")
		}
	}
	// Hash every byte, including any tar padding. Nonzero trailing data is rejected.
	tail, err := io.ReadAll(io.LimitReader(io.TeeReader(f, all), 1<<20))
	if err != nil {
		return nil, err
	}
	for _, v := range tail {
		if v != 0 {
			return nil, fmt.Errorf("private backup trailing data invalid")
		}
	}
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	b.Size = info.Size()
	pos, err := f.Seek(0, io.SeekCurrent)
	if err != nil || pos != b.Size {
		return nil, fmt.Errorf("private backup trailing data exceeds limit")
	}
	if "sha256:"+hex.EncodeToString(all.Sum(nil)) != expected {
		return nil, fmt.Errorf("private backup hash mismatch")
	}
	m := b.Manifest
	if !seen["manifest.json"] || !seen["database.dump"] || m.SchemaVersion != privateBackupSchema || !privateBackupOperationPattern.MatchString(m.OperationID) || m.Dump.SHA256 != dumpSHA || m.Dump.SizeBytes != dumpBytes || m.Dump.PGDumpMajor != 16 || m.Dump.PGRestoreMajor != 16 || !sha256ReferencePattern.MatchString(m.Fingerprint.SHA256) || m.Fingerprint.EventCount < 0 || m.Fingerprint.SampleCount < 0 || len(m.Migrations) == 0 {
		return nil, fmt.Errorf("private backup binding invalid")
	}
	if (m.Custody != nil) != seen["review-body.key"] || m.Custody != nil && (!custodyKeyIDPattern.MatchString(m.Custody.KeyID) || len(b.Key) != 32) {
		return nil, fmt.Errorf("private backup custody binding invalid")
	}
	ok = true
	return b, nil
}

func (e *Engine) checkBackupKey(b *PrivateBackup) error {
	c, _, present, err := e.custodyBinding()
	if err != nil {
		return err
	}
	if b.Manifest.Custody == nil {
		return nil
	}
	if !present {
		return fmt.Errorf("restore_original_review_body_key_required: install with this recovery bundle first")
	}
	raw, err := config.LoadReviewBodyKeyFile(c.KeyFile)
	if err != nil {
		return err
	}
	defer clear(raw)
	if c.KeyID != b.Manifest.Custody.KeyID || !equalSecret(raw, b.Key) {
		return fmt.Errorf("private backup custody conflicts with target binding")
	}
	return nil
}

func validateBackupRelease(b *PrivateBackup, target verifiedTarget) error {
	n, err := databaseSchemaNumber(b.Manifest.Release)
	if err != nil || n != len(b.Manifest.Migrations) {
		return fmt.Errorf("private backup schema binding invalid")
	}
	if len(b.Manifest.Migrations) > len(target.Bundle.Manifest.MigrationChecksums) {
		return fmt.Errorf("private backup schema newer than target")
	}
	for i, c := range b.Manifest.Migrations {
		if c != target.Bundle.Manifest.MigrationChecksums[i] {
			return fmt.Errorf("private backup migration binding mismatch")
		}
	}
	return nil
}

// The dump and key are never returned through a Web/API response.
func (e *Engine) writePrivateBackup(ctx context.Context, path string, j *Journal, credentials DatabaseCredentials, current verifiedTarget) (BackupResult, error) {
	c, active, present, err := e.custodyBinding()
	if err != nil {
		return BackupResult{}, fmt.Errorf("complete backup requires original custody key")
	}
	var key []byte
	if present {
		key, err = config.LoadReviewBodyKeyFile(c.KeyFile)
		if err != nil {
			return BackupResult{}, err
		}
		defer clear(key)
	} else if err = e.Database.VerifyReviewBodyInventory(ctx, credentials); err != nil {
		return BackupResult{}, err
	}
	if err = e.verifyRestoredReviewBodyKeys(ctx, credentials); err != nil {
		return BackupResult{}, err
	}
	fingerprint, err := e.Database.EvidenceFingerprint(ctx, credentials)
	if err != nil {
		return BackupResult{}, err
	}
	if j.PrivateFingerprint == nil {
		j.PrivateFingerprint = &fingerprint
		if err = e.updateJournal(j); err != nil {
			return BackupResult{}, err
		}
	} else if *j.PrivateFingerprint != fingerprint {
		return BackupResult{}, fmt.Errorf("backup source changed since snapshot")
	}
	if exists, err := fileExists(path); err != nil {
		return BackupResult{}, err
	} else if exists {
		digest, size, err := fileSHA256(path, privateBackupMaxBytes)
		if err != nil {
			return BackupResult{}, err
		}
		b, err := e.inspectPrivateBackup(ctx, path, digest)
		if err != nil {
			return BackupResult{}, err
		}
		defer b.Clear()
		if b.Manifest.OperationID != j.OperationID || b.Manifest.Dump.SHA256 != j.PrivateDumpSHA256 || b.Manifest.Fingerprint != fingerprint {
			return BackupResult{}, fmt.Errorf("existing private backup is not this snapshot")
		}
		if err = e.checkBackupKey(b); err != nil {
			return BackupResult{}, err
		}
		return BackupResult{Path: path, SHA256: digest, SizeBytes: size, PGDumpMajor: 16, PGRestoreMajor: 16}, nil
	}
	dumpPath := filepath.Join(e.Paths.OperationsDir, j.OperationID+".dump")
	dump, err := e.preparePrivateDump(ctx, j, credentials, dumpPath)
	if err != nil {
		return BackupResult{}, err
	}
	deployment, _, err := readConnectorIdentityEnvironmentFileIfPresent(e.Paths.ConnectorIdentityEnv)
	if err != nil {
		return BackupResult{}, err
	}
	m := PrivateBackupManifest{SchemaVersion: privateBackupSchema, OperationID: j.OperationID, DeploymentID: deployment, Release: current.Identity, Migrations: current.Bundle.Manifest.MigrationChecksums, Dump: dump, Fingerprint: fingerprint}
	m.Dump.Path = "database.dump"
	if present {
		m.Custody = &BackupCustody{KeyID: c.KeyID, Enabled: active}
	}
	manifest, err := json.Marshal(m)
	if err != nil {
		return BackupResult{}, err
	}
	if err = e.validateReferenceParent(path); err != nil {
		return BackupResult{}, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+j.OperationID+"-bundle-")
	if err != nil {
		return BackupResult{}, err
	}
	temporary := f.Name()
	defer os.Remove(temporary)
	tw := tar.NewWriter(f)
	write := func(name string, body io.Reader, size int64) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: size, Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		_, err := e.copyPrivateBackup(tw, body, size)
		return err
	}
	if err = write("manifest.json", bytes.NewReader(manifest), int64(len(manifest))); err == nil {
		df, er := openPrivateBackup(dumpPath)
		if er != nil {
			err = er
		} else {
			err = write("database.dump", df, dump.SizeBytes)
			df.Close()
		}
	}
	if err == nil && present {
		raw := []byte(hex.EncodeToString(key) + "\n")
		err = write("review-body.key", bytes.NewReader(raw), int64(len(raw)))
		clear(raw)
	}
	closeErr := tw.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr = f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return BackupResult{}, err
	}
	if e.PrivateBackupPublishHook != nil {
		if err = e.PrivateBackupPublishHook(); err != nil {
			return BackupResult{}, err
		}
	}
	if err = publishExclusiveFile(temporary, path); err != nil {
		return BackupResult{}, err
	}
	if err = syncDirectory(filepath.Dir(path)); err != nil {
		return BackupResult{}, err
	}
	digest, size, err := fileSHA256(path, privateBackupMaxBytes)
	if err != nil {
		return BackupResult{}, err
	}
	verify, err := e.inspectPrivateBackup(ctx, path, digest)
	if err != nil {
		return BackupResult{}, err
	}
	verify.Clear()
	return BackupResult{Path: path, SHA256: digest, SizeBytes: size, PGDumpMajor: 16, PGRestoreMajor: 16}, nil
}

func (e *Engine) extractPrivateDump(ctx context.Context, b *PrivateBackup, j *Journal) (BackupResult, error) {
	path := filepath.Join(e.Paths.OperationsDir, j.OperationID+"-restore.dump")
	if exists, err := fileExists(path); err != nil {
		return BackupResult{}, err
	} else if exists {
		return e.Database.VerifyBackup(ctx, path, b.Manifest.Dump.SHA256)
	}
	f, err := openPrivateBackup(b.Path)
	if err != nil {
		return BackupResult{}, err
	}
	defer f.Close()
	r := tar.NewReader(f)
	for {
		h, err := r.Next()
		if err != nil {
			return BackupResult{}, fmt.Errorf("private backup dump unavailable")
		}
		if h.Name != "database.dump" {
			continue
		}
		out, err := os.CreateTemp(filepath.Dir(path), "."+j.OperationID+"-restore-")
		if err != nil {
			return BackupResult{}, err
		}
		temporary := out.Name()
		defer os.Remove(temporary)
		_, err = e.copyPrivateBackup(out, r, h.Size)
		if err == nil {
			err = out.Sync()
		}
		closeErr := out.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			return BackupResult{}, err
		}
		if e.PrivateBackupPublishHook != nil {
			if err = e.PrivateBackupPublishHook(); err != nil {
				return BackupResult{}, err
			}
		}
		if err = publishExclusiveFile(temporary, path); err != nil {
			return BackupResult{}, err
		}
		if err = syncDirectory(filepath.Dir(path)); err != nil {
			return BackupResult{}, err
		}
		return e.Database.VerifyBackup(ctx, path, b.Manifest.Dump.SHA256)
	}
}

var custodyKeyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)

func decodePrivateManifest(raw []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("trailing manifest")
	}
	return nil
}

var privateBackupOperationPattern = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z-(?:[0-9a-f]{12}|[0-9a-f]{16})$`)

func (e *Engine) copyPrivateBackup(dst io.Writer, src io.Reader, size int64) (int64, error) {
	if e.PrivateBackupCopy != nil {
		return e.PrivateBackupCopy(dst, src, size)
	}
	return io.CopyN(dst, src, size)
}

func (e *Engine) preparePrivateDump(ctx context.Context, j *Journal, creds DatabaseCredentials, path string) (BackupResult, error) {
	if present, err := fileExists(path); err != nil {
		return BackupResult{}, err
	} else if present {
		if j.PrivateDumpSHA256 == "" {
			return BackupResult{}, fmt.Errorf("unverified final dump exists; preserve it for recovery")
		}
		result, err := e.Database.VerifyBackup(ctx, path, j.PrivateDumpSHA256)
		result.PGDumpMajor = 16
		return result, err
	}
	if j.PrivateDumpTempPath == "" {
		f, err := os.CreateTemp(e.Paths.OperationsDir, "."+j.OperationID+"-dump-")
		if err != nil {
			return BackupResult{}, err
		}
		j.PrivateDumpTempPath = f.Name()
		f.Close()
		if err = os.Remove(j.PrivateDumpTempPath); err != nil {
			return BackupResult{}, err
		}
		if err = e.updateJournal(j); err != nil {
			return BackupResult{}, err
		}
	}
	stage := j.PrivateDumpTempPath
	if filepath.Dir(stage) != e.Paths.OperationsDir || !strings.HasPrefix(filepath.Base(stage), "."+j.OperationID+"-dump-") {
		return BackupResult{}, fmt.Errorf("private dump staging identity invalid")
	}
	var result BackupResult
	var err error
	if j.PrivateDumpSHA256 != "" {
		result, err = e.Database.VerifyBackup(ctx, stage, j.PrivateDumpSHA256)
	} else {
		if present, er := fileExists(stage); er != nil {
			return BackupResult{}, er
		} else if present {
			// Only this journal's never-published staging file may be discarded.
			info, er := os.Lstat(stage)
			if er != nil {
				return BackupResult{}, er
			}
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || st.Nlink != 1 || st.Uid != uint32(os.Geteuid()) {
				return BackupResult{}, fmt.Errorf("private dump staging unsafe")
			}
			if er = os.Remove(stage); er != nil {
				return BackupResult{}, er
			}
		}
		result, err = e.Database.Backup(ctx, creds, stage)
		if err == nil {
			j.PrivateDumpSHA256 = result.SHA256
			err = e.updateJournal(j)
		}
	}
	if err != nil {
		return BackupResult{}, err
	}
	if err = publishExclusiveFile(stage, path); err != nil {
		return BackupResult{}, err
	}
	if err = syncDirectory(filepath.Dir(path)); err != nil {
		return BackupResult{}, err
	}
	result.Path = path
	result.PGDumpMajor = 16
	return result, nil
}
