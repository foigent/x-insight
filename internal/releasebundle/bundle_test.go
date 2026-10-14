package releasebundle_test

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/releasebundle"
)

func TestCurrentReleaseConfigDeclaresRC13HardeningBoundary(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "release", "release-config.json"))
	if err != nil {
		t.Fatalf("read current release config: %v", err)
	}
	var config releasebundle.ReleaseConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("decode current release config: %v", err)
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("validate current release config: %v", err)
	}
	if config.ProductVersion != "0.3.0-rc.13" || config.Manual.Version != "0.3.0-rc.13/manual-r13" {
		t.Fatalf("current release identity=%s manual=%s", config.ProductVersion, config.Manual.Version)
	}
	if !slices.Equal(
		config.APIVersions,
		[]string{"xinsight.investigation.v1", "xinsight.access.v1"},
	) {
		t.Fatalf("current API versions=%v", config.APIVersions)
	}
	if !slices.Equal(config.ReceiptSchemaVersions, []string{
		"xinsight.import-receipt.v1",
		"foigent.operational-receipt.v2",
		"xinsight.admin-credential-rotation-receipt.v1",
		"xinsight.unirouter-campaign-package-import-receipt.v1",
	}) {
		t.Fatalf("current receipt versions=%v", config.ReceiptSchemaVersions)
	}
	expectedUpgradeSources := []string{
		"0.2.0-rc.3",
		"0.2.0-rc.4",
		"0.2.0-rc.5",
		"0.2.0-rc.6",
		"0.2.0-rc.7",
		"0.2.0-rc.8",
		"0.2.0-rc.9",
		"0.2.0-rc.10",
		"0.2.0-rc.11",
		"0.2.0-rc.12",
		"0.2.0-rc.13",
		"0.2.0-rc.14",
		"0.2.0-rc.15",
		"0.3.0-rc.2",
		"0.3.0-rc.3",
		"0.3.0-rc.4",
		"0.3.0-rc.5",
		"0.3.0-rc.6",
		"0.3.0-rc.7",
		"0.3.0-rc.8",
		"0.3.0-rc.9",
		"0.3.0-rc.10",
		"0.3.0-rc.11",
		"0.3.0-rc.12",
	}
	if !slices.Equal(config.SupportedUpgradeFromVersions, expectedUpgradeSources) {
		t.Fatalf(
			"rc.13 upgrade sources=%v expected=%v",
			config.SupportedUpgradeFromVersions,
			expectedUpgradeSources,
		)
	}
	if slices.Contains(config.SupportedUpgradeFromVersions, config.ProductVersion) ||
		slices.Contains(config.SupportedUpgradeFromVersions, "0.2.0-rc.2") {
		t.Fatalf("same-version or unsupported old source was accepted: %v", config.SupportedUpgradeFromVersions)
	}
	if config.ApplicationCompatibility.DatabaseSchemaMin != "23" ||
		config.ApplicationCompatibility.DatabaseSchemaMax != "23" ||
		config.DatabaseSchema.Current != "23" ||
		config.Rollback.ApplicationRollbackCompatible {
		t.Fatalf(
			"rc.13 schema/rollback boundary=%+v schema=%+v rollback=%+v",
			config.ApplicationCompatibility,
			config.DatabaseSchema,
			config.Rollback,
		)
	}
	if len(config.SupportedLegacySources) != 1 ||
		config.SupportedLegacySources[0].ApplicationDatabaseSchemaMax != "11" {
		t.Fatalf("legacy application compatibility was widened: %+v", config.SupportedLegacySources)
	}
	if config.SchemaContract.SchemaVersion != "xinsight.schema-contract.v2" ||
		config.SchemaContract.SemanticSHA256 != "sha256:18cdc17c34ca477a6e9259cbbbe6affd0fa77906cfc1b2d74c5a48934afbab81" ||
		config.SchemaContract.MigrationSetSHA256 != "sha256:346eddbda29eae52b62f1b8e7383387ba99dc608ab065160695462eaff806cfe" {
		t.Fatalf("rc.13 schema contract identity=%+v", config.SchemaContract)
	}
}

func TestCurrentRC13ReleaseConfigRejectsCurrentDevelopmentSchema(t *testing.T) {
	root := persistentTestDir(t)
	payload := filepath.Join(root, "payload")
	writeProductPayload(t, payload)
	migrations, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	currentSchema, err := strconv.Atoi(buildinfo.DatabaseSchemaMax)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != currentSchema {
		t.Fatalf("development migration count=%d", len(migrations))
	}
	for _, source := range migrations {
		contents, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(payload, "migrations", filepath.Base(source)), contents, 0o644)
	}
	privateKey := filepath.Join(root, "signing-key.json")
	trustStore := filepath.Join(root, "trusted-keys.json")
	createdAt := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	if _, err := releasebundle.GenerateKeyPair(privateKey, trustStore, createdAt.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	_, err = releasebundle.Pack(releasebundle.PackOptions{
		PayloadDir: payload, ConfigPath: filepath.Join("..", "..", "release", "release-config.json"),
		SigningKeyPath: privateKey, OutputPath: filepath.Join(root, "must-not-exist.tar.gz"),
		Commit: "2424242424242424242424242424242424242424", SourceDateEpoch: createdAt,
		BuildToolchain: releasebundle.BuildToolchain{
			GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "database_schema.current does not match packaged migration count") {
		t.Fatalf("schema-23 release config with current development payload error=%v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "must-not-exist.tar.gz")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("development payload produced an rc.13 artifact: %v", statErr)
	}
}

func TestSignedBundleRoundTripIsDeterministicAndExtractable(t *testing.T) {
	root := persistentTestDir(t)
	payload := filepath.Join(root, "payload")
	writeProductPayload(t, payload)
	configPath := filepath.Join(root, "release-config.json")
	writeReleaseConfig(t, configPath)
	privateKey := filepath.Join(root, "signing-key.json")
	trustStore := filepath.Join(root, "trusted-keys.json")
	createdAt := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	if _, err := releasebundle.GenerateKeyPair(privateKey, trustStore, createdAt.Add(-time.Minute)); err != nil {
		t.Fatalf("generate key: %v", err)
	}

	options := releasebundle.PackOptions{
		PayloadDir:      payload,
		ConfigPath:      configPath,
		SigningKeyPath:  privateKey,
		OutputPath:      filepath.Join(root, "xinsight-v0.2.0-rc.1-linux-amd64-a.tar.gz"),
		Commit:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SourceDateEpoch: createdAt,
		BuildToolchain: releasebundle.BuildToolchain{
			GoVersion:   "go1.25.11",
			NodeVersion: "v22.23.1",
			NPMVersion:  "10.9.8",
		},
	}
	first, err := releasebundle.Pack(options)
	if err != nil {
		t.Fatalf("pack first: %v", err)
	}
	options.OutputPath = filepath.Join(root, "xinsight-v0.2.0-rc.1-linux-amd64-b.tar.gz")
	second, err := releasebundle.Pack(options)
	if err != nil {
		t.Fatalf("pack second: %v", err)
	}
	if first.BundleSHA256 != second.BundleSHA256 || first.ManifestSHA256 != second.ManifestSHA256 || first.PayloadSHA256 != second.PayloadSHA256 {
		t.Fatalf("nondeterministic results first=%+v second=%+v", first, second)
	}

	verified, err := releasebundle.Verify(first.OutputPath, releasebundle.VerifyOptions{
		TrustStorePath:  trustStore,
		ExpectedCommit:  first.Manifest.Commit,
		ExpectedVersion: "0.2.0-rc.1",
		ExpectedOS:      "linux",
		ExpectedArch:    "amd64",
		Now:             createdAt.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verified.Manifest.SchemaVersion != releasebundle.ManifestSchemaVersion || verified.Manifest.Manual.Version != "0.2.0-rc.1/manual-r1" {
		t.Fatalf("manifest=%+v", verified.Manifest)
	}
	destination := filepath.Join(root, "extracted")
	var extractErr error
	func() {
		previousUmask := syscall.Umask(0o077)
		defer syscall.Umask(previousUmask)
		extractErr = releasebundle.ExtractPayload(verified, destination)
	}()
	if extractErr != nil {
		t.Fatalf("extract under restrictive umask: %v", extractErr)
	}
	if err := releasebundle.VerifyExtractedPayload(verified, destination); err != nil {
		t.Fatalf("verify extracted payload: %v", err)
	}
	if err := os.Chmod(filepath.Join(destination, "manual", "zh-CN"), 0o700); err != nil {
		t.Fatalf("change extracted directory mode: %v", err)
	}
	if err := releasebundle.VerifyExtractedPayload(verified, destination); err == nil {
		t.Fatal("expected extracted directory mode rejection")
	}
	if contents, err := os.ReadFile(filepath.Join(destination, "bin", "xinsightctl")); err != nil || string(contents) != "ctl\n" {
		t.Fatalf("extracted ctl=%q err=%v", string(contents), err)
	}
	if err := releasebundle.ExtractPayload(verified, destination); err == nil {
		t.Fatal("expected existing destination refusal")
	}
}

func TestBundleRejectsWrongOrRevokedTrustAndTampering(t *testing.T) {
	root := persistentTestDir(t)
	payload := filepath.Join(root, "payload")
	writeProductPayload(t, payload)
	configPath := filepath.Join(root, "release-config.json")
	writeReleaseConfig(t, configPath)
	privateKey := filepath.Join(root, "signing-key.json")
	trustStore := filepath.Join(root, "trusted-keys.json")
	createdAt := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	if _, err := releasebundle.GenerateKeyPair(privateKey, trustStore, createdAt.Add(-time.Minute)); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	output := filepath.Join(root, "release.tar.gz")
	if _, err := releasebundle.Pack(releasebundle.PackOptions{
		PayloadDir:      payload,
		ConfigPath:      configPath,
		SigningKeyPath:  privateKey,
		OutputPath:      output,
		Commit:          "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		SourceDateEpoch: createdAt,
		BuildToolchain:  releasebundle.BuildToolchain{GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8"},
	}); err != nil {
		t.Fatalf("pack: %v", err)
	}

	otherPrivate := filepath.Join(root, "other-key.json")
	otherTrust := filepath.Join(root, "other-trust.json")
	if _, err := releasebundle.GenerateKeyPair(otherPrivate, otherTrust, createdAt.Add(-time.Minute)); err != nil {
		t.Fatalf("generate other key: %v", err)
	}
	if _, err := releasebundle.Verify(output, releasebundle.VerifyOptions{TrustStorePath: otherTrust, Now: createdAt}); !errors.Is(err, releasebundle.ErrUntrustedRelease) {
		t.Fatalf("wrong trust error=%v", err)
	}
	trustLink := filepath.Join(root, "trust-link.json")
	if err := os.Symlink(trustStore, trustLink); err != nil {
		t.Fatal(err)
	}
	if _, err := releasebundle.Verify(output, releasebundle.VerifyOptions{TrustStorePath: trustLink, Now: createdAt}); err == nil {
		t.Fatal("expected symbolic-link trust store rejection")
	}

	var store releasebundle.TrustStore
	readJSON(t, trustStore, &store)
	store.Keys[0].Status = "revoked"
	revokedPath := filepath.Join(root, "revoked-trust.json")
	writeJSON(t, revokedPath, store, 0o644)
	if _, err := releasebundle.Verify(output, releasebundle.VerifyOptions{TrustStorePath: revokedPath, Now: createdAt}); !errors.Is(err, releasebundle.ErrUntrustedRelease) {
		t.Fatalf("revoked trust error=%v", err)
	}

	contents, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	contents[len(contents)/2] ^= 0xff
	tampered := filepath.Join(root, "tampered.tar.gz")
	if err := os.WriteFile(tampered, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := releasebundle.Verify(tampered, releasebundle.VerifyOptions{TrustStorePath: trustStore, Now: createdAt}); err == nil {
		t.Fatal("expected tampered bundle rejection")
	}
}

func TestHistoricalRC3BundleWithoutManagedLauncherRemainsVerifiable(t *testing.T) {
	root := persistentTestDir(t)
	payload := filepath.Join(root, "payload")
	writeProductPayload(t, payload)
	managedLauncher := filepath.Join(payload, "deploy", "99-xinsight-managed-launcher.conf")
	if err := os.Remove(managedLauncher); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(payload, "deploy", "xinsight-feed.service"),
		filepath.Join(payload, "deploy", "xinsight-feed.env.example"),
		filepath.Join(payload, "deploy", "xinsight-litellm-feed.service"),
		filepath.Join(payload, "deploy", "xinsight-litellm-feed.env.example"),
	} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	configPath := filepath.Join(root, "release-config.json")
	writeReleaseConfigVersion(t, configPath, "0.2.0-rc.3", "0.2.0-rc.3/manual-r3")
	privateKey := filepath.Join(root, "signing-key.json")
	trustStore := filepath.Join(root, "trusted-keys.json")
	createdAt := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	if _, err := releasebundle.GenerateKeyPair(privateKey, trustStore, createdAt.Add(-time.Minute)); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	packed, err := releasebundle.Pack(releasebundle.PackOptions{
		PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: privateKey,
		OutputPath: filepath.Join(root, "xinsight-v0.2.0-rc.3-linux-amd64.tar.gz"),
		Commit:     "3333333333333333333333333333333333333333", SourceDateEpoch: createdAt,
		BuildToolchain: releasebundle.BuildToolchain{GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8"},
	})
	if err != nil {
		t.Fatalf("pack historical rc.3: %v", err)
	}
	verified, err := releasebundle.Verify(packed.OutputPath, releasebundle.VerifyOptions{
		TrustStorePath: trustStore, ExpectedVersion: "0.2.0-rc.3", ExpectedCommit: packed.Manifest.Commit, Now: createdAt,
	})
	if err != nil {
		t.Fatalf("verify historical rc.3: %v", err)
	}
	destination := filepath.Join(root, "extracted")
	if err := releasebundle.ExtractPayload(verified, destination); err != nil {
		t.Fatalf("extract historical rc.3: %v", err)
	}
	if err := releasebundle.VerifyExtractedPayload(verified, destination); err != nil {
		t.Fatalf("verify extracted historical rc.3: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "deploy", "99-xinsight-managed-launcher.conf")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("historical rc.3 unexpectedly contains managed launcher: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "deploy", "xinsight-feed.service")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("historical rc.3 unexpectedly contains managed feed worker: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destination, "deploy", "xinsight-litellm-feed.service")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("historical rc.3 unexpectedly contains managed LiteLLM worker: %v", err)
	}
}

func TestCurrentAndUnknownBundlesRequireManagedLauncher(t *testing.T) {
	root := persistentTestDir(t)
	payload := filepath.Join(root, "payload")
	writeProductPayload(t, payload)
	if err := os.Remove(filepath.Join(payload, "deploy", "99-xinsight-managed-launcher.conf")); err != nil {
		t.Fatal(err)
	}
	privateKey := filepath.Join(root, "signing-key.json")
	trustStore := filepath.Join(root, "trusted-keys.json")
	createdAt := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	if _, err := releasebundle.GenerateKeyPair(privateKey, trustStore, createdAt.Add(-time.Minute)); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	for _, candidate := range []struct {
		version string
		manual  string
	}{
		{version: "0.2.0-rc.5", manual: "0.2.0-rc.5/manual-r5"},
		{version: "0.2.0-rc.6", manual: "0.2.0-rc.6/manual-r6"},
		{version: "0.2.0-rc.99", manual: "0.2.0-rc.99/manual-r99"},
	} {
		t.Run(candidate.version, func(t *testing.T) {
			version := candidate.version
			configPath := filepath.Join(root, version+"-release-config.json")
			writeReleaseConfigVersion(t, configPath, version, candidate.manual)
			_, err := releasebundle.Pack(releasebundle.PackOptions{
				PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: privateKey,
				OutputPath: filepath.Join(root, version+".tar.gz"),
				Commit:     "5555555555555555555555555555555555555555", SourceDateEpoch: createdAt,
				BuildToolchain: releasebundle.BuildToolchain{GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8"},
			})
			if !errors.Is(err, releasebundle.ErrInvalidBundle) || !strings.Contains(err.Error(), "deploy/99-xinsight-managed-launcher.conf") {
				t.Fatalf("missing managed launcher error=%v", err)
			}
		})
	}
}

func TestNewProductIdentitiesRequireManagedFeedWorker(t *testing.T) {
	root := persistentTestDir(t)
	payload := filepath.Join(root, "payload")
	writeProductPayload(t, payload)
	if err := os.Remove(filepath.Join(payload, "deploy", "xinsight-feed.service")); err != nil {
		t.Fatal(err)
	}
	privateKey := filepath.Join(root, "signing-key.json")
	trustStore := filepath.Join(root, "trusted-keys.json")
	createdAt := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	if _, err := releasebundle.GenerateKeyPair(privateKey, trustStore, createdAt.Add(-time.Minute)); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	for _, candidate := range []struct {
		version string
		manual  string
	}{
		{version: "0.2.0-rc.9", manual: "0.2.0-rc.9/manual-r9"},
		{version: "0.2.0-rc.10", manual: "0.2.0-rc.10/manual-r10"},
		{version: "0.2.0-rc.11", manual: "0.2.0-rc.11/manual-r11"},
		{version: "0.2.0-rc.12", manual: "0.2.0-rc.12/manual-r12"},
		{version: "0.2.0", manual: "0.2.0/manual-r1"},
		{version: "0.2.0-rc.99", manual: "0.2.0-rc.99/manual-r99"},
	} {
		t.Run(candidate.version, func(t *testing.T) {
			configPath := filepath.Join(root, candidate.version+"-feed-release-config.json")
			writeReleaseConfigVersion(t, configPath, candidate.version, candidate.manual)
			_, err := releasebundle.Pack(releasebundle.PackOptions{
				PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: privateKey,
				OutputPath: filepath.Join(root, candidate.version+"-feed.tar.gz"),
				Commit:     "6666666666666666666666666666666666666666", SourceDateEpoch: createdAt,
				BuildToolchain: releasebundle.BuildToolchain{GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8"},
			})
			if !errors.Is(err, releasebundle.ErrInvalidBundle) || !strings.Contains(err.Error(), "deploy/xinsight-feed.service") {
				t.Fatalf("missing managed feed worker error=%v", err)
			}
		})
	}
}

func TestRC12AndUnknownBundlesRequireManagedLiteLLMWorker(t *testing.T) {
	root := persistentTestDir(t)
	payload := filepath.Join(root, "payload")
	writeProductPayload(t, payload)
	if err := os.Remove(filepath.Join(payload, "deploy", "xinsight-litellm-feed.service")); err != nil {
		t.Fatal(err)
	}
	privateKey := filepath.Join(root, "signing-key.json")
	trustStore := filepath.Join(root, "trusted-keys.json")
	createdAt := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	if _, err := releasebundle.GenerateKeyPair(privateKey, trustStore, createdAt.Add(-time.Minute)); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	for _, candidate := range []struct {
		version string
		manual  string
	}{
		{version: "0.2.0-rc.12", manual: "0.2.0-rc.12/manual-r12"},
		{version: "0.2.0", manual: "0.2.0/manual-r1"},
		{version: "0.2.0-rc.99", manual: "0.2.0-rc.99/manual-r99"},
	} {
		t.Run(candidate.version, func(t *testing.T) {
			configPath := filepath.Join(root, candidate.version+"-litellm-release-config.json")
			writeReleaseConfigVersion(t, configPath, candidate.version, candidate.manual)
			_, err := releasebundle.Pack(releasebundle.PackOptions{
				PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: privateKey,
				OutputPath: filepath.Join(root, candidate.version+"-litellm.tar.gz"),
				Commit:     "8888888888888888888888888888888888888888", SourceDateEpoch: createdAt,
				BuildToolchain: releasebundle.BuildToolchain{
					GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8",
				},
			})
			if !errors.Is(err, releasebundle.ErrInvalidBundle) ||
				!strings.Contains(err.Error(), "deploy/xinsight-litellm-feed.service") {
				t.Fatalf("missing managed LiteLLM worker error=%v", err)
			}
		})
	}
}

func TestRC12AndUnknownBundlesRequireConnectorController(t *testing.T) {
	root := persistentTestDir(t)
	payload := filepath.Join(root, "payload")
	writeProductPayload(t, payload)
	if err := os.Remove(filepath.Join(payload, "deploy", "xinsight-connector-controller.service")); err != nil {
		t.Fatal(err)
	}
	privateKey := filepath.Join(root, "signing-key.json")
	trustStore := filepath.Join(root, "trusted-keys.json")
	createdAt := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	if _, err := releasebundle.GenerateKeyPair(privateKey, trustStore, createdAt.Add(-time.Minute)); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	for _, candidate := range []struct {
		version string
		manual  string
	}{
		{version: "0.3.0-rc.12", manual: "0.3.0-rc.12/manual-r12"},
		{version: "0.3.0-rc.99", manual: "0.3.0-rc.99/manual-r99"},
	} {
		t.Run(candidate.version, func(t *testing.T) {
			configPath := filepath.Join(root, candidate.version+"-controller-release-config.json")
			writeReleaseConfigVersion(t, configPath, candidate.version, candidate.manual)
			_, err := releasebundle.Pack(releasebundle.PackOptions{
				PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: privateKey,
				OutputPath: filepath.Join(root, candidate.version+"-controller.tar.gz"),
				Commit:     "1212121212121212121212121212121212121212", SourceDateEpoch: createdAt,
				BuildToolchain: releasebundle.BuildToolchain{GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8"},
			})
			if !errors.Is(err, releasebundle.ErrInvalidBundle) ||
				!strings.Contains(err.Error(), "deploy/xinsight-connector-controller.service") {
				t.Fatalf("missing connector controller error=%v", err)
			}
		})
	}
}

func TestRC12BundleRequiresConnectorTargetEnvironmentExample(t *testing.T) {
	root := persistentTestDir(t)
	payload := filepath.Join(root, "payload")
	writeProductPayload(t, payload)
	if err := os.Remove(filepath.Join(payload, "deploy", "xinsight-connector.env.example")); err != nil {
		t.Fatal(err)
	}
	privateKey := filepath.Join(root, "signing-key.json")
	trustStore := filepath.Join(root, "trusted-keys.json")
	createdAt := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	if _, err := releasebundle.GenerateKeyPair(privateKey, trustStore, createdAt.Add(-time.Minute)); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	configPath := filepath.Join(root, "release-config.json")
	writeReleaseConfigVersion(t, configPath, "0.3.0-rc.12", "0.3.0-rc.12/manual-r12")
	_, err := releasebundle.Pack(releasebundle.PackOptions{
		PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: privateKey,
		OutputPath: filepath.Join(root, "release.tar.gz"),
		Commit:     "1212121212121212121212121212121212121212", SourceDateEpoch: createdAt,
		BuildToolchain: releasebundle.BuildToolchain{GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8"},
	})
	if !errors.Is(err, releasebundle.ErrInvalidBundle) ||
		!strings.Contains(err.Error(), "deploy/xinsight-connector.env.example") {
		t.Fatalf("missing Connector target environment error=%v", err)
	}
}

func TestReceiverCredentialReleaseIdentitiesRequireFrozenRegistrationSchema(t *testing.T) {
	root := persistentTestDir(t)
	payload := filepath.Join(root, "payload")
	writeProductPayload(t, payload)
	schemaPath := filepath.Join(
		payload,
		"schemas",
		"integration",
		"feed-credential-registration-v1.schema.json",
	)
	if err := os.Remove(schemaPath); err != nil {
		t.Fatal(err)
	}
	privateKey := filepath.Join(root, "signing-key.json")
	trustStore := filepath.Join(root, "trusted-keys.json")
	createdAt := time.Date(2026, 7, 26, 4, 0, 0, 0, time.UTC)
	if _, err := releasebundle.GenerateKeyPair(privateKey, trustStore, createdAt.Add(-time.Minute)); err != nil {
		t.Fatalf("generate key: %v", err)
	}

	for _, candidate := range []struct {
		version string
		manual  string
	}{
		{version: "0.2.0-rc.9", manual: "0.2.0-rc.9/manual-r9"},
		{version: "0.2.0-rc.10", manual: "0.2.0-rc.10/manual-r10"},
		{version: "0.2.0-rc.11", manual: "0.2.0-rc.11/manual-r11"},
		{version: "0.2.0", manual: "0.2.0/manual-r1"},
		{version: "0.2.0-rc.99", manual: "0.2.0-rc.99/manual-r99"},
	} {
		t.Run(candidate.version, func(t *testing.T) {
			configPath := filepath.Join(root, candidate.version+"-credential-release-config.json")
			writeReleaseConfigVersion(t, configPath, candidate.version, candidate.manual)
			_, err := releasebundle.Pack(releasebundle.PackOptions{
				PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: privateKey,
				OutputPath: filepath.Join(root, candidate.version+"-credential.tar.gz"),
				Commit:     "7777777777777777777777777777777777777777", SourceDateEpoch: createdAt,
				BuildToolchain: releasebundle.BuildToolchain{
					GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8",
				},
			})
			if !errors.Is(err, releasebundle.ErrInvalidBundle) ||
				!strings.Contains(err.Error(), "feed-credential-registration-v1.schema.json") {
				t.Fatalf("missing Frozen Registration Bundle schema error=%v", err)
			}
		})
	}
}

func TestRC13AndUnknownBundlesRequireLocalAccountSchema(t *testing.T) {
	root := persistentTestDir(t)
	payload := filepath.Join(root, "payload")
	writeProductPayload(t, payload)
	schemaPath := filepath.Join(
		payload,
		"schemas",
		"access",
		"local-accounts-v1.schema.json",
	)
	if err := os.Remove(schemaPath); err != nil {
		t.Fatal(err)
	}
	privateKey := filepath.Join(root, "signing-key.json")
	trustStore := filepath.Join(root, "trusted-keys.json")
	createdAt := time.Date(2026, 7, 29, 4, 0, 0, 0, time.UTC)
	if _, err := releasebundle.GenerateKeyPair(privateKey, trustStore, createdAt.Add(-time.Minute)); err != nil {
		t.Fatalf("generate key: %v", err)
	}

	for _, candidate := range []struct {
		version string
		manual  string
	}{
		{version: "0.2.0-rc.13", manual: "0.2.0-rc.13/manual-r13"},
		{version: "0.2.0", manual: "0.2.0/manual-r1"},
		{version: "0.2.0-rc.99", manual: "0.2.0-rc.99/manual-r99"},
	} {
		t.Run(candidate.version, func(t *testing.T) {
			configPath := filepath.Join(root, candidate.version+"-access-release-config.json")
			writeReleaseConfigVersion(t, configPath, candidate.version, candidate.manual)
			_, err := releasebundle.Pack(releasebundle.PackOptions{
				PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: privateKey,
				OutputPath: filepath.Join(root, candidate.version+"-access.tar.gz"),
				Commit:     "9999999999999999999999999999999999999999", SourceDateEpoch: createdAt,
				BuildToolchain: releasebundle.BuildToolchain{
					GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8",
				},
			})
			if !errors.Is(err, releasebundle.ErrInvalidBundle) ||
				!strings.Contains(err.Error(), "schemas/access/local-accounts-v1.schema.json") {
				t.Fatalf("missing local account schema error=%v", err)
			}
		})
	}
}

func TestRC14RC15AndUnknownBundlesRequireTrustedTLSProxyTemplates(t *testing.T) {
	root := persistentTestDir(t)
	payload := filepath.Join(root, "payload")
	writeProductPayload(t, payload)
	if err := os.Remove(filepath.Join(payload, "deploy", "xinsight-nginx-ip.conf.example")); err != nil {
		t.Fatal(err)
	}
	privateKey := filepath.Join(root, "signing-key.json")
	trustStore := filepath.Join(root, "trusted-keys.json")
	createdAt := time.Date(2026, 7, 30, 4, 0, 0, 0, time.UTC)
	if _, err := releasebundle.GenerateKeyPair(privateKey, trustStore, createdAt.Add(-time.Minute)); err != nil {
		t.Fatalf("generate key: %v", err)
	}

	for _, candidate := range []struct {
		version string
		manual  string
	}{
		{version: "0.2.0-rc.14", manual: "0.2.0-rc.14/manual-r14"},
		{version: "0.2.0-rc.15", manual: "0.2.0-rc.15/manual-r15"},
		{version: "0.3.0-rc.1", manual: "0.3.0-rc.1/manual-r1"},
		{version: "0.3.0-rc.2", manual: "0.3.0-rc.2/manual-r2"},
		{version: "0.3.0-rc.3", manual: "0.3.0-rc.3/manual-r3"},
		{version: "0.3.0-rc.4", manual: "0.3.0-rc.4/manual-r4"},
		{version: "0.3.0-rc.5", manual: "0.3.0-rc.5/manual-r5"},
		{version: "0.3.0-rc.6", manual: "0.3.0-rc.6/manual-r6"},
		{version: "0.3.0-rc.7", manual: "0.3.0-rc.7/manual-r7"},
		{version: "0.3.0-rc.8", manual: "0.3.0-rc.8/manual-r8"},
		{version: "0.3.0-rc.9", manual: "0.3.0-rc.9/manual-r9"},
		{version: "0.3.0-rc.10", manual: "0.3.0-rc.10/manual-r10"},
		{version: "0.3.0-rc.11", manual: "0.3.0-rc.11/manual-r11"},
		{version: "0.2.0", manual: "0.2.0/manual-r1"},
		{version: "0.2.0-rc.99", manual: "0.2.0-rc.99/manual-r99"},
	} {
		t.Run(candidate.version, func(t *testing.T) {
			configPath := filepath.Join(root, candidate.version+"-proxy-release-config.json")
			writeReleaseConfigVersion(t, configPath, candidate.version, candidate.manual)
			_, err := releasebundle.Pack(releasebundle.PackOptions{
				PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: privateKey,
				OutputPath: filepath.Join(root, candidate.version+"-proxy.tar.gz"),
				Commit:     "1414141414141414141414141414141414141414", SourceDateEpoch: createdAt,
				BuildToolchain: releasebundle.BuildToolchain{
					GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8",
				},
			})
			if !errors.Is(err, releasebundle.ErrInvalidBundle) ||
				!strings.Contains(err.Error(), "deploy/xinsight-nginx-ip.conf.example") {
				t.Fatalf("missing trusted TLS proxy template error=%v", err)
			}
		})
	}
}

func TestRC7RC8AndUnknownBundlesRequireCheckpointEpochSchema(t *testing.T) {
	root := persistentTestDir(t)
	payload := filepath.Join(root, "payload")
	writeProductPayload(t, payload)
	schemaPath := filepath.Join(
		payload,
		"schemas",
		"integration",
		"connector-checkpoint-epoch-prepare-v1.schema.json",
	)
	if err := os.Remove(schemaPath); err != nil {
		t.Fatal(err)
	}
	privateKey := filepath.Join(root, "signing-key.json")
	trustStore := filepath.Join(root, "trusted-keys.json")
	createdAt := time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC)
	if _, err := releasebundle.GenerateKeyPair(privateKey, trustStore, createdAt.Add(-time.Minute)); err != nil {
		t.Fatalf("generate key: %v", err)
	}

	for _, candidate := range []struct {
		version string
		manual  string
	}{
		{version: "0.3.0-rc.7", manual: "0.3.0-rc.7/manual-r7"},
		{version: "0.3.0-rc.8", manual: "0.3.0-rc.8/manual-r8"},
		{version: "0.3.0-rc.9", manual: "0.3.0-rc.9/manual-r9"},
		{version: "0.3.0-rc.10", manual: "0.3.0-rc.10/manual-r10"},
		{version: "0.3.0-rc.11", manual: "0.3.0-rc.11/manual-r11"},
		{version: "0.3.0-rc.99", manual: "0.3.0-rc.99/manual-r99"},
	} {
		t.Run(candidate.version, func(t *testing.T) {
			configPath := filepath.Join(root, candidate.version+"-epoch-release-config.json")
			writeReleaseConfigVersion(t, configPath, candidate.version, candidate.manual)
			_, err := releasebundle.Pack(releasebundle.PackOptions{
				PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: privateKey,
				OutputPath: filepath.Join(root, candidate.version+"-epoch.tar.gz"),
				Commit:     "1515151515151515151515151515151515151515", SourceDateEpoch: createdAt,
				BuildToolchain: releasebundle.BuildToolchain{
					GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8",
				},
			})
			if !errors.Is(err, releasebundle.ErrInvalidBundle) ||
				!strings.Contains(err.Error(), "connector-checkpoint-epoch-prepare-v1.schema.json") {
				t.Fatalf("missing checkpoint epoch schema error=%v", err)
			}
		})
	}
}

func TestRC9AndUnknownBundlesRequireGeneratedSchemaContract(t *testing.T) {
	root := persistentTestDir(t)
	payload := filepath.Join(root, "payload")
	writeProductPayload(t, payload)
	if err := os.Remove(filepath.Join(payload, "release", "schema-contract.json")); err != nil {
		t.Fatal(err)
	}
	privateKey := filepath.Join(root, "signing-key.json")
	trustStore := filepath.Join(root, "trusted-keys.json")
	createdAt := time.Date(2026, 8, 6, 4, 0, 0, 0, time.UTC)
	if _, err := releasebundle.GenerateKeyPair(privateKey, trustStore, createdAt.Add(-time.Minute)); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	for _, candidate := range []struct {
		version string
		manual  string
	}{
		{version: "0.3.0-rc.9", manual: "0.3.0-rc.9/manual-r9"},
		{version: "0.3.0-rc.10", manual: "0.3.0-rc.10/manual-r10"},
		{version: "0.3.0-rc.11", manual: "0.3.0-rc.11/manual-r11"},
		{version: "0.3.0-rc.99", manual: "0.3.0-rc.99/manual-r99"},
	} {
		t.Run(candidate.version, func(t *testing.T) {
			configPath := filepath.Join(root, candidate.version+"-schema-contract-release-config.json")
			writeReleaseConfigVersion(t, configPath, candidate.version, candidate.manual)
			_, err := releasebundle.Pack(releasebundle.PackOptions{
				PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: privateKey,
				OutputPath: filepath.Join(root, candidate.version+"-schema-contract.tar.gz"),
				Commit:     "1616161616161616161616161616161616161616", SourceDateEpoch: createdAt,
				BuildToolchain: releasebundle.BuildToolchain{
					GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8",
				},
			})
			if !errors.Is(err, releasebundle.ErrInvalidBundle) ||
				!strings.Contains(err.Error(), "release/schema-contract.json") {
				t.Fatalf("missing generated schema contract error=%v", err)
			}
		})
	}
}

func TestRC9BundleRejectsSchemaContractContentDigestDrift(t *testing.T) {
	root := persistentTestDir(t)
	payload := filepath.Join(root, "payload")
	writeProductPayload(t, payload)
	contractPath := filepath.Join(payload, "release", "schema-contract.json")
	contract, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	contract = bytes.Replace(
		contract,
		[]byte("sha256:2222222222222222222222222222222222222222222222222222222222222222"),
		[]byte("sha256:4444444444444444444444444444444444444444444444444444444444444444"),
		1,
	)
	if err := os.WriteFile(contractPath, contract, 0o644); err != nil {
		t.Fatal(err)
	}
	privateKey := filepath.Join(root, "signing-key.json")
	trustStore := filepath.Join(root, "trusted-keys.json")
	createdAt := time.Date(2026, 8, 6, 4, 0, 0, 0, time.UTC)
	if _, err := releasebundle.GenerateKeyPair(privateKey, trustStore, createdAt.Add(-time.Minute)); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	configPath := filepath.Join(root, "release-config.json")
	writeReleaseConfigVersion(t, configPath, "0.3.0-rc.9", "0.3.0-rc.9/manual-r9")
	_, err = releasebundle.Pack(releasebundle.PackOptions{
		PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: privateKey,
		OutputPath: filepath.Join(root, "schema-contract-drift.tar.gz"),
		Commit:     "1616161616161616161616161616161616161616", SourceDateEpoch: createdAt,
		BuildToolchain: releasebundle.BuildToolchain{
			GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8",
		},
	})
	if !errors.Is(err, releasebundle.ErrInvalidBundle) ||
		!strings.Contains(err.Error(), "schema contract contents") {
		t.Fatalf("schema contract content drift error=%v", err)
	}
}

func TestEnglishReleaseRequiresBothManualsAndRedistributionNotices(t *testing.T) {
	required := []string{"manual/en-US/index.html", "manual/en-US/manual.css", "manual/en-US/manual-icon.svg", "LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.md", "licenses/manifest.json"}
	for _, missing := range append([]string{""}, required...) {
		t.Run(missing, func(t *testing.T) {
			root := persistentTestDir(t)
			payload, configPath := filepath.Join(root, "payload"), filepath.Join(root, "config.json")
			writeProductPayload(t, payload)
			writeReleaseConfig(t, configPath)
			var config releasebundle.ReleaseConfig
			readJSON(t, configPath, &config)
			config.Manual.Language = "en-US"
			writeJSON(t, configPath, config, 0644)
			for _, name := range required {
				if name != missing {
					writeFile(t, filepath.Join(payload, name), []byte("test presentation or notice\n"), 0644)
				}
			}
			createdAt := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
			key := filepath.Join(root, "test-key.json")
			if _, err := releasebundle.GenerateKeyPair(key, filepath.Join(root, "test-trust.json"), createdAt.Add(-time.Minute)); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(root, "test-only.tar.gz")
			_, err := releasebundle.Pack(releasebundle.PackOptions{
				PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: key, OutputPath: output,
				Commit: strings.Repeat("c", 40), SourceDateEpoch: createdAt,
				BuildToolchain: releasebundle.BuildToolchain{GoVersion: "go1.25.12", NodeVersion: "v22.23.1", NPMVersion: "10.9.8"},
			})
			if missing == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil || !strings.Contains(err.Error(), missing) {
					t.Fatalf("missing %s: %v", missing, err)
				}
				if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("invalid payload created artifact: %v", err)
				}
			}
		})
	}
}

func TestPackRejectsUnsafePayloadAndPrivateKeyMode(t *testing.T) {
	root := persistentTestDir(t)
	payload := filepath.Join(root, "payload")
	writeProductPayload(t, payload)
	configPath := filepath.Join(root, "release-config.json")
	writeReleaseConfig(t, configPath)
	privateKey := filepath.Join(root, "signing-key.json")
	trustStore := filepath.Join(root, "trusted-keys.json")
	createdAt := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	if _, err := releasebundle.GenerateKeyPair(privateKey, trustStore, createdAt.Add(-time.Minute)); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	if err := os.Chmod(privateKey, 0o644); err != nil {
		t.Fatal(err)
	}
	options := releasebundle.PackOptions{
		PayloadDir: payload, ConfigPath: configPath, SigningKeyPath: privateKey,
		OutputPath: filepath.Join(root, "bad-mode.tar.gz"), Commit: "cccccccccccccccccccccccccccccccccccccccc",
		SourceDateEpoch: createdAt,
		BuildToolchain:  releasebundle.BuildToolchain{GoVersion: "go1.25.11", NodeVersion: "v22.23.1", NPMVersion: "10.9.8"},
	}
	if _, err := releasebundle.Pack(options); err == nil {
		t.Fatal("expected permissive private key rejection")
	}
	if err := os.Chmod(privateKey, 0o600); err != nil {
		t.Fatal(err)
	}
	privateLink := filepath.Join(root, "private-link.json")
	if err := os.Symlink(privateKey, privateLink); err != nil {
		t.Fatal(err)
	}
	options.SigningKeyPath = privateLink
	options.OutputPath = filepath.Join(root, "linked-private-key.tar.gz")
	if _, err := releasebundle.Pack(options); err == nil {
		t.Fatal("expected symbolic-link private key rejection")
	}
	options.SigningKeyPath = privateKey
	if err := os.Symlink(filepath.Join(payload, "bin", "xinsightctl"), filepath.Join(payload, "bin", "linked-ctl")); err != nil {
		t.Fatal(err)
	}
	options.OutputPath = filepath.Join(root, "symlink.tar.gz")
	if _, err := releasebundle.Pack(options); err == nil {
		t.Fatal("expected payload symlink rejection")
	}
}

func writeProductPayload(t *testing.T, root string) {
	t.Helper()
	for path, contents := range map[string]string{
		"bin/xinsight-server":                          "server\n",
		"bin/xinsight-importer":                        "importer\n",
		"bin/xinsightctl":                              "ctl\n",
		"bin/xinsight-launcher":                        "launcher\n",
		"migrations/000001_create_test.up.sql":         "CREATE TABLE release_test (id bigint PRIMARY KEY);\n",
		"schemas/test.json":                            "{}\n",
		"schemas/access/local-accounts-v1.schema.json": "{}\n",
		"schemas/integration/feed-credential-registration-v1.schema.json":       "{}\n",
		"schemas/integration/connector-checkpoint-epoch-prepare-v1.schema.json": "{}\n",
		"fixtures/test.json":                           "{}\n",
		"manual/zh-CN/index.html":                      "<!doctype html><html lang=\"zh-CN\"></html>\n",
		"manual/zh-CN/manual.css":                      "body { color: #123; }\n",
		"manual/zh-CN/manual-icon.svg":                 "<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>\n",
		"release/compatibility-matrix.json":            "{}\n",
		"release/release-notes.md":                     "# Release notes\n",
		"release/schema-contract.json":                 "{\"schema_version\":\"xinsight.schema-contract.v2\",\"database_schema\":\"1\",\"postgres_major\":16,\"migration_count\":1,\"migration_set_sha256\":\"sha256:ff863083e02fd886d9cb26b3f0f6bb348c89a623a7e8cf743dd1e135042112e2\",\"semantic_sha256\":\"sha256:2b7556417ee2d2c7b3c36919db9d95db7dd4ae4418b7eee592deecf71535317d\",\"column_layout_sha256\":\"sha256:3333333333333333333333333333333333333333333333333333333333333333\",\"objects\":{\"namespaces\":1,\"tables\":0,\"columns\":0,\"constraints\":0,\"indexes\":0,\"triggers\":0,\"functions\":0,\"extensions\":0,\"types\":0,\"relations\":0},\"entries\":[{\"kind\":\"N\",\"namespace\":\"public\",\"name\":\"public\",\"semantic_sha256\":\"sha256:2222222222222222222222222222222222222222222222222222222222222222\"}]}\n",
		"deploy/xinsight.service":                      "[Service]\n",
		"deploy/xinsight-connector-controller.service": "[Service]\nExecStart=/opt/x-insight/current/bin/xinsightctl connector-controller serve\n",
		"deploy/xinsight-feed.service":                 "[Service]\nExecStart=/opt/x-insight/current/bin/xinsight-importer unirouter pull-feed\n",
		"deploy/xinsight-litellm-feed.service":         "[Service]\nExecStart=/opt/x-insight/current/bin/xinsight-importer litellm pull-spend-logs\n",
		"deploy/99-xinsight-managed-launcher.conf":     "[Service]\nExecStart=\nExecStart=/opt/x-insight/control/xinsight-launcher\n",
		"deploy/xinsight.env.example":                  "XINSIGHT_MIGRATIONS_DIR=/opt/x-insight/current/migrations\n",
		"deploy/xinsight-connector.env.example":        "XINSIGHT_UNIROUTER_INSTALL_ORIGIN=https://REPLACE_WITH_APPROVED_UNIROUTER_ORIGIN\n",
		"deploy/trusted-tls-proxy.env.example":         "XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback\nXINSIGHT_EXTERNAL_ORIGIN=https://REPLACE_WITH_APPROVED_HOST_OR_IP\n",
		"deploy/xinsight-nginx-ip.conf.example":        "server_name REPLACE_WITH_PUBLIC_IP;\nproxy_pass http://127.0.0.1:8080;\n",
		"deploy/xinsight-feed.env.example":             "XINSIGHT_UNIROUTER_FEED_ENDPOINT=https://private.example.invalid/api/v1/integrations/xinsight/completed-executions\nXINSIGHT_UNIROUTER_FEED_POLL_INTERVAL=5s\nXINSIGHT_UNIROUTER_FEED_ALLOW_PRIVATE_TUNNEL_HTTP=false\nXINSIGHT_UNIROUTER_FEED_MAPPING_MODE=legacy\n",
		"deploy/xinsight-litellm-feed.env.example":     "XINSIGHT_LITELLM_FEED_ENDPOINT=https://private.example.invalid/spend/logs/v2\nXINSIGHT_LITELLM_FEED_POLL_INTERVAL=30s\nXINSIGHT_LITELLM_FEED_SOURCE_INSTANCE=litellm-development\nXINSIGHT_LITELLM_FEED_TENANT_REF=\nXINSIGHT_LITELLM_FEED_ORG_REF=1\nXINSIGHT_LITELLM_FEED_PROJECT_REF=2\n",
		"deploy/postgres.compose.yml":                  "services: {}\n",
		"deploy/postgres.env.example":                  "POSTGRES_PASSWORD=REPLACE_WITH_FIELD_SECRET\n",
	} {
		writeFile(t, filepath.Join(root, filepath.FromSlash(path)), []byte(contents), 0o644)
	}
}

func writeReleaseConfig(t *testing.T, path string) {
	t.Helper()
	writeReleaseConfigVersion(t, path, "0.2.0-rc.1", "0.2.0-rc.1/manual-r1")
}

func writeReleaseConfigVersion(t *testing.T, path, productVersion, manualVersion string) {
	t.Helper()
	writeJSON(t, path, releasebundle.ReleaseConfig{
		SchemaVersion:  releasebundle.ConfigSchemaVersion,
		ProductName:    "x-insight",
		ProductVersion: productVersion,
		ReleaseChannel: "release_candidate",
		TrustReference: releasebundle.DefaultTrustReference,
		ApplicationCompatibility: releasebundle.ApplicationCompatibility{
			DatabaseSchemaMin: "1", DatabaseSchemaMax: "1",
			ConfigSchemaMin: "xinsight.config.v1", ConfigSchemaMax: "xinsight.config.v1",
		},
		DatabaseSchema:         releasebundle.SchemaCompatibility{Current: "1", Min: "1", Max: "1"},
		ConfigSchema:           releasebundle.SchemaCompatibility{Current: "xinsight.config.v1", Min: "xinsight.config.v1", Max: "xinsight.config.v1"},
		APIVersions:            []string{"xinsight.investigation.v1"},
		EvidenceSchemaVersions: []string{"m0.1"},
		ReceiptSchemaVersions:  []string{"xinsight.import-receipt.v1"},
		Manual:                 releasebundle.ManualIdentity{Language: "zh-CN", Version: manualVersion},
		Rollback:               releasebundle.RollbackPolicy{ApplicationRollbackCompatible: true, Barriers: []string{"no_contract_migration"}},
		SchemaContract: releasebundle.SchemaContractIdentity{
			SchemaVersion:      "xinsight.schema-contract.v2",
			SemanticSHA256:     "sha256:2b7556417ee2d2c7b3c36919db9d95db7dd4ae4418b7eee592deecf71535317d",
			MigrationSetSHA256: "sha256:ff863083e02fd886d9cb26b3f0f6bb348c89a623a7e8cf743dd1e135042112e2",
		},
	}, 0o644)
}

func persistentTestDir(t *testing.T) string {
	t.Helper()
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(os.TempDir(), "xinsight-releasebundle-test-"+hex.EncodeToString(random))
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFile(t *testing.T, path string, contents []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, contents, mode); err != nil {
		t.Fatal(err)
	}
}

func writeJSON(t *testing.T, path string, value any, mode os.FileMode) {
	t.Helper()
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, append(encoded, '\n'), mode)
}

func readJSON(t *testing.T, path string, value any) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(contents, value); err != nil {
		t.Fatal(err)
	}
}
