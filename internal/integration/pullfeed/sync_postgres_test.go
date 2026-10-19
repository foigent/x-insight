package pullfeed

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
)

func TestSyncFirstFeedActivationAndRestartUseRequiredPermit(t *testing.T) {
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("scoped synthetic PG required")
	}
	ctx := t.Context()
	if _, e := db.RunMigrations(ctx, dsn, "../../../migrations"); e != nil {
		t.Fatal(e)
	}
	database, e := sql.Open("pgx", dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer database.Close()
	sourceID, adapterID := stateTestUUID(t), stateTestUUID(t)
	id := "xci_" + strings.ReplaceAll(stateTestUUID(t), "-", "")[:22]
	if _, e = database.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name)VALUES($1,$2,'unirouter','sync activation fixture')`, sourceID, "sync:"+sourceID); e != nil {
		t.Fatal(e)
	}
	if _, e = database.ExecContext(ctx, `INSERT INTO adapter_manifests(id,adapter_id,adapter_version,source_type,fidelity_level,payload_behavior,idempotency_strategy)VALUES($1,$2,'1.0.0','unirouter','F2','sanitized','event_hash')`, adapterID, "sync:"+adapterID); e != nil {
		t.Fatal(e)
	}
	if _, e = database.ExecContext(ctx, `INSERT INTO connector_installations(id,deployment_id,connector_type,requested_event_scope,status,desired_state,observed_state,created_at,updated_at)VALUES($1,'xid_0000000000000000000000','unirouter',ARRAY['model.request.completed'],'pending','active','awaiting_consent',statement_timestamp(),statement_timestamp());`, id); e != nil {
		t.Fatal(e)
	}
	if _, e = database.ExecContext(ctx, `INSERT INTO connector_installation_source_bindings(installation_id,source_binding_sha256)VALUES($1,'sha256:'||repeat('a',64))`, id); e != nil {
		t.Fatal(e)
	}
	// Synthetic accepted receiver-phase records exercise the real, restricted
	// resolver. Full redemption/ACK authentication has its existing separate tests.
	credential := "sync-fixture-service-key-" + id
	hash := sha256.New()
	hash.Write([]byte("unirouter.xinsight.feed-credential.v1"))
	hash.Write([]byte{0})
	hash.Write([]byte(credential))
	verifier := hex.EncodeToString(hash.Sum(nil))
	prior := ""
	for _, phase := range []string{"prepare_paused_source", "register_receiver"} {
		op := "xco_" + strings.ReplaceAll(stateTestUUID(t), "-", "")[:22]
		body := map[string]any{"schema_version": "xinsight.connector-control-operation.v2", "request": map[string]any{"receiver_registration": map[string]any{"verifier_sha256": verifier}}}
		body["operation_id"] = op
		body["installation_id"] = id
		body["kind"] = "connect"
		body["phase"] = phase
		body["desired_state"] = "active"
		if prior != "" {
			body["predecessor_operation_id"] = prior
			body["predecessor_result_sha256"] = "sha256:" + strings.Repeat("b", 64)
		}
		raw, _ := json.Marshal(body)
		digest := sha256.Sum256([]byte(phase))
		var predecessor, preHash any
		if prior != "" {
			predecessor = prior
			preHash = "sha256:" + strings.Repeat("b", 64)
		}
		if _, e = database.ExecContext(ctx, `INSERT INTO connector_control_operations(operation_id,installation_id,operation_kind,desired_state,request_sha256,request_payload,status,available_at,result_sha256,result_payload,created_at,updated_at,completed_at,phase,predecessor_operation_id,predecessor_result_sha256)VALUES($1,$2,'connect','active',$3,$4::jsonb,'completed',statement_timestamp(),'sha256:'||repeat('b',64),'{}',statement_timestamp(),statement_timestamp(),statement_timestamp(),$5,$6,$7)`, op, id, "sha256:"+hex.EncodeToString(digest[:]), string(raw), phase, predecessor, preHash); e != nil {
			t.Fatal(e)
		}
		prior = op
	}
	gate := connectorsync.New(database)
	resolved, e := gate.Receiver(ctx, credential)
	if e != nil || resolved != id {
		t.Fatal("receiver before state", resolved, e)
	}
	if _, e = gate.Receiver(ctx, "unrelated-synthetic-key"); !errors.Is(e, connectorsync.ErrBinding) {
		t.Fatal("unknown receiver bound", e)
	}
	var calls atomic.Int32
	now := time.Now().UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write(mustLivePage(t, FeedPage{SchemaVersion: SchemaVersion, SourceInstance: "sync-initial-source", GeneratedAt: now, Items: []FeedItem{}, NextCursor: "initial-cursor", HighWatermark: "initial-cursor"}))
	}))
	defer server.Close()
	httpSource, e := NewHTTPSource(HTTPSourceConfig{SyncRequired: true, Endpoint: server.URL, ServiceKey: credential, AllowPrivateTunnelHTTP: true})
	if e != nil {
		t.Fatal(e)
	}
	state, e := NewPostgresStateStore(database, sourceID, adapterID)
	if e != nil {
		t.Fatal(e)
	}
	begin := func(ctx context.Context, unit string) (context.Context, func(error), error) {
		bound, e := gate.Receiver(ctx, credential)
		if e != nil {
			return ctx, nil, e
		}
		return gate.Begin(ctx, bound, "feed", time.Second, unit)
	}
	worker, e := NewLiveWorker(httpSource, state, LiveCommitFunc(func(context.Context, FeedPage, FeedItem) (LiveItemCommitResult, error) {
		t.Fatal("empty activation committed item")
		return LiveItemCommitResult{}, nil
	}), LiveWorkerConfig{PollInterval: time.Second, AuthorizationRef: AcceptedLiveContractRef, BeginActivation: func(ctx context.Context) (context.Context, func(error), error) { return begin(ctx, "feed:init") }, BeginSync: func(ctx context.Context, _ PostgresWorkerState) (context.Context, func(error), error) {
		return begin(ctx, "feed")
	}})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = worker.ensureActivation(ctx); !errors.Is(e, connectorsync.ErrPaused) || calls.Load() != 0 {
		t.Fatal("manual first install sent HTTP", calls.Load(), e)
	}
	if _, e = state.Load(ctx); !errors.Is(e, ErrWorkerStateNotFound) {
		t.Fatal("manual installed checkpoint without permission", e)
	}

	nonceDigest := sha256.Sum256([]byte("sync-fixture-invitation:" + id))
	if _, e = database.ExecContext(ctx, `INSERT INTO connector_installation_invitations(id,installation_id,nonce_digest,token_prefix,audience,xinsight_origin,unirouter_install_origin,requested_event_scope,status,created_by_subject,created_at,expires_at,consumed_at,acknowledgement_sha256)VALUES($1,$2,$3,'xiiv1_AAAAAA','unirouter','https://xi.example','https://ur.example',ARRAY['model.request.completed'],'consumed','xs_4646464646464646',statement_timestamp(),statement_timestamp()+interval '1 hour',statement_timestamp(),'sha256:'||repeat('e',64))`, "xii_"+strings.TrimPrefix(id, "xci_"), id, "sha256:"+hex.EncodeToString(nonceDigest[:])); e != nil {
		t.Fatal(e)
	}
	owner, e := db.OpenWorkspaceStore(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer owner.Close()
	// Initial Connect shares the same finite receipt cap. It cannot append a
	// 33rd receipt or consume setup consent while the bound is full.
	if _, e = database.ExecContext(ctx, `INSERT INTO connector_sync_runs(installation_id,request_id,revision,started_at,deadline,state,updated_by) SELECT $1,'capacity-'||i::text,1,statement_timestamp()-interval '61 seconds',statement_timestamp()-interval '1 second','expired','synthetic-capacity' FROM generate_series(1,32) i`, id); e != nil {
		t.Fatal(e)
	}
	if e = owner.BeginInitialConnectorSync(ctx, id, prior); e != nil {
		t.Fatal(e)
	}
	var count int
	var unused bool
	if e = database.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM connector_sync_runs WHERE installation_id=$1),NOT setup_used FROM connector_sync_controls WHERE installation_id=$1`, id).Scan(&count, &unused); e != nil || count != 32 || !unused || calls.Load() != 0 {
		t.Fatal("initial Connect exceeded receipt cap or consumed consent", count, unused, e)
	}
	// Only this fixture's own 32 synthetic capacity rows are removed.
	if _, e = database.ExecContext(ctx, `DELETE FROM connector_sync_runs WHERE installation_id=$1 AND request_id LIKE 'capacity-%' AND updated_by='synthetic-capacity'`, id); e != nil {
		t.Fatal(e)
	}
	if e = owner.BeginInitialConnectorSync(ctx, id, prior); e != nil {
		t.Fatal(e)
	}
	initial, e := gate.Status(ctx, id)
	if e != nil || initial.Auto || initial.Run == nil || initial.Run.State != "running" {
		t.Fatal("finite Connect did not start in manual mode", e)
	}
	deadline := initial.Run.Deadline
	if e = owner.BeginInitialConnectorSync(ctx, id, prior); e != nil {
		t.Fatal(e)
	}
	replayed, e := gate.Status(ctx, id)
	if e != nil || !replayed.Run.Deadline.Equal(deadline) {
		t.Fatal("ACK replay renewed setup budget", e)
	}
	if _, e = worker.ensureActivation(ctx); e != nil || calls.Load() != 1 {
		t.Fatal("original bounded Connect failed", calls.Load(), e)
	}
	stop := connectorsync.Request{SchemaVersion: connectorsync.LocalSchema, Action: "set", InstallationID: id, Actor: "xs_4646464646464646", ExpectedRevision: initial.Revision, RequestID: stateTestUUID(t)}
	stopped, e := gate.Change(ctx, stop)
	if e != nil {
		t.Fatal(e)
	}
	if e = owner.BeginInitialConnectorSync(ctx, id, prior); e != nil {
		t.Fatal(e)
	}
	afterReplay, e := gate.Status(ctx, id)
	if e != nil || afterReplay.Auto || afterReplay.Run.State == "running" {
		t.Fatal("ACK restored expired/cancelled setup", e)
	}
	// Remove only this synthetic empty checkpoint to cover Sync once itself
	// performing first initialization, independently of the original Connect.
	if _, e = database.ExecContext(ctx, `DELETE FROM adapter_import_states WHERE source_system_id=$1 AND adapter_manifest_id=$2`, sourceID, adapterID); e != nil {
		t.Fatal(e)
	}
	r := connectorsync.Request{SchemaVersion: connectorsync.LocalSchema, Action: "once", InstallationID: id, Actor: "xs_4646464646464646", ExpectedRevision: stopped.Revision, RequestID: stateTestUUID(t)}
	status, e := gate.Change(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = worker.ensureActivation(ctx); e != nil || calls.Load() != 2 {
		t.Fatal("explicit finite restart failed", calls.Load(), e)
	}
	if _, e = worker.ensureActivation(ctx); e != nil || calls.Load() != 2 {
		t.Fatal("restart silently fetched activation again", calls.Load(), e)
	}
	if _, e = worker.PollOnce(ctx); e != nil || calls.Load() != 3 {
		t.Fatal("permitted first poll", calls.Load(), e)
	}
	r.Action = "set"
	r.ExpectedRevision = status.Revision
	r.RequestID = stateTestUUID(t)
	if _, e = gate.Change(ctx, r); e != nil {
		t.Fatal(e)
	}
	if _, e = worker.PollOnce(ctx); !errors.Is(e, connectorsync.ErrPaused) || calls.Load() != 3 {
		t.Fatal("restart/poll bypassed manual", calls.Load(), e)
	}
}
