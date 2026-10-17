package openinferenceoffline

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/foigent/x-insight/internal/adapters/openinference"
	"github.com/foigent/x-insight/internal/connector"
	"github.com/foigent/x-insight/internal/integration/boundedoffline"
)

type PostgresStore struct {
	db         *sql.DB
	operations *boundedoffline.PostgresOperationStore
}

func NewPostgresStore(database *sql.DB) (*PostgresStore, error) {
	if database == nil {
		return nil, errors.New("OpenInference operational database is required")
	}
	operations, err := boundedoffline.NewPostgresOperationStore(database)
	if err != nil {
		return nil, err
	}
	return &PostgresStore{db: database, operations: operations}, nil
}

func (store *PostgresStore) EnsureRegistry(
	ctx context.Context,
	sourceInstance string,
) error {
	if store == nil || store.db == nil || !validOperationalText(sourceInstance, 128) {
		return errors.New("OpenInference registry binding is invalid")
	}
	binding := openinference.Binding()
	if err := binding.Validate(); err != nil {
		return fmt.Errorf("validate OpenInference registry binding: %w", err)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin OpenInference registry transaction: %w", err)
	}
	defer tx.Rollback()
	sourceResult, err := tx.ExecContext(ctx, `
INSERT INTO source_systems (
  id, source_key, source_type, display_name, source_version, deployment_ref,
  status, metadata
) VALUES (
  $1::uuid, $2, 'openinference', 'OpenInference bounded OTLP export', $7, $3,
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
		openinference.SourceSystemID,
		"source-system:"+openinference.SourceSystemID,
		sourceInstance,
		openinference.ConnectorID,
		openinference.ConnectorVersion,
		openinference.ProtocolVersion,
		openinference.SourceSchemaV1,
	)
	if err != nil {
		return fmt.Errorf("register OpenInference source: %w", err)
	}
	if err := requireOneRow(sourceResult, "OpenInference source registry"); err != nil {
		return err
	}
	adapterResult, err := tx.ExecContext(ctx, `
INSERT INTO adapter_manifests (
  id, adapter_id, adapter_version, source_type, supported_source_versions,
  fidelity_level, ingestion_modes, direct_field_groups, derived_field_groups,
  known_capture_gaps, payload_behavior, idempotency_strategy, cursor_strategy,
  declared_event_types, manifest_json, active
) VALUES (
  $1::uuid, $2, $3, 'openinference', ARRAY[$8::text]::text[],
  'F2', ARRAY[$11::text]::text[],
  ARRAY['correlation','capability','usage','outcome']::text[],
  ARRAY['identity','payload_policy']::text[],
  ARRAY['not_available_from_source']::text[],
  'metadata_only_default',
  'source_ref_event_type_source_event_id',
  NULL,
  ARRAY['model.request.succeeded','model.request.failed','gap.capture.detected']::text[],
  jsonb_build_object(
    'connector_id', $2::text,
    'connector_version', $3::text,
    'protocol_version', $4::text,
    'protocol_commit', $5::text,
    'protocol_release_digest', $6::text,
    'projection_profile', $7::text,
    'reference_manifest_sha256', $9::text,
    'event_to_m0_sha256', $10::text,
    'delivery_mode', 'bounded_offline',
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
  AND adapter_manifests.cursor_strategy IS NOT DISTINCT FROM EXCLUDED.cursor_strategy
  AND adapter_manifests.declared_event_types = EXCLUDED.declared_event_types
  AND adapter_manifests.manifest_json = EXCLUDED.manifest_json`,
		openinference.AdapterManifestID,
		openinference.ConnectorID,
		openinference.ConnectorVersion,
		openinference.ProtocolVersion,
		openinference.ProtocolCommit,
		openinference.ProtocolReleaseSHA,
		openinference.ProjectionProfileID,
		openinference.SourceSchemaV1,
		binding.ReferenceManifest.SHA256,
		binding.Mappings.EventToM0.SHA256,
		openinference.IngestionCaptureMode,
	)
	if err != nil {
		return fmt.Errorf("register OpenInference adapter: %w", err)
	}
	if err := requireOneRow(adapterResult, "OpenInference adapter registry"); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit OpenInference registry transaction: %w", err)
	}
	return nil
}

func (store *PostgresStore) WithOperation(
	ctx context.Context,
	binding OperationBinding,
	operation func(
		context.Context,
		*connector.SourceNeutralOperationalReceipt,
	) (connector.SourceNeutralOperationalReceipt, error),
) (connector.SourceNeutralOperationalReceipt, error) {
	if store == nil || store.operations == nil ||
		binding.ConnectorID != openinference.ConnectorID {
		return connector.SourceNeutralOperationalReceipt{}, errors.New(
			"OpenInference receipt operation is invalid",
		)
	}
	return store.operations.WithOperation(ctx, binding, operation)
}

func requireOneRow(result sql.Result, label string) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read %s result: %w", label, err)
	}
	if rowsAffected != 1 {
		return fmt.Errorf("%s conflicts with an existing immutable identity", label)
	}
	return nil
}

func validOperationalText(value string, maximum int) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) < 1 ||
		utf8.RuneCountInString(value) > maximum {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}
