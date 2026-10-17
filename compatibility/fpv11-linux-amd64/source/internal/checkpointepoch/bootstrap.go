package checkpointepoch

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// EmptyBootstrapInput is intentionally separate from an epoch transition.
// It creates the first state in a freshly migrated, historically empty
// database while retaining the maximum epoch proved from the rollback source.
type EmptyBootstrapInput struct {
	PrepareInput
	CheckpointEpoch int64
}

// BootstrapEmpty inserts exactly one inactive Connector state into a database
// with no current state and no epoch operation/history. It never polls, creates
// transition history, or assigns a hard-coded epoch.
func BootstrapEmpty(
	ctx context.Context,
	database *sql.DB,
	input EmptyBootstrapInput,
	activatedAt time.Time,
) (ActiveStateSummary, error) {
	if database == nil {
		return ActiveStateSummary{}, fmt.Errorf("%w: database", ErrInvalid)
	}
	if err := validatePrepareInput(input.PrepareInput); err != nil {
		return ActiveStateSummary{}, err
	}
	if input.CheckpointEpoch < 1 || activatedAt.IsZero() {
		return ActiveStateSummary{}, fmt.Errorf("%w: empty bootstrap epoch or activation time", ErrInvalid)
	}
	activatedAt = normalizedTime(activatedAt)
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return ActiveStateSummary{}, fmt.Errorf("begin empty checkpoint bootstrap: %w", err)
	}
	defer tx.Rollback()

	var sourceActive, adapterActive, sourceTypeMatches bool
	if err := tx.QueryRowContext(ctx, `
SELECT source.status = 'active', adapter.active, source.source_type = adapter.source_type
FROM source_systems AS source
JOIN adapter_manifests AS adapter ON adapter.id = $2::uuid
WHERE source.id = $1::uuid`, input.SourceSystemID, input.AdapterManifestID).Scan(
		&sourceActive,
		&adapterActive,
		&sourceTypeMatches,
	); err != nil || !sourceActive || !adapterActive || !sourceTypeMatches {
		return ActiveStateSummary{}, fmt.Errorf("%w: inactive or mismatched release catalog", ErrNotReady)
	}

	var states, operations, history int64
	if err := tx.QueryRowContext(ctx, `
SELECT
  (SELECT count(*) FROM adapter_import_states),
  (SELECT count(*) FROM connector_checkpoint_epoch_operations),
  (SELECT count(*) FROM connector_checkpoint_epoch_history)`).Scan(
		&states,
		&operations,
		&history,
	); err != nil {
		return ActiveStateSummary{}, fmt.Errorf("inspect empty checkpoint bootstrap boundary: %w", err)
	}
	if states != 0 || operations != 0 || history != 0 {
		return ActiveStateSummary{}, fmt.Errorf("%w: empty checkpoint bootstrap requires zero state and history", ErrConflict)
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
  metadata
) VALUES (
  $1::uuid, $2::uuid, $3, $4, $5, $4, $6, $7, $8, $6,
  'idle', 'inactive', $9, NULL,
  jsonb_build_object(
    'contract', $3::text,
    'state_version', 1,
    'checkpoint_epoch', $9::bigint,
    'bootstrap_mode', 'fresh_database'
  )
)`,
		input.SourceSystemID,
		input.AdapterManifestID,
		input.CursorKey,
		input.NextCursor,
		input.NextSourceInstance,
		input.NextHighWatermark,
		activatedAt,
		input.AuthorizationRef,
		input.CheckpointEpoch,
	)
	if err != nil {
		return ActiveStateSummary{}, fmt.Errorf("persist empty checkpoint bootstrap: %w", err)
	}
	state, err := loadActiveState(ctx, tx, input.Binding, false)
	if err != nil {
		return ActiveStateSummary{}, err
	}
	if err := validateTransitionReady(state); err != nil {
		return ActiveStateSummary{}, err
	}
	if state.Epoch != input.CheckpointEpoch ||
		state.EpochOperationID != "" ||
		state.SourceInstance != input.NextSourceInstance ||
		state.Cursor != input.NextCursor ||
		state.HighWatermark != input.NextHighWatermark ||
		state.PagesCommitted != 0 ||
		state.ItemsReceived != 0 ||
		state.ItemsCommitted != 0 ||
		state.DuplicateNoop != 0 ||
		state.ConsecutiveFailures != 0 {
		return ActiveStateSummary{}, fmt.Errorf("%w: empty checkpoint bootstrap readback mismatch", ErrConflict)
	}
	stateSHA256, err := hashActiveState(state)
	if err != nil {
		return ActiveStateSummary{}, err
	}
	if err := tx.Commit(); err != nil {
		return ActiveStateSummary{}, fmt.Errorf("commit empty checkpoint bootstrap: %w", err)
	}
	report, err := reportFrom(state, nil, activatedAt)
	if err != nil {
		return ActiveStateSummary{}, err
	}
	if report.Active.StateSHA256 != stateSHA256 {
		return ActiveStateSummary{}, fmt.Errorf("%w: empty checkpoint bootstrap state digest mismatch", ErrConflict)
	}
	return report.Active, nil
}
