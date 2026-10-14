package main

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/evidence"
	"github.com/foigent/x-insight/internal/integration/uniroutercampaign"
	"github.com/foigent/x-insight/internal/integrationcontrol"
	"github.com/foigent/x-insight/internal/releasebundle"
)

var errOfflineCampaignAuthority = errors.New("offline campaign authority rejected")

func TestCampaignPackageCommandCompletesOfflineAuthorityBeforeLoadingDatabase(t *testing.T) {
	calls := make([]string, 0, 3)
	dependencies := campaignPackageCommandDependencies{
		prepare: func(
			_ context.Context,
			input campaignPackageCommandInput,
		) (uniroutercampaign.PreparedImport, error) {
			calls = append(calls, "prepare")
			if input.OperationID != "campaign-operation-001" ||
				input.PackagePath != "/offline/package.tgz" {
				t.Fatalf("input=%+v", input)
			}
			return uniroutercampaign.PreparedImport{}, errOfflineCampaignAuthority
		},
		loadImporterDSN: func() (string, error) {
			calls = append(calls, "load-dsn")
			return "", errors.New("database must not be reached")
		},
		persist: func(
			context.Context,
			string,
			uniroutercampaign.PreparedImport,
		) ([]byte, error) {
			calls = append(calls, "persist")
			return nil, errors.New("database must not be reached")
		},
	}
	err := runUniRouterImportCampaignPackageContext(
		context.Background(),
		campaignPackageCommandArgs(),
		bytes.NewBuffer(nil),
		dependencies,
	)
	if !errors.Is(err, errOfflineCampaignAuthority) {
		t.Fatalf("error=%v", err)
	}
	if !slices.Equal(calls, []string{"prepare"}) {
		t.Fatalf("calls=%v", calls)
	}
}

func TestCampaignPackageCommandPersistsPreparedImportAndPrintsTypedReceipt(t *testing.T) {
	calls := make([]string, 0, 3)
	canonical := []byte(`{"schema_version":"xinsight.unirouter-campaign-package-import-receipt.v1","package_scope_complete":true}`)
	dependencies := campaignPackageCommandDependencies{
		prepare: func(
			_ context.Context,
			_ campaignPackageCommandInput,
		) (uniroutercampaign.PreparedImport, error) {
			calls = append(calls, "prepare")
			return uniroutercampaign.PreparedImport{}, nil
		},
		loadImporterDSN: func() (string, error) {
			calls = append(calls, "load-dsn")
			return "opaque-importer-database-binding", nil
		},
		persist: func(
			_ context.Context,
			dsn string,
			_ uniroutercampaign.PreparedImport,
		) ([]byte, error) {
			calls = append(calls, "persist")
			if dsn != "opaque-importer-database-binding" {
				t.Fatalf("database binding=%q", dsn)
			}
			return canonical, nil
		},
	}
	var output bytes.Buffer
	if err := runUniRouterImportCampaignPackageContext(
		context.Background(),
		campaignPackageCommandArgs(),
		&output,
		dependencies,
	); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(calls, []string{"prepare", "load-dsn", "persist"}) {
		t.Fatalf("calls=%v", calls)
	}
	if !bytes.Equal(output.Bytes(), append(canonical, '\n')) {
		t.Fatalf("output=%s", output.Bytes())
	}
}

func TestCampaignPackageCommandRejectsIdentityAndTransportInputs(t *testing.T) {
	for _, extra := range [][]string{
		{"--source-instance", "operator-supplied-source"},
		{"--cursor", "operator-supplied-cursor"},
		{"--endpoint", "https://example.invalid"},
		{"--worker", "xinsight-feed.service"},
	} {
		args := append(campaignPackageCommandArgs(), extra...)
		if _, err := parseCampaignPackageCommandInput(args); err == nil {
			t.Fatalf("unsupported operator authority %v was accepted", extra)
		}
	}
	args := campaignPackageCommandArgs()
	args[3] = "../relative-package.tgz"
	if _, err := parseCampaignPackageCommandInput(args); err == nil ||
		!strings.Contains(err.Error(), "absolute clean path") {
		t.Fatalf("relative path error=%v", err)
	}
}

func TestRealCampaignLifecycleRejectsSyntheticProductReleaseIdentity(t *testing.T) {
	scopeBytes, err := os.ReadFile(filepath.Join(
		"..", "..", "internal", "integrationcontrol", "testdata", "component-scope-receipt.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	scopeBytes = bytes.TrimSuffix(scopeBytes, []byte("\n"))
	err = validateRealCampaignProductLifecycle(
		integrationcontrol.N1ManifestVerification{},
		scopeBytes,
		nil,
		nil,
		nil,
		nil,
		integrationcontrol.ReleaseAuthority{},
		campaignRuntimeIdentity{},
	)
	if err == nil || !strings.Contains(err.Error(), "authoritative X-Insight product release") {
		t.Fatalf("synthetic product authority error=%v", err)
	}
}

func TestRealCampaignLifecycleClosesSignedManifestProductAndRunningExecutable(t *testing.T) {
	for _, schema := range []string{integrationcontrol.HistoricalCampaignDatabaseSchema, buildinfo.DatabaseSchemaMin} {
		t.Run("schema"+schema, func(t *testing.T) { testRealCampaignLifecycleForSchema(t, schema) })
	}
}

func testRealCampaignLifecycleForSchema(t *testing.T, schema string) {
	authority, runtimeIdentity := realCampaignReleaseAuthorityForSchemaTest(t, schema)
	contractSHA256, err := integrationcontrol.NewComponentContract().SHA256()
	if err != nil {
		t.Fatal(err)
	}
	productBytes, err := authority.ProductRelease.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	verifierBytes, err := authority.Verifier.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	verifierSHA256, err := authority.Verifier.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	productSHA256, err := authority.ProductRelease.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	scope, err := integrationcontrol.NewComponentScopeReceipt(integrationcontrol.ComponentScopeInput{
		CampaignID:              "fixture-campaign-20260811",
		ComponentContractSHA256: contractSHA256,
		ProductRelease: integrationcontrol.ArtifactReference{
			SchemaVersion: integrationcontrol.ProductReleaseIdentityVersion,
			SHA256:        productSHA256,
		},
		VerifierReleaseSHA256: verifierSHA256,
		IssuedAt:              time.Date(2029, 12, 31, 22, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	scopeBytes, err := scope.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}

	n1Verifier, manifestPath := realCampaignN1ManifestForScope(t, scope)
	manifest, err := n1Verifier.VerifyManifest(context.Background(), manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := integrationcontrol.NewComponentVerificationLifecycle(
		scope,
		manifest,
		time.Date(2029, 12, 31, 23, 31, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	verification, err := lifecycle.VerificationReceipt()
	if err != nil {
		t.Fatal(err)
	}
	verificationBytes, err := verification.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	acknowledgement, err := integrationcontrol.NewComponentAcknowledgement(lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	acknowledgementBytes, err := acknowledgement.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}

	validate := func(
		product []byte,
		expected integrationcontrol.ReleaseAuthority,
		running campaignRuntimeIdentity,
	) error {
		return validateRealCampaignProductLifecycle(
			manifest,
			scopeBytes,
			verificationBytes,
			acknowledgementBytes,
			verifierBytes,
			product,
			expected,
			running,
		)
	}
	if err := validate(productBytes, authority, runtimeIdentity); err != nil {
		t.Fatalf("exact signed release lifecycle: %v", err)
	}

	t.Run("running executable drift", func(t *testing.T) {
		drifted := runtimeIdentity
		drifted.ImporterExecutableSHA256 = "sha256:" + strings.Repeat("d", 64)
		if err := validate(productBytes, authority, drifted); err == nil ||
			!strings.Contains(err.Error(), "running importer executable") {
			t.Fatalf("error=%v", err)
		}
	})
	t.Run("product receipt drift", func(t *testing.T) {
		drifted := authority.ProductRelease
		drifted.SignedReleaseManifestSHA256 = "sha256:" + strings.Repeat("e", 64)
		driftedBytes, err := drifted.CanonicalBytes()
		if err != nil {
			t.Fatal(err)
		}
		if err := validate(driftedBytes, authority, runtimeIdentity); err == nil {
			t.Fatal("product receipt drift was accepted")
		}
	})
	t.Run("signed manifest drift", func(t *testing.T) {
		drifted := authorityWithSignedManifestForTest(t, authority, "sha256:"+strings.Repeat("f", 64))
		if err := validate(productBytes, drifted, runtimeIdentity); err == nil ||
			!strings.Contains(err.Error(), "verified signed release manifest") {
			t.Fatalf("error=%v", err)
		}
	})
	for name, mutate := range map[string]func(*campaignRuntimeIdentity){
		"runtime version":        func(v *campaignRuntimeIdentity) { v.ProductVersion += "-other" },
		"runtime commit":         func(v *campaignRuntimeIdentity) { v.Commit = strings.Repeat("2", 40) },
		"runtime manual":         func(v *campaignRuntimeIdentity) { v.ManualVersion += "1" },
		"runtime schema minimum": func(v *campaignRuntimeIdentity) { v.DatabaseSchemaMin = "20" },
		"runtime schema maximum": func(v *campaignRuntimeIdentity) {
			maximum, err := strconv.Atoi(v.DatabaseSchemaMax)
			if err != nil {
				t.Fatal(err)
			}
			v.DatabaseSchemaMax = strconv.Itoa(maximum + 1)
		},
		"runtime other generation": func(v *campaignRuntimeIdentity) {
			v.DatabaseSchemaMin, v.DatabaseSchemaMax = buildinfo.DatabaseSchemaMin, buildinfo.DatabaseSchemaMax
			if schema == buildinfo.DatabaseSchemaMin {
				v.DatabaseSchemaMin, v.DatabaseSchemaMax = "23", "23"
			}
		},
		"runtime executable size": func(v *campaignRuntimeIdentity) { v.ImporterExecutableSizeBytes++ },
	} {
		t.Run(name+" before database", func(t *testing.T) {
			drift := runtimeIdentity
			mutate(&drift)
			assertCampaignValidationFailsBeforeDatabase(t, func() error { return validate(productBytes, authority, drift) })
		})
	}
	for name, mutate := range map[string]func(){
		"scope product": func() {
			scopeBytes = bytes.Replace(scopeBytes, []byte(productSHA256), []byte("sha256:"+strings.Repeat("d", 64)), 1)
		},
		"scope verifier": func() {
			scopeBytes = bytes.Replace(scopeBytes, []byte(verifierSHA256), []byte("sha256:"+strings.Repeat("d", 64)), 1)
		},
		"scope component": func() {
			scopeBytes = bytes.Replace(scopeBytes, []byte(contractSHA256), []byte("sha256:"+strings.Repeat("d", 64)), 1)
		},
		"other generation verifier": func() {
			otherSchema := buildinfo.DatabaseSchemaMin
			if schema == otherSchema {
				otherSchema = "23"
			}
			other, _ := realCampaignReleaseAuthorityForSchemaTest(t, otherSchema)
			var err error
			verifierBytes, err = other.Verifier.CanonicalBytes()
			if err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name+" before database", func(t *testing.T) {
			originalScope, originalVerifier := scopeBytes, verifierBytes
			defer func() { scopeBytes, verifierBytes = originalScope, originalVerifier }()
			mutate()
			assertCampaignValidationFailsBeforeDatabase(t, func() error { return validate(productBytes, authority, runtimeIdentity) })
		})
	}

}

func TestCampaignEvidenceSchemaPinRejectsPermissiveSubstituteBeforeDatabase(t *testing.T) {
	exactPath, err := filepath.Abs(filepath.Join(
		"..", "..", "schemas", "m0", "evidence-event.schema.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyCampaignEvidenceSchema(exactPath); err != nil {
		t.Fatalf("exact product schema: %v", err)
	}

	permissivePath := filepath.Join(t.TempDir(), "permissive.schema.json")
	permissive := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"}`)
	if err := os.WriteFile(permissivePath, permissive, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := evidence.NewSchemaValidator(permissivePath); err != nil {
		t.Fatalf("permissive substitute is not a valid JSON Schema: %v", err)
	}
	if err := verifyCampaignEvidenceSchema(permissivePath); err == nil ||
		!strings.Contains(err.Error(), "compiled X-Insight product pin") {
		t.Fatalf("permissive schema pin error=%v", err)
	}

	args := campaignPackageCommandArgs()
	args[len(args)-1] = permissivePath
	databaseLoads := 0
	err = runUniRouterImportCampaignPackageContext(
		context.Background(),
		args,
		bytes.NewBuffer(nil),
		campaignPackageCommandDependencies{
			prepare: prepareUniRouterCampaignPackageImport,
			loadImporterDSN: func() (string, error) {
				databaseLoads++
				return "", errors.New("database must not be reached")
			},
			persist: func(
				context.Context,
				string,
				uniroutercampaign.PreparedImport,
			) ([]byte, error) {
				return nil, errors.New("database must not be reached")
			},
		},
	)
	if err == nil || !strings.Contains(err.Error(), "compiled X-Insight product pin") || databaseLoads != 0 {
		t.Fatalf("substitute error=%v database_loads=%d", err, databaseLoads)
	}
}

func campaignPackageCommandArgs() []string {
	return []string{
		"--operation-id", "campaign-operation-001",
		"--package", "/offline/package.tgz",
		"--n1-release-manifest", "/offline/n1-release.json",
		"--n1-verifier", "/offline/n1-verifier",
		"--campaign-manifest", "/offline/campaign-manifest.json",
		"--unirouter-ack", "/offline/unirouter-ack.json",
		"--xinsight-ack", "/offline/xinsight-ack.json",
		"--authorization", "/offline/authorization.json",
		"--authorization-signature", "/offline/authorization.sig",
		"--authorization-trust-set", "/offline/authorization-trust.json",
		"--source-evidence-trust-set", "/offline/source-evidence-trust.json",
		"--component-scope-receipt", "/offline/component-scope.json",
		"--component-verification-receipt", "/offline/component-verification.json",
		"--verifier-release", "/offline/verifier-release.json",
		"--product-release", "/offline/product-release.json",
		"--release-artifact", "/offline/xinsight-signed-release.tar.gz",
		"--release-trust-store", "/offline/xinsight-release-trust.json",
		"--evidence-schema", "/offline/evidence-event.schema.json",
	}
}

func realCampaignReleaseAuthorityForSchemaTest(t *testing.T, schema string) (
	integrationcontrol.ReleaseAuthority,
	campaignRuntimeIdentity,
) {
	t.Helper()
	importerSHA256, importerSize, err := currentImporterExecutableIdentity()
	if err != nil {
		t.Fatal(err)
	}
	contractSHA256, err := integrationcontrol.NewComponentContract().SHA256()
	if err != nil {
		t.Fatal(err)
	}
	product := integrationcontrol.ProductReleaseIdentity{
		SchemaVersion:                  integrationcontrol.ProductReleaseIdentityVersion,
		Product:                        integrationcontrol.ProductXInsight,
		ProductVersion:                 "0.3.0-rc.11",
		Commit:                         strings.Repeat("1", 40),
		SignedReleaseManifestSHA256:    "sha256:" + strings.Repeat("a", 64),
		ReleaseSigningKeyID:            "ed25519:sha256:" + strings.Repeat("b", 64),
		TargetOS:                       "linux",
		TargetArch:                     "amd64",
		DatabaseSchema:                 schema,
		ManualVersion:                  "0.3.0-rc.11/manual-r11",
		ImporterExecutableSHA256:       importerSHA256,
		ImporterExecutableSizeBytes:    importerSize,
		M0EvidenceSchemaSHA256:         integrationcontrol.M0EvidenceSchemaSHA256,
		ComponentContractSHA256:        contractSHA256,
		NeutralN1ReleaseManifestSHA256: integrationcontrol.FrozenN1ReleaseManifestSHA256,
	}
	if schema == buildinfo.DatabaseSchemaMin {
		product.ProductVersion = "0.2.0-dev.fpv7"
		product.ManualVersion = "0.2.0-dev.fpv7/manual-r1"
	}
	productSHA256, err := product.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := integrationcontrol.NewVerifierRelease(integrationcontrol.VerifierReleaseInput{
		Commit: product.Commit, Version: product.ProductVersion,
		ComponentContractSHA256: contractSHA256, ProductReleaseSHA256: productSHA256,
		SignedReleaseManifestSHA256: product.SignedReleaseManifestSHA256,
		ImporterExecutableSHA256:    importerSHA256,
		M0EvidenceSchemaSHA256:      integrationcontrol.M0EvidenceSchemaSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	authority := integrationcontrol.ReleaseAuthority{ProductRelease: product, Verifier: verifier}
	if err := authority.Validate(); err != nil {
		t.Fatal(err)
	}
	return authority, campaignRuntimeIdentity{
		ProductVersion: product.ProductVersion, Commit: product.Commit,
		ManualVersion:     product.ManualVersion,
		DatabaseSchemaMin: product.DatabaseSchema, DatabaseSchemaMax: product.DatabaseSchema,
		ImporterExecutableSHA256: importerSHA256, ImporterExecutableSizeBytes: importerSize,
	}
}

func authorityWithSignedManifestForTest(
	t *testing.T,
	authority integrationcontrol.ReleaseAuthority,
	manifestSHA256 string,
) integrationcontrol.ReleaseAuthority {
	t.Helper()
	authority.ProductRelease.SignedReleaseManifestSHA256 = manifestSHA256
	productSHA256, err := authority.ProductRelease.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	authority.Verifier.SignedReleaseManifestSHA256 = manifestSHA256
	authority.Verifier.ProductReleaseSHA256 = productSHA256
	if err := authority.Validate(); err != nil {
		t.Fatal(err)
	}
	return authority
}

func realCampaignN1ManifestForScope(
	t *testing.T,
	scope integrationcontrol.ComponentScopeReceipt,
) (*integrationcontrol.N1Verifier, string) {
	t.Helper()
	assetDirectory := os.Getenv("XINSIGHT_N1_RELEASE_ASSET_DIRECTORY")
	if assetDirectory == "" {
		t.Skip("XINSIGHT_N1_RELEASE_ASSET_DIRECTORY is required for immutable N1 compatibility")
	}
	manifestPath := filepath.Join(assetDirectory, "foigent.integration-control.release.v1.json")
	binaryPath := filepath.Join(
		assetDirectory,
		fmt.Sprintf("foigent-integration-control-%s-%s-%s", integrationcontrol.FrozenN1Version, runtime.GOOS, runtime.GOARCH),
	)
	sourceArchive := filepath.Join(
		assetDirectory,
		"foigent-integration-control-"+integrationcontrol.FrozenN1Version+"-source.tar",
	)
	verifier, err := integrationcontrol.NewN1Verifier(manifestPath, binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.VerifySourceArchive(sourceArchive); err != nil {
		t.Fatal(err)
	}
	fixture := extractCampaignN1Fixture(t, sourceArchive, "fixtures/v1/valid/manifest.json")
	manifestBytes, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	scopeSHA256, err := scope.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	replacements := [][2]string{
		{"sha256:6f8a1cf45567781783c3eae587bf52d46b38d3a977a074ccbf087a449080adad", scope.ComponentContract.SHA256},
		{"sha256:6247d648d7546fc8418c30db74611376e427520d30dcc3ca08ddf4479ee0cda6", scope.ProductRelease.SHA256},
		{"xinsight.source-evidence-import-scope-receipt.v1", integrationcontrol.ComponentScopeVersion},
		{"sha256:89f5beab36e73ed42893b7ae1a71bee0b742546c1a917d2f779abffa3b6f2ba2", scopeSHA256},
	}
	for _, replacement := range replacements {
		if !bytes.Contains(manifestBytes, []byte(replacement[0])) {
			t.Fatalf("N1 compatibility manifest lacks fixture value %q", replacement[0])
		}
		manifestBytes = bytes.Replace(manifestBytes, []byte(replacement[0]), []byte(replacement[1]), 1)
	}
	modified := filepath.Join(t.TempDir(), "campaign-manifest.json")
	if err := os.WriteFile(modified, manifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return verifier, modified
}

func extractCampaignN1Fixture(t *testing.T, archivePath, name string) string {
	t.Helper()
	file, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader := tar.NewReader(file)
	wanted := "foigent-integration-control-" + integrationcontrol.FrozenN1Version + "/" + name
	for {
		header, err := reader.Next()
		if err == io.EOF {
			t.Fatalf("N1 source archive is missing %s", name)
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name != wanted {
			continue
		}
		if header.Typeflag != tar.TypeReg || header.Size < 1 || header.Size > maxCampaignControlArtifactBytes {
			t.Fatal("N1 campaign manifest fixture is invalid")
		}
		payload, err := io.ReadAll(io.LimitReader(reader, header.Size+1))
		if err != nil || int64(len(payload)) != header.Size {
			t.Fatal("read exact N1 campaign manifest fixture")
		}
		path := filepath.Join(t.TempDir(), "manifest.json")
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
}

func TestHistoricalSignedCampaignReleaseFailsBeforeDatabase(t *testing.T) {
	verified, artifact, trust := verifiedHistoricalCampaignReleaseBundle(t)
	if verified.Manifest.DatabaseSchema.Current != "20" {
		t.Fatal("fixture is not historical schema20")
	}
	if _, err := integrationcontrol.NewReleaseAuthorityFromVerifiedBundle(verified); err == nil || !strings.Contains(err.Error(), "does not authorize") {
		t.Fatalf("verified historical bundle gained current authority: %v", err)
	}
	previousVersion, previousCommit, previousManual := buildinfo.ProductVersion, buildinfo.Commit, buildinfo.ManualVersion
	buildinfo.ProductVersion, buildinfo.Commit, buildinfo.ManualVersion = "0.3.0-rc.11", strings.Repeat("1", 40), "0.3.0-rc.11/manual-r11"
	t.Cleanup(func() {
		buildinfo.ProductVersion, buildinfo.Commit, buildinfo.ManualVersion = previousVersion, previousCommit, previousManual
	})
	args := campaignPackageCommandArgs()
	schema, err := filepath.Abs(filepath.Join("..", "..", "schemas", "m0", "evidence-event.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < len(args); index += 2 {
		switch args[index] {
		case "--release-artifact":
			args[index+1] = artifact
		case "--release-trust-store":
			args[index+1] = trust
		case "--evidence-schema":
			args[index+1] = schema
		}
	}
	loads, writes := 0, 0
	var output bytes.Buffer
	err = runUniRouterImportCampaignPackageContext(context.Background(), args, &output, campaignPackageCommandDependencies{
		prepare:         prepareUniRouterCampaignPackageImport,
		loadImporterDSN: func() (string, error) { loads++; return "", errors.New("database must not be reached") },
		persist: func(context.Context, string, uniroutercampaign.PreparedImport) ([]byte, error) {
			writes++
			return nil, errors.New("persistence must not be reached")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "signed release manifest does not authorize") || loads != 0 || writes != 0 || output.Len() != 0 {
		t.Fatalf("historical import error=%v database_loads=%d persists=%d output_bytes=%d", err, loads, writes, output.Len())
	}
}
func verifiedHistoricalCampaignReleaseBundle(t *testing.T) (releasebundle.VerifiedBundle, string, string) {
	t.Helper()
	repositoryRoot := filepath.Clean(filepath.Join("..", ".."))
	root := t.TempDir()
	payload := filepath.Join(root, "payload")
	for _, name := range []string{"xinsight-server", "xinsight-importer", "xinsightctl", "xinsight-launcher"} {
		writeHistoricalCampaignTestFile(t, filepath.Join(payload, "bin", name), []byte("exact "+name+"\n"), 0o755)
	}
	// This fixture verifies the historical rc.11/schema20 authority, not the
	// current development migration set. Copy the historical migration prefix.
	entries, err := os.ReadDir(filepath.Join(repositoryRoot, "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && entry.Name() < "000021_" {
			copyHistoricalCampaignTestFile(t, filepath.Join(repositoryRoot, "migrations", entry.Name()), filepath.Join(payload, "migrations", entry.Name()), 0644)
		}
	}
	for _, directory := range []string{"schemas", "fixtures"} {
		copyHistoricalCampaignTestTree(t, filepath.Join(repositoryRoot, directory), filepath.Join(payload, directory))
	}
	for _, source := range []string{
		"internal/webui/manual/zh-CN/index.html",
		"internal/webui/manual/zh-CN/manual.css",
		"internal/webui/manual/zh-CN/manual-icon.svg",
		"release/compatibility-matrix.json",
		"release/release-notes.md",
		"internal/integrationcontrol/testdata/schema20-release/schema-contract.json",
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
		} else if source == "internal/integrationcontrol/testdata/schema20-release/schema-contract.json" {
			destination = filepath.Join(payload, "release", "schema-contract.json")
		}
		copyHistoricalCampaignTestFile(t, filepath.Join(repositoryRoot, source), destination, 0o644)
	}

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
		ConfigPath:      filepath.Join(repositoryRoot, "internal", "integrationcontrol", "testdata", "schema20-release", "release-config.json"),
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
		TrustStorePath: trustStore, ExpectedCommit: commit, ExpectedVersion: "0.3.0-rc.11",
		ExpectedOS: "linux", ExpectedArch: "amd64", Now: createdAt.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	return verified, artifact, trustStore
}

func copyHistoricalCampaignTestTree(t *testing.T, sourceRoot, destinationRoot string) {
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

func copyHistoricalCampaignTestFile(t *testing.T, source, destination string, mode os.FileMode) {
	t.Helper()
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	writeHistoricalCampaignTestFile(t, destination, contents, mode)
}

func writeHistoricalCampaignTestFile(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatal(err)
	}
}

// This command-order unit test uses the real lifecycle validator, with an
// injected prepare step; the signed CLI-to-PostgreSQL path is a separate gate.
func assertCampaignValidationFailsBeforeDatabase(t *testing.T, validate func() error) {
	t.Helper()
	loads, persists := 0, 0
	var output bytes.Buffer
	err := runUniRouterImportCampaignPackageContext(context.Background(), campaignPackageCommandArgs(), &output, campaignPackageCommandDependencies{
		prepare: func(context.Context, campaignPackageCommandInput) (uniroutercampaign.PreparedImport, error) {
			return uniroutercampaign.PreparedImport{}, validate()
		},
		loadImporterDSN: func() (string, error) { loads++; return "", errors.New("database must not be reached") },
		persist: func(context.Context, string, uniroutercampaign.PreparedImport) ([]byte, error) {
			persists++
			return nil, errors.New("persist must not be reached")
		},
	})
	if err == nil || loads != 0 || persists != 0 || output.Len() != 0 {
		t.Fatalf("error=%v loads=%d persists=%d output=%d", err, loads, persists, output.Len())
	}
}
