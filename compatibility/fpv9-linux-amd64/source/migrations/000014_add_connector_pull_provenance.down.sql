DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM import_provenance
    WHERE integration_mode = 'connector_pull'
  ) THEN
    RAISE EXCEPTION 'cannot remove connector pull provenance while connector_pull rows exist';
  END IF;
END $$;

ALTER TABLE import_provenance
  DROP CONSTRAINT IF EXISTS import_provenance_integration_mode_check,
  DROP CONSTRAINT IF EXISTS import_provenance_proof_requirements_check;

ALTER TABLE import_provenance
  ADD CONSTRAINT import_provenance_integration_mode_check CHECK (
    integration_mode IN (
      'sidecar',
      'completed_execution_pull',
      'synthetic_fixture'
    )
  ),
  ADD CONSTRAINT import_provenance_proof_requirements_check CHECK (
    (
      proof_class = 'synthetic_fixture'
      AND integration_mode = 'synthetic_fixture'
      AND assurance_mode = 'synthetic_fixture'
      AND source_artifact_sha256 IS NOT NULL
      AND source_payload_sha256 IS NULL
      AND approval_manifest_sha256 IS NULL
      AND exchange_id IS NULL
    ) OR (
      proof_class = 'operator_approved_integration'
      AND integration_mode = 'sidecar'
      AND assurance_mode = 'operator_attested_sidecar'
      AND source_artifact_sha256 IS NOT NULL
      AND source_payload_sha256 IS NULL
      AND approval_manifest_sha256 IS NOT NULL
      AND NULLIF(btrim(exchange_id), '') IS NOT NULL
    ) OR (
      proof_class = 'operator_approved_integration'
      AND integration_mode = 'completed_execution_pull'
      AND assurance_mode = 'contracted_pull'
      AND source_artifact_sha256 IS NULL
      AND source_payload_sha256 IS NOT NULL
      AND approval_manifest_sha256 IS NULL
      AND exchange_id IS NULL
      AND source_watermark IS NULL
      AND import_watermark IS NULL
    )
  );
