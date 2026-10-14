DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM investigation_case_scope_labels) THEN
    RAISE EXCEPTION 'scoped case labels must be retained; restore a separate schema29 backup or fix forward';
  END IF;
END $$;
DROP TRIGGER workchain_grants_synchronize ON workbench_access_grants;
DROP FUNCTION synchronize_workchain_grants();
DROP TABLE investigation_case_scope_labels;
