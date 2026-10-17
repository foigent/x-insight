DROP INDEX IF EXISTS idx_adapter_import_states_worker_status;

ALTER TABLE adapter_import_states
  DROP CONSTRAINT IF EXISTS adapter_import_states_completed_execution_v1_check,
  DROP CONSTRAINT IF EXISTS adapter_import_states_pull_counters_check,
  DROP CONSTRAINT IF EXISTS adapter_import_states_worker_status_check;

ALTER TABLE adapter_import_states
  DROP COLUMN IF EXISTS last_poll_failed_at,
  DROP COLUMN IF EXISTS consecutive_failures,
  DROP COLUMN IF EXISTS duplicate_noop,
  DROP COLUMN IF EXISTS items_committed,
  DROP COLUMN IF EXISTS items_received,
  DROP COLUMN IF EXISTS pages_committed,
  DROP COLUMN IF EXISTS worker_status,
  DROP COLUMN IF EXISTS high_watermark,
  DROP COLUMN IF EXISTS authorization_ref,
  DROP COLUMN IF EXISTS activated_at,
  DROP COLUMN IF EXISTS activation_high_watermark,
  DROP COLUMN IF EXISTS activation_cursor,
  DROP COLUMN IF EXISTS activation_source_instance;
