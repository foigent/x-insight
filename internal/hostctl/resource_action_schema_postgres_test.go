package hostctl

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/sourcefacts"
	"github.com/foigent/x-insight/internal/workbench"
)

func TestActionSchema40PreservesSchema39SyntheticRecordAndRejectsProduction(t *testing.T) {
	ctx, c := connectorDigestDatabase(t)
	m := PostgresManager{Runner: ExecRunner{}}
	if err := m.EnsureRoles(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RunMigrations(ctx, c.AdminDSN, connectorDigestMigrations(t, 39)); err != nil {
		t.Fatal(err)
	}
	admin := managedSQL(t, c.AdminDSN)
	var reporterID, targetID string
	if err := admin.QueryRow(`SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&reporterID, &targetID); err != nil {
		t.Fatal(err)
	}
	managedExec(t, admin, `INSERT INTO source_systems(id,source_key,source_type,display_name) VALUES($1::uuid,$2,'unirouter','reporter'),($3::uuid,$4,'unirouter','target')`, reporterID, "source-system:"+reporterID, targetID, "source-system:"+targetID)
	reporter := workbench.ResourceScope{SourceSystemID: reporterID, SourceInstance: "synthetic-reporter", OrgID: "17", ProjectID: "101"}
	target := workbench.ResourceScope{SourceSystemID: targetID, SourceInstance: "synthetic-target", OrgID: "17", ProjectID: "101"}
	at := time.Now().UTC().Truncate(time.Microsecond)
	old := workbench.ResourceAccessRecord{Version: workbench.ResourceAccessVersion, Synthetic: true, RecordRef: "predecessor-1", Revision: 1, Reporter: reporter, Target: target, RequestID: "request-1", ApplicationRef: "app-1", ActorRef: "actor-1", OccurredAt: at, ReportKind: "source_audit", Resource: workbench.ResourceObject{Ref: "file-1", Version: "v1", Name: "Synthetic", Location: "enterprise_service", Ownership: "unknown", Classification: "unknown", DeviceManagement: "unknown"}, Action: "read", AccessOutcome: "unknown", AccessDecision: "unknown"}
	oldRaw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = workbench.DecodeResourceAccessForImport(oldRaw); err != nil {
		t.Fatal(err)
	}
	insert := func(ref string, raw []byte) error {
		_, err := admin.ExecContext(ctx, `INSERT INTO resource_access_records(source_system_id,record_ref,revision,previous_hash,record_hash,source_instance,org_id,project_id,target_source_system_id,target_source_instance,target_org_id,target_project_id,request_id,occurred_at,record_bytes,record_json) VALUES($1::uuid,$2,1,'',$3,$4,$5,$6,$7::uuid,$8,$9,$10,$11,$12,$13::bytea,convert_from($13::bytea,'UTF8')::jsonb)`, reporterID, ref, sourcefacts.Hash(raw), reporter.SourceInstance, reporter.OrgID, reporter.ProjectID, targetID, target.SourceInstance, target.OrgID, target.ProjectID, "request-1", at, raw)
		return err
	}
	if err = insert(old.RecordRef, oldRaw); err != nil {
		t.Fatal("schema39 record", err)
	}
	if n, err := db.RunMigrations(ctx, c.AdminDSN, "../../migrations"); err != nil || n != currentMigrationSchema(t)-39 {
		t.Fatal("schema39 to current schema", n, err)
	}
	var persisted []byte
	if err = admin.QueryRowContext(ctx, `SELECT record_bytes FROM resource_access_records WHERE record_ref=$1`, old.RecordRef).Scan(&persisted); err != nil || string(persisted) != string(oldRaw) {
		t.Fatal("predecessor bytes changed", err)
	}
	bad := strings.Replace(string(oldRaw), `"schema_version":"xinsight.resource-access.internal.v1"`, `"schema_version":"xinsight.resource-action.v1","environment":"production"`, 1)
	if err = insert("production-action", []byte(strings.Replace(bad, `"record_ref":"predecessor-1"`, `"record_ref":"production-action"`, 1))); err == nil {
		t.Fatal("production action admitted by schema")
	}
	badSynthetic := strings.Replace(string(oldRaw), `"synthetic":true`, `"synthetic":false`, 1)
	if err = insert("false-synthetic", []byte(strings.Replace(badSynthetic, `"record_ref":"predecessor-1"`, `"record_ref":"false-synthetic"`, 1))); err == nil {
		t.Fatal("v1 synthetic guard widened")
	}
}
