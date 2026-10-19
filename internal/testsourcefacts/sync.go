package testsourcefacts

import (
	"database/sql"
	"strings"
	"testing"
)

// BindSyncScope declares this synthetic fixture's exact owner and collection
// intent. It never changes a previously bound connection's preference.
func BindSyncScope(t *testing.T, admin *sql.DB, source, org, project string, automatic bool) string {
	t.Helper()
	ctx := t.Context()
	rows, err := admin.QueryContext(ctx, `SELECT public.xinsight_sync_scope($1,$2,$3,false)`, source, org, project)
	if err != nil {
		t.Fatal(err)
	}
	var existing string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil || existing != "" {
			t.Fatal("ambiguous synthetic sync scope", err)
		}
		existing = id
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	if existing != "" {
		return existing
	}
	var suffix string
	if err = admin.QueryRowContext(ctx, `SELECT gen_random_uuid()::text`).Scan(&suffix); err != nil {
		t.Fatal(err)
	}
	suffix = strings.ReplaceAll(suffix, "-", "")[:22]
	id, identity := "xci_"+suffix, "xsi_"+suffix
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO service_identities(id,display_name,status,permissions,created_by_subject,created_at,updated_at) VALUES($1,'synthetic sync fixture','active',ARRAY['connector.control.poll','connector.control.ack'],'system',clock_timestamp(),clock_timestamp())`, []any{identity}},
		{`INSERT INTO connector_installations(id,deployment_id,connector_type,requested_event_scope,final_event_scope,project_id,source_instance,source_instance_sha256,control_service_identity_id,installation_acknowledgement_sha256,status,desired_state,observed_state,created_at,updated_at) VALUES($1,'xid_0000000000000000000000','unirouter',ARRAY['model.request.completed'],ARRAY['model.request.completed'],$2,$3,'sha256:'||repeat('a',64),$4,'sha256:'||repeat('b',64),'connected','active','connected',clock_timestamp(),clock_timestamp())`, []any{id, project, source, identity}},
		{`INSERT INTO connector_installation_source_bindings(installation_id,source_binding_sha256) VALUES($1,'sha256:'||repeat('c',64))`, []any{id}},
		{`INSERT INTO connector_source_enrollments(installation_id,enrollment_id,source_instance,org_id,project_id,revision,status,updated_at) VALUES($1,'xise_1',$2,$3,$4,'1','active',clock_timestamp())`, []any{id, source, org, project}},
	}
	for _, s := range statements {
		if _, err = admin.ExecContext(ctx, s.query, s.args...); err != nil {
			t.Fatal(err)
		}
	}
	if automatic {
		if _, err = admin.ExecContext(ctx, `UPDATE connector_sync_controls SET auto_sync=true WHERE installation_id=$1`, id); err != nil {
			t.Fatal(err)
		}
	}
	return id
}
