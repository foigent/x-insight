-- Responsibility history is retained. Downgrade through a verified pre-upgrade
-- backup using the supported restore workflow, never by dropping human records.
DO $$ BEGIN
  RAISE EXCEPTION 'Finding responsibility migration is forward-only; restore a verified pre-upgrade backup';
END $$;
