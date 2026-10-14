package db_test

import (
	"encoding/json"
	"github.com/foigent/x-insight/internal/buildinfo"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func TestBudgetOfflineAtomicCheckReplayRevocationAndRestore(t *testing.T) {
	f := newSourceFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	profiles := []string{sourcefacts.BudgetDefinitionProfile, sourcefacts.BudgetSummaryProfile, sourcefacts.BudgetAlertProfile}
	raw := budgetFixtureReading(t, 909, profiles, nil)
	r, err := sourcefacts.DecodeBudgetReading(raw)
	if err != nil {
		t.Fatal(err)
	}
	bundle, _ := json.Marshal(sourcefacts.BudgetOfflineBundle{SchemaVersion: sourcefacts.BudgetOfflineBundleVersion, Catalog: budgetFixtureCatalog(t, r), Readings: []sourcefacts.BudgetOfflineEntry{{Reading: raw}}})
	a := sourcefacts.BudgetOfflineAuthorization{SchemaVersion: sourcefacts.BudgetOfflineAuthorizationVersion, CollectionID: testUUID(t), Environment: "test", TargetRef: "xi-d-offline-test", MachineIDSHA256: sourcefacts.Hash([]byte("synthetic-machine")), ProductVersion: "dev-d", ProductCommit: strings.Repeat("a", 40), TargetDatabaseSchema: "36", SourceCommit: sourcefacts.BudgetSourceCommit, SourceManifestSHA256: sourcefacts.BudgetManifestSHA256(), SourceSystemID: f.source, SourceInstance: r.SourceInstance, OrgID: r.OrgID, ProjectID: r.ProjectID, ScopeGeneration: "1", Profiles: profiles, NotBefore: now.Add(-time.Minute).Format(sourcefacts.TimeFormat), ExpiresAt: now.Add(time.Hour).Format(sourcefacts.TimeFormat), BundlePath: "/test/controlled-budget.json", BundleSHA256: sourcefacts.Hash(bundle)}
	if a.TargetDatabaseSchema != buildinfo.DatabaseSchemaMax {
		if _, err := f.consumerStore.RegisterBudgetOffline(f.ctx, a); err == nil {
			t.Fatal("schema36 authorization accepted by a newer actual database")
		}
	}
	a.TargetDatabaseSchema = buildinfo.DatabaseSchemaMax
	c, err := f.consumerStore.RegisterBudgetOffline(f.ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if c.Status != "paused" || c.Transport != "bounded_offline" {
		t.Fatal("offline implicitly activated")
	}
	if _, err = f.consumerStore.ImportBudgetOffline(f.ctx, a, bundle, now); err == nil {
		t.Fatal("unapproved offline import")
	}
	if _, err = f.consumerStore.ControlBudgetCollection(f.ctx, c.ID, "active", c.Revision, false); err == nil {
		t.Fatal("live control enabled offline")
	}
	c, err = f.consumerStore.ControlBudgetOffline(f.ctx, a, c.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	state := func() string {
		var value string
		err := f.admin.QueryRowContext(f.ctx, `SELECT row_to_json(c)::text || (SELECT count(*)::text FROM source_budget_catalogs) || (SELECT count(*)::text FROM source_budget_readings) || (SELECT count(*)::text FROM source_budget_offline_imports) FROM source_budget_collections c WHERE id=$1::uuid`, c.ID).Scan(&value)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := state()
	// Every public operation must reject an unusable target before changing
	// authorization, collection progress, receipts, or retained source bytes.
	var ledger string
	if err = f.admin.QueryRowContext(f.ctx, `SELECT jsonb_agg(to_jsonb(m) ORDER BY version)::text FROM schema_migrations m`).Scan(&ledger); err != nil {
		t.Fatal(err)
	}
	persistentState := func(t *testing.T) string {
		t.Helper()
		var result strings.Builder
		for _, table := range []string{"source_budget_collections", "source_budget_offline_authorizations", "source_budget_catalogs", "source_budget_catalog_entries", "source_budget_readings", "source_budget_receipts", "source_budget_offline_imports", "source_fact_management_events"} {
			var digest string
			if err := f.admin.QueryRowContext(f.ctx, `SELECT encode(sha256(convert_to(COALESCE(jsonb_agg(to_jsonb(r) ORDER BY to_jsonb(r))::text,'[]'),'UTF8')),'hex') FROM `+table+` r`).Scan(&digest); err != nil {
				t.Fatal(err)
			}
			result.WriteString(digest)
		}
		return result.String()
	}
	for _, scenario := range []struct{ name, mutation string }{
		{"old_target", ""},
		{"future_head", `UPDATE schema_migrations SET version='000041_synthetic_future' WHERE version=(SELECT max(version) FROM schema_migrations)`},
		{"dirty", `UPDATE schema_migrations SET dirty=true WHERE version=(SELECT min(version) FROM schema_migrations)`},
		{"missing_checksum", `UPDATE schema_migrations SET checksum_sha256=NULL WHERE version=(SELECT min(version) FROM schema_migrations)`},
		{"empty_ledger", `DELETE FROM schema_migrations`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			candidate := a
			if scenario.mutation == "" {
				candidate.TargetDatabaseSchema = "36"
			} else {
				if _, err := f.admin.ExecContext(f.ctx, scenario.mutation); err != nil {
					t.Fatal(err)
				}
				defer func() {
					tx, err := f.admin.BeginTx(f.ctx, nil)
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback()
					if _, err = tx.ExecContext(f.ctx, `DELETE FROM schema_migrations`); err != nil {
						t.Fatal(err)
					}
					if _, err = tx.ExecContext(f.ctx, `INSERT INTO schema_migrations SELECT * FROM jsonb_populate_recordset(NULL::schema_migrations,$1::jsonb)`, ledger); err != nil {
						t.Fatal(err)
					}
					if err = tx.Commit(); err != nil {
						t.Fatal(err)
					}
				}()
			}
			unchanged := persistentState(t)
			if _, err := f.consumerStore.RegisterBudgetOffline(f.ctx, candidate); err == nil {
				t.Fatal("invalid target registered")
			}
			if _, err := f.consumerStore.ControlBudgetOffline(f.ctx, candidate, c.Revision, false); err == nil {
				t.Fatal("invalid target activated")
			}
			if err := f.consumerStore.CheckBudgetOffline(f.ctx, candidate, bundle, now); err == nil {
				t.Fatal("invalid target passed read-only check")
			}
			if _, err := f.consumerStore.ImportBudgetOffline(f.ctx, candidate, bundle, now); err == nil {
				t.Fatal("invalid target imported")
			}
			if unchanged != persistentState(t) {
				t.Fatal("rejected target mutated persisted budget state")
			}
		})
	}
	if err = f.consumerStore.CheckBudgetOffline(f.ctx, a, bundle, now); err != nil {
		t.Fatal(err)
	}
	if before != state() {
		t.Fatal("offline check mutated rows or progress")
	}
	if err = f.consumerStore.CommitBudgetCatalog(f.ctx, c, budgetFixtureCatalog(t, r), now); err == nil {
		t.Fatal("network catalog admitted into offline collection")
	}
	receipt, err := f.consumerStore.ImportBudgetOffline(f.ctx, a, bundle, now)
	if err != nil || receipt.Duplicate || receipt.SourceReadings != 1 {
		t.Fatal(receipt, err)
	}
	before = state()
	receipt, err = f.consumerStore.ImportBudgetOffline(f.ctx, a, bundle, now.Add(time.Second))
	if err != nil || !receipt.Duplicate || before != state() {
		t.Fatal("replay changed immutable state", receipt, err)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_budget_definition=true,allow_budget_summary=true,allow_budget_alert=true WHERE subject_id=$1`, f.subject)
	reading, err := f.workspace.CreateBudgetReading(f.ctx, f.subject, workbench.BudgetReadingRequest{ContractVersion: workbench.BudgetContractVersion}, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	items, err := f.workspace.GetBudgetItems(f.ctx, f.subject, reading.ID, 25, "", now.Add(time.Second))
	if err != nil || len(items.Items) != 1 || items.Items[0].SourceState != "bounded_offline" {
		t.Fatal(items, err)
	}
	if err = db.ValidateWorkbenchRestore(f.ctx, f.admin); err != nil {
		t.Fatal(err)
	}
	if err = db.SuspendRestoredSourceFacts(f.ctx, f.admin); err != nil {
		t.Fatal(err)
	}
	if _, err = f.workspace.GetBudgetReading(f.ctx, f.subject, reading.ID, now.Add(time.Second)); err == nil {
		t.Fatal("restored offline snapshot retained implicit visibility")
	}
	if _, err = f.consumerStore.ImportBudgetOffline(f.ctx, a, bundle, now); err == nil {
		t.Fatal("restore silently reactivated offline authority")
	}
	current, err := f.consumerStore.LoadBudgetCollection(f.ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.consumerStore.ControlBudgetOffline(f.ctx, a, current.Revision, true); err == nil {
		t.Fatal("consumed restored offline authorization re-enabled; new authorization required")
	}
	if _, err = f.consumerStore.ControlBudgetCollection(f.ctx, c.ID, "revoked", current.Revision, false); err != nil {
		t.Fatal(err)
	}
	if err = f.consumerStore.CheckBudgetOffline(f.ctx, a, bundle, now); err == nil {
		t.Fatal("revoked authorization passed check")
	}
}
