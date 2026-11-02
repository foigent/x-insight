-- Account authority cannot fall back to an obsolete file or resurrect old
-- credentials. Use the supported complete-database recovery procedure instead.
DO $$ BEGIN RAISE EXCEPTION 'local_account_authority_requires_complete_recovery'; END $$;
