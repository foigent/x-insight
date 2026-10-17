CREATE TABLE IF NOT EXISTS connector_import_operation_bindings (
  connector_id text NOT NULL,
  operation_id text NOT NULL,
  request_binding_sha256 text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (connector_id, operation_id),
  CONSTRAINT connector_import_operation_bindings_identity_unique UNIQUE (
    connector_id,
    operation_id,
    request_binding_sha256
  ),
  CONSTRAINT connector_import_operation_bindings_connector_check CHECK (
    char_length(connector_id) <= 160
    AND connector_id ~ '^[a-z][a-z0-9]*(\.[a-z][a-z0-9_-]*)+$'
  ),
  CONSTRAINT connector_import_operation_bindings_operation_check CHECK (
    char_length(operation_id) BETWEEN 1 AND 128
    AND operation_id !~ '[[:cntrl:]]'
  ),
  CONSTRAINT connector_import_operation_bindings_request_check CHECK (
    request_binding_sha256 ~ '^sha256:[a-f0-9]{64}$'
  )
);

CREATE TABLE IF NOT EXISTS connector_operational_receipts (
  receipt_version text NOT NULL,
  operation_id text NOT NULL,
  connector_id text NOT NULL,
  request_binding_sha256 text NOT NULL,
  producer_instance text NOT NULL,
  delivery_mode text NOT NULL,
  operation text NOT NULL,
  status text NOT NULL,
  received bigint NOT NULL,
  inserted bigint NOT NULL,
  duplicate_noop bigint NOT NULL,
  conflict bigint NOT NULL,
  rejected bigint NOT NULL,
  delivery_state jsonb NOT NULL,
  observed_at timestamptz NOT NULL,
  receipt_sha256 text NOT NULL,
  canonical_receipt bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (connector_id, operation_id),
  CONSTRAINT connector_operational_receipts_operation_binding_fk FOREIGN KEY (
    connector_id,
    operation_id,
    request_binding_sha256
  ) REFERENCES connector_import_operation_bindings (
    connector_id,
    operation_id,
    request_binding_sha256
  ) ON DELETE RESTRICT,
  CONSTRAINT connector_operational_receipts_version_check CHECK (
    receipt_version = 'foigent.operational-receipt.v2'
  ),
  CONSTRAINT connector_operational_receipts_operation_id_check CHECK (
    char_length(operation_id) BETWEEN 1 AND 128
    AND operation_id !~ '[[:cntrl:]]'
  ),
  CONSTRAINT connector_operational_receipts_connector_id_check CHECK (
    char_length(connector_id) <= 160
    AND connector_id ~ '^[a-z][a-z0-9]*(\.[a-z][a-z0-9_-]*)+$'
  ),
  CONSTRAINT connector_operational_receipts_request_binding_check CHECK (
    request_binding_sha256 ~ '^sha256:[a-f0-9]{64}$'
  ),
  CONSTRAINT connector_operational_receipts_producer_check CHECK (
    char_length(producer_instance) BETWEEN 1 AND 128
    AND producer_instance !~ '[[:cntrl:]]'
  ),
  CONSTRAINT connector_operational_receipts_mode_check CHECK (
    delivery_mode = 'bounded_offline'
  ),
  CONSTRAINT connector_operational_receipts_operation_check CHECK (
    operation IN ('import_bundle', 'commit_batch')
  ),
  CONSTRAINT connector_operational_receipts_status_check CHECK (
    status = 'completed'
  ),
  CONSTRAINT connector_operational_receipts_counts_check CHECK (
    received BETWEEN 0 AND 9007199254740991
    AND inserted BETWEEN 0 AND 9007199254740991
    AND duplicate_noop BETWEEN 0 AND 9007199254740991
    AND conflict BETWEEN 0 AND 9007199254740991
    AND rejected BETWEEN 0 AND 9007199254740991
    AND received = inserted + duplicate_noop
    AND conflict = 0
    AND rejected = 0
  ),
  CONSTRAINT connector_operational_receipts_delivery_state_check CHECK (
    jsonb_typeof(delivery_state) = 'object'
    AND delivery_state ?& ARRAY['mode', 'bundle_id', 'bundle_sha256']
    AND delivery_state - ARRAY['mode', 'bundle_id', 'bundle_sha256'] = '{}'::jsonb
    AND delivery_state ->> 'mode' = 'bounded_offline'
    AND char_length(delivery_state ->> 'bundle_id') BETWEEN 1 AND 128
    AND delivery_state ->> 'bundle_id' !~ '[[:cntrl:]]'
    AND delivery_state ->> 'bundle_sha256' ~ '^sha256:[a-f0-9]{64}$'
  ),
  CONSTRAINT connector_operational_receipts_digest_check CHECK (
    receipt_sha256 ~ '^sha256:[a-f0-9]{64}$'
  ),
  CONSTRAINT connector_operational_receipts_bytes_check CHECK (
    octet_length(canonical_receipt) BETWEEN 1 AND 32768
  )
);

CREATE INDEX IF NOT EXISTS idx_connector_operational_receipts_observed
  ON connector_operational_receipts (observed_at DESC, connector_id, operation_id);
