CREATE TABLE IF NOT EXISTS evidence_events (
  id uuid PRIMARY KEY,
  schema_version text NOT NULL,
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE CASCADE,
  adapter_manifest_id uuid NOT NULL REFERENCES adapter_manifests(id) ON DELETE CASCADE,
  source_type text NOT NULL,
  source_version text,
  source_event_id text,
  source_event_type text,
  source_ref text NOT NULL,
  idempotency_key text NOT NULL,
  import_cursor text,
  event_type text NOT NULL,
  stage text NOT NULL,
  severity text NOT NULL,
  capture_mode text NOT NULL,
  occurred_at timestamptz,
  observed_at timestamptz,
  ingested_at timestamptz NOT NULL,
  request_id text,
  trace_id text,
  span_id text,
  org_ref text,
  project_ref text,
  payload_capture_mode text NOT NULL,
  event_hash text NOT NULL,
  source_event_hash text,
  correlation jsonb NOT NULL DEFAULT '{}'::jsonb,
  identity jsonb NOT NULL DEFAULT '{}'::jsonb,
  capability jsonb NOT NULL DEFAULT '{}'::jsonb,
  decision jsonb NOT NULL DEFAULT '{}'::jsonb,
  usage_data jsonb NOT NULL DEFAULT '{}'::jsonb,
  outcome jsonb NOT NULL DEFAULT '{}'::jsonb,
  payload_policy jsonb NOT NULL DEFAULT '{}'::jsonb,
  field_provenance jsonb NOT NULL DEFAULT '{}'::jsonb,
  raw_source_excerpt jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT evidence_events_severity_check CHECK (severity IN ('info', 'warning', 'error')),
  CONSTRAINT evidence_events_payload_capture_mode_check CHECK (payload_capture_mode IN ('none', 'metadata_only', 'hash_only', 'redacted', 'reference_only', 'full_content')),
  CONSTRAINT evidence_events_event_hash_check CHECK (event_hash ~ '^sha256:[a-f0-9]{64}$'),
  CONSTRAINT evidence_events_unique_idempotency UNIQUE (source_system_id, adapter_manifest_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_evidence_events_request_id ON evidence_events (request_id);
CREATE INDEX IF NOT EXISTS idx_evidence_events_trace_id ON evidence_events (trace_id);
CREATE INDEX IF NOT EXISTS idx_evidence_events_span_id ON evidence_events (span_id);
CREATE INDEX IF NOT EXISTS idx_evidence_events_org_ref ON evidence_events (org_ref);
CREATE INDEX IF NOT EXISTS idx_evidence_events_project_ref ON evidence_events (project_ref);
CREATE INDEX IF NOT EXISTS idx_evidence_events_payload_capture_mode ON evidence_events (payload_capture_mode);
CREATE INDEX IF NOT EXISTS idx_evidence_events_occurred_at ON evidence_events (occurred_at);
CREATE INDEX IF NOT EXISTS idx_evidence_events_stage ON evidence_events (stage);
CREATE INDEX IF NOT EXISTS idx_evidence_events_source_ref ON evidence_events (source_ref);
