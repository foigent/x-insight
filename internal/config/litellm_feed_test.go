package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseLiteLLMFeedRuntimeConfigAcceptsExactPrivateHTTPSBinding(t *testing.T) {
	config, err := ParseLiteLLMFeedRuntimeConfig(
		"https://litellm.internal.example/spend/logs/v2",
		"30s",
		"litellm-development",
		"tenant-a",
		"1",
		"2",
	)
	if err != nil {
		t.Fatalf("parse LiteLLM feed config: %v", err)
	}
	if config.Endpoint != "https://litellm.internal.example/spend/logs/v2" ||
		config.PollInterval != 30*time.Second ||
		config.SourceInstance != "litellm-development" ||
		config.TenantRef == nil ||
		*config.TenantRef != "tenant-a" ||
		config.OrgRef != "1" ||
		config.ProjectRef != "2" {
		t.Fatalf("LiteLLM config=%+v", config)
	}
}

func TestParseLiteLLMFeedRuntimeConfigRejectsBoundaryDrift(t *testing.T) {
	for name, values := range map[string][6]string{
		"http": {
			"http://litellm.internal.example/spend/logs/v2",
			"30s", "litellm-development", "", "1", "2",
		},
		"path": {
			"https://litellm.internal.example/spend/logs",
			"30s", "litellm-development", "", "1", "2",
		},
		"query": {
			"https://litellm.internal.example/spend/logs/v2?scope=all",
			"30s", "litellm-development", "", "1", "2",
		},
		"poll": {
			"https://litellm.internal.example/spend/logs/v2",
			"61s", "litellm-development", "", "1", "2",
		},
		"org-leading-zero": {
			"https://litellm.internal.example/spend/logs/v2",
			"30s", "litellm-development", "", "01", "2",
		},
		"invalid-source-utf8": {
			"https://litellm.internal.example/spend/logs/v2",
			"30s", string([]byte{0xff}), "", "1", "2",
		},
		"invalid-tenant-utf8": {
			"https://litellm.internal.example/spend/logs/v2",
			"30s", "litellm-development", string([]byte{0xff}), "1", "2",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseLiteLLMFeedRuntimeConfig(
				values[0],
				values[1],
				values[2],
				values[3],
				values[4],
				values[5],
			); err == nil {
				t.Fatal("invalid LiteLLM config was accepted")
			}
		})
	}
}

func TestLoadLiteLLMFeedAPIKeyUsesIsolatedRestrictedCredential(t *testing.T) {
	const secret = "litellm-secret-must-not-leak"
	directory := persistentTestDirectory(t)
	explicit := filepath.Join(directory, "litellm.key")
	if err := os.WriteFile(explicit, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatalf("write LiteLLM key: %v", err)
	}
	t.Setenv(LiteLLMFeedKeyFileEnv, explicit)
	t.Setenv(CredentialsDirEnv, "")
	value, err := LoadLiteLLMFeedAPIKey()
	if err != nil || value != secret {
		t.Fatalf("load LiteLLM key value_match=%t err=%v", value == secret, err)
	}

	credentialsDirectory := persistentTestDirectory(t)
	systemdCredential := filepath.Join(
		credentialsDirectory,
		LiteLLMFeedCredential,
	)
	if err := os.WriteFile(
		systemdCredential,
		[]byte(secret),
		0o600,
	); err != nil {
		t.Fatalf("write systemd LiteLLM key: %v", err)
	}
	t.Setenv(CredentialsDirEnv, credentialsDirectory)
	if _, err := LoadLiteLLMFeedAPIKey(); err == nil ||
		strings.Contains(err.Error(), secret) {
		t.Fatalf("ambiguous LiteLLM credential error=%v", err)
	}

	t.Setenv(CredentialsDirEnv, "")
	if err := os.Chmod(explicit, 0o644); err != nil {
		t.Fatalf("chmod LiteLLM key: %v", err)
	}
	if _, err := LoadLiteLLMFeedAPIKey(); err == nil ||
		strings.Contains(err.Error(), secret) {
		t.Fatalf("unsafe LiteLLM credential error=%v", err)
	}
}

func TestLoadLiteLLMFeedAPIKeyRejectsHardLinkedCredential(t *testing.T) {
	const secret = "litellm-hard-link-secret"
	directory := persistentTestDirectory(t)
	credential := filepath.Join(directory, "litellm.key")
	hardLink := filepath.Join(directory, "litellm-linked.key")
	if err := os.WriteFile(credential, []byte(secret), 0o600); err != nil {
		t.Fatalf("write LiteLLM key: %v", err)
	}
	if err := os.Link(credential, hardLink); err != nil {
		t.Fatalf("hard-link LiteLLM key: %v", err)
	}
	t.Setenv(LiteLLMFeedKeyFileEnv, credential)
	t.Setenv(CredentialsDirEnv, "")
	if _, err := LoadLiteLLMFeedAPIKey(); err == nil ||
		strings.Contains(err.Error(), secret) {
		t.Fatalf("hard-linked LiteLLM credential error=%v", err)
	}
}
