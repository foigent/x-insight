CREATE TABLE IF NOT EXISTS adapter_manifests (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  adapter_id text NOT NULL,
  adapter_version text NOT NULL,
  source_type text NOT NULL,
  supported_source_versions text[] NOT NULL DEFAULT ARRAY[]::text[],
  fidelity_level text NOT NULL,
  ingestion_modes text[] NOT NULL DEFAULT ARRAY[]::text[],
  direct_field_groups text[] NOT NULL DEFAULT ARRAY[]::text[],
  derived_field_groups text[] NOT NULL DEFAULT ARRAY[]::text[],
  known_capture_gaps text[] NOT NULL DEFAULT ARRAY[]::text[],
  payload_behavior text NOT NULL,
  idempotency_strategy text NOT NULL,
  cursor_strategy text,
  declared_event_types text[] NOT NULL DEFAULT ARRAY[]::text[],
  manifest_json jsonb NOT NULL DEFAULT '{}'::jsonb,
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT adapter_manifests_fidelity_check CHECK (fidelity_level IN ('F0', 'F1', 'F2', 'F3')),
  CONSTRAINT adapter_manifests_unique_version UNIQUE (adapter_id, adapter_version)
);

CREATE INDEX IF NOT EXISTS idx_adapter_manifests_source_type ON adapter_manifests (source_type);
CREATE INDEX IF NOT EXISTS idx_adapter_manifests_active ON adapter_manifests (active);
