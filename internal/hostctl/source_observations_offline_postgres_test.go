package hostctl

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/jackc/pgx/v5"
)

func managedObservationOfflineFixtures(t *testing.T, source string) ([]sourcefacts.OfflineAuthorization, [][]byte) {
	t.Helper()
	a, _, _ := managedOfflineFixture(t, source)
	bytes, err := os.ReadFile("../sourcefacts/contract/observations/golden-observations.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Valid []struct {
			Canonical string `json:"canonical_utf8"`
		}
	}
	if err := json.Unmarshal(bytes, &vectors); err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile("../sourcefacts/contract/observations/source-observation.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var grants []sourcefacts.OfflineAuthorization
	var bundles [][]byte
	seen := map[string]bool{}
	for _, vector := range vectors.Valid {
		d, err := sourcefacts.DecodeObservation([]byte(vector.Canonical))
		if err != nil {
			t.Fatal(err)
		}
		if seen[d.Profile] {
			continue
		}
		seen[d.Profile] = true
		wire, err := sourcefacts.ObservationEnvelope([]byte(vector.Canonical))
		if err != nil {
			t.Fatal(err)
		}
		p := sourcefacts.Page{BatchVersion: "foigent.event-batch.v1", ConnectorID: sourcefacts.ConnectorID, ConnectorVersion: sourcefacts.ObservationConnectorVersion, ProducerInstance: d.SourceInstance, GeneratedAt: time.Now().UTC().Format(sourcefacts.TimeFormat), Events: []json.RawMessage{wire}}
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		g := a
		g.SchemaVersion, g.TargetDatabaseSchema = sourcefacts.ObservationOfflineAuthorizationVersion, sourcefacts.ObservationOfflineDatabaseSchema
		g.AuthorizationID, g.OperationID = fmt.Sprintf("observation-%d", len(grants)), fmt.Sprintf("observation-%d", len(grants))
		g.SourceSchemaSHA256, g.BundleSHA256 = sourcefacts.Hash(schema), sourcefacts.Hash(raw)
		g.SourceInstance, g.OrgID, g.ProjectID, g.Profile = d.SourceInstance, d.OrgID, d.ProjectID, d.Profile
		if _, err := sourcefacts.ValidateOfflineBatch(raw, g, time.Now()); err != nil {
			t.Fatal("B offline fixture", err)
		}
		grants, bundles = append(grants, g), append(bundles, raw)
	}
	if len(grants) != 5 {
		t.Fatal("missing frozen observation profiles")
	}
	return grants, bundles
}

func TestManagedObservationOfflineNativeRolesAndRestore(t *testing.T) {
	ctx, c := connectorDigestDatabase(t)
	m := PostgresManager{Runner: ExecRunner{}}
	if err := m.EnsureRoles(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, c.AdminDSN, connectorDigestMigrations(t, 32)); err != nil {
		t.Fatal(err)
	}
	if err := m.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	admin := managedSQL(t, c.AdminDSN)
	owner, importer := managedStore(t, c.AdminDSN), managedStore(t, c.ImporterDSN)
	var sourceA, sourceB string
	if err := admin.QueryRow(`SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&sourceA, &sourceB); err != nil {
		t.Fatal(err)
	}
	a, rawA, _ := managedOfflineFixture(t, sourceA)
	activate := func(a sourcefacts.OfflineAuthorization) db.SourceOfflineRegistration {
		t.Helper()
		g, err := owner.RegisterSourceOfflineAuthorization(ctx, a)
		if err != nil {
			t.Fatal("register", err)
		}
		g, err = owner.ControlSourceOfflineAuthorization(ctx, g.ID, "active", g.Revision, a.Digest())
		if err != nil {
			t.Fatal("enable", err)
		}
		return g
	}
	activate(a)
	old, err := importer.ImportSourceFactsOffline(ctx, a, rawA, time.Now())
	if err != nil || old.Counts.Inserted == 0 {
		t.Fatal("populated A predecessor", err)
	}
	grants, bundles := managedObservationOfflineFixtures(t, sourceB)
	before := managedOfflineDatabaseState(t, admin)
	if _, err := owner.RegisterSourceOfflineAuthorization(ctx, grants[0]); !errors.Is(err, sourcefacts.ErrAuthorization) {
		t.Fatal("schema32 accepted B", err)
	}
	if !reflect.DeepEqual(before, managedOfflineDatabaseState(t, admin)) {
		t.Fatal("rejected B registration changed schema32")
	}
	targetSchema, parseErr := strconv.Atoi(buildinfo.DatabaseSchemaMax)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	if n, err := db.RunMigrations(ctx, c.AdminDSN, connectorDigestMigrations(t, targetSchema-1)); err != nil || n != targetSchema-33 {
		t.Fatal("populated predecessor before target generation", n, err)
	}
	before = managedOfflineDatabaseState(t, admin)
	if _, err := owner.RegisterSourceOfflineAuthorization(ctx, grants[0]); !errors.Is(err, sourcefacts.ErrAuthorization) {
		t.Fatal("previous schema accepted target-bound B authorization", err)
	}
	if !reflect.DeepEqual(before, managedOfflineDatabaseState(t, admin)) {
		t.Fatal("rejected target-bound registration changed predecessor")
	}
	if n, err := db.RunMigrations(ctx, c.AdminDSN, "../../migrations"); err != nil || n != 1 {
		t.Fatal("populated schema32 through current schema", n, err)
	}
	if err := m.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := m.VerifyRoleBoundaries(ctx, c); err != nil {
		t.Fatal(err)
	}
	if replay, err := importer.ImportSourceFactsOffline(ctx, a, rawA, time.Now()); err != nil || !reflect.DeepEqual(replay, old) {
		t.Fatal("A identity/replay after upgrade", err)
	}
	for _, dsn := range []string{c.ImporterDSN, c.WorkspaceDSN, c.ReaderDSN} {
		if _, err := managedStore(t, dsn).RegisterSourceOfflineAuthorization(ctx, grants[0]); err == nil {
			t.Fatal("runtime role registered B grant")
		}
	}
	for i, grant := range grants {
		raw := bundles[i]
		g, err := owner.RegisterSourceOfflineAuthorization(ctx, grant)
		if err != nil || g.Status != "disabled" {
			t.Fatal("B default disabled", err)
		}
		before := managedOfflineDatabaseState(t, admin)
		if err := importer.CheckSourceFactsOffline(ctx, grant, raw, time.Now()); !errors.Is(err, sourcefacts.ErrAuthorization) {
			t.Fatal("disabled check", err)
		}
		if _, err := importer.ImportSourceFactsOffline(ctx, grant, raw, time.Now()); !errors.Is(err, sourcefacts.ErrAuthorization) {
			t.Fatal("disabled import", err)
		}
		if !reflect.DeepEqual(before, managedOfflineDatabaseState(t, admin)) {
			t.Fatal("disabled B check/import changed state")
		}
		activate(grant)
		before = managedOfflineDatabaseState(t, admin)
		for j := 0; j < 2; j++ {
			if err := importer.CheckSourceFactsOffline(ctx, grant, raw, time.Now()); err != nil {
				t.Fatal("read-only B check", err)
			}
		}
		if !reflect.DeepEqual(before, managedOfflineDatabaseState(t, admin)) {
			t.Fatal("B check mutated table/sequence/cursor")
		}
		if i == 0 {
			managedExec(t, admin, `CREATE FUNCTION synthetic_b_receipt_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic B receipt failure'; END $$`)
			managedExec(t, admin, `CREATE TRIGGER synthetic_b_receipt_failure BEFORE INSERT ON connector_operational_receipts FOR EACH ROW EXECUTE FUNCTION synthetic_b_receipt_failure()`)
			if _, err := importer.ImportSourceFactsOffline(ctx, grant, raw, time.Now()); err == nil {
				t.Fatal("receipt failure absent")
			}
			if managedCount(t, admin, "source_observation_documents") != 0 || managedCount(t, admin, "connector_operational_receipts") != 1 {
				t.Fatal("B fact/receipt transaction did not roll back")
			}
			managedExec(t, admin, `DROP TRIGGER synthetic_b_receipt_failure ON connector_operational_receipts`)
		}
		r, err := importer.ImportSourceFactsOffline(ctx, grant, raw, time.Now())
		if err != nil || r.Counts.Inserted != 1 || r.Counts.Received != 1 {
			t.Fatal("B profile import", grant.Profile, r, err)
		}
		retry, err := importer.ImportSourceFactsOffline(ctx, grant, raw, time.Now())
		if err != nil || !reflect.DeepEqual(r, retry) {
			t.Fatal("B exact replay", err)
		}
	}
	duplicate := grants[0]
	duplicate.AuthorizationID, duplicate.OperationID = "b-independent-duplicate", "b-independent-duplicate"
	activate(duplicate)
	r, err := importer.ImportSourceFactsOffline(ctx, duplicate, bundles[0], time.Now())
	if err != nil || r.Counts.Inserted != 0 || r.Counts.DuplicateNoop != 1 {
		t.Fatal("B independent duplicate", r, err)
	}
	// A valid authorization cannot check or reserve against a dirty target ledger.
	managedExec(t, admin, `UPDATE schema_migrations SET dirty=true WHERE version='000033_add_source_observations'`)
	before = managedOfflineDatabaseState(t, admin)
	if err := importer.CheckSourceFactsOffline(ctx, duplicate, bundles[0], time.Now()); !errors.Is(err, sourcefacts.ErrAuthorization) {
		t.Fatal("dirty target check", err)
	}
	if _, err := importer.ImportSourceFactsOffline(ctx, duplicate, bundles[0], time.Now()); !errors.Is(err, sourcefacts.ErrAuthorization) {
		t.Fatal("dirty target replay", err)
	}
	if !reflect.DeepEqual(before, managedOfflineDatabaseState(t, admin)) {
		t.Fatal("dirty target rejection mutated state")
	}
	managedExec(t, admin, `UPDATE schema_migrations SET dirty=false WHERE version='000033_add_source_observations'`)
	g, err := owner.LoadSourceOfflineRegistration(ctx, grants[0].AuthorizationID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ControlSourceOfflineAuthorization(ctx, g.ID, "revoked", g.Revision, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := importer.ImportSourceFactsOffline(ctx, grants[0], bundles[0], time.Now()); !errors.Is(err, sourcefacts.ErrAuthorization) {
		t.Fatal("B revoked replay", err)
	}
	if managedCount(t, admin, "source_observation_documents") != 5 || managedCount(t, admin, "source_fact_documents") != old.Counts.Inserted || managedCount(t, admin, "workbench_economic_facts") != 0 {
		t.Fatal("B changed A or money membership")
	}
	before = managedOfflineDatabaseState(t, admin)
	if err := db.ValidateWorkbenchRestore(ctx, admin); err != nil {
		t.Fatal("B restore check", err)
	}
	if !reflect.DeepEqual(before, managedOfflineDatabaseState(t, admin)) {
		t.Fatal("B restore validation wrote state")
	}
	backup, err := m.Backup(ctx, c, filepath.Join(t.TempDir(), "b-offline.dump"))
	if err != nil {
		t.Fatal(err)
	}
	f := newInstallFixture(t)
	bound := digestSyntheticCredentials(t, f, c)
	name := fmt.Sprintf("xinsight_restore_bo_%d", time.Now().UnixNano())
	restored, err := StageRestoredCredentialSet(f.paths, bound, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE IF EXISTS " + pgx.Identifier{name}.Sanitize() + " WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	expected, err := db.LoadMigrationChecksums("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.RestoreToNewDatabase(ctx, c, restored, backup.Path, "../../migrations", expected); err != nil {
		t.Fatal("native B restore", err)
	}
	if !reflect.DeepEqual(before, managedOfflineDatabaseState(t, admin)) {
		t.Fatal("restore changed original database")
	}
	recovered, restoredImporter := managedStore(t, restored.AdminDSN), managedStore(t, restored.ImporterDSN)
	for i, a := range grants {
		got, err := recovered.LoadSourceOfflineRegistration(ctx, a.AuthorizationID)
		want := "disabled"
		if i == 0 {
			want = "revoked"
		}
		if err != nil || got.Status != want {
			t.Fatal("restored B authority", got, err)
		}
		if err := restoredImporter.CheckSourceFactsOffline(ctx, a, bundles[i], time.Now()); !errors.Is(err, sourcefacts.ErrAuthorization) {
			t.Fatal("restored grant authorized check", err)
		}
		if _, err := restoredImporter.ImportSourceFactsOffline(ctx, a, bundles[i], time.Now()); !errors.Is(err, sourcefacts.ErrAuthorization) {
			t.Fatal("restored grant imported", err)
		}
	}
	restoredSQL := managedSQL(t, restored.AdminDSN)
	if managedCount(t, restoredSQL, "source_observation_documents") != 5 || managedCount(t, restoredSQL, "source_fact_documents") != old.Counts.Inserted || managedCount(t, restoredSQL, "connector_operational_receipts") != 7 {
		t.Fatal("restored facts/receipts differ")
	}
	if err := db.ValidateWorkbenchRestore(ctx, restoredSQL); err != nil {
		t.Fatal(err)
	}
	t.Logf("populated A schema32 to %d; previous generation rejects target-bound B; B five profiles; actual four roles; check fingerprints; receipt atomicity/replay/revocation; native pg_dump/restore to a separate target; original unchanged and B grants suspended", targetSchema)
}
