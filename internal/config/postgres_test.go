package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validTestDSN = "postgres://xinsight:top-secret@127.0.0.1:5432/xinsight?sslmode=disable&connect_timeout=5"

func TestValidatePostgresDSNAcceptsExplicitLoopbackURLs(t *testing.T) {
	for _, value := range []string{
		validTestDSN,
		"postgresql://role:p%40ss@localhost/database?application_name=xinsight",
		"postgres://role:password@[::1]:5432/database?sslmode=require",
	} {
		if actual, err := ValidatePostgresDSN(value); err != nil || actual != value {
			t.Fatalf("valid DSN rejected: err=%v", err)
		}
	}
}

func TestValidatePostgresDSNRejectsBoundaryBypassesWithoutLeakingSecret(t *testing.T) {
	for name, value := range map[string]string{
		"remote":           "postgres://role:top-secret@10.0.0.2/database",
		"fallback":         "postgres://role:top-secret@127.0.0.1,10.0.0.2/database",
		"service":          "postgres://role:top-secret@127.0.0.1/database?service=outside",
		"passfile":         "postgres://role:top-secret@127.0.0.1/database?passfile=/tmp/outside",
		"missing_password": "postgres://role@127.0.0.1/database",
		"nested_database":  "postgres://role:top-secret@127.0.0.1/one/two",
		"control":          "postgres://role:top-secret@127.0.0.1/database\nother",
		"encoded_control":  "postgres://role:top%00secret@127.0.0.1/database",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ValidatePostgresDSN(value)
			if err == nil {
				t.Fatal("invalid DSN was accepted")
			}
			if strings.Contains(err.Error(), "top-secret") {
				t.Fatalf("secret leaked in error: %v", err)
			}
		})
	}
}

func TestLoadPostgresDSNReadsRestrictedAbsoluteFile(t *testing.T) {
	directory := persistentTestDirectory(t)
	path := filepath.Join(directory, "postgres.dsn")
	if err := os.WriteFile(path, []byte(validTestDSN+"\n"), 0o600); err != nil {
		t.Fatalf("write DSN file: %v", err)
	}
	t.Setenv(PostgresDSNEnv, "")
	t.Setenv(PostgresDSNFileEnv, path)
	t.Setenv(CredentialsDirEnv, "")

	actual, err := LoadPostgresDSN()
	if err != nil {
		t.Fatalf("load DSN: %v", err)
	}
	if actual != validTestDSN {
		t.Fatalf("unexpected DSN readback")
	}
}

func TestRoleSpecificDSNRequiresExplicitDevelopmentFallback(t *testing.T) {
	t.Setenv(ReaderDSNEnv, "")
	t.Setenv(ReaderDSNFileEnv, "")
	t.Setenv(WorkspaceDSNEnv, "")
	t.Setenv(WorkspaceDSNFileEnv, "")
	t.Setenv(CredentialsDirEnv, "")
	t.Setenv(PostgresDSNEnv, validTestDSN)
	t.Setenv(PostgresDSNFileEnv, "")
	t.Setenv(AllowLegacyRoleEnv, "")
	if _, err := LoadReaderPostgresDSN(); err == nil {
		t.Fatal("legacy single database role was accepted without opt-in")
	}
	if _, err := LoadWorkspacePostgresDSN(); err == nil {
		t.Fatal("legacy single database role was accepted for workspace without opt-in")
	}
	t.Setenv(AllowLegacyRoleEnv, "1")
	if _, err := LoadReaderPostgresDSN(); err != nil {
		t.Fatalf("explicit development fallback rejected: %v", err)
	}
	if _, err := LoadWorkspacePostgresDSN(); err != nil {
		t.Fatalf("explicit workspace development fallback rejected: %v", err)
	}
	t.Setenv(ReaderDSNEnv, "postgres://reader:password@127.0.0.1:5432/xinsight")
	if _, err := LoadReaderPostgresDSN(); err != nil {
		t.Fatalf("role-specific DSN rejected: %v", err)
	}
	t.Setenv(WorkspaceDSNEnv, "postgres://workspace:password@127.0.0.1:5432/xinsight")
	if _, err := LoadWorkspacePostgresDSN(); err != nil {
		t.Fatalf("workspace role-specific DSN rejected: %v", err)
	}
}

func TestLoadPostgresDSNRejectsAmbiguityAndUnsafeFiles(t *testing.T) {
	directory := persistentTestDirectory(t)
	path := filepath.Join(directory, "postgres.dsn")
	if err := os.WriteFile(path, []byte(validTestDSN), 0o644); err != nil {
		t.Fatalf("write DSN file: %v", err)
	}
	t.Setenv(PostgresDSNEnv, validTestDSN)
	t.Setenv(PostgresDSNFileEnv, path)
	t.Setenv(CredentialsDirEnv, "")
	if _, err := LoadPostgresDSN(); err == nil {
		t.Fatal("ambiguous DSN sources were accepted")
	}

	t.Setenv(PostgresDSNEnv, "")
	if _, err := LoadPostgresDSN(); err == nil {
		t.Fatal("world-readable DSN file was accepted")
	}

	target := filepath.Join(directory, "target.dsn")
	if err := os.WriteFile(target, []byte(validTestDSN), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(directory, "link.dsn")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	t.Setenv(PostgresDSNFileEnv, link)
	if _, err := LoadPostgresDSN(); err == nil {
		t.Fatal("symlink DSN file was accepted")
	}
}

func persistentTestDirectory(t *testing.T) string {
	t.Helper()
	path := filepath.Join(os.TempDir(), fmt.Sprintf("xinsight-config-test-%d-%d", os.Getpid(), time.Now().UnixNano()))
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("create persistent test directory: %v", err)
	}
	return path
}
