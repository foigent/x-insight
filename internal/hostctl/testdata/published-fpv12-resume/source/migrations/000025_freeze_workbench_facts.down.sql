-- No in-place downgrade can recreate schema24 physical/schema-contract identity.
-- Restore the exact schema24 backup into a NEW database with its matching binary.
DO $$ BEGIN
  RAISE EXCEPTION 'schema25 downgrade requires exact schema24 backup restore to a new database';
END $$;
