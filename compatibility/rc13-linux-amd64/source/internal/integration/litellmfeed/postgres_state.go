package litellmfeed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/foigent/x-insight/internal/adapters/litellm"
)

var (
	ErrStateNotFound = errors.New("LiteLLM Connector state was not found")
	ErrStateConflict = errors.New("LiteLLM Connector state changed concurrently")
)

type WorkerState struct {
	ID                      string
	SourceSystemID          string
	AdapterManifestID       string
	CursorKey               string
	CheckpointEpoch         int64
	CheckpointOperationID   string
	SourceInstance          string
	TenantRefPresent        bool
	TenantRef               string
	OrgRef                  string
	ProjectRef              string
	ActivationCursor        string
	ActivationHighWatermark string
	ActivatedAt             time.Time
	AuthorizationRef        string
	Cursor                  string
	HighWatermark           string
	WorkerStatus            string
	PagesCommitted          int64
	ItemsReceived           int64
	ItemsCommitted          int64
	DuplicateNoop           int64
	ConsecutiveFailures     int64
	LastErrorCode           string
}

type ScopeBinding struct {
	TenantRef  *string
	OrgRef     string
	ProjectRef string
}

type PageCommit struct {
	ExpectedCursor string
	NextCursor     string
	CompletedAt    time.Time
	ItemsReceived  int64
	ItemsInserted  int64
	DuplicateNoop  int64
}

type StateStore struct {
	db *sql.DB
}

func NewStateStore(database *sql.DB) (*StateStore, error) {
	if database == nil {
		return nil, errors.New("LiteLLM state database is required")
	}
	return &StateStore{db: database}, nil
}

func (store *StateStore) EnsureRegistry(
	ctx context.Context,
	sourceInstance string,
) error {
	if store == nil || store.db == nil || !validSourceInstance(sourceInstance) {
		return errors.New("LiteLLM registry binding is invalid")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin LiteLLM registry transaction: %w", err)
	}
	defer tx.Rollback()
	sourceResult, err := tx.ExecContext(ctx, `
INSERT INTO source_systems (
  id, source_key, source_type, display_name, source_version, deployment_ref,
  status, metadata
) VALUES (
  $1::uuid, $2, 'litellm', 'LiteLLM spend logs', $7, $3,
  'active', jsonb_build_object(
    'connector_id', $4::text,
    'connector_version', $5::text,
    'protocol_version', $6::text,
    'producer_instance', $3::text
  )
)
ON CONFLICT (id) DO UPDATE SET
  status = source_systems.status
WHERE source_systems.source_key = EXCLUDED.source_key
  AND source_systems.source_type = EXCLUDED.source_type
  AND source_systems.source_version = EXCLUDED.source_version
  AND source_systems.deployment_ref = EXCLUDED.deployment_ref
  AND source_systems.metadata = EXCLUDED.metadata`,
		litellm.SourceSystemID,
		"source-system:"+litellm.SourceSystemID,
		sourceInstance,
		litellm.ConnectorID,
		litellm.ConnectorVersion,
		litellm.ProtocolVersion,
		litellm.SourceSchemaV1,
	)
	if err != nil {
		return fmt.Errorf("register LiteLLM source: %w", err)
	}
	if err := requireOneRow(sourceResult, "LiteLLM source registry"); err != nil {
		return err
	}
	adapterResult, err := tx.ExecContext(ctx, `
INSERT INTO adapter_manifests (
  id, adapter_id, adapter_version, source_type, supported_source_versions,
  fidelity_level, ingestion_modes, direct_field_groups, derived_field_groups,
  known_capture_gaps, payload_behavior, idempotency_strategy, cursor_strategy,
  declared_event_types, manifest_json, active
) VALUES (
  $1::uuid, $2, $3, 'litellm', ARRAY[$8::text]::text[],
  'F1', ARRAY['api_import']::text[],
  ARRAY['correlation','capability','usage','outcome']::text[],
  ARRAY['identity','payload_policy']::text[],
  ARRAY['not_available_from_source']::text[],
  'metadata_only_default',
  'source_ref_event_type_source_event_id',
  'litellm_start_time_boundary_v1',
  ARRAY['model.request.succeeded','model.request.failed','gap.capture.detected']::text[],
  jsonb_build_object(
    'connector_id', $2::text,
    'connector_version', $3::text,
    'protocol_version', $4::text,
    'protocol_commit', $5::text,
    'protocol_release_digest', $6::text,
    'projection_profile', $7::text,
    'product_managed_registry', true
  ),
  true
)
ON CONFLICT (id) DO UPDATE SET
  active = adapter_manifests.active
WHERE adapter_manifests.adapter_id = EXCLUDED.adapter_id
  AND adapter_manifests.adapter_version = EXCLUDED.adapter_version
  AND adapter_manifests.source_type = EXCLUDED.source_type
  AND adapter_manifests.supported_source_versions = EXCLUDED.supported_source_versions
  AND adapter_manifests.fidelity_level = EXCLUDED.fidelity_level
  AND adapter_manifests.ingestion_modes = EXCLUDED.ingestion_modes
  AND adapter_manifests.direct_field_groups = EXCLUDED.direct_field_groups
  AND adapter_manifests.derived_field_groups = EXCLUDED.derived_field_groups
  AND adapter_manifests.known_capture_gaps = EXCLUDED.known_capture_gaps
  AND adapter_manifests.payload_behavior = EXCLUDED.payload_behavior
  AND adapter_manifests.idempotency_strategy = EXCLUDED.idempotency_strategy
  AND adapter_manifests.cursor_strategy = EXCLUDED.cursor_strategy
  AND adapter_manifests.declared_event_types = EXCLUDED.declared_event_types
  AND adapter_manifests.manifest_json = EXCLUDED.manifest_json`,
		litellm.AdapterManifestID,
		litellm.ConnectorID,
		litellm.ConnectorVersion,
		litellm.ProtocolVersion,
		litellm.ProtocolCommit,
		litellm.ProtocolReleaseSHA,
		litellm.ProjectionProfileID,
		litellm.SourceSchemaV1,
	)
	if err != nil {
		return fmt.Errorf("register LiteLLM adapter: %w", err)
	}
	if err := requireOneRow(adapterResult, "LiteLLM adapter registry"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit LiteLLM registry transaction: %w", err)
	}
	return nil
}

func (store *StateStore) Load(ctx context.Context) (WorkerState, error) {
	if store == nil || store.db == nil {
		return WorkerState{}, errors.New("LiteLLM state store is unavailable")
	}
	var state WorkerState
	var sourceInstance sql.NullString
	var activationCursor sql.NullString
	var activationHighWatermark sql.NullString
	var activatedAt sql.NullTime
	var authorizationRef sql.NullString
	var cursor sql.NullString
	var highWatermark sql.NullString
	var lastError sql.NullString
	var tenantRefPresent sql.NullBool
	var tenantRef sql.NullString
	var orgRef sql.NullString
	var projectRef sql.NullString
	var sourceActive bool
	var adapterActive bool
	err := store.db.QueryRowContext(ctx, `
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
  state.pages_committed,
  state.items_received,
  state.items_committed,
  state.duplicate_noop,
  state.consecutive_failures::bigint,
  state.last_error,
  (state.metadata ->> 'tenant_ref_present')::boolean,
  state.metadata ->> 'tenant_ref',
  state.metadata ->> 'org_ref',
  state.metadata ->> 'project_ref',
  source.status = 'active',
  adapter.active
FROM adapter_import_states AS state
JOIN source_systems AS source ON source.id = state.source_system_id
JOIN adapter_manifests AS adapter ON adapter.id = state.adapter_manifest_id
WHERE state.source_system_id = $1::uuid
  AND state.adapter_manifest_id = $2::uuid
  AND state.cursor_key = $3`,
		litellm.SourceSystemID,
		litellm.AdapterManifestID,
		litellm.CursorKey,
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
		&state.WorkerStatus,
		&state.PagesCommitted,
		&state.ItemsReceived,
		&state.ItemsCommitted,
		&state.DuplicateNoop,
		&state.ConsecutiveFailures,
		&lastError,
		&tenantRefPresent,
		&tenantRef,
		&orgRef,
		&projectRef,
		&sourceActive,
		&adapterActive,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkerState{}, ErrStateNotFound
	}
	if err != nil {
		return WorkerState{}, fmt.Errorf("load LiteLLM state: %w", err)
	}
	if !sourceActive || !adapterActive ||
		state.SourceSystemID != litellm.SourceSystemID ||
		state.AdapterManifestID != litellm.AdapterManifestID ||
		state.CursorKey != litellm.CursorKey ||
		state.CheckpointEpoch < 1 ||
		!sourceInstance.Valid || !activationCursor.Valid ||
		!activationHighWatermark.Valid || !activatedAt.Valid ||
		!authorizationRef.Valid || !cursor.Valid || !highWatermark.Valid {
		return WorkerState{}, errors.New("LiteLLM state registry or activation identity is invalid")
	}
	if !tenantRefPresent.Valid ||
		!orgRef.Valid ||
		!projectRef.Valid ||
		tenantRef.Valid != tenantRefPresent.Bool ||
		!validScopeBinding(ScopeBinding{
			TenantRef:  nullableStringPointer(tenantRef),
			OrgRef:     orgRef.String,
			ProjectRef: projectRef.String,
		}) {
		return WorkerState{}, errors.New("LiteLLM state mapping scope is invalid")
	}
	state.SourceInstance = sourceInstance.String
	state.TenantRefPresent = tenantRefPresent.Bool
	if tenantRef.Valid {
		state.TenantRef = tenantRef.String
	}
	state.OrgRef = orgRef.String
	state.ProjectRef = projectRef.String
	state.ActivationCursor = activationCursor.String
	state.ActivationHighWatermark = activationHighWatermark.String
	state.ActivatedAt = activatedAt.Time.UTC()
	state.AuthorizationRef = authorizationRef.String
	state.Cursor = cursor.String
	state.HighWatermark = highWatermark.String
	if lastError.Valid {
		state.LastErrorCode = lastError.String
	}
	if _, err := DecodeCheckpoint(state.ActivationCursor); err != nil {
		return WorkerState{}, fmt.Errorf(
			"decode durable LiteLLM activation Checkpoint: %w",
			err,
		)
	}
	if _, err := DecodeCheckpoint(state.ActivationHighWatermark); err != nil {
		return WorkerState{}, fmt.Errorf(
			"decode durable LiteLLM activation high-watermark: %w",
			err,
		)
	}
	if _, err := DecodeCheckpoint(state.Cursor); err != nil {
		return WorkerState{}, fmt.Errorf("decode durable LiteLLM Checkpoint: %w", err)
	}
	if _, err := DecodeCheckpoint(state.HighWatermark); err != nil {
		return WorkerState{}, fmt.Errorf(
			"decode durable LiteLLM high-watermark: %w",
			err,
		)
	}
	if state.ActivationCursor != state.ActivationHighWatermark ||
		state.Cursor != state.HighWatermark ||
		state.ItemsCommitted+state.DuplicateNoop != state.ItemsReceived {
		return WorkerState{}, errors.New("LiteLLM durable state counters are invalid")
	}
	return state, nil
}

func (store *StateStore) Activate(
	ctx context.Context,
	sourceInstance string,
	scope ScopeBinding,
	checkpoint Checkpoint,
	activatedAt time.Time,
) (WorkerState, error) {
	if !validScopeBinding(scope) {
		return WorkerState{}, errors.New("LiteLLM activation mapping scope is invalid")
	}
	if err := store.EnsureRegistry(ctx, sourceInstance); err != nil {
		return WorkerState{}, err
	}
	encoded, err := checkpoint.Encode()
	if err != nil {
		return WorkerState{}, err
	}
	activatedAt = activatedAt.UTC().Truncate(time.Microsecond)
	result, err := store.db.ExecContext(ctx, `
INSERT INTO adapter_import_states (
  source_system_id, adapter_manifest_id, cursor_key, cursor_value,
  activation_source_instance, activation_cursor, activation_high_watermark,
  activated_at, authorization_ref, high_watermark, status, worker_status,
  metadata
) VALUES (
  $1::uuid, $2::uuid, $3, $4,
  $5, $4, $4,
  $6, $7, $4, 'idle', 'idle',
  jsonb_build_object(
    'contract', $3::text,
    'state_version', 1,
    'checkpoint_strategy', 'litellm_start_time_boundary_v1',
    'tenant_ref_present', $8::boolean,
    'tenant_ref', $9::text,
    'org_ref', $10::text,
    'project_ref', $11::text
  )
)
ON CONFLICT (source_system_id, adapter_manifest_id, cursor_key) DO NOTHING`,
		litellm.SourceSystemID,
		litellm.AdapterManifestID,
		litellm.CursorKey,
		encoded,
		sourceInstance,
		activatedAt,
		litellm.ProtocolReleaseSHA,
		scope.TenantRef != nil,
		nullableScopeRef(scope.TenantRef),
		scope.OrgRef,
		scope.ProjectRef,
	)
	if err != nil {
		return WorkerState{}, fmt.Errorf("activate LiteLLM state: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil || (affected != 0 && affected != 1) {
		return WorkerState{}, errors.New("activate LiteLLM state affected an invalid row count")
	}
	state, err := store.Load(ctx)
	if err != nil {
		return WorkerState{}, err
	}
	if state.SourceInstance != sourceInstance ||
		!stateMatchesScope(state, scope) ||
		state.ActivationCursor != encoded ||
		state.AuthorizationRef != litellm.ProtocolReleaseSHA {
		return WorkerState{}, errors.New("LiteLLM state activation conflicts with existing state")
	}
	return state, nil
}

func validScopeBinding(scope ScopeBinding) bool {
	if !validScopeRef(scope.OrgRef) || !validScopeRef(scope.ProjectRef) {
		return false
	}
	return scope.TenantRef == nil || validSourceInstance(*scope.TenantRef)
}

func validScopeRef(value string) bool {
	if value == "" || value == "0" || (len(value) > 1 && value[0] == '0') {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func nullableScopeRef(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	copy := value.String
	return &copy
}

func stateMatchesScope(state WorkerState, scope ScopeBinding) bool {
	return state.OrgRef == scope.OrgRef &&
		state.ProjectRef == scope.ProjectRef &&
		state.TenantRefPresent == (scope.TenantRef != nil) &&
		(!state.TenantRefPresent || state.TenantRef == *scope.TenantRef)
}

func (store *StateStore) MarkPollStarted(
	ctx context.Context,
	expectedCursor string,
	at time.Time,
) error {
	result, err := store.db.ExecContext(ctx, `
UPDATE adapter_import_states
SET status = 'running', worker_status = 'polling',
    last_import_started_at = $4, updated_at = $4
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3
  AND cursor_value = $5
  AND NOT EXISTS (
    SELECT 1
    FROM connector_checkpoint_epoch_operations AS epoch_operation
    WHERE epoch_operation.source_system_id = adapter_import_states.source_system_id
      AND epoch_operation.adapter_manifest_id = adapter_import_states.adapter_manifest_id
      AND epoch_operation.cursor_key = adapter_import_states.cursor_key
      AND epoch_operation.status = 'prepared'
  )`,
		litellm.SourceSystemID,
		litellm.AdapterManifestID,
		litellm.CursorKey,
		at.UTC().Truncate(time.Microsecond),
		expectedCursor,
	)
	if err != nil {
		return err
	}
	return requireOneRow(result, "LiteLLM poll start")
}

func (store *StateStore) CommitPage(ctx context.Context, page PageCommit) error {
	if page.ItemsReceived < 0 || page.ItemsInserted < 0 ||
		page.DuplicateNoop < 0 ||
		page.ItemsInserted+page.DuplicateNoop != page.ItemsReceived {
		return errors.New("LiteLLM page convergence counters are invalid")
	}
	if _, err := DecodeCheckpoint(page.ExpectedCursor); err != nil {
		return err
	}
	if _, err := DecodeCheckpoint(page.NextCursor); err != nil {
		return err
	}
	result, err := store.db.ExecContext(ctx, `
UPDATE adapter_import_states
SET cursor_value = $4,
    high_watermark = $4,
    status = 'idle',
    worker_status = 'idle',
    last_import_completed_at = $5,
    last_error = NULL,
    last_poll_failed_at = NULL,
    pages_committed = pages_committed + 1,
    items_received = items_received + $6,
    items_committed = items_committed + $7,
    duplicate_noop = duplicate_noop + $8,
    consecutive_failures = 0,
    updated_at = $5
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3
  AND cursor_value = $9`,
		litellm.SourceSystemID,
		litellm.AdapterManifestID,
		litellm.CursorKey,
		page.NextCursor,
		page.CompletedAt.UTC().Truncate(time.Microsecond),
		page.ItemsReceived,
		page.ItemsInserted,
		page.DuplicateNoop,
		page.ExpectedCursor,
	)
	if err != nil {
		return err
	}
	return requireOneRow(result, "LiteLLM page commit")
}

func (store *StateStore) MarkFailure(
	ctx context.Context,
	expectedCursor string,
	code string,
	retryable bool,
	at time.Time,
) error {
	status := "error"
	if retryable {
		status = "backoff"
	}
	result, err := store.db.ExecContext(ctx, `
UPDATE adapter_import_states
SET status = 'error', worker_status = $4, last_error = $5,
    last_poll_failed_at = $6, consecutive_failures = consecutive_failures + 1,
    updated_at = $6
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3
  AND cursor_value = $7`,
		litellm.SourceSystemID,
		litellm.AdapterManifestID,
		litellm.CursorKey,
		status,
		code,
		at.UTC().Truncate(time.Microsecond),
		expectedCursor,
	)
	if err != nil {
		return err
	}
	return requireOneRow(result, "LiteLLM failure state")
}

func (store *StateStore) MarkInactive(ctx context.Context, expectedCursor string, at time.Time) error {
	result, err := store.db.ExecContext(ctx, `
UPDATE adapter_import_states
SET status = 'idle', worker_status = 'inactive', updated_at = $4
WHERE source_system_id = $1::uuid
  AND adapter_manifest_id = $2::uuid
  AND cursor_key = $3
  AND cursor_value = $5`,
		litellm.SourceSystemID,
		litellm.AdapterManifestID,
		litellm.CursorKey,
		at.UTC().Truncate(time.Microsecond),
		expectedCursor,
	)
	if err != nil {
		return err
	}
	return requireOneRow(result, "LiteLLM inactive state")
}

func requireOneRow(result sql.Result, label string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return fmt.Errorf("%w: %s affected %d rows", ErrStateConflict, label, affected)
	}
	return nil
}

func validSourceInstance(value string) bool {
	if len(value) < 1 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character == 0 || character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}
