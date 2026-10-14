package access

import (
	"bytes"
	"testing"
)

func TestOIDCConfigurationIsProviderNeutralStrictAndDeterministic(t *testing.T) {
	payload := []byte(`{
	  "schema_version":"xinsight.oidc-provider.v1",
	  "provider_key":"enterprise_oidc",
	  "issuer":"https://identity.example.test/tenant",
	  "client_id":"xinsight",
	  "redirect_path":"/api/v1/access/oidc/callback",
	  "scopes":["openid","profile"],
	  "subject_claim":"sub",
	  "role_claim":"groups",
	  "role_mapping":{"security":"platform_admin","analysts":"investigator"},
	  "pkce_required":true,
	  "authorization_code_only":true,
	  "enabled":false
	}`)
	config, err := ParseOIDCConfig(payload)
	if err != nil {
		t.Fatalf("parse OIDC config: %v", err)
	}
	first, err := CanonicalOIDCConfig(config)
	if err != nil {
		t.Fatalf("canonicalize OIDC config: %v", err)
	}
	second, err := CanonicalOIDCConfig(config)
	if err != nil {
		t.Fatalf("canonicalize OIDC config again: %v", err)
	}
	reordered := config
	reordered.Scopes = []string{"profile", "openid"}
	reorderedBytes, err := CanonicalOIDCConfig(reordered)
	if err != nil {
		t.Fatalf("canonicalize reordered OIDC scopes: %v", err)
	}
	if !bytes.Equal(first, second) ||
		!bytes.Equal(first, reorderedBytes) ||
		!bytes.Contains(first, []byte(`"external":"analysts"`)) {
		t.Fatalf("OIDC config is not deterministic: %s", first)
	}
}

func TestOIDCConfigurationFailsClosed(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"schema_version":"xinsight.oidc-provider.v1","schema_version":"xinsight.oidc-provider.v1"}`),
		[]byte(`{"schema_version":"xinsight.oidc-provider.v1","provider_key":"enterprise_oidc","issuer":"http://identity.example.test","client_id":"x","redirect_path":"/api/v1/access/oidc/callback","scopes":["openid"],"subject_claim":"sub","pkce_required":true,"authorization_code_only":true,"enabled":false}`),
		[]byte(`{"schema_version":"xinsight.oidc-provider.v1","provider_key":"enterprise_oidc","issuer":"https://identity.example.test","client_id":"x","redirect_path":"/api/v1/access/oidc/callback","scopes":["profile"],"subject_claim":"sub","pkce_required":true,"authorization_code_only":true,"enabled":false}`),
		[]byte(`{"schema_version":"xinsight.oidc-provider.v1","provider_key":"enterprise_oidc","issuer":"https://identity.example.test","client_id":"x","redirect_path":"/api/v1/access/oidc/callback","scopes":["openid"],"subject_claim":"sub","role_mapping":{"analysts":"investigator"},"pkce_required":true,"authorization_code_only":true,"enabled":false}`),
	}
	for index, payload := range cases {
		if _, err := ParseOIDCConfig(payload); err == nil {
			t.Fatalf("case %d unexpectedly accepted", index)
		}
	}
}
