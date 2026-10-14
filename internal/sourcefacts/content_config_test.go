package sourcefacts

import (
	"encoding/json"
	"strings"
	"testing"
)

func contentConfigFixture() ManagedConfig {
	return ManagedConfig{SchemaVersion: ManagedContentConfigVersion, PollIntervalSeconds: 1, RetryMaximumSeconds: 10, Profiles: []ManagedProfileConfig{}, Bindings: []ManagedCredentialBinding{{Name: "content-primary", Scope: "content", SourceSystemID: contentTestSourceSystem, SourceInstance: "synthetic-source", OrgID: "7", ProjectID: "9", CredentialID: "c-synthetic", RegistrationHash: Hash([]byte("registration")), KeyFile: "/etc/x-insight/content.key"}}, Contents: []ManagedContentConfig{{CollectionID: "11111111-2222-3333-4444-666666666666", SourceSystemID: contentTestSourceSystem, SourceInstance: "synthetic-source", OrgID: "7", ProjectID: "9", CredentialBinding: "content-primary", ContentScopeRevision: "scope-1", RecoveryEpoch: "1", AllowedPurposes: []string{ContentPurposeHumanReview}, CoverageFrom: ContentBoundary{contentTestTime, "0"}, Endpoint: "https://source.invalid" + ContentEndpointPath}}}
}
func TestContentConfigOnlyAndNamedBinding(t *testing.T) {
	c := contentConfigFixture()
	b, _ := json.Marshal(c)
	got, e := ParseManagedConfig(b)
	if e != nil {
		t.Fatal(e)
	}
	resolved, e := got.ResolveContent(got.Contents[0])
	if e != nil || resolved.KeyFile != c.Bindings[0].KeyFile || resolved.RegistrationHash != c.Bindings[0].RegistrationHash {
		t.Fatal("binding not retained", e)
	}
	if len(got.CredentialFiles()) != 1 || got.CredentialFiles()[0] != resolved.KeyFile {
		t.Fatal("credential map")
	}
	for name, change := range map[string]func(*ManagedConfig){"old_version": func(c *ManagedConfig) { c.SchemaVersion = ManagedObservationConfigVersion }, "wrong_scope": func(c *ManagedConfig) { c.Bindings[0].Scope = "legacy" }, "wrong_identity": func(c *ManagedConfig) { c.Bindings[0].ProjectID = "8" }, "unknown_purpose": func(c *ManagedConfig) { c.Contents[0].AllowedPurposes = []string{"admin"} }, "wrong_endpoint": func(c *ManagedConfig) { c.Contents[0].Endpoint = "https://source.invalid/api/payload" }, "duplicate_name": func(c *ManagedConfig) { c.Bindings = append(c.Bindings, c.Bindings[0]) }} {
		t.Run(name, func(t *testing.T) {
			c := contentConfigFixture()
			change(&c)
			b, _ := json.Marshal(c)
			if _, e := ParseManagedConfig(b); e == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}
func TestContentConfigLegacyWireUnchanged(t *testing.T) {
	for _, version := range []string{ManagedConfigVersion, ManagedObservationConfigVersion} {
		raw := `{"schema_version":"` + version + `","poll_interval_seconds":1,"retry_maximum_seconds":10,"profiles":[{"stream_id":"11111111-2222-3333-4444-666666666666","source_system_id":"` + contentTestSourceSystem + `","source_instance":"synthetic-source","org_id":"7","project_id":"9","profile":"unirouter.execution-context.v1","scope_generation":"1","endpoint":"https://source.invalid/api/v1/integrations/xinsight/source-facts","key_file":"/etc/x-insight/legacy.key","allow_private_tunnel_http":false}]}`
		c, e := ParseManagedConfig([]byte(raw))
		if e != nil {
			t.Fatal(e)
		}
		b, _ := json.Marshal(c)
		if string(b) != raw {
			t.Fatalf("legacy bytes changed: %s", b)
		}
	}
}
func TestContentConfigSameSourceIndependentCredentials(t *testing.T) {
	c := contentConfigFixture()
	legacy := c.Bindings[0]
	legacy.Name, legacy.Scope = "legacy", "legacy"
	c.Bindings = append(c.Bindings, legacy)
	if c.Validate() == nil {
		t.Fatal("same credential reused in distinct domains")
	}
	c.Bindings[1].CredentialID = "legacy-id"
	c.Bindings[1].RegistrationHash = Hash([]byte("legacy"))
	c.Bindings[1].KeyFile = "/etc/x-insight/legacy.key"
	if e := c.Validate(); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(c)
	if strings.Contains(string(raw), `"credential_id":""`) {
		t.Fatal("fabricated credential")
	}
}

func TestContentConfigPreservesLegacyV3Bytes(t *testing.T) {
	c := ManagedConfig{SchemaVersion: ManagedBudgetConfigVersion, PollIntervalSeconds: 1, RetryMaximumSeconds: 10, Profiles: []ManagedProfileConfig{}, Budgets: []ManagedBudgetConfig{{CollectionID: "11111111-2222-3333-4444-666666666666", SourceSystemID: contentTestSourceSystem, SourceInstance: "synthetic-source", OrgID: "7", ProjectID: "9", ScopeGeneration: "1", Profiles: []string{BudgetDefinitionProfile, BudgetSummaryProfile, BudgetAlertProfile}, Endpoint: "https://source.invalid" + BudgetEndpointPath, KeyFile: "/etc/x-insight/legacy.key"}}}
	before, _ := json.Marshal(c)
	got, e := ParseManagedConfig(before)
	if e != nil {
		t.Fatal(e)
	}
	after, _ := json.Marshal(got)
	if string(before) != string(after) {
		t.Fatal("legacy v3 bytes changed")
	}
}

func TestManagedProjectConfigPreservesOriginalPurposeWires(t *testing.T) {
	original := contentConfigFixture()
	profile := ManagedProfileConfig{StreamID: "11111111-2222-3333-4444-777777777777", SourceSystemID: contentTestSourceSystem, SourceInstance: "synthetic-source", OrgID: "7", ProjectID: "9", Profile: ContextProfile, ScopeGeneration: "1", Endpoint: "https://source.invalid" + ManagedEndpointPath(ContextProfile), KeyFile: "/etc/x-insight/legacy.key"}
	original.Profiles = []ManagedProfileConfig{profile}
	legacy := original.Bindings[0]
	legacy.Name = "facts-primary"
	legacy.Scope = "legacy"
	legacy.CredentialID = "facts-synthetic"
	legacy.RegistrationHash = Hash([]byte("facts-registration"))
	legacy.KeyFile = profile.KeyFile
	original.Bindings = append(original.Bindings, legacy)
	base := original
	foreign := profile
	foreign.StreamID = "11111111-2222-3333-4444-888888888888"
	foreign.ProjectID = "10"
	base.Profiles = append([]ManagedProfileConfig{foreign}, profile)
	for _, facts := range []bool{true, false} {
		for _, content := range []bool{true, false} {
			got := ManagedProjectConfig(base, "synthetic-source", "7", "9", facts, content)
			// The original qualifier's known per-purpose wire, including nil profiles
			// and the pre-existing no-C schema rule, is the independent expected value.
			want := ManagedConfig{SchemaVersion: ManagedObservationConfigVersion, PollIntervalSeconds: original.PollIntervalSeconds, RetryMaximumSeconds: original.RetryMaximumSeconds}
			if facts {
				want.Profiles = []ManagedProfileConfig{profile}
			}
			if content {
				want.SchemaVersion = original.SchemaVersion
				want.Bindings = original.Bindings
				want.Contents = original.Contents
			}
			wire, _ := json.Marshal(got)
			expected, _ := json.Marshal(want)
			if string(wire) != string(expected) {
				t.Fatalf("original purpose wire changed facts=%t content=%t", facts, content)
			}
			if facts || content {
				if got.Validate() != nil {
					t.Fatal("supported projection invalid")
				}
			} else if got.Validate() == nil {
				t.Fatal("empty purposes admitted")
			}
		}
	}
}
