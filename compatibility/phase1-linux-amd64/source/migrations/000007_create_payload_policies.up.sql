CREATE TABLE IF NOT EXISTS payload_policies (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  source_system_id uuid REFERENCES source_systems(id) ON DELETE CASCADE,
  scope_type text NOT NULL,
  scope_ref text NOT NULL,
  payload_capture_mode text NOT NULL,
  retention_days integer,
  encryption_ref text,
  audit_read_required boolean NOT NULL DEFAULT true,
  redaction_profile_ref text,
  legal_hold boolean NOT NULL DEFAULT false,
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT payload_policies_capture_mode_check CHECK (payload_capture_mode IN ('none', 'metadata_only', 'hash_only', 'redacted', 'reference_only', 'full_content')),
  CONSTRAINT payload_policies_status_check CHECK (status IN ('active', 'inactive'))
);

CREATE INDEX IF NOT EXISTS idx_payload_policies_scope ON payload_policies (scope_type, scope_ref);
CREATE INDEX IF NOT EXISTS idx_payload_policies_capture_mode ON payload_policies (payload_capture_mode);
