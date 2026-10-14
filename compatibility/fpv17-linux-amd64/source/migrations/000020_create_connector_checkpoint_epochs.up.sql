ALTER TABLE adapter_import_states
  ADD COLUMN checkpoint_epoch bigint NOT NULL DEFAULT 1,
  ADD COLUMN checkpoint_epoch_operation_id text;

ALTER TABLE adapter_import_states
  ADD CONSTRAINT adapter_import_states_checkpoint_epoch_check CHECK (checkpoint_epoch > 0),
  ADD CONSTRAINT adapter_import_states_checkpoint_epoch_operation_check CHECK (
    checkpoint_epoch_operation_id IS NULL
    OR checkpoint_epoch_operation_id ~ '^[a-f0-9]{32}$'
  );

CREATE TABLE connector_checkpoint_epoch_operations (
  operation_id text PRIMARY KEY,
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  adapter_manifest_id uuid NOT NULL REFERENCES adapter_manifests(id) ON DELETE RESTRICT,
  cursor_key text NOT NULL,
  worker_unit text NOT NULL,
  status text NOT NULL,
  expected_state_id uuid NOT NULL,
  expected_epoch bigint NOT NULL,
  expected_state_sha256 text NOT NULL,
  previous_source_instance text NOT NULL,
  previous_cursor_sha256 text NOT NULL,
  previous_high_watermark_sha256 text NOT NULL,
  next_source_instance text NOT NULL,
  next_cursor text NOT NULL,
  next_high_watermark text NOT NULL,
  next_authorization_ref text NOT NULL,
  plan_sha256 text NOT NULL,
  prepared_at timestamptz NOT NULL,
  activated_at timestamptz,
  aborted_at timestamptz,
  CONSTRAINT connector_checkpoint_epoch_operations_id_check CHECK (
    operation_id ~ '^[a-f0-9]{32}$'
  ),
  CONSTRAINT connector_checkpoint_epoch_operations_status_check CHECK (
    status IN ('prepared', 'activated', 'aborted')
  ),
  CONSTRAINT connector_checkpoint_epoch_operations_worker_unit_check CHECK (
    worker_unit IN ('xinsight-feed.service', 'xinsight-litellm-feed.service')
  ),
  CONSTRAINT connector_checkpoint_epoch_operations_worker_binding_check CHECK (
    (worker_unit = 'xinsight-feed.service'
      AND cursor_key = 'unirouter.completed-execution.v1')
    OR (worker_unit = 'xinsight-litellm-feed.service'
      AND cursor_key = 'litellm.spend-logs.v1')
  ),
  CONSTRAINT connector_checkpoint_epoch_operations_epoch_check CHECK (expected_epoch > 0),
  CONSTRAINT connector_checkpoint_epoch_operations_hashes_check CHECK (
    expected_state_sha256 ~ '^sha256:[a-f0-9]{64}$'
    AND previous_cursor_sha256 ~ '^sha256:[a-f0-9]{64}$'
    AND previous_high_watermark_sha256 ~ '^sha256:[a-f0-9]{64}$'
    AND plan_sha256 ~ '^sha256:[a-f0-9]{64}$'
  ),
  CONSTRAINT connector_checkpoint_epoch_operations_values_check CHECK (
    octet_length(cursor_key) BETWEEN 1 AND 256
    AND octet_length(previous_source_instance) BETWEEN 1 AND 128
    AND octet_length(next_source_instance) BETWEEN 1 AND 128
    AND octet_length(next_cursor) BETWEEN 1 AND 524288
    AND octet_length(next_high_watermark) BETWEEN 1 AND 524288
    AND next_cursor = next_high_watermark
    AND octet_length(next_authorization_ref) BETWEEN 1 AND 256
  ),
  CONSTRAINT connector_checkpoint_epoch_operations_terminal_time_check CHECK (
    (status = 'prepared' AND activated_at IS NULL AND aborted_at IS NULL)
    OR (status = 'activated' AND activated_at IS NOT NULL AND aborted_at IS NULL)
    OR (status = 'aborted' AND activated_at IS NULL AND aborted_at IS NOT NULL)
  )
);

CREATE UNIQUE INDEX connector_checkpoint_epoch_operations_one_prepared
  ON connector_checkpoint_epoch_operations (
    source_system_id,
    adapter_manifest_id,
    cursor_key
  )
  WHERE status = 'prepared';

CREATE INDEX connector_checkpoint_epoch_operations_binding
  ON connector_checkpoint_epoch_operations (
    source_system_id,
    adapter_manifest_id,
    cursor_key,
    prepared_at DESC
  );

CREATE TABLE connector_checkpoint_epoch_history (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  operation_id text NOT NULL UNIQUE
    REFERENCES connector_checkpoint_epoch_operations(operation_id) ON DELETE RESTRICT,
  state_id uuid NOT NULL,
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  adapter_manifest_id uuid NOT NULL REFERENCES adapter_manifests(id) ON DELETE RESTRICT,
  cursor_key text NOT NULL,
  checkpoint_epoch bigint NOT NULL,
  source_instance text NOT NULL,
  activation_cursor text NOT NULL,
  activation_high_watermark text NOT NULL,
  activated_at timestamptz NOT NULL,
  authorization_ref text NOT NULL,
  final_cursor text NOT NULL,
  final_high_watermark text NOT NULL,
  worker_status text NOT NULL,
  last_import_started_at timestamptz,
  last_import_completed_at timestamptz,
  last_poll_failed_at timestamptz,
  last_error text,
  pages_committed bigint NOT NULL,
  items_received bigint NOT NULL,
  items_committed bigint NOT NULL,
  duplicate_noop bigint NOT NULL,
  consecutive_failures integer NOT NULL,
  state_created_at timestamptz NOT NULL,
  state_updated_at timestamptz NOT NULL,
  retired_at timestamptz NOT NULL,
  snapshot_sha256 text NOT NULL,
  CONSTRAINT connector_checkpoint_epoch_history_unique_epoch UNIQUE (
    source_system_id,
    adapter_manifest_id,
    cursor_key,
    checkpoint_epoch
  ),
  CONSTRAINT connector_checkpoint_epoch_history_epoch_check CHECK (checkpoint_epoch > 0),
  CONSTRAINT connector_checkpoint_epoch_history_snapshot_check CHECK (
    snapshot_sha256 ~ '^sha256:[a-f0-9]{64}$'
  ),
  CONSTRAINT connector_checkpoint_epoch_history_counters_check CHECK (
    pages_committed >= 0
    AND items_received >= 0
    AND items_committed >= 0
    AND duplicate_noop >= 0
    AND consecutive_failures >= 0
    AND items_committed + duplicate_noop = items_received
  )
);

CREATE FUNCTION reject_connector_checkpoint_epoch_history_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  RAISE EXCEPTION 'connector checkpoint epoch history is immutable';
END;
$$;

CREATE TRIGGER connector_checkpoint_epoch_history_immutable
BEFORE UPDATE OR DELETE ON connector_checkpoint_epoch_history
FOR EACH ROW EXECUTE FUNCTION reject_connector_checkpoint_epoch_history_mutation();

ALTER TABLE adapter_import_states
  ADD CONSTRAINT adapter_import_states_checkpoint_epoch_operation_fk
  FOREIGN KEY (checkpoint_epoch_operation_id)
  REFERENCES connector_checkpoint_epoch_operations(operation_id)
  ON DELETE RESTRICT;
