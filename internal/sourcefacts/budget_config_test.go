package sourcefacts

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestBudgetManagedConfigurationExplicitVersionAndCredential(t *testing.T) {
	p := ManagedBudgetConfig{CollectionID: "22222222-2222-4222-8222-222222222222", SourceSystemID: "11111111-1111-4111-8111-111111111111", SourceInstance: "ur", OrgID: "1", ProjectID: "2", Profiles: []string{BudgetDefinitionProfile, BudgetSummaryProfile}, ScopeGeneration: "1", Endpoint: "https://source.example" + BudgetEndpointPath, KeyFile: "/etc/x-insight/d-source.key"}
	c := ManagedConfig{SchemaVersion: ManagedBudgetConfigVersion, PollIntervalSeconds: 1, RetryMaximumSeconds: 30, Profiles: []ManagedProfileConfig{}, Budgets: []ManagedBudgetConfig{p}}
	raw, _ := json.Marshal(c)
	decoded, err := ParseManagedConfig(raw)
	if err != nil || !slices.Equal(decoded.CredentialFiles(), []string{p.KeyFile}) {
		t.Fatal("explicit D registration", err)
	}
	for _, version := range []string{ManagedConfigVersion, ManagedObservationConfigVersion} {
		copy := c
		copy.SchemaVersion = version
		raw, _ = json.Marshal(copy)
		if _, err := ParseManagedConfig(raw); err == nil {
			t.Fatal("old config acquired D permission")
		}
	}
	for _, mutate := range []func(*ManagedBudgetConfig){
		func(p *ManagedBudgetConfig) {
			p.Endpoint = "https://source.example/api/v1/integrations/xinsight/source/v2/events"
		},
		func(p *ManagedBudgetConfig) { p.Endpoint += "?profile=" + BudgetDefinitionProfile },
		func(p *ManagedBudgetConfig) { p.KeyFile = "/etc/x-insight/bad key" },
		func(p *ManagedBudgetConfig) { p.Profiles = append(p.Profiles, p.Profiles[0]) },
		func(p *ManagedBudgetConfig) { p.Profiles = []string{RuntimeProfile} },
	} {
		copy := p
		copy.Profiles = slices.Clone(p.Profiles)
		mutate(&copy)
		if copy.Validate() == nil {
			t.Fatal("invalid D configuration")
		}
	}
}
