package checkpointepoch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct {
	db *sql.DB
}

type activeState struct {
	ID string
	Binding
	Epoch                   int64
	EpochOperationID        string
	SourceInstance          string
	ActivationCursor        string
	ActivationHighWatermark string
	ActivatedAt             time.Time
	AuthorizationRef        string
	Cursor                  string
	HighWatermark           string
	LegacyStatus            string
	WorkerStatus            string
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

type operation struct {
	OperationID string
	Binding
	WorkerUnit                  string
	Status                      string
	ExpectedStateID             string
	ExpectedEpoch               int64
	ExpectedStateSHA256         string
	PreviousSourceInstance      string
	PreviousCursorSHA256        string
	PreviousHighWatermarkSHA256 string
	NextSourceInstance          string
	NextCursor                  string
	NextHighWatermark           string
	AuthorizationRef            string
	PlanSHA256                  string
	PreparedAt                  time.Time
	ActivatedAt                 *time.Time
	AbortedAt                   *time.Time
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, err
	}
	database.SetMaxOpenConns(2)
	database.SetMaxIdleConns(1)
	return &Store{db: database}, nil
}

func NewStore(database *sql.DB) (*Store, error) {
	if database == nil {
		return nil, fmt.Errorf("checkpoint epoch database is required")
	}
	return &Store{db: database}, nil
}

func (store *Store) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}

func (store *Store) Prepare(
	ctx context.Context,
	input PrepareInput,
	operationID string,
	preparedAt time.Time,
) (Report, error) {
	if err := store.requireReady(); err != nil {
		return Report{}, err
	}
	if err := validatePrepareInput(input); err != nil {
		return Report{}, err
	}
	if !operationPattern.MatchString(operationID) {
		return Report{}, fmt.Errorf("%w: operation_id", ErrInvalid)
	}
	if preparedAt.IsZero() {
		return Report{}, fmt.Errorf("%w: prepared_at", ErrInvalid)
	}
	preparedAt = normalizedTime(preparedAt)
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Report{}, fmt.Errorf("begin checkpoint epoch prepare: %w", err)
	}
	defer tx.Rollback()

	state, err := loadActiveState(ctx, tx, input.Binding, true)
	if err != nil {
		return Report{}, err
	}
	if err := validateTransitionReady(state); err != nil {
		return Report{}, err
	}
	if state.SourceInstance == input.NextSourceInstance {
		return Report{}, fmt.Errorf("%w: next source_instance must change", ErrInvalid)
	}
	stateSHA256, err := hashActiveState(state)
	if err != nil {
		return Report{}, err
	}
	plan := Plan{
		SchemaVersion:               PlanSchema,
		OperationID:                 operationID,
		Binding:                     input.Binding,
		WorkerUnit:                  input.WorkerUnit,
		ExpectedActiveStateSHA256:   stateSHA256,
		PreviousEpoch:               state.Epoch,
		PreviousSourceInstance:      state.SourceInstance,
		PreviousCursorSHA256:        hashText(state.Cursor),
		PreviousHighWatermarkSHA256: hashText(state.HighWatermark),
		NextEpoch:                   state.Epoch + 1,
		NextSourceInstance:          input.NextSourceInstance,
		NextCursorSHA256:            hashText(input.NextCursor),
		NextHighWatermarkSHA256:     hashText(input.NextHighWatermark),
		AuthorizationRef:            input.AuthorizationRef,
		PreparedAt:                  preparedAt,
	}
	plan.PlanSHA256, err = hashPlan(plan)
	if err != nil {
		return Report{}, err
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO connector_checkpoint_epoch_operations (
  operation_id,
  source_system_id,
  adapter_manifest_id,
  cursor_key,
  worker_unit,
  status,
  expected_state_id,
  expected_epoch,
  expected_state_sha256,
  previous_source_instance,
  previous_cursor_sha256,
  previous_high_watermark_sha256,
  next_source_instance,
  next_cursor,
  next_high_watermark,
  next_authorization_ref,
  plan_sha256,
  prepared_at
) VALUES (
  $1, $2::uuid, $3::uuid, $4, $5, 'prepared', $6::uuid, $7, $8,
  $9, $10, $11, $12, $13, $14, $15, $16, $17
)`,
		operationID,
		input.SourceSystemID,
		input.AdapterManifestID,
		input.CursorKey,
		input.WorkerUnit,
		state.ID,
		state.Epoch,
		stateSHA256,
		state.SourceInstance,
		plan.PreviousCursorSHA256,
		plan.PreviousHighWatermarkSHA256,
		input.NextSourceInstance,
		input.NextCursor,
		input.NextHighWatermark,
		input.AuthorizationRef,
		plan.PlanSHA256,
		preparedAt,
	)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" {
			return Report{}, fmt.Errorf("%w: an operation identity or prepared binding already exists", ErrConflict)
		}
		return Report{}, fmt.Errorf("persist checkpoint epoch plan: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Report{}, fmt.Errorf("commit checkpoint epoch prepare: %w", err)
	}
	return reportFrom(state, operationFromPlan(plan, input), preparedAt)
}

func (store *Store) Activate(
	ctx context.Context,
	operationID string,
	planSHA256 string,
	activatedAt time.Time,
) (Report, error) {
	if err := store.requireReady(); err != nil {
		return Report{}, err
	}
	if err := ValidateOperationBinding(operationID, planSHA256); err != nil {
		return Report{}, err
	}
	if activatedAt.IsZero() {
		return Report{}, fmt.Errorf("%w: activated_at", ErrInvalid)
	}
	activatedAt = normalizedTime(activatedAt)
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Report{}, fmt.Errorf("begin checkpoint epoch activation: %w", err)
	}
	defer tx.Rollback()

	op, err := loadOperation(ctx, tx, operationID, true)
	if err != nil {
		return Report{}, err
	}
	if op.PlanSHA256 != planSHA256 {
		return Report{}, fmt.Errorf("%w: plan digest mismatch", ErrConflict)
	}
	if op.Status == StatusAborted {
		return Report{}, fmt.Errorf("%w: operation was aborted", ErrConflict)
	}
	state, err := loadActiveState(ctx, tx, op.Binding, true)
	if err != nil {
		return Report{}, err
	}
	if op.Status == StatusActivated {
		if state.EpochOperationID != op.OperationID ||
			state.Epoch != op.ExpectedEpoch+1 ||
			state.SourceInstance != op.NextSourceInstance ||
			state.Cursor != op.NextCursor ||
			state.HighWatermark != op.NextHighWatermark {
			return Report{}, fmt.Errorf("%w: activated operation no longer matches active state", ErrConflict)
		}
		if err := tx.Commit(); err != nil {
			return Report{}, err
		}
		return reportFrom(state, op, activatedAt)
	}
	if err := validateTransitionReady(state); err != nil {
		return Report{}, err
	}
	stateSHA256, err := hashActiveState(state)
	if err != nil {
		return Report{}, err
	}
	if state.ID != op.ExpectedStateID || state.Epoch != op.ExpectedEpoch ||
		stateSHA256 != op.ExpectedStateSHA256 {
		return Report{}, fmt.Errorf("%w: active state no longer matches prepared plan", ErrConflict)
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO connector_checkpoint_epoch_history (
  operation_id,
  state_id,
  source_system_id,
  adapter_manifest_id,
  cursor_key,
  checkpoint_epoch,
  source_instance,
  activation_cursor,
  activation_high_watermark,
  activated_at,
  authorization_ref,
  final_cursor,
  final_high_watermark,
  worker_status,
  last_import_started_at,
  last_import_completed_at,
  last_poll_failed_at,
  last_error,
  pages_committed,
  items_received,
  items_committed,
  duplicate_noop,
  consecutive_failures,
  state_created_at,
  state_updated_at,
  retired_at,
  snapshot_sha256
) VALUES (
  $1, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7, $8, $9, $10, $11,
  $12, $13, $14, $15, $16, $17, NULLIF($18, ''), $19, $20, $21, $22,
  $23, $24, $25, $26, $27
)`,
		op.OperationID,
		state.ID,
		state.SourceSystemID,
		state.AdapterManifestID,
		state.CursorKey,
		state.Epoch,
		state.SourceInstance,
		state.ActivationCursor,
		state.ActivationHighWatermark,
		state.ActivatedAt,
		state.AuthorizationRef,
		state.Cursor,
		state.HighWatermark,
		state.WorkerStatus,
		state.LastPollStartedAt,
		state.LastPollSucceededAt,
		state.LastPollFailedAt,
		state.LastErrorCode,
		state.PagesCommitted,
		state.ItemsReceived,
		state.ItemsCommitted,
		state.DuplicateNoop,
		state.ConsecutiveFailures,
		state.CreatedAt,
		state.UpdatedAt,
		activatedAt,
		stateSHA256,
	)
	if err != nil {
		return Report{}, fmt.Errorf("archive completed checkpoint epoch: %w", err)
	}

	result, err := tx.ExecContext(ctx, `
UPDATE adapter_import_states
SET
  cursor_value = $5,
  activation_source_instance = $6,
  activation_cursor = $5,
  activation_high_watermark = $7,
  activated_at = $8,
  authorization_ref = $9,
  high_watermark = $7,
  status = 'idle',
  worker_status = 'inactive',
  last_import_started_at = NULL,
  last_import_completed_at = NULL,
  last_poll_failed_at = NULL,
  last_error = NULL,
  pages_committed = 0,
  items_received = 0,
  items_committed = 0,
  duplicate_noop = 0,
  consecutive_failures = 0,
  checkpoint_epoch = checkpoint_epoch + 1,
  checkpoint_epoch_operation_id = $10,
  metadata = metadata || jsonb_build_object(
    'checkpoint_epoch', checkpoint_epoch + 1,
    'checkpoint_epoch_operation_id', $10::text
  ),
  updated_at = $8
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3
  AND id = $4::uuid
  AND checkpoint_epoch = $11
  AND worker_status = 'inactive'
  AND status = 'idle'
  AND last_error IS NULL
  AND consecutive_failures = 0
  AND cursor_value = high_watermark`,
		op.SourceSystemID,
		op.AdapterManifestID,
		op.CursorKey,
		state.ID,
		op.NextCursor,
		op.NextSourceInstance,
		op.NextHighWatermark,
		activatedAt,
		op.AuthorizationRef,
		op.OperationID,
		op.ExpectedEpoch,
	)
	if err != nil {
		return Report{}, fmt.Errorf("activate new checkpoint epoch: %w", err)
	}
	if err := requireOneRow(result); err != nil {
		return Report{}, err
	}
	result, err = tx.ExecContext(ctx, `
UPDATE connector_checkpoint_epoch_operations
SET status = 'activated', activated_at = $2
WHERE operation_id = $1 AND status = 'prepared'`, op.OperationID, activatedAt)
	if err != nil {
		return Report{}, fmt.Errorf("complete checkpoint epoch activation: %w", err)
	}
	if err := requireOneRow(result); err != nil {
		return Report{}, err
	}
	op.Status = StatusActivated
	op.ActivatedAt = &activatedAt
	state, err = loadActiveState(ctx, tx, op.Binding, false)
	if err != nil {
		return Report{}, err
	}
	if err := tx.Commit(); err != nil {
		return Report{}, fmt.Errorf("commit checkpoint epoch activation: %w", err)
	}
	return reportFrom(state, op, activatedAt)
}

func (store *Store) Abort(
	ctx context.Context,
	operationID string,
	planSHA256 string,
	abortedAt time.Time,
) (Report, error) {
	if err := store.requireReady(); err != nil {
		return Report{}, err
	}
	if err := ValidateOperationBinding(operationID, planSHA256); err != nil {
		return Report{}, err
	}
	if abortedAt.IsZero() {
		return Report{}, fmt.Errorf("%w: aborted_at", ErrInvalid)
	}
	abortedAt = normalizedTime(abortedAt)
	tx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return Report{}, err
	}
	defer tx.Rollback()
	op, err := loadOperation(ctx, tx, operationID, true)
	if err != nil {
		return Report{}, err
	}
	if op.PlanSHA256 != planSHA256 {
		return Report{}, fmt.Errorf("%w: plan digest mismatch", ErrConflict)
	}
	if op.Status == StatusActivated {
		return Report{}, fmt.Errorf("%w: activated operation cannot be aborted", ErrConflict)
	}
	if op.Status == StatusPrepared {
		result, updateErr := tx.ExecContext(ctx, `
UPDATE connector_checkpoint_epoch_operations
SET status = 'aborted', aborted_at = $2
WHERE operation_id = $1 AND status = 'prepared'`, operationID, abortedAt)
		if updateErr != nil {
			return Report{}, updateErr
		}
		if err := requireOneRow(result); err != nil {
			return Report{}, err
		}
		op.Status = StatusAborted
		op.AbortedAt = &abortedAt
	}
	state, err := loadActiveState(ctx, tx, op.Binding, false)
	if err != nil {
		return Report{}, err
	}
	if err := tx.Commit(); err != nil {
		return Report{}, err
	}
	return reportFrom(state, op, abortedAt)
}

func (store *Store) StatusByBinding(ctx context.Context, binding Binding, now time.Time) (Report, error) {
	if err := store.requireReady(); err != nil {
		return Report{}, err
	}
	if err := ValidateBinding(binding); err != nil {
		return Report{}, err
	}
	state, err := loadActiveState(ctx, store.db, binding, false)
	if err != nil {
		return Report{}, err
	}
	op, err := loadLatestOperation(ctx, store.db, binding)
	if err != nil && !errors.Is(err, ErrOperationMissing) {
		return Report{}, err
	}
	if errors.Is(err, ErrOperationMissing) {
		return reportFrom(state, nil, now)
	}
	return reportFrom(state, op, now)
}

func (store *Store) StatusByOperation(ctx context.Context, operationID string, now time.Time) (Report, error) {
	if err := store.requireReady(); err != nil {
		return Report{}, err
	}
	if !operationPattern.MatchString(operationID) {
		return Report{}, fmt.Errorf("%w: operation_id", ErrInvalid)
	}
	op, err := loadOperation(ctx, store.db, operationID, false)
	if err != nil {
		return Report{}, err
	}
	state, err := loadActiveState(ctx, store.db, op.Binding, false)
	if err != nil {
		return Report{}, err
	}
	return reportFrom(state, op, now)
}

func (store *Store) OperationWorkerUnit(ctx context.Context, operationID string) (string, error) {
	if err := store.requireReady(); err != nil {
		return "", err
	}
	if !operationPattern.MatchString(operationID) {
		return "", fmt.Errorf("%w: operation_id", ErrInvalid)
	}
	op, err := loadOperation(ctx, store.db, operationID, false)
	if err != nil {
		return "", err
	}
	return op.WorkerUnit, nil
}

func (store *Store) requireReady() error {
	if store == nil || store.db == nil {
		return fmt.Errorf("checkpoint epoch store is not initialized")
	}
	return nil
}

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadActiveState(ctx context.Context, queryer rowQueryer, binding Binding, lock bool) (activeState, error) {
	query := `
SELECT
  state.id::text,
  state.source_system_id::text,
  state.adapter_manifest_id::text,
  state.cursor_key,
  state.checkpoint_epoch,
  COALESCE(state.checkpoint_epoch_operation_id, ''),
  COALESCE(state.activation_source_instance, ''),
  COALESCE(state.activation_cursor, ''),
  COALESCE(state.activation_high_watermark, ''),
  state.activated_at,
  COALESCE(state.authorization_ref, ''),
  COALESCE(state.cursor_value, ''),
  COALESCE(state.high_watermark, ''),
  state.status,
  state.worker_status,
  state.last_import_started_at,
  state.last_import_completed_at,
  state.last_poll_failed_at,
  COALESCE(state.last_error, ''),
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
  AND state.cursor_key = $3`
	if lock {
		query += " FOR UPDATE OF state"
	}
	var state activeState
	var activatedAt sql.NullTime
	var startedAt, completedAt, failedAt sql.NullTime
	var sourceActive, adapterActive, sourceTypeMatches bool
	err := queryer.QueryRowContext(ctx, query,
		binding.SourceSystemID,
		binding.AdapterManifestID,
		binding.CursorKey,
	).Scan(
		&state.ID,
		&state.SourceSystemID,
		&state.AdapterManifestID,
		&state.CursorKey,
		&state.Epoch,
		&state.EpochOperationID,
		&state.SourceInstance,
		&state.ActivationCursor,
		&state.ActivationHighWatermark,
		&activatedAt,
		&state.AuthorizationRef,
		&state.Cursor,
		&state.HighWatermark,
		&state.LegacyStatus,
		&state.WorkerStatus,
		&startedAt,
		&completedAt,
		&failedAt,
		&state.LastErrorCode,
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
		return activeState{}, fmt.Errorf("%w: active binding", ErrNotReady)
	}
	if err != nil {
		return activeState{}, fmt.Errorf("load active checkpoint epoch: %w", err)
	}
	if !activatedAt.Valid || !sourceActive || !adapterActive || !sourceTypeMatches {
		return activeState{}, fmt.Errorf("%w: inactive or incomplete binding", ErrNotReady)
	}
	state.ActivatedAt = normalizedTime(activatedAt.Time)
	state.LastPollStartedAt = nullableTime(startedAt)
	state.LastPollSucceededAt = nullableTime(completedAt)
	state.LastPollFailedAt = nullableTime(failedAt)
	state.CreatedAt = normalizedTime(state.CreatedAt)
	state.UpdatedAt = normalizedTime(state.UpdatedAt)
	return state, nil
}

func validateTransitionReady(state activeState) error {
	if state.Epoch < 1 || state.SourceInstance == "" || state.ActivationCursor == "" ||
		state.AuthorizationRef == "" || state.Cursor == "" || state.HighWatermark == "" {
		return fmt.Errorf("%w: active state is incomplete", ErrNotReady)
	}
	if state.WorkerStatus != "inactive" || state.LegacyStatus != "idle" {
		return fmt.Errorf("%w: worker must be inactive and idle", ErrNotReady)
	}
	if state.Cursor != state.HighWatermark {
		return fmt.Errorf("%w: cursor does not equal high-watermark", ErrNotReady)
	}
	if state.LastErrorCode != "" || state.ConsecutiveFailures != 0 {
		return fmt.Errorf("%w: unresolved worker failure", ErrNotReady)
	}
	if state.PagesCommitted < 0 || state.ItemsReceived < 0 || state.ItemsCommitted < 0 ||
		state.DuplicateNoop < 0 || state.ItemsCommitted+state.DuplicateNoop != state.ItemsReceived {
		return fmt.Errorf("%w: delivery counters are inconsistent", ErrNotReady)
	}
	return nil
}

func hashActiveState(state activeState) (string, error) {
	type stateHashInput struct {
		SchemaVersion           string     `json:"schema_version"`
		ID                      string     `json:"id"`
		Binding                 Binding    `json:"binding"`
		Epoch                   int64      `json:"epoch"`
		EpochOperationID        string     `json:"epoch_operation_id"`
		SourceInstance          string     `json:"source_instance"`
		ActivationCursor        string     `json:"activation_cursor"`
		ActivationHighWatermark string     `json:"activation_high_watermark"`
		ActivatedAt             time.Time  `json:"activated_at"`
		AuthorizationRef        string     `json:"authorization_ref"`
		Cursor                  string     `json:"cursor"`
		HighWatermark           string     `json:"high_watermark"`
		LegacyStatus            string     `json:"legacy_status"`
		WorkerStatus            string     `json:"worker_status"`
		LastPollStartedAt       *time.Time `json:"last_poll_started_at"`
		LastPollSucceededAt     *time.Time `json:"last_poll_succeeded_at"`
		LastPollFailedAt        *time.Time `json:"last_poll_failed_at"`
		LastErrorCode           string     `json:"last_error_code"`
		PagesCommitted          int64      `json:"pages_committed"`
		ItemsReceived           int64      `json:"items_received"`
		ItemsCommitted          int64      `json:"items_committed"`
		DuplicateNoop           int64      `json:"duplicate_noop"`
		ConsecutiveFailures     int64      `json:"consecutive_failures"`
		CreatedAt               time.Time  `json:"created_at"`
		UpdatedAt               time.Time  `json:"updated_at"`
	}
	return hashJSON(stateHashInput{
		SchemaVersion:           "xinsight.connector-checkpoint-active-state.v1",
		ID:                      state.ID,
		Binding:                 state.Binding,
		Epoch:                   state.Epoch,
		EpochOperationID:        state.EpochOperationID,
		SourceInstance:          state.SourceInstance,
		ActivationCursor:        state.ActivationCursor,
		ActivationHighWatermark: state.ActivationHighWatermark,
		ActivatedAt:             state.ActivatedAt,
		AuthorizationRef:        state.AuthorizationRef,
		Cursor:                  state.Cursor,
		HighWatermark:           state.HighWatermark,
		LegacyStatus:            state.LegacyStatus,
		WorkerStatus:            state.WorkerStatus,
		LastPollStartedAt:       state.LastPollStartedAt,
		LastPollSucceededAt:     state.LastPollSucceededAt,
		LastPollFailedAt:        state.LastPollFailedAt,
		LastErrorCode:           state.LastErrorCode,
		PagesCommitted:          state.PagesCommitted,
		ItemsReceived:           state.ItemsReceived,
		ItemsCommitted:          state.ItemsCommitted,
		DuplicateNoop:           state.DuplicateNoop,
		ConsecutiveFailures:     state.ConsecutiveFailures,
		CreatedAt:               state.CreatedAt,
		UpdatedAt:               state.UpdatedAt,
	})
}

func hashPlan(plan Plan) (string, error) {
	plan.PlanSHA256 = ""
	return hashJSON(plan)
}

func reportFrom(state activeState, op *operation, now time.Time) (Report, error) {
	stateSHA256, err := hashActiveState(state)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		SchemaVersion: ReportSchema,
		Active: ActiveStateSummary{
			StateID:             state.ID,
			Binding:             state.Binding,
			Epoch:               state.Epoch,
			EpochOperationID:    state.EpochOperationID,
			SourceInstance:      state.SourceInstance,
			CursorSHA256:        hashText(state.Cursor),
			HighWatermarkSHA256: hashText(state.HighWatermark),
			WorkerStatus:        state.WorkerStatus,
			LastErrorCode:       state.LastErrorCode,
			PagesCommitted:      state.PagesCommitted,
			ItemsReceived:       state.ItemsReceived,
			ItemsCommitted:      state.ItemsCommitted,
			DuplicateNoop:       state.DuplicateNoop,
			ConsecutiveFailures: state.ConsecutiveFailures,
			LastPollSucceededAt: state.LastPollSucceededAt,
			StateSHA256:         stateSHA256,
		},
		GeneratedAt: normalizedTime(now),
	}
	if op != nil {
		plan := planFromOperation(*op)
		report.Operation = &OperationSummary{
			OperationID: op.OperationID,
			Status:      op.Status,
			Plan:        plan,
			ActivatedAt: op.ActivatedAt,
			AbortedAt:   op.AbortedAt,
		}
	}
	return report, nil
}

func operationFromPlan(plan Plan, input PrepareInput) *operation {
	return &operation{
		OperationID:                 plan.OperationID,
		Binding:                     plan.Binding,
		WorkerUnit:                  plan.WorkerUnit,
		Status:                      StatusPrepared,
		ExpectedEpoch:               plan.PreviousEpoch,
		ExpectedStateSHA256:         plan.ExpectedActiveStateSHA256,
		PreviousSourceInstance:      plan.PreviousSourceInstance,
		PreviousCursorSHA256:        plan.PreviousCursorSHA256,
		PreviousHighWatermarkSHA256: plan.PreviousHighWatermarkSHA256,
		NextSourceInstance:          plan.NextSourceInstance,
		NextCursor:                  input.NextCursor,
		NextHighWatermark:           input.NextHighWatermark,
		AuthorizationRef:            plan.AuthorizationRef,
		PlanSHA256:                  plan.PlanSHA256,
		PreparedAt:                  plan.PreparedAt,
	}
}

func planFromOperation(op operation) Plan {
	return Plan{
		SchemaVersion:               PlanSchema,
		OperationID:                 op.OperationID,
		Binding:                     op.Binding,
		WorkerUnit:                  op.WorkerUnit,
		ExpectedActiveStateSHA256:   op.ExpectedStateSHA256,
		PreviousEpoch:               op.ExpectedEpoch,
		PreviousSourceInstance:      op.PreviousSourceInstance,
		PreviousCursorSHA256:        op.PreviousCursorSHA256,
		PreviousHighWatermarkSHA256: op.PreviousHighWatermarkSHA256,
		NextEpoch:                   op.ExpectedEpoch + 1,
		NextSourceInstance:          op.NextSourceInstance,
		NextCursorSHA256:            hashText(op.NextCursor),
		NextHighWatermarkSHA256:     hashText(op.NextHighWatermark),
		AuthorizationRef:            op.AuthorizationRef,
		PreparedAt:                  op.PreparedAt,
		PlanSHA256:                  op.PlanSHA256,
	}
}

func loadOperation(ctx context.Context, queryer rowQueryer, operationID string, lock bool) (*operation, error) {
	query := `
SELECT
  operation_id,
  source_system_id::text,
  adapter_manifest_id::text,
  cursor_key,
  worker_unit,
  status,
  expected_state_id::text,
  expected_epoch,
  expected_state_sha256,
  previous_source_instance,
  previous_cursor_sha256,
  previous_high_watermark_sha256,
  next_source_instance,
  next_cursor,
  next_high_watermark,
  next_authorization_ref,
  plan_sha256,
  prepared_at,
  activated_at,
  aborted_at
FROM connector_checkpoint_epoch_operations
WHERE operation_id = $1`
	if lock {
		query += " FOR UPDATE"
	}
	return scanOperation(queryer.QueryRowContext(ctx, query, operationID))
}

func loadLatestOperation(ctx context.Context, queryer rowQueryer, binding Binding) (*operation, error) {
	return scanOperation(queryer.QueryRowContext(ctx, `
SELECT
  operation_id,
  source_system_id::text,
  adapter_manifest_id::text,
  cursor_key,
  worker_unit,
  status,
  expected_state_id::text,
  expected_epoch,
  expected_state_sha256,
  previous_source_instance,
  previous_cursor_sha256,
  previous_high_watermark_sha256,
  next_source_instance,
  next_cursor,
  next_high_watermark,
  next_authorization_ref,
  plan_sha256,
  prepared_at,
  activated_at,
  aborted_at
FROM connector_checkpoint_epoch_operations
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3
ORDER BY prepared_at DESC, operation_id DESC
LIMIT 1`, binding.SourceSystemID, binding.AdapterManifestID, binding.CursorKey))
}

func scanOperation(row *sql.Row) (*operation, error) {
	var op operation
	var activatedAt, abortedAt sql.NullTime
	err := row.Scan(
		&op.OperationID,
		&op.SourceSystemID,
		&op.AdapterManifestID,
		&op.CursorKey,
		&op.WorkerUnit,
		&op.Status,
		&op.ExpectedStateID,
		&op.ExpectedEpoch,
		&op.ExpectedStateSHA256,
		&op.PreviousSourceInstance,
		&op.PreviousCursorSHA256,
		&op.PreviousHighWatermarkSHA256,
		&op.NextSourceInstance,
		&op.NextCursor,
		&op.NextHighWatermark,
		&op.AuthorizationRef,
		&op.PlanSHA256,
		&op.PreparedAt,
		&activatedAt,
		&abortedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOperationMissing
	}
	if err != nil {
		return nil, fmt.Errorf("load checkpoint epoch operation: %w", err)
	}
	op.PreparedAt = normalizedTime(op.PreparedAt)
	op.ActivatedAt = nullableTime(activatedAt)
	op.AbortedAt = nullableTime(abortedAt)
	return &op, nil
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	normalized := normalizedTime(value.Time)
	return &normalized
}

func requireOneRow(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrConflict
	}
	return nil
}
