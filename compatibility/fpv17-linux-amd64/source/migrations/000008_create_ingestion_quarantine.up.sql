CREATE TABLE IF NOT EXISTS ingestion_quarantine (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  source_system_id uuid REFERENCES source_systems(id) ON DELETE SET NULL,
  adapter_manifest_id uuid REFERENCES adapter_manifests(id) ON DELETE SET NULL,
  source_ref text,
  idempotency_key text,
  reason text NOT NULL,
  status text NOT NULL DEFAULT 'open',
  event_hash text,
  safe_excerpt jsonb NOT NULL DEFAULT '{}'::jsonb,
  error_detail text,
  created_at timestamptz NOT NULL DEFAULT now(),
  reviewed_at timestamptz,
  CONSTRAINT ingestion_quarantine_status_check CHECK (status IN ('open', 'reviewed', 'ignored'))
);

CREATE INDEX IF NOT EXISTS idx_ingestion_quarantine_status ON ingestion_quarantine (status);
CREATE INDEX IF NOT EXISTS idx_ingestion_quarantine_reason ON ingestion_quarantine (reason);
CREATE INDEX IF NOT EXISTS idx_ingestion_quarantine_source_ref ON ingestion_quarantine (source_ref);
