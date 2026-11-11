package releasebundle

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
)

const (
	DeltaPlanSchema = "xinsight.release-delta.v1"
	DeltaCodec      = "zstd-raw-tar-v1"
	// Keep this profile separate from the full-bundle format. A release
	// outside these limits still uses the ordinary full-bundle path.
	MaxDeltaRawBytes = 64 << 20
	MaxDeltaWindow   = 64 << 20
	maxDeltaBundle   = 64 << 20
	maxDeltaPatch    = 32 << 20
	maxDeltaManifest = 1 << 20
	maxDeltaSig      = 16 << 10
	maxDeltaPlan     = 16 << 10
)

type DeltaBlob struct {
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type DeltaRelease struct {
	Commit  string    `json:"commit"`
	Version string    `json:"version"`
	Bundle  DeltaBlob `json:"bundle"`
	RawTar  DeltaBlob `json:"raw_tar"`
}

// DeltaPlan is an input inventory. Its digest must come from the independently
// reviewed delivery record; it does not grant trust in either release.
type DeltaPlan struct {
	SchemaVersion string       `json:"schema_version"`
	Codec         string       `json:"codec"`
	TargetOS      string       `json:"target_os"`
	TargetArch    string       `json:"target_arch"`
	Basis         DeltaRelease `json:"basis"`
	Target        DeltaRelease `json:"target"`
	Patch         DeltaBlob    `json:"patch"`
	Manifest      DeltaBlob    `json:"manifest"`
	Signature     DeltaBlob    `json:"signature"`
}

type DeltaOptions struct {
	PlanPath, PlanSHA256, BasisPath, PatchPath  string
	ManifestPath, SignaturePath, TrustStorePath string
	// An empty OutputPath is a check: run all verification without writes.
	OutputPath string
}

// DeltaDecoder is supplied by the standalone release tool so the product
// binaries do not import the codec. All decoded bytes remain untrusted.
type DeltaDecoder func(basis, patch []byte, rawBytes int64) (io.ReadCloser, error)

// MaterializeDelta builds and verifies the exact normal release bundle. It
// never examines or changes install state, or invokes the native upgrader.
func MaterializeDelta(ctx context.Context, options DeltaOptions, decode DeltaDecoder) (VerifiedBundle, error) {
	if err := ctx.Err(); err != nil {
		return VerifiedBundle{}, err
	}
	if decode == nil || !validSHA256(options.PlanSHA256) {
		return VerifiedBundle{}, fmt.Errorf("%w: delta requires a decoder and the reviewed plan digest", ErrInvalidBundle)
	}
	if options.OutputPath != "" {
		if err := checkDeltaOutput(options.OutputPath); err != nil {
			return VerifiedBundle{}, err
		}
	}
	planBytes, err := readBoundedRegularFile(options.PlanPath, maxDeltaPlan)
	if err != nil {
		return VerifiedBundle{}, fmt.Errorf("delta plan: %w", err)
	}
	if sha256Reference(planBytes) != options.PlanSHA256 {
		return VerifiedBundle{}, fmt.Errorf("%w: delta plan digest", ErrInvalidBundle)
	}
	var plan DeltaPlan
	if err := decodeStrictBytes(planBytes, &plan); err != nil {
		return VerifiedBundle{}, fmt.Errorf("%w: delta plan: %v", ErrInvalidBundle, err)
	}
	if err := plan.validate(); err != nil {
		return VerifiedBundle{}, err
	}
	basisOptions := plan.verifyOptions(plan.Basis, options.TrustStorePath)
	targetOptions := plan.verifyOptions(plan.Target, options.TrustStorePath)

	manifestBytes, err := readDeltaBlob(options.ManifestPath, plan.Manifest, "target manifest")
	if err != nil {
		return VerifiedBundle{}, err
	}
	signatureBytes, err := readDeltaBlob(options.SignaturePath, plan.Signature, "target signature")
	if err != nil {
		return VerifiedBundle{}, err
	}
	var manifest ReleaseManifest
	if err := decodeStrictBytes(manifestBytes, &manifest); err != nil {
		return VerifiedBundle{}, fmt.Errorf("%w: target manifest: %v", ErrInvalidBundle, err)
	}
	if err := validateManifest(manifest); err != nil {
		return VerifiedBundle{}, err
	}
	if err := verifyManifestSignature(manifest, manifestBytes, signatureBytes, options.TrustStorePath); err != nil {
		return VerifiedBundle{}, fmt.Errorf("delta target: %w", err)
	}
	if err := verifyManifestExpectations(manifest, targetOptions); err != nil {
		return VerifiedBundle{}, err
	}
	if !slices.Contains(manifest.SupportedUpgradeFromVersions, plan.Basis.Version) {
		return VerifiedBundle{}, fmt.Errorf("%w: signed target does not admit the basis version", ErrInvalidBundle)
	}
	if manifest.BuildToolchain.GoVersion != runtime.Version() || manifest.ArtifactSizeBytes > maxDeltaBundle {
		return VerifiedBundle{}, fmt.Errorf("%w: target is outside the delta gzip toolchain or size profile", ErrInvalidBundle)
	}

	basisBytes, err := readDeltaBlob(options.BasisPath, plan.Basis.Bundle, "basis bundle")
	if err != nil {
		return VerifiedBundle{}, err
	}
	basis, err := verifyBundleBytes(basisBytes, basisOptions)
	if err != nil {
		return VerifiedBundle{}, fmt.Errorf("delta basis: %w", err)
	}
	rawBasis, err := inflateDeltaBasis(ctx, basis.payload, plan.Basis.RawTar)
	if err != nil {
		return VerifiedBundle{}, err
	}
	patch, err := readDeltaBlob(options.PatchPath, plan.Patch, "patch")
	if err != nil {
		return VerifiedBundle{}, err
	}
	if err := ctx.Err(); err != nil {
		return VerifiedBundle{}, err
	}
	payload, err := reconstructDeltaPayload(ctx, decode, rawBasis, patch, plan.Target.RawTar, manifest)
	if err != nil {
		return VerifiedBundle{}, err
	}
	if err := ctx.Err(); err != nil {
		return VerifiedBundle{}, err
	}

	name := "xinsight-v" + manifest.ProductVersion + "-linux-amd64"
	bundle, err := buildEnvelope(name, manifest.CreatedAt, manifestBytes, signatureBytes, payload)
	if err != nil {
		return VerifiedBundle{}, err
	}
	if int64(len(bundle)) != plan.Target.Bundle.Bytes || sha256Reference(bundle) != plan.Target.Bundle.SHA256 {
		return VerifiedBundle{}, fmt.Errorf("%w: reconstructed full bundle identity", ErrInvalidBundle)
	}
	verified, err := verifyBundleBytes(bundle, targetOptions)
	if err != nil {
		return VerifiedBundle{}, fmt.Errorf("reconstructed full bundle: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return VerifiedBundle{}, err
	}
	if options.OutputPath != "" {
		if err := publishDeltaOutput(ctx, options.OutputPath, bundle); err != nil {
			return VerifiedBundle{}, err
		}
	}
	return verified, nil
}

func (plan DeltaPlan) validate() error {
	if plan.SchemaVersion != DeltaPlanSchema || plan.Codec != DeltaCodec {
		return fmt.Errorf("%w: delta schema or codec", ErrInvalidBundle)
	}
	if plan.TargetOS != "linux" || plan.TargetArch != "amd64" {
		return ErrUnsupportedPlatform
	}
	if !commitPattern.MatchString(plan.Basis.Commit) || !commitPattern.MatchString(plan.Target.Commit) ||
		!semverPattern.MatchString(plan.Basis.Version) || !semverPattern.MatchString(plan.Target.Version) ||
		plan.Basis.Version == plan.Target.Version {
		return fmt.Errorf("%w: delta release identities", ErrInvalidBundle)
	}
	for _, entry := range []struct {
		blob DeltaBlob
		max  int64
	}{
		{plan.Basis.Bundle, maxDeltaBundle}, {plan.Target.Bundle, maxDeltaBundle},
		{plan.Basis.RawTar, MaxDeltaRawBytes}, {plan.Target.RawTar, MaxDeltaRawBytes},
		{plan.Patch, maxDeltaPatch}, {plan.Manifest, maxDeltaManifest}, {plan.Signature, maxDeltaSig},
	} {
		if entry.blob.Bytes <= 0 || entry.blob.Bytes > entry.max || !validSHA256(entry.blob.SHA256) {
			return fmt.Errorf("%w: delta input size or digest is outside the profile", ErrInvalidBundle)
		}
	}
	return nil
}

func (plan DeltaPlan) verifyOptions(release DeltaRelease, trustStore string) VerifyOptions {
	return VerifyOptions{
		TrustStorePath: trustStore, ExpectedCommit: release.Commit, ExpectedVersion: release.Version,
		ExpectedOS: plan.TargetOS, ExpectedArch: plan.TargetArch,
	}
}

func readDeltaBlob(path string, expected DeltaBlob, label string) ([]byte, error) {
	data, err := readBoundedRegularFile(path, expected.Bytes)
	if err != nil {
		return nil, fmt.Errorf("delta %s: %w", label, err)
	}
	if int64(len(data)) != expected.Bytes || sha256Reference(data) != expected.SHA256 {
		return nil, fmt.Errorf("%w: delta %s identity", ErrInvalidBundle, label)
	}
	return data, nil
}

func inflateDeltaBasis(ctx context.Context, compressed []byte, expected DeltaBlob) ([]byte, error) {
	underlying := bytes.NewReader(compressed)
	gz, err := gzip.NewReader(underlying)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	gz.Multistream(false)
	raw := make([]byte, expected.Bytes)
	reader := deltaContextReader{ctx, gz}
	if _, err := io.ReadFull(reader, raw); err != nil {
		return nil, fmt.Errorf("delta basis raw tar: %w", err)
	}
	if err := deltaRequireEOF(reader); err != nil {
		return nil, fmt.Errorf("delta basis raw tar: %w", err)
	}
	if underlying.Len() != 0 || sha256Reference(raw) != expected.SHA256 {
		return nil, fmt.Errorf("%w: delta basis raw tar identity", ErrInvalidBundle)
	}
	return raw, nil
}

func reconstructDeltaPayload(ctx context.Context, decode DeltaDecoder, basis, patch []byte, expected DeltaBlob, manifest ReleaseManifest) ([]byte, error) {
	decoded, err := decode(basis, patch, expected.Bytes)
	if err != nil {
		return nil, fmt.Errorf("delta decoder: %w", err)
	}
	defer decoded.Close()
	reader := deltaContextReader{ctx, decoded}
	compressed := &deltaPayloadBuffer{limit: manifest.ArtifactSizeBytes}
	compressed.Grow(int(manifest.ArtifactSizeBytes))
	gz := newArchiveGzipWriter(compressed, manifest.CreatedAt)
	rawHash := sha256.New()
	if _, err := io.CopyN(io.MultiWriter(gz, rawHash), reader, expected.Bytes); err != nil {
		return nil, fmt.Errorf("delta target raw tar: %w", err)
	}
	if err := deltaRequireEOF(reader); err != nil {
		return nil, fmt.Errorf("delta target raw tar: %w", err)
	}
	if "sha256:"+hex.EncodeToString(rawHash.Sum(nil)) != expected.SHA256 {
		return nil, fmt.Errorf("%w: delta target raw tar digest", ErrInvalidBundle)
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	if int64(compressed.Len()) != manifest.ArtifactSizeBytes || sha256Reference(compressed.Bytes()) != manifest.ArtifactDigest {
		return nil, fmt.Errorf("%w: reconstructed signed payload identity", ErrInvalidBundle)
	}
	return compressed.Bytes(), nil
}

type deltaContextReader struct {
	ctx context.Context
	io.Reader
}

func (r deltaContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

func deltaRequireEOF(reader io.Reader) error {
	var extra [1]byte
	n, err := reader.Read(extra[:])
	if n == 0 && errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return fmt.Errorf("%w: delta raw tar exceeds declared size", ErrInvalidBundle)
}

type deltaPayloadBuffer struct {
	bytes.Buffer
	limit int64
}

func (b *deltaPayloadBuffer) Write(data []byte) (int, error) {
	if int64(len(data)) > b.limit-int64(b.Len()) {
		return 0, fmt.Errorf("%w: gzip output exceeds signed size", ErrInvalidBundle)
	}
	return b.Buffer.Write(data)
}

func checkDeltaOutput(output string) error {
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		return fmt.Errorf("delta output must be an absolute clean path")
	}
	dir, err := os.Lstat(filepath.Dir(output))
	if err != nil {
		return err
	}
	if !dir.IsDir() || dir.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("delta output requires an existing private staging directory")
	}
	if _, err := os.Lstat(output); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return fmt.Errorf("delta output already exists: %w", os.ErrExist)
		}
		return err
	}
	return nil
}

func publishDeltaOutput(ctx context.Context, output string, bundle []byte) error {
	file, err := os.CreateTemp(filepath.Dir(output), ".xinsight-delta-*.part")
	if err != nil {
		return err
	}
	stage := file.Name()
	defer os.Remove(stage)
	defer file.Close()
	if _, err := io.Copy(file, deltaContextReader{ctx, bytes.NewReader(bundle)}); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Same directory and atomic no-replace: neither a concurrent writer nor
	// interruption during the staging write exposes a partial final bundle.
	return os.Link(stage, output)
}
