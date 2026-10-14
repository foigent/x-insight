package hostctl

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateBackupPartialCopyAndPrePublishFailureResumeSameDump(t *testing.T) {
	for _, failure := range []string{"partial_copy", "before_publish"} {
		t.Run(failure, func(t *testing.T) {
			e, c := managedCustodyFixture(t)
			database := &fakeDatabase{}
			e.Database = database
			if err := e.SetReviewBodyConfig(c, false); err != nil {
				t.Fatal(err)
			}
			if err := e.ensureControlDirectories(); err != nil {
				t.Fatal(err)
			}
			fixture := newInstallFixture(t)
			target, err := fixture.engine.verifyTarget(fixture.artifact)
			if err != nil {
				t.Fatal(err)
			}
			j, err := e.startJournal("backup", "", "", &target.Identity, &target.Identity)
			if err != nil {
				t.Fatal(err)
			}
			originalKey, _ := os.ReadFile(c.KeyFile)
			if failure == "partial_copy" {
				e.PrivateBackupCopy = func(dst io.Writer, src io.Reader, n int64) (int64, error) {
					written, err := io.CopyN(dst, src, n/2)
					if err != nil {
						return written, err
					}
					return written, fmt.Errorf("synthetic copy failed after half member")
				}
			} else {
				e.PrivateBackupPublishHook = func() error { return fmt.Errorf("synthetic failure before publication") }
			}
			path := filepath.Join(e.Paths.BackupsDir, "resume.tar")
			if _, err = e.writePrivateBackup(context.Background(), path, j, DatabaseCredentials{}, target); err == nil {
				t.Fatal("injected write failure not reached")
			}
			if exists, _ := fileExists(path); exists {
				t.Fatal("failed write exposed incomplete final package")
			}
			dumpHash := j.PrivateDumpSHA256
			if dumpHash == "" {
				t.Fatal("completed dump identity not persisted")
			}
			e.PrivateBackupCopy = nil
			e.PrivateBackupPublishHook = nil
			result, err := e.writePrivateBackup(context.Background(), path, j, DatabaseCredentials{}, target)
			if err != nil {
				t.Fatal("resume", err)
			}
			if database.backupCount != 1 || j.PrivateDumpSHA256 != dumpHash {
				t.Fatal("resume created another dump")
			}
			currentKey, _ := os.ReadFile(c.KeyFile)
			if !bytes.Equal(originalKey, currentKey) {
				t.Fatal("resume changed original key")
			}
			bundle, err := e.inspectPrivateBackup(context.Background(), path, result.SHA256)
			if err != nil {
				t.Fatal(err)
			}
			defer bundle.Clear()
			e.PrivateBackupCopy = func(dst io.Writer, src io.Reader, n int64) (int64, error) {
				written, _ := io.CopyN(dst, src, n/2)
				return written, fmt.Errorf("half extraction")
			}
			if _, err = e.extractPrivateDump(context.Background(), bundle, j); err == nil {
				t.Fatal("partial extract accepted")
			}
			restorePath := filepath.Join(e.Paths.OperationsDir, j.OperationID+"-restore.dump")
			if exists, _ := fileExists(restorePath); exists {
				t.Fatal("partial extract exposed final dump")
			}
			e.PrivateBackupCopy = nil
			restored, err := e.extractPrivateDump(context.Background(), bundle, j)
			if err != nil || restored.SHA256 != dumpHash {
				t.Fatal("extract resume changed dump", err)
			}
		})
	}
}

func TestPrivateBackupKeyBindingTamperAndIndependentRoot(t *testing.T) {
	e, c := managedCustodyFixture(t)
	e.Database = &fakeDatabase{}
	if err := e.SetReviewBodyConfig(c, false); err != nil {
		t.Fatal(err)
	}
	if err := e.ensureControlDirectories(); err != nil {
		t.Fatal(err)
	}
	identity := ReleaseIdentity{DatabaseSchema: "1"}
	target := verifiedTarget{Identity: identity}
	f := newInstallFixture(t)
	verified, err := f.engine.verifyTarget(f.artifact)
	if err != nil {
		t.Fatal(err)
	}
	target.Bundle = verified.Bundle
	j, err := e.startJournal("backup", "", "", &identity, &identity)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(e.Paths.BackupsDir, "complete.tar")
	result, err := e.writePrivateBackup(context.Background(), path, j, DatabaseCredentials{}, target)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := e.inspectPrivateBackup(context.Background(), path, result.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Clear()
	if backup.Manifest.Custody == nil || backup.Manifest.Custody.KeyID != c.KeyID || len(backup.Key) != 32 {
		t.Fatal("bundle missed original custody")
	}
	if err = e.checkBackupKey(backup); err != nil {
		t.Fatal(err)
	}
	// Different root has no access to the old key reference; import exact bundle bytes.
	other, _ := managedCustodyFixture(t)
	otherKey := filepath.Join(other.Paths.EtcDir, "review-body.key")
	putManagedFile(t, otherKey, []byte(strings.Repeat("42", 32)+"\n"), 0600)
	if err = other.SetReviewBodyConfig(ReviewBodyConfig{c.KeyID, otherKey}, false); err != nil {
		t.Fatal(err)
	}
	if err = other.checkBackupKey(backup); err != nil {
		t.Fatal("independent key bytes rejected", err)
	}
	putManagedFile(t, otherKey, []byte(strings.Repeat("43", 32)+"\n"), 0600)
	if other.checkBackupKey(backup) == nil {
		t.Fatal("wrong key bytes with same ID accepted")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[600] ^= 1
	bad := filepath.Join(e.Paths.BackupsDir, "tampered.tar")
	putManagedFile(t, bad, raw, 0600)
	if b, err := e.inspectPrivateBackup(context.Background(), bad, result.SHA256); err == nil {
		b.Clear()
		t.Fatal("tampered bundle accepted")
	}
}
func TestPrivateBackupRejectsUnsafeAndDuplicateMembers(t *testing.T) {
	e, _ := managedCustodyFixture(t)
	for _, names := range [][]string{{"../review-body.key"}, {"manifest.json", "manifest.json"}} {
		var out bytes.Buffer
		w := tar.NewWriter(&out)
		for _, name := range names {
			if err := w.WriteHeader(&tar.Header{Name: name, Mode: 0600, Typeflag: tar.TypeReg, Size: 2}); err != nil {
				t.Fatal(err)
			}
			w.Write([]byte("{}"))
		}
		w.Close()
		path := filepath.Join(e.Paths.EtcDir, "unsafe.tar")
		putManagedFile(t, path, out.Bytes(), 0600)
		hash, _, err := fileSHA256(path, privateBackupMaxBytes)
		if err != nil {
			t.Fatal(err)
		}
		if b, err := e.inspectPrivateBackup(context.Background(), path, hash); err == nil {
			b.Clear()
			t.Fatal("unsafe archive accepted")
		}
	}
}
