package pullfeed

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/foigent/x-insight/internal/adapters/unirouter"
	"github.com/foigent/x-insight/internal/db"
)

func TestReferenceNeutralCanaryPersistsLegacyExactRowsAndReplaysWithOneCheckpoint(
	t *testing.T,
) {
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	requestID := "req_phase2c_" + stateTestUUID(t)[:8]
	sourceInstance := "unirouter-phase2c-" + stateTestUUID(t)[:8]
	item := phase2CPostgresItem(t, requestID)
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	page := FeedPage{
		SchemaVersion:  SchemaVersion,
		SourceInstance: sourceInstance,
		GeneratedAt:    now.Add(-time.Second),
		Items:          []FeedItem{item},
		NextCursor:     "cursor-phase2c-1",
		HighWatermark:  "watermark-phase2c-1",
	}
	legacy, workerErr := prepareLegacyEvidenceItem(page, item, now)
	if workerErr != nil {
		t.Fatalf("prepare legacy baseline: %v", workerErr)
	}

	cleanup := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
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
		_, _ = database.ExecContext(
			cleanupCtx,
			`DELETE FROM import_provenance WHERE source_payload_sha256 = $1`,
			item.SnapshotSHA256,
		)
		_, _ = database.ExecContext(cleanupCtx, `
DELETE FROM adapter_import_states
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3`,
			unirouter.DefaultSourceSystemID,
			unirouter.DefaultCompletedExecutionAdapterManifestID,
			CompletedExecutionCursorKey,
		)
	}
	cleanup()
	t.Cleanup(cleanup)

	stateStore, err := NewPostgresStateStore(
		database,
		unirouter.DefaultSourceSystemID,
		unirouter.DefaultCompletedExecutionAdapterManifestID,
	)
	if err != nil {
		t.Fatalf("new state store: %v", err)
	}
	if _, err := stateStore.Activate(ctx, DurableActivation{
		SourceInstance:   sourceInstance,
		Cursor:           "cursor-phase2c-0",
		HighWatermark:    "watermark-phase2c-0",
		ActivatedAt:      now.Add(-time.Minute),
		AuthorizationRef: "decision://phase2c-reference-runtime-test",
	}); err != nil {
		t.Fatalf("activate state: %v", err)
	}
	projector, err := unirouter.NewCompletedExecutionReferenceProjector()
	if err != nil {
		t.Fatalf("new Reference projector: %v", err)
	}
	committer, err := NewModeAwareEvidenceCommitter(
		evidenceStore,
		ReferenceMappingNeutralCanary,
		projector,
		func() time.Time { return now },
		nil,
	)
	if err != nil {
		t.Fatalf("new canary committer: %v", err)
	}
	checkpointStore := &failFirstCheckpointStore{
		LiveWorkerStateStore: stateStore,
	}
	payload := mustLivePage(t, page)
	worker, err := NewLiveWorker(
		&liveSequenceSource{payloads: [][]byte{payload, payload}},
		checkpointStore,
		committer,
		LiveWorkerConfig{
			PollInterval: time.Second,
			Now:          func() time.Time { return now },
			Sleep:        func(context.Context, time.Duration) error { return nil },
		},
	)
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	if _, err := worker.PollOnce(ctx); err == nil {
		t.Fatal("first poll unexpectedly committed the injected failing Checkpoint")
	} else {
		var liveErr *LiveWorkerError
		if !errors.As(err, &liveErr) ||
			!liveErr.Retryable ||
			liveErr.Code != "state_page_commit_failed" {
			t.Fatalf("first poll error=%v", err)
		}
	}
	beforeReplay, err := stateStore.Load(ctx)
	if err != nil {
		t.Fatalf("load before replay: %v", err)
	}
	if beforeReplay.Cursor != "cursor-phase2c-0" ||
		beforeReplay.PagesCommitted != 0 {
		t.Fatalf("failed Checkpoint advanced state: %+v", beforeReplay)
	}

	replay, err := worker.PollOnce(ctx)
	if err != nil {
		t.Fatalf("replay poll: %v", err)
	}
	if replay.ItemsReceived != 1 ||
		replay.ItemsCommitted != 0 ||
		replay.DuplicateNoop != 1 ||
		replay.CompatibilityMatched != 1 ||
		!replay.CompatibilityEligible {
		t.Fatalf("replay report=%+v", replay)
	}
	afterReplay, err := stateStore.Load(ctx)
	if err != nil {
		t.Fatalf("load after replay: %v", err)
	}
	if afterReplay.Cursor != page.NextCursor ||
		afterReplay.HighWatermark != page.HighWatermark ||
		afterReplay.PagesCommitted != 1 ||
		afterReplay.ItemsReceived != 1 ||
		afterReplay.ItemsCommitted != 0 ||
		afterReplay.DuplicateNoop != 1 {
		t.Fatalf("replay state=%+v", afterReplay)
	}

	records, err := evidenceStore.ListEvidenceEventsByRequestID(ctx, requestID, 10)
	if err != nil {
		t.Fatalf("list persisted events: %v", err)
	}
	if len(records) != len(legacy.events) {
		t.Fatalf("persisted events=%d legacy=%d", len(records), len(legacy.events))
	}
	recordsByID := make(map[string]db.EvidenceEventRecord, len(records))
	for _, record := range records {
		recordsByID[record.ID] = record
	}
	for _, expected := range legacy.events {
		assertPersistedReferenceEventMatchesLegacy(
			t,
			ctx,
			database,
			recordsByID[expected["id"].(string)],
			expected,
		)
	}

	gaps, err := evidenceStore.ListCaptureGapsByRequestID(ctx, requestID, 10)
	if err != nil {
		t.Fatalf("list persisted gaps: %v", err)
	}
	if len(gaps) != 4 {
		t.Fatalf("capture gaps=%d", len(gaps))
	}
	expectedGaps := make(map[string]map[string]any, 4)
	for _, event := range legacy.events {
		if event["event_type"] == "gap.capture.detected" {
			expectedGaps[event["id"].(string)] = event
		}
	}
	for _, gap := range gaps {
		event, exists := expectedGaps[gap.EvidenceEventID]
		if !exists {
			t.Fatalf("capture gap linked to unexpected event %s", gap.EvidenceEventID)
		}
		raw := event["raw_source_excerpt"].(map[string]any)
		expectedCandidates := expectedStringSlice(raw["candidate_capture_gaps"])
		if gap.SourceSystemID != event["source_system_id"] ||
			gap.AdapterManifestID != event["adapter_manifest_id"] ||
			gap.RequestID != requestID ||
			gap.GapType != raw["capture_gap"] ||
			!reflect.DeepEqual(gap.CandidateGapTypes, expectedCandidates) ||
			gap.AffectedClaim != raw["gap_source"] ||
			gap.GapReason != raw["reason"] ||
			gap.Severity != event["severity"] ||
			gap.Recommendation != "review source visibility and adapter coverage" ||
			gap.CreatedAt.IsZero() {
			t.Fatalf("gap=%+v expected_event=%+v", gap, event)
		}
	}

	var provenanceID string
	var sourceSystemID string
	var adapterManifestID string
	var integrationMode string
	var proofClass string
	var assuranceMode string
	var payloadHash string
	var sourceTerminal time.Time
	var sourceRevision string
	var importedRevision string
	var sourceWatermark sql.NullString
	var importWatermark sql.NullString
	var timelineComplete bool
	if err := database.QueryRowContext(ctx, `
SELECT
  p.id::text,
  p.source_system_id::text,
  p.adapter_manifest_id::text,
  p.integration_mode,
  p.proof_class,
  p.assurance_mode,
  p.source_payload_sha256,
  p.source_terminal_at,
  p.source_revision,
  p.imported_revision,
  p.source_watermark,
  p.import_watermark,
  p.timeline_complete
FROM import_provenance p
JOIN import_provenance_events ipe ON ipe.import_provenance_id = p.id
JOIN evidence_events e ON e.id = ipe.evidence_event_id
WHERE e.request_id = $1
GROUP BY p.id`,
		requestID,
	).Scan(
		&provenanceID,
		&sourceSystemID,
		&adapterManifestID,
		&integrationMode,
		&proofClass,
		&assuranceMode,
		&payloadHash,
		&sourceTerminal,
		&sourceRevision,
		&importedRevision,
		&sourceWatermark,
		&importWatermark,
		&timelineComplete,
	); err != nil {
		t.Fatalf("read provenance: %v", err)
	}
	if provenanceID == "" ||
		sourceSystemID != legacy.provenance.SourceSystemID ||
		adapterManifestID != legacy.provenance.AdapterManifestID ||
		integrationMode != legacy.provenance.IntegrationMode ||
		proofClass != legacy.provenance.ProofClass ||
		assuranceMode != legacy.provenance.AssuranceMode ||
		payloadHash != legacy.provenance.SourcePayloadSHA256 ||
		legacy.provenance.SourceTerminalAt == nil ||
		!sourceTerminal.Equal(*legacy.provenance.SourceTerminalAt) ||
		sourceRevision != legacy.provenance.SourceRevision ||
		importedRevision != legacy.provenance.ImportedRevision ||
		sourceWatermark.Valid ||
		importWatermark.Valid ||
		timelineComplete != legacy.provenance.TimelineComplete {
		t.Fatalf(
			"provenance id=%s source=%s adapter=%s mode=%s proof=%s assurance=%s payload=%s terminal=%s revisions=%s/%s watermarks=%+v/%+v complete=%t",
			provenanceID,
			sourceSystemID,
			adapterManifestID,
			integrationMode,
			proofClass,
			assuranceMode,
			payloadHash,
			sourceTerminal,
			sourceRevision,
			importedRevision,
			sourceWatermark,
			importWatermark,
			timelineComplete,
		)
	}
	var linkCount int
	if err := database.QueryRowContext(ctx, `
SELECT count(*)
FROM import_provenance_events ipe
JOIN evidence_events e ON e.id = ipe.evidence_event_id
WHERE ipe.import_provenance_id = $1::uuid
  AND e.request_id = $2`,
		provenanceID,
		requestID,
	).Scan(&linkCount); err != nil {
		t.Fatalf("count provenance links: %v", err)
	}
	if linkCount != len(legacy.events) {
		t.Fatalf("provenance links=%d events=%d", linkCount, len(legacy.events))
	}
}

type failFirstCheckpointStore struct {
	LiveWorkerStateStore
	failed bool
}

func (store *failFirstCheckpointStore) CommitPage(
	ctx context.Context,
	expectedCursor string,
	commit PageCommit,
) error {
	if !store.failed {
		store.failed = true
		return errors.New("injected checkpoint failure")
	}
	return store.LiveWorkerStateStore.CommitPage(ctx, expectedCursor, commit)
}

func phase2CPostgresItem(t *testing.T, requestID string) FeedItem {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(
		"..", "..", "..", "contracts", "completed-execution", "v1",
		"fixtures", "valid-settled.json",
	))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	snapshot["request_id"] = requestID
	payload, err = json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	decoded, _, err := unirouter.DecodeCompletedExecutionV1(payload)
	if err != nil {
		t.Fatalf("decode completed execution: %v", err)
	}
	digest := sha256.Sum256(payload)
	return FeedItem{
		RequestID:           requestID,
		TerminalAvailableAt: decoded.Terminal.AvailableAt,
		Revision:            unirouter.CompletedExecutionRevisionV1,
		TimelineComplete:    true,
		SnapshotSHA256:      "sha256:" + hex.EncodeToString(digest[:]),
		Snapshot:            payload,
	}
}

func assertPersistedReferenceEventMatchesLegacy(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	record db.EvidenceEventRecord,
	expected map[string]any,
) {
	t.Helper()
	if record.ID != expected["id"] ||
		record.SchemaVersion != expected["schema_version"] ||
		record.SourceSystemID != expected["source_system_id"] ||
		record.AdapterManifestID != expected["adapter_manifest_id"] ||
		record.SourceType != expected["source_type"] ||
		record.SourceVersion != expected["source_version"] ||
		record.SourceEventID != expected["source_event_id"] ||
		record.SourceEventType != expected["source_event_type"] ||
		record.SourceRef != expected["source_ref"] ||
		record.EventType != expected["event_type"] ||
		record.Stage != expected["stage"] ||
		record.Severity != expected["severity"] ||
		record.CaptureMode != expected["capture_mode"] ||
		record.RequestID != expected["request_id"] ||
		record.OrgRef != expected["org_ref"] ||
		record.ProjectRef != expected["project_ref"] ||
		record.PayloadCaptureMode != expected["payload_capture_mode"] ||
		record.EventHash != expected["event_hash"] ||
		record.SourceEventHash != expected["source_event_hash"] {
		t.Fatalf("persisted record=%+v expected=%+v", record, expected)
	}
	for name, comparison := range map[string]struct {
		actual   *time.Time
		expected any
	}{
		"occurred_at": {actual: record.OccurredAt, expected: expected["occurred_at"]},
		"observed_at": {actual: record.ObservedAt, expected: expected["observed_at"]},
		"ingested_at": {actual: &record.IngestedAt, expected: expected["ingested_at"]},
	} {
		expectedTime, ok := parseExpectedTime(comparison.expected)
		if !ok ||
			comparison.actual == nil ||
			!comparison.actual.Equal(expectedTime) {
			t.Fatalf(
				"%s actual=%v expected=%v",
				name,
				comparison.actual,
				comparison.expected,
			)
		}
	}
	if record.TraceID != expectedOptionalString(expected["trace_id"]) ||
		record.SpanID != expectedOptionalString(expected["span_id"]) ||
		record.CreatedAt.IsZero() {
		t.Fatalf(
			"trace/span/created actual=%q/%q/%s expected=%v/%v/nonzero",
			record.TraceID,
			record.SpanID,
			record.CreatedAt,
			expected["trace_id"],
			expected["span_id"],
		)
	}
	for name, pair := range map[string][2]any{
		"correlation":        {record.Correlation, expected["correlation"]},
		"identity":           {record.Identity, expected["identity"]},
		"capability":         {record.Capability, expected["capability"]},
		"decision":           {record.Decision, expected["decision"]},
		"usage":              {record.Usage, expected["usage"]},
		"outcome":            {record.Outcome, expected["outcome"]},
		"payload_policy":     {record.PayloadPolicy, expected["payload_policy"]},
		"field_provenance":   {record.FieldProvenance, expected["field_provenance"]},
		"raw_source_excerpt": {record.RawSourceExcerpt, expected["raw_source_excerpt"]},
	} {
		actualJSON, err := json.Marshal(pair[0])
		if err != nil {
			t.Fatal(err)
		}
		expectedJSON, err := json.Marshal(pair[1])
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(actualJSON, expectedJSON) {
			t.Fatalf("%s actual=%s expected=%s", name, actualJSON, expectedJSON)
		}
	}
	var idempotencyKey string
	var importCursor sql.NullString
	if err := database.QueryRowContext(ctx, `
SELECT idempotency_key, import_cursor
FROM evidence_events
WHERE id = $1::uuid`,
		record.ID,
	).Scan(&idempotencyKey, &importCursor); err != nil {
		t.Fatalf("read event identity: %v", err)
	}
	if idempotencyKey != expected["idempotency_key"] ||
		!importCursor.Valid ||
		importCursor.String != expected["import_cursor"] {
		t.Fatalf(
			"idempotency=%q import_cursor=%+v expected=%q/%q",
			idempotencyKey,
			importCursor,
			expected["idempotency_key"],
			expected["import_cursor"],
		)
	}
}

func parseExpectedTime(value any) (time.Time, bool) {
	text, ok := value.(string)
	if !ok {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	return parsed, err == nil
}

func expectedOptionalString(value any) string {
	text, _ := value.(string)
	return text
}

func expectedStringSlice(value any) []string {
	switch values := value.(type) {
	case nil:
		return nil
	case []string:
		return append([]string(nil), values...)
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				return nil
			}
			result = append(result, text)
		}
		return result
	default:
		return nil
	}
}
