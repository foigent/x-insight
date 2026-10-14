package db_test

import (
	"bytes"
	"context"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/checkpointepoch"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
)

func TestManagedAdoptionPostgresUsedEpochReceiptRecoveryAndNaturalFeedRead(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	createConnectorTestRoles(t, ctx, base)
	dsn := connectorPullMigrationDatabaseDSN(t, ctx, base)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store, err := db.OpenWorkspaceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().Truncate(time.Microsecond)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/integrations/xinsight/completed-executions" || r.Header.Get("Authorization") == "" {
			t.Error("unexpected production feed request")
			w.WriteHeader(403)
			return
		}
		json.NewEncoder(w).Encode(pullfeed.FeedPage{SchemaVersion: pullfeed.SchemaVersion, SourceInstance: "synthetic-source", GeneratedAt: now, Items: []pullfeed.FeedItem{}, NextCursor: "manual-cursor", HighWatermark: "manual-cursor"})
	}))
	defer server.Close()
	inv, _, err := store.CreateInvitation(ctx, connectorcontrol.CreateInvitationInput{ActorSubject: "xs_0123456789abcdef", Config: connectorcontrol.InvitationConfig{DeploymentID: "xid_AAAAAAAAAAAAAAAAAAAAAA", XInsightOrigin: "https://xinsight.example", UniRouterInstallOrigin: server.URL}, Now: now, Random: bytes.NewReader(bytes.Repeat([]byte{0x11}, 64))})
	if err != nil {
		t.Fatal(err)
	}
	boot := connectorcontrol.ReceiverAdoptionBootstrap{InstallationID: inv.InstallationID, OrgID: 2, ProjectID: 86, SourceInstance: "synthetic-source", SourceBindingSHA256: "sha256:" + strings.Repeat("c", 64), FeedKeyID: "105", AllowedIP: "192.0.2.10/32", KeyPrefix: "ur-xf1-0123456789abc", RegistrationOperationID: strings.Repeat("2", 32), VerifierScheme: "receiver_sha256_v1"}
	boot.SourceBindingSHA256, err = connectorcontrol.SourceBindingDigest(inv.DeploymentID, boot)
	if err != nil {
		t.Fatal(err)
	}
	bootSHA, _, _ := connectorcontrol.CanonicalSHA256(boot)
	r := connectorcontrol.RedeemInvitationInputV2{SchemaVersion: connectorcontrol.RedeemInvitationSchemaV2, RedeemInvitationInput: connectorcontrol.RedeemInvitationInput{InvitationToken: inv.Token, ProjectID: "synthetic-control-project", FinalEventScope: []string{connectorcontrol.CompletedEventType}, ConsentSHA256: boot.SourceBindingSHA256, ConfirmedAt: now, ControlToken: "xib_" + strings.Repeat("A", 43), ControlTokenExpires: now.Add(24 * time.Hour)}, ReceiverAdoptionBootstrap: boot, ReceiverAdoptionBootstrapSHA256: bootSHA}
	first, err := store.RedeemAdoptionInvitation(ctx, r, now, nil)
	if err != nil {
		t.Fatal("bootstrap", err)
	}
	second, err := store.RedeemAdoptionInvitation(ctx, r, now.Add(time.Second), nil)
	if err != nil || second.Installation.ControlServiceIdentityID != first.Installation.ControlServiceIdentityID {
		t.Fatal("exact bootstrap replay", err)
	}
	var operationCount int
	if err = database.QueryRowContext(ctx, `SELECT count(*) FROM connector_control_operations`).Scan(&operationCount); err != nil || operationCount != 0 {
		t.Fatal("bootstrap created operation", err)
	}
	if _, err := store.RedeemInvitation(ctx, r.RedeemInvitationInput, now, nil); err == nil {
		t.Fatal("legacy redeem accepted bootstrap invitation")
	}
	changed := r
	changed.ReceiverAdoptionBootstrap.FeedKeyID = "106"
	changed.ReceiverAdoptionBootstrapSHA256, _, _ = connectorcontrol.CanonicalSHA256(changed.ReceiverAdoptionBootstrap)
	if _, err := store.RedeemAdoptionInvitation(ctx, changed, now, nil); err == nil {
		t.Fatal("non-exact bootstrap replay")
	}
	// Use the actual M20 transition to produce epoch 2 and retained history.
	_, err = database.ExecContext(ctx, `INSERT INTO source_systems(id,source_key,source_type,display_name) VALUES($1,'unirouter','unirouter','Synthetic UR') ON CONFLICT DO NOTHING`, unirouter.DefaultSourceSystemID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.ExecContext(ctx, `INSERT INTO adapter_manifests(id,adapter_id,adapter_version,source_type,fidelity_level,payload_behavior,idempotency_strategy) VALUES($1,'synthetic-ur','1.0.0','unirouter','F2','metadata_only_default','source_ref') ON CONFLICT DO NOTHING`, unirouter.DefaultCompletedExecutionAdapterManifestID)
	if err != nil {
		t.Fatal(err)
	}
	state, err := pullfeed.NewPostgresStateStore(database, unirouter.DefaultSourceSystemID, unirouter.DefaultCompletedExecutionAdapterManifestID)
	if err != nil {
		t.Fatal(err)
	}
	active, err := state.Activate(ctx, pullfeed.DurableActivation{SourceInstance: "old-source", Cursor: "old-cursor", HighWatermark: "old-cursor", ActivatedAt: now.Add(-time.Hour), AuthorizationRef: "decision://old-manual"})
	if err != nil {
		t.Fatal(err)
	}
	if err = state.MarkInactive(ctx, active.Cursor, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	epoch, err := checkpointepoch.NewStore(database)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := epoch.Prepare(ctx, checkpointepoch.PrepareInput{Binding: checkpointepoch.Binding{SourceSystemID: unirouter.DefaultSourceSystemID, AdapterManifestID: unirouter.DefaultCompletedExecutionAdapterManifestID, CursorKey: pullfeed.CompletedExecutionCursorKey}, WorkerUnit: checkpointepoch.CompletedExecutionWorkerUnit, NextSourceInstance: boot.SourceInstance, NextCursor: "manual-cursor", NextHighWatermark: "manual-cursor", AuthorizationRef: "decision://used-manual-epoch2"}, strings.Repeat("3", 32), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = epoch.Activate(ctx, prepared.Operation.OperationID, prepared.Operation.Plan.PlanSHA256, now); err != nil {
		t.Fatal(err)
	}
	if err = state.MarkPollStarted(ctx, "manual-cursor", now); err != nil {
		t.Fatal(err)
	}
	if err = state.CommitPage(ctx, "manual-cursor", pullfeed.PageCommit{NextCursor: "manual-cursor", HighWatermark: "manual-cursor", CompletedAt: now, ItemsReceived: 3, ItemsCommitted: 3}); err != nil {
		t.Fatal(err)
	}
	boundary, err := store.InspectManagedConnector(ctx, inv.InstallationID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join("..", "connectorcontrol", "testdata", "control-v3", "valid", "01-bind-existing-operation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var op connectorcontrol.ControlOperationRequestV3
	if err = json.Unmarshal(raw, &op); err != nil {
		t.Fatal(err)
	}
	op.InstallationID = inv.InstallationID
	op.Request.Candidate.SourceBindingSHA256 = boot.SourceBindingSHA256
	op.Request.Candidate.Checkpoint = *boundary.Checkpoint
	op.Request.Candidate.WorkerConfig.UROrigin = server.URL
	op.Request.CandidateSHA256, _, _ = connectorcontrol.CanonicalSHA256(*op.Request.Candidate)
	queued, err := store.EnqueueManagedControlOperation(ctx, op, now)
	if err != nil {
		t.Fatal("enqueue bind", err)
	}
	identity := first.Installation.ControlServiceIdentityID
	if _, err = store.PollControlOperation(ctx, identity, inv.InstallationID, now); err != nil {
		t.Fatal("poll bind", err)
	}
	bind := connectorcontrol.ReceiverAdoptionBinding{InstallationID: inv.InstallationID, OrgID: 2, ProjectID: 86, OperationID: queued.OperationID, FeedKeyID: "105", CandidateSHA256: op.Request.CandidateSHA256}
	bindSHA, _, _ := connectorcontrol.CanonicalSHA256(bind)
	result := connectorcontrol.ControlResultV3{SchemaVersion: connectorcontrol.ControlResultSchemaV3, OperationID: op.OperationID, InstallationID: op.InstallationID, Kind: op.Kind, Phase: op.Phase, Status: "completed", ObservedAt: now, Result: &connectorcontrol.ControlPhaseResultV3{SourceInstance: boot.SourceInstance, CandidateSHA256: op.Request.CandidateSHA256, AdoptionBinding: &bind, AdoptionBindingSHA256: bindSHA, SourceState: "active", ReceiverState: "feed_active", Readiness: "existing_receiver_bound"}}
	wrong := result
	wrongResult := *result.Result
	wrongBinding := bind
	wrongBinding.OrgID = 3
	wrongResult.AdoptionBinding = &wrongBinding
	wrongResult.AdoptionBindingSHA256, _, _ = connectorcontrol.CanonicalSHA256(wrongBinding)
	wrong.Result = &wrongResult
	if _, err = store.CompleteManagedControlOperation(ctx, identity, wrong, now); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatal("wrong UR scope accepted", err)
	}
	// ObservedAt is UR's own host time, not a timestamp ordered against XI's
	// operation creation. XI's committed receipt continues to use its own clock.
	result.ObservedAt = now.Add(-500 * time.Millisecond)
	if _, err = store.CompleteManagedControlOperation(ctx, identity, result, now); err != nil {
		t.Fatal("bind ACK", err)
	}
	boundary, err = store.InspectManagedConnector(ctx, inv.InstallationID)
	if err != nil || boundary.LocalReceipt == nil || boundary.LocalReceipt.PagesCommittedBefore != 1 || boundary.Checkpoint.ConsumerEpoch != 2 {
		t.Fatal("atomic receipt", err)
	}
	receiptSHA, _, _ := connectorcontrol.CanonicalSHA256(*boundary.LocalReceipt)
	for _, field := range []string{"bind_operation_id", "bind_result_sha256", "local_receipt", "local_receipt_sha256"} {
		if _, err = database.ExecContext(ctx, `UPDATE connector_receiver_adoptions SET `+field+`=NULL WHERE installation_id=$1`, inv.InstallationID); err == nil {
			t.Fatal("bound required field accepted NULL", field)
		}
	}
	for _, field := range []string{"bootstrap", "bootstrap_sha256", "redeem_request_sha256"} {
		if _, err = database.ExecContext(ctx, `UPDATE connector_installation_source_bindings SET `+field+`=NULL WHERE installation_id=$1`, inv.InstallationID); err == nil {
			t.Fatal("bootstrap required field accepted NULL", field)
		}
	}
	// Fresh store represents controller recovery. Replay must not rewrite receipt.
	recovered, err := db.OpenWorkspaceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if _, err = recovered.CompleteManagedControlOperation(ctx, identity, result, now.Add(time.Second)); err != nil {
		t.Fatal("ACK recovery", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	source, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: server.URL + "/api/v1/integrations/xinsight/completed-executions", ServiceKey: "ur-xf1-" + strings.Repeat("1", 64), TLSRootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := pullfeed.NewLiveWorker(source, state, pullfeed.LiveCommitFunc(func(context.Context, pullfeed.FeedPage, pullfeed.FeedItem) (pullfeed.LiveItemCommitResult, error) {
		t.Fatal("empty page invoked item commit")
		return pullfeed.LiveItemCommitResult{}, nil
	}), pullfeed.LiveWorkerConfig{PollInterval: time.Second, Now: func() time.Time { return now.Add(2 * time.Second) }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = worker.PollOnce(ctx); err != nil {
		t.Fatal("actual HTTPS Fetch/Decode/Commit", err)
	}
	boundary, err = recovered.InspectManagedConnector(ctx, inv.InstallationID)
	if err != nil || boundary.FeedRead == nil || boundary.FeedRead.PagesCommitted != 2 {
		t.Fatal("natural read receipt", err)
	}
	replayedSHA, _, _ := connectorcontrol.CanonicalSHA256(*boundary.LocalReceipt)
	if replayedSHA != receiptSHA {
		t.Fatal("local receipt changed on replay")
	}
	readSHA, _, _ := connectorcontrol.CanonicalSHA256(*boundary.FeedRead)
	resultSHA, _, _ := connectorcontrol.CanonicalSHA256(result)
	confirm := connectorcontrol.ControlOperationRequestV3{SchemaVersion: connectorcontrol.ControlOperationSchemaV3, OperationID: "xco_CCCCCCCCCCCCCCCCCCCCCC", InstallationID: inv.InstallationID, ProjectID: r.ProjectID, EventScope: r.FinalEventScope, DesiredState: "active", Kind: connectorcontrol.KindAdoptExistingReceiver, Phase: connectorcontrol.PhaseConfirmExistingReceiver, PredecessorOperationID: op.OperationID, PredecessorResultSHA256: resultSHA, Request: connectorcontrol.ControlPhaseRequestV3{AdoptionBindingSHA256: bindSHA, XIAdoptionReceipt: boundary.LocalReceipt, XIAdoptionReceiptSHA256: receiptSHA, FeedReadReceipt: boundary.FeedRead, FeedReadReceiptSHA256: readSHA}}
	if _, err = recovered.EnqueueManagedControlOperation(ctx, confirm, now.Add(3*time.Second)); err != nil {
		t.Fatal("confirm enqueue", err)
	}
	if _, err = recovered.PollControlOperation(ctx, identity, inv.InstallationID, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	confirmed := connectorcontrol.ControlResultV3{SchemaVersion: connectorcontrol.ControlResultSchemaV3, OperationID: confirm.OperationID, InstallationID: confirm.InstallationID, Kind: confirm.Kind, Phase: confirm.Phase, Status: "completed", ObservedAt: now.Add(3 * time.Second), Result: &connectorcontrol.ControlPhaseResultV3{SourceInstance: boot.SourceInstance, AdoptionBindingSHA256: bindSHA, XIAdoptionReceiptSHA256: receiptSHA, FeedReadReceiptSHA256: readSHA, SourceState: "active", ReceiverState: "feed_active", Readiness: "existing_receiver_adopted"}}
	connected, err := recovered.CompleteManagedControlOperation(ctx, identity, confirmed, now.Add(3*time.Second))
	if err != nil || connected.Status != "connected" {
		t.Fatal("confirmed", err)
	}
	for _, field := range []string{"confirm_operation_id", "confirm_result_sha256"} {
		if _, err = database.ExecContext(ctx, `UPDATE connector_receiver_adoptions SET `+field+`=NULL WHERE installation_id=$1`, inv.InstallationID); err == nil {
			t.Fatal("confirmed required field accepted NULL", field)
		}
	}
	current, err := state.Load(ctx)
	if err != nil || current.CheckpointEpoch != 2 || current.CheckpointOperationID != strings.Repeat("3", 32) || current.Cursor != "manual-cursor" || current.PagesCommitted != 2 || current.ItemsCommitted != 3 || current.AuthorizationRef != "decision://used-manual-epoch2" {
		t.Fatal("manual state changed", err)
	}
	var historyCount int
	if err = database.QueryRowContext(ctx, `SELECT count(*) FROM connector_checkpoint_epoch_history`).Scan(&historyCount); err != nil || historyCount != 1 {
		t.Fatal("epoch history changed", err)
	}
	confirmSHA, _, _ := connectorcontrol.CanonicalSHA256(confirmed)
	for n, files := range [][2]string{{"05-policy-pair-operation.json", "06-policy-pair-result.json"}, {"07-source-enroll-pair-operation.json", "08-source-enroll-pair-result.json"}} {
		wire, err := os.ReadFile(filepath.Join("..", "connectorcontrol", "testdata", "control-v3", "valid", files[0]))
		if err != nil {
			t.Fatal(err)
		}
		var pair connectorcontrol.ControlOperationRequestV3
		json.Unmarshal(wire, &pair)
		pair.InstallationID = inv.InstallationID
		pair.PredecessorOperationID = confirm.OperationID
		pair.PredecessorResultSHA256 = confirmSHA
		pair.Request.SourceBindingSHA256 = boot.SourceBindingSHA256
		pair.Request.Registration.InstallationID = inv.InstallationID
		pair.Request.CustodyReceipt.InstallationID = inv.InstallationID
		pair.Request.RegistrationSHA256, _, _ = connectorcontrol.CanonicalSHA256(*pair.Request.Registration)
		pair.Request.CustodyReceipt.RegistrationSHA256 = pair.Request.RegistrationSHA256
		pair.Request.CustodyReceiptSHA256, _, _ = connectorcontrol.CanonicalSHA256(*pair.Request.CustodyReceipt)
		at := now.Add(time.Duration(4+n) * time.Second)
		if _, err = recovered.EnqueueManagedControlOperation(ctx, pair, at); err != nil {
			t.Fatal("independent pair enqueue", err)
		}
		if _, err = recovered.PollControlOperation(ctx, identity, inv.InstallationID, at); err != nil {
			t.Fatal(err)
		}
		wire, err = os.ReadFile(filepath.Join("..", "connectorcontrol", "testdata", "control-v3", "valid", files[1]))
		if err != nil {
			t.Fatal(err)
		}
		var delivered connectorcontrol.ControlResultV3
		json.Unmarshal(wire, &delivered)
		delivered.InstallationID = inv.InstallationID
		delivered.ObservedAt = at
		delivered.Result.RegistrationSHA256 = pair.Request.RegistrationSHA256
		delivered.Result.CustodyReceiptSHA256 = pair.Request.CustodyReceiptSHA256
		installation, err := recovered.CompleteManagedControlOperation(ctx, identity, delivered, at)
		if err != nil || installation.Status != "connected" || installation.ObservedState != "connected" {
			t.Fatal("pair degraded connected install", err)
		}
		if _, err = recovered.CompleteManagedControlOperation(ctx, identity, delivered, at.Add(time.Second)); err != nil {
			t.Fatal("pair ACK replay", err)
		}
	}
}
