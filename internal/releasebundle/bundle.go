package releasebundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/foigent/x-insight/internal/db"
)

const maxTarEndPaddingBytes = 1 << 20

type PackOptions struct {
	PayloadDir      string
	ConfigPath      string
	SigningKeyPath  string
	OutputPath      string
	Commit          string
	SourceDateEpoch time.Time
	BuildToolchain  BuildToolchain
}

type PackResult struct {
	ReleaseName    string
	OutputPath     string
	BundleSHA256   string
	ManifestSHA256 string
	PayloadSHA256  string
	KeyID          string
	Manifest       ReleaseManifest
}

type VerifyOptions struct {
	TrustStorePath  string
	ExpectedCommit  string
	ExpectedVersion string
	ExpectedOS      string
	ExpectedArch    string
	Now             time.Time
}

type VerifiedBundle struct {
	ReleaseName    string
	BundleSHA256   string
	ManifestSHA256 string
	Manifest       ReleaseManifest
	manifest       []byte
	payload        []byte
}

type payloadSource struct {
	contents []byte
	entry    FileEntry
}

// ReadRuntimeUnit inspects only the three verified managed units during a
// side-effect-free host preflight; it never extracts or changes a release.
func ReadRuntimeUnit(bundle VerifiedBundle, name string) ([]byte, error) {
	if name != "xinsight.service" && name != "xinsight-source-facts.service" && name != "xinsight-connector-controller.service" {
		return nil, ErrInvalidBundle
	}
	if bundle.Manifest.SchemaVersion != ManifestSchemaVersion || len(bundle.payload) == 0 {
		return nil, ErrInvalidBundle
	}
	gz, err := gzip.NewReader(bytes.NewReader(bundle.payload))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	r := tar.NewReader(gz)
	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			return nil, os.ErrNotExist
		}
		if err != nil {
			return nil, err
		}
		if h.Name != "deploy/"+name {
			continue
		}
		if h.Typeflag != tar.TypeReg || h.Size < 0 || h.Size > MaxRuntimeWorkerFileBytes {
			return nil, ErrInvalidBundle
		}
		return io.ReadAll(io.LimitReader(r, MaxRuntimeWorkerFileBytes+1))
	}
}

func Pack(options PackOptions) (PackResult, error) {
	var config ReleaseConfig
	if err := decodeStrictFile(options.ConfigPath, &config, 1<<20); err != nil {
		return PackResult{}, err
	}
	if err := config.Validate(); err != nil {
		return PackResult{}, err
	}
	if !commitPattern.MatchString(options.Commit) {
		return PackResult{}, fmt.Errorf("invalid release commit")
	}
	if options.SourceDateEpoch.IsZero() {
		return PackResult{}, fmt.Errorf("source date epoch is required")
	}
	if options.BuildToolchain.GoVersion == "" || options.BuildToolchain.NodeVersion == "" || options.BuildToolchain.NPMVersion == "" {
		return PackResult{}, fmt.Errorf("complete build toolchain identity is required")
	}
	keyFile, privateKey, err := loadPrivateKey(options.SigningKeyPath)
	if err != nil {
		return PackResult{}, err
	}
	sources, err := collectPayload(options.PayloadDir)
	if err != nil {
		return PackResult{}, err
	}
	if err := validateRequiredPayload(sources, config.ProductVersion, config.DatabaseSchema.Current, config.Manual.Language); err != nil {
		return PackResult{}, err
	}
	migrations, err := db.LoadMigrationChecksums(filepath.Join(options.PayloadDir, "migrations"))
	if err != nil {
		return PackResult{}, err
	}
	currentSchema, err := strconv.Atoi(config.DatabaseSchema.Current)
	if err != nil || currentSchema != len(migrations) {
		return PackResult{}, fmt.Errorf("database_schema.current does not match packaged migration count")
	}
	if err := validateSchemaContractPayload(sources, config, migrations); err != nil {
		return PackResult{}, err
	}

	createdAt := options.SourceDateEpoch.UTC().Truncate(time.Second)
	payload, err := buildPayloadArchive(sources, createdAt)
	if err != nil {
		return PackResult{}, err
	}
	manifest := ReleaseManifest{
		SchemaVersion:     ManifestSchemaVersion,
		ProductName:       config.ProductName,
		ProductVersion:    config.ProductVersion,
		ReleaseChannel:    config.ReleaseChannel,
		Commit:            options.Commit,
		BuildToolchain:    options.BuildToolchain,
		TargetOS:          "linux",
		TargetArch:        "amd64",
		CGOEnabled:        false,
		ArtifactPath:      PayloadArtifactPath,
		ArtifactDigest:    sha256Reference(payload),
		ArtifactSizeBytes: int64(len(payload)),
		Signature: ManifestSignatureIdentity{
			Algorithm:      SignatureAlgorithm,
			KeyID:          keyFile.KeyID,
			TrustReference: config.TrustReference,
		},
		CreatedAt:                    createdAt,
		SupportedUpgradeFromVersions: append([]string(nil), config.SupportedUpgradeFromVersions...),
		SupportedLegacySources:       append([]LegacySource(nil), config.SupportedLegacySources...),
		ApplicationCompatibility:     config.ApplicationCompatibility,
		DatabaseSchema:               config.DatabaseSchema,
		MigrationChecksums:           migrations,
		ConfigSchema:                 config.ConfigSchema,
		APIVersions:                  append([]string(nil), config.APIVersions...),
		EvidenceSchemaVersions:       append([]string(nil), config.EvidenceSchemaVersions...),
		ReceiptSchemaVersions:        append([]string(nil), config.ReceiptSchemaVersions...),
		Manual:                       config.Manual,
		Rollback:                     config.Rollback,
		SchemaContract:               config.SchemaContract,
		Files:                        payloadEntries(sources),
	}
	if err := validateManifest(manifest); err != nil {
		return PackResult{}, err
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return PackResult{}, err
	}
	manifestBytes = append(manifestBytes, '\n')
	signature := DetachedSignature{
		SchemaVersion:  SignatureSchemaVersion,
		Algorithm:      SignatureAlgorithm,
		KeyID:          keyFile.KeyID,
		ManifestSHA256: sha256Reference(manifestBytes),
		Signature:      base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, manifestBytes)),
	}
	signatureBytes, err := json.MarshalIndent(signature, "", "  ")
	if err != nil {
		return PackResult{}, err
	}
	signatureBytes = append(signatureBytes, '\n')
	releaseName := "xinsight-v" + config.ProductVersion + "-linux-amd64"
	bundle, err := buildEnvelope(releaseName, createdAt, manifestBytes, signatureBytes, payload)
	if err != nil {
		return PackResult{}, err
	}
	if int64(len(bundle)) > MaxBundleBytes {
		return PackResult{}, fmt.Errorf("release bundle exceeds %d bytes", MaxBundleBytes)
	}
	if err := writeBytesExclusive(options.OutputPath, bundle, 0o644); err != nil {
		return PackResult{}, err
	}
	return PackResult{
		ReleaseName:    releaseName,
		OutputPath:     options.OutputPath,
		BundleSHA256:   sha256Reference(bundle),
		ManifestSHA256: signature.ManifestSHA256,
		PayloadSHA256:  manifest.ArtifactDigest,
		KeyID:          keyFile.KeyID,
		Manifest:       manifest,
	}, nil
}

func Verify(bundlePath string, options VerifyOptions) (VerifiedBundle, error) {
	bundleBytes, err := readBoundedRegularFile(bundlePath, MaxBundleBytes)
	if err != nil {
		return VerifiedBundle{}, err
	}
	releaseName, files, headers, err := readEnvelope(bundleBytes)
	if err != nil {
		return VerifiedBundle{}, err
	}
	manifestBytes := files["release-manifest.json"]
	signatureBytes := files["release-manifest.sig"]
	payload := files[PayloadArtifactPath]
	if len(manifestBytes) == 0 || len(signatureBytes) == 0 || len(payload) == 0 || len(files) != 3 {
		return VerifiedBundle{}, fmt.Errorf("%w: envelope file set", ErrInvalidBundle)
	}
	var manifest ReleaseManifest
	if err := decodeStrictBytes(manifestBytes, &manifest); err != nil {
		return VerifiedBundle{}, fmt.Errorf("%w: manifest: %v", ErrInvalidBundle, err)
	}
	if err := validateManifest(manifest); err != nil {
		return VerifiedBundle{}, err
	}
	if releaseName != "xinsight-v"+manifest.ProductVersion+"-linux-amd64" {
		return VerifiedBundle{}, fmt.Errorf("%w: envelope release name", ErrInvalidBundle)
	}
	for _, header := range headers {
		if !header.ModTime.Equal(manifest.CreatedAt) || header.Uid != 0 || header.Gid != 0 || header.Uname != "root" || header.Gname != "root" {
			return VerifiedBundle{}, fmt.Errorf("%w: envelope metadata", ErrInvalidBundle)
		}
	}
	var signature DetachedSignature
	if err := decodeStrictBytes(signatureBytes, &signature); err != nil {
		return VerifiedBundle{}, fmt.Errorf("%w: signature record: %v", ErrInvalidBundle, err)
	}
	if signature.SchemaVersion != SignatureSchemaVersion || signature.Algorithm != SignatureAlgorithm || signature.KeyID != manifest.Signature.KeyID ||
		signature.ManifestSHA256 != sha256Reference(manifestBytes) {
		return VerifiedBundle{}, fmt.Errorf("%w: signature metadata", ErrInvalidBundle)
	}
	store, err := LoadTrustStore(options.TrustStorePath)
	if err != nil {
		return VerifiedBundle{}, err
	}
	publicKey, err := trustedPublicKey(store, signature.KeyID, manifest.CreatedAt)
	if err != nil {
		return VerifiedBundle{}, err
	}
	signatureValue, err := base64.StdEncoding.Strict().DecodeString(signature.Signature)
	if err != nil || len(signatureValue) != ed25519.SignatureSize || !ed25519.Verify(publicKey, manifestBytes, signatureValue) {
		return VerifiedBundle{}, ErrUntrustedRelease
	}
	if manifest.ArtifactDigest != sha256Reference(payload) || manifest.ArtifactSizeBytes != int64(len(payload)) {
		return VerifiedBundle{}, fmt.Errorf("%w: payload digest", ErrInvalidBundle)
	}
	actualEntries, err := inspectPayload(payload, manifest.CreatedAt)
	if err != nil {
		return VerifiedBundle{}, err
	}
	if !equalFileEntries(actualEntries, manifest.Files) {
		return VerifiedBundle{}, fmt.Errorf("%w: payload file manifest", ErrInvalidBundle)
	}
	if err := validateRequiredEntries(actualEntries, manifest.ProductVersion, manifest.DatabaseSchema.Current, manifest.Manual.Language); err != nil {
		return VerifiedBundle{}, err
	}
	if options.ExpectedCommit != "" && manifest.Commit != options.ExpectedCommit {
		return VerifiedBundle{}, fmt.Errorf("%w: unexpected commit", ErrInvalidBundle)
	}
	if options.ExpectedVersion != "" && manifest.ProductVersion != options.ExpectedVersion {
		return VerifiedBundle{}, fmt.Errorf("%w: unexpected product version", ErrInvalidBundle)
	}
	expectedOS := options.ExpectedOS
	if expectedOS == "" {
		expectedOS = "linux"
	}
	expectedArch := options.ExpectedArch
	if expectedArch == "" {
		expectedArch = "amd64"
	}
	if manifest.TargetOS != expectedOS || manifest.TargetArch != expectedArch || manifest.CGOEnabled {
		return VerifiedBundle{}, ErrUnsupportedPlatform
	}
	verificationTime := options.Now
	if verificationTime.IsZero() {
		verificationTime = time.Now().UTC()
	}
	if manifest.CreatedAt.After(verificationTime.Add(5 * time.Minute)) {
		return VerifiedBundle{}, fmt.Errorf("%w: release creation time is in the future", ErrInvalidBundle)
	}
	return VerifiedBundle{
		ReleaseName:    releaseName,
		BundleSHA256:   sha256Reference(bundleBytes),
		ManifestSHA256: sha256Reference(manifestBytes),
		Manifest:       manifest,
		manifest:       bytes.Clone(manifestBytes),
		payload:        payload,
	}, nil
}

// VerifiedManifest returns the exact signed manifest retained by Verify. The
// private manifest bytes are the seal: callers cannot manufacture a verified
// release by populating the public diagnostic fields of VerifiedBundle.
func (bundle VerifiedBundle) VerifiedManifest() (ReleaseManifest, string, error) {
	if len(bundle.manifest) == 0 || len(bundle.payload) == 0 ||
		bundle.ManifestSHA256 != sha256Reference(bundle.manifest) {
		return ReleaseManifest{}, "", fmt.Errorf("%w: bundle has not been verified", ErrInvalidBundle)
	}
	var manifest ReleaseManifest
	if err := decodeStrictBytes(bundle.manifest, &manifest); err != nil {
		return ReleaseManifest{}, "", fmt.Errorf("%w: retained signed manifest", ErrInvalidBundle)
	}
	if err := validateManifest(manifest); err != nil {
		return ReleaseManifest{}, "", err
	}
	if manifest.ArtifactDigest != sha256Reference(bundle.payload) ||
		manifest.ArtifactSizeBytes != int64(len(bundle.payload)) {
		return ReleaseManifest{}, "", fmt.Errorf("%w: retained signed payload", ErrInvalidBundle)
	}
	return manifest, bundle.ManifestSHA256, nil
}

func ExtractPayload(bundle VerifiedBundle, destination string) error {
	if bundle.Manifest.SchemaVersion != ManifestSchemaVersion || len(bundle.payload) == 0 {
		return fmt.Errorf("%w: bundle has not been verified", ErrInvalidBundle)
	}
	if !filepath.IsAbs(destination) || filepath.Clean(destination) != destination {
		return fmt.Errorf("release destination must be an absolute clean path")
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("release destination already exists: %s", destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(destination, 0o755); err != nil {
		return err
	}
	if err := os.Chmod(destination, 0o755); err != nil {
		return err
	}
	reader := bytes.NewReader(bundle.payload)
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return err
	}
	gzipReader.Multistream(false)
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		rel, err := validateArchivePath(header.Name)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, filepath.FromSlash(rel))
		if !strings.HasPrefix(target, destination+string(os.PathSeparator)) {
			return fmt.Errorf("%w: extraction path", ErrInvalidBundle)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.Mkdir(target, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, err := os.Lstat(target)
			if err != nil {
				return err
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("%w: extraction directory", ErrInvalidBundle)
			}
			if err := os.Chmod(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Chmod(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if strings.HasPrefix(rel, "bin/") {
				mode = 0o755
			}
			file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if err != nil {
				return err
			}
			if err := file.Chmod(mode); err != nil {
				_ = file.Close()
				return err
			}
			if _, err := io.CopyN(file, tarReader, header.Size); err != nil {
				_ = file.Close()
				return err
			}
			if err := file.Sync(); err != nil {
				_ = file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: payload member type", ErrInvalidBundle)
		}
	}
	if err := finishGzipTarStream(gzipReader, reader, "payload"); err != nil {
		return err
	}
	return nil
}

func VerifyExtractedPayload(bundle VerifiedBundle, destination string) error {
	if bundle.Manifest.SchemaVersion != ManifestSchemaVersion || len(bundle.payload) == 0 {
		return fmt.Errorf("%w: bundle has not been verified", ErrInvalidBundle)
	}
	if !filepath.IsAbs(destination) || filepath.Clean(destination) != destination {
		return fmt.Errorf("release destination must be an absolute clean path")
	}
	root, err := os.Lstat(destination)
	if err != nil {
		return err
	}
	if !root.IsDir() || root.Mode()&os.ModeSymlink != 0 || root.Mode().Perm() != 0o755 {
		return fmt.Errorf("%w: extracted release root", ErrInvalidBundle)
	}
	sources, err := collectPayload(destination)
	if err != nil {
		return err
	}
	if !equalFileEntries(payloadEntries(sources), bundle.Manifest.Files) {
		return fmt.Errorf("%w: extracted payload file manifest", ErrInvalidBundle)
	}
	return filepath.WalkDir(destination, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == destination {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: extracted payload symlink", ErrInvalidBundle)
		}
		if entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode().Perm() != 0o755 {
				return fmt.Errorf("%w: extracted payload directory mode", ErrInvalidBundle)
			}
		}
		return nil
	})
}

func collectPayload(root string) ([]payloadSource, error) {
	rootInfo, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !rootInfo.IsDir() {
		return nil, fmt.Errorf("payload root is not a directory")
	}
	sources := make([]payloadSource, 0)
	err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == root {
			return nil
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if _, err := validateArchivePath(rel); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("payload symlink is forbidden: %s", rel)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > MaxPayloadFileBytes {
			return fmt.Errorf("payload file type or size is invalid: %s", rel)
		}
		contents, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		mode := "0644"
		if strings.HasPrefix(rel, "bin/") {
			mode = "0755"
		}
		sources = append(sources, payloadSource{
			contents: contents,
			entry:    FileEntry{Path: rel, SHA256: sha256Reference(contents), SizeBytes: int64(len(contents)), Mode: mode},
		})
		if len(sources) > MaxPayloadFiles {
			return fmt.Errorf("payload exceeds %d files", MaxPayloadFiles)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].entry.Path < sources[j].entry.Path })
	return sources, nil
}

func buildPayloadArchive(sources []payloadSource, createdAt time.Time) ([]byte, error) {
	var output bytes.Buffer
	gzipWriter, _ := gzip.NewWriterLevel(&output, gzip.BestCompression)
	gzipWriter.Header.ModTime = createdAt
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	directories := payloadDirectories(sources)
	for _, directory := range directories {
		if err := tarWriter.WriteHeader(archiveHeader(directory, 0, 0o755, tar.TypeDir, createdAt)); err != nil {
			return nil, err
		}
	}
	for _, source := range sources {
		mode := int64(0o644)
		if source.entry.Mode == "0755" {
			mode = 0o755
		}
		if err := tarWriter.WriteHeader(archiveHeader(source.entry.Path, source.entry.SizeBytes, mode, tar.TypeReg, createdAt)); err != nil {
			return nil, err
		}
		if _, err := tarWriter.Write(source.contents); err != nil {
			return nil, err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return nil, err
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func buildEnvelope(releaseName string, createdAt time.Time, manifest []byte, signature []byte, payload []byte) ([]byte, error) {
	var output bytes.Buffer
	gzipWriter, _ := gzip.NewWriterLevel(&output, gzip.BestCompression)
	gzipWriter.Header.ModTime = createdAt
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(archiveHeader(releaseName, 0, 0o755, tar.TypeDir, createdAt)); err != nil {
		return nil, err
	}
	entries := []struct {
		name string
		data []byte
	}{
		{name: "release-manifest.json", data: manifest},
		{name: "release-manifest.sig", data: signature},
		{name: PayloadArtifactPath, data: payload},
	}
	for _, entry := range entries {
		name := releaseName + "/" + entry.name
		if err := tarWriter.WriteHeader(archiveHeader(name, int64(len(entry.data)), 0o644, tar.TypeReg, createdAt)); err != nil {
			return nil, err
		}
		if _, err := tarWriter.Write(entry.data); err != nil {
			return nil, err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return nil, err
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func readEnvelope(bundle []byte) (string, map[string][]byte, []*tar.Header, error) {
	reader := bytes.NewReader(bundle)
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return "", nil, nil, fmt.Errorf("%w: envelope compression", ErrInvalidBundle)
	}
	gzipReader.Multistream(false)
	tarReader := tar.NewReader(gzipReader)
	files := make(map[string][]byte)
	headers := make([]*tar.Header, 0, 4)
	releaseName := ""
	for count := 0; ; count++ {
		if count > 10 {
			return "", nil, nil, fmt.Errorf("%w: envelope member count", ErrInvalidBundle)
		}
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", nil, nil, fmt.Errorf("%w: envelope tar", ErrInvalidBundle)
		}
		name, err := validateArchivePath(header.Name)
		if err != nil {
			return "", nil, nil, err
		}
		parts := strings.Split(name, "/")
		if releaseName == "" {
			releaseName = parts[0]
		}
		if parts[0] != releaseName {
			return "", nil, nil, fmt.Errorf("%w: multiple envelope roots", ErrInvalidBundle)
		}
		copyHeader := *header
		headers = append(headers, &copyHeader)
		if header.Typeflag == tar.TypeDir {
			if name != releaseName || header.Mode != 0o755 {
				return "", nil, nil, fmt.Errorf("%w: envelope directory", ErrInvalidBundle)
			}
			continue
		}
		if header.Typeflag != tar.TypeReg || len(parts) != 2 || header.Mode != 0o644 || header.Size <= 0 || header.Size > MaxBundleBytes {
			return "", nil, nil, fmt.Errorf("%w: envelope member", ErrInvalidBundle)
		}
		basename := parts[1]
		if basename != "release-manifest.json" && basename != "release-manifest.sig" && basename != PayloadArtifactPath {
			return "", nil, nil, fmt.Errorf("%w: unknown envelope member", ErrInvalidBundle)
		}
		if _, exists := files[basename]; exists {
			return "", nil, nil, fmt.Errorf("%w: duplicate envelope member", ErrInvalidBundle)
		}
		contents, err := io.ReadAll(io.LimitReader(tarReader, header.Size+1))
		if err != nil || int64(len(contents)) != header.Size {
			return "", nil, nil, fmt.Errorf("%w: envelope member bytes", ErrInvalidBundle)
		}
		files[basename] = contents
	}
	if err := finishGzipTarStream(gzipReader, reader, "envelope"); err != nil {
		return "", nil, nil, err
	}
	return releaseName, files, headers, nil
}

func inspectPayload(payload []byte, createdAt time.Time) ([]FileEntry, error) {
	reader := bytes.NewReader(payload)
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return nil, fmt.Errorf("%w: payload compression", ErrInvalidBundle)
	}
	gzipReader.Multistream(false)
	tarReader := tar.NewReader(gzipReader)
	entries := make([]FileEntry, 0)
	seen := make(map[string]struct{})
	for count := 0; ; count++ {
		if count > MaxPayloadFiles*2 {
			return nil, fmt.Errorf("%w: payload member count", ErrInvalidBundle)
		}
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: payload tar", ErrInvalidBundle)
		}
		name, err := validateArchivePath(header.Name)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("%w: duplicate payload path", ErrInvalidBundle)
		}
		seen[name] = struct{}{}
		if !header.ModTime.Equal(createdAt) || header.Uid != 0 || header.Gid != 0 || header.Uname != "root" || header.Gname != "root" {
			return nil, fmt.Errorf("%w: payload metadata", ErrInvalidBundle)
		}
		if header.Typeflag == tar.TypeDir {
			if header.Mode != 0o755 {
				return nil, fmt.Errorf("%w: payload directory mode", ErrInvalidBundle)
			}
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > MaxPayloadFileBytes {
			return nil, fmt.Errorf("%w: payload file", ErrInvalidBundle)
		}
		expectedMode := int64(0o644)
		modeText := "0644"
		if strings.HasPrefix(name, "bin/") {
			expectedMode = 0o755
			modeText = "0755"
		}
		if header.Mode != expectedMode {
			return nil, fmt.Errorf("%w: payload file mode", ErrInvalidBundle)
		}
		contents, err := io.ReadAll(io.LimitReader(tarReader, header.Size+1))
		if err != nil || int64(len(contents)) != header.Size {
			return nil, fmt.Errorf("%w: payload file bytes", ErrInvalidBundle)
		}
		entries = append(entries, FileEntry{Path: name, SHA256: sha256Reference(contents), SizeBytes: header.Size, Mode: modeText})
	}
	if err := finishGzipTarStream(gzipReader, reader, "payload"); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// tar.Reader returns io.EOF after the tar end markers, before gzip.Reader is
// necessarily driven through its checksum/footer. Drain that bounded remainder
// so a valid gzip footer is not mistaken for raw trailing bytes. Tar padding
// may contain only zeros; a second gzip member or bytes after the first member
// remain forbidden by Multistream(false) and the underlying-reader check.
func finishGzipTarStream(gzipReader *gzip.Reader, reader *bytes.Reader, scope string) error {
	buffer := make([]byte, 32<<10)
	paddingBytes := 0
	for {
		readBytes, err := gzipReader.Read(buffer)
		if readBytes > 0 {
			paddingBytes += readBytes
			if paddingBytes > maxTarEndPaddingBytes {
				return fmt.Errorf("%w: trailing %s bytes", ErrInvalidBundle, scope)
			}
			for _, value := range buffer[:readBytes] {
				if value != 0 {
					return fmt.Errorf("%w: trailing %s bytes", ErrInvalidBundle, scope)
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || readBytes == 0 {
			return fmt.Errorf("%w: %s compression trailer", ErrInvalidBundle, scope)
		}
	}
	if err := gzipReader.Close(); err != nil {
		return fmt.Errorf("%w: %s compression trailer", ErrInvalidBundle, scope)
	}
	if reader.Len() != 0 {
		return fmt.Errorf("%w: trailing %s bytes", ErrInvalidBundle, scope)
	}
	return nil
}

func validateManifest(manifest ReleaseManifest) error {
	config := ReleaseConfig{
		SchemaVersion:                ConfigSchemaVersion,
		ProductName:                  manifest.ProductName,
		ProductVersion:               manifest.ProductVersion,
		ReleaseChannel:               manifest.ReleaseChannel,
		TrustReference:               manifest.Signature.TrustReference,
		SupportedUpgradeFromVersions: manifest.SupportedUpgradeFromVersions,
		SupportedLegacySources:       manifest.SupportedLegacySources,
		ApplicationCompatibility:     manifest.ApplicationCompatibility,
		DatabaseSchema:               manifest.DatabaseSchema,
		ConfigSchema:                 manifest.ConfigSchema,
		APIVersions:                  manifest.APIVersions,
		EvidenceSchemaVersions:       manifest.EvidenceSchemaVersions,
		ReceiptSchemaVersions:        manifest.ReceiptSchemaVersions,
		Manual:                       manifest.Manual,
		Rollback:                     manifest.Rollback,
		SchemaContract:               manifest.SchemaContract,
	}
	if manifest.SchemaVersion != ManifestSchemaVersion || !commitPattern.MatchString(manifest.Commit) || manifest.CreatedAt.IsZero() ||
		manifest.TargetOS != "linux" || manifest.TargetArch != "amd64" || manifest.CGOEnabled || manifest.ArtifactPath != PayloadArtifactPath ||
		!validSHA256(manifest.ArtifactDigest) || manifest.ArtifactSizeBytes <= 0 || manifest.Signature.Algorithm != SignatureAlgorithm ||
		!keyIDPattern.MatchString(manifest.Signature.KeyID) {
		return fmt.Errorf("%w: release manifest identity", ErrInvalidBundle)
	}
	if err := config.Validate(); err != nil {
		return err
	}
	if err := validateMigrationChecksums(manifest.MigrationChecksums); err != nil {
		return err
	}
	if len(manifest.Files) == 0 || len(manifest.Files) > MaxPayloadFiles {
		return fmt.Errorf("%w: release manifest files", ErrInvalidBundle)
	}
	previous := ""
	for _, entry := range manifest.Files {
		if _, err := validateArchivePath(entry.Path); err != nil || !validSHA256(entry.SHA256) || entry.SizeBytes < 0 ||
			(entry.Mode != "0644" && entry.Mode != "0755") || (previous != "" && strings.Compare(previous, entry.Path) >= 0) {
			return fmt.Errorf("%w: release manifest file entry", ErrInvalidBundle)
		}
		previous = entry.Path
	}
	return validateRuntimeFileSizes(manifest.Files)
}

func validateRequiredPayload(sources []payloadSource, productVersion string, schema string, manualLanguage string) error {
	return validateRequiredEntries(payloadEntries(sources), productVersion, schema, manualLanguage)
}

type payloadSchemaContract struct {
	SchemaVersion      string                        `json:"schema_version"`
	DatabaseSchema     string                        `json:"database_schema"`
	PostgresMajor      int                           `json:"postgres_major"`
	MigrationCount     int                           `json:"migration_count"`
	MigrationSetSHA256 string                        `json:"migration_set_sha256"`
	SemanticSHA256     string                        `json:"semantic_sha256"`
	ColumnLayoutSHA256 string                        `json:"column_layout_sha256"`
	Objects            payloadSchemaContractObjects  `json:"objects"`
	Entries            []payloadSchemaContractObject `json:"entries"`
}

type payloadSchemaContractObjects struct {
	Namespaces  int `json:"namespaces"`
	Tables      int `json:"tables"`
	Columns     int `json:"columns"`
	Constraints int `json:"constraints"`
	Indexes     int `json:"indexes"`
	Triggers    int `json:"triggers"`
	Functions   int `json:"functions"`
	Extensions  int `json:"extensions"`
	Types       int `json:"types"`
	Relations   int `json:"relations"`
}

type payloadSchemaContractObject struct {
	Kind           string `json:"kind"`
	Namespace      string `json:"namespace"`
	Owner          string `json:"owner,omitempty"`
	Name           string `json:"name"`
	SemanticSHA256 string `json:"semantic_sha256"`
}

func validateSchemaContractPayload(
	sources []payloadSource,
	config ReleaseConfig,
	migrations []db.MigrationChecksum,
) error {
	if !requiresGeneratedSchemaContract(config.ProductVersion) {
		return nil
	}
	var payload []byte
	for _, source := range sources {
		if source.entry.Path == "release/schema-contract.json" {
			payload = source.contents
			break
		}
	}
	var contract payloadSchemaContract
	if len(payload) == 0 || decodeStrictBytes(payload, &contract) != nil {
		return fmt.Errorf("%w: generated schema contract payload", ErrInvalidBundle)
	}
	migrationDigest := sha256.New()
	for _, migration := range migrations {
		_, _ = fmt.Fprintf(migrationDigest, "%s\t%s\n", migration.ID, migration.SHA256)
	}
	expectedMigrationSet := "sha256:" + hex.EncodeToString(migrationDigest.Sum(nil))
	if contract.SchemaVersion != config.SchemaContract.SchemaVersion ||
		contract.DatabaseSchema != config.DatabaseSchema.Current || contract.PostgresMajor != 16 ||
		contract.MigrationCount != len(migrations) || contract.MigrationSetSHA256 != expectedMigrationSet ||
		contract.MigrationSetSHA256 != config.SchemaContract.MigrationSetSHA256 ||
		contract.SemanticSHA256 != config.SchemaContract.SemanticSHA256 ||
		!validSHA256(contract.SemanticSHA256) || !validSHA256(contract.ColumnLayoutSHA256) ||
		len(contract.Entries) == 0 {
		return fmt.Errorf("%w: generated schema contract identity", ErrInvalidBundle)
	}
	semanticDigest := sha256.New()
	objects := payloadSchemaContractObjects{}
	previous := ""
	for _, entry := range contract.Entries {
		key := strings.Join([]string{entry.Kind, entry.Namespace, entry.Owner, entry.Name}, "\x00")
		if entry.Namespace == "" || (entry.Owner == "" && entry.Name == "") || !validSHA256(entry.SemanticSHA256) ||
			(previous != "" && previous >= key) || !incrementPayloadSchemaContractObject(&objects, entry.Kind) {
			return fmt.Errorf("%w: generated schema contract object", ErrInvalidBundle)
		}
		previous = key
		encoded, _ := json.Marshal(entry)
		_, _ = semanticDigest.Write(encoded)
		_, _ = semanticDigest.Write([]byte{'\n'})
	}
	computedSemantic := "sha256:" + hex.EncodeToString(semanticDigest.Sum(nil))
	if objects != contract.Objects || computedSemantic != contract.SemanticSHA256 {
		return fmt.Errorf("%w: generated schema contract contents", ErrInvalidBundle)
	}
	return nil
}

func incrementPayloadSchemaContractObject(objects *payloadSchemaContractObjects, kind string) bool {
	switch kind {
	case "N":
		objects.Namespaces++
	case "T":
		objects.Tables++
	case "C":
		objects.Columns++
	case "K":
		objects.Constraints++
	case "I":
		objects.Indexes++
	case "G":
		objects.Triggers++
	case "F":
		objects.Functions++
	case "E":
		objects.Extensions++
	case "Y":
		objects.Types++
	case "R":
		objects.Relations++
	default:
		return false
	}
	return true
}

func validateRequiredEntries(entries []FileEntry, productVersion string, schema string, manualLanguage string) error {
	paths := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		paths[entry.Path] = struct{}{}
	}
	requiredPaths := []string{
		"bin/xinsight-server",
		"bin/xinsight-importer",
		"bin/xinsightctl",
		"bin/xinsight-launcher",
		"manual/zh-CN/index.html",
		"manual/zh-CN/manual.css",
		"manual/zh-CN/manual-icon.svg",
		"release/compatibility-matrix.json",
		"release/release-notes.md",
		"deploy/xinsight.service",
		"deploy/xinsight.env.example",
		"deploy/postgres.compose.yml",
		"deploy/postgres.env.example",
	}
	// English-default releases include both manuals and redistributable notices.
	// Historical Chinese-only signed artifacts retain their exact contract.
	if manualLanguage == "en-US" {
		requiredPaths = append(requiredPaths,
			"manual/en-US/index.html", "manual/en-US/manual.css", "manual/en-US/manual-icon.svg",
			"LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md", "licenses/manifest.json",
		)
	}
	if version, err := strconv.Atoi(schema); err == nil && version >= 32 {
		requiredPaths = append(requiredPaths, "deploy/xinsight-source-facts.service", "deploy/source-facts.json.example")
	}
	// rc.3 predates the signed managed-launcher drop-in. Keep this exception
	// bound to that exact historical product identity so current and unknown
	// release versions remain fail-closed when the runtime bridge is absent.
	if productVersion != "0.2.0-rc.3" {
		requiredPaths = append(requiredPaths, "deploy/99-xinsight-managed-launcher.conf")
	}
	if requiresManagedFeedWorker(productVersion) {
		requiredPaths = append(requiredPaths,
			"deploy/xinsight-feed.service",
			"deploy/xinsight-feed.env.example",
		)
	}
	if requiresManagedLiteLLMWorker(productVersion) {
		requiredPaths = append(requiredPaths,
			"deploy/xinsight-litellm-feed.service",
			"deploy/xinsight-litellm-feed.env.example",
		)
	}
	if requiresReceiverGeneratedFeedCredential(productVersion) {
		requiredPaths = append(
			requiredPaths,
			"schemas/integration/feed-credential-registration-v1.schema.json",
		)
	}
	if requiresOperatorAccessPlane(productVersion) {
		requiredPaths = append(
			requiredPaths,
			"schemas/access/local-accounts-v1.schema.json",
		)
	}
	if requiresTrustedTLSProxyTemplates(productVersion) {
		requiredPaths = append(
			requiredPaths,
			"deploy/trusted-tls-proxy.env.example",
			"deploy/xinsight-nginx-ip.conf.example",
		)
	}
	if requiresCheckpointEpochControl(productVersion) {
		requiredPaths = append(
			requiredPaths,
			"schemas/integration/connector-checkpoint-epoch-prepare-v1.schema.json",
		)
	}
	if requiresGeneratedSchemaContract(productVersion) {
		requiredPaths = append(requiredPaths, "release/schema-contract.json")
	}
	if requiresConnectorController(productVersion) {
		requiredPaths = append(requiredPaths,
			"deploy/xinsight-connector-controller.service",
			"deploy/xinsight-connector.env.example",
		)
	}
	for _, required := range requiredPaths {
		if _, exists := paths[required]; !exists {
			return fmt.Errorf("%w: missing required payload file %s", ErrInvalidBundle, required)
		}
	}
	for _, prefix := range []string{"migrations/", "schemas/", "fixtures/"} {
		found := false
		for file := range paths {
			if strings.HasPrefix(file, prefix) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%w: empty required payload directory %s", ErrInvalidBundle, prefix)
		}
	}
	return nil
}

func requiresManagedFeedWorker(productVersion string) bool {
	switch productVersion {
	case "0.2.0-rc.1", "0.2.0-rc.2", "0.2.0-rc.3", "0.2.0-rc.4", "0.2.0-rc.5", "0.2.0-rc.6":
		return false
	default:
		return true
	}
}

func requiresReceiverGeneratedFeedCredential(productVersion string) bool {
	switch productVersion {
	case "0.2.0-rc.1", "0.2.0-rc.2", "0.2.0-rc.3", "0.2.0-rc.4",
		"0.2.0-rc.5", "0.2.0-rc.6", "0.2.0-rc.7", "0.2.0-rc.8":
		return false
	default:
		return true
	}
}

func requiresManagedLiteLLMWorker(productVersion string) bool {
	switch productVersion {
	case "0.2.0-rc.1", "0.2.0-rc.2", "0.2.0-rc.3", "0.2.0-rc.4",
		"0.2.0-rc.5", "0.2.0-rc.6", "0.2.0-rc.7", "0.2.0-rc.8",
		"0.2.0-rc.9", "0.2.0-rc.10", "0.2.0-rc.11":
		return false
	default:
		return true
	}
}

func requiresOperatorAccessPlane(productVersion string) bool {
	switch productVersion {
	case "0.2.0-rc.1", "0.2.0-rc.2", "0.2.0-rc.3", "0.2.0-rc.4",
		"0.2.0-rc.5", "0.2.0-rc.6", "0.2.0-rc.7", "0.2.0-rc.8",
		"0.2.0-rc.9", "0.2.0-rc.10", "0.2.0-rc.11", "0.2.0-rc.12":
		return false
	default:
		return true
	}
}

func requiresTrustedTLSProxyTemplates(productVersion string) bool {
	switch productVersion {
	case "0.2.0-rc.1", "0.2.0-rc.2", "0.2.0-rc.3", "0.2.0-rc.4",
		"0.2.0-rc.5", "0.2.0-rc.6", "0.2.0-rc.7", "0.2.0-rc.8",
		"0.2.0-rc.9", "0.2.0-rc.10", "0.2.0-rc.11", "0.2.0-rc.12",
		"0.2.0-rc.13":
		return false
	default:
		return true
	}
}

func requiresCheckpointEpochControl(productVersion string) bool {
	switch productVersion {
	case "0.2.0-rc.1", "0.2.0-rc.2", "0.2.0-rc.3", "0.2.0-rc.4",
		"0.2.0-rc.5", "0.2.0-rc.6", "0.2.0-rc.7", "0.2.0-rc.8",
		"0.2.0-rc.9", "0.2.0-rc.10", "0.2.0-rc.11", "0.2.0-rc.12",
		"0.2.0-rc.13", "0.2.0-rc.14", "0.2.0-rc.15", "0.2.0",
		"0.3.0-rc.2", "0.3.0-rc.3", "0.3.0-rc.4", "0.3.0-rc.5",
		"0.3.0-rc.6":
		return false
	default:
		return true
	}
}

func requiresGeneratedSchemaContract(productVersion string) bool {
	switch productVersion {
	case "0.2.0-rc.1", "0.2.0-rc.2", "0.2.0-rc.3", "0.2.0-rc.4",
		"0.2.0-rc.5", "0.2.0-rc.6", "0.2.0-rc.7", "0.2.0-rc.8",
		"0.2.0-rc.9", "0.2.0-rc.10", "0.2.0-rc.11", "0.2.0-rc.12",
		"0.2.0-rc.13", "0.2.0-rc.14", "0.2.0-rc.15", "0.2.0",
		"0.3.0-rc.1", "0.3.0-rc.2", "0.3.0-rc.3", "0.3.0-rc.4",
		"0.3.0-rc.5", "0.3.0-rc.6", "0.3.0-rc.7", "0.3.0-rc.8":
		return false
	default:
		return true
	}
}

func requiresConnectorController(productVersion string) bool {
	switch productVersion {
	case "0.2.0-rc.1", "0.2.0-rc.2", "0.2.0-rc.3", "0.2.0-rc.4",
		"0.2.0-rc.5", "0.2.0-rc.6", "0.2.0-rc.7", "0.2.0-rc.8",
		"0.2.0-rc.9", "0.2.0-rc.10", "0.2.0-rc.11", "0.2.0-rc.12",
		"0.2.0-rc.13", "0.2.0-rc.14", "0.2.0-rc.15", "0.2.0",
		"0.3.0-rc.1", "0.3.0-rc.2", "0.3.0-rc.3", "0.3.0-rc.4",
		"0.3.0-rc.5", "0.3.0-rc.6", "0.3.0-rc.7", "0.3.0-rc.8",
		"0.3.0-rc.9", "0.3.0-rc.10", "0.3.0-rc.11":
		return false
	default:
		return true
	}
}

func payloadEntries(sources []payloadSource) []FileEntry {
	entries := make([]FileEntry, len(sources))
	for index, source := range sources {
		entries[index] = source.entry
	}
	return entries
}

func payloadDirectories(sources []payloadSource) []string {
	directories := make(map[string]struct{})
	for _, source := range sources {
		parent := path.Dir(source.entry.Path)
		for parent != "." {
			directories[parent] = struct{}{}
			parent = path.Dir(parent)
		}
	}
	values := make([]string, 0, len(directories))
	for directory := range directories {
		values = append(values, directory)
	}
	sort.Strings(values)
	return values
}

func archiveHeader(name string, size int64, mode int64, typeFlag byte, createdAt time.Time) *tar.Header {
	if typeFlag == tar.TypeDir {
		name = strings.TrimSuffix(name, "/") + "/"
	}
	return &tar.Header{
		Name:       name,
		Size:       size,
		Mode:       mode,
		Typeflag:   typeFlag,
		Uid:        0,
		Gid:        0,
		Uname:      "root",
		Gname:      "root",
		ModTime:    createdAt,
		AccessTime: time.Time{},
		ChangeTime: time.Time{},
		Format:     tar.FormatPAX,
	}
}

func validateArchivePath(value string) (string, error) {
	if value == "" || strings.Contains(value, "\\") || path.IsAbs(value) {
		return "", fmt.Errorf("%w: unsafe archive path", ErrInvalidBundle)
	}
	clean := path.Clean(strings.TrimSuffix(value, "/"))
	if clean == "." || clean != strings.TrimSuffix(value, "/") || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("%w: unsafe archive path", ErrInvalidBundle)
	}
	for _, component := range strings.Split(clean, "/") {
		if component == ".git" || component == "" {
			return "", fmt.Errorf("%w: forbidden archive path", ErrInvalidBundle)
		}
	}
	return clean, nil
}

func equalFileEntries(left []FileEntry, right []FileEntry) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func readBoundedRegularFile(filePath string, limit int64) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return nil, fmt.Errorf("file size or type is invalid: %s", filePath)
	}
	contents, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(contents)) != info.Size() {
		return nil, fmt.Errorf("file changed while reading: %s", filePath)
	}
	return contents, nil
}

func writeBytesExclusive(filePath string, contents []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func sha256Reference(contents []byte) string {
	digest := sha256.Sum256(contents)
	return "sha256:" + hex.EncodeToString(digest[:])
}
