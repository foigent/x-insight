package sourcefacts

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/connector"
)

func budgetGolden(t *testing.T) (positive []json.RawMessage, negative []struct {
	Name  string
	Value json.RawMessage
}) {
	t.Helper()
	b, err := budgetAssets.ReadFile("contract/budgets/golden-readings.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Positive []json.RawMessage
		Negative []struct {
			Name  string
			Value json.RawMessage
		}
	}
	if err = json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v.Positive, v.Negative
}

func budgetRaw(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func budgetMutation(t *testing.T, raw []byte, change func(map[string]any)) []byte {
	t.Helper()
	m, err := connector.DecodeStrictJSONObject(raw)
	if err != nil {
		t.Fatal(err)
	}
	change(m)
	for _, rawDoc := range m["documents"].([]any) {
		w := rawDoc.(map[string]any)
		b, err := connector.CanonicalizeJSONValue(w["document"])
		if err != nil {
			t.Fatal(err)
		}
		w["source_sha256"] = Hash(b)
	}
	return budgetRaw(t, m)
}

func TestBudgetFrozenContract(t *testing.T) {
	b, err := budgetAssets.ReadFile("contract/budgets/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct{ Files map[string]string }
	if err = json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	for name, want := range manifest.Files {
		raw, err := budgetAssets.ReadFile("contract/budgets/" + name)
		if err != nil || Hash(raw) != want {
			t.Fatalf("frozen asset changed: %s", name)
		}
	}
	positive, negative := budgetGolden(t)
	if len(positive) != 5 || len(negative) != 8 {
		t.Fatal("frozen vector count")
	}
	for i, raw := range positive {
		t.Run(fmt.Sprintf("positive_%d", i), func(t *testing.T) {
			r, err := DecodeBudgetReading(raw)
			if err != nil {
				t.Fatal(err)
			}
			again, err := DecodeBudgetReading(r.Canonical)
			if err != nil || r.SHA256 != again.SHA256 {
				t.Fatal("canonical replay", err)
			}
		})
	}
	for _, v := range negative {
		t.Run(v.Name, func(t *testing.T) {
			if _, err := DecodeBudgetReading(v.Value); err == nil {
				t.Fatal("source negative accepted")
			}
		})
	}
}

func TestBudgetAuthorizedSubsets(t *testing.T) {
	positive, _ := budgetGolden(t)
	all := []string{BudgetDefinitionProfile, BudgetSummaryProfile, BudgetAlertProfile}
	for mask := 1; mask < 8; mask++ {
		t.Run(fmt.Sprint(mask), func(t *testing.T) {
			raw := budgetMutation(t, positive[0], func(m map[string]any) {
				docs := m["documents"].([]any)
				selected := []any{}
				for i, doc := range docs {
					if mask&(1<<i) != 0 {
						selected = append(selected, doc)
					}
				}
				m["documents"] = selected
			})
			r, err := DecodeBudgetReading(raw)
			if err != nil {
				t.Fatal(err)
			}
			e := BudgetCatalogEntry{BudgetRef: r.BudgetRef, Origin: "gateway_project", DefinitionRevision: r.DefinitionRevision}
			c := BudgetCatalog{SourceInstance: r.SourceInstance, OrgID: r.OrgID, ProjectID: r.ProjectID, AccessGeneration: r.AccessGeneration, CatalogRevision: r.CatalogRevision, Profiles: r.Profiles(), Entries: []BudgetCatalogEntry{e}}
			if err = MatchBudgetReading(r, c, e, all); err != nil {
				t.Fatal("legal grant subset", err)
			}
			// The exact expected set is catalog-bound, never inferred from this payload.
			c.Profiles = all
			if err = MatchBudgetReading(r, c, e, all); (mask == 7) != (err == nil) {
				t.Fatal("missing expected document", err)
			}
			if mask != 1 && MatchBudgetReading(r, c, e, []string{BudgetDefinitionProfile}) == nil {
				t.Fatal("extra source grant widened config")
			}
			c.Profiles = r.Profiles()
			c.AccessGeneration = "2"
			if MatchBudgetReading(r, c, e, all) == nil {
				t.Fatal("mixed access generation")
			}
		})
	}
}

func TestBudgetReadingSemanticBoundaries(t *testing.T) {
	positive, _ := budgetGolden(t)
	mutations := map[string]func(map[string]any){
		"mixed_cutoff":  func(m map[string]any) { m["reading_cutoff"] = "2026-10-03T00:00:00Z" },
		"mixed_binding": func(m map[string]any) { m["binding_revision"] = "7" },
		"invalid_time":  func(m map[string]any) { m["observed_at"] = "2026-02-30T00:00:00Z" },
		"overflow_doc": func(m map[string]any) {
			body := m["documents"].([]any)[0].(map[string]any)["document"].(map[string]any)["body"].(map[string]any)
			body["authority_ref"] = strings.Repeat("x", MaxDocumentBytes)
		},
		"same_reading_wrong_definition": func(m map[string]any) {
			body := m["documents"].([]any)[0].(map[string]any)["document"].(map[string]any)["body"].(map[string]any)
			body["amount"].(map[string]any)["value_minor"] = "1500000000"
		},
		"source_units_disagree": func(m map[string]any) {
			body := m["documents"].([]any)[1].(map[string]any)["document"].(map[string]any)["body"].(map[string]any)
			body["comparison"].(map[string]any)["remaining"].(map[string]any)["currency"] = "USD"
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeBudgetReading(budgetMutation(t, positive[0], mutate)); err == nil {
				t.Fatal("invalid reading accepted")
			}
		})
	}
	// Numeric range is the original 78-digit decimal domain, not int64/float64.
	raw := budgetMutation(t, positive[0], func(m map[string]any) {
		m["documents"] = m["documents"].([]any)[:1]
		body := m["documents"].([]any)[0].(map[string]any)["document"].(map[string]any)["body"].(map[string]any)
		body["amount"].(map[string]any)["value_minor"] = strings.Repeat("9", 78)
		body["amount"].(map[string]any)["scale"] = "9999999999999999999"
	})
	if _, err := DecodeBudgetReading(raw); err != nil {
		t.Fatal("legal exact numeric domain rejected", err)
	}
	dup := strings.Replace(string(positive[0]), `"access_generation": "1"`, `"access_generation": "1", "access_generation": "1"`, 1)
	if dup == string(positive[0]) {
		t.Fatal("duplicate-key fixture ineffective")
	}
	if _, err := DecodeBudgetReading([]byte(dup)); err == nil {
		t.Fatal("duplicate JSON key accepted")
	}
}

func TestBudgetCatalogBoundaries(t *testing.T) {
	c := BudgetCatalog{SchemaVersion: BudgetCatalogVersion, SourceInstance: "source", OrgID: "1", ProjectID: "2", ReadingCutoff: "2026-09-13T00:00:00Z", AccessGeneration: "1", CatalogRevision: "1", Profiles: []string{BudgetSummaryProfile}, EnterpriseScope: "scope_not_configured", Entries: []BudgetCatalogEntry{}}
	for i := 0; i < 50; i++ {
		c.Entries = append(c.Entries, BudgetCatalogEntry{BudgetRef: fmt.Sprintf("gateway-project:%03d", i), Origin: "gateway_project", DefinitionRevision: "1"})
	}
	c.HasMore = true
	c.NextCursor = strings.Repeat("c", 4096)
	if _, err := DecodeBudgetCatalog(budgetRaw(t, c)); err != nil {
		t.Fatal("50-entry 4096 cursor", err)
	}
	c.NextCursor += "c"
	if _, err := DecodeBudgetCatalog(budgetRaw(t, c)); err == nil {
		t.Fatal("oversized cursor")
	}
	c.NextCursor = "next"
	c.Entries = append(c.Entries, c.Entries[0])
	if _, err := DecodeBudgetCatalog(budgetRaw(t, c)); err == nil {
		t.Fatal("51 entries")
	}
	c.Entries = c.Entries[:50]
	c.Entries[1] = c.Entries[0]
	if _, err := DecodeBudgetCatalog(budgetRaw(t, c)); err == nil {
		t.Fatal("duplicate catalog entry")
	}
	c.Entries = c.Entries[:1]
	c.NextCursor = ""
	if _, err := DecodeBudgetCatalog(budgetRaw(t, c)); err == nil {
		t.Fatal("has_more without cursor")
	}
}

// The directory, not the optional definition grant, binds the financial origin.
func TestBudgetCatalogAuthorityAllAuthorizedSubsets(t *testing.T) {
	positive, _ := budgetGolden(t)
	all := []string{BudgetDefinitionProfile, BudgetSummaryProfile, BudgetAlertProfile}
	for _, origin := range []string{"gateway_project", "enterprise_project"} {
		for mask := 1; mask < 8; mask++ {
			t.Run(fmt.Sprintf("%s/%d", origin, mask), func(t *testing.T) {
				raw := budgetMutation(t, positive[0], func(m map[string]any) {
					selected := []any{}
					for i, item := range m["documents"].([]any) {
						if mask&(1<<i) == 0 {
							continue
						}
						w := item.(map[string]any)
						b := w["document"].(map[string]any)["body"].(map[string]any)
						if origin == "enterprise_project" {
							switch Text(w, "profile") {
							case BudgetDefinitionProfile:
								b["origin"] = origin
								b["period_kind"] = "fiscal_period"
								b["authority_ref"] = "synthetic-enterprise-budget:2"
								b["fiscal_period_ref"] = "synthetic-2026-09"
							case BudgetSummaryProfile:
								b["authority"] = "enterprise_finance_allocation_results.project_slice"
								b["period_state"] = "closed"
								b["allocation_run_ref"] = "synthetic-allocation:1"
								b["excluded"] = []any{"gateway_journal", "other_allocation_targets", "organization_totals"}
								Object(b, "unresolved_economics")["scope"] = "enterprise_project_allocation"
							}
						}
						selected = append(selected, item)
					}
					m["documents"] = selected
				})
				r, err := DecodeBudgetReading(raw)
				if err != nil {
					t.Fatal("legal reading", err)
				}
				e := BudgetCatalogEntry{BudgetRef: r.BudgetRef, Origin: origin, DefinitionRevision: r.DefinitionRevision, BindingRevision: r.BindingRevision}
				c := BudgetCatalog{SourceInstance: r.SourceInstance, OrgID: r.OrgID, ProjectID: r.ProjectID, AccessGeneration: r.AccessGeneration, CatalogRevision: r.CatalogRevision, Profiles: r.Profiles(), Entries: []BudgetCatalogEntry{e}}
				if err = MatchBudgetReading(r, c, e, all); err != nil {
					t.Fatal("legal subset", err)
				}
				if r.Body(BudgetSummaryProfile) == nil {
					return
				}
				bad := budgetMutation(t, raw, func(m map[string]any) {
					for _, item := range m["documents"].([]any) {
						w := item.(map[string]any)
						if Text(w, "profile") != BudgetSummaryProfile {
							continue
						}
						b := w["document"].(map[string]any)["body"].(map[string]any)
						b["authority"] = "enterprise_finance_allocation_results.project_slice"
						if origin == "enterprise_project" {
							b["authority"] = "usage_finalization_journal.settled.charged_cost_micros"
						}
					}
				})
				wrong, decodeErr := DecodeBudgetReading(bad)
				if mask&1 == 0 && decodeErr != nil {
					t.Fatal("counterexample must reach catalog check without definition", decodeErr)
				}
				if decodeErr == nil && MatchBudgetReading(wrong, c, e, all) == nil {
					t.Fatal("catalog accepted wrong financial authority")
				}
			})
		}
	}
}
