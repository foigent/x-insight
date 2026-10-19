package db_test

import (
	"context"
	"encoding/json"
	"github.com/foigent/x-insight/internal/db"
	"reflect"
	"testing"
	"time"
)

func TestWorkbenchGrantControlAtomicRevisionScopeAndReaderFence(t *testing.T) {
	f := newSourceFixture(t)
	project, all, rev := "42", "*", int64(0)
	r := db.WorkbenchGrantChange{SchemaVersion: "xinsight.workbench-grant-change.v1", Action: "create", ID: testUUID(t), Subject: f.subject, Source: f.source, SourceProject: &project, CostProject: &all, ExpectedRevision: &rev, Capabilities: []string{"summary", "details", "resource", "budget_summary"}}
	auditCount := func() int {
		var n int
		if err := f.admin.QueryRow(`SELECT count(*) FROM access_audit_records WHERE resource_ref=$1`, r.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	check, err := f.consumerStore.ChangeWorkbenchGrant(f.ctx, r, true)
	if err != nil || check.Changed || !check.CheckOnly || auditCount() != 0 {
		t.Fatalf("check wrote or failed: %+v %v", check, err)
	}
	if _, err = f.consumerStore.InspectWorkbenchGrant(f.ctx, r.ID); err == nil {
		t.Fatal("check created grant")
	}
	first, err := f.consumerStore.ChangeWorkbenchGrant(f.ctx, r, false)
	if err != nil || first.Revision != 1 || !first.Changed || auditCount() != 1 {
		t.Fatalf("create %+v %v", first, err)
	}
	// Existing global grant must remain separate; explicit scope isn't widened.
	var after map[string]any
	if json.Unmarshal(first.After, &after) != nil || after["source_project_ref"] != "42" || after["cost_project_ref"] != nil || after["allow_budget_definition"] != false {
		t.Fatalf("wrong explicit scope/capabilities: %s", first.After)
	}
	rev = 1
	r.Action = "replace"
	r.Capabilities = []string{"summary", "budget_alert"}
	second, err := f.consumerStore.ChangeWorkbenchGrant(f.ctx, r, false)
	if err != nil || second.Revision != 2 || auditCount() != 2 {
		t.Fatalf("replace %+v %v", second, err)
	}
	if _, err = f.consumerStore.ChangeWorkbenchGrant(f.ctx, r, false); err == nil {
		t.Fatal("stale revision accepted")
	}
	unchanged, _ := f.consumerStore.InspectWorkbenchGrant(f.ctx, r.ID)
	if !reflect.DeepEqual([]byte(unchanged), []byte(second.After)) || auditCount() != 2 {
		t.Fatal("stale operation changed state")
	}
	// An audit failure rolls back the grant and its revision too.
	f.exec(`CREATE FUNCTION reject_test_grant_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected audit failure'; END $$; CREATE TRIGGER reject_test_grant_audit BEFORE INSERT ON access_audit_records FOR EACH ROW EXECUTE FUNCTION reject_test_grant_audit()`)
	rev = 2
	r.Capabilities = []string{"summary", "details"}
	if _, err = f.consumerStore.ChangeWorkbenchGrant(f.ctx, r, false); err == nil {
		t.Fatal("audit failure accepted")
	}
	unchanged, _ = f.consumerStore.InspectWorkbenchGrant(f.ctx, r.ID)
	if !reflect.DeepEqual([]byte(unchanged), []byte(second.After)) || auditCount() != 2 {
		t.Fatal("audit failure partially applied")
	}
	f.exec(`DROP TRIGGER reject_test_grant_audit ON access_audit_records; DROP FUNCTION reject_test_grant_audit()`)
	// A live scoped reader/reviewer holds the original lock through commit.
	reader, err := f.admin.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Rollback()
	if _, err = reader.Exec(`LOCK TABLE workbench_access_grants IN ACCESS SHARE MODE; SELECT pg_advisory_xact_lock_shared(6361703472894002481)`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, 150*time.Millisecond)
	defer cancel()
	if _, err = f.consumerStore.ChangeWorkbenchGrant(ctx, r, false); err == nil {
		t.Fatal("changed grant during reader fence")
	}
	reader.Rollback()
	if auditCount() != 2 {
		t.Fatal("cancelled writer left audit")
	}
	third, err := f.consumerStore.ChangeWorkbenchGrant(f.ctx, r, false)
	if err != nil || third.Revision != 3 {
		t.Fatalf("post fence replace %+v %v", third, err)
	}
	rev = 3
	r.Action = "revoke"
	r.Capabilities = []string{}
	wrong := r
	other := "99"
	wrong.SourceProject = &other
	if _, err = f.consumerStore.ChangeWorkbenchGrant(f.ctx, wrong, false); err == nil {
		t.Fatal("revoked different scope")
	}
	beforeCheck, _ := f.consumerStore.InspectWorkbenchGrant(f.ctx, r.ID)
	n := auditCount()
	checked, err := f.consumerStore.ChangeWorkbenchGrant(f.ctx, r, true)
	if err != nil || checked.Changed {
		t.Fatal(err)
	}
	afterCheck, _ := f.consumerStore.InspectWorkbenchGrant(f.ctx, r.ID)
	if !reflect.DeepEqual(beforeCheck, afterCheck) || auditCount() != n {
		t.Fatal("revoke check wrote")
	}
	last, err := f.consumerStore.ChangeWorkbenchGrant(f.ctx, r, false)
	if err != nil || last.Revision != 4 || auditCount() != 4 {
		t.Fatalf("revoke %+v %v", last, err)
	}
	json.Unmarshal(last.After, &after)
	if after["active"] != false {
		t.Fatal("grant remains active")
	}
}

// This vector requires an explicitly supplied disposable PostgreSQL fixture.
// Local content implementation runs only the pure tests, never this fixture.
func TestWorkbenchGrantContentDomainsCheckAndLegacyReplacement(t *testing.T) {
	f := newSourceFixture(t)
	p, all, rev, yes, no := "content-project", "*", int64(0), true, false
	r := db.WorkbenchGrantChange{SchemaVersion: "xinsight.workbench-grant-change.v2", Action: "create", ID: testUUID(t), Subject: f.subject, Source: f.source, SourceProject: &p, CostProject: &all, ExpectedRevision: &rev, Capabilities: []string{"details", "source_runtime"}, AllowContent: &yes, AllowedContentPurposes: []string{"human_review"}}
	snapshot := func() string {
		t.Helper()
		var raw string
		err := f.admin.QueryRow(`SELECT jsonb_build_array(
   (SELECT COALESCE(jsonb_agg(to_jsonb(g) ORDER BY id),'[]') FROM workbench_access_grants g),
   (SELECT COALESCE(jsonb_agg(to_jsonb(a) ORDER BY id),'[]') FROM access_audit_records a),
   (SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY sequencename),'[]') FROM pg_sequences s WHERE schemaname='public'))::text`).Scan(&raw)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	check := func(req db.WorkbenchGrantChange, wantErr bool) {
		t.Helper()
		before := snapshot()
		res, err := f.consumerStore.ChangeWorkbenchGrant(f.ctx, req, true)
		if (err != nil) != wantErr || res.Changed {
			t.Fatalf("check: %+v %v", res, err)
		}
		if after := snapshot(); after != before {
			t.Fatal("check changed rows, audit or sequence")
		}
	}
	apply := func(req db.WorkbenchGrantChange) map[string]any {
		t.Helper()
		res, err := f.consumerStore.ChangeWorkbenchGrant(f.ctx, req, false)
		if err != nil {
			t.Fatal(err)
		}
		var row map[string]any
		if err = json.Unmarshal(res.After, &row); err != nil {
			t.Fatal(err)
		}
		rev = res.Revision
		return row
	}
	expect := func(row map[string]any, legacy, content int64) {
		t.Helper()
		if row["legacy_revision"] != float64(legacy) || row["content_revision"] != float64(content) {
			t.Fatalf("unexpected domain epochs: %v", row)
		}
	}
	// Current-schema support must still reject dirty and future ledgers before
	// either a preview or an application can mutate grants, audits or sequences.
	for _, fence := range []struct {
		name, version string
		dirty         bool
	}{{"dirty_current", "000046_add_connector_sync_control", true}, {"future", "000047_unpublished", false}} {
		t.Run(fence.name, func(t *testing.T) {
			f.exec(`UPDATE schema_migrations SET version=$1,dirty=$2 WHERE version='000046_add_connector_sync_control'`, fence.version, fence.dirty)
			defer f.exec(`UPDATE schema_migrations SET version='000046_add_connector_sync_control',dirty=false WHERE version=$1`, fence.version)
			check(r, true)
			before := snapshot()
			if _, err := f.consumerStore.ChangeWorkbenchGrant(f.ctx, r, false); err == nil {
				t.Fatal("unsupported ledger accepted")
			}
			if snapshot() != before {
				t.Fatal("unsupported ledger changed grants, audits or sequences")
			}
		})
	}
	check(r, false)
	row := apply(r)
	expect(row, 1, 1)
	r.Action = "replace"
	v1 := r
	v1.SchemaVersion = "xinsight.workbench-grant-change.v1"
	v1.AllowContent = nil
	v1.AllowedContentPurposes = nil
	check(v1, true)
	before := snapshot()
	if _, err := f.consumerStore.ChangeWorkbenchGrant(f.ctx, v1, false); err == nil {
		t.Fatal("v1 replaced content grant")
	}
	if snapshot() != before {
		t.Fatal("rejected v1 mutated")
	}
	r.AllowContent = &no
	r.AllowedContentPurposes = []string{}
	check(r, false)
	row = apply(r)
	expect(row, 1, 2)
	r.AllowContent = &yes
	r.AllowedContentPurposes = []string{"human_review"}
	row = apply(r)
	expect(row, 1, 3)
	r.Capabilities = []string{"details"}
	row = apply(r)
	expect(row, 2, 3)
	// v1 full revoke remains all-domain, but its inactive C row is still protected.
	v1.Action = "revoke"
	v1.Capabilities = []string{}
	check(v1, false)
	row = apply(v1)
	expect(row, 3, 4)
	if row["active"] != false || row["allow_content"] != true {
		t.Fatal("full revoke lost its explicit retained state")
	}
	v1.Action = "replace"
	v1.Capabilities = []string{"details"}
	check(v1, true)
	row = apply(r)
	expect(row, 4, 5)
	// Non-CLI writes use the same ALWAYS trigger. C-only and legacy edits isolate.
	f.exec(`UPDATE workbench_access_grants SET allow_content=false,allowed_content_purposes='[]' WHERE id=$1::uuid`, r.ID)
	var legacy, content, whole int64
	read := func() {
		t.Helper()
		if err := f.admin.QueryRow(`SELECT legacy_revision,content_revision,revision FROM workbench_access_grants WHERE id=$1::uuid`, r.ID).Scan(&legacy, &content, &whole); err != nil {
			t.Fatal(err)
		}
	}
	read()
	if legacy != 4 || content != 6 || whole != rev+1 {
		t.Fatalf("direct C write %d/%d/%d", legacy, content, whole)
	}
	f.exec(`UPDATE workbench_access_grants SET allow_source_attempt=true WHERE id=$1::uuid`, r.ID)
	read()
	if legacy != 5 || content != 6 || whole != rev+2 {
		t.Fatalf("direct legacy write %d/%d/%d", legacy, content, whole)
	}
	f.exec(`UPDATE workbench_access_grants SET active=false WHERE id=$1::uuid`, r.ID)
	f.exec(`UPDATE workbench_access_grants SET active=true WHERE id=$1::uuid`, r.ID)
	read()
	if legacy != 7 || content != 8 || whole != rev+4 {
		t.Fatalf("direct ABA write %d/%d/%d", legacy, content, whole)
	}
	// A brand-new C-only scope is valid and has no legacy permissions.
	p = "content-only"
	rev = 0
	r.Action = "create"
	r.ID = testUUID(t)
	r.Capabilities = []string{}
	check(r, false)
	row = apply(r)
	expect(row, 1, 1)
	if row["allow_details"] != false || row["allow_source_runtime"] != false {
		t.Fatal("C-only implied old permissions")
	}
}
