DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM connector_checkpoint_epoch_operations)
    OR EXISTS (SELECT 1 FROM connector_checkpoint_epoch_history)
    OR EXISTS (
      SELECT 1
      FROM adapter_import_states
      WHERE checkpoint_epoch <> 1
        OR checkpoint_epoch_operation_id IS NOT NULL
    ) THEN
    RAISE EXCEPTION 'cannot remove connector checkpoint epoch support while epoch evidence exists';
  END IF;
END $$;

ALTER TABLE adapter_import_states
  DROP CONSTRAINT IF EXISTS adapter_import_states_checkpoint_epoch_operation_fk,
  DROP CONSTRAINT IF EXISTS adapter_import_states_checkpoint_epoch_operation_check,
  DROP CONSTRAINT IF EXISTS adapter_import_states_checkpoint_epoch_check,
  DROP COLUMN IF EXISTS checkpoint_epoch_operation_id,
  DROP COLUMN IF EXISTS checkpoint_epoch;

DROP TRIGGER IF EXISTS connector_checkpoint_epoch_history_immutable
  ON connector_checkpoint_epoch_history;
DROP FUNCTION IF EXISTS reject_connector_checkpoint_epoch_history_mutation();
DROP TABLE connector_checkpoint_epoch_history;
DROP TABLE connector_checkpoint_epoch_operations;
