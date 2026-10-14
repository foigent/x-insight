package pullfeed

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/foigent/x-insight/internal/db"
)

func TestPostgresStateStoreValidatesBindingAndPayloads(t *testing.T) {
	store, err := NewPostgresStateStore(
		new(sql.DB),
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
	)
	if err != nil {
		t.Fatalf("new state store: %v", err)
	}
	if store.sourceSystemID == "" || store.adapterManifestID == "" {
		t.Fatal("state store did not retain identity")
	}

	for _, test := range []struct {
		name      string
		sourceID  string
		adapterID string
	}{
		{
			name:      "uppercase source uuid",
			sourceID:  "11111111-1111-4111-8111-11111111111A",
			adapterID: "22222222-2222-4222-8222-222222222222",
		},
		{
			name:      "noncanonical adapter uuid",
			sourceID:  "11111111-1111-4111-8111-111111111111",
			adapterID: "not-a-uuid",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewPostgresStateStore(new(sql.DB), test.sourceID, test.adapterID); err == nil {
				t.Fatal("expected identity validation error")
			}
		})
	}
	if _, err := NewPostgresStateStore(
		new(sql.DB),
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	); err != nil {
		t.Fatalf("accept existing canonical lowercase fixture UUIDs: %v", err)
	}

	now := time.Date(2026, 7, 25, 12, 0, 0, 123456789, time.UTC)
	activation := normalizeActivation(DurableActivation{
		SourceInstance:   "unirouter-development",
		Cursor:           "cursor-activation",
		HighWatermark:    "watermark-activation",
		ActivatedAt:      now,
		AuthorizationRef: "decision://completed-execution-v1",
	})
	if err := validateDurableActivation(activation); err != nil {
		t.Fatalf("validate activation: %v", err)
	}
	if activation.ActivatedAt.Nanosecond()%1000 != 0 {
		t.Fatalf("activation was not normalized to PostgreSQL precision: %s", activation.ActivatedAt)
	}
	if err := validateDurableActivation(DurableActivation{}); err == nil {
		t.Fatal("expected empty activation validation error")
	}
	if err := validateDurableActivation(DurableActivation{
		SourceInstance:   "unirouter-development",
		ActivatedAt:      now,
		AuthorizationRef: "decision://completed-execution-v1",
	}); err == nil {
		t.Fatal("expected empty durable cursor validation error")
	}
	if err := validatePageCommit(PageCommit{
		NextCursor:     "cursor-next",
		HighWatermark:  "watermark-next",
		CompletedAt:    now,
		ItemsReceived:  1,
		ItemsCommitted: 1,
	}); err != nil {
		t.Fatalf("validate page commit: %v", err)
	}
	if err := validatePageCommit(PageCommit{
		NextCursor:     "cursor-next",
		CompletedAt:    now,
		ItemsReceived:  1,
		ItemsCommitted: 1,
		DuplicateNoop:  1,
	}); err == nil {
		t.Fatal("expected invalid page counter error")
	}
	if err := validatePageCommit(PageCommit{
		NextCursor:     "cursor-next",
		CompletedAt:    now,
		ItemsReceived:  2,
		ItemsCommitted: 1,
	}); err == nil {
		t.Fatal("expected incomplete page counter error")
	}
	if failureCodePattern.MatchString("database unavailable: password=secret") {
		t.Fatal("failure code validation accepted an unstructured message")
	}
}

func TestPostgresStateCursorAdvancesOnlyAfterSuccessfulReplay(t *testing.T) {
	dsn := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "..", "migrations")); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	defer database.Close()

	sourceSystemID := stateTestUUID(t)
	adapterManifestID := stateTestUUID(t)
	store, err := NewPostgresStateStore(database, sourceSystemID, adapterManifestID)
	if err != nil {
		t.Fatalf("new state store: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = database.ExecContext(cleanupCtx, `
DELETE FROM adapter_import_states
WHERE source_system_id = $1::uuid AND adapter_manifest_id = $2::uuid`,
			sourceSystemID,
			adapterManifestID,
		)
		_, _ = database.ExecContext(cleanupCtx, `DELETE FROM adapter_manifests WHERE id = $1::uuid`, adapterManifestID)
		_, _ = database.ExecContext(cleanupCtx, `DELETE FROM source_systems WHERE id = $1::uuid`, sourceSystemID)
	})

	setupTx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin setup: %v", err)
	}
	if _, err := setupTx.ExecContext(ctx, `
INSERT INTO source_systems (id, source_key, source_type, display_name)
VALUES ($1::uuid, $2, 'unirouter', 'Pull worker state test')`,
		sourceSystemID,
		"pull-worker-state-"+sourceSystemID,
	); err != nil {
		_ = setupTx.Rollback()
		t.Fatalf("insert source system: %v", err)
	}
	if _, err := setupTx.ExecContext(ctx, `
INSERT INTO adapter_manifests (
  id,
  adapter_id,
  adapter_version,
  source_type,
  fidelity_level,
  payload_behavior,
  idempotency_strategy
) VALUES (
  $1::uuid,
  $2,
  '1.0.0',
  'unirouter',
  'F2',
  'sanitized',
  'event_hash'
)`,
		adapterManifestID,
		"pull-worker-state-"+adapterManifestID,
	); err != nil {
		_ = setupTx.Rollback()
		t.Fatalf("insert adapter manifest: %v", err)
	}
	activatedAt := time.Now().UTC().Add(-time.Minute)
	if err := setupTx.Commit(); err != nil {
		t.Fatalf("commit setup: %v", err)
	}

	state, err := store.Activate(ctx, DurableActivation{
		SourceInstance:   "unirouter-development",
		Cursor:           "cursor-0",
		HighWatermark:    "watermark-0",
		ActivatedAt:      activatedAt,
		AuthorizationRef: "decision://completed-execution-v1-test",
	})
	if err != nil {
		t.Fatalf("activate state: %v", err)
	}
	if state.Cursor != "cursor-0" || state.Status != WorkerStatusIdle {
		t.Fatalf("activated state=%+v", state)
	}
	var metadataContract string
	var metadataStateVersion int
	if err := database.QueryRowContext(ctx, `
SELECT
  metadata->>'contract',
  (metadata->>'state_version')::integer
FROM adapter_import_states
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3::text`,
		sourceSystemID,
		adapterManifestID,
		CompletedExecutionCursorKey,
	).Scan(&metadataContract, &metadataStateVersion); err != nil {
		t.Fatalf("read activation metadata: %v", err)
	}
	if metadataContract != CompletedExecutionCursorKey || metadataStateVersion != 1 {
		t.Fatalf(
			"activation metadata contract=%q state_version=%d",
			metadataContract,
			metadataStateVersion,
		)
	}
	if _, err := store.Activate(ctx, DurableActivation{
		SourceInstance:   "unirouter-development",
		Cursor:           "cursor-0",
		HighWatermark:    "watermark-0",
		ActivatedAt:      activatedAt,
		AuthorizationRef: "decision://completed-execution-v1-test",
	}); err != nil {
		t.Fatalf("idempotent activation: %v", err)
	}
	if _, err := store.Activate(ctx, DurableActivation{
		SourceInstance:   "unirouter-other",
		Cursor:           "cursor-0",
		HighWatermark:    "watermark-0",
		ActivatedAt:      activatedAt,
		AuthorizationRef: "decision://completed-execution-v1-test",
	}); !errors.Is(err, ErrWorkerStateActivationConflict) {
		t.Fatalf("source instance activation conflict=%v", err)
	}
	startedAt := time.Now().UTC()
	if err := store.MarkPollStarted(ctx, "cursor-0", startedAt); err != nil {
		t.Fatalf("mark started before partial failure: %v", err)
	}
	// One item may already have been committed by EvidenceStore when a later
	// item fails. Recording the failure must leave this state cursor unchanged.
	if err := store.MarkFailure(ctx, "cursor-0", WorkerFailure{
		OccurredAt: startedAt.Add(time.Second),
		Code:       "page_item_persist_failed",
		Retryable:  true,
	}); err != nil {
		t.Fatalf("mark partial page failure: %v", err)
	}
	state, err = store.Load(ctx)
	if err != nil {
		t.Fatalf("load after partial failure: %v", err)
	}
	if state.Cursor != "cursor-0" ||
		state.PagesCommitted != 0 ||
		state.Status != WorkerStatusBackoff ||
		state.ConsecutiveFailures != 1 {
		t.Fatalf("partial failure advanced cursor or counters: %+v", state)
	}

	// The retry sees the already-written event as a duplicate-noop, completes
	// the remaining item, and only then advances the page cursor.
	if err := store.MarkPollStarted(ctx, "cursor-0", startedAt.Add(2*time.Second)); err != nil {
		t.Fatalf("mark replay started: %v", err)
	}
	if err := store.CommitPage(ctx, "cursor-stale", PageCommit{
		NextCursor:    "cursor-wrong",
		HighWatermark: "watermark-wrong",
		CompletedAt:   startedAt.Add(3 * time.Second),
	}); !errors.Is(err, ErrWorkerStateConflict) {
		t.Fatalf("stale cursor error=%v", err)
	}
	if err := store.CommitPage(ctx, "cursor-0", PageCommit{
		NextCursor:     "cursor-1",
		HighWatermark:  "watermark-1",
		CompletedAt:    startedAt.Add(3 * time.Second),
		ItemsReceived:  2,
		ItemsCommitted: 1,
		DuplicateNoop:  1,
	}); err != nil {
		t.Fatalf("commit page: %v", err)
	}
	state, err = store.Load(ctx)
	if err != nil {
		t.Fatalf("load committed state: %v", err)
	}
	if state.Cursor != "cursor-1" ||
		state.HighWatermark != "watermark-1" ||
		state.PagesCommitted != 1 ||
		state.ItemsReceived != 2 ||
		state.ItemsCommitted != 1 ||
		state.DuplicateNoop != 1 ||
		state.Status != WorkerStatusIdle {
		t.Fatalf("committed state=%+v", state)
	}

	if state.ConsecutiveFailures != 0 || state.LastErrorCode != "" {
		t.Fatalf("successful replay did not clear active failure state: %+v", state)
	}

	if _, err := database.ExecContext(ctx, `
UPDATE source_systems SET status = 'inactive' WHERE id = $1::uuid`,
		sourceSystemID,
	); err != nil {
		t.Fatalf("deactivate source identity: %v", err)
	}
	if err := store.MarkPollStarted(ctx, "cursor-1", startedAt.Add(4*time.Second)); !errors.Is(err, ErrWorkerStateConflict) {
		t.Fatalf("inactive source state transition error=%v", err)
	}
	if _, err := store.Load(ctx); !errors.Is(err, ErrWorkerStateIdentityMismatch) {
		t.Fatalf("inactive source load error=%v", err)
	}
}

func stateTestUUID(t *testing.T) string {
	t.Helper()
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		t.Fatalf("random bytes: %v", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}
