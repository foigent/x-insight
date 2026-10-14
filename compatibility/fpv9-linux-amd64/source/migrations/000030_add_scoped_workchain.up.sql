CREATE TABLE investigation_case_scope_labels (
  case_id text PRIMARY KEY REFERENCES investigation_cases(id) ON DELETE RESTRICT,
  schema_version text NOT NULL CHECK (schema_version = 'xinsight.case-scope.v1'),
  case_version bigint NOT NULL CHECK (case_version > 0),
  requirements jsonb NOT NULL CHECK (jsonb_typeof(requirements) = 'array' AND jsonb_array_length(requirements) > 0),
  content_digest text NOT NULL CHECK (content_digest ~ '^sha256:[a-f0-9]{64}$'),
  labeled_at timestamptz NOT NULL
);

-- PostgreSQL already obtains the statement's table lock before firing this
-- trigger, including AccessExclusive for TRUNCATE. Readers take AccessShare
-- first, so no reader owns the advisory lock while waiting for that table.
CREATE FUNCTION synchronize_workchain_grants() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
  PERFORM pg_catalog.pg_advisory_xact_lock(6361703472894002481);
  RETURN NULL;
END
$$;
REVOKE ALL ON FUNCTION synchronize_workchain_grants() FROM PUBLIC;
CREATE TRIGGER workchain_grants_synchronize
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON workbench_access_grants
FOR EACH STATEMENT EXECUTE FUNCTION synchronize_workchain_grants();
ALTER TABLE workbench_access_grants ENABLE ALWAYS TRIGGER workchain_grants_synchronize;

COMMENT ON TABLE investigation_case_scope_labels IS 'Data requirements and content/version binding for explicitly scoped cases; grants remain the authorization authority';
