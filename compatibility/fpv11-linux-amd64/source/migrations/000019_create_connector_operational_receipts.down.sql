DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM connector_operational_receipts
  ) OR EXISTS (
    SELECT 1
    FROM connector_import_operation_bindings
  ) THEN
    RAISE EXCEPTION 'cannot remove Connector import operation evidence while rows exist';
  END IF;
END $$;

DROP TABLE IF EXISTS connector_operational_receipts;
DROP TABLE IF EXISTS connector_import_operation_bindings;
