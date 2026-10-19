package hostctl

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/testsourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
	"github.com/jackc/pgx/v5"
)

func TestBudgetNativeSchema35UpgradeOfflineBackupRestore(t *testing.T) {
	ctx, c := connectorDigestDatabase(t)
	manager := PostgresManager{Runner: ExecRunner{}}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(manager.EnsureRoles(ctx, c))
	_, err := db.RunMigrations(ctx, c.AdminDSN, connectorDigestMigrations(t, 35))
	must(err)
	must(manager.ApplyGrants(ctx, c))
	admin, err := sql.Open("pgx", c.AdminDSN)
	must(err)
	defer admin.Close()
	source := testsourcefacts.RegisterSource(t, admin)
	captured := testsourcefacts.Load(t, testsourcefacts.Chat)
	testsourcefacts.PersistMoney(t, ctx, c.ImporterDSN, source, captured)
	testsourcefacts.ReplayPreSync35(t, admin, c.AdminDSN, c.ImporterDSN, source, captured, "1")
	oldSubject := "xs_d0d0d0d0d0d0d0d0"
	_, err = admin.Exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_coverage,allow_preferences) VALUES($1,$2::uuid,true,true,true,true)`, oldSubject, source)
	must(err)
	workspace, err := db.OpenWorkspaceStore(ctx, c.WorkspaceDSN)
	must(err)
	defer workspace.Close()
	first, last := testsourcefacts.RetainedPeriod(t, captured)
	request := workbench.ReadingRequest{ContractVersion: workbench.ContractVersion, MetricVersion: workbench.MetricVersion, Page: "cost", MoneyBasis: workbench.MoneyGatewayCharged, Currency: "CNY", Period: workbench.PeriodInput{StartInclusive: &first, EndExclusive: &last, DisplayTimezone: "UTC"}, Filters: workbench.Filters{SourceIDs: []string{source}}}
	old, err := workspace.CreateWorkbenchReading(ctx, oldSubject, request, time.Now())
	must(err)
	old, err = workspace.GetWorkbenchReading(ctx, oldSubject, old.ID, time.Now())
	must(err)
	oldSummary, err := workspace.GetWorkbenchSummary(ctx, oldSubject, old.ID, time.Now())
	must(err)
	if oldSummary.RequestCount != 1 {
		t.Fatal("empty predecessor is insufficient")
	}
	_, err = db.RunMigrations(ctx, c.AdminDSN, "../../migrations")
	must(err)
	must(manager.ApplyGrants(ctx, c))
	must(manager.VerifyRoleBoundaries(ctx, c))
	got, err := workspace.GetWorkbenchReading(ctx, oldSubject, old.ID, time.Now())
	must(err)
	if !reflect.DeepEqual(old, got) {
		t.Fatal("schema35 reading changed during upgrade")
	}
	budgetRequest := workbench.BudgetReadingRequest{ContractVersion: workbench.BudgetContractVersion}
	if _, err = workspace.CreateBudgetReading(ctx, oldSubject, budgetRequest, time.Now()); err == nil {
		t.Fatal("old economic grant gained budget authority")
	}
	raw, err := os.ReadFile("../sourcefacts/contract/budgets/golden-readings.json")
	must(err)
	var vectors struct{ Positive []json.RawMessage }
	must(json.Unmarshal(raw, &vectors))
	wire := vectors.Positive[0]
	reading, err := sourcefacts.DecodeBudgetReading(wire)
	must(err)
	catalog := sourcefacts.BudgetCatalog{SchemaVersion: sourcefacts.BudgetCatalogVersion, SourceInstance: reading.SourceInstance, OrgID: reading.OrgID, ProjectID: reading.ProjectID, ReadingCutoff: reading.ReadingCutoff, AccessGeneration: reading.AccessGeneration, CatalogRevision: reading.CatalogRevision, Profiles: reading.Profiles(), EnterpriseScope: "scope_not_configured", Entries: []sourcefacts.BudgetCatalogEntry{{BudgetRef: reading.BudgetRef, Origin: "gateway_project", DefinitionRevision: reading.DefinitionRevision, BindingRevision: reading.BindingRevision}}}
	catalogRaw, err := json.Marshal(catalog)
	must(err)
	bundle, err := json.Marshal(sourcefacts.BudgetOfflineBundle{SchemaVersion: sourcefacts.BudgetOfflineBundleVersion, Catalog: catalogRaw, Readings: []sourcefacts.BudgetOfflineEntry{{Reading: wire}}})
	must(err)
	now := time.Now().UTC()
	authorization := sourcefacts.BudgetOfflineAuthorization{SchemaVersion: sourcefacts.BudgetOfflineAuthorizationVersion, CollectionID: "dededede-dede-4ede-8ede-dededededede", Environment: "test", TargetRef: "xi-d-native-lifecycle-test", MachineIDSHA256: sourcefacts.Hash([]byte("synthetic-machine")), ProductVersion: "dev-d", ProductCommit: strings.Repeat("d", 40), TargetDatabaseSchema: "36", SourceCommit: sourcefacts.BudgetSourceCommit, SourceManifestSHA256: sourcefacts.BudgetManifestSHA256(), SourceSystemID: source, SourceInstance: reading.SourceInstance, OrgID: reading.OrgID, ProjectID: reading.ProjectID, ScopeGeneration: "1", Profiles: reading.Profiles(), NotBefore: now.Add(-time.Minute).Format(sourcefacts.TimeFormat), ExpiresAt: now.Add(time.Hour).Format(sourcefacts.TimeFormat), BundlePath: "/test/native-budget.json", BundleSHA256: sourcefacts.Hash(bundle)}
	owner, err := db.OpenSourceFactsStore(c.AdminDSN)
	must(err)
	defer owner.Close()
	if authorization.TargetDatabaseSchema != buildinfo.DatabaseSchemaMax {
		if _, err = owner.RegisterBudgetOffline(ctx, authorization); err == nil {
			t.Fatal("schema36 authorization accepted after upgrade to current schema")
		}
	}
	authorization.TargetDatabaseSchema = buildinfo.DatabaseSchemaMax
	importer, err := db.OpenSourceFactsStore(c.ImporterDSN)
	must(err)
	defer importer.Close()
	collection, err := owner.RegisterBudgetOffline(ctx, authorization)
	must(err)
	collection, err = owner.ControlBudgetOffline(ctx, authorization, collection.Revision, false)
	must(err)
	beforeCheck, err := manager.PreviewCleanBaseline(ctx, c)
	must(err)
	must(importer.CheckBudgetOffline(ctx, authorization, bundle, now))
	afterCheck, err := manager.PreviewCleanBaseline(ctx, c)
	must(err)
	if !reflect.DeepEqual(beforeCheck, afterCheck) {
		t.Fatal("import check mutated native tables")
	}
	receipt, err := importer.ImportBudgetOffline(ctx, authorization, bundle, now)
	must(err)
	if receipt.SourceReadings != 1 || receipt.Duplicate {
		t.Fatal("native importer did not append exactly one reading")
	}
	subject := "xs_d1d1d1d1d1d1d1d1"
	_, err = admin.Exec(`INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_budget_definition,allow_budget_summary,allow_budget_alert) VALUES($1,$2::uuid,true,true,true)`, subject, source)
	must(err)
	frozen, err := workspace.CreateBudgetReading(ctx, subject, budgetRequest, time.Now())
	must(err)
	items, err := workspace.GetBudgetItems(ctx, subject, frozen.ID, 25, "", time.Now())
	must(err)
	if len(items.Items) != 1 || items.Items[0].SourceState != "bounded_offline" {
		t.Fatal("native budget reading missing")
	}
	immutableTables := []string{"source_budget_catalogs", "source_budget_catalog_entries", "source_budget_readings", "source_budget_receipts", "source_budget_offline_authorizations", "source_budget_offline_imports", "budget_readings", "budget_reading_members", "source_fact_documents", "evidence_events", "workbench_economic_facts"}
	fingerprints := func(conn *sql.DB) map[string]string {
		t.Helper()
		result := map[string]string{}
		for _, name := range immutableTables {
			var value string
			must(conn.QueryRowContext(ctx, `SELECT count(*)::text||':'||md5(coalesce(string_agg(to_jsonb(t)::text,'' ORDER BY to_jsonb(t)::text),'')) FROM `+pgx.Identifier{name}.Sanitize()+` t`).Scan(&value))
			if strings.HasPrefix(value, "0:") {
				t.Fatal("empty native restore evidence", name)
			}
			result[name] = value
		}
		return result
	}
	beforeRows := fingerprints(admin)
	must(db.ValidateWorkbenchRestore(ctx, admin))
	before, err := manager.PreviewCleanBaseline(ctx, c)
	must(err)
	backup, err := manager.Backup(ctx, c, filepath.Join(t.TempDir(), "budget-offline.dump"))
	must(err)
	fixture := newInstallFixture(t)
	c = digestSyntheticCredentials(t, fixture, c)
	name := fmt.Sprintf("xinsight_restore_d_%d", time.Now().UnixNano())
	restored, err := StageRestoredCredentialSet(fixture.paths, c, name)
	must(err)
	t.Cleanup(func() {
		a, e := sql.Open("pgx", c.AdminDSN)
		if e != nil {
			t.Error(e)
			return
		}
		defer a.Close()
		if _, e = a.Exec("DROP DATABASE IF EXISTS " + pgx.Identifier{name}.Sanitize() + " WITH (FORCE)"); e != nil {
			t.Error(e)
		}
	})
	checksums, err := db.LoadMigrationChecksums("../../migrations")
	must(err)
	must(manager.RestoreToNewDatabase(ctx, c, restored, backup.Path, "../../migrations", checksums))
	must(manager.VerifyRoleBoundaries(ctx, restored))
	recoveredAdmin, err := sql.Open("pgx", restored.AdminDSN)
	must(err)
	defer recoveredAdmin.Close()
	if !reflect.DeepEqual(beforeRows, fingerprints(recoveredAdmin)) {
		t.Fatal("native restore changed immutable A/I01/D facts, receipts or frozen members")
	}
	recovered, err := db.OpenWorkspaceStore(ctx, restored.WorkspaceDSN)
	must(err)
	defer recovered.Close()
	got, err = recovered.GetWorkbenchReading(ctx, oldSubject, old.ID, time.Now())
	must(err)
	if !reflect.DeepEqual(old, got) {
		t.Fatal("old reading changed in new database")
	}
	summary, err := recovered.GetWorkbenchSummary(ctx, oldSubject, old.ID, time.Now())
	must(err)
	if !reflect.DeepEqual(oldSummary, summary) {
		t.Fatal("old money changed in new database")
	}
	if _, err = recovered.GetBudgetReading(ctx, subject, frozen.ID, time.Now()); err == nil {
		t.Fatal("restore implicitly enabled saved budget reading")
	}
	recoveredOwner, err := db.OpenSourceFactsStore(restored.AdminDSN)
	must(err)
	defer recoveredOwner.Close()
	collection, err = recoveredOwner.LoadBudgetCollection(ctx, collection.ID)
	must(err)
	if collection.Status != "paused" || collection.VisibleCycle != 0 {
		t.Fatal("restore failed to suspend budget authority")
	}
	if _, err = recoveredOwner.ControlBudgetOffline(ctx, authorization, collection.Revision, true); err == nil {
		t.Fatal("old offline authorization resumed after restore")
	}
	recoveredImporter, err := db.OpenSourceFactsStore(restored.ImporterDSN)
	must(err)
	defer recoveredImporter.Close()
	if _, err = recoveredImporter.ImportBudgetOffline(ctx, authorization, bundle, time.Now()); err == nil {
		t.Fatal("restored importer replayed stale authorization")
	}
	final, err := manager.PreviewCleanBaseline(ctx, c)
	must(err)
	if !reflect.DeepEqual(before, final) {
		t.Fatal("preview/backup/new-database restore changed source database")
	}
	t.Logf("actual schema35→%s; nonempty A/I01 and old reading; default-deny D; real importer/workspace; offline read-only check; pg_dump/new-database restore; exact 11-table fingerprints; restored D paused, old authorization rejected; source unchanged", buildinfo.DatabaseSchemaMax)
}
