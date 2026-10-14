package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadUniRouterFeedRuntimeConfig(t *testing.T) {
	t.Setenv(UniRouterFeedEndpointEnv, "https://unirouter.internal.example"+UniRouterCompletedExecutionsPath)
	t.Setenv(UniRouterFeedPollIntervalEnv, "")

	runtimeConfig, err := LoadUniRouterFeedRuntimeConfig()
	if err != nil {
		t.Fatalf("load feed runtime config: %v", err)
	}
	if runtimeConfig.Endpoint != "https://unirouter.internal.example"+UniRouterCompletedExecutionsPath ||
		runtimeConfig.PollInterval != DefaultUniRouterFeedPollInterval ||
		runtimeConfig.AllowPrivateTunnelHTTP ||
		runtimeConfig.MappingMode != UniRouterFeedMappingLegacy {
		t.Fatalf("runtime config=%+v", runtimeConfig)
	}

	t.Setenv(UniRouterFeedPollIntervalEnv, "9s")
	runtimeConfig, err = LoadUniRouterFeedRuntimeConfig()
	if err != nil || runtimeConfig.PollInterval != 9*time.Second {
		t.Fatalf("custom poll interval config=%+v err=%v", runtimeConfig, err)
	}

	t.Setenv(UniRouterFeedAllowHTTPEnv, "true")
	runtimeConfig, err = LoadUniRouterFeedRuntimeConfig()
	if err != nil || !runtimeConfig.AllowPrivateTunnelHTTP {
		t.Fatalf("private tunnel HTTP config=%+v err=%v", runtimeConfig, err)
	}

	for _, mode := range []string{
		UniRouterFeedMappingLegacy,
		UniRouterFeedMappingShadow,
		UniRouterFeedMappingNeutralCanary,
	} {
		t.Setenv(UniRouterFeedMappingModeEnv, mode)
		runtimeConfig, err = LoadUniRouterFeedRuntimeConfig()
		if err != nil || runtimeConfig.MappingMode != mode {
			t.Fatalf("mapping mode %q config=%+v err=%v", mode, runtimeConfig, err)
		}
	}
}

func TestLoadUniRouterFeedRuntimeConfigRejectsUnsafeBounds(t *testing.T) {
	t.Setenv(UniRouterFeedEndpointEnv, " https://private.example"+UniRouterCompletedExecutionsPath)
	if _, err := LoadUniRouterFeedRuntimeConfig(); err == nil {
		t.Fatal("endpoint whitespace was accepted")
	}

	t.Setenv(UniRouterFeedEndpointEnv, "https://private.example"+UniRouterCompletedExecutionsPath)
	for _, value := range []string{"not-a-duration", "999ms", "61s"} {
		t.Setenv(UniRouterFeedPollIntervalEnv, value)
		if _, err := LoadUniRouterFeedRuntimeConfig(); err == nil {
			t.Fatalf("poll interval %q was accepted", value)
		}
	}

	t.Setenv(UniRouterFeedPollIntervalEnv, "")
	for _, value := range []string{"TRUE", "1", " true", "false "} {
		t.Setenv(UniRouterFeedAllowHTTPEnv, value)
		if _, err := LoadUniRouterFeedRuntimeConfig(); err == nil {
			t.Fatalf("private tunnel HTTP value %q was accepted", value)
		}
	}

	t.Setenv(UniRouterFeedAllowHTTPEnv, "false")
	for _, value := range []string{"Legacy", " shadow", "canary", "neutral_canary "} {
		t.Setenv(UniRouterFeedMappingModeEnv, value)
		if _, err := LoadUniRouterFeedRuntimeConfig(); err == nil {
			t.Fatalf("mapping mode %q was accepted", value)
		}
	}
}

func TestLoadUniRouterFeedRuntimeConfigRejectsRouteDrift(t *testing.T) {
	for _, endpoint := range []string{
		"https://private.example/internal/v1/completed-executions",
		"https://private.example/api/v1/integrations/xinsight/completed-executions/",
		"https://private.example/api/v1/integrations/other/completed-executions",
	} {
		t.Setenv(UniRouterFeedEndpointEnv, endpoint)
		if _, err := LoadUniRouterFeedRuntimeConfig(); err == nil {
			t.Fatalf("drifted completed-execution endpoint %q was accepted", endpoint)
		}
	}
}

func TestLoadUniRouterFeedServiceKeyReadsRestrictedFile(t *testing.T) {
	const secret = "feed-service-key"
	directory := persistentTestDirectory(t)
	path := filepath.Join(directory, "feed.key")
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatalf("write feed key: %v", err)
	}
	t.Setenv(UniRouterFeedKeyFileEnv, path)
	t.Setenv(CredentialsDirEnv, "")

	value, err := LoadUniRouterFeedServiceKey()
	if err != nil {
		t.Fatalf("load feed key: %v", err)
	}
	if value != secret {
		t.Fatal("unexpected feed key")
	}
}

func TestLoadUniRouterFeedServiceKeyRejectsAmbiguityAndUnsafeFilesWithoutLeaks(t *testing.T) {
	const secret = "feed-secret-must-not-leak"
	directory := persistentTestDirectory(t)
	explicit := filepath.Join(directory, "explicit.key")
	if err := os.WriteFile(explicit, []byte(secret), 0o600); err != nil {
		t.Fatalf("write explicit key: %v", err)
	}
	credentialsDirectory := persistentTestDirectory(t)
	systemdCredential := filepath.Join(credentialsDirectory, UniRouterFeedCredential)
	if err := os.WriteFile(systemdCredential, []byte(secret), 0o600); err != nil {
		t.Fatalf("write systemd credential: %v", err)
	}
	t.Setenv(UniRouterFeedKeyFileEnv, explicit)
	t.Setenv(CredentialsDirEnv, credentialsDirectory)
	if _, err := LoadUniRouterFeedServiceKey(); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("ambiguous credential error=%v", err)
	}

	t.Setenv(CredentialsDirEnv, "")
	if err := os.Chmod(explicit, 0o644); err != nil {
		t.Fatalf("chmod explicit key: %v", err)
	}
	if _, err := LoadUniRouterFeedServiceKey(); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("unsafe credential error=%v", err)
	}

	target := filepath.Join(directory, "target.key")
	if err := os.WriteFile(target, []byte(secret), 0o600); err != nil {
		t.Fatalf("write target key: %v", err)
	}
	link := filepath.Join(directory, "link.key")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create key symlink: %v", err)
	}
	t.Setenv(UniRouterFeedKeyFileEnv, link)
	if _, err := LoadUniRouterFeedServiceKey(); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("symlink credential error=%v", err)
	}
}
