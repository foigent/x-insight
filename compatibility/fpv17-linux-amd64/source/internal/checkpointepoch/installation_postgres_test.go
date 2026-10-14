package checkpointepoch_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/foigent/x-insight/internal/checkpointepoch"
	"github.com/foigent/x-insight/internal/db"
)

func TestNewPostgresInstallationEpochPortRejectsNilDatabase(t *testing.T) {
	if _, err := NewPostgresInstallationEpochPort(nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("error=%v", err)
	}
}

func TestPostgresInstallationEpochPortFirstReplayMaxPlusOneAndConflicts(t *testing.T) {
	baseDSN := os.Getenv("XINSIGHT_POSTGRES_DSN")
	if baseDSN == "" {
		t.Skip("XINSIGHT_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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

	binding := insertInstallationEpochCatalog(t, ctx, database)
	port, err := NewPostgresInstallationEpochPort(database)
	if err != nil {
		t.Fatal(err)
	}
	installedAt := time.Date(2026, 8, 11, 12, 0, 0, 123456000, time.UTC)
	firstInput := postgresInstallationEpochInput(binding, TestInstallationA, "Source.Installation.A")
	first, err := ApplyInstallationEpoch(ctx, port, firstInput, installedAt)
	if err != nil {
		t.Fatalf("first installation epoch: %v", err)
	}
	if first.Epoch != 1 || first.Replayed || first.Active.WorkerStatus != "inactive" ||
		first.Active.EpochOperationID != "" || first.Active.PagesCommitted != 0 ||
		first.Active.ItemsReceived != 0 || first.Active.ItemsCommitted != 0 ||
		first.Active.DuplicateNoop != 0 || first.Active.ConsecutiveFailures != 0 ||
		first.Active.LastPollSucceededAt != nil {
		t.Fatalf("first authority=%+v", first)
	}
	assertInstallationEpochDatabaseState(t, ctx, database, binding, 1, firstInput, 0, 0)

	replay, err := ApplyInstallationEpoch(ctx, port, firstInput, installedAt.Add(time.Second))
	if err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	if !replay.Replayed || replay.Epoch != first.Epoch || replay.Active.StateID != first.Active.StateID ||
		replay.Active.StateSHA256 != first.Active.StateSHA256 {
		t.Fatalf("replay=%+v first=%+v", replay, first)
	}
	directReplayCommit := installationEpochCommitForTest(t, firstInput, first.BoundarySHA256, first.Epoch)
	directReplay, err := port.CommitInstallationEpoch(ctx, directReplayCommit, installedAt.Add(2*time.Second))
	if err != nil || directReplay.StateID != first.Active.StateID {
		t.Fatalf("direct replay=%+v err=%v", directReplay, err)
	}
	tamperedReplay := directReplayCommit
	tamperedReplay.ExpectedBoundarySHA256 = "sha256:" + strings.Repeat("f", 64)
	if _, err := port.CommitInstallationEpoch(ctx, tamperedReplay, installedAt.Add(3*time.Second)); !errors.Is(err, ErrConflict) {
		t.Fatalf("tampered replay boundary error=%v", err)
	}
	if _, err := database.ExecContext(ctx, `
UPDATE adapter_import_states
SET metadata = jsonb_set(
  metadata,
  '{installation_epoch_request_sha256}',
  to_jsonb($4::text)
)
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3`,
		binding.SourceSystemID,
		binding.AdapterManifestID,
		binding.CursorKey,
		"sha256:"+strings.Repeat("e", 64),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyInstallationEpoch(ctx, port, firstInput, installedAt.Add(4*time.Second)); !errors.Is(err, ErrConflict) {
		t.Fatalf("tampered durable authority error=%v", err)
	}
	if _, err := database.ExecContext(ctx, `
UPDATE adapter_import_states
SET
  metadata = jsonb_set(
    metadata,
    '{installation_epoch_request_sha256}',
    to_jsonb($4::text)
  ),
  last_import_started_at = $5
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3`,
		binding.SourceSystemID,
		binding.AdapterManifestID,
		binding.CursorKey,
		directReplayCommit.RequestSHA256,
		installedAt.Add(5*time.Second),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyInstallationEpoch(ctx, port, firstInput, installedAt.Add(6*time.Second)); !errors.Is(err, ErrConflict) {
		t.Fatalf("non-dormant replay error=%v", err)
	}
	if _, err := database.ExecContext(ctx, `
UPDATE adapter_import_states
SET last_import_started_at = NULL
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3`, binding.SourceSystemID, binding.AdapterManifestID, binding.CursorKey); err != nil {
		t.Fatal(err)
	}

	transitionStore, err := NewStore(database)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := transitionStore.Prepare(ctx, PrepareInput{
		Binding:            binding,
		WorkerUnit:         CompletedExecutionWorkerUnit,
		NextSourceInstance: "legacy.source.after.installation.a",
		NextCursor:         "legacy-cursor-after-installation-a",
		NextHighWatermark:  "legacy-cursor-after-installation-a",
		AuthorizationRef:   "decision://legacy-after-installation-a",
	}, strings.Repeat("a", 32), installedAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("prepare historical transition: %v", err)
	}
	if _, err := transitionStore.Activate(
		ctx,
		prepared.Operation.OperationID,
		prepared.Operation.Plan.PlanSHA256,
		installedAt.Add(2*time.Minute),
	); err != nil {
		t.Fatalf("activate historical transition: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
DELETE FROM adapter_import_states
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3`, binding.SourceSystemID, binding.AdapterManifestID, binding.CursorKey); err != nil {
		t.Fatalf("remove test-only active state: %v", err)
	}

	for _, conflict := range []InstallationEpochInput{
		postgresInstallationEpochInput(binding, TestInstallationA, "new.source.for.reused.installation"),
		postgresInstallationEpochInput(binding, "xci_CCCCCCCCCCCCCCCCCCCCCC", firstInput.SourceInstance),
		postgresInstallationEpochInput(binding, "xci_DDDDDDDDDDDDDDDDDDDDDD", strings.ToLower(firstInput.SourceInstance)),
	} {
		if _, err := ApplyInstallationEpoch(ctx, port, conflict, installedAt.Add(3*time.Minute)); !errors.Is(err, ErrConflict) {
			t.Fatalf("identity conflict input=%+v error=%v", conflict, err)
		}
	}

	boundaryBeforeSecond, err := port.InspectInstallationEpochBoundary(ctx, binding)
	if err != nil {
		t.Fatal(err)
	}
	boundaryBeforeSecondSHA256, _, _, err := TestValidateInstallationEpochBoundary(boundaryBeforeSecond, binding)
	if err != nil {
		t.Fatal(err)
	}
	secondInput := postgresInstallationEpochInput(binding, TestInstallationB, "source.installation.b")
	second, err := ApplyInstallationEpoch(ctx, port, secondInput, installedAt.Add(4*time.Minute))
	if err != nil {
		t.Fatalf("historical max+1 installation: %v", err)
	}
	if second.Epoch != 3 || second.Replayed {
		t.Fatalf("second authority=%+v", second)
	}
	assertInstallationEpochDatabaseState(t, ctx, database, binding, 3, secondInput, 1, 1)

	staleInput := postgresInstallationEpochInput(binding, "xci_EEEEEEEEEEEEEEEEEEEEEE", "source.installation.stale")
	staleCommit := installationEpochCommitForTest(t, staleInput, boundaryBeforeSecondSHA256, 3)
	if _, err := port.CommitInstallationEpoch(ctx, staleCommit, installedAt.Add(5*time.Minute)); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale CAS error=%v", err)
	}
	assertInstallationEpochDatabaseState(t, ctx, database, binding, 3, secondInput, 1, 1)

	if _, err := database.ExecContext(ctx, `
UPDATE adapter_manifests SET active = false WHERE id = $1::uuid`, binding.AdapterManifestID); err != nil {
		t.Fatal(err)
	}
	if _, err := port.InspectInstallationEpochBoundary(ctx, binding); !errors.Is(err, ErrNotReady) {
		t.Fatalf("inactive catalog error=%v", err)
	}
}

func insertInstallationEpochCatalog(t *testing.T, ctx context.Context, database *sql.DB) Binding {
	t.Helper()
	sourceID := epochTestUUID(t)
	adapterID := epochTestUUID(t)
	if _, err := database.ExecContext(ctx, `
INSERT INTO source_systems (id, source_key, source_type, display_name, status)
VALUES ($1::uuid, $2, 'unirouter', 'Installation epoch PostgreSQL test', 'active')`,
		sourceID, "installation-epoch-source-"+sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO adapter_manifests (
  id, adapter_id, adapter_version, source_type, fidelity_level,
  payload_behavior, idempotency_strategy, active
) VALUES ($1::uuid, $2, '1.0.0', 'unirouter', 'F2', 'metadata_only_default', 'source_ref', true)`,
		adapterID, "installation-epoch-adapter-"+adapterID); err != nil {
		t.Fatal(err)
	}
	return Binding{
		SourceSystemID:    sourceID,
		AdapterManifestID: adapterID,
		CursorKey:         CompletedExecutionCursorKey,
	}
}

func postgresInstallationEpochInput(
	binding Binding,
	installationID string,
	sourceInstance string,
) InstallationEpochInput {
	boundary := "cursor-for-" + sourceInstance
	return InstallationEpochInput{
		Binding:        binding,
		InstallationID: installationID,
		WorkerUnit:     CompletedExecutionWorkerUnit,
		SourceInstance: sourceInstance,
		Cursor:         boundary,
		HighWatermark:  boundary,
	}
}

func installationEpochCommitForTest(
	t *testing.T,
	input InstallationEpochInput,
	boundarySHA256 string,
	epoch int64,
) InstallationEpochCommit {
	t.Helper()
	_, prepare, err := TestInstallationPrepareInput(input)
	if err != nil {
		t.Fatal(err)
	}
	requestSHA256, err := TestInstallationPrepareRequestSHA256(prepare)
	if err != nil {
		t.Fatal(err)
	}
	return InstallationEpochCommit{
		SchemaVersion:          InstallationEpochCommitSchema,
		ExpectedBoundarySHA256: boundarySHA256,
		InstallationID:         input.InstallationID,
		RequestSHA256:          requestSHA256,
		PrepareInput:           prepare,
		Epoch:                  epoch,
	}
}

func assertInstallationEpochDatabaseState(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	binding Binding,
	wantEpoch int64,
	wantInput InstallationEpochInput,
	wantOperations int64,
	wantHistory int64,
) {
	t.Helper()
	var epoch, pages, received, committed, duplicate, failures int64
	var sourceInstance, cursor, highWatermark, workerStatus, legacyStatus, operationID string
	var startedAt, completedAt, failedAt sql.NullTime
	if err := database.QueryRowContext(ctx, `
SELECT
  checkpoint_epoch,
  COALESCE(checkpoint_epoch_operation_id, ''),
  activation_source_instance,
  cursor_value,
  high_watermark,
  worker_status,
  status,
  last_import_started_at,
  last_import_completed_at,
  last_poll_failed_at,
  pages_committed,
  items_received,
  items_committed,
  duplicate_noop,
  consecutive_failures
FROM adapter_import_states
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3`, binding.SourceSystemID, binding.AdapterManifestID, binding.CursorKey).Scan(
		&epoch,
		&operationID,
		&sourceInstance,
		&cursor,
		&highWatermark,
		&workerStatus,
		&legacyStatus,
		&startedAt,
		&completedAt,
		&failedAt,
		&pages,
		&received,
		&committed,
		&duplicate,
		&failures,
	); err != nil {
		t.Fatal(err)
	}
	if epoch != wantEpoch || operationID != "" || sourceInstance != wantInput.SourceInstance ||
		cursor != wantInput.Cursor || highWatermark != wantInput.HighWatermark ||
		workerStatus != "inactive" || legacyStatus != "idle" || startedAt.Valid || completedAt.Valid || failedAt.Valid ||
		pages != 0 || received != 0 || committed != 0 || duplicate != 0 || failures != 0 {
		t.Fatalf(
			"state epoch=%d operation=%q source=%q cursor=%q hwm=%q worker=%q status=%q timestamps=%v/%v/%v counters=%d/%d/%d/%d/%d",
			epoch, operationID, sourceInstance, cursor, highWatermark, workerStatus, legacyStatus,
			startedAt.Valid, completedAt.Valid, failedAt.Valid, pages, received, committed, duplicate, failures,
		)
	}
	var operations, history int64
	if err := database.QueryRowContext(ctx, `
SELECT
  (SELECT count(*) FROM connector_checkpoint_epoch_operations
   WHERE source_system_id = $1::uuid AND adapter_manifest_id = $2::uuid AND cursor_key = $3),
  (SELECT count(*) FROM connector_checkpoint_epoch_history
   WHERE source_system_id = $1::uuid AND adapter_manifest_id = $2::uuid AND cursor_key = $3)`,
		binding.SourceSystemID, binding.AdapterManifestID, binding.CursorKey).Scan(&operations, &history); err != nil {
		t.Fatal(err)
	}
	if operations != wantOperations || history != wantHistory {
		t.Fatalf("operations=%d history=%d want=%d/%d", operations, history, wantOperations, wantHistory)
	}
}
