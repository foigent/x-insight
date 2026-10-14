CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS source_systems (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  source_key text NOT NULL UNIQUE,
  source_type text NOT NULL,
  display_name text NOT NULL,
  source_version text,
  deployment_ref text,
  owner_ref text,
  status text NOT NULL DEFAULT 'active',
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT source_systems_status_check CHECK (status IN ('active', 'inactive', 'error'))
);

CREATE INDEX IF NOT EXISTS idx_source_systems_source_type ON source_systems (source_type);
CREATE INDEX IF NOT EXISTS idx_source_systems_status ON source_systems (status);
