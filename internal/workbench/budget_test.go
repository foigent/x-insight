package workbench

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

func TestBudgetProjectionKeepsOriginalMoneyAndProfileAuthority(t *testing.T) {
	b, err := os.ReadFile("../sourcefacts/contract/budgets/golden-readings.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct{ Positive []json.RawMessage }
	if err = json.Unmarshal(b, &vectors); err != nil {
		t.Fatal(err)
	}
	r, err := sourcefacts.DecodeBudgetReading(vectors.Positive[0])
	if err != nil {
		t.Fatal(err)
	}
	all := []string{sourcefacts.BudgetDefinitionProfile, sourcefacts.BudgetSummaryProfile, sourcefacts.BudgetAlertProfile}
	for mask := 0; mask < 8; mask++ {
		var granted []string
		for i, p := range all {
			if mask&(1<<i) != 0 {
				granted = append(granted, p)
			}
		}
		item := ProjectBudget(r, granted, all)
		if mask&1 == 0 && item.Definition.Body != nil {
			t.Fatal("definition exposed")
		}
		if mask&2 == 0 && item.Summary.Body != nil {
			t.Fatal("summary exposed")
		}
		if mask&4 == 0 && item.Alerts.Body != nil {
			t.Fatal("alerts exposed")
		}
		if mask&2 != 0 && mask&1 == 0 {
			comparison := sourcefacts.Object(item.Summary.Body, "comparison")
			if len(comparison) != 1 || sourcefacts.Text(comparison, "state") != "definition_not_authorized" || item.Summary.SourceHash != "" {
				t.Fatal("hidden budget can be inferred from summary")
			}
		}
		if mask == 0 {
			raw, _ := json.Marshal(item)
			if bytes.Contains(raw, []byte("1000000000")) || bytes.Contains(raw, []byte("600000000")) {
				t.Fatal("amount leaked without budget grants")
			}
		}
	}
	full := ProjectBudget(r, all, all)
	comparison := sourcefacts.Object(full.Summary.Body, "comparison")
	if sourcefacts.Text(comparison, "utilization_numerator") != "600000000" || sourcefacts.Text(sourcefacts.Object(comparison, "remaining"), "value_minor") != "400000000" {
		t.Fatal("source comparison changed")
	}
	if full.RuntimeEffect != "report_only" || full.DetailRelationship != "request_membership_not_provided" {
		t.Fatal("snapshot invented authority or detail coverage")
	}
	if sourcefacts.Text(sourcefacts.Object(r.Body(sourcefacts.BudgetSummaryProfile), "comparison"), "state") != "comparable" {
		t.Fatal("projection changed retained source")
	}
	// Historical bytes do not restore a source permission that has been removed.
	shrunk := ProjectBudget(r, all, all[1:])
	if shrunk.Definition.State != "source_profile_not_authorized" || shrunk.Definition.Body != nil || sourcefacts.Text(sourcefacts.Object(shrunk.Summary.Body, "comparison"), "state") != "source_definition_not_authorized" {
		t.Fatal("historical source bytes bypassed current source grant or lost its reason")
	}
	// A summary authorized by the source on its own is usable as a subtotal,
	// without splicing an earlier definition or exposing its hidden denominator.
	only := r
	only.Documents = only.Documents[1:2]
	i := ProjectBudget(only, all, only.Profiles())
	if i.Definition.State != "source_profile_not_authorized" || sourcefacts.Text(sourcefacts.Object(i.Summary.Body, "comparison"), "state") != "source_definition_not_authorized" {
		t.Fatal("source subset fabricated definition")
	}
}

func TestBudgetRequestCannotSubstituteAnEconomicProjectForSourceScope(t *testing.T) {
	r := BudgetReadingRequest{ContractVersion: BudgetContractVersion}
	if r.Validate() != nil {
		t.Fatal("authorized all-source selection rejected")
	}
	r.Filters.CostProjects = []string{"allocation-a"}
	if r.Validate() == nil {
		t.Fatal("unproven cost-project membership narrowed full source budget")
	}
}
