//go:build integration

package db_test

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/checkpointepoch"
	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connectorcontrol"
	"github.com/foigent/x-insight/internal/hostctl"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
	"github.com/foigent/x-insight/internal/sourcefacts"
)

type jointHumanCall func(string, string, any) map[string]json.RawMessage

// Use the actual manual receiver preparation, original human lifecycle, epoch
// transition and live importer before admission. No managed terminal is seeded.
func setupJointOriginalFeed(t *testing.T, ctx context.Context, input urJointConsumerInput, pool *x509.CertPool, e *hostctl.Engine, f *sourceFixture, call jointHumanCall) (*pullfeed.LiveWorker, *pullfeed.PostgresStateStore) {
	t.Helper()
	cfg, err := e.StageManagedFeedConfig(ctx, input.BaseURL)
	if err != nil {
		t.Fatal("original feed config stage", err)
	}
	if cfg.NetworkBinding == nil || *cfg.NetworkBinding != input.ReceiverNetworkBinding {
		t.Fatal("original network authority differs")
	}
	if _, err = e.PromoteManagedFeedConfig(ctx, cfg.ConfigSHA256); err != nil {
		t.Fatal("original feed config promote", err)
	}
	reg, err := e.PrepareFeedCredential(ctx, "", "")
	if err != nil {
		t.Fatal("original receiver prepare", err)
	}
	path := "/api/v1/data-exchange/xinsight/connections/" + input.ProjectID
	response := call("POST", path+"/credentials", map[string]any{"allowed_ip": input.ReceiverAllowedIP, "registration": reg})
	var key int64
	if json.Unmarshal(response["id"], &key) != nil || key < 1 {
		t.Fatal("original receiver key readback")
	}
	call("POST", path+"/credentials/activate", map[string]any{"staged_credential_id": key})
	// Activation verifies actual authenticated paused feed over TLS before the
	// root atomic credential replacement and unit restart.
	if _, err = e.ActivateFeedCredential(ctx, reg.OperationID, reg.VerifierSHA256); err != nil {
		t.Fatal("original receiver activate", err)
	}
	call("POST", path+"/resume", map[string]any{})
	token, err := config.LoadOwnerOnlySecretFile(e.Paths.FeedCredentialFile)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: input.BaseURL + connectorcontrol.ReceiverFeedRoute, ServiceKey: token, TLSRootCAs: pool})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.CloseContentConnections)
	raw, err := reader.Fetch(ctx, "", pullfeed.MaxPageItems)
	if err != nil {
		t.Fatal("original legal empty feed", err)
	}
	var page pullfeed.FeedPage
	if json.Unmarshal(raw, &page) != nil || page.SourceInstance != input.SourceInstance || len(page.Items) != 0 || page.NextCursor == "" {
		t.Fatal("original source boundary")
	}
	state, authorization := jointOriginalEpochPrestate(t, ctx, f, page)
	committer, err := pullfeed.NewEvidenceCommitter(f.evidence, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := pullfeed.NewLiveWorker(reader, state, committer, pullfeed.LiveWorkerConfig{PollInterval: 100 * time.Millisecond, AuthorizationRef: authorization})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = worker.PollOnce(ctx); err != nil {
		t.Fatal("original live importer", err)
	}
	return worker, state
}

// Original indexed root files and actual human A/C lifecycle remain independent
// of the later enrollment. The macOS fixture does not claim a Linux UID/mount.
func setupJointOriginalSources(t *testing.T, ctx context.Context, input urJointConsumerInput, pool *x509.CertPool, e *hostctl.Engine, f *sourceFixture, call jointHumanCall, put func(string, []byte, os.FileMode)) sourcefacts.ManagedConfig {
	t.Helper()
	cfg := sourcefacts.ManagedConfig{SchemaVersion: sourcefacts.ManagedContentConfigVersion, PollIntervalSeconds: 1, RetryMaximumSeconds: 10, Profiles: []sourcefacts.ManagedProfileConfig{}}
	path := "/api/v1/data-exchange/xinsight/source-connections/" + input.ProjectID
	newRegistration := func(name string, profiles []string) (sourcefacts.SourceCredentialRegistration, string) {
		var secret [32]byte
		if _, err := rand.Read(secret[:]); err != nil {
			t.Fatal(err)
		}
		token := "ur-xf1-" + hex.EncodeToString(secret[:])
		clear(secret[:])
		file := filepath.Join(e.Paths.EtcDir, name+".key")
		put(file, []byte(token+"\n"), 0600)
		raw := jointRegistration(t, token, input.SourceInstance, input.OrgID, input.ProjectID, profiles)
		var r sourcefacts.SourceCredentialRegistration
		if json.Unmarshal(raw, &r) != nil {
			t.Fatal("original registration")
		}
		return r, file
	}
	facts, factsFile := newRegistration("joint-original-facts", input.Profiles)
	factReceipt := call("POST", path+"/prepare", map[string]any{"allowed_ip": input.ReceiverAllowedIP, "registration": facts})
	var factsID string
	if json.Unmarshal(factReceipt["credential_id"], &factsID) != nil || factsID == "" {
		t.Fatal("original facts credential")
	}
	call("POST", path+"/activate", map[string]any{"credential_id": factsID})
	call("POST", path+"/resume", map[string]any{})
	factsRaw, _ := json.Marshal(facts)
	cfg.Bindings = append(cfg.Bindings, sourcefacts.ManagedCredentialBinding{Name: "original-facts", Scope: "legacy", SourceSystemID: f.source, SourceInstance: input.SourceInstance, OrgID: input.OrgID, ProjectID: input.ProjectID, CredentialID: factsID, RegistrationHash: sourcefacts.Hash(factsRaw), KeyFile: factsFile})
	for _, profile := range input.Profiles {
		token, err := config.LoadOwnerOnlySecretFile(factsFile)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: input.BaseURL + sourcefacts.ManagedEndpointPath(profile), ServiceKey: token, TLSRootCAs: pool})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := reader.FetchSourceCapabilities(ctx)
		if err != nil {
			t.Fatal("original facts capabilities", err)
		}
		var cap sourcefacts.Capabilities
		if sourcefacts.ValidProfile(profile) {
			cap, err = sourcefacts.DecodeCapabilities(raw)
		} else {
			cap, err = sourcefacts.DecodeObservationCapabilities(raw)
		}
		if err != nil {
			t.Fatal("original capability contract", err)
		}
		stream := sourcefacts.Stream{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: input.SourceInstance, OrgID: input.OrgID, ProjectID: input.ProjectID, Profile: profile, ScopeGeneration: cap.ScopeGeneration}
		stream, err = f.consumerStore.RegisterSourceFactsStream(ctx, stream)
		if err != nil {
			t.Fatal(err)
		}
		if err = sourcefacts.Preflight(ctx, reader, stream); err != nil {
			t.Fatal(err)
		}
		stream, err = f.consumerStore.ControlSourceFactsStream(ctx, stream.ID, "active", stream.Revision, false)
		if err != nil {
			t.Fatal(err)
		}
		consumer := sourcefacts.Consumer{Store: f.consumerStore, Reader: reader, ExpectedStream: &stream}
		if _, err = consumer.PullOnce(ctx, stream.ID); err != nil {
			t.Fatal("original profile pull", profile, err)
		}
		reader.CloseContentConnections()
		cfg.Profiles = append(cfg.Profiles, sourcefacts.ManagedProfileConfig{StreamID: stream.ID, SourceSystemID: f.source, SourceInstance: input.SourceInstance, OrgID: input.OrgID, ProjectID: input.ProjectID, Profile: profile, ScopeGeneration: stream.ScopeGeneration, Endpoint: input.BaseURL + sourcefacts.ManagedEndpointPath(profile), KeyFile: factsFile})
	}
	content, contentFile := newRegistration("joint-original-content", []string{sourcefacts.ContentProfile})
	initial := call("GET", path+"/content", nil)
	var version string
	if json.Unmarshal(initial["state_version"], &version) != nil {
		t.Fatal("original content version")
	}
	result := call("POST", path+"/content/prepare", map[string]any{"operation_id": content.OperationID, "expected_state_version": version, "registration": content, "allowed_ip": input.ReceiverAllowedIP})
	decodeReceipt := func(result map[string]json.RawMessage) sourcefacts.ContentControlReceipt {
		raw, _ := json.Marshal(result)
		r, err := sourcefacts.DecodeContentControlReceipt(raw)
		if err != nil {
			t.Fatal("original content receipt", err)
		}
		return r
	}
	receipt := decodeReceipt(result)
	for _, action := range []string{"activate", "resume"} {
		var op [16]byte
		if _, err := rand.Read(op[:]); err != nil {
			t.Fatal(err)
		}
		result = call("POST", path+"/content/"+action, map[string]any{"operation_id": hex.EncodeToString(op[:]), "expected_state_version": receipt.StateVersion, "credential_id": receipt.CredentialID, "registration_hash": receipt.RegistrationHash})
		receipt = decodeReceipt(result)
	}
	token, err := config.LoadOwnerOnlySecretFile(contentFile)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: input.BaseURL + sourcefacts.ContentEndpointPath, ServiceKey: token, TLSRootCAs: pool})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.CloseContentConnections()
	raw, err := reader.FetchContentCapabilities(ctx)
	if err != nil {
		t.Fatal("original content capabilities", err)
	}
	cap, err := sourcefacts.DecodeContentCapabilities(raw)
	if err != nil {
		t.Fatal(err)
	}
	collection := sourcefacts.ContentCollection{ID: testUUID(t), SourceSystemID: f.source, SourceInstance: input.SourceInstance, OrgID: input.OrgID, ProjectID: input.ProjectID, CredentialID: receipt.CredentialID, RegistrationHash: receipt.RegistrationHash, ContentScopeRevision: cap.ContentScopeRevision, RecoveryEpoch: cap.RecoveryEpoch, AllowedPurposes: cap.AllowedPurposes, CoverageFrom: cap.CoverageFrom}
	collection, err = f.consumerStore.RegisterContentCollection(ctx, collection)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = sourcefacts.PreflightContent(ctx, reader, collection); err != nil {
		t.Fatal(err)
	}
	collection, err = f.consumerStore.ControlContentCollection(ctx, collection.ID, "active", collection.Revision, false, collection)
	if err != nil {
		t.Fatal(err)
	}
	consumer := sourcefacts.ContentConsumer{Store: f.consumerStore, Reader: reader, Expected: collection}
	if _, err = consumer.PullOnce(ctx); err != nil {
		t.Fatal("original manifest pull", err)
	}
	cfg.Bindings = append(cfg.Bindings, sourcefacts.ManagedCredentialBinding{Name: "original-content", Scope: "content", SourceSystemID: f.source, SourceInstance: input.SourceInstance, OrgID: input.OrgID, ProjectID: input.ProjectID, CredentialID: collection.CredentialID, RegistrationHash: collection.RegistrationHash, KeyFile: contentFile})
	cfg.Contents = []sourcefacts.ManagedContentConfig{{CollectionID: collection.ID, SourceSystemID: f.source, SourceInstance: input.SourceInstance, OrgID: input.OrgID, ProjectID: input.ProjectID, CredentialBinding: "original-content", ContentScopeRevision: collection.ContentScopeRevision, RecoveryEpoch: collection.RecoveryEpoch, AllowedPurposes: collection.AllowedPurposes, CoverageFrom: collection.CoverageFrom, Endpoint: input.BaseURL + sourcefacts.ContentEndpointPath}}
	if err = cfg.Validate(); err != nil {
		t.Fatal("original root configuration", err)
	}
	bytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	put(e.Paths.SourceFactsConfig, bytes, 0644)
	dropin := "[Service]\nLoadCredential=\nLoadCredential=postgres-importer.dsn:" + e.Paths.ImporterDSNFile + "\n"
	for i, key := range cfg.CredentialFiles() {
		dropin += fmt.Sprintf("LoadCredential=source-profile-%d.key:%s\n", i, key)
	}
	put(filepath.Join(e.Paths.SourceFactsServiceUnit+".d", "10-source-credentials.conf"), []byte(dropin), 0644)
	put(filepath.Join(e.Paths.ServiceUnit+".d", "30-content-credentials.conf"), []byte("[Service]\nEnvironment=XINSIGHT_CONTENT_CONFIG="+e.Paths.SourceFactsConfig+"\nLoadCredential=source-content-"+collection.ID+".key:"+contentFile+"\n"), 0644)
	return cfg
}

func jointOriginalEpochPrestate(t *testing.T, ctx context.Context, f *sourceFixture, page pullfeed.FeedPage) (*pullfeed.PostgresStateStore, string) {
	t.Helper()
	binding := checkpointepoch.Binding{SourceSystemID: unirouter.DefaultSourceSystemID, AdapterManifestID: unirouter.DefaultCompletedExecutionAdapterManifestID, CursorKey: pullfeed.CompletedExecutionCursorKey}
	first := checkpointepoch.PrepareInput{Binding: binding, WorkerUnit: checkpointepoch.CompletedExecutionWorkerUnit, NextSourceInstance: "joint-historical-source-epoch1", NextCursor: "joint-historical-epoch1", NextHighWatermark: "joint-historical-epoch1", AuthorizationRef: "decision://joint-original-epoch1"}
	if _, err := checkpointepoch.BootstrapEmpty(ctx, f.admin, checkpointepoch.EmptyBootstrapInput{PrepareInput: first, CheckpointEpoch: 1}, time.Now().UTC()); err != nil {
		t.Fatal("original initial epoch", err)
	}
	epochs, err := checkpointepoch.NewStore(f.admin)
	if err != nil {
		t.Fatal(err)
	}
	// A supported epoch transition changes source identity. Bootstrap records
	// the synthetic historical empty source; Prepare/Activate then accepts
	// the actual current TLS source boundary, without inserting terminal rows.
	first.NextSourceInstance = page.SourceInstance
	first.NextCursor = page.NextCursor
	first.NextHighWatermark = page.HighWatermark
	first.AuthorizationRef = "decision://joint-original-epoch2"
	var op [16]byte
	if _, err = rand.Read(op[:]); err != nil {
		t.Fatal(err)
	}
	prepared, err := epochs.Prepare(ctx, first, hex.EncodeToString(op[:]), time.Now().UTC())
	if err != nil {
		t.Fatal("original epoch2 prepare", err)
	}
	if prepared.Operation == nil {
		t.Fatal("original epoch2 absent")
	}
	activated, err := epochs.Activate(ctx, prepared.Operation.OperationID, prepared.Operation.Plan.PlanSHA256, time.Now().UTC())
	if err != nil || activated.Active.Epoch != 2 {
		t.Fatal("original epoch2 activate", err)
	}
	state, err := pullfeed.NewPostgresStateStore(f.admin, binding.SourceSystemID, binding.AdapterManifestID)
	if err != nil {
		t.Fatal(err)
	}
	return state, first.AuthorizationRef
}

func TestJointOriginalEpochPrestatePostgres(t *testing.T) {
	f := newSourceFixture(t)
	f.exec(`INSERT INTO source_systems(id,source_key,source_type,display_name,status) VALUES($1,'unirouter','unirouter','Joint UR','active') ON CONFLICT DO NOTHING`, unirouter.DefaultSourceSystemID)
	f.exec(`INSERT INTO adapter_manifests(id,adapter_id,adapter_version,source_type,fidelity_level,payload_behavior,idempotency_strategy) VALUES($1,'synthetic-joint-ur','1.0.0','unirouter','F2','metadata_only_default','source_ref') ON CONFLICT DO NOTHING`, unirouter.DefaultCompletedExecutionAdapterManifestID)
	now := time.Now().UTC()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != connectorcontrol.ReceiverFeedRoute || r.Header.Get("Authorization") != "Bearer synthetic-local-prestate" {
			w.WriteHeader(403)
			return
		}
		json.NewEncoder(w).Encode(pullfeed.FeedPage{SchemaVersion: pullfeed.SchemaVersion, SourceInstance: "joint-current-source", GeneratedAt: now, Items: []pullfeed.FeedItem{}, NextCursor: "actual-current-empty-boundary", HighWatermark: "actual-current-empty-boundary"})
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	reader, err := pullfeed.NewHTTPSource(pullfeed.HTTPSourceConfig{Endpoint: server.URL + connectorcontrol.ReceiverFeedRoute, ServiceKey: "synthetic-local-prestate", TLSRootCAs: roots})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.CloseContentConnections()
	raw, err := reader.Fetch(f.ctx, "", pullfeed.MaxPageItems)
	if err != nil {
		t.Fatal(err)
	}
	var page pullfeed.FeedPage
	if err = json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	state, authorization := jointOriginalEpochPrestate(t, f.ctx, f, page)
	before, err := state.Load(f.ctx)
	if err != nil || before.CheckpointEpoch != 2 || before.SourceInstance != page.SourceInstance || before.Cursor != page.NextCursor || before.HighWatermark != page.HighWatermark || before.AuthorizationRef != authorization || before.CheckpointOperationID == "" {
		t.Fatal("actual supported epoch2 prestate", err)
	}
	committer, err := pullfeed.NewEvidenceCommitter(f.evidence, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	worker, err := pullfeed.NewLiveWorker(reader, state, committer, pullfeed.LiveWorkerConfig{PollInterval: time.Second, AuthorizationRef: authorization})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = worker.PollOnce(f.ctx); err != nil {
		t.Fatal("real prestate warmup", err)
	}
	after, err := state.Load(f.ctx)
	if err != nil || after.PagesCommitted != before.PagesCommitted+1 || after.CheckpointEpoch != 2 || after.CheckpointOperationID != before.CheckpointOperationID || after.Cursor != before.Cursor || after.HighWatermark != before.HighWatermark {
		t.Fatal("warmup changed authority", err)
	}
	var history int
	var oldSource, cursor, hwm string
	if err = f.admin.QueryRowContext(f.ctx, `SELECT count(*),min(source_instance),min(final_cursor),min(final_high_watermark) FROM connector_checkpoint_epoch_history`).Scan(&history, &oldSource, &cursor, &hwm); err != nil || history != 1 || oldSource != "joint-historical-source-epoch1" || cursor != "joint-historical-epoch1" || hwm != cursor {
		t.Fatal("retained exact historical authority", err)
	}
}
