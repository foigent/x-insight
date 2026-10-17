CREATE TABLE IF NOT EXISTS evidence_refs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  evidence_event_id uuid NOT NULL REFERENCES evidence_events(id) ON DELETE CASCADE,
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE CASCADE,
  adapter_manifest_id uuid NOT NULL REFERENCES adapter_manifests(id) ON DELETE CASCADE,
  source_ref text NOT NULL,
  source_entity text,
  source_event_id text,
  source_event_hash text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_evidence_refs_event_id ON evidence_refs (evidence_event_id);
CREATE INDEX IF NOT EXISTS idx_evidence_refs_source_ref ON evidence_refs (source_ref);
