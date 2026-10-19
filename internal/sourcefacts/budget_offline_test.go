package sourcefacts

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
)

func budgetOfflineFixture(t *testing.T) (BudgetOfflineAuthorization, []byte, time.Time) {
	t.Helper()
	positive, _ := budgetGolden(t)
	r, err := DecodeBudgetReading(positive[0])
	if err != nil {
		t.Fatal(err)
	}
	c := BudgetCatalog{SchemaVersion: BudgetCatalogVersion, SourceInstance: r.SourceInstance, OrgID: r.OrgID, ProjectID: r.ProjectID, ReadingCutoff: r.ReadingCutoff, AccessGeneration: r.AccessGeneration, CatalogRevision: r.CatalogRevision, Profiles: r.Profiles(), EnterpriseScope: "scope_not_configured", Entries: []BudgetCatalogEntry{{BudgetRef: r.BudgetRef, Origin: "gateway_project", DefinitionRevision: r.DefinitionRevision, BindingRevision: r.BindingRevision}}}
	b := BudgetOfflineBundle{SchemaVersion: BudgetOfflineBundleVersion, Catalog: budgetRaw(t, c), Readings: []BudgetOfflineEntry{{Reading: positive[0]}}}
	raw := budgetRaw(t, b)
	now := time.Now().UTC()
	a := BudgetOfflineAuthorization{SchemaVersion: BudgetOfflineAuthorizationVersion, CollectionID: "10000000-0000-4000-8000-000000000001", SourceSystemID: "10000000-0000-4000-8000-000000000002", Environment: "test", TargetRef: "xi-budget-synthetic", MachineIDSHA256: Hash([]byte("machine")), ProductVersion: "dev-d", ProductCommit: strings.Repeat("a", 40), TargetDatabaseSchema: "36", SourceCommit: BudgetSourceCommit, SourceManifestSHA256: BudgetManifestSHA256(), SourceInstance: r.SourceInstance, OrgID: r.OrgID, ProjectID: r.ProjectID, ScopeGeneration: "1", Profiles: r.Profiles(), NotBefore: now.Add(-time.Minute).Format(TimeFormat), ExpiresAt: now.Add(time.Hour).Format(TimeFormat), BundlePath: "/private-budget-fixtures/bundle.json", BundleSHA256: Hash(raw)}
	return a, raw, now
}
func TestBudgetOfflineExactBindingAndCompleteness(t *testing.T) {
	a, raw, now := budgetOfflineFixture(t)
	for _, schema := range append([]string{"36", "37", "38", "39", "40", "41", "42", "43", "44", "45"}, buildinfo.DatabaseSchemaMax) {
		v := a
		v.TargetDatabaseSchema = schema
		if _, err := ParseBudgetOfflineAuthorization(budgetRaw(t, v), now); err != nil {
			t.Fatalf("supported budget schema %s: %v", schema, err)
		}
	}
	if _, err := ValidateBudgetOfflineBundle(raw, a, now); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseBudgetOfflineAuthorization(budgetRaw(t, a), now); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*BudgetOfflineAuthorization){func(x *BudgetOfflineAuthorization) { x.BundleSHA256 = Hash([]byte("other")) }, func(x *BudgetOfflineAuthorization) { x.SourceCommit = strings.Repeat("b", 40) }, func(x *BudgetOfflineAuthorization) { x.TargetDatabaseSchema = "35" }, func(x *BudgetOfflineAuthorization) { x.TargetDatabaseSchema = "47" }, func(x *BudgetOfflineAuthorization) { x.Environment = "production" }, func(x *BudgetOfflineAuthorization) { x.Profiles = []string{BudgetSummaryProfile} }, func(x *BudgetOfflineAuthorization) { x.ExpiresAt = now.Add(-time.Second).Format(TimeFormat) }} {
		x := a
		mutate(&x)
		if _, err := ValidateBudgetOfflineBundle(raw, x, now); err == nil {
			t.Fatal("changed authority accepted")
		}
	}
	for _, mutate := range []func(*BudgetOfflineBundle){func(b *BudgetOfflineBundle) { b.Readings = nil }, func(b *BudgetOfflineBundle) { b.Readings = append(b.Readings, b.Readings[0]) }, func(b *BudgetOfflineBundle) { b.Readings[0].AlertsInputCursor = "unrequested-page" }} {
		var b BudgetOfflineBundle
		if err := json.Unmarshal(raw, &b); err != nil {
			t.Fatal(err)
		}
		mutate(&b)
		changed := budgetRaw(t, b)
		x := a
		x.BundleSHA256 = Hash(changed)
		if _, err := ValidateBudgetOfflineBundle(changed, x, now); err == nil {
			t.Fatal("incomplete, duplicate or mismatched position accepted")
		}
	}
	for _, bad := range []string{strings.Replace(string(budgetRaw(t, a)), `"environment":"test"`, `"environment":"test","environment":"test"`, 1), strings.Replace(string(budgetRaw(t, a)), `"scope_generation":"1"`, `"scope_generation":null`, 1)} {
		if _, err := ParseBudgetOfflineAuthorization([]byte(bad), now); err == nil {
			t.Fatal("ambiguous authorization accepted")
		}
	}
	if _, err := PreflightBudget(t.Context(), nil, a.Collection()); err == nil {
		t.Fatal("offline collection has a network preflight")
	}
}
