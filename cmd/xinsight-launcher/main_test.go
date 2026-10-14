package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/foigent/x-insight/internal/config"
)

func TestLauncherReportsReleaseIdentityWithoutStartingServer(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"version", "--json"}, &output); err != nil {
		t.Fatalf("version: %v", err)
	}
	if !bytes.Contains(output.Bytes(), []byte(`"product_name": "x-insight"`)) {
		t.Fatalf("output=%s", output.String())
	}
}

func TestManagedReleaseKeepsCredentialDirectoryWithoutDSNEnvironment(t *testing.T) {
	inherited := []string{
		config.CredentialsDirEnv + "=/run/credentials/xinsight.service",
		config.ReaderDSNEnv + "=must-not-pass",
		config.WorkspaceDSNEnv + "=must-not-pass",
		config.PostgresDSNEnv + "=must-not-pass",
		"PATH=/usr/bin",
	}
	environment, err := launchEnvironment(true, inherited)
	if err != nil {
		t.Fatalf("managed environment: %v", err)
	}
	assertEnvironment(t, environment, config.CredentialsDirEnv, "/run/credentials/xinsight.service")
	assertEnvironmentAbsent(t, environment, config.ReaderDSNEnv)
	assertEnvironmentAbsent(t, environment, config.WorkspaceDSNEnv)
	assertEnvironmentAbsent(t, environment, config.PostgresDSNEnv)
	assertEnvironmentAbsent(t, environment, config.AllowLegacyRoleEnv)
}

func TestLegacyReleaseGetsOnlyExplicitCompatibilityDSN(t *testing.T) {
	dsn := "postgres://xinsight_reader:reader-password@127.0.0.1:5432/xinsight?sslmode=disable"
	t.Setenv(config.CredentialsDirEnv, "")
	t.Setenv(config.ReaderDSNEnv, dsn)
	t.Setenv(config.ReaderDSNFileEnv, "")
	t.Setenv(config.AllowLegacyRoleEnv, "")
	environment, err := launchEnvironment(false, []string{
		config.CredentialsDirEnv + "=/run/credentials/xinsight.service",
		config.ReaderDSNEnv + "=must-not-pass",
		config.WorkspaceDSNEnv + "=must-not-pass",
		"PATH=/usr/bin",
	})
	if err != nil {
		t.Fatalf("legacy environment: %v", err)
	}
	assertEnvironmentAbsent(t, environment, config.CredentialsDirEnv)
	assertEnvironmentAbsent(t, environment, config.ReaderDSNEnv)
	assertEnvironmentAbsent(t, environment, config.WorkspaceDSNEnv)
	assertEnvironment(t, environment, config.PostgresDSNEnv, dsn)
	assertEnvironment(t, environment, config.AllowLegacyRoleEnv, "1")
}

func TestManagedReleaseMarkerMustBeRegular(t *testing.T) {
	root := persistentLauncherTestDir(t)
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if managed, err := isManagedRelease(root); err != nil || managed {
		t.Fatalf("missing marker managed=%t err=%v", managed, err)
	}
	if err := os.Mkdir(filepath.Join(bin, "xinsightctl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := isManagedRelease(root); err == nil {
		t.Fatal("expected non-regular marker rejection")
	}
}

func assertEnvironment(t *testing.T, environment []string, key string, expected string) {
	t.Helper()
	for _, value := range environment {
		if hasEnvironmentKey(value, key) {
			if value != key+"="+expected {
				t.Fatalf("%s=%q expected=%q", key, value, expected)
			}
			return
		}
	}
	t.Fatalf("missing environment key %s", key)
}

func assertEnvironmentAbsent(t *testing.T, environment []string, key string) {
	t.Helper()
	for _, value := range environment {
		if hasEnvironmentKey(value, key) {
			t.Fatalf("unexpected environment key %s", key)
		}
	}
}

func persistentLauncherTestDir(t *testing.T) string {
	t.Helper()
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(os.TempDir(), "xinsight-launcher-test-"+hex.EncodeToString(random))
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
