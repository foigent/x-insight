CREATE TABLE IF NOT EXISTS service_identities (
  id text PRIMARY KEY,
  display_name text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  permissions text[] NOT NULL,
  created_by_subject text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  expires_at timestamptz,
  revoked_at timestamptz,
  revoke_reason text,
  last_used_at timestamptz,
  CONSTRAINT service_identities_id_check CHECK (id ~ '^xsi_[A-Za-z0-9_-]{22}$'),
  CONSTRAINT service_identities_name_check CHECK (octet_length(display_name) BETWEEN 1 AND 120),
  CONSTRAINT service_identities_status_check CHECK (status IN ('active', 'revoked', 'expired')),
  CONSTRAINT service_identities_permissions_check CHECK (
    cardinality(permissions) BETWEEN 1 AND 4
    AND permissions <@ ARRAY['evidence.read', 'evidence.cite']::text[]
  ),
  CONSTRAINT service_identities_creator_check CHECK (
    created_by_subject = 'root'
    OR created_by_subject ~ '^xs_[a-z0-9]{16,64}$'
  ),
  CONSTRAINT service_identities_expiry_check CHECK (
    expires_at IS NULL OR expires_at > created_at
  ),
  CONSTRAINT service_identities_revoke_check CHECK (
    (status = 'revoked' AND revoked_at IS NOT NULL AND octet_length(revoke_reason) BETWEEN 1 AND 512)
    OR (status <> 'revoked' AND revoked_at IS NULL AND revoke_reason IS NULL)
  )
);

CREATE INDEX IF NOT EXISTS idx_service_identities_status
  ON service_identities (status, expires_at, id);

CREATE TABLE IF NOT EXISTS service_identity_tokens (
  token_digest text PRIMARY KEY,
  service_identity_id text NOT NULL REFERENCES service_identities(id) ON DELETE RESTRICT,
  token_prefix text NOT NULL,
  created_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz,
  last_used_at timestamptz,
  CONSTRAINT service_identity_tokens_digest_check CHECK (token_digest ~ '^sha256:[a-f0-9]{64}$'),
  CONSTRAINT service_identity_tokens_prefix_check CHECK (token_prefix ~ '^xib_[A-Za-z0-9_-]{8}$'),
  CONSTRAINT service_identity_tokens_expiry_check CHECK (expires_at > created_at),
  CONSTRAINT service_identity_tokens_revoke_check CHECK (
    revoked_at IS NULL OR revoked_at >= created_at
  )
);

CREATE INDEX IF NOT EXISTS idx_service_identity_tokens_identity
  ON service_identity_tokens (service_identity_id, expires_at DESC, token_prefix);

CREATE TABLE IF NOT EXISTS access_audit_records (
  id text PRIMARY KEY,
  occurred_at timestamptz NOT NULL,
  actor_kind text NOT NULL,
  actor_ref text NOT NULL,
  action text NOT NULL,
  resource_kind text NOT NULL,
  resource_ref text NOT NULL,
  result text NOT NULL,
  correlation_id text NOT NULL,
  details jsonb NOT NULL DEFAULT '{}'::jsonb,
  CONSTRAINT access_audit_records_id_check CHECK (id ~ '^xaa_[A-Za-z0-9_-]{22}$'),
  CONSTRAINT access_audit_records_actor_kind_check CHECK (actor_kind IN ('human', 'service', 'root', 'system')),
  CONSTRAINT access_audit_records_action_check CHECK (octet_length(action) BETWEEN 1 AND 160),
  CONSTRAINT access_audit_records_resource_check CHECK (
    octet_length(resource_kind) BETWEEN 1 AND 80
    AND octet_length(resource_ref) BETWEEN 1 AND 256
  ),
  CONSTRAINT access_audit_records_result_check CHECK (result IN ('accepted', 'denied', 'revoked', 'expired', 'error')),
  CONSTRAINT access_audit_records_correlation_check CHECK (correlation_id ~ '^xa_[A-Za-z0-9_-]{8,64}$'),
  CONSTRAINT access_audit_records_details_check CHECK (
    jsonb_typeof(details) = 'object' AND octet_length(details::text) <= 8192
  )
);

CREATE INDEX IF NOT EXISTS idx_access_audit_records_time
  ON access_audit_records (occurred_at DESC, id);

CREATE INDEX IF NOT EXISTS idx_access_audit_records_actor
  ON access_audit_records (actor_kind, actor_ref, occurred_at DESC);
