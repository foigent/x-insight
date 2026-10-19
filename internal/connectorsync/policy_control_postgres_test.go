package connectorsync_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/connectorsync"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/urmachine"
	"github.com/foigent/x-insight/internal/reviewpolicycontrol"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type policyGateOwner struct {
	reviewpolicycontrol.OwnerStore
	gate *connectorsync.Store
}

func (o policyGateOwner) SyncStore() *connectorsync.Store { return o.gate }

type policyLedger struct {
	reviewpolicycontrol.UROperationStore
	op db.ReviewCaptureUROperation
}

func (*policyLedger) ValidateReviewCaptureURInstallation(context.Context, string, string) error {
	return nil
}
func (*policyLedger) CurrentReviewCaptureDesiredRevision(context.Context) (int64, error) {
	return 2, nil
}
func (l *policyLedger) ReadReviewCaptureUROperation(_ context.Context, _, ref string) (db.ReviewCaptureUROperation, error) {
	if l.op.OperationRef != ref {
		return db.ReviewCaptureUROperation{}, sql.ErrNoRows
	}
	return l.op, nil
}
func (l *policyLedger) CreateReviewCaptureUROperation(_ context.Context, op db.ReviewCaptureUROperation) error {
	l.op = op
	return nil
}
func (l *policyLedger) RecordReviewCaptureUROperation(_ context.Context, _, _ string, state string, raw json.RawMessage, _ time.Time) error {
	l.op.State, l.op.URResult = state, raw
	return nil
}

func TestExplicitPolicyControlRealHTTPIsFiniteAndDoesNotWakeBackground(t *testing.T) {
	gate, id := install(t, 30)
	ledger := &policyLedger{}
	var calls atomic.Int32
	var unavailable atomic.Bool
	const actor = "xs_1111111111111111"
	const base = "/api/v1/integrations/xinsight/payload-policy"
	var operation string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		// Even while the explicit operation is active, no ordinary worker can
		// consume its run. Count real HTTP through the production policy client.
		for _, worker := range []string{"activation", "projects", "feed", "source", "credentials"} {
			_, finish, err := gate.Begin(t.Context(), id, worker, time.Minute)
			if err == nil {
				finish(nil)
				t.Errorf("control woke %s", worker)
			}
		}
		_, pending, _, err := gate.Schedule(t.Context(), id, time.Minute)
		if err != nil || pending {
			t.Errorf("control scheduled a collector: %v", err)
		}
		if strings.Contains(r.URL.Path, "/operations/") && unavailable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == base+"/projects/9" {
			_, _ = w.Write([]byte(`{"schema_version":"unirouter.payload-policy.v2","source_instance":"ur-local","org_id":"7","project_id":"9","enterprise":{},"project":{"policy_ref":"content-policy:7:9","desired":{"policy_version":"1"}},"version_vector":"{\"enterprise\":1,\"project\":1}"}`))
			return
		}
		if r.Method == http.MethodPut && ledger.op.OperationRef != operation {
			t.Error("real write preceded original durable operation identity")
		}
		_ = json.NewEncoder(w).Encode(urmachine.WriteResult{SchemaVersion: "unirouter.xinsight.policy-write-result.v1", OperationRef: operation, Result: "committed", SourceInstance: "ur-local", PolicyRef: "content-policy:7:9", DesiredVersion: "2", EffectiveVersion: "2", ApplicationStatus: "applied", ReadbackPath: base + "/projects/9"})
	}))
	defer server.Close()
	identity := urmachine.Identity{InstallationID: id, SourceInstance: "ur-local", OrgID: "7"}
	client, err := urmachine.NewPolicyClient(server.URL, "ur-xpm1-synthetic-only", identity, connectorsync.RequireClient(server.Client()))
	if err != nil {
		t.Fatal(err)
	}
	manager := reviewpolicycontrol.Manager{Store: policyGateOwner{gate: gate}, URStore: ledger, URClient: client, URIdentity: identity}
	request := reviewpolicycontrol.Request{SchemaVersion: reviewpolicycontrol.RequestSchema, Action: reviewpolicycontrol.ActionURApplyProject, ActorSubject: actor, RequestedAt: time.Now().UTC(), UR: &reviewpolicycontrol.URRequest{ProjectID: "9", DelegationID: "xipg_1", DelegationRevision: "1", OperationRef: "xipw_30303030303030303030303030303030", Policy: json.RawMessage(`{"schema_version":"unirouter.payload-policy.v2","expected_policy_ref":"content-policy:7:9","expected_version":"1","expected_vector":"{\"enterprise\":1,\"project\":1}","desired":{"mode":"disabled"}}`)}}
	operation = request.UR.OperationRef
	if _, err = manager.Execute(t.Context(), request); !errors.Is(err, urmachine.ErrUnauthorized) || calls.Load() != 0 {
		t.Fatal("worker could mint human control", err, calls.Load())
	}
	manager.AuthorizeControlActor = func(subject string) error {
		if subject != actor {
			return urmachine.ErrUnauthorized
		}
		return nil
	}
	out, err := manager.Execute(t.Context(), request)
	if err != nil || out.State != "effective" || calls.Load() != 3 {
		t.Fatalf("manual explicit policy: %+v %v HTTP=%d", out, err, calls.Load())
	}
	status, err := gate.Status(t.Context(), id)
	if err != nil || status.Auto || status.Revision != 1 || status.Run.State != "completed" || status.Run.Attempts != 3 || status.Run.Pages != 3 {
		t.Fatalf("control changed preference/budget: %+v %v", status, err)
	}
	deadline := status.Run.Deadline
	if _, err = manager.Execute(t.Context(), request); err != nil || calls.Load() != 3 {
		t.Fatal("terminal replay issued HTTP", err, calls.Load())
	}
	read := request
	read.Action = reviewpolicycontrol.ActionURReadOperation
	read.UR = &reviewpolicycontrol.URRequest{OperationRef: operation}
	if _, err = manager.Execute(t.Context(), read); err != nil || calls.Load() != 3 {
		t.Fatal("local terminal read issued HTTP", err)
	}
	status, _ = gate.Status(t.Context(), id)
	if !status.Run.Deadline.Equal(deadline) {
		t.Fatal("replay renewed budget")
	}
	// A lost/unconfirmed readback uses only the same remaining control budget.
	operation = "xipw_31313131313131313131313131313131"
	request.UR.OperationRef = operation
	unavailable.Store(true)
	out, err = manager.Execute(t.Context(), request)
	if err != nil || out.State != "unconfirmed" || calls.Load() != 6 {
		t.Fatalf("unconfirmed: %+v %v %d", out, err, calls.Load())
	}
	status, _ = gate.Status(t.Context(), id)
	deadline = status.Run.Deadline
	// Even knowledge of this real active control's public identifiers does not
	// allow importer to resume it through the supported store entry point.
	cfg, err := pgx.ParseConfig(os.Getenv("XINSIGHT_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database = testDatabase
	cfg.RuntimeParams["role"] = "xinsight_importer"
	importer := stdlib.OpenDB(*cfg)
	defer importer.Close()
	if _, _, err = connectorsync.New(importer).BeginControl(t.Context(), id, operation, actor, ledger.op.RequestSHA256, false); !errors.Is(err, connectorsync.ErrInvalid) {
		t.Fatal("importer resumed human control", err)
	}
	read.UR.OperationRef = operation
	if _, err = manager.Execute(t.Context(), read); err != nil || calls.Load() != 7 {
		t.Fatal("bounded readback failed", err, calls.Load())
	}
	status, _ = gate.Status(t.Context(), id)
	if !status.Run.Deadline.Equal(deadline) || status.Run.Attempts != 4 || status.Auto {
		t.Fatal("readback renewed/expanded control", status)
	}
	if _, err = database.Exec(`UPDATE connector_sync_runs SET started_at=statement_timestamp()-interval '61 seconds',deadline=statement_timestamp()-interval '1 second' WHERE installation_id=$1 AND request_id=$2`, id, "control:"+operation); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.Execute(t.Context(), read); !errors.Is(err, connectorsync.ErrPaused) || calls.Load() != 7 {
		t.Fatal("expired control replay emitted HTTP", err, calls.Load())
	}
	if _, err = manager.Execute(t.Context(), request); !errors.Is(err, connectorsync.ErrPaused) || calls.Load() != 7 {
		t.Fatal("expired mutation renewed budget", err, calls.Load())
	}
}

func TestControlReceiptBoundCannotEvictReplayProtection(t *testing.T) {
	gate, id := install(t, 31)
	if _, err := database.Exec(`INSERT INTO connector_sync_runs(installation_id,request_id,revision,started_at,deadline,state,updated_by,results) SELECT $1,'control:xipw_'||lpad(i::text,32,'0'),1,statement_timestamp()-interval '61 seconds',statement_timestamp()-interval '1 second','expired','xs_1111111111111111',jsonb_build_object('request_sha256','sha256:'||repeat('a',64)) FROM generate_series(1,32) i`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := gate.Change(t.Context(), request(id, 1, 31, "once")); !errors.Is(err, connectorsync.ErrConflict) {
		t.Fatal("receipt cap silently evicted unprotected control keys", err)
	}
	if _, _, err := gate.BeginControl(t.Context(), id, "xipw_00000000000000000000000000000001", "xs_1111111111111111", "sha256:"+strings.Repeat("a", 64), true); !errors.Is(err, connectorsync.ErrPaused) {
		t.Fatal("expired key renewed", err)
	}
	var count int
	if err := database.QueryRow(`SELECT count(*) FROM connector_sync_runs WHERE installation_id=$1`, id).Scan(&count); err != nil || count != 32 {
		t.Fatal("receipt bound", count, err)
	}
}
