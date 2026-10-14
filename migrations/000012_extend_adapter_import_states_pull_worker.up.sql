ALTER TABLE adapter_import_states
  ADD COLUMN activation_source_instance text,
  ADD COLUMN activation_cursor text,
  ADD COLUMN activation_high_watermark text,
  ADD COLUMN activated_at timestamptz,
  ADD COLUMN authorization_ref text,
  ADD COLUMN high_watermark text,
  ADD COLUMN worker_status text NOT NULL DEFAULT 'inactive',
  ADD COLUMN pages_committed bigint NOT NULL DEFAULT 0,
  ADD COLUMN items_received bigint NOT NULL DEFAULT 0,
  ADD COLUMN items_committed bigint NOT NULL DEFAULT 0,
  ADD COLUMN duplicate_noop bigint NOT NULL DEFAULT 0,
  ADD COLUMN consecutive_failures integer NOT NULL DEFAULT 0,
  ADD COLUMN last_poll_failed_at timestamptz;

ALTER TABLE adapter_import_states
  ADD CONSTRAINT adapter_import_states_worker_status_check CHECK (
    worker_status IN ('inactive', 'idle', 'polling', 'backoff', 'stopping', 'error')
  ),
  ADD CONSTRAINT adapter_import_states_pull_counters_check CHECK (
    pages_committed >= 0
    AND items_received >= 0
    AND items_committed >= 0
    AND duplicate_noop >= 0
    AND consecutive_failures >= 0
    AND items_committed + duplicate_noop = items_received
  ),
  ADD CONSTRAINT adapter_import_states_completed_execution_v1_check CHECK (
    cursor_key <> 'unirouter.completed-execution.v1'
    OR (
      activation_source_instance IS NOT NULL
      AND octet_length(activation_source_instance) BETWEEN 1 AND 128
      AND activation_cursor IS NOT NULL
      AND octet_length(activation_cursor) BETWEEN 1 AND 512
      AND activation_high_watermark IS NOT NULL
      AND octet_length(activation_high_watermark) <= 512
      AND activated_at IS NOT NULL
      AND authorization_ref IS NOT NULL
      AND octet_length(authorization_ref) BETWEEN 1 AND 256
      AND cursor_value IS NOT NULL
      AND octet_length(cursor_value) BETWEEN 1 AND 512
      AND high_watermark IS NOT NULL
      AND octet_length(high_watermark) <= 512
    )
  );

CREATE INDEX idx_adapter_import_states_worker_status
  ON adapter_import_states (worker_status, updated_at, id);
