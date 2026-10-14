package checkpointepoch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// PostgresInstallationEpochPort persists normal-install checkpoint authority
// through the existing checkpoint state, operation and immutable history
// tables. It does not bootstrap an empty DATA_RESET database and it does not
// start or poll a worker.
type PostgresInstallationEpochPort struct {
	db *sql.DB
}

var _ InstallationEpochPort = (*PostgresInstallationEpochPort)(nil)

func NewPostgresInstallationEpochPort(database *sql.DB) (*PostgresInstallationEpochPort, error) {
	if database == nil {
		return nil, fmt.Errorf("%w: installation epoch database", ErrInvalid)
	}
	return &PostgresInstallationEpochPort{db: database}, nil
}

func (port *PostgresInstallationEpochPort) InspectInstallationEpochBoundary(
	ctx context.Context,
	binding Binding,
) (InstallationEpochBoundary, error) {
	if err := port.requireReady(); err != nil {
		return InstallationEpochBoundary{}, err
	}
	if err := ValidateBinding(binding); err != nil {
		return InstallationEpochBoundary{}, err
	}
	tx, err := port.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelSerializable,
		ReadOnly:  true,
	})
	if err != nil {
		return InstallationEpochBoundary{}, fmt.Errorf("begin installation epoch inspection: %w", err)
	}
	defer tx.Rollback()

	boundary, err := inspectPostgresInstallationEpochBoundary(ctx, tx, binding, false)
	if err != nil {
		return InstallationEpochBoundary{}, err
	}
	if err := tx.Commit(); err != nil {
		return InstallationEpochBoundary{}, postgresInstallationEpochError("commit installation epoch inspection", err)
	}
	return boundary, nil
}

func (port *PostgresInstallationEpochPort) CommitInstallationEpoch(
	ctx context.Context,
	commit InstallationEpochCommit,
	installedAt time.Time,
) (ActiveStateSummary, error) {
	if err := port.requireReady(); err != nil {
		return ActiveStateSummary{}, err
	}
	input, err := validatePostgresInstallationEpochCommit(commit, installedAt)
	if err != nil {
		return ActiveStateSummary{}, err
	}
	installedAt = normalizedTime(installedAt)
	tx, err := port.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ActiveStateSummary{}, fmt.Errorf("begin installation epoch commit: %w", err)
	}
	defer tx.Rollback()

	boundary, err := inspectPostgresInstallationEpochBoundary(ctx, tx, commit.Binding, true)
	if err != nil {
		return ActiveStateSummary{}, err
	}
	boundarySHA256, records, activeRecord, err := validateInstallationEpochBoundary(boundary, commit.Binding)
	if err != nil {
		return ActiveStateSummary{}, err
	}
	if boundary.Active != nil && activeRecord != nil &&
		boundary.Active.Epoch == commit.Epoch &&
		installationActiveMatches(*boundary.Active, *activeRecord, input, commit.AuthorizationRef) {
		replayMatches, err := postgresInstallationEpochReplayMatches(ctx, tx, commit)
		if err != nil {
			return ActiveStateSummary{}, err
		}
		if !replayMatches {
			return ActiveStateSummary{}, fmt.Errorf("%w: installation checkpoint replay authority", ErrConflict)
		}
		if err := tx.Commit(); err != nil {
			return ActiveStateSummary{}, postgresInstallationEpochError("commit installation epoch replay", err)
		}
		return *boundary.Active, nil
	}
	if boundarySHA256 != commit.ExpectedBoundarySHA256 {
		return ActiveStateSummary{}, fmt.Errorf("%w: installation epoch boundary digest", ErrConflict)
	}
	if boundary.Active != nil {
		return ActiveStateSummary{}, fmt.Errorf("%w: installation checkpoint state already exists", ErrConflict)
	}

	maximum := int64(0)
	for _, record := range records {
		if err := rejectInstallationIdentityReuse(record, input); err != nil {
			return ActiveStateSummary{}, err
		}
		if record.Epoch > maximum {
			maximum = record.Epoch
		}
	}
	if maximum == math.MaxInt64 {
		return ActiveStateSummary{}, fmt.Errorf("%w: installation checkpoint epoch overflow", ErrConflict)
	}
	expectedEpoch := int64(1)
	if len(records) != 0 {
		expectedEpoch = maximum + 1
	}
	if commit.Epoch != expectedEpoch {
		return ActiveStateSummary{}, fmt.Errorf("%w: installation checkpoint epoch is not strict max+1", ErrConflict)
	}

	_, err = tx.ExecContext(ctx, `
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
  checkpoint_epoch,
  checkpoint_epoch_operation_id,
  metadata,
  created_at,
  updated_at
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $4, $6, $7, $8, $6,
  'idle', 'inactive', $9, NULL,
  jsonb_build_object(
    'contract', $3::text,
    'state_version', 1,
    'checkpoint_epoch', $9::bigint,
    'installation_id', $10::text,
    'installation_epoch_request_sha256', $11::text,
    'installation_epoch_boundary_sha256', $12::text
  ),
  $7,
  $7
)`,
		commit.SourceSystemID,
		commit.AdapterManifestID,
		commit.CursorKey,
		commit.NextCursor,
		commit.NextSourceInstance,
		commit.NextHighWatermark,
		installedAt,
		commit.AuthorizationRef,
		commit.Epoch,
		commit.InstallationID,
		commit.RequestSHA256,
		commit.ExpectedBoundarySHA256,
	)
	if err != nil {
		return ActiveStateSummary{}, postgresInstallationEpochError("persist installation checkpoint epoch", err)
	}

	state, err := loadActiveState(ctx, tx, commit.Binding, true)
	if err != nil {
		return ActiveStateSummary{}, err
	}
	if err := validateTransitionReady(state); err != nil {
		return ActiveStateSummary{}, err
	}
	report, err := reportFrom(state, nil, installedAt)
	if err != nil {
		return ActiveStateSummary{}, err
	}
	if !installationCommittedStateMatches(report.Active, input, commit.Epoch) {
		return ActiveStateSummary{}, fmt.Errorf("%w: installation checkpoint durable readback", ErrConflict)
	}
	if err := tx.Commit(); err != nil {
		return ActiveStateSummary{}, postgresInstallationEpochError("commit installation checkpoint epoch", err)
	}
	return report.Active, nil
}

func postgresInstallationEpochReplayMatches(
	ctx context.Context,
	queryer rowQueryer,
	commit InstallationEpochCommit,
) (bool, error) {
	var installationID, requestSHA256, boundarySHA256 string
	var dormant bool
	err := queryer.QueryRowContext(ctx, `
SELECT
  COALESCE(metadata ->> 'installation_id', ''),
  COALESCE(metadata ->> 'installation_epoch_request_sha256', ''),
  COALESCE(metadata ->> 'installation_epoch_boundary_sha256', ''),
  status = 'idle'
    AND worker_status = 'inactive'
    AND last_import_started_at IS NULL
    AND last_import_completed_at IS NULL
    AND last_poll_failed_at IS NULL
    AND last_error IS NULL
    AND cursor_value = high_watermark
    AND pages_committed = 0
    AND items_received = 0
    AND items_committed = 0
    AND duplicate_noop = 0
    AND consecutive_failures = 0
FROM adapter_import_states
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3`, commit.SourceSystemID, commit.AdapterManifestID, commit.CursorKey).Scan(
		&installationID,
		&requestSHA256,
		&boundarySHA256,
		&dormant,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect installation checkpoint replay authority: %w", err)
	}
	return dormant && installationID == commit.InstallationID &&
		requestSHA256 == commit.RequestSHA256 &&
		boundarySHA256 == commit.ExpectedBoundarySHA256, nil
}

func (port *PostgresInstallationEpochPort) requireReady() error {
	if port == nil || port.db == nil {
		return fmt.Errorf("%w: installation epoch database", ErrInvalid)
	}
	return nil
}

type installationEpochQueryer interface {
	rowQueryer
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func inspectPostgresInstallationEpochBoundary(
	ctx context.Context,
	queryer installationEpochQueryer,
	binding Binding,
	lock bool,
) (InstallationEpochBoundary, error) {
	catalogQuery := `
SELECT source.status = 'active', adapter.active, source.source_type = adapter.source_type
FROM source_systems AS source
JOIN adapter_manifests AS adapter ON adapter.id = $2::uuid
WHERE source.id = $1::uuid`
	if lock {
		catalogQuery += " FOR KEY SHARE OF source, adapter"
	}
	var sourceActive, adapterActive, sourceTypeMatches bool
	err := queryer.QueryRowContext(ctx, catalogQuery, binding.SourceSystemID, binding.AdapterManifestID).Scan(
		&sourceActive,
		&adapterActive,
		&sourceTypeMatches,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return InstallationEpochBoundary{}, fmt.Errorf("%w: installation release catalog binding", ErrNotReady)
	}
	if err != nil {
		return InstallationEpochBoundary{}, fmt.Errorf("inspect installation release catalog: %w", err)
	}
	if !sourceActive || !adapterActive || !sourceTypeMatches {
		return InstallationEpochBoundary{}, fmt.Errorf("%w: inactive or mismatched installation release catalog", ErrNotReady)
	}

	boundary := InstallationEpochBoundary{
		SchemaVersion: InstallationEpochBoundarySchema,
		Binding:       binding,
		Records:       make([]InstallationEpochRecord, 0),
	}
	var activeExists bool
	if err := queryer.QueryRowContext(ctx, `
SELECT EXISTS (
  SELECT 1
  FROM adapter_import_states
  WHERE source_system_id = $1::uuid
    AND adapter_manifest_id = $2::uuid
    AND cursor_key = $3
)`, binding.SourceSystemID, binding.AdapterManifestID, binding.CursorKey).Scan(&activeExists); err != nil {
		return InstallationEpochBoundary{}, fmt.Errorf("inspect installation checkpoint state existence: %w", err)
	}
	if activeExists {
		state, err := loadActiveState(ctx, queryer, binding, lock)
		if err != nil {
			return InstallationEpochBoundary{}, err
		}
		if err := validatePostgresInstallationActiveState(ctx, queryer, state); err != nil {
			return InstallationEpochBoundary{}, err
		}
		report, err := reportFrom(state, nil, state.UpdatedAt)
		if err != nil {
			return InstallationEpochBoundary{}, err
		}
		boundary.Active = &report.Active
		boundary.Records = append(boundary.Records, InstallationEpochRecord{
			RecordID:            state.ID,
			Kind:                InstallationEpochRecordActive,
			Binding:             binding,
			Epoch:               state.Epoch,
			AuthorizationRef:    state.AuthorizationRef,
			SourceInstance:      state.SourceInstance,
			CursorSHA256:        hashText(state.Cursor),
			HighWatermarkSHA256: hashText(state.HighWatermark),
		})
	}

	historyRows, err := queryer.QueryContext(ctx, `
SELECT
  id::text,
  checkpoint_epoch,
  authorization_ref,
  source_instance,
  final_cursor,
  final_high_watermark
FROM connector_checkpoint_epoch_history
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3
ORDER BY checkpoint_epoch, id`, binding.SourceSystemID, binding.AdapterManifestID, binding.CursorKey)
	if err != nil {
		return InstallationEpochBoundary{}, fmt.Errorf("inspect installation checkpoint history: %w", err)
	}
	for historyRows.Next() {
		var id, authorizationRef, sourceInstance, cursor, highWatermark string
		var epoch int64
		if err := historyRows.Scan(
			&id,
			&epoch,
			&authorizationRef,
			&sourceInstance,
			&cursor,
			&highWatermark,
		); err != nil {
			historyRows.Close()
			return InstallationEpochBoundary{}, fmt.Errorf("read installation checkpoint history: %w", err)
		}
		boundary.Records = append(boundary.Records, InstallationEpochRecord{
			RecordID:            id,
			Kind:                InstallationEpochRecordHistory,
			Binding:             binding,
			Epoch:               epoch,
			AuthorizationRef:    authorizationRef,
			SourceInstance:      sourceInstance,
			CursorSHA256:        hashText(cursor),
			HighWatermarkSHA256: hashText(highWatermark),
		})
	}
	if err := historyRows.Err(); err != nil {
		historyRows.Close()
		return InstallationEpochBoundary{}, fmt.Errorf("iterate installation checkpoint history: %w", err)
	}
	if err := historyRows.Close(); err != nil {
		return InstallationEpochBoundary{}, fmt.Errorf("close installation checkpoint history: %w", err)
	}

	operationQuery := `
SELECT
  operation_id,
  status,
  expected_epoch,
  next_authorization_ref,
  next_source_instance,
  next_cursor,
  next_high_watermark
FROM connector_checkpoint_epoch_operations
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3
ORDER BY expected_epoch, operation_id`
	if lock {
		operationQuery += " FOR SHARE"
	}
	operationRows, err := queryer.QueryContext(ctx, operationQuery, binding.SourceSystemID, binding.AdapterManifestID, binding.CursorKey)
	if err != nil {
		return InstallationEpochBoundary{}, fmt.Errorf("inspect installation checkpoint operations: %w", err)
	}
	for operationRows.Next() {
		var operationID, status, authorizationRef, sourceInstance, cursor, highWatermark string
		var expectedEpoch int64
		if err := operationRows.Scan(
			&operationID,
			&status,
			&expectedEpoch,
			&authorizationRef,
			&sourceInstance,
			&cursor,
			&highWatermark,
		); err != nil {
			operationRows.Close()
			return InstallationEpochBoundary{}, fmt.Errorf("read installation checkpoint operation: %w", err)
		}
		if expectedEpoch == math.MaxInt64 {
			operationRows.Close()
			return InstallationEpochBoundary{}, fmt.Errorf("%w: reserved installation checkpoint epoch overflow", ErrConflict)
		}
		boundary.Records = append(boundary.Records, InstallationEpochRecord{
			RecordID:            operationID,
			Kind:                InstallationEpochRecordOperation,
			Status:              status,
			Binding:             binding,
			Epoch:               expectedEpoch + 1,
			AuthorizationRef:    authorizationRef,
			SourceInstance:      sourceInstance,
			CursorSHA256:        hashText(cursor),
			HighWatermarkSHA256: hashText(highWatermark),
		})
	}
	if err := operationRows.Err(); err != nil {
		operationRows.Close()
		return InstallationEpochBoundary{}, fmt.Errorf("iterate installation checkpoint operations: %w", err)
	}
	if err := operationRows.Close(); err != nil {
		return InstallationEpochBoundary{}, fmt.Errorf("close installation checkpoint operations: %w", err)
	}

	_, records, _, err := validateInstallationEpochBoundary(boundary, binding)
	if err != nil {
		return InstallationEpochBoundary{}, err
	}
	boundary.Records = records
	return boundary, nil
}

func validatePostgresInstallationActiveState(
	ctx context.Context,
	queryer rowQueryer,
	state activeState,
) error {
	installationID, installationBound, err := installationIDFromAuthorizationRef(state.AuthorizationRef)
	if err != nil || !installationBound {
		return err
	}
	if state.EpochOperationID != "" || state.ActivationCursor != state.Cursor ||
		state.ActivationHighWatermark != state.HighWatermark || state.Cursor != state.HighWatermark ||
		state.LegacyStatus != "idle" || state.WorkerStatus != "inactive" ||
		state.LastPollStartedAt != nil || state.LastPollSucceededAt != nil || state.LastPollFailedAt != nil ||
		state.LastErrorCode != "" || state.PagesCommitted != 0 || state.ItemsReceived != 0 ||
		state.ItemsCommitted != 0 || state.DuplicateNoop != 0 || state.ConsecutiveFailures != 0 {
		return fmt.Errorf("%w: installation checkpoint is no longer dormant", ErrConflict)
	}
	workerUnit := ""
	switch state.CursorKey {
	case CompletedExecutionCursorKey:
		workerUnit = CompletedExecutionWorkerUnit
	case LiteLLMCursorKey:
		workerUnit = LiteLLMWorkerUnit
	default:
		return fmt.Errorf("%w: installation checkpoint cursor binding", ErrConflict)
	}
	expectedRequestSHA256, err := installationPrepareRequestSHA256(PrepareInput{
		Binding:            state.Binding,
		WorkerUnit:         workerUnit,
		NextSourceInstance: state.SourceInstance,
		NextCursor:         state.Cursor,
		NextHighWatermark:  state.HighWatermark,
		AuthorizationRef:   state.AuthorizationRef,
	})
	if err != nil {
		return err
	}
	var contract, stateVersion, checkpointEpoch, metadataInstallationID string
	var requestSHA256, boundarySHA256 string
	err = queryer.QueryRowContext(ctx, `
SELECT
  COALESCE(metadata ->> 'contract', ''),
  COALESCE(metadata ->> 'state_version', ''),
  COALESCE(metadata ->> 'checkpoint_epoch', ''),
  COALESCE(metadata ->> 'installation_id', ''),
  COALESCE(metadata ->> 'installation_epoch_request_sha256', ''),
  COALESCE(metadata ->> 'installation_epoch_boundary_sha256', '')
FROM adapter_import_states
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3`, state.SourceSystemID, state.AdapterManifestID, state.CursorKey).Scan(
		&contract,
		&stateVersion,
		&checkpointEpoch,
		&metadataInstallationID,
		&requestSHA256,
		&boundarySHA256,
	)
	if err != nil {
		return fmt.Errorf("inspect installation checkpoint metadata: %w", err)
	}
	if contract != state.CursorKey || stateVersion != "1" || checkpointEpoch != fmt.Sprintf("%d", state.Epoch) ||
		metadataInstallationID != installationID || requestSHA256 != expectedRequestSHA256 ||
		!sha256Pattern.MatchString(boundarySHA256) {
		return fmt.Errorf("%w: installation checkpoint metadata authority", ErrConflict)
	}
	return nil
}

func validatePostgresInstallationEpochCommit(
	commit InstallationEpochCommit,
	installedAt time.Time,
) (InstallationEpochInput, error) {
	if commit.SchemaVersion != InstallationEpochCommitSchema || installedAt.IsZero() || commit.Epoch < 1 ||
		!sha256Pattern.MatchString(commit.ExpectedBoundarySHA256) || !sha256Pattern.MatchString(commit.RequestSHA256) {
		return InstallationEpochInput{}, fmt.Errorf("%w: installation epoch commit envelope", ErrInvalid)
	}
	if err := validatePrepareInput(commit.PrepareInput); err != nil {
		return InstallationEpochInput{}, err
	}
	authorizationRef, err := InstallationAuthorizationRef(commit.InstallationID)
	if err != nil || authorizationRef != commit.AuthorizationRef {
		return InstallationEpochInput{}, fmt.Errorf("%w: installation epoch authorization", ErrInvalid)
	}
	requestSHA256, err := installationPrepareRequestSHA256(commit.PrepareInput)
	if err != nil {
		return InstallationEpochInput{}, err
	}
	if requestSHA256 != commit.RequestSHA256 {
		return InstallationEpochInput{}, fmt.Errorf("%w: installation epoch request digest", ErrConflict)
	}
	return InstallationEpochInput{
		Binding:        commit.Binding,
		InstallationID: commit.InstallationID,
		WorkerUnit:     commit.WorkerUnit,
		SourceInstance: commit.NextSourceInstance,
		Cursor:         commit.NextCursor,
		HighWatermark:  commit.NextHighWatermark,
	}, nil
}

func postgresInstallationEpochError(action string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503", "23505", "40001", "40P01":
			return fmt.Errorf("%w: %s", ErrConflict, action)
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}
