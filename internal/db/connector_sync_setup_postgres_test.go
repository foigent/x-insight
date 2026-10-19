package db_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/sourcefacts"
)

// Hold the same control lock used by BeginControl without updating the control
// row. Initial must wait, then see the new receipt in a NEW statement snapshot.
// Terminal case proves the receipt cap; running case proves admission exclusion.
func TestInitialConnectorSyncRechecksAdmissionAfterControlLockWait(t *testing.T) {
	for _, state := range []string{"completed", "running"} {
		t.Run(state, func(t *testing.T) {
			f := newSourceFixture(t)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			id, wake := seedInitialSyncConsent(t, f)
			f.exec(`INSERT INTO connector_sync_runs(installation_id,request_id,revision,started_at,deadline,state,updated_by) SELECT $1,'prior-'||i::text,1,statement_timestamp()-interval '61 seconds',statement_timestamp()-interval '1 second','expired','synthetic-prior' FROM generate_series(1,31) i`, id)
			holder, err := f.admin.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			defer holder.Rollback()
			var holderPID int
			var revision int64
			if err = holder.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&holderPID); err != nil {
				t.Fatal(err)
			}
			if err = holder.QueryRowContext(ctx, `SELECT revision FROM connector_sync_controls WHERE installation_id=$1 FOR UPDATE`, id).Scan(&revision); err != nil {
				t.Fatal(err)
			}
			if _, err = holder.ExecContext(ctx, `INSERT INTO connector_sync_runs(installation_id,request_id,revision,started_at,deadline,state,updated_by,results)VALUES($1,'control:xipw_46464646464646464646464646464646',$2,statement_timestamp(),statement_timestamp()+interval '60 seconds',$3,'xs_4646464646464646',jsonb_build_object('request_sha256','sha256:'||repeat('a',64)))`, id, revision, state); err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() { result <- f.workspace.BeginInitialConnectorSync(ctx, id, wake) }()
			blocked := false
			for !blocked {
				if err = f.admin.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND position('connector_sync_controls' in query)>0 AND $1=ANY(pg_blocking_pids(pid)))`, holderPID).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if !blocked {
					select {
					case early := <-result:
						t.Fatalf("initial did not wait for control lock: %v", early)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(5 * time.Millisecond):
					}
				}
			}
			if err = holder.Commit(); err != nil {
				t.Fatal(err)
			}
			initialErr := <-result
			var count, initial, running int
			var used bool
			if err = f.admin.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM connector_sync_runs WHERE installation_id=$1),(SELECT count(*) FROM connector_sync_runs WHERE installation_id=$1 AND request_id='initial:'||$1),(SELECT count(*) FROM connector_sync_runs WHERE installation_id=$1 AND state='running'),setup_used FROM connector_sync_controls WHERE installation_id=$1`, id).Scan(&count, &initial, &running, &used); err != nil {
				t.Fatal(err)
			}
			t.Logf("control_state=%s waited_for_exact_holder=true receipts=%d initial_receipts=%d running=%d setup_used=%t initial_error=%v", state, count, initial, running, used, initialErr)
			if initialErr != nil || count != 32 || initial != 0 || used || (state == "running" && running != 1) || (state == "completed" && running != 0) {
				t.Fatal("initial admitted from a snapshot older than the control lock")
			}
		})
	}
}

func seedInitialSyncConsent(t *testing.T, f *sourceFixture) (string, string) {
	t.Helper()
	id := "xci_" + strings.ReplaceAll(testUUID(t), "-", "")[:22]
	f.exec(`INSERT INTO connector_installations(id,deployment_id,connector_type,requested_event_scope,status,desired_state,observed_state,created_at,updated_at)VALUES($1,'xid_0000000000000000000000','unirouter',ARRAY['model.request.completed'],'pending','active','awaiting_consent',statement_timestamp(),statement_timestamp())`, id)
	prior := ""
	for _, phase := range []string{"prepare_paused_source", "register_receiver"} {
		op := "xco_" + strings.ReplaceAll(testUUID(t), "-", "")[:22]
		body := map[string]any{"schema_version": "xinsight.connector-control-operation.v2", "operation_id": op, "installation_id": id, "kind": "connect", "phase": phase, "desired_state": "active", "request": map[string]any{}}
		var predecessor, preHash any
		if prior != "" {
			predecessor, preHash = prior, "sha256:"+strings.Repeat("b", 64)
			body["predecessor_operation_id"] = prior
			body["predecessor_result_sha256"] = preHash
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		f.exec(`INSERT INTO connector_control_operations(operation_id,installation_id,operation_kind,desired_state,request_sha256,request_payload,status,available_at,result_sha256,result_payload,created_at,updated_at,completed_at,phase,predecessor_operation_id,predecessor_result_sha256)VALUES($1,$2,'connect','active',$3,$4::jsonb,'completed',statement_timestamp(),'sha256:'||repeat('b',64),'{}',statement_timestamp(),statement_timestamp(),statement_timestamp(),$5,$6,$7)`, op, id, sourcefacts.Hash(raw), string(raw), phase, predecessor, preHash)
		prior = op
	}
	f.exec(`INSERT INTO connector_installation_invitations(id,installation_id,nonce_digest,token_prefix,audience,xinsight_origin,unirouter_install_origin,requested_event_scope,status,created_by_subject,created_at,expires_at,consumed_at,acknowledgement_sha256)VALUES($1,$2,'sha256:'||repeat('d',64),'xiiv1_AAAAAA','unirouter','https://xi.example','https://ur.example',ARRAY['model.request.completed'],'consumed','xs_4646464646464646',statement_timestamp(),statement_timestamp()+interval '1 hour',statement_timestamp(),'sha256:'||repeat('e',64))`, "xii_"+strings.TrimPrefix(id, "xci_"), id)
	return id, prior
}
