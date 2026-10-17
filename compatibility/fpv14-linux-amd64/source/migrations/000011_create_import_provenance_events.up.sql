CREATE TABLE IF NOT EXISTS import_provenance_events (
  import_provenance_id uuid NOT NULL REFERENCES import_provenance(id) ON DELETE CASCADE,
  evidence_event_id uuid NOT NULL REFERENCES evidence_events(id) ON DELETE CASCADE,
  execution_id text,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (import_provenance_id, evidence_event_id),
  CONSTRAINT import_provenance_events_execution_id_check CHECK (
    execution_id IS NULL OR execution_id ~ '^xe1_[a-f0-9]{64}$'
  )
);

INSERT INTO import_provenance_events (
  import_provenance_id,
  evidence_event_id,
  execution_id,
  created_at
)
SELECT
  import_provenance_id,
  id,
  execution_id,
  created_at
FROM evidence_events
WHERE import_provenance_id IS NOT NULL
ON CONFLICT (import_provenance_id, evidence_event_id) DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_import_provenance_events_execution
  ON import_provenance_events (execution_id, created_at, import_provenance_id)
  WHERE execution_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_import_provenance_events_event
  ON import_provenance_events (evidence_event_id, created_at, import_provenance_id);
