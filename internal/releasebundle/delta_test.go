package releasebundle_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/releasebundle"
)

func TestDeltaMaterializesOriginalSignedBundle(t *testing.T) {
	f := newDeltaFixture(t)
	check, err := releasebundle.MaterializeDelta(context.Background(), f.options, f.decode)
	if err != nil {
		t.Fatal(err)
	}
	if check.BundleSHA256 != f.plan.Target.Bundle.SHA256 {
		t.Fatalf("check returned %s", check.BundleSHA256)
	}
	assertDeltaOutputAbsent(t, f.output)
	f.options.OutputPath = f.output
	result, err := releasebundle.MaterializeDelta(context.Background(), f.options, f.decode)
	if err != nil {
		t.Fatal(err)
	}
	if got := deltaRead(t, f.output); !bytes.Equal(got, deltaRead(t, f.target)) {
		t.Fatal("materialized bytes differ from original signed bundle")
	}
	if result.BundleSHA256 != check.BundleSHA256 {
		t.Fatal("materialization does not match check")
	}
	if _, err := releasebundle.Verify(f.output, releasebundle.VerifyOptions{
		TrustStorePath: f.options.TrustStorePath, ExpectedCommit: f.plan.Target.Commit,
		ExpectedVersion: f.plan.Target.Version,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := releasebundle.MaterializeDelta(context.Background(), f.options, f.decode); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing output: %v", err)
	}
}

func TestDeltaRejectsInvalidInputsWithoutPublishing(t *testing.T) {
	f := newDeltaFixture(t)
	for _, tc := range []struct {
		name, want string
		change     func(*releasebundle.DeltaOptions, *releasebundle.DeltaPlan)
	}{
		{"wrong basis digest", "basis bundle identity", func(o *releasebundle.DeltaOptions, p *releasebundle.DeltaPlan) {
			p.Basis.Bundle.SHA256 = deltaBlob([]byte("wrong basis")).SHA256
		}},
		{"missing basis", "basis bundle", func(o *releasebundle.DeltaOptions, p *releasebundle.DeltaPlan) {
			o.BasisPath += ".missing"
		}},
		{"wrong basis commit", "unexpected commit", func(o *releasebundle.DeltaOptions, p *releasebundle.DeltaPlan) {
			p.Basis.Commit = strings.Repeat("f", 40)
		}},
		{"wrong raw basis", "basis raw tar identity", func(o *releasebundle.DeltaOptions, p *releasebundle.DeltaPlan) {
			p.Basis.RawTar.SHA256 = deltaBlob([]byte("wrong raw basis")).SHA256
		}},
		{"wrong target commit", "unexpected commit", func(o *releasebundle.DeltaOptions, p *releasebundle.DeltaPlan) {
			p.Target.Commit = strings.Repeat("f", 40)
		}},
		{"unadmitted predecessor", "does not admit the basis version", func(o *releasebundle.DeltaOptions, p *releasebundle.DeltaPlan) {
			p.Basis.Version = "0.2.0-rc.2"
		}},
		{"unsupported platform", "target is not supported", func(o *releasebundle.DeltaOptions, p *releasebundle.DeltaPlan) {
			p.TargetArch = "arm64"
		}},
		{"same version", "delta release identities", func(o *releasebundle.DeltaOptions, p *releasebundle.DeltaPlan) {
			p.Target.Version = p.Basis.Version
		}},
		{"wrong patch digest", "patch identity", func(o *releasebundle.DeltaOptions, p *releasebundle.DeltaPlan) {
			p.Patch.SHA256 = deltaBlob([]byte("wrong patch")).SHA256
		}},
		{"short decoded tar", "target raw tar", func(o *releasebundle.DeltaOptions, p *releasebundle.DeltaPlan) {
			p.Target.RawTar.Bytes++
		}},
		{"long decoded tar", "exceeds declared size", func(o *releasebundle.DeltaOptions, p *releasebundle.DeltaPlan) {
			p.Target.RawTar.Bytes--
		}},
		{"wrong decoded tar digest", "target raw tar digest", func(o *releasebundle.DeltaOptions, p *releasebundle.DeltaPlan) {
			p.Target.RawTar.SHA256 = deltaBlob([]byte("wrong raw target")).SHA256
		}},
		{"raw limit", "outside the profile", func(o *releasebundle.DeltaOptions, p *releasebundle.DeltaPlan) {
			p.Target.RawTar.Bytes = releasebundle.MaxDeltaRawBytes + 1
		}},
		{"wrong full bundle", "full bundle identity", func(o *releasebundle.DeltaOptions, p *releasebundle.DeltaPlan) {
			p.Target.Bundle.SHA256 = deltaBlob([]byte("wrong full bundle")).SHA256
		}},
		{"untrusted signature", "signature is not trusted", func(o *releasebundle.DeltaOptions, p *releasebundle.DeltaPlan) {
			sig := deltaRead(t, o.SignaturePath)
			var record releasebundle.DetachedSignature
			if err := json.Unmarshal(sig, &record); err != nil {
				t.Fatal(err)
			}
			if record.Signature[0] == 'A' {
				record.Signature = "B" + record.Signature[1:]
			} else {
				record.Signature = "A" + record.Signature[1:]
			}
			o.SignaturePath += ".bad"
			sig = deltaJSON(t, record)
			deltaWrite(t, o.SignaturePath, sig)
			p.Signature = deltaBlob(sig)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o, p := f.options, f.plan
			o.OutputPath = f.output
			tc.change(&o, &p)
			setDeltaPlan(t, &o, p)
			if _, err := releasebundle.MaterializeDelta(context.Background(), o, f.decode); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %q, got %v", tc.want, err)
			}
			assertDeltaOutputAbsent(t, f.output)
		})
	}
	badPlan := f.options
	badPlan.OutputPath = f.output
	badPlan.PlanSHA256 = deltaBlob([]byte("wrong plan")).SHA256
	if _, err := releasebundle.MaterializeDelta(context.Background(), badPlan, f.decode); err == nil || !strings.Contains(err.Error(), "plan digest") {
		t.Fatalf("wrong plan: %v", err)
	}
	assertDeltaOutputAbsent(t, f.output)
}

func TestDeltaInterruptedOrRacingOutputCannotReplaceArtifact(t *testing.T) {
	f := newDeltaFixture(t)
	f.options.OutputPath = f.output
	ctx, cancel := context.WithCancel(context.Background())
	interrupted := func(basis, patch []byte, n int64) (io.ReadCloser, error) {
		reader, err := f.decode(basis, patch, n)
		cancel()
		return reader, err
	}
	if _, err := releasebundle.MaterializeDelta(ctx, f.options, interrupted); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted: %v", err)
	}
	assertDeltaOutputAbsent(t, f.output)
	raceBytes := []byte("concurrent writer\n")
	racing := func(basis, patch []byte, n int64) (io.ReadCloser, error) {
		deltaWrite(t, f.output, raceBytes)
		return f.decode(basis, patch, n)
	}
	if _, err := releasebundle.MaterializeDelta(context.Background(), f.options, racing); !errors.Is(err, os.ErrExist) {
		t.Fatalf("racing output: %v", err)
	}
	if !bytes.Equal(deltaRead(t, f.output), raceBytes) {
		t.Fatal("concurrent output was overwritten")
	}
	partials, err := filepath.Glob(filepath.Join(filepath.Dir(f.output), ".xinsight-delta-*.part"))
	if err != nil || len(partials) != 0 {
		t.Fatalf("unexpected staging files: %v, %v", partials, err)
	}
}

type deltaFixture struct {
	options        releasebundle.DeltaOptions
	plan           releasebundle.DeltaPlan
	output, target string
	decode         releasebundle.DeltaDecoder
}

func newDeltaFixture(t *testing.T) deltaFixture {
	t.Helper()
	root := t.TempDir()
	payload := filepath.Join(root, "payload")
	writeProductPayload(t, payload)
	key, trust := filepath.Join(root, "test-only-key.json"), filepath.Join(root, "test-only-trust.json")
	at := time.Unix(1700000000, 0).UTC()
	if _, err := releasebundle.GenerateKeyPair(key, trust, at); err != nil {
		t.Fatal(err)
	}
	pack := func(version, commit, predecessor, name string) string {
		cfg := filepath.Join(root, name+".json")
		writeReleaseConfigVersion(t, cfg, version, version+"/manual-r1")
		if predecessor != "" {
			var config releasebundle.ReleaseConfig
			if err := json.Unmarshal(deltaRead(t, cfg), &config); err != nil {
				t.Fatal(err)
			}
			config.SupportedUpgradeFromVersions = []string{predecessor}
			deltaWrite(t, cfg, deltaJSON(t, config))
		}
		bundle := filepath.Join(root, name+".tar.gz")
		if _, err := releasebundle.Pack(releasebundle.PackOptions{
			PayloadDir: payload, ConfigPath: cfg, SigningKeyPath: key, OutputPath: bundle, Commit: commit,
			SourceDateEpoch: at, BuildToolchain: releasebundle.BuildToolchain{GoVersion: runtime.Version(), NodeVersion: "v22.23.1", NPMVersion: "10.9.8"},
		}); err != nil {
			t.Fatal(err)
		}
		return bundle
	}
	basisVersion, targetVersion := "0.2.0-rc.1", "0.2.0-rc.3"
	basisCommit, targetCommit := strings.Repeat("1", 40), strings.Repeat("3", 40)
	basis := pack(basisVersion, basisCommit, "", "basis")
	deltaWrite(t, filepath.Join(payload, "fixtures", "test.json"), []byte("{\"new\":true}\n"))
	target := pack(targetVersion, targetCommit, basisVersion, "target")
	basisParts := deltaBundleParts(t, basis)
	targetParts := deltaBundleParts(t, target)
	basisRaw := deltaGunzip(t, basisParts[releasebundle.PayloadArtifactPath])
	targetRaw := deltaGunzip(t, targetParts[releasebundle.PayloadArtifactPath])
	patch := []byte("codec input placeholder for the reconstruction trust tests")
	o := releasebundle.DeltaOptions{
		PlanPath: filepath.Join(root, "delta.json"), BasisPath: basis, PatchPath: filepath.Join(root, "patch.zst"),
		ManifestPath: filepath.Join(root, "target-manifest.json"), SignaturePath: filepath.Join(root, "target-manifest.sig"), TrustStorePath: trust,
	}
	manifest, signature := targetParts["release-manifest.json"], targetParts["release-manifest.sig"]
	deltaWrite(t, o.ManifestPath, manifest)
	deltaWrite(t, o.SignaturePath, signature)
	deltaWrite(t, o.PatchPath, patch)
	plan := releasebundle.DeltaPlan{
		SchemaVersion: releasebundle.DeltaPlanSchema, Codec: releasebundle.DeltaCodec, TargetOS: "linux", TargetArch: "amd64",
		Basis:  releasebundle.DeltaRelease{Commit: basisCommit, Version: basisVersion, Bundle: deltaBlob(deltaRead(t, basis)), RawTar: deltaBlob(basisRaw)},
		Target: releasebundle.DeltaRelease{Commit: targetCommit, Version: targetVersion, Bundle: deltaBlob(deltaRead(t, target)), RawTar: deltaBlob(targetRaw)},
		Patch:  deltaBlob(patch), Manifest: deltaBlob(manifest), Signature: deltaBlob(signature),
	}
	setDeltaPlan(t, &o, plan)
	stage := filepath.Join(root, "staging")
	if err := os.Mkdir(stage, 0o700); err != nil {
		t.Fatal(err)
	}
	return deltaFixture{options: o, plan: plan, output: filepath.Join(stage, "out.tar.gz"), target: target,
		decode: func(gotBasis, gotPatch []byte, n int64) (io.ReadCloser, error) {
			if !bytes.Equal(gotBasis, basisRaw) || !bytes.Equal(gotPatch, patch) {
				t.Fatal("decoder received unverified or different inputs")
			}
			return io.NopCloser(bytes.NewReader(targetRaw)), nil
		}}
}

func assertDeltaOutputAbsent(t *testing.T, output string) {
	t.Helper()
	files, err := os.ReadDir(filepath.Dir(output))
	if err != nil || len(files) != 0 {
		t.Fatalf("staging changed: %v, %v", files, err)
	}
}

func setDeltaPlan(t *testing.T, o *releasebundle.DeltaOptions, p releasebundle.DeltaPlan) {
	t.Helper()
	d := deltaJSON(t, p)
	deltaWrite(t, o.PlanPath, d)
	o.PlanSHA256 = deltaBlob(d).SHA256
}

func deltaBundleParts(t *testing.T, path string) map[string][]byte {
	t.Helper()
	tarBytes := deltaGunzip(t, deltaRead(t, path))
	r := tar.NewReader(bytes.NewReader(tarBytes))
	parts := make(map[string][]byte)
	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			return parts
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			d, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			parts[filepath.Base(h.Name)] = d
		}
	}
}

func deltaGunzip(t *testing.T, d []byte) []byte {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(d))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func deltaBlob(d []byte) releasebundle.DeltaBlob {
	return releasebundle.DeltaBlob{Bytes: int64(len(d)), SHA256: fmt.Sprintf("sha256:%x", sha256.Sum256(d))}
}

func deltaJSON(t *testing.T, v any) []byte {
	t.Helper()
	d, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func deltaRead(t *testing.T, path string) []byte {
	t.Helper()
	d, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func deltaWrite(t *testing.T, path string, d []byte) {
	t.Helper()
	if err := os.WriteFile(path, d, 0o600); err != nil {
		t.Fatal(err)
	}
}
