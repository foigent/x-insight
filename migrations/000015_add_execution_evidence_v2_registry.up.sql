BEGIN;

DO $$
DECLARE
  affected_rows integer;
BEGIN
  INSERT INTO source_systems (
    id, source_key, source_type, display_name, source_version, status, metadata
  ) VALUES (
    '11111111-1111-1111-1111-111111111111',
    'source-system:11111111-1111-1111-1111-111111111111',
    'unirouter',
    'Imported unirouter source',
    'unirouter.execution-evidence.bundle.v2',
    'active',
    '{}'::jsonb
  )
  ON CONFLICT (id) DO UPDATE SET
    source_version = EXCLUDED.source_version,
    status = 'active'
  WHERE source_systems.source_key = EXCLUDED.source_key
    AND source_systems.source_type = EXCLUDED.source_type
    AND (
      source_systems.source_version IS NULL
      OR source_systems.source_version IN (
        'unirouter.prd5.xinsight.snapshot.v1',
        'unirouter.prd5.xinsight.snapshot.v2',
        'unirouter.completed-execution.v1',
        'unirouter.execution-evidence.bundle.v2'
      )
    );

  GET DIAGNOSTICS affected_rows = ROW_COUNT;
  IF affected_rows <> 1 THEN
    RAISE EXCEPTION 'fixed UniRouter source identity conflicts with execution-evidence V2 registry state';
  END IF;
END $$;

DO $$
DECLARE
  affected_rows integer;
BEGIN
  INSERT INTO adapter_manifests (
    id, adapter_id, adapter_version, source_type, fidelity_level, ingestion_modes,
    direct_field_groups, derived_field_groups, known_capture_gaps,
    payload_behavior, idempotency_strategy, cursor_strategy,
    declared_event_types, manifest_json, active
  ) VALUES (
    '22222222-2222-4222-8222-222222222225',
    'adapter-unirouter:22222222-2222-4222-8222-222222222225',
    'unirouter.execution-evidence.bundle.v2',
    'unirouter',
    'F2',
    ARRAY['db_import']::text[],
    ARRAY['correlation','identity','capability','decision','usage','outcome']::text[],
    ARRAY['payload_policy']::text[],
    ARRAY[
      'not_available_from_source',
      'provider_not_returned',
      'redacted',
      'sampled_out',
      'retention_expired',
      'not_instrumented',
      'permission_denied',
      'decision_not_emitted'
    ]::text[],
    'metadata_only_default',
    'source_ref_event_type_source_event_id',
    'source_ref',
    ARRAY[]::text[],
    '{}'::jsonb,
    true
  )
  ON CONFLICT (id) DO UPDATE SET
    active = true
  WHERE adapter_manifests.adapter_id = EXCLUDED.adapter_id
    AND adapter_manifests.adapter_version = EXCLUDED.adapter_version
    AND adapter_manifests.source_type = EXCLUDED.source_type
    AND adapter_manifests.fidelity_level = EXCLUDED.fidelity_level
    AND adapter_manifests.ingestion_modes = EXCLUDED.ingestion_modes
    AND adapter_manifests.direct_field_groups = EXCLUDED.direct_field_groups
    AND adapter_manifests.derived_field_groups = EXCLUDED.derived_field_groups
    AND adapter_manifests.known_capture_gaps = EXCLUDED.known_capture_gaps
    AND adapter_manifests.payload_behavior = EXCLUDED.payload_behavior
    AND adapter_manifests.idempotency_strategy = EXCLUDED.idempotency_strategy
    AND adapter_manifests.cursor_strategy IS NOT DISTINCT FROM EXCLUDED.cursor_strategy
    AND adapter_manifests.declared_event_types = EXCLUDED.declared_event_types
    AND adapter_manifests.manifest_json = EXCLUDED.manifest_json;

  GET DIAGNOSTICS affected_rows = ROW_COUNT;
  IF affected_rows <> 1 THEN
    RAISE EXCEPTION 'fixed UniRouter execution-evidence V2 adapter identity conflicts with existing registry state';
  END IF;
END $$;

COMMIT;
