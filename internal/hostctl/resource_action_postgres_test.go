package hostctl

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/buildinfo"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func TestActionResourceOfflineCheckImportAndContentFence(t *testing.T) {
	ctx, c := connectorDigestDatabase(t)
	m := PostgresManager{Runner: ExecRunner{}}
	if err := m.EnsureRoles(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, c.AdminDSN, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	if err := m.ApplyGrants(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := m.VerifyRoleBoundaries(ctx, c); err != nil {
		t.Fatal(err)
	}
	admin := managedSQL(t, c.AdminDSN)
	var reporterID, targetID, name, oid, system string
	if err := admin.QueryRow(`SELECT gen_random_uuid()::text,gen_random_uuid()::text,current_database(),oid::text,public.synthetic_resource_cluster_id() FROM pg_database WHERE datname=current_database()`).Scan(&reporterID, &targetID, &name, &oid, &system); err != nil {
		t.Fatal(err)
	}
	managedExec(t, admin, `INSERT INTO source_systems(id,source_key,source_type,display_name) VALUES($1::uuid,$2,'unirouter','action target')`, targetID, "source-system:"+targetID)
	reporter := workbench.ResourceScope{SourceSystemID: reporterID, SourceInstance: "openclaw-test", OrgID: "17", ProjectID: "101"}
	target := workbench.ResourceScope{SourceSystemID: targetID, SourceInstance: "ur-test", OrgID: "17", ProjectID: "101"}
	now := time.Now().UTC().Truncate(time.Microsecond)
	hash := "sha256:" + strings.Repeat("a", 64)
	record := workbench.ActionWitness{Version: workbench.ActionWitnessVersion, Environment: "test", RecordRef: "action-1", Revision: 1, Reporter: reporter, Target: target, RequestID: "request-1", OccurredAt: now, RunID: "run-1", ToolCallID: "call-1", ActionKind: "read", Object: workbench.ActionObject{Ref: "file-1", Version: "v1", Name: "资料.md"}, ToolResult: workbench.ActionToolResult{State: "completed", ResultSHA256: hash}, NativeEvidence: workbench.ActionNativeEvidence{TraceID: "trace-1", SessionID: "session-1", ManifestSHA256: hash, EventsSHA256: hash, CallSeq: 1, CallSHA256: hash, ResultSeq: 2, ResultSHA256: hash}, Acceptance: workbench.ActionAcceptance{State: "not_checked", GoalState: "not_checked", HashScope: "unknown"}, ContentLinks: []workbench.ActionContentLink{}}
	if err := record.Validate(); err != nil {
		t.Fatal(err)
	}
	encode := func(r workbench.ActionWitness) []byte {
		t.Helper()
		raw, err := json.Marshal(map[string]any{"schema_version": sourcefacts.ActionOfflineBundleVersion, "records": []workbench.ActionWitness{r}})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	raw := encode(record)
	a := sourcefacts.OfflineAuthorization{SchemaVersion: sourcefacts.ResourceOfflineAuthorizationVersion, AuthorizationID: "action-local-test", Environment: "test", TargetRef: "action-local-test", MachineIDSHA256: sourcefacts.Hash([]byte("machine")), ProductVersion: "dev", ProductCommit: "action-test", SourceSchemaSHA256: sourcefacts.ActionSchemaSHA256(), SourceSystemID: reporter.SourceSystemID, SourceInstance: reporter.SourceInstance, OrgID: reporter.OrgID, ProjectID: reporter.ProjectID, Profile: sourcefacts.ActionOfflineProfile, NotBefore: now.Add(-time.Minute).Format(sourcefacts.TimeFormat), ExpiresAt: now.Add(time.Hour).Format(sourcefacts.TimeFormat), BundlePath: "/synthetic/action.json", BundleSHA256: sourcefacts.Hash(raw), BundleID: "action-local-test", OperationID: "action-local-test", MaxRecords: 1, PayloadCaptureMode: "metadata_only", TargetDatabaseSchema: buildinfo.DatabaseSchemaMax, TargetDatabaseName: name, TargetDatabaseOID: oid, TargetDatabaseSystemID: system, TargetSourceSystemID: target.SourceSystemID, TargetSourceInstance: target.SourceInstance, TargetOrgID: target.OrgID, TargetProjectID: target.ProjectID}
	owner, importer := managedStore(t, c.AdminDSN), managedStore(t, c.ImporterDSN)
	registration, err := owner.RegisterSourceOfflineAuthorization(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.ControlSourceOfflineAuthorization(ctx, a.AuthorizationID, "active", registration.Revision, a.Digest(), a); err != nil {
		t.Fatal(err)
	}
	var before int
	if err = admin.QueryRow(`SELECT count(*) FROM resource_access_records`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	beforeCheck := managedOfflineDatabaseState(t, admin)
	if n, err := importer.CheckResourceActions(ctx, a, raw, now); err != nil || n != 1 {
		t.Fatal("check", n, err)
	}
	if !reflect.DeepEqual(beforeCheck, managedOfflineDatabaseState(t, admin)) {
		t.Fatal("action check changed a table or sequence")
	}
	var after int
	if err = admin.QueryRow(`SELECT count(*) FROM resource_access_records`).Scan(&after); err != nil || after != before {
		t.Fatal("check wrote", before, after, err)
	}
	receipt, err := importer.ImportResourceActions(ctx, a, raw, now)
	if err != nil || receipt.Counts.Inserted != 1 {
		t.Fatal("import", receipt, err)
	}
	if err = admin.QueryRow(`SELECT count(*) FROM resource_access_records WHERE record_json->>'schema_version'='xinsight.resource-action.v1' AND source_system_id=$1::uuid`, reporterID).Scan(&after); err != nil || after != 1 {
		t.Fatal("action record", after, err)
	}
	bad := record
	bad.RecordRef = "action-missing-content"
	bad.ContentLinks = []workbench.ActionContentLink{{Purpose: "result", Extent: "full_file", SourceSystemID: target.SourceSystemID, SourceInstance: target.SourceInstance, OrgID: target.OrgID, ProjectID: target.ProjectID, RequestID: record.RequestID, SourceFactID: "absent-fact", Revision: "1", ContentRef: "absent-content", SourceSHA256: hash, Pointer: "/messages/1/content"}}
	if err := bad.Validate(); err != nil {
		t.Fatal(err)
	}
	badRaw := encode(bad)
	badAuth := a
	badAuth.AuthorizationID = "action-missing-content"
	badAuth.OperationID = badAuth.AuthorizationID
	badAuth.BundleID = badAuth.AuthorizationID
	badAuth.BundleSHA256 = sourcefacts.Hash(badRaw)
	registration, err = owner.RegisterSourceOfflineAuthorization(ctx, badAuth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.ControlSourceOfflineAuthorization(ctx, badAuth.AuthorizationID, "active", registration.Revision, badAuth.Digest(), badAuth); err != nil {
		t.Fatal(err)
	}
	if _, err = importer.CheckResourceActions(ctx, badAuth, badRaw, now); err == nil {
		t.Fatal("nonexistent C reference passed check")
	}
	if _, err = importer.ImportResourceActions(ctx, badAuth, badRaw, now); err == nil {
		t.Fatal("nonexistent C reference imported")
	}
	if err = admin.QueryRow(`SELECT count(*) FROM resource_access_records WHERE record_json->>'schema_version'='xinsight.resource-action.v1' AND source_system_id=$1::uuid`, reporterID).Scan(&after); err != nil || after != 1 {
		t.Fatal("rejected import changed records", after, err)
	}
}
