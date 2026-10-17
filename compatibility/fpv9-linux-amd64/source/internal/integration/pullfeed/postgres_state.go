package pullfeed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"
)

const CompletedExecutionCursorKey = "unirouter.completed-execution.v1"

type WorkerStatus string

const (
	WorkerStatusInactive WorkerStatus = "inactive"
	WorkerStatusIdle     WorkerStatus = "idle"
	WorkerStatusPolling  WorkerStatus = "polling"
	WorkerStatusBackoff  WorkerStatus = "backoff"
	WorkerStatusStopping WorkerStatus = "stopping"
	WorkerStatusError    WorkerStatus = "error"
)

var (
	ErrWorkerStateNotFound           = errors.New("completed-execution worker state was not found")
	ErrWorkerStateIdentityMismatch   = errors.New("completed-execution worker state identity does not match an active source and adapter")
	ErrWorkerStateNotActivated       = errors.New("completed-execution worker state is not activated")
	ErrWorkerStateActivationConflict = errors.New("completed-execution worker activation conflicts with durable state")
	ErrWorkerStateConflict           = errors.New("completed-execution worker state changed concurrently")

	canonicalUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	failureCodePattern   = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,127}$`)
)

type stateQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type DurableActivation struct {
	SourceInstance   string
	Cursor           string
	HighWatermark    string
	ActivatedAt      time.Time
	AuthorizationRef string
}

type PageCommit struct {
	NextCursor     string
	HighWatermark  string
	CompletedAt    time.Time
	ItemsReceived  int64
	ItemsCommitted int64
	DuplicateNoop  int64
}

type WorkerFailure struct {
	OccurredAt time.Time
	Code       string
	Retryable  bool
}

type PostgresWorkerState struct {
	ID                      string
	SourceSystemID          string
	AdapterManifestID       string
	CursorKey               string
	CheckpointEpoch         int64
	CheckpointOperationID   string
	SourceInstance          string
	ActivationCursor        string
	ActivationHighWatermark string
	ActivatedAt             time.Time
	AuthorizationRef        string
	Cursor                  string
	HighWatermark           string
	Status                  WorkerStatus
	LastPollStartedAt       *time.Time
	LastPollSucceededAt     *time.Time
	LastPollFailedAt        *time.Time
	LastErrorCode           string
	PagesCommitted          int64
	ItemsReceived           int64
	ItemsCommitted          int64
	DuplicateNoop           int64
	ConsecutiveFailures     int64
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

// PostgresStateStore binds one active source/adapter pair to the fixed V1
// completed-execution cursor key. The caller commits every page item through
// EvidenceStore first and calls CommitPage only after all item calls succeed.
// Partial item persistence is intentional: a crash leaves this cursor unchanged
// and the next poll converges through evidence idempotency before advancing it.
type PostgresStateStore struct {
	db                *sql.DB
	sourceSystemID    string
	adapterManifestID string
}

func NewPostgresStateStore(
	db *sql.DB,
	sourceSystemID string,
	adapterManifestID string,
) (*PostgresStateStore, error) {
	if db == nil {
		return nil, fmt.Errorf("state database is required")
	}
	if !canonicalUUIDPattern.MatchString(sourceSystemID) {
		return nil, fmt.Errorf("source_system_id must be a canonical lowercase UUID")
	}
	if !canonicalUUIDPattern.MatchString(adapterManifestID) {
		return nil, fmt.Errorf("adapter_manifest_id must be a canonical lowercase UUID")
	}
	return &PostgresStateStore{
		db:                db,
		sourceSystemID:    sourceSystemID,
		adapterManifestID: adapterManifestID,
	}, nil
}

func (s *PostgresStateStore) Load(ctx context.Context) (PostgresWorkerState, error) {
	if err := s.requireInitialized(); err != nil {
		return PostgresWorkerState{}, err
	}
	return s.load(ctx, s.db)
}

func (s *PostgresStateStore) load(ctx context.Context, queryer stateQueryer) (PostgresWorkerState, error) {
	var state PostgresWorkerState
	var sourceInstance sql.NullString
	var activationCursor sql.NullString
	var activationHighWatermark sql.NullString
	var activatedAt sql.NullTime
	var authorizationRef sql.NullString
	var cursor sql.NullString
	var highWatermark sql.NullString
	var status string
	var lastStarted sql.NullTime
	var lastSucceeded sql.NullTime
	var lastFailed sql.NullTime
	var lastError sql.NullString
	var sourceActive bool
	var adapterActive bool
	var sourceTypeMatches bool
	err := queryer.QueryRowContext(ctx, `
SELECT
  state.id::text,
  state.source_system_id::text,
  state.adapter_manifest_id::text,
  state.cursor_key,
  state.checkpoint_epoch,
  COALESCE(state.checkpoint_epoch_operation_id, ''),
  state.activation_source_instance,
  state.activation_cursor,
  state.activation_high_watermark,
  state.activated_at,
  state.authorization_ref,
  state.cursor_value,
  state.high_watermark,
  state.worker_status,
  state.last_import_started_at,
  state.last_import_completed_at,
  state.last_poll_failed_at,
  state.last_error,
  state.pages_committed,
  state.items_received,
  state.items_committed,
  state.duplicate_noop,
  state.consecutive_failures::bigint,
  state.created_at,
  state.updated_at,
  source.status = 'active',
  adapter.active,
  source.source_type = adapter.source_type
FROM adapter_import_states AS state
JOIN source_systems AS source ON source.id = state.source_system_id
JOIN adapter_manifests AS adapter ON adapter.id = state.adapter_manifest_id
WHERE state.source_system_id = $1::uuid
  AND state.adapter_manifest_id = $2::uuid
  AND state.cursor_key = $3`,
		s.sourceSystemID,
		s.adapterManifestID,
		CompletedExecutionCursorKey,
	).Scan(
		&state.ID,
		&state.SourceSystemID,
		&state.AdapterManifestID,
		&state.CursorKey,
		&state.CheckpointEpoch,
		&state.CheckpointOperationID,
		&sourceInstance,
		&activationCursor,
		&activationHighWatermark,
		&activatedAt,
		&authorizationRef,
		&cursor,
		&highWatermark,
		&status,
		&lastStarted,
		&lastSucceeded,
		&lastFailed,
		&lastError,
		&state.PagesCommitted,
		&state.ItemsReceived,
		&state.ItemsCommitted,
		&state.DuplicateNoop,
		&state.ConsecutiveFailures,
		&state.CreatedAt,
		&state.UpdatedAt,
		&sourceActive,
		&adapterActive,
		&sourceTypeMatches,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return PostgresWorkerState{}, ErrWorkerStateNotFound
	}
	if err != nil {
		return PostgresWorkerState{}, fmt.Errorf("load completed-execution worker state: %w", err)
	}
	if state.SourceSystemID != s.sourceSystemID ||
		state.AdapterManifestID != s.adapterManifestID ||
		state.CursorKey != CompletedExecutionCursorKey ||
		state.CheckpointEpoch < 1 ||
		!sourceActive ||
		!adapterActive ||
		!sourceTypeMatches {
		return PostgresWorkerState{}, ErrWorkerStateIdentityMismatch
	}
	if !sourceInstance.Valid ||
		!activationCursor.Valid ||
		!activationHighWatermark.Valid ||
		!activatedAt.Valid ||
		!authorizationRef.Valid ||
		!cursor.Valid ||
		!highWatermark.Valid {
		return PostgresWorkerState{}, ErrWorkerStateNotActivated
	}

	state.SourceInstance = sourceInstance.String
	state.ActivationCursor = activationCursor.String
	state.ActivationHighWatermark = activationHighWatermark.String
	state.ActivatedAt = activatedAt.Time.UTC()
	state.AuthorizationRef = authorizationRef.String
	state.Cursor = cursor.String
	state.HighWatermark = highWatermark.String
	state.Status = WorkerStatus(status)
	state.LastPollStartedAt = nullableTime(lastStarted)
	state.LastPollSucceededAt = nullableTime(lastSucceeded)
	state.LastPollFailedAt = nullableTime(lastFailed)
	if lastError.Valid {
		state.LastErrorCode = lastError.String
	}
	if err := validateLoadedWorkerState(state); err != nil {
		return PostgresWorkerState{}, err
	}
	return state, nil
}

func (s *PostgresStateStore) Activate(
	ctx context.Context,
	activation DurableActivation,
) (PostgresWorkerState, error) {
	if err := s.requireInitialized(); err != nil {
		return PostgresWorkerState{}, err
	}
	activation = normalizeActivation(activation)
	if err := validateDurableActivation(activation); err != nil {
		return PostgresWorkerState{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PostgresWorkerState{}, fmt.Errorf("begin completed-execution worker activation: %w", err)
	}
	defer tx.Rollback()
	if err := s.requireActiveIdentity(ctx, tx); err != nil {
		return PostgresWorkerState{}, err
	}

	result, err := tx.ExecContext(ctx, `
INSERT INTO adapter_import_states (
  source_system_id,
  adapter_manifest_id,
  cursor_key,
  cursor_value,
  activation_source_instance,
  activation_cursor,
  activation_high_watermark,
  activated_at,
  authorization_ref,
  high_watermark,
  status,
  worker_status,
  metadata
) VALUES (
  $1::uuid,
  $2::uuid,
  $3,
  $4,
  $5,
  $4,
  $6,
  $7,
  $8,
  $6,
  'idle',
  'idle',
  jsonb_build_object('contract', $3::text, 'state_version', 1)
)
ON CONFLICT (source_system_id, adapter_manifest_id, cursor_key) DO NOTHING`,
		s.sourceSystemID,
		s.adapterManifestID,
		CompletedExecutionCursorKey,
		activation.Cursor,
		activation.SourceInstance,
		activation.HighWatermark,
		activation.ActivatedAt,
		activation.AuthorizationRef,
	)
	if err != nil {
		return PostgresWorkerState{}, fmt.Errorf("activate completed-execution worker state: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return PostgresWorkerState{}, fmt.Errorf("inspect completed-execution worker activation: %w", err)
	}
	if inserted != 0 && inserted != 1 {
		return PostgresWorkerState{}, fmt.Errorf("activate completed-execution worker state affected %d rows", inserted)
	}

	state, err := s.load(ctx, tx)
	if err != nil {
		return PostgresWorkerState{}, err
	}
	if inserted == 0 && !sameActivation(state, activation) {
		return PostgresWorkerState{}, ErrWorkerStateActivationConflict
	}
	if err := tx.Commit(); err != nil {
		return PostgresWorkerState{}, fmt.Errorf("commit completed-execution worker activation: %w", err)
	}
	return state, nil
}

func (s *PostgresStateStore) MarkPollStarted(
	ctx context.Context,
	expectedCursor string,
	startedAt time.Time,
) error {
	if err := validateStateWrite(s, expectedCursor, startedAt); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE adapter_import_states AS state
SET
  status = 'running',
  worker_status = 'polling',
  last_import_started_at = $4,
  updated_at = $4
WHERE state.source_system_id = $1::uuid
  AND state.adapter_manifest_id = $2::uuid
  AND state.cursor_key = $3
  AND state.cursor_value = $5
  AND state.worker_status IN ('inactive', 'idle', 'polling', 'backoff', 'error')
  AND NOT EXISTS (
    SELECT 1
    FROM connector_checkpoint_epoch_operations AS epoch_operation
    WHERE epoch_operation.source_system_id = state.source_system_id
      AND epoch_operation.adapter_manifest_id = state.adapter_manifest_id
      AND epoch_operation.cursor_key = state.cursor_key
      AND epoch_operation.status = 'prepared'
  )
  AND EXISTS (
    SELECT 1
    FROM source_systems AS source
    JOIN adapter_manifests AS adapter ON adapter.id = state.adapter_manifest_id
    WHERE source.id = state.source_system_id
      AND source.status = 'active'
      AND adapter.active
      AND source.source_type = adapter.source_type
  )`,
		s.sourceSystemID,
		s.adapterManifestID,
		CompletedExecutionCursorKey,
		startedAt.UTC().Truncate(time.Microsecond),
		expectedCursor,
	)
	if err != nil {
		return fmt.Errorf("mark completed-execution poll started: %w", err)
	}
	return requireSingleStateRow(result)
}

// CommitPage must be called only after every item in the page has independently
// converged through EvidenceStore. Its state update is a separate atomic
// transaction; until this update succeeds, a crash or failure leaves the old
// cursor available for at-least-once replay. The expected-cursor predicate also
// prevents a stale worker from advancing durable state.
func (s *PostgresStateStore) CommitPage(
	ctx context.Context,
	expectedCursor string,
	page PageCommit,
) error {
	if err := validateStateWrite(s, expectedCursor, page.CompletedAt); err != nil {
		return err
	}
	page.CompletedAt = page.CompletedAt.UTC().Truncate(time.Microsecond)
	if err := validatePageCommit(page); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `
UPDATE adapter_import_states AS state
SET
  cursor_value = $4,
  high_watermark = $5,
  status = 'idle',
  worker_status = 'idle',
  last_import_completed_at = $6,
  last_error = NULL,
  pages_committed = pages_committed + 1,
  items_received = items_received + $7,
  items_committed = items_committed + $8,
  duplicate_noop = duplicate_noop + $9,
  consecutive_failures = 0,
  updated_at = $6
WHERE state.source_system_id = $1::uuid
  AND state.adapter_manifest_id = $2::uuid
  AND state.cursor_key = $3
  AND state.cursor_value = $10
  AND state.worker_status = 'polling'
  AND EXISTS (
    SELECT 1
    FROM source_systems AS source
    JOIN adapter_manifests AS adapter ON adapter.id = state.adapter_manifest_id
    WHERE source.id = state.source_system_id
      AND source.status = 'active'
      AND adapter.active
      AND source.source_type = adapter.source_type
  )`,
		s.sourceSystemID,
		s.adapterManifestID,
		CompletedExecutionCursorKey,
		page.NextCursor,
		page.HighWatermark,
		page.CompletedAt,
		page.ItemsReceived,
		page.ItemsCommitted,
		page.DuplicateNoop,
		expectedCursor,
	)
	if err != nil {
		return fmt.Errorf("commit completed-execution page state: %w", err)
	}
	return requireSingleStateRow(result)
}

func (s *PostgresStateStore) MarkFailure(
	ctx context.Context,
	expectedCursor string,
	failure WorkerFailure,
) error {
	if err := validateStateWrite(s, expectedCursor, failure.OccurredAt); err != nil {
		return err
	}
	if !failureCodePattern.MatchString(failure.Code) {
		return fmt.Errorf("worker failure code must match %s", failureCodePattern)
	}
	status := WorkerStatusError
	if failure.Retryable {
		status = WorkerStatusBackoff
	}
	occurredAt := failure.OccurredAt.UTC().Truncate(time.Microsecond)
	result, err := s.db.ExecContext(ctx, `
UPDATE adapter_import_states AS state
SET
  status = 'error',
  worker_status = $4,
  last_poll_failed_at = $5,
  last_error = $6,
  consecutive_failures = consecutive_failures + 1,
  updated_at = $5
WHERE state.source_system_id = $1::uuid
  AND state.adapter_manifest_id = $2::uuid
  AND state.cursor_key = $3
  AND state.cursor_value = $7
  AND state.worker_status = 'polling'
  AND EXISTS (
    SELECT 1
    FROM source_systems AS source
    JOIN adapter_manifests AS adapter ON adapter.id = state.adapter_manifest_id
    WHERE source.id = state.source_system_id
      AND source.status = 'active'
      AND adapter.active
      AND source.source_type = adapter.source_type
  )`,
		s.sourceSystemID,
		s.adapterManifestID,
		CompletedExecutionCursorKey,
		status,
		occurredAt,
		failure.Code,
		expectedCursor,
	)
	if err != nil {
		return fmt.Errorf("mark completed-execution poll failure: %w", err)
	}
	return requireSingleStateRow(result)
}

func (s *PostgresStateStore) MarkStopping(
	ctx context.Context,
	expectedCursor string,
	occurredAt time.Time,
) error {
	return s.transitionLifecycle(ctx, expectedCursor, occurredAt, WorkerStatusStopping, "running")
}

func (s *PostgresStateStore) MarkInactive(
	ctx context.Context,
	expectedCursor string,
	occurredAt time.Time,
) error {
	return s.transitionLifecycle(ctx, expectedCursor, occurredAt, WorkerStatusInactive, "idle")
}

func (s *PostgresStateStore) transitionLifecycle(
	ctx context.Context,
	expectedCursor string,
	occurredAt time.Time,
	workerStatus WorkerStatus,
	legacyStatus string,
) error {
	if err := validateStateWrite(s, expectedCursor, occurredAt); err != nil {
		return err
	}
	occurredAt = occurredAt.UTC().Truncate(time.Microsecond)
	result, err := s.db.ExecContext(ctx, `
UPDATE adapter_import_states AS state
SET
  status = $4,
  worker_status = $5,
  updated_at = $6
WHERE state.source_system_id = $1::uuid
  AND state.adapter_manifest_id = $2::uuid
  AND state.cursor_key = $3
  AND state.cursor_value = $7
  AND EXISTS (
    SELECT 1
    FROM source_systems AS source
    JOIN adapter_manifests AS adapter ON adapter.id = state.adapter_manifest_id
    WHERE source.id = state.source_system_id
      AND source.status = 'active'
      AND adapter.active
      AND source.source_type = adapter.source_type
  )`,
		s.sourceSystemID,
		s.adapterManifestID,
		CompletedExecutionCursorKey,
		legacyStatus,
		workerStatus,
		occurredAt,
		expectedCursor,
	)
	if err != nil {
		return fmt.Errorf("transition completed-execution worker lifecycle: %w", err)
	}
	return requireSingleStateRow(result)
}

func (s *PostgresStateStore) requireActiveIdentity(ctx context.Context, tx *sql.Tx) error {
	var sourceActive bool
	var adapterActive bool
	var sourceTypeMatches bool
	err := tx.QueryRowContext(ctx, `
SELECT
  source.status = 'active',
  adapter.active,
  source.source_type = adapter.source_type
FROM source_systems AS source
CROSS JOIN adapter_manifests AS adapter
WHERE source.id = $1::uuid
  AND adapter.id = $2::uuid`,
		s.sourceSystemID,
		s.adapterManifestID,
	).Scan(&sourceActive, &adapterActive, &sourceTypeMatches)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrWorkerStateIdentityMismatch
	}
	if err != nil {
		return fmt.Errorf("validate completed-execution source and adapter identity: %w", err)
	}
	if !sourceActive || !adapterActive || !sourceTypeMatches {
		return ErrWorkerStateIdentityMismatch
	}
	return nil
}

func validateStateWrite(
	store *PostgresStateStore,
	expectedCursor string,
	occurredAt time.Time,
) error {
	if err := store.requireInitialized(); err != nil {
		return err
	}
	if err := validateBoundedText("expected cursor", expectedCursor, 1, MaxCursorBytes); err != nil {
		return err
	}
	if occurredAt.IsZero() {
		return fmt.Errorf("worker state transition time is required")
	}
	return nil
}

func validateDurableActivation(activation DurableActivation) error {
	if err := validateBoundedText("activation source instance", activation.SourceInstance, 1, 128); err != nil {
		return err
	}
	if err := validateBoundedText("activation cursor", activation.Cursor, 1, MaxCursorBytes); err != nil {
		return err
	}
	if activation.HighWatermark != "" {
		if err := validateBoundedText("activation high watermark", activation.HighWatermark, 1, MaxCursorBytes); err != nil {
			return err
		}
	}
	if activation.ActivatedAt.IsZero() {
		return fmt.Errorf("activation time is required")
	}
	if err := validateBoundedText("activation authorization ref", activation.AuthorizationRef, 1, MaxAuthorizationRefBytes); err != nil {
		return err
	}
	return nil
}

func validatePageCommit(page PageCommit) error {
	if err := validateBoundedText("next cursor", page.NextCursor, 1, MaxCursorBytes); err != nil {
		return err
	}
	if page.HighWatermark != "" {
		if err := validateBoundedText("high watermark", page.HighWatermark, 1, MaxCursorBytes); err != nil {
			return err
		}
	}
	if page.CompletedAt.IsZero() {
		return fmt.Errorf("page completion time is required")
	}
	if page.ItemsReceived < 0 || page.ItemsCommitted < 0 || page.DuplicateNoop < 0 {
		return fmt.Errorf("page counters must not be negative")
	}
	if page.ItemsCommitted+page.DuplicateNoop != page.ItemsReceived {
		return fmt.Errorf("every received item must be committed or a duplicate before cursor advance")
	}
	return nil
}

func validateLoadedWorkerState(state PostgresWorkerState) error {
	if err := validateDurableActivation(DurableActivation{
		SourceInstance:   state.SourceInstance,
		Cursor:           state.ActivationCursor,
		HighWatermark:    state.ActivationHighWatermark,
		ActivatedAt:      state.ActivatedAt,
		AuthorizationRef: state.AuthorizationRef,
	}); err != nil {
		return fmt.Errorf("%w: %v", ErrWorkerStateNotActivated, err)
	}
	if err := validateBoundedText("durable cursor", state.Cursor, 1, MaxCursorBytes); err != nil {
		return fmt.Errorf("%w: %v", ErrWorkerStateNotActivated, err)
	}
	if state.HighWatermark != "" {
		if err := validateBoundedText("durable high watermark", state.HighWatermark, 1, MaxCursorBytes); err != nil {
			return fmt.Errorf("%w: %v", ErrWorkerStateNotActivated, err)
		}
	}
	switch state.Status {
	case WorkerStatusInactive, WorkerStatusIdle, WorkerStatusPolling, WorkerStatusBackoff, WorkerStatusStopping, WorkerStatusError:
	default:
		return fmt.Errorf("unsupported durable worker status %q", state.Status)
	}
	if state.PagesCommitted < 0 ||
		state.ItemsReceived < 0 ||
		state.ItemsCommitted < 0 ||
		state.DuplicateNoop < 0 ||
		state.ConsecutiveFailures < 0 ||
		state.ItemsCommitted+state.DuplicateNoop != state.ItemsReceived {
		return fmt.Errorf("durable worker counters are invalid")
	}
	if state.LastErrorCode != "" && !failureCodePattern.MatchString(state.LastErrorCode) {
		return fmt.Errorf("durable worker failure code is invalid")
	}
	return nil
}

func normalizeActivation(activation DurableActivation) DurableActivation {
	activation.ActivatedAt = activation.ActivatedAt.UTC().Truncate(time.Microsecond)
	return activation
}

func sameActivation(state PostgresWorkerState, activation DurableActivation) bool {
	return state.SourceInstance == activation.SourceInstance &&
		state.ActivationCursor == activation.Cursor &&
		state.ActivationHighWatermark == activation.HighWatermark &&
		state.ActivatedAt.Equal(activation.ActivatedAt) &&
		state.AuthorizationRef == activation.AuthorizationRef
}

func requireSingleStateRow(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect completed-execution worker state update: %w", err)
	}
	if affected != 1 {
		return ErrWorkerStateConflict
	}
	return nil
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	normalized := value.Time.UTC()
	return &normalized
}

func (s *PostgresStateStore) requireInitialized() error {
	if s == nil {
		return fmt.Errorf("postgres worker state store is nil")
	}
	if s.db == nil {
		return fmt.Errorf("state database is required")
	}
	return nil
}
