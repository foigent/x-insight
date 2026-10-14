package litellmfeed

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/foigent/x-insight/internal/adapters/litellm"
	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/investigation"
)

func TestPostgresLiteLLMStatePersistenceCheckpointReplayAndScopeBinding(
	t *testing.T,
) {
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if _, err := db.RunMigrations(
		ctx,
		dsn,
		filepath.Join("..", "..", "..", "migrations"),
	); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	defer database.Close()
	evidenceStore, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatalf("open EvidenceStore: %v", err)
	}
	defer evidenceStore.Close()

	requestID := "litellm_pg_" + postgresTestNonce(t)
	sourceInstance := "litellm-pg-" + postgresTestNonce(t)
	cleanup := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			10*time.Second,
		)
		defer cleanupCancel()
		_, _ = database.ExecContext(
			cleanupCtx,
			`DELETE FROM capture_gaps WHERE request_id = $1`,
			requestID,
		)
		_, _ = database.ExecContext(
			cleanupCtx,
			`DELETE FROM evidence_events WHERE request_id = $1`,
			requestID,
		)
		_, _ = database.ExecContext(cleanupCtx, `
DELETE FROM import_provenance
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND NOT EXISTS (
    SELECT 1
    FROM import_provenance_events
    WHERE import_provenance_id = import_provenance.id
  )`,
			litellm.SourceSystemID,
			litellm.AdapterManifestID,
		)
		_, _ = database.ExecContext(cleanupCtx, `
DELETE FROM adapter_import_states
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3`,
			litellm.SourceSystemID,
			litellm.AdapterManifestID,
			litellm.CursorKey,
		)
		_, _ = database.ExecContext(
			cleanupCtx,
			`DELETE FROM adapter_manifests WHERE id = $1::uuid`,
			litellm.AdapterManifestID,
		)
		_, _ = database.ExecContext(
			cleanupCtx,
			`DELETE FROM source_systems WHERE id = $1::uuid`,
			litellm.SourceSystemID,
		)
	}
	cleanup()
	t.Cleanup(cleanup)

	stateStore, err := NewStateStore(database)
	if err != nil {
		t.Fatalf("new StateStore: %v", err)
	}
	activation := Checkpoint{
		LastStartTime:     "2026-07-28T01:59:59.000000Z",
		BoundaryRequestID: []string{},
	}
	activatedAt := time.Date(
		2026,
		7,
		28,
		2,
		1,
		0,
		123456789,
		time.UTC,
	)
	scope := ScopeBinding{OrgRef: "101", ProjectRef: "202"}
	state, err := stateStore.Activate(
		ctx,
		sourceInstance,
		scope,
		activation,
		activatedAt,
	)
	if err != nil {
		t.Fatalf("activate StateStore: %v", err)
	}
	activationCursor, err := activation.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if state.Cursor != activationCursor ||
		state.HighWatermark != activationCursor ||
		state.CheckpointEpoch != 1 ||
		state.CheckpointOperationID != "" ||
		state.ActivationCursor != activationCursor ||
		state.ActivationHighWatermark != activationCursor ||
		state.SourceInstance != sourceInstance ||
		state.TenantRefPresent ||
		state.OrgRef != scope.OrgRef ||
		state.ProjectRef != scope.ProjectRef ||
		state.ActivatedAt.Nanosecond()%1000 != 0 {
		t.Fatalf("activated state=%+v", state)
	}
	var metadataContract string
	var metadataStateVersion int
	var metadataTenantPresent bool
	var metadataTenant sql.NullString
	var metadataOrg string
	var metadataProject string
	if err := database.QueryRowContext(ctx, `
SELECT
  metadata ->> 'contract',
  (metadata ->> 'state_version')::integer,
  (metadata ->> 'tenant_ref_present')::boolean,
  metadata ->> 'tenant_ref',
  metadata ->> 'org_ref',
  metadata ->> 'project_ref'
FROM adapter_import_states
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3`,
		litellm.SourceSystemID,
		litellm.AdapterManifestID,
		litellm.CursorKey,
	).Scan(
		&metadataContract,
		&metadataStateVersion,
		&metadataTenantPresent,
		&metadataTenant,
		&metadataOrg,
		&metadataProject,
	); err != nil {
		t.Fatalf("read activation metadata: %v", err)
	}
	if metadataContract != litellm.CursorKey ||
		metadataStateVersion != 1 ||
		metadataTenantPresent ||
		metadataTenant.Valid ||
		metadataOrg != scope.OrgRef ||
		metadataProject != scope.ProjectRef {
		t.Fatalf(
			"activation metadata contract=%q version=%d tenant=%t/%+v scope=%s/%s",
			metadataContract,
			metadataStateVersion,
			metadataTenantPresent,
			metadataTenant,
			metadataOrg,
			metadataProject,
		)
	}
	for _, corrupted := range []string{
		`{"last_start_time":"invalid","boundary_request_ids":[]}`,
		mustEncodeCheckpoint(t, Checkpoint{
			LastStartTime:     "2026-07-28T01:59:59.000001Z",
			BoundaryRequestID: []string{},
		}),
	} {
		if _, err := database.ExecContext(ctx, `
UPDATE adapter_import_states
SET activation_high_watermark = $4
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3`,
			litellm.SourceSystemID,
			litellm.AdapterManifestID,
			litellm.CursorKey,
			corrupted,
		); err != nil {
			t.Fatalf("corrupt activation high-watermark: %v", err)
		}
		if _, err := stateStore.Load(ctx); err == nil {
			t.Fatalf("corrupt activation high-watermark %q was accepted", corrupted)
		}
	}
	if _, err := database.ExecContext(ctx, `
UPDATE adapter_import_states
SET activation_high_watermark = activation_cursor
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3`,
		litellm.SourceSystemID,
		litellm.AdapterManifestID,
		litellm.CursorKey,
	); err != nil {
		t.Fatalf("restore activation high-watermark: %v", err)
	}
	if restored, err := stateStore.Load(ctx); err != nil ||
		restored.ActivationCursor != restored.ActivationHighWatermark {
		t.Fatalf("restored activation state=%+v err=%v", restored, err)
	}
	var sourceMetadata []byte
	var adapterManifest []byte
	if err := database.QueryRowContext(ctx, `
SELECT
  source.metadata,
  adapter.manifest_json
FROM source_systems AS source
CROSS JOIN adapter_manifests AS adapter
WHERE source.id = $1::uuid
  AND adapter.id = $2::uuid`,
		litellm.SourceSystemID,
		litellm.AdapterManifestID,
	).Scan(&sourceMetadata, &adapterManifest); err != nil {
		t.Fatalf("read exact product registry: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
UPDATE source_systems
SET metadata = jsonb_set(
  metadata,
  '{connector_version}',
  to_jsonb('2.0.0'::text)
)
WHERE id = $1::uuid`,
		litellm.SourceSystemID,
	); err != nil {
		t.Fatalf("drift source registry: %v", err)
	}
	if err := stateStore.EnsureRegistry(ctx, sourceInstance); err == nil {
		t.Fatal("existing-state source registry drift passed the startup gate")
	}
	if _, err := database.ExecContext(ctx, `
UPDATE source_systems SET metadata = $2::jsonb WHERE id = $1::uuid`,
		litellm.SourceSystemID,
		sourceMetadata,
	); err != nil {
		t.Fatalf("restore source registry: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
UPDATE adapter_manifests
SET manifest_json = jsonb_set(
  manifest_json,
  '{protocol_version}',
  to_jsonb('1.0.0'::text)
)
WHERE id = $1::uuid`,
		litellm.AdapterManifestID,
	); err != nil {
		t.Fatalf("drift adapter registry: %v", err)
	}
	if err := stateStore.EnsureRegistry(ctx, sourceInstance); err == nil {
		t.Fatal("existing-state adapter registry drift passed the startup gate")
	}
	if _, err := database.ExecContext(ctx, `
UPDATE adapter_manifests SET manifest_json = $2::jsonb WHERE id = $1::uuid`,
		litellm.AdapterManifestID,
		adapterManifest,
	); err != nil {
		t.Fatalf("restore adapter registry: %v", err)
	}
	if err := stateStore.EnsureRegistry(ctx, sourceInstance); err != nil {
		t.Fatalf("exact restored registry rejected: %v", err)
	}

	pollStarted := activatedAt.Add(time.Second)
	if err := stateStore.MarkPollStarted(
		ctx,
		activationCursor,
		pollStarted,
	); err != nil {
		t.Fatalf("mark real PostgreSQL poll started: %v", err)
	}
	if err := stateStore.MarkFailure(
		ctx,
		activationCursor,
		"source_status_503",
		true,
		pollStarted.Add(time.Second),
	); err != nil {
		t.Fatalf("mark real PostgreSQL retryable failure: %v", err)
	}
	state, err = stateStore.Load(ctx)
	if err != nil {
		t.Fatalf("load failed state: %v", err)
	}
	if state.Cursor != activationCursor ||
		state.WorkerStatus != "backoff" ||
		state.ConsecutiveFailures != 1 ||
		state.LastErrorCode != "source_status_503" {
		t.Fatalf("failed state=%+v", state)
	}
	if err := stateStore.MarkInactive(
		ctx,
		activationCursor,
		pollStarted.Add(2*time.Second),
	); err != nil {
		t.Fatalf("mark real PostgreSQL worker inactive: %v", err)
	}
	state, err = stateStore.Load(ctx)
	if err != nil {
		t.Fatalf("load inactive state: %v", err)
	}
	if state.WorkerStatus != "inactive" || state.Cursor != activationCursor {
		t.Fatalf("inactive state=%+v", state)
	}
	if state.ConsecutiveFailures != 1 ||
		state.LastErrorCode != "source_status_503" {
		t.Fatalf("inactive state discarded failure diagnostics=%+v", state)
	}

	source := postgresLiteLLMRecord(t, requestID)
	projector, err := litellm.NewProjector()
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}
	committer, err := NewEvidenceCommitter(evidenceStore)
	if err != nil {
		t.Fatalf("new Evidence committer: %v", err)
	}
	now := time.Date(2026, 7, 28, 2, 2, 0, 0, time.UTC)
	checkpointStore := &failFirstPostgresCheckpoint{
		StateStore: stateStore,
	}
	worker, err := NewWorker(
		staticWindowSource{window: CompleteWindow{
			Rows:            []litellm.SanitizedRecord{source},
			Total:           1,
			EligibleThrough: now.Add(-EligibilityDelay),
		}},
		checkpointStore,
		projector,
		committer,
		WorkerConfig{
			SourceInstance: sourceInstance,
			OrgRef:         scope.OrgRef,
			ProjectRef:     scope.ProjectRef,
			PollInterval:   time.Second,
			Now:            func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatalf("new real PostgreSQL worker: %v", err)
	}

	if _, err := worker.PollOnce(ctx); err == nil {
		t.Fatal("injected Checkpoint failure unexpectedly succeeded")
	} else {
		var workerErr *WorkerError
		if !errors.As(err, &workerErr) ||
			workerErr.Code != "state_page_commit_failed" ||
			!workerErr.Retryable {
			t.Fatalf("first poll error=%v", err)
		}
	}
	state, err = stateStore.Load(ctx)
	if err != nil {
		t.Fatalf("load after failed Checkpoint: %v", err)
	}
	if state.Cursor != activationCursor ||
		state.PagesCommitted != 0 ||
		state.WorkerStatus != "backoff" ||
		state.ConsecutiveFailures != 2 ||
		state.LastErrorCode != "state_page_commit_failed" {
		t.Fatalf("failed Checkpoint advanced state=%+v", state)
	}
	records, err := evidenceStore.ListEvidenceEventsByRequestID(ctx, requestID, 10)
	if err != nil {
		t.Fatalf("read persisted events before replay: %v", err)
	}
	if len(records) != 5 {
		t.Fatalf("persisted events before replay=%d", len(records))
	}

	report, err := worker.PollOnce(ctx)
	if err != nil {
		t.Fatalf("replay poll: %v", err)
	}
	if report.Accepted != 1 ||
		report.Inserted != 0 ||
		report.DuplicateNoop != 1 ||
		!report.CheckpointAdvanced {
		t.Fatalf("replay report=%+v", report)
	}
	state, err = stateStore.Load(ctx)
	if err != nil {
		t.Fatalf("load converged state: %v", err)
	}
	nextCursor, err := Checkpoint{
		LastStartTime:     source.Record.StartTime,
		BoundaryRequestID: []string{requestID},
	}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if state.Cursor != nextCursor ||
		state.HighWatermark != nextCursor ||
		state.PagesCommitted != 1 ||
		state.ItemsReceived != 1 ||
		state.ItemsCommitted != 0 ||
		state.DuplicateNoop != 1 ||
		state.WorkerStatus != "idle" ||
		state.ConsecutiveFailures != 0 ||
		state.LastErrorCode != "" {
		t.Fatalf("converged state=%+v", state)
	}
	records, err = evidenceStore.ListEvidenceEventsByRequestID(ctx, requestID, 10)
	if err != nil {
		t.Fatalf("read persisted events: %v", err)
	}
	gaps, err := evidenceStore.ListCaptureGapsByRequestID(ctx, requestID, 10)
	if err != nil {
		t.Fatalf("read persisted gaps: %v", err)
	}
	if len(records) != 5 || len(gaps) != 4 {
		t.Fatalf("readback events=%d gaps=%d", len(records), len(gaps))
	}
	searchAt := time.Now().UTC().Add(time.Second)
	filtered, err := evidenceStore.SearchExecutions(ctx, investigation.SearchQuery{
		AsOf:           searchAt,
		From:           time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		To:             searchAt,
		RequestID:      requestID,
		SourceSystemID: litellm.SourceSystemID,
		Limit:          2,
	})
	if err != nil ||
		len(filtered) != 1 ||
		filtered[0].RequestID != requestID ||
		filtered[0].SourceSystemID != litellm.SourceSystemID ||
		filtered[0].SourceType != "litellm" {
		t.Fatalf("LiteLLM source filter results=%+v err=%v", filtered, err)
	}
	wrongSource, err := evidenceStore.SearchExecutions(ctx, investigation.SearchQuery{
		AsOf:           searchAt,
		From:           time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		To:             searchAt,
		RequestID:      requestID,
		SourceSystemID: unirouter.DefaultSourceSystemID,
		Limit:          2,
	})
	if err != nil || len(wrongSource) != 0 {
		t.Fatalf("LiteLLM request crossed UniRouter source filter results=%+v err=%v", wrongSource, err)
	}
	var provenanceRows int
	var provenanceLinks int
	var sourceWatermark sql.NullString
	var importWatermark sql.NullString
	if err := database.QueryRowContext(ctx, `
SELECT
  count(DISTINCT provenance.id),
  count(links.evidence_event_id),
  min(provenance.source_watermark),
  min(provenance.import_watermark)
FROM import_provenance AS provenance
JOIN import_provenance_events AS links
  ON links.import_provenance_id = provenance.id
JOIN evidence_events AS event
  ON event.id = links.evidence_event_id
WHERE event.request_id = $1`,
		requestID,
	).Scan(
		&provenanceRows,
		&provenanceLinks,
		&sourceWatermark,
		&importWatermark,
	); err != nil {
		t.Fatalf("read persisted provenance: %v", err)
	}
	if provenanceRows != 1 ||
		provenanceLinks != 5 ||
		sourceWatermark.Valid ||
		importWatermark.Valid {
		t.Fatalf(
			"provenance rows=%d links=%d source/import watermark=%+v/%+v",
			provenanceRows,
			provenanceLinks,
			sourceWatermark,
			importWatermark,
		)
	}

	driftSource := &recordingWindowSource{}
	driftWorker, err := NewWorker(
		driftSource,
		stateStore,
		projector,
		&rejectingCommitter{},
		WorkerConfig{
			SourceInstance: sourceInstance,
			OrgRef:         scope.OrgRef,
			ProjectRef:     "203",
			PollInterval:   time.Second,
			Now:            func() time.Time { return now },
		},
	)
	if err != nil {
		t.Fatalf("new scope-drift worker: %v", err)
	}
	_, err = driftWorker.PollOnce(ctx)
	var workerErr *WorkerError
	if err == nil ||
		!errors.As(err, &workerErr) ||
		workerErr.Code != "mapping_scope_mismatch" ||
		driftSource.fetches != 0 {
		t.Fatalf("scope drift error=%v fetches=%d", err, driftSource.fetches)
	}
	afterDrift, err := stateStore.Load(ctx)
	if err != nil {
		t.Fatalf("load after scope drift: %v", err)
	}
	if afterDrift.Cursor != nextCursor ||
		afterDrift.PagesCommitted != state.PagesCommitted ||
		afterDrift.ItemsReceived != state.ItemsReceived {
		t.Fatalf("scope drift mutated state=%+v", afterDrift)
	}
}

type failFirstPostgresCheckpoint struct {
	*StateStore
	failed bool
}

func (state *failFirstPostgresCheckpoint) CommitPage(
	ctx context.Context,
	page PageCommit,
) error {
	if !state.failed {
		state.failed = true
		return errors.New("injected Checkpoint commit failure")
	}
	return state.StateStore.CommitPage(ctx, page)
}

type rejectingCommitter struct{}

func (*rejectingCommitter) CommitItem(
	context.Context,
	connector.NormalizedItem,
) (connector.ItemConvergence, *connector.ConnectorError) {
	return "", connector.NewConnectorError(
		connector.ErrorInternal,
		"unexpected_commit",
		connector.PhaseCommit,
		"commit_item",
		false,
		"scope drift reached the commit port",
		connector.RecoveryOperatorRepair,
	)
}

func postgresLiteLLMRecord(
	t *testing.T,
	requestID string,
) litellm.SanitizedRecord {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"schema_version":    litellm.SourceSchemaV1,
		"request_id":        requestID,
		"status":            "succeeded",
		"start_time":        "2026-07-28T02:00:00.100000Z",
		"end_time":          "2026-07-28T02:00:01.100000Z",
		"model":             "postgres-test-model",
		"model_group":       "postgres-test",
		"provider":          "test",
		"prompt_tokens":     "2",
		"completion_tokens": "3",
		"total_tokens":      "5",
		"reported_spend":    "0.000001",
		"currency":          "USD",
		"cache_status":      "miss",
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := litellm.DecodeSourceRecord(payload)
	if err != nil {
		t.Fatalf("decode PostgreSQL Source fixture: %v", err)
	}
	return record
}

func postgresTestNonce(t *testing.T) string {
	t.Helper()
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(value)
}

func mustEncodeCheckpoint(t *testing.T, checkpoint Checkpoint) string {
	t.Helper()
	encoded, err := checkpoint.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
