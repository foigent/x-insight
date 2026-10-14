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

func TestNewInstallDescriptorPostgresOriginalV2ReplayRestartAndEmptyHTTPS(t *testing.T) {
	base := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if base == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	createConnectorTestRoles(t, ctx, base)
	dsn := connectorPullMigrationDatabaseDSN(t, ctx, base)
	migrationsDir := filepath.Join("..", "..", "migrations")
	schema43Dir := connectorMigrationPrefixDir(t, migrationsDir, "000043")
	if _, err := db.RunMigrations(ctx, dsn, schema43Dir); err != nil {
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
	var descriptor connectorcontrol.SourceDescriptorV1
	var id string
	const sourceInstance = "ecs.second-integration.v2"
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.RawQuery != "" && r.URL.Path != connectorcontrol.ReceiverFeedRoute || r.Header.Get("Authorization") != "Bearer ur-xf1-"+strings.Repeat("1", 64) {
			w.WriteHeader(403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		switch r.URL.Path {
		case connectorcontrol.ReceiverFeedRoute:
			json.NewEncoder(w).Encode(pullfeed.FeedPage{SchemaVersion: pullfeed.SchemaVersion, SourceInstance: sourceInstance, GeneratedAt: now, Items: []pullfeed.FeedItem{}, NextCursor: "cursor-0001", HighWatermark: "cursor-0001"})
		case "/api/v1/integrations/xinsight/connector-installations/" + id + "/source-binding":
			json.NewEncoder(w).Encode(descriptor)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	inv, _, err := store.CreateInvitation(ctx, connectorcontrol.CreateInvitationInput{ActorSubject: "xs_0123456789abcdef", Config: connectorcontrol.InvitationConfig{DeploymentID: "xid_ZZZZZZZZZZZZZZZZZZZZZZ", XInsightOrigin: "https://xinsight.example", UniRouterInstallOrigin: server.URL}, Now: now, Random: bytes.NewReader(bytes.Repeat([]byte{0x17}, 64))})
	if err != nil {
		t.Fatal(err)
	}
	id = inv.InstallationID
	binding := connectorcontrol.SourceBindingV1{SchemaVersion: connectorcontrol.SourceBindingSchema, InstallationID: id, DeploymentID: inv.DeploymentID, OrgID: 2, ProjectID: 86, SourceInstance: sourceInstance, EventScope: connectorcontrol.CompletedEventType, FeedSchema: connectorcontrol.SourceBindingFeedSchema}
	consent, _, _ := connectorcontrol.CanonicalSHA256(binding)
	redeemInput := connectorcontrol.RedeemInvitationInput{InvitationToken: inv.Token, ProjectID: "project.fixture", FinalEventScope: []string{connectorcontrol.CompletedEventType}, ConsentSHA256: consent, ConfirmedAt: now, ControlToken: "xib_" + strings.Repeat("A", 43), ControlTokenExpires: now.Add(24 * time.Hour)}
	redeemed, err := store.RedeemInvitation(ctx, redeemInput, now, bytes.NewReader(bytes.Repeat([]byte{0x31}, 128)))
	if err != nil {
		t.Fatal(err)
	}
	identity := redeemed.Installation.ControlServiceIdentityID
	root := filepath.Join("..", "..", "contracts", "connector-control", "v2", "fixtures", "valid")
	readResult := func(file string, op connectorcontrol.ControlOperation, at time.Time) connectorcontrol.ControlOperationResult {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		var r connectorcontrol.ControlOperationResult
		if json.Unmarshal(raw, &r) != nil {
			t.Fatal("fixture decode")
		}
		r.OperationID, r.InstallationID, r.ObservedAt = op.OperationID, id, at
		return r
	}
	if _, err = store.PollControlOperation(ctx, identity, id, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	prepare := readResult("02-prepare-paused-source-result.json", redeemed.Operation, now.Add(time.Second))
	prepared, err := store.CompleteControlOperation(ctx, identity, prepare, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	prepareSHA, _, _ := connectorcontrol.CanonicalSHA256(prepare)
	if _, err = store.ClaimInstallationActivation(ctx, id, prepared.Version, prepare.OperationID, prepareSHA, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		query string
		id    string
	}{{`INSERT INTO source_systems(id,source_key,source_type,display_name) VALUES($1,'unirouter','unirouter','Synthetic UR') ON CONFLICT DO NOTHING`, unirouter.DefaultSourceSystemID}, {`INSERT INTO adapter_manifests(id,adapter_id,adapter_version,source_type,fidelity_level,payload_behavior,idempotency_strategy) VALUES($1,'synthetic-ur','1.0.0','unirouter','F2','metadata_only_default','source_ref') ON CONFLICT DO NOTHING`, unirouter.DefaultCompletedExecutionAdapterManifestID}} {
		if _, err = database.ExecContext(ctx, q.query, q.id); err != nil {
			t.Fatal(err)
		}
	}
	state, err := pullfeed.NewPostgresStateStore(database, unirouter.DefaultSourceSystemID, unirouter.DefaultCompletedExecutionAdapterManifestID)
	if err != nil {
		t.Fatal(err)
	}
	port, err := checkpointepoch.NewPostgresInstallationEpochPort(database)
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := checkpointepoch.ApplyInstallationEpoch(ctx, port, checkpointepoch.InstallationEpochInput{Binding: checkpointepoch.Binding{SourceSystemID: unirouter.DefaultSourceSystemID, AdapterManifestID: unirouter.DefaultCompletedExecutionAdapterManifestID, CursorKey: connectorcontrol.ControlCursorKey}, InstallationID: id, WorkerUnit: checkpointepoch.CompletedExecutionWorkerUnit, SourceInstance: sourceInstance, Cursor: "cursor-0001", HighWatermark: "cursor-0001"}, now.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	active, err := state.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "03-register-receiver-operation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var registerRequest connectorcontrol.ControlOperationRequestV2
	if json.Unmarshal(raw, &registerRequest) != nil {
		t.Fatal("fixture register")
	}
	registerRequest.Request.ConsumerEpoch = active.CheckpointEpoch
	registerRequest.Request.CheckpointAuthoritySHA256 = epoch.Active.StateSHA256
	register, err := store.EnqueueControlPhase(ctx, connectorcontrol.EnqueueControlPhaseInput{InstallationID: id, Phase: connectorcontrol.ControlPhaseRegisterReceiver, PredecessorOperationID: prepare.OperationID, PredecessorResultSHA256: prepareSHA, Request: registerRequest.Request}, now.Add(3*time.Second), bytes.NewReader(bytes.Repeat([]byte{0x32}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PollControlOperation(ctx, identity, id, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	registered := readResult("04-register-receiver-result.json", register, now.Add(4*time.Second))
	if _, err = store.CompleteControlOperation(ctx, identity, registered, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	registerSHA, _, _ := connectorcontrol.CanonicalSHA256(registered)
	raw, err = os.ReadFile(filepath.Join(root, "05-resume-source-operation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var resumeRequest connectorcontrol.ControlOperationRequestV2
	if json.Unmarshal(raw, &resumeRequest) != nil {
		t.Fatal("fixture resume")
	}
	resume, err := store.EnqueueControlPhase(ctx, connectorcontrol.EnqueueControlPhaseInput{InstallationID: id, Phase: connectorcontrol.ControlPhaseResumeSource, PredecessorOperationID: register.OperationID, PredecessorResultSHA256: registerSHA, Request: resumeRequest.Request}, now.Add(5*time.Second), bytes.NewReader(bytes.Repeat([]byte{0x33}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PollControlOperation(ctx, identity, id, now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	resumed := readResult("06-resume-source-result.json", resume, now.Add(6*time.Second))
	completed, err := store.CompleteControlOperation(ctx, identity, resumed, now.Add(6*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	resumeSHA, _, _ := connectorcontrol.CanonicalSHA256(resumed)
	if _, err = store.FinalizeInstallationConnected(ctx, id, completed.Version, resume.OperationID, resumeSHA, now.Add(7*time.Second)); err != nil {
		t.Fatal(err)
	}
	qualifyManagedSchema44Upgrade(t, ctx, base, dsn, database, store, id, migrationsDir)
	// Schema43 retained only the original acknowledgement digest. Its exact
	// authenticated redeem replay supplies the original consent preimage; M44
	// must not guess that missing identity or create a new original operation.
	replayed, err := store.RedeemInvitation(ctx, redeemInput, now.Add(7*time.Second), nil)
	if err != nil || replayed.Operation.OperationID != redeemed.Operation.OperationID || replayed.Installation.ControlServiceIdentityID != identity {
		t.Fatal("schema44 original redeem replay", err)
	}
	network := connectorcontrol.ReceiverNetworkV1{FeedKeyID: "105", KeyPrefix: registerRequest.Request.ReceiverRegistration.KeyPrefix, RegistrationOperationID: registerRequest.Request.ReceiverRegistration.OperationID, VerifierScheme: "receiver_sha256_v1", AllowedIP: "192.0.2.10/32"}
	networkSHA, _, _ := connectorcontrol.CanonicalSHA256(network)
	descriptor = connectorcontrol.SourceDescriptorV1{SchemaVersion: connectorcontrol.SourceDescriptorSchema, SourceBinding: binding, SourceBindingSHA256: consent, ReceiverNetwork: network, ReceiverNetworkSHA256: networkSHA, ResumeOperationID: resume.OperationID, ResumeResultSHA256: resumeSHA}
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: server.URL + connectorcontrol.ReceiverFeedRoute, ServiceKey: "ur-xf1-" + strings.Repeat("1", 64), TLSRootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.CloseContentConnections()
	page, err := reader.Fetch(ctx, "cursor-0001", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(page, []byte(`"items":[]`)) {
		t.Fatal("expected legal empty feed")
	}
	wire, err := reader.FetchConnectorSourceDescriptor(ctx, id)
	if err != nil {
		t.Fatal("fixed GET", err)
	}
	decoded, err := connectorcontrol.DecodeSourceDescriptor(wire)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SaveManagedSourceDescriptor(ctx, decoded, now.Add(8*time.Second)); err != nil {
		t.Fatal("first save", err)
	}
	recovered, err := db.OpenWorkspaceStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if err = recovered.SaveManagedSourceDescriptor(ctx, decoded, now.Add(9*time.Second)); err != nil {
		t.Fatal("exact replay", err)
	}
	boundary, err := recovered.InspectManagedConnector(ctx, id)
	if err != nil || boundary.Descriptor == nil || boundary.Bootstrap != nil || *boundary.Descriptor != decoded {
		t.Fatal("durable restart/mode", err)
	}
	for n, files := range [][2]string{{"05-policy-pair-operation.json", "06-policy-pair-result.json"}, {"07-source-enroll-pair-operation.json", "08-source-enroll-pair-result.json"}} {
		root := filepath.Join("..", "connectorcontrol", "testdata", "control-v3", "valid")
		raw, err := os.ReadFile(filepath.Join(root, files[0]))
		if err != nil {
			t.Fatal(err)
		}
		var pair connectorcontrol.ControlOperationRequestV3
		if json.Unmarshal(raw, &pair) != nil {
			t.Fatal("pair fixture")
		}
		pair.InstallationID = id
		pair.ProjectID = boundary.Installation.ProjectID
		pair.PredecessorOperationID = resume.OperationID
		pair.PredecessorResultSHA256 = resumeSHA
		pair.Request.SourceInstance = sourceInstance
		pair.Request.SourceBindingSHA256 = consent
		pair.Request.Registration.InstallationID = id
		pair.Request.CustodyReceipt.InstallationID = id
		pair.Request.RegistrationSHA256, _, _ = connectorcontrol.CanonicalSHA256(*pair.Request.Registration)
		pair.Request.CustodyReceipt.RegistrationSHA256 = pair.Request.RegistrationSHA256
		pair.Request.CustodyReceiptSHA256, _, _ = connectorcontrol.CanonicalSHA256(*pair.Request.CustodyReceipt)
		at := now.Add(time.Duration(9+n) * time.Second)
		wrongPredecessor := pair
		wrongPredecessor.PredecessorResultSHA256 = "sha256:" + strings.Repeat("f", 64)
		if _, err = recovered.EnqueueManagedControlOperation(ctx, wrongPredecessor, at); !errors.Is(err, connectorcontrol.ErrConflict) {
			t.Fatal("wrong original predecessor accepted", err)
		}
		if _, err = recovered.EnqueueManagedControlOperation(ctx, pair, at); err != nil {
			t.Fatal("new-install independent pair", err)
		}
		legacy, err := recovered.InspectActivationBoundary(ctx, id)
		if err != nil || legacy.ResumeSource == nil || legacy.ResumeSource.Operation.OperationID != resume.OperationID || legacy.Installation.Status != "connected" {
			t.Fatal("pending V3 pair broke original V2 activation boundary", err)
		}
		if _, err = recovered.PollControlOperation(ctx, identity, id, at); err != nil {
			t.Fatal(err)
		}
		raw, err = os.ReadFile(filepath.Join(root, files[1]))
		if err != nil {
			t.Fatal(err)
		}
		var result connectorcontrol.ControlResultV3
		if json.Unmarshal(raw, &result) != nil {
			t.Fatal("pair result fixture")
		}
		result.InstallationID = id
		// UR may be slightly behind XI. Never sort the two host clocks; the
		// exact completed predecessor and durable operation establish order.
		result.ObservedAt = at.Add(-500 * time.Millisecond)
		result.Result.SourceInstance = sourceInstance
		result.Result.RegistrationSHA256 = pair.Request.RegistrationSHA256
		result.Result.CustodyReceiptSHA256 = pair.Request.CustodyReceiptSHA256
		if _, err = database.ExecContext(ctx, `UPDATE service_identities SET expires_at=$2 WHERE id=$1`, identity, at.Add(-time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err = recovered.CompleteManagedControlOperation(ctx, identity, result, at); !errors.Is(err, connectorcontrol.ErrConflict) {
			t.Fatal("XI-local expired identity accepted", err)
		}
		if _, err = database.ExecContext(ctx, `UPDATE service_identities SET expires_at=$2 WHERE id=$1`, identity, redeemInput.ControlTokenExpires); err != nil {
			t.Fatal(err)
		}
		install, err := recovered.CompleteManagedControlOperation(ctx, identity, result, at)
		if err != nil || install.Status != "connected" || install.ObservedState != "connected" {
			t.Fatal("new-install pair changed original connected feed", err)
		}
		legacy, err = recovered.InspectActivationBoundary(ctx, id)
		if err != nil || legacy.ResumeSource == nil || legacy.ResumeSource.Operation.OperationID != resume.OperationID || legacy.Installation.Status != "connected" {
			t.Fatal("completed V3 pair broke original V2 activation boundary", err)
		}
	}
	bad := decoded
	bad.ReceiverNetwork.AllowedIP = "192.0.2.11/32"
	bad.ReceiverNetworkSHA256, _, _ = connectorcontrol.CanonicalSHA256(bad.ReceiverNetwork)
	if err = recovered.SaveManagedSourceDescriptor(ctx, bad, now.Add(10*time.Second)); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatal("changed identity replay", err)
	}
	if _, err = database.ExecContext(ctx, `UPDATE connector_installation_source_bindings SET descriptor_sha256=NULL WHERE installation_id=$1`, id); err == nil {
		t.Fatal("NULL persisted digest")
	}
	current, err := state.Load(ctx)
	if err != nil || current.Cursor != active.Cursor || current.CheckpointEpoch != active.CheckpointEpoch || current.PagesCommitted != 0 {
		t.Fatal("descriptor changed checkpoint", err)
	}
	worker, err := pullfeed.NewLiveWorker(reader, state, pullfeed.LiveCommitFunc(func(context.Context, pullfeed.FeedPage, pullfeed.FeedItem) (pullfeed.LiveItemCommitResult, error) {
		t.Fatal("legal empty feed unexpectedly invoked item commit")
		return pullfeed.LiveItemCommitResult{}, nil
	}), pullfeed.LiveWorkerConfig{PollInterval: time.Second, Now: func() time.Time { return now.Add(10 * time.Second) }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = worker.PollOnce(ctx); err != nil {
		t.Fatal("actual worker HTTPS empty read", err)
	}
	if _, err = recovered.InspectManagedConnector(ctx, id); err != nil {
		t.Fatal("original digest remains qualified after real polling", err)
	}
	// Changing an immutable activation field must still fail even though normal
	// worker cursor/statistics changes are allowed after the original register.
	if _, err = database.ExecContext(ctx, `UPDATE adapter_import_states SET authorization_ref='decision://wrong-original-authority' WHERE id=$1::uuid`, active.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = recovered.InspectManagedConnector(ctx, id); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatal("changed original authority accepted", err)
	}
	if _, err = database.ExecContext(ctx, `UPDATE adapter_import_states SET authorization_ref=$2 WHERE id=$1::uuid`, active.ID, epoch.AuthorizationRef); err != nil {
		t.Fatal(err)
	}
	if _, err = recovered.InspectManagedConnector(ctx, id); err != nil {
		t.Fatal("restored exact original authority", err)
	}
	if _, err = database.ExecContext(ctx, `UPDATE service_identities SET status='revoked',revoked_at=$2,revoke_reason='synthetic scope test' WHERE id=$1`, identity, now.Add(11*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err = recovered.SaveManagedSourceDescriptor(ctx, decoded, now.Add(12*time.Second)); !errors.Is(err, connectorcontrol.ErrConflict) {
		t.Fatal("revoked control identity", err)
	}
}
