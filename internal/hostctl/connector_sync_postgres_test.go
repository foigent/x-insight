package hostctl

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connectorsync"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

func TestSyncSchema46UpgradeRolesAndContract(t *testing.T) {
	ctx, credentials := connectorDigestDatabase(t)
	manager := PostgresManager{Runner: ExecRunner{}}
	if e := manager.EnsureRoles(ctx, credentials); e != nil {
		t.Fatal(e)
	}
	if _, e := db.RunMigrations(ctx, credentials.AdminDSN, connectorDigestMigrations(t, 45)); e != nil {
		t.Fatal(e)
	}
	if e := manager.ApplyGrants(ctx, credentials); e != nil {
		t.Fatal(e)
	}
	admin := managedSQL(t, credentials.AdminDSN)
	id := "xci_4646464646464646464646"
	if _, e := admin.ExecContext(ctx, `INSERT INTO connector_installations(id,deployment_id,connector_type,requested_event_scope,status,desired_state,observed_state,created_at,updated_at) VALUES($1,'xid_4646464646464646464646','unirouter',ARRAY['model.request.completed'],'pending','active','awaiting_consent',clock_timestamp(),clock_timestamp())`, id); e != nil {
		t.Fatal(e)
	}
	if n, e := db.RunMigrations(ctx, credentials.AdminDSN, connectorDigestMigrations(t, 46)); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	if e := manager.ApplyGrants(ctx, credentials); e != nil {
		t.Fatal(e)
	}
	if e := manager.VerifyRoleBoundaries(ctx, credentials); e != nil {
		t.Fatal(e)
	}
	var preserved bool
	if e := admin.QueryRowContext(ctx, `SELECT auto_sync AND cadence_seconds IS NULL FROM connector_sync_controls WHERE installation_id=$1`, id).Scan(&preserved); e != nil || !preserved {
		t.Fatal("existing cadence/automation drift", e)
	}
	importer := managedSQL(t, credentials.ImporterDSN)
	if _, e := importer.ExecContext(ctx, `UPDATE connector_sync_controls SET auto_sync=false`); e == nil {
		t.Fatal("importer gained preference authority")
	}
	for _, fn := range []string{"public.xinsight_sync_scope(text,text,text,boolean)", "public.xinsight_sync_units(text)", "public.xinsight_sync_receiver(text)"} {
		var allowed bool
		if e := admin.QueryRowContext(ctx, `SELECT has_function_privilege('xinsight_importer',$1,'EXECUTE') AND NOT has_function_privilege('xinsight_reader',$1,'EXECUTE') AND NOT has_function_privilege('xinsight_workspace',$1,'EXECUTE WITH GRANT OPTION')`, fn).Scan(&allowed); e != nil || !allowed {
			t.Fatal("ownership projection grants", fn, e)
		}
	}
	manifest, e := GenerateCleanBaselineSchemaContract(ctx, admin)
	if e != nil {
		t.Fatal(e)
	}
	if manifest.DatabaseSchema != "46" || manifest.MigrationCount != 46 {
		t.Fatal("wrong generated schema identity")
	}
	if target := os.Getenv("XINSIGHT_SYNC_SCHEMA_OUTPUT"); target != "" {
		if !filepath.IsAbs(target) || filepath.Clean(target) != target {
			t.Fatal("absolute schema output required")
		}
		raw, e := json.Marshal(manifest)
		if e != nil {
			t.Fatal(e)
		}
		file, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = file.Write(append(raw, '\n')); e != nil {
			file.Close()
			t.Fatal(e)
		}
		if e = file.Close(); e != nil {
			t.Fatal(e)
		}
	}
}

func TestSyncPauseAfterEnrollmentResponsePreventsNativePublishAndSave(t *testing.T) {
	ctx, credentials := connectorDigestDatabase(t)
	manager := PostgresManager{Runner: ExecRunner{}}
	if e := manager.EnsureRoles(ctx, credentials); e != nil {
		t.Fatal(e)
	}
	if _, e := db.RunMigrations(ctx, credentials.AdminDSN, connectorDigestMigrations(t, 46)); e != nil {
		t.Fatal(e)
	}
	admin := managedSQL(t, credentials.AdminDSN)
	id := "xci_AAAAAAAAAAAAAAAAAAAAAA"
	if _, e := admin.ExecContext(ctx, `INSERT INTO connector_installations(id,deployment_id,connector_type,requested_event_scope,status,desired_state,observed_state,created_at,updated_at)VALUES($1,'xid_0000000000000000000000','unirouter',ARRAY['model.request.completed'],'pending','active','awaiting_consent',statement_timestamp(),statement_timestamp())`, id); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.ExecContext(ctx, `INSERT INTO connector_installation_source_bindings(installation_id,source_binding_sha256)VALUES($1,'sha256:'||repeat('a',64))`, id); e != nil {
		t.Fatal(e)
	}
	gate := connectorsync.New(admin)
	request := connectorsync.Request{SchemaVersion: connectorsync.LocalSchema, InstallationID: id, Action: "once", Actor: "xs_4646464646464646", ExpectedRevision: 1, RequestID: "46464646-4646-4646-8646-000000000010"}
	status, e := gate.Change(ctx, request)
	if e != nil {
		t.Fatal(e)
	}
	permit, finish, e := gate.Begin(ctx, id, "projects", time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	defer finish(connectorsync.ErrChanged)
	engine, _ := managedCustodyFixture(t)
	engine.Runner = &fakeRunner{}
	prepared, e := engine.PrepareSourceCredential(ctx, id, "xise_1", "facts", "fixture-source", "7", "9", []string{sourcefacts.CostProfile})
	if e != nil {
		t.Fatal(e)
	}
	cfg := sourcefacts.ManagedConfig{SchemaVersion: sourcefacts.ManagedConfigVersion, PollIntervalSeconds: 5, RetryMaximumSeconds: 30, Profiles: []sourcefacts.ManagedProfileConfig{{StreamID: ManagedCollectionID(id, "xise_1", "9", "facts"), SourceSystemID: "11111111-1111-4111-8111-111111111111", SourceInstance: "fixture-source", OrgID: "7", ProjectID: "9", Profile: sourcefacts.CostProfile, ScopeGeneration: "1", Endpoint: "https://source.example/api/v1/integrations/xinsight/source-facts", KeyFile: prepared.KeyFile}}}
	before := snapshotManagedFiles(t, engine.Paths.Root)
	request.Action = "set"
	request.ExpectedRevision = status.Revision
	request.RequestID = "46464646-4646-4646-8646-000000000011"
	if _, e = gate.Change(ctx, request); e != nil {
		t.Fatal(e)
	}
	if e = engine.PublishManagedProjects(permit, map[string]sourcefacts.ManagedConfig{id + "-xise_1-project-9.json": cfg}); !errors.Is(e, connectorsync.ErrChanged) {
		t.Fatal("native publication did not observe pause", e)
	}
	if after := snapshotManagedFiles(t, engine.Paths.Root); !reflect.DeepEqual(before, after) {
		t.Fatal("paused response published native configuration")
	}
	store, e := db.OpenWorkspaceStore(ctx, credentials.AdminDSN)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	e = store.SaveManagedEnrollment(permit, db.ManagedEnrollment{InstallationID: id, EnrollmentID: "xise_1", SourceInstance: "fixture-source", OrgID: "7", ProjectID: "9", Revision: "1", Status: "active"}, time.Now())
	if !errors.Is(e, connectorsync.ErrChanged) {
		t.Fatal("enrollment save did not use same transaction fence", e)
	}
	var count int
	if e = admin.QueryRowContext(ctx, `SELECT count(*) FROM connector_source_enrollments WHERE installation_id=$1`, id).Scan(&count); e != nil || count != 0 {
		t.Fatal("paused enrollment persisted", count, e)
	}
}
