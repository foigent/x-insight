CREATE TABLE IF NOT EXISTS import_provenance (
  id uuid PRIMARY KEY,
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE CASCADE,
  adapter_manifest_id uuid NOT NULL REFERENCES adapter_manifests(id) ON DELETE CASCADE,
  integration_mode text NOT NULL,
  proof_class text NOT NULL,
  assurance_mode text NOT NULL,
  source_artifact_sha256 text,
  approval_manifest_sha256 text,
  exchange_id text,
  source_generated_at timestamptz,
  source_terminal_at timestamptz,
  source_revision text,
  imported_revision text,
  source_watermark text,
  import_watermark text,
  timeline_complete boolean NOT NULL DEFAULT false,
  imported_at timestamptz NOT NULL DEFAULT now(),
  search_visible_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT import_provenance_integration_mode_check CHECK (
    integration_mode IN ('sidecar', 'completed_execution_pull', 'synthetic_fixture')
  ),
  CONSTRAINT import_provenance_proof_class_check CHECK (
    proof_class IN ('operator_approved_integration', 'synthetic_fixture')
  ),
  CONSTRAINT import_provenance_assurance_mode_check CHECK (
    assurance_mode IN ('operator_attested_sidecar', 'contracted_pull', 'synthetic_fixture')
  ),
  CONSTRAINT import_provenance_proof_requirements_check CHECK (
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
  ),
  CONSTRAINT import_provenance_artifact_hash_check CHECK (
    source_artifact_sha256 IS NULL OR source_artifact_sha256 ~ '^sha256:[a-f0-9]{64}$'
  ),
  CONSTRAINT import_provenance_manifest_hash_check CHECK (
    approval_manifest_sha256 IS NULL OR approval_manifest_sha256 ~ '^sha256:[a-f0-9]{64}$'
  )
);

ALTER TABLE evidence_events
  ADD COLUMN IF NOT EXISTS import_provenance_id uuid REFERENCES import_provenance(id) ON DELETE SET NULL;

ALTER TABLE evidence_events
  ADD COLUMN IF NOT EXISTS execution_id text;

UPDATE evidence_events
SET execution_id = 'xe1_' || encode(digest(
  convert_to('xinsight-execution-v1', 'UTF8') || decode('00', 'hex') ||
  convert_to(source_system_id::text, 'UTF8') || decode('00', 'hex') ||
  convert_to(request_id, 'UTF8'),
  'sha256'
), 'hex')
WHERE request_id IS NOT NULL
  AND request_id <> ''
  AND execution_id IS NULL;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'evidence_events_execution_id_check'
      AND conrelid = 'evidence_events'::regclass
  ) THEN
    ALTER TABLE evidence_events ADD CONSTRAINT evidence_events_execution_id_check CHECK (
      (
        (request_id IS NULL OR request_id = '')
        AND execution_id IS NULL
      ) OR (
        request_id IS NOT NULL
        AND request_id <> ''
        AND execution_id ~ '^xe1_[a-f0-9]{64}$'
      )
    );
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_import_provenance_source_imported
  ON import_provenance (source_system_id, imported_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_evidence_events_import_provenance
  ON evidence_events (import_provenance_id)
  WHERE import_provenance_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_evidence_events_execution_id_snapshot
  ON evidence_events (
    execution_id,
    ingested_at,
    (COALESCE(occurred_at, observed_at, ingested_at)),
    id
  )
  WHERE execution_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_evidence_events_execution_snapshot
  ON evidence_events (
    source_system_id,
    request_id,
    ingested_at,
    (COALESCE(occurred_at, observed_at, ingested_at)),
    id
  )
  WHERE request_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_evidence_events_search_effective
  ON evidence_events (
    (COALESCE(occurred_at, observed_at, ingested_at)),
    ingested_at,
    source_system_id,
    request_id
  )
  WHERE request_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_capture_gaps_execution_snapshot
  ON capture_gaps (source_system_id, request_id, created_at, id)
  WHERE request_id IS NOT NULL;
