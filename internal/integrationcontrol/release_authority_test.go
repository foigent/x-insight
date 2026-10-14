package integrationcontrol

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/releasebundle"
)

func TestReleaseAuthorityIsDerivedFromExactVerifiedSignedManifest(t *testing.T) {
	verified := verifiedCurrentReleaseBundle(t)
	authority, err := NewReleaseAuthorityFromVerifiedBundle(verified)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.Validate(); err != nil {
		t.Fatal(err)
	}
	if authority.ProductRelease.ProductVersion != "0.3.0-rc.13" ||
		authority.ProductRelease.DatabaseSchema != "23" ||
		authority.ProductRelease.ManualVersion != "0.3.0-rc.13/manual-r13" ||
		authority.ProductRelease.SignedReleaseManifestSHA256 != verified.ManifestSHA256 {
		t.Fatalf("release authority=%+v", authority.ProductRelease)
	}

	// Public VerifiedBundle fields are diagnostics, not construction authority.
	// Derivation must retain the exact manifest bytes sealed by Verify.
	publicFieldTamper := verified
	publicFieldTamper.Manifest.ProductVersion = "9.9.9-tampered"
	publicFieldTamper.Manifest.Files = nil
	sealed, err := NewReleaseAuthorityFromVerifiedBundle(publicFieldTamper)
	if err != nil {
		t.Fatal(err)
	}
	sealedBytes, _ := sealed.ProductRelease.CanonicalBytes()
	authorityBytes, _ := authority.ProductRelease.CanonicalBytes()
	if string(sealedBytes) != string(authorityBytes) {
		t.Fatal("public manifest diagnostics changed sealed release authority")
	}

	productBytes, err := authority.ProductRelease.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	parsedProduct, err := ParseProductReleaseIdentity(productBytes)
	if err != nil {
		t.Fatal(err)
	}
	verifierBytes, err := authority.Verifier.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	parsedVerifier, err := ParseVerifierRelease(verifierBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := (ReleaseAuthority{ProductRelease: parsedProduct, Verifier: parsedVerifier}).Validate(); err != nil {
		t.Fatal(err)
	}

	for name, mutate := range map[string]func(*ReleaseAuthority){
		"signed manifest": func(value *ReleaseAuthority) {
			value.ProductRelease.SignedReleaseManifestSHA256 = bytesSHA256([]byte("other-manifest"))
		},
		"product executable": func(value *ReleaseAuthority) {
			value.ProductRelease.ImporterExecutableSHA256 = bytesSHA256([]byte("other-importer"))
		},
		"product schema": func(value *ReleaseAuthority) {
			value.ProductRelease.DatabaseSchema = "19"
		},
		"product manual": func(value *ReleaseAuthority) {
			value.ProductRelease.ManualVersion = "0.3.0-rc.13/manual-r12"
		},
		"verifier product": func(value *ReleaseAuthority) {
			value.Verifier.ProductReleaseSHA256 = bytesSHA256([]byte("other-product-release"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			drifted := authority
			mutate(&drifted)
			if err := drifted.Validate(); err == nil {
				t.Fatal("drifted release authority was accepted")
			}
		})
	}
}

func TestReleaseAuthorityRejectsUnverifiedConstruction(t *testing.T) {
	if _, err := NewReleaseAuthorityFromVerifiedBundle(releasebundle.VerifiedBundle{}); err == nil {
		t.Fatal("unverified release bundle was accepted as product authority")
	}
}

func verifiedCurrentReleaseBundle(t *testing.T) releasebundle.VerifiedBundle {
	t.Helper()
	return verifiedCampaignReleaseBundle(t, false, nil)
}

// Keys and payload binaries here are disposable test fixtures, never releases.
func verifiedCampaignReleaseBundle(t *testing.T, development bool, mutate func(*releasebundle.ReleaseConfig, string)) releasebundle.VerifiedBundle {
	t.Helper()
	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	root := t.TempDir()
	payload := filepath.Join(root, "payload")
	for _, name := range []string{"xinsight-server", "xinsight-importer", "xinsightctl", "xinsight-launcher"} {
		writeAuthorityTestFile(t, filepath.Join(payload, "bin", name), []byte("exact "+name+"\n"), 0o755)
	}
	// Keep the historical migration prefix separate from the development set.
	entries, err := os.ReadDir(filepath.Join(repositoryRoot, "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && (development || entry.Name() < "000024_") {
			copyAuthorityTestFile(t, filepath.Join(repositoryRoot, "migrations", entry.Name()), filepath.Join(payload, "migrations", entry.Name()), 0644)
		}
	}
	for _, directory := range []string{"schemas", "fixtures"} {
		copyAuthorityTestTree(t, filepath.Join(repositoryRoot, directory), filepath.Join(payload, directory))
	}
	for _, source := range []string{
		"internal/webui/manual/zh-CN/index.html",
		"internal/webui/manual/zh-CN/manual.css",
		"internal/webui/manual/zh-CN/manual-icon.svg",
		"release/compatibility-matrix.json",
		"release/release-notes.md",
		"internal/hostctl/clean_baseline_schema_contract.json",
		"deploy/xinsight.service",
		"deploy/xinsight-connector-controller.service",
		"deploy/xinsight-feed.service",
		"deploy/xinsight-litellm-feed.service",
		"deploy/99-xinsight-managed-launcher.conf",
		"deploy/xinsight.env.example",
		"deploy/xinsight-connector.env.example",
		"deploy/trusted-tls-proxy.env.example",
		"deploy/xinsight-nginx-ip.conf.example",
		"deploy/xinsight-feed.env.example",
		"deploy/xinsight-litellm-feed.env.example",
		"deploy/postgres.compose.yml",
		"deploy/postgres.env.example",
	} {
		destination := filepath.Join(payload, source)
		if source == "internal/webui/manual/zh-CN/index.html" ||
			source == "internal/webui/manual/zh-CN/manual.css" ||
			source == "internal/webui/manual/zh-CN/manual-icon.svg" {
			destination = filepath.Join(payload, "manual", "zh-CN", filepath.Base(source))
		} else if source == "internal/hostctl/clean_baseline_schema_contract.json" {
			destination = filepath.Join(payload, "release", "schema-contract.json")
		}
		if development {
			switch source {
			case "release/compatibility-matrix.json":
				source = "release/d-validation-compatibility-matrix.json"
			case "release/release-notes.md":
				source = "release/d-validation-notes.md"
			case "internal/hostctl/clean_baseline_schema_contract.json":
				source = "internal/hostctl/development_schema_contract.json"
			}
		}
		copyAuthorityTestFile(t, filepath.Join(repositoryRoot, source), destination, 0o644)
	}
	if development {
		copyAuthorityTestTree(t, filepath.Join(repositoryRoot, "internal/webui/manual/en-US"), filepath.Join(payload, "manual/en-US"))
		copyAuthorityTestTree(t, filepath.Join(repositoryRoot, "licenses"), filepath.Join(payload, "licenses"))
		for _, name := range []string{"LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md"} {
			copyAuthorityTestFile(t, filepath.Join(repositoryRoot, name), filepath.Join(payload, name), 0o644)
		}
	}
	configName := "release/release-config.json"
	if development {
		configName = "release/d-validation-config.json"
		for _, source := range []string{"deploy/xinsight-source-facts.service", "deploy/source-facts.json.example"} {
			copyAuthorityTestFile(t, filepath.Join(repositoryRoot, source), filepath.Join(payload, source), 0o644)
		}
	}
	configBytes, err := os.ReadFile(filepath.Join(repositoryRoot, configName))
	if err != nil {
		t.Fatal(err)
	}
	var config releasebundle.ReleaseConfig
	if err := json.Unmarshal(configBytes, &config); err != nil {
		t.Fatal(err)
	}
	if development {
		// The checked-in D validation inputs are frozen at schema 40. Build a
		// synthetic current candidate only in this test directory; the source
		// release sample is never rewritten or treated as schema 41 authority.
		contractBytes, err := os.ReadFile(filepath.Join(repositoryRoot, "internal/hostctl/development_schema_contract.json"))
		if err != nil {
			t.Fatal(err)
		}
		var contract releasebundle.SchemaContractIdentity
		if err := json.Unmarshal(contractBytes, &contract); err != nil {
			t.Fatal(err)
		}
		config.DatabaseSchema = releasebundle.SchemaCompatibility{Current: buildinfo.DatabaseSchemaMax, Min: buildinfo.DatabaseSchemaMin, Max: buildinfo.DatabaseSchemaMax}
		config.ApplicationCompatibility.DatabaseSchemaMin = buildinfo.DatabaseSchemaMin
		config.ApplicationCompatibility.DatabaseSchemaMax = buildinfo.DatabaseSchemaMax
		config.SchemaContract = contract
		matrixPath := filepath.Join(payload, "release/compatibility-matrix.json")
		matrixBytes, err := os.ReadFile(matrixPath)
		if err != nil {
			t.Fatal(err)
		}
		var matrix map[string]any
		if err := json.Unmarshal(matrixBytes, &matrix); err != nil {
			t.Fatal(err)
		}
		matrix["database_schema"] = buildinfo.DatabaseSchemaMax
		matrix["schema_contract"] = contract
		matrixBytes, err = json.Marshal(matrix)
		if err != nil {
			t.Fatal(err)
		}
		writeAuthorityTestFile(t, matrixPath, matrixBytes, 0644)
	}
	if mutate != nil {
		mutate(&config, payload)
	}
	configBytes, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "release-config.json")
	writeAuthorityTestFile(t, configPath, configBytes, 0o600)

	privateKey := filepath.Join(root, "release-key.json")
	trustStore := filepath.Join(root, "release-trust.json")
	createdAt := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	if _, err := releasebundle.GenerateKeyPair(privateKey, trustStore, createdAt.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(root, "release.tar.gz")
	commit := strings.Repeat("1", 40)
	if _, err := releasebundle.Pack(releasebundle.PackOptions{
		PayloadDir:      payload,
		ConfigPath:      configPath,
		SigningKeyPath:  privateKey,
		OutputPath:      artifact,
		Commit:          commit,
		SourceDateEpoch: createdAt,
		BuildToolchain: releasebundle.BuildToolchain{
			GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8",
		},
	}); err != nil {
		t.Fatal(err)
	}
	verified, err := releasebundle.Verify(artifact, releasebundle.VerifyOptions{
		TrustStorePath: trustStore, ExpectedCommit: commit, ExpectedVersion: config.ProductVersion,
		ExpectedOS: "linux", ExpectedArch: "amd64", Now: createdAt.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	return verified
}

func copyAuthorityTestTree(t *testing.T, sourceRoot, destinationRoot string) {
	t.Helper()
	if err := filepath.WalkDir(sourceRoot, func(source string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(sourceRoot, source)
		if err != nil {
			return err
		}
		destination := filepath.Join(destinationRoot, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		contents, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		return os.WriteFile(destination, contents, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
}

func copyAuthorityTestFile(t *testing.T, source, destination string, mode os.FileMode) {
	t.Helper()
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	writeAuthorityTestFile(t, destination, contents, mode)
}

func writeAuthorityTestFile(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatal(err)
	}
}

func TestHistoricalProductSchemaStructurePreservesAuthorityBinding(t *testing.T) {
	authority, err := NewReleaseAuthorityFromVerifiedBundle(verifiedCurrentReleaseBundle(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range []string{"20", "23", "19", "21", "22", "24", "39", "", "020"} {
		t.Run("schema_"+schema, func(t *testing.T) {
			product := authority.ProductRelease
			product.DatabaseSchema = schema
			payload, err := product.CanonicalBytes()
			supported := schema == "20" || schema == "23" || schema == buildinfo.DatabaseSchemaMin
			if !supported {
				if err == nil {
					t.Fatal("unsupported schema accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := ParseProductReleaseIdentity(payload)
			if err != nil || parsed != product {
				t.Fatalf("historical round trip: %v", err)
			}
			if schema != "23" {
				mixed := authority
				mixed.ProductRelease = parsed
				if err := mixed.Validate(); err == nil {
					t.Fatalf("schema%s borrowed schema23 verifier authority", schema)
				}
			}
		})
	}
}

func TestCompiledCampaignReleaseAuthority(t *testing.T) {
	currentBytes, err := os.ReadFile("../../release/d-validation-config.json")
	if err != nil {
		t.Fatal(err)
	}
	var current releasebundle.ReleaseConfig
	if err := json.Unmarshal(currentBytes, &current); err != nil {
		t.Fatal(err)
	}
	if current.DatabaseSchema.Current != buildinfo.DatabaseSchemaMax || buildinfo.DatabaseSchemaMin != buildinfo.DatabaseSchemaMax {
		t.Fatalf("current D config and compiled database generations diverge: config=%s compiled=%s..%s", current.DatabaseSchema.Current, buildinfo.DatabaseSchemaMin, buildinfo.DatabaseSchemaMax)
	}
	verified := verifiedCampaignReleaseBundle(t, true, nil)
	authority, err := NewReleaseAuthorityFromVerifiedBundle(verified)
	if err != nil {
		t.Fatal(err)
	}
	if err := authority.Validate(); err != nil {
		t.Fatal(err)
	}
	if authority.ProductRelease.DatabaseSchema != buildinfo.DatabaseSchemaMin || buildinfo.DatabaseSchemaMin != buildinfo.DatabaseSchemaMax ||
		authority.ProductRelease.ProductVersion != verified.Manifest.ProductVersion || authority.ProductRelease.ManualVersion != verified.Manifest.Manual.Version {
		t.Fatalf("compiled and signed development identities diverge: %+v", authority.ProductRelease)
	}
	// The payload contract was checked by Pack and Verify. Check its config
	// against the compiled generation too, rather than trusting an input label.
	publicTamper := verified
	publicTamper.Manifest.DatabaseSchema.Current = "23"
	sealed, err := NewReleaseAuthorityFromVerifiedBundle(publicTamper)
	if err != nil || sealed != authority {
		t.Fatalf("sealed authority changed: %v", err)
	}
	for name, mutate := range map[string]func(*ReleaseAuthority){
		"schema23 product": func(v *ReleaseAuthority) { v.ProductRelease.DatabaseSchema = "23" },
		"schema20 product": func(v *ReleaseAuthority) { v.ProductRelease.DatabaseSchema = "20" },
		"manifest":         func(v *ReleaseAuthority) { v.ProductRelease.SignedReleaseManifestSHA256 = bytesSHA256([]byte("other")) },
		"importer":         func(v *ReleaseAuthority) { v.ProductRelease.ImporterExecutableSHA256 = bytesSHA256([]byte("other")) },
		"importer size":    func(v *ReleaseAuthority) { v.ProductRelease.ImporterExecutableSizeBytes++ },
		"component":        func(v *ReleaseAuthority) { v.ProductRelease.ComponentContractSHA256 = bytesSHA256([]byte("other")) },
		"M0":               func(v *ReleaseAuthority) { v.ProductRelease.M0EvidenceSchemaSHA256 = bytesSHA256([]byte("other")) },
		"N1": func(v *ReleaseAuthority) {
			v.ProductRelease.NeutralN1ReleaseManifestSHA256 = bytesSHA256([]byte("other"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			drift := authority
			mutate(&drift)
			if drift.Validate() == nil {
				t.Fatal("tampered authority accepted")
			}
		})
	}
}

func TestCampaignSchemaGenerationIsCompiledAndExact(t *testing.T) {
	compiled, err := strconv.Atoi(buildinfo.DatabaseSchemaMax)
	if err != nil {
		t.Fatal(err)
	}
	future := strconv.Itoa(compiled + 1)
	for _, tc := range []struct {
		current, min, max string
		want              bool
	}{
		{"23", "23", "23", true}, {buildinfo.DatabaseSchemaMin, buildinfo.DatabaseSchemaMin, buildinfo.DatabaseSchemaMax, true},
		{"20", "20", "20", false}, {"24", "24", "24", false}, {"38", "38", "38", false}, {future, future, future, false},
		{"39", "23", "39", false}, {"39", "39", "40", false}, {"23", "23", "39", false}, {"039", "039", "039", false}, {"", "", "", false},
	} {
		t.Run(tc.current+"_"+tc.min+"_"+tc.max, func(t *testing.T) {
			if got := campaignDatabaseSchema(tc.current, tc.min, tc.max); got != tc.want {
				t.Fatalf("allowed=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestCompiledCampaignFactoryRejectsSignedRangeAndEvidenceDrift(t *testing.T) {
	compiled, err := strconv.Atoi(buildinfo.DatabaseSchemaMax)
	if err != nil {
		t.Fatal(err)
	}
	future := strconv.Itoa(compiled + 1)
	for name, mutate := range map[string]func(*releasebundle.ReleaseConfig, string){
		"historical minimum": func(c *releasebundle.ReleaseConfig, _ string) { c.DatabaseSchema.Min = "23" },
		"future maximum":     func(c *releasebundle.ReleaseConfig, _ string) { c.DatabaseSchema.Max = future },
		"M0 substitute": func(_ *releasebundle.ReleaseConfig, payload string) {
			writeAuthorityTestFile(t, filepath.Join(payload, "schemas/m0/evidence-event.schema.json"), []byte(`{"type":"object"}`), 0o644)
		},
	} {
		t.Run(name, func(t *testing.T) {
			verified := verifiedCampaignReleaseBundle(t, true, mutate)
			if _, err := NewReleaseAuthorityFromVerifiedBundle(verified); err == nil || !strings.Contains(err.Error(), "does not authorize") {
				t.Fatalf("signed but unauthorized input: %v", err)
			}
		})
	}
}
