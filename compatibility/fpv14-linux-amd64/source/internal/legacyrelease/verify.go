package legacyrelease

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/foigent/x-insight/internal/releasebundle"
)

const (
	maxLegacyArtifactBytes = 256 << 20
	maxLegacyExpandedBytes = 512 << 20
)

type Manifest struct {
	SchemaVersion   string         `json:"schema_version"`
	Project         string         `json:"project"`
	Commit          string         `json:"commit"`
	SourceDateEpoch int64          `json:"source_date_epoch"`
	Toolchain       Toolchain      `json:"toolchain"`
	Target          Target         `json:"target"`
	Builder         Builder        `json:"builder"`
	Layout          Layout         `json:"layout"`
	Files           []ManifestFile `json:"files"`
}

type Toolchain struct {
	GoVersion string `json:"go_version"`
}

type Target struct {
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	CGOEnabled bool   `json:"cgo_enabled"`
}

type Builder struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type Layout struct {
	TopLevel []string `json:"top_level"`
}

type ManifestFile struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

type Verified struct {
	ReleaseName    string
	ArtifactSHA256 string
	ManifestSHA256 string
	Manifest       Manifest
	files          map[string][]byte
}

func Verify(path string, expected releasebundle.LegacySource) (Verified, error) {
	artifact, err := readRegular(path, maxLegacyArtifactBytes)
	if err != nil {
		return Verified{}, err
	}
	artifactDigest := sha256Reference(artifact)
	if artifactDigest != expected.ArtifactSHA256 {
		return Verified{}, fmt.Errorf("legacy artifact digest does not match the allowlisted source")
	}
	releaseName, files, err := inspectArchive(artifact)
	if err != nil {
		return Verified{}, err
	}
	manifestBytes := files["release-manifest.json"]
	checksumsBytes := files["SHA256SUMS"]
	if len(manifestBytes) == 0 || len(checksumsBytes) == 0 {
		return Verified{}, fmt.Errorf("legacy release metadata is incomplete")
	}
	manifestDigest := sha256Reference(manifestBytes)
	if manifestDigest != expected.ManifestSHA256 {
		return Verified{}, fmt.Errorf("legacy manifest digest does not match the allowlisted source")
	}
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(manifestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Verified{}, fmt.Errorf("legacy release manifest is invalid")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Verified{}, fmt.Errorf("legacy release manifest has trailing content")
	}
	if manifest.SchemaVersion != expected.ReleaseLayout || manifest.SchemaVersion != "xinsight.release-manifest.v1" || manifest.Project != "x-insight" ||
		manifest.Commit != expected.Commit || manifest.Target.OS != "linux" || manifest.Target.Arch != "amd64" || manifest.Target.CGOEnabled ||
		manifest.SourceDateEpoch <= 0 || manifest.Toolchain.GoVersion == "" {
		return Verified{}, fmt.Errorf("legacy release identity does not match the allowlisted source")
	}
	if releaseName != "xinsight-release-linux-amd64-"+manifest.Commit {
		return Verified{}, fmt.Errorf("legacy release archive root is invalid")
	}
	if err := verifyFileSets(files, manifest, checksumsBytes, expected); err != nil {
		return Verified{}, err
	}
	return Verified{ReleaseName: releaseName, ArtifactSHA256: artifactDigest, ManifestSHA256: manifestDigest, Manifest: manifest, files: files}, nil
}

func VerifyInstalled(verified Verified, directory string) error {
	if !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return fmt.Errorf("legacy installation path must be absolute and clean")
	}
	root, err := os.Lstat(directory)
	if err != nil || !root.IsDir() || root.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("legacy installation root is invalid")
	}
	actual := make(map[string]string)
	err = filepath.WalkDir(directory, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == directory {
			return nil
		}
		relative, err := filepath.Rel(directory, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("legacy installation contains a symbolic link")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if info.Mode().Perm() != 0o755 {
				return fmt.Errorf("legacy installation directory mode drift")
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("legacy installation contains an unsupported file type")
		}
		expectedMode := os.FileMode(0o644)
		if strings.HasPrefix(relative, "bin/") {
			expectedMode = 0o755
		}
		if info.Mode().Perm() != expectedMode {
			return fmt.Errorf("legacy installation file mode drift")
		}
		digest, _, err := fileDigest(current, maxLegacyArtifactBytes)
		if err != nil {
			return err
		}
		actual[relative] = digest
		return nil
	})
	if err != nil {
		return err
	}
	if len(actual) != len(verified.files) {
		return fmt.Errorf("legacy installation file set drift")
	}
	for name, contents := range verified.files {
		if actual[name] != sha256Reference(contents) {
			return fmt.Errorf("legacy installation file digest drift")
		}
	}
	return nil
}

func inspectArchive(artifact []byte) (string, map[string][]byte, error) {
	reader := bytes.NewReader(artifact)
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return "", nil, fmt.Errorf("legacy release compression is invalid")
	}
	gzipReader.Multistream(false)
	expanded := &io.LimitedReader{R: gzipReader, N: maxLegacyExpandedBytes + 1}
	tarReader := tar.NewReader(expanded)
	releaseName := ""
	files := make(map[string][]byte)
	seen := make(map[string]struct{})
	for count := 0; ; count++ {
		if count > 4000 {
			return "", nil, fmt.Errorf("legacy release contains too many members")
		}
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, fmt.Errorf("legacy release tar is invalid")
		}
		name, err := safeArchivePath(header.Name)
		if err != nil {
			return "", nil, err
		}
		parts := strings.Split(name, "/")
		if releaseName == "" {
			releaseName = parts[0]
		}
		if parts[0] != releaseName {
			return "", nil, fmt.Errorf("legacy release has multiple roots")
		}
		if _, exists := seen[name]; exists {
			return "", nil, fmt.Errorf("legacy release contains duplicate paths")
		}
		seen[name] = struct{}{}
		if header.Uid != 0 || header.Gid != 0 || header.Uname != "root" || header.Gname != "root" {
			return "", nil, fmt.Errorf("legacy release ownership metadata is invalid")
		}
		relative := strings.TrimPrefix(name, releaseName+"/")
		if header.Typeflag == tar.TypeDir {
			if header.Mode != 0o755 {
				return "", nil, fmt.Errorf("legacy release directory mode is invalid")
			}
			continue
		}
		if header.Typeflag != tar.TypeReg || relative == name || header.Size < 0 || header.Size > maxLegacyArtifactBytes {
			return "", nil, fmt.Errorf("legacy release member type or size is invalid")
		}
		expectedMode := int64(0o644)
		if strings.HasPrefix(relative, "bin/") {
			expectedMode = 0o755
		}
		if header.Mode != expectedMode {
			return "", nil, fmt.Errorf("legacy release file mode is invalid")
		}
		contents, err := io.ReadAll(io.LimitReader(tarReader, header.Size+1))
		if err != nil || int64(len(contents)) != header.Size {
			return "", nil, fmt.Errorf("legacy release member bytes are invalid")
		}
		files[relative] = contents
	}
	if err := drainZeroPadding(expanded); err != nil {
		return "", nil, err
	}
	if expanded.N == 0 {
		return "", nil, fmt.Errorf("legacy release expanded size exceeds the limit")
	}
	if err := gzipReader.Close(); err != nil || reader.Len() != 0 || releaseName == "" {
		return "", nil, fmt.Errorf("legacy release trailing bytes are invalid")
	}
	return releaseName, files, nil
}

func drainZeroPadding(reader io.Reader) error {
	buffer := make([]byte, 32<<10)
	for {
		count, err := reader.Read(buffer)
		for _, value := range buffer[:count] {
			if value != 0 {
				return fmt.Errorf("legacy release contains non-zero bytes after the tar terminator")
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("legacy release trailing bytes are invalid")
		}
	}
}

func verifyFileSets(files map[string][]byte, manifest Manifest, checksums []byte, expected releasebundle.LegacySource) error {
	expectedFiles := make(map[string]ManifestFile, len(manifest.Files))
	for _, entry := range manifest.Files {
		if _, err := safeRelativePath(entry.Path); err != nil || entry.SizeBytes < 0 || !strings.HasPrefix(entry.SHA256, "sha256:") {
			return fmt.Errorf("legacy manifest file entry is invalid")
		}
		if _, exists := expectedFiles[entry.Path]; exists {
			return fmt.Errorf("legacy manifest contains duplicate files")
		}
		expectedFiles[entry.Path] = entry
	}
	if len(files) != len(expectedFiles)+2 {
		return fmt.Errorf("legacy archive file set does not match its manifest")
	}
	for name, entry := range expectedFiles {
		contents, exists := files[name]
		if !exists || int64(len(contents)) != entry.SizeBytes || sha256Reference(contents) != entry.SHA256 {
			return fmt.Errorf("legacy archive file does not match its manifest")
		}
	}
	checksumMap := make(map[string]string)
	for _, line := range strings.Split(string(checksums), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		digest, name, present := strings.Cut(line, "  ")
		if !present || len(digest) != 64 || name == "SHA256SUMS" {
			return fmt.Errorf("legacy SHA256SUMS is invalid")
		}
		checksumMap[name] = "sha256:" + digest
	}
	if len(checksumMap) != len(files)-1 {
		return fmt.Errorf("legacy SHA256SUMS file set is invalid")
	}
	for name, contents := range files {
		if name == "SHA256SUMS" {
			continue
		}
		if checksumMap[name] != sha256Reference(contents) {
			return fmt.Errorf("legacy SHA256SUMS digest mismatch")
		}
	}
	for _, migration := range expected.MigrationChecksums {
		name := "migrations/" + migration.ID + ".up.sql"
		if sha256Reference(files[name]) != migration.SHA256 {
			return fmt.Errorf("legacy migration checksum does not match allowlisted source")
		}
	}
	return nil
}

func safeArchivePath(name string) (string, error) {
	name = strings.TrimSuffix(name, "/")
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return "", fmt.Errorf("legacy archive path is unsafe")
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("legacy archive path is unsafe")
		}
	}
	return name, nil
}

func safeRelativePath(name string) (string, error) {
	if strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || name == "" {
		return "", fmt.Errorf("legacy relative path is unsafe")
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("legacy relative path is unsafe")
		}
	}
	return name, nil
}

func readRegular(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return nil, fmt.Errorf("legacy artifact type or size is invalid")
	}
	return os.ReadFile(path)
}

func fileDigest(path string, limit int64) (string, int64, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > limit {
		return "", 0, fmt.Errorf("legacy installation file is invalid")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	return sha256Reference(contents), int64(len(contents)), nil
}

func sha256Reference(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func sortedKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
