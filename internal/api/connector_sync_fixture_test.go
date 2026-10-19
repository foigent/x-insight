package api_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/connectorsync"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/workspace"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// Older browser/component fixtures intentionally exercise continuous collection.
// Declare their exact synthetic binding and Auto preference, rather than relying
// on an unbound worker or weakening the production manual default. The caller
// has already checked its protected producer receipt and loopback/database scope.
func seedBrowserAutomaticSync(t *testing.T, admin *sql.DB, id, source, org, project, feedKey string) {
	t.Helper()
	var exists bool
	if err := admin.QueryRowContext(t.Context(), `SELECT EXISTS(SELECT 1 FROM connector_installations WHERE id=$1)`, id).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		service, err := workspace.NewID("xsi_", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = admin.ExecContext(t.Context(), `INSERT INTO service_identities(id,display_name,status,permissions,created_by_subject,created_at,updated_at)VALUES($1,'Synthetic browser sync','active',ARRAY['connector.control.poll','connector.control.ack'],'system',statement_timestamp(),statement_timestamp())`, service); err != nil {
			t.Fatal(err)
		}
		if _, err = admin.ExecContext(t.Context(), `INSERT INTO connector_installations(id,deployment_id,connector_type,requested_event_scope,final_event_scope,project_id,source_instance,source_instance_sha256,control_service_identity_id,installation_acknowledgement_sha256,status,desired_state,observed_state,created_at,updated_at)VALUES($1,'xid_AAAAAAAAAAAAAAAAAAAAAA','unirouter',ARRAY['model.request.completed'],ARRAY['model.request.completed'],$2,$3,$4,$5,'sha256:'||repeat('a',64),'connected','active','connected',statement_timestamp(),statement_timestamp())`, id, project, source, syncFixtureHash(source), service); err != nil {
			t.Fatal(err)
		}
	}
	var exact bool
	if err := admin.QueryRowContext(t.Context(), `SELECT source_instance=$2 AND project_id=$3 AND status='connected' FROM connector_installations WHERE id=$1`, id, source, project).Scan(&exact); err != nil || !exact {
		t.Fatal("synthetic sync binding mismatch", err)
	}
	if _, err := admin.ExecContext(t.Context(), `INSERT INTO connector_installation_source_bindings(installation_id,source_binding_sha256)VALUES($1,$2)`, id, syncFixtureHash(source+":"+org+":"+project)); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(t.Context(), `INSERT INTO connector_source_enrollments(installation_id,enrollment_id,source_instance,org_id,project_id,revision,status,updated_at)VALUES($1,'xise_1',$2,$3,$4,'1','active',statement_timestamp())`, id, source, org, project); err != nil {
		t.Fatal(err)
	}
	if feedKey != "" {
		hash := sha256.New()
		hash.Write([]byte("unirouter.xinsight.feed-credential.v1"))
		hash.Write([]byte{0})
		hash.Write([]byte(feedKey))
		verifier := hex.EncodeToString(hash.Sum(nil))
		prior := ""
		for _, phase := range []string{"prepare_paused_source", "register_receiver"} {
			op, err := connectorcontrol.NewID("xco_", nil)
			if err != nil {
				t.Fatal(err)
			}
			body := map[string]any{"schema_version": "xinsight.connector-control-operation.v2", "operation_id": op, "installation_id": id, "kind": "connect", "phase": phase, "desired_state": "active", "request": map[string]any{"receiver_registration": map[string]any{"verifier_sha256": verifier}}}
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
			if _, err = admin.ExecContext(t.Context(), `INSERT INTO connector_control_operations(operation_id,installation_id,operation_kind,desired_state,request_sha256,request_payload,status,available_at,result_sha256,result_payload,created_at,updated_at,completed_at,phase,predecessor_operation_id,predecessor_result_sha256)VALUES($1,$2,'connect','active',$3,$4::jsonb,'completed',statement_timestamp(),'sha256:'||repeat('b',64),'{}',statement_timestamp(),statement_timestamp(),statement_timestamp(),$5,$6,$7)`, op, id, syncFixtureHash(string(raw)), string(raw), phase, predecessor, preHash); err != nil {
				t.Fatal(err)
			}
			prior = op
		}
	}
	if _, err := admin.ExecContext(t.Context(), `UPDATE connector_sync_controls SET auto_sync=true WHERE installation_id=$1`, id); err != nil {
		t.Fatal(err)
	}
}
func syncFixtureHash(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(h[:])
}

func TestBrowserSyncFixtureDeclaresExactSyntheticAutoAndReceiver(t *testing.T) {
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires scoped synthetic PostgreSQL")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	control := stdlib.OpenDB(*cfg)
	defer control.Close()
	name := fmt.Sprintf("xi_browser_sync_%d_%d", os.Getpid(), time.Now().UnixNano())
	if _, err = control.ExecContext(t.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = control.ExecContext(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize())
	}()
	cfg.Database = name
	registered := stdlib.RegisterConnConfig(cfg)
	defer stdlib.UnregisterConnConfig(registered)
	if _, err = db.RunMigrations(t.Context(), registered, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*cfg)
	defer admin.Close()
	const id = "xci_4646464646464646464646"
	seedBrowserAutomaticSync(t, admin, id, "synthetic-http", "7", "9", "synthetic-receiver-only")
	gate := connectorsync.New(admin)
	if got, e := gate.Receiver(t.Context(), "synthetic-receiver-only"); e != nil || got != id {
		t.Fatal("receiver binding", got, e)
	}
	if got, e := gate.Resolve(t.Context(), "synthetic-http", "7", "9", false); e != nil || got != id {
		t.Fatal("source binding", got, e)
	}
	if _, e := gate.Resolve(t.Context(), "synthetic-http", "7", "10", false); e == nil {
		t.Fatal("fixture claimed another project")
	}
	status, e := gate.Status(t.Context(), id)
	if e != nil || !status.Auto || status.Cadence != nil {
		t.Fatal("fixture changed original polling cadence", e)
	}
}
