DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM import_provenance
    WHERE integration_mode = 'connector_bounded_offline'
  ) THEN
    RAISE EXCEPTION 'cannot remove bounded offline provenance while connector_bounded_offline rows exist';
  END IF;
END $$;

ALTER TABLE import_provenance
  DROP CONSTRAINT IF EXISTS import_provenance_integration_mode_check,
  DROP CONSTRAINT IF EXISTS import_provenance_assurance_mode_check,
  DROP CONSTRAINT IF EXISTS import_provenance_proof_requirements_check;

ALTER TABLE import_provenance
  ADD CONSTRAINT import_provenance_integration_mode_check CHECK (
    integration_mode IN (
      'sidecar',
      'completed_execution_pull',
      'connector_pull',
      'synthetic_fixture'
    )
  ),
  ADD CONSTRAINT import_provenance_assurance_mode_check CHECK (
    assurance_mode IN (
      'operator_attested_sidecar',
      'contracted_pull',
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
      AND integration_mode IN (
        'completed_execution_pull',
        'connector_pull'
      )
      AND assurance_mode = 'contracted_pull'
      AND source_artifact_sha256 IS NULL
      AND source_payload_sha256 IS NOT NULL
      AND approval_manifest_sha256 IS NULL
      AND exchange_id IS NULL
      AND source_watermark IS NULL
      AND import_watermark IS NULL
    )
  );

DROP TABLE IF EXISTS discovery_hypotheses;
DROP TABLE IF EXISTS discovery_analysis_runs;
DROP TABLE IF EXISTS execution_version_dimensions;
DROP TABLE IF EXISTS investigation_alert_case_links;
DROP TABLE IF EXISTS investigation_alerts;
DROP TABLE IF EXISTS evaluation_feedback;
DROP TABLE IF EXISTS session_execution_links;
DROP TABLE IF EXISTS authoritative_sessions;
DROP TABLE IF EXISTS capture_gap_resolutions;
