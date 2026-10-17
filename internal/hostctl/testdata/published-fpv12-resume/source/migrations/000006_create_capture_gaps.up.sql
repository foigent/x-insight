CREATE TABLE IF NOT EXISTS capture_gaps (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE CASCADE,
  adapter_manifest_id uuid NOT NULL REFERENCES adapter_manifests(id) ON DELETE CASCADE,
  evidence_event_id uuid REFERENCES evidence_events(id) ON DELETE SET NULL,
  request_id text,
  gap_type text NOT NULL,
  affected_claim text NOT NULL,
  gap_reason text NOT NULL,
  severity text NOT NULL DEFAULT 'warning',
  recommendation text,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT capture_gaps_severity_check CHECK (severity IN ('info', 'warning', 'error'))
);

CREATE INDEX IF NOT EXISTS idx_capture_gaps_request_id ON capture_gaps (request_id);
CREATE INDEX IF NOT EXISTS idx_capture_gaps_gap_type ON capture_gaps (gap_type);
CREATE INDEX IF NOT EXISTS idx_capture_gaps_severity ON capture_gaps (severity);
