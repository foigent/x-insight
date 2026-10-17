DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM import_provenance
    WHERE integration_mode = 'completed_execution_pull'
      OR source_payload_sha256 IS NOT NULL
  ) OR EXISTS (
    SELECT 1
    FROM adapter_import_states
    WHERE source_system_id = '11111111-1111-1111-1111-111111111111'
      AND adapter_manifest_id = '22222222-2222-4222-8222-222222222224'
  ) OR EXISTS (
    SELECT 1
    FROM evidence_events
    WHERE adapter_manifest_id = '22222222-2222-4222-8222-222222222224'
  ) THEN
    RAISE EXCEPTION 'cannot remove live pull support while completed-execution state or evidence exists';
  END IF;
END $$;

DELETE FROM adapter_manifests
WHERE id = '22222222-2222-4222-8222-222222222224'
  AND adapter_id = 'adapter-unirouter:22222222-2222-4222-8222-222222222224'
  AND adapter_version = 'unirouter.completed-execution.v1'
  AND source_type = 'unirouter'
  AND ingestion_modes = ARRAY['api_import']::text[]
  AND direct_field_groups = ARRAY['correlation','identity','capability','usage','outcome']::text[]
  AND derived_field_groups = ARRAY['payload_policy']::text[]
  AND declared_event_types = ARRAY[
    'billing.execution.cancelled',
    'billing.execution.settled',
    'gap.capture.detected'
  ]::text[];

UPDATE source_systems
SET source_version = CASE
  WHEN EXISTS (
    SELECT 1
    FROM evidence_events
    WHERE source_system_id = '11111111-1111-1111-1111-111111111111'
      AND source_version = 'unirouter.prd5.xinsight.snapshot.v2'
  ) THEN 'unirouter.prd5.xinsight.snapshot.v2'
  WHEN EXISTS (
    SELECT 1
    FROM evidence_events
    WHERE source_system_id = '11111111-1111-1111-1111-111111111111'
      AND source_version = 'unirouter.prd5.xinsight.snapshot.v1'
  ) THEN 'unirouter.prd5.xinsight.snapshot.v1'
  ELSE NULL
END
WHERE id = '11111111-1111-1111-1111-111111111111'
  AND source_type = 'unirouter'
  AND source_version = 'unirouter.completed-execution.v1';

ALTER TABLE import_provenance
  DROP CONSTRAINT IF EXISTS import_provenance_payload_hash_check,
  DROP CONSTRAINT IF EXISTS import_provenance_proof_requirements_check;

ALTER TABLE import_provenance
  ADD CONSTRAINT import_provenance_proof_requirements_check CHECK (
    (
      proof_class = 'synthetic_fixture'
      AND integration_mode = 'synthetic_fixture'
      AND assurance_mode = 'synthetic_fixture'
      AND source_artifact_sha256 IS NOT NULL
      AND approval_manifest_sha256 IS NULL
      AND exchange_id IS NULL
    ) OR (
      proof_class = 'operator_approved_integration'
      AND source_artifact_sha256 IS NOT NULL
      AND approval_manifest_sha256 IS NOT NULL
      AND NULLIF(btrim(exchange_id), '') IS NOT NULL
      AND (
        (integration_mode = 'sidecar' AND assurance_mode = 'operator_attested_sidecar')
        OR
        (integration_mode = 'completed_execution_pull' AND assurance_mode = 'contracted_pull')
      )
    )
  );

ALTER TABLE import_provenance
  DROP COLUMN IF EXISTS source_payload_sha256;
