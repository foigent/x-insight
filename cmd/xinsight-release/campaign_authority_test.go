package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/integrationcontrol"
)

func TestCampaignReleaseAuthorityDeriveAndVerifyExactSignedManifestInputs(t *testing.T) {
	for _, schema := range []string{integrationcontrol.HistoricalCampaignDatabaseSchema, buildinfo.DatabaseSchemaMin} {
		t.Run("schema"+schema, func(t *testing.T) { testCampaignAuthorityDeriveAndVerify(t, schema) })
	}
}

func testCampaignAuthorityDeriveAndVerify(t *testing.T, schema string) {
	root := t.TempDir()
	artifactPath := filepath.Join(root, "signed-release.tar.gz")
	trustPath := filepath.Join(root, "release-trust.json")
	for _, path := range []string{artifactPath, trustPath} {
		if err := os.WriteFile(path, []byte("transport locator only\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	commit := strings.Repeat("1", 40)
	version := "0.3.0-rc.13"
	if schema == buildinfo.DatabaseSchemaMin {
		version = "0.2.0-dev.fpv7"
	}
	authority := campaignAuthorityForSchemaTest(t, commit, version, schema)
	deriveCalls := 0
	derive := func(artifact, trust, expectedCommit, expectedVersion string) (integrationcontrol.ReleaseAuthority, error) {
		deriveCalls++
		if artifact != artifactPath || trust != trustPath || expectedCommit != commit || expectedVersion != version {
			t.Fatalf("derive inputs=%q %q %q %q", artifact, trust, expectedCommit, expectedVersion)
		}
		return authority, nil
	}
	outputDirectory := filepath.Join(root, "campaign-authority")
	var emitted bytes.Buffer
	if err := runCampaignAuthorityWithDeriver([]string{
		"derive",
		"--artifact", artifactPath,
		"--trust-store", trustPath,
		"--expected-commit", commit,
		"--expected-version", version,
		"--output-directory", outputDirectory,
	}, &emitted, derive); err != nil {
		t.Fatal(err)
	}
	productPath := filepath.Join(outputDirectory, productReleaseIdentityFilename)
	verifierPath := filepath.Join(outputDirectory, verifierReleaseFilename)
	for _, path := range []string{productPath, verifierPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		// OpenFile requests 0644; the caller's umask may remove optional reads.
		mode := info.Mode().Perm()
		if mode&0o600 != 0o600 || mode&^0o644 != 0 || info.Size() == 0 {
			t.Fatalf("authority file %s mode=%o size=%d", path, info.Mode().Perm(), info.Size())
		}
		t.Logf("authority file %s mode=%o size=%d", filepath.Base(path), mode, info.Size())
	}
	var verified bytes.Buffer
	verifyArgs := []string{
		"verify",
		"--artifact", artifactPath,
		"--trust-store", trustPath,
		"--product-release", productPath,
		"--verifier-release", verifierPath,
		"--expected-commit", commit,
		"--expected-version", version,
	}
	if err := runCampaignAuthorityWithDeriver(verifyArgs, &verified, derive); err != nil {
		t.Fatal(err)
	}
	if deriveCalls != 2 || !strings.Contains(verified.String(), `"valid": true`) ||
		!strings.Contains(emitted.String(), `"signed_release_manifest_sha256"`) ||
		!strings.Contains(emitted.String(), `"database_schema": "`+schema+`"`) ||
		!strings.Contains(emitted.String(), `"manual_version": "`+authority.ProductRelease.ManualVersion+`"`) {
		t.Fatalf("calls=%d derive=%s verify=%s", deriveCalls, emitted.String(), verified.String())
	}

	productBytes, err := os.ReadFile(productPath)
	if err != nil {
		t.Fatal(err)
	}
	verifierOriginal, err := os.ReadFile(verifierPath)
	if err != nil {
		t.Fatal(err)
	}
	otherSchema := buildinfo.DatabaseSchemaMin
	if schema == otherSchema {
		otherSchema = integrationcontrol.HistoricalCampaignDatabaseSchema
	}
	other := campaignAuthorityForSchemaTest(t, commit, version, otherSchema)
	otherProduct, err := other.ProductRelease.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	otherVerifier, err := other.Verifier.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(productPath, otherProduct, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCampaignAuthorityWithDeriver(verifyArgs, &bytes.Buffer{}, derive); err == nil {
		t.Fatal("mixed-generation sidecars accepted")
	}
	if err := os.WriteFile(verifierPath, otherVerifier, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCampaignAuthorityWithDeriver(verifyArgs, &bytes.Buffer{}, derive); err == nil {
		t.Fatal("other generation pair borrowed signed manifest authority")
	}
	if err := os.WriteFile(verifierPath, verifierOriginal, 0o644); err != nil {
		t.Fatal(err)
	}
	productBytes = bytes.Replace(
		productBytes,
		[]byte(authority.ProductRelease.SignedReleaseManifestSHA256),
		[]byte("sha256:"+strings.Repeat("f", 64)),
		1,
	)
	if err := os.WriteFile(productPath, productBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCampaignAuthorityWithDeriver(verifyArgs, &bytes.Buffer{}, derive); err == nil {
		t.Fatalf("manifest drift error=%v", err)
	}
}

func TestCampaignReleaseAuthorityRejectsOperatorSemanticInputsAndExistingOutput(t *testing.T) {
	root := t.TempDir()
	artifactPath := filepath.Join(root, "signed-release.tar.gz")
	trustPath := filepath.Join(root, "release-trust.json")
	outputDirectory := filepath.Join(root, "campaign-authority")
	for _, path := range []string{artifactPath, trustPath} {
		if err := os.WriteFile(path, []byte("transport locator only\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	derive := func(string, string, string, string) (integrationcontrol.ReleaseAuthority, error) {
		return campaignAuthorityForTest(t, strings.Repeat("1", 40), "0.3.0-rc.13"), nil
	}
	args := []string{
		"derive", "--artifact", artifactPath, "--trust-store", trustPath,
		"--expected-commit", strings.Repeat("1", 40), "--expected-version", "0.3.0-rc.13",
		"--output-directory", outputDirectory,
		"--importer", "/operator/semantic/input",
	}
	if err := runCampaignAuthorityWithDeriver(args, &bytes.Buffer{}, derive); err == nil {
		t.Fatal("operator-supplied importer identity was accepted")
	}
	if _, err := os.Stat(outputDirectory); !os.IsNotExist(err) {
		t.Fatalf("failed derive created output directory: %v", err)
	}

	args = args[:len(args)-2]
	if err := runCampaignAuthorityWithDeriver(args, &bytes.Buffer{}, derive); err != nil {
		t.Fatal(err)
	}
	if err := runCampaignAuthorityWithDeriver(args, &bytes.Buffer{}, derive); err == nil {
		t.Fatal("existing authority directory was overwritten")
	}
}

func campaignAuthorityForTest(t *testing.T, commit, version string) integrationcontrol.ReleaseAuthority {
	t.Helper()
	return campaignAuthorityForSchemaTest(t, commit, version, integrationcontrol.HistoricalCampaignDatabaseSchema)
}

func campaignAuthorityForSchemaTest(t *testing.T, commit, version, schema string) integrationcontrol.ReleaseAuthority {
	t.Helper()
	contractSHA256, err := integrationcontrol.NewComponentContract().SHA256()
	if err != nil {
		t.Fatal(err)
	}
	product := integrationcontrol.ProductReleaseIdentity{
		SchemaVersion:                  integrationcontrol.ProductReleaseIdentityVersion,
		Product:                        integrationcontrol.ProductXInsight,
		ProductVersion:                 version,
		Commit:                         commit,
		SignedReleaseManifestSHA256:    "sha256:" + strings.Repeat("a", 64),
		ReleaseSigningKeyID:            "ed25519:sha256:" + strings.Repeat("b", 64),
		TargetOS:                       "linux",
		TargetArch:                     "amd64",
		DatabaseSchema:                 schema,
		ManualVersion:                  version + "/manual-r13",
		ImporterExecutableSHA256:       "sha256:" + strings.Repeat("c", 64),
		ImporterExecutableSizeBytes:    4096,
		M0EvidenceSchemaSHA256:         integrationcontrol.M0EvidenceSchemaSHA256,
		ComponentContractSHA256:        contractSHA256,
		NeutralN1ReleaseManifestSHA256: integrationcontrol.FrozenN1ReleaseManifestSHA256,
	}
	productSHA256, err := product.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := integrationcontrol.NewVerifierRelease(integrationcontrol.VerifierReleaseInput{
		Commit: commit, Version: version, ComponentContractSHA256: contractSHA256,
		ProductReleaseSHA256:        productSHA256,
		SignedReleaseManifestSHA256: product.SignedReleaseManifestSHA256,
		ImporterExecutableSHA256:    product.ImporterExecutableSHA256,
		M0EvidenceSchemaSHA256:      integrationcontrol.M0EvidenceSchemaSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	authority := integrationcontrol.ReleaseAuthority{ProductRelease: product, Verifier: verifier}
	if err := authority.Validate(); err != nil {
		t.Fatal(err)
	}
	return authority
}
