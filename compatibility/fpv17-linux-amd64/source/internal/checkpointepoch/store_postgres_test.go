package checkpointepoch_test

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	. "github.com/foigent/x-insight/internal/checkpointepoch"
	"github.com/foigent/x-insight/internal/db"
	"github.com/foigent/x-insight/internal/integration/pullfeed"
)

func TestPostgresEpochTransitionArchivesAndAtomicallyRebindsCheckpoint(t *testing.T) {
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	dsn := newEpochTestDatabase(t, ctx, baseDSN)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	sourceID := epochTestUUID(t)
	adapterID := epochTestUUID(t)
	if _, err := database.ExecContext(ctx, `
INSERT INTO source_systems (id, source_key, source_type, display_name)
VALUES ($1::uuid, $2, 'unirouter', 'Epoch transition test')`, sourceID, "epoch-source-"+sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO adapter_manifests (
  id, adapter_id, adapter_version, source_type, fidelity_level,
  payload_behavior, idempotency_strategy
) VALUES ($1::uuid, $2, '1.0.0', 'unirouter', 'F2', 'metadata_only_default', 'source_ref')`,
		adapterID, "epoch-adapter-"+adapterID); err != nil {
		t.Fatal(err)
	}
	workerState, err := pullfeed.NewPostgresStateStore(database, sourceID, adapterID)
	if err != nil {
		t.Fatal(err)
	}
	firstActivatedAt := time.Date(2026, 8, 1, 1, 0, 0, 123456000, time.UTC)
	first, err := workerState.Activate(ctx, pullfeed.DurableActivation{
		SourceInstance: "unirouter-campaign-1", Cursor: "cursor-campaign-1",
		HighWatermark: "cursor-campaign-1", ActivatedAt: firstActivatedAt,
		AuthorizationRef: "decision://campaign-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := workerState.MarkInactive(ctx, first.Cursor, firstActivatedAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	downSQL, err := os.ReadFile(filepath.Join("..", "..", "migrations", "000020_create_connector_checkpoint_epochs.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
UPDATE adapter_import_states
SET checkpoint_epoch = 2
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3`, sourceID, adapterID, pullfeed.CompletedExecutionCursorKey); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, string(downSQL)); err == nil || !strings.Contains(err.Error(), "epoch evidence exists") {
		t.Fatalf("down migration accepted active-state epoch drift: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
UPDATE adapter_import_states
SET checkpoint_epoch = 1
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3`, sourceID, adapterID, pullfeed.CompletedExecutionCursorKey); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(database)
	if err != nil {
		t.Fatal(err)
	}
	binding := Binding{SourceSystemID: sourceID, AdapterManifestID: adapterID, CursorKey: pullfeed.CompletedExecutionCursorKey}
	preparedAt := time.Date(2026, 8, 5, 2, 0, 0, 654321000, time.UTC)
	prepare, err := store.Prepare(ctx, PrepareInput{
		Binding: binding, WorkerUnit: CompletedExecutionWorkerUnit,
		NextSourceInstance: "unirouter-campaign-2", NextCursor: "cursor-campaign-2",
		NextHighWatermark: "cursor-campaign-2", AuthorizationRef: "decision://campaign-2",
	}, strings.Repeat("1", 32), preparedAt)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if prepare.Operation == nil || prepare.Operation.Status != StatusPrepared ||
		prepare.Operation.Plan.PreviousEpoch != 1 || prepare.Operation.Plan.NextEpoch != 2 ||
		prepare.Operation.Plan.PreviousSourceInstance != "unirouter-campaign-1" {
		t.Fatalf("prepare report=%+v", prepare)
	}
	encoded, err := jsonBytes(prepare)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "cursor-campaign-1") || strings.Contains(string(encoded), "cursor-campaign-2") {
		t.Fatalf("prepare report leaked cursor: %s", encoded)
	}
	if err := workerState.MarkPollStarted(ctx, first.Cursor, preparedAt.Add(time.Second)); !errors.Is(err, pullfeed.ErrWorkerStateConflict) {
		t.Fatalf("prepared epoch did not gate worker poll start: %v", err)
	}
	if _, err := store.Prepare(ctx, PrepareInput{
		Binding: binding, WorkerUnit: CompletedExecutionWorkerUnit,
		NextSourceInstance: "unirouter-campaign-conflict", NextCursor: "cursor-conflict",
		NextHighWatermark: "cursor-conflict", AuthorizationRef: "decision://campaign-conflict",
	}, strings.Repeat("9", 32), preparedAt.Add(2*time.Second)); !errors.Is(err, ErrConflict) {
		t.Fatalf("second prepared operation error=%v", err)
	}

	activatedAt := preparedAt.Add(time.Minute)
	activated, err := store.Activate(ctx, prepare.Operation.OperationID, prepare.Operation.Plan.PlanSHA256, activatedAt)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	if activated.Active.Epoch != 2 || activated.Active.SourceInstance != "unirouter-campaign-2" ||
		activated.Active.EpochOperationID != strings.Repeat("1", 32) ||
		activated.Active.PagesCommitted != 0 || activated.Active.ItemsReceived != 0 ||
		activated.Active.WorkerStatus != "inactive" {
		t.Fatalf("activated report=%+v", activated)
	}
	evidenceStore, err := db.OpenEvidenceStore(ctx, dsn)
	if err != nil {
		t.Fatalf("open read model store: %v", err)
	}
	connectorTrust, err := evidenceStore.ListConnectorTrust(ctx, 10)
	if closeErr := evidenceStore.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("read connector trust after epoch activation: %v", err)
	}
	if len(connectorTrust) != 1 || connectorTrust[0].Checkpoint.Epoch != 2 ||
		connectorTrust[0].Checkpoint.EpochOperationID != strings.Repeat("1", 32) ||
		connectorTrust[0].Checkpoint.CursorDigest != TestHashText("cursor-campaign-2") ||
		connectorTrust[0].Checkpoint.HighWatermarkDigest != TestHashText("cursor-campaign-2") {
		t.Fatalf("connector trust epoch projection=%+v", connectorTrust)
	}

	var historySource, historyCursor, historySnapshot string
	var historyEpoch int64
	if err := database.QueryRowContext(ctx, `
SELECT source_instance, final_cursor, checkpoint_epoch, snapshot_sha256
FROM connector_checkpoint_epoch_history
WHERE operation_id = $1`, strings.Repeat("1", 32)).Scan(
		&historySource, &historyCursor, &historyEpoch, &historySnapshot,
	); err != nil {
		t.Fatal(err)
	}
	if historySource != "unirouter-campaign-1" || historyCursor != "cursor-campaign-1" ||
		historyEpoch != 1 || !TestSHA256Pattern.MatchString(historySnapshot) {
		t.Fatalf("history source=%q cursor=%q epoch=%d hash=%q", historySource, historyCursor, historyEpoch, historySnapshot)
	}
	if _, err := database.ExecContext(ctx, `UPDATE connector_checkpoint_epoch_history SET retired_at = retired_at WHERE operation_id = $1`, strings.Repeat("1", 32)); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("history update error=%v", err)
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM connector_checkpoint_epoch_history WHERE operation_id = $1`, strings.Repeat("1", 32)); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("history delete error=%v", err)
	}

	idempotent, err := store.Activate(ctx, prepare.Operation.OperationID, prepare.Operation.Plan.PlanSHA256, activatedAt.Add(time.Minute))
	if err != nil || idempotent.Active.Epoch != 2 {
		t.Fatalf("idempotent activation report=%+v err=%v", idempotent, err)
	}

	abortPrepare, err := store.Prepare(ctx, PrepareInput{
		Binding: binding, WorkerUnit: CompletedExecutionWorkerUnit,
		NextSourceInstance: "unirouter-campaign-3", NextCursor: "cursor-campaign-3",
		NextHighWatermark: "cursor-campaign-3", AuthorizationRef: "decision://campaign-3",
	}, strings.Repeat("2", 32), activatedAt.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	aborted, err := store.Abort(ctx, abortPrepare.Operation.OperationID, abortPrepare.Operation.Plan.PlanSHA256, activatedAt.Add(3*time.Minute))
	if err != nil || aborted.Operation == nil || aborted.Operation.Status != StatusAborted || aborted.Active.Epoch != 2 {
		t.Fatalf("abort report=%+v err=%v", aborted, err)
	}

	stalePrepare, err := store.Prepare(ctx, PrepareInput{
		Binding: binding, WorkerUnit: CompletedExecutionWorkerUnit,
		NextSourceInstance: "unirouter-campaign-4", NextCursor: "cursor-campaign-4",
		NextHighWatermark: "cursor-campaign-4", AuthorizationRef: "decision://campaign-4",
	}, strings.Repeat("3", 32), activatedAt.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
UPDATE adapter_import_states SET updated_at = updated_at + interval '1 microsecond'
WHERE source_system_id = $1::uuid AND adapter_manifest_id = $2::uuid AND cursor_key = $3`,
		sourceID, adapterID, pullfeed.CompletedExecutionCursorKey); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Activate(ctx, stalePrepare.Operation.OperationID, stalePrepare.Operation.Plan.PlanSHA256, activatedAt.Add(5*time.Minute)); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale activation error=%v", err)
	}
	if _, err := store.Abort(ctx, stalePrepare.Operation.OperationID, stalePrepare.Operation.Plan.PlanSHA256, activatedAt.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}

	if _, err := database.ExecContext(ctx, string(downSQL)); err == nil || !strings.Contains(err.Error(), "epoch evidence exists") {
		t.Fatalf("down migration error=%v", err)
	}
}

func TestEmptyDatabaseBootstrapUsesProvedEpochAndLeavesHistoryEmpty(t *testing.T) {
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	dsn := newEpochTestDatabase(t, ctx, baseDSN)
	if _, err := db.RunMigrations(ctx, dsn, filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	input := EmptyBootstrapInput{
		PrepareInput: PrepareInput{
			Binding: Binding{
				SourceSystemID:    "11111111-1111-1111-1111-111111111111",
				AdapterManifestID: "22222222-2222-4222-8222-222222222224",
				CursorKey:         CompletedExecutionCursorKey,
			},
			WorkerUnit:         CompletedExecutionWorkerUnit,
			NextSourceInstance: "ecs.second-integration.v2",
			NextCursor:         "empty-cursor-v2",
			NextHighWatermark:  "empty-cursor-v2",
			AuthorizationRef:   "decision://second-integration",
		},
		CheckpointEpoch: 7,
	}
	activatedAt := time.Date(2026, 8, 5, 4, 0, 0, 123456000, time.UTC)
	active, err := BootstrapEmpty(ctx, database, input, activatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if active.Epoch != 7 || active.EpochOperationID != "" ||
		active.SourceInstance != input.NextSourceInstance ||
		active.WorkerStatus != "inactive" || active.PagesCommitted != 0 ||
		active.ItemsReceived != 0 || active.ItemsCommitted != 0 ||
		active.DuplicateNoop != 0 || active.ConsecutiveFailures != 0 {
		t.Fatalf("active=%+v", active)
	}
	var operations, history int64
	if err := database.QueryRowContext(ctx, `
SELECT
  (SELECT count(*) FROM connector_checkpoint_epoch_operations),
  (SELECT count(*) FROM connector_checkpoint_epoch_history)`).Scan(&operations, &history); err != nil {
		t.Fatal(err)
	}
	if operations != 0 || history != 0 {
		t.Fatalf("operations=%d history=%d", operations, history)
	}
	if _, err := BootstrapEmpty(ctx, database, input, activatedAt.Add(time.Second)); !errors.Is(err, ErrConflict) {
		t.Fatalf("second empty bootstrap error=%v", err)
	}
}

func jsonBytes(value any) ([]byte, error) {
	return json.Marshal(value)
}

func newEpochTestDatabase(t *testing.T, ctx context.Context, baseDSN string) string {
	t.Helper()
	parsed, err := url.Parse(baseDSN)
	if err != nil || parsed.Scheme == "" {
		t.Fatalf("epoch test requires URL DSN: %v", err)
	}
	databaseName := "checkpoint_epoch_" + strings.ReplaceAll(epochTestUUID(t), "-", "")
	admin, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", databaseName)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		cleanup, openErr := sql.Open("pgx", baseDSN)
		if openErr != nil {
			return
		}
		defer cleanup.Close()
		_, _ = cleanup.ExecContext(cleanupCtx, fmt.Sprintf("DROP DATABASE %s WITH (FORCE)", databaseName))
	})
	parsed.Path = "/" + databaseName
	return parsed.String()
}

func epochTestUUID(t *testing.T) string {
	t.Helper()
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		t.Fatal(err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}
