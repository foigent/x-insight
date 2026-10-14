package db_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

func TestBudgetCatalogAuthorityReceiveAndRestore(t *testing.T) {
	f := newSourceFixture(t)
	all := []string{sourcefacts.BudgetDefinitionProfile, sourcefacts.BudgetSummaryProfile, sourcefacts.BudgetAlertProfile}
	for oi, origin := range []string{"gateway_project", "enterprise_project"} {
		for mask := 1; mask < 8; mask++ {
			t.Run(fmt.Sprintf("%s/%d", origin, mask), func(t *testing.T) {
				var profiles []string
				for i, p := range all {
					if mask&(1<<i) != 0 {
						profiles = append(profiles, p)
					}
				}
				wire := func(wrong bool) []byte {
					return budgetFixtureReading(t, 900+oi*8+mask, profiles, func(m map[string]any) {
						for _, item := range m["documents"].([]any) {
							w := item.(map[string]any)
							b := w["document"].(map[string]any)["body"].(map[string]any)
							if origin == "enterprise_project" {
								switch sourcefacts.Text(w, "profile") {
								case sourcefacts.BudgetDefinitionProfile:
									b["origin"] = origin
									b["period_kind"] = "fiscal_period"
									b["authority_ref"] = "synthetic-enterprise-budget:2"
									b["fiscal_period_ref"] = "synthetic-2026-09"
								case sourcefacts.BudgetSummaryProfile:
									b["authority"] = "enterprise_finance_allocation_results.project_slice"
									b["period_state"] = "closed"
									b["allocation_run_ref"] = "synthetic-allocation:1"
									b["excluded"] = []any{"gateway_journal", "other_allocation_targets", "organization_totals"}
									sourcefacts.Object(b, "unresolved_economics")["scope"] = "enterprise_project_allocation"
								}
							}
							if wrong && sourcefacts.Text(w, "profile") == sourcefacts.BudgetSummaryProfile {
								b["authority"] = "enterprise_finance_allocation_results.project_slice"
								if origin == "enterprise_project" {
									b["authority"] = "usage_finalization_journal.settled.charged_cost_micros"
								}
							}
						}
					})
				}
				good, err := sourcefacts.DecodeBudgetReading(wire(false))
				if err != nil {
					t.Fatal(err)
				}
				catalog, err := sourcefacts.DecodeBudgetCatalog(budgetFixtureCatalog(t, good))
				if err != nil {
					t.Fatal(err)
				}
				catalog.Entries[0].Origin = origin
				catalogRaw, err := json.Marshal(catalog)
				if err != nil {
					t.Fatal(err)
				}
				s, err := f.consumerStore.RegisterBudgetCollection(f.ctx, sourcefacts.BudgetCollection{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: good.SourceInstance, OrgID: good.OrgID, ProjectID: good.ProjectID, ScopeGeneration: "1", Profiles: all})
				if err != nil {
					t.Fatal(err)
				}
				s, err = f.consumerStore.ControlBudgetCollection(f.ctx, s.ID, "active", s.Revision, false)
				if err != nil {
					t.Fatal(err)
				}
				if err = f.consumerStore.CommitBudgetCatalog(f.ctx, s, catalogRaw, time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
				s, err = f.consumerStore.LoadBudgetCollection(f.ctx, s.ID)
				if err != nil {
					t.Fatal(err)
				}
				if mask&2 != 0 {
					if err = f.consumerStore.CommitBudgetReading(f.ctx, s, wire(true), time.Now().UTC()); err == nil {
						t.Fatal("wrong authority admitted")
					}
					after, e := f.consumerStore.LoadBudgetCollection(f.ctx, s.ID)
					if e != nil || !reflect.DeepEqual(s, after) {
						t.Fatal("rejected reading advanced collection", e)
					}
					var count int
					if e = f.admin.QueryRow(`SELECT count(*) FROM source_budget_readings WHERE source_system_id=$1::uuid AND reading_id=$2`, f.source, good.ReadingID).Scan(&count); e != nil || count != 0 {
						t.Fatal("rejected reading persisted", count, e)
					}
					if e = f.admin.QueryRow(`SELECT count(*) FROM source_budget_receipts WHERE collection_id=$1::uuid`, s.ID).Scan(&count); e != nil || count != 0 {
						t.Fatal("rejected reading created receipt", count, e)
					}
				}
				if err = f.consumerStore.CommitBudgetReading(f.ctx, s, wire(false), time.Now().UTC()); err != nil {
					t.Fatal("legal subset receive", err)
				}
				after, err := f.consumerStore.LoadBudgetCollection(f.ctx, s.ID)
				if err != nil || after.VisibleCycle != 1 {
					t.Fatal("legal reading not visible", err)
				}
				if err = db.ValidateWorkbenchRestore(f.ctx, f.admin); err != nil {
					t.Fatal("legal subset restore", err)
				}
				if mask&2 == 0 || mask&1 != 0 {
					return
				}
				// Model a snapshot the old partial-grant path could admit. Keep the
				// catalog and materialized entry coherent; rehash the modified reading.
				badReading, e := sourcefacts.DecodeBudgetReading(wire(true))
				if e != nil {
					t.Fatal(e)
				}
				if sourcefacts.MatchBudgetReading(badReading, catalog, catalog.Entries[0], all) == nil {
					t.Fatal("restore counterexample not mismatched")
				}
				tx, e := f.admin.BeginTx(f.ctx, nil)
				if e != nil {
					t.Fatal(e)
				}
				defer tx.Rollback()
				if _, e = tx.ExecContext(f.ctx, `ALTER TABLE source_budget_readings DISABLE TRIGGER source_budget_readings_immutable`); e != nil {
					t.Fatal(e)
				}
				if _, e = tx.ExecContext(f.ctx, `UPDATE source_budget_readings SET reading_bytes=$1,reading_sha256=$2 WHERE source_system_id=$3::uuid AND reading_id=$4`, badReading.Canonical, badReading.SHA256, f.source, good.ReadingID); e != nil {
					t.Fatal(e)
				}
				if _, e = tx.ExecContext(f.ctx, `ALTER TABLE source_budget_readings ENABLE ALWAYS TRIGGER source_budget_readings_immutable`); e != nil {
					t.Fatal(e)
				}
				e = db.ValidateWorkbenchRestore(f.ctx, tx)
				if e == nil || !strings.Contains(e.Error(), "restored budget receipt crosses source authorization or catalog versions") {
					t.Fatal("restore did not reject financial origin mismatch", e)
				}
				if e = tx.Rollback(); e != nil {
					t.Fatal(e)
				}
				if err = db.ValidateWorkbenchRestore(f.ctx, f.admin); err != nil {
					t.Fatal("negative restore changed good source", err)
				}
			})
		}
	}
}
