CREATE TABLE IF NOT EXISTS adapter_import_states (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE CASCADE,
  adapter_manifest_id uuid NOT NULL REFERENCES adapter_manifests(id) ON DELETE CASCADE,
  cursor_key text NOT NULL,
  cursor_value text,
  last_import_started_at timestamptz,
  last_import_completed_at timestamptz,
  last_error text,
  status text NOT NULL DEFAULT 'idle',
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT adapter_import_states_status_check CHECK (status IN ('idle', 'running', 'error')),
  CONSTRAINT adapter_import_states_unique_cursor UNIQUE (source_system_id, adapter_manifest_id, cursor_key)
);

CREATE INDEX IF NOT EXISTS idx_adapter_import_states_status ON adapter_import_states (status);
