-- Internal resource-domain qualification records are separate from A, B and
-- economic source contracts. No public producer/import route is enabled here.
ALTER TABLE workbench_access_grants
  ADD COLUMN allow_resource boolean NOT NULL DEFAULT false;
ALTER TABLE workbench_access_grants
  DROP CONSTRAINT workbench_access_grants_has_capability_check;
ALTER TABLE workbench_access_grants
  ADD CONSTRAINT workbench_access_grants_has_capability_check CHECK (
    allow_summary OR allow_details OR allow_coverage OR allow_preferences
    OR allow_source_runtime OR allow_source_attempt OR allow_source_capability
    OR allow_source_policy OR allow_source_destination OR allow_resource
  );

CREATE FUNCTION advance_resource_grant_revision() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
  IF NEW.allow_resource IS DISTINCT FROM OLD.allow_resource AND NEW.revision<=OLD.revision THEN
    NEW.revision=OLD.revision+1;
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION advance_resource_grant_revision() FROM PUBLIC;
CREATE TRIGGER workbench_resource_grant_revision BEFORE UPDATE ON workbench_access_grants
  FOR EACH ROW EXECUTE FUNCTION advance_resource_grant_revision();
ALTER TABLE workbench_access_grants ENABLE ALWAYS TRIGGER workbench_resource_grant_revision;

CREATE TABLE resource_access_records (
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  record_ref text NOT NULL,
  revision bigint NOT NULL CHECK(revision BETWEEN 1 AND 1000000),
  previous_hash text NOT NULL DEFAULT '',
  record_hash text NOT NULL CHECK(record_hash ~ '^sha256:[a-f0-9]{64}$'),
  source_instance text NOT NULL,
  org_id text NOT NULL,
  project_id text NOT NULL,
  target_source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  target_source_instance text NOT NULL,
  target_org_id text NOT NULL,
  target_project_id text NOT NULL,
  request_id text NOT NULL,
  occurred_at timestamptz NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  record_bytes bytea NOT NULL CHECK(octet_length(record_bytes) BETWEEN 1 AND 16384),
  record_json jsonb NOT NULL CHECK(record_json=convert_from(record_bytes,'UTF8')::jsonb),
  PRIMARY KEY(source_system_id,source_instance,org_id,project_id,record_ref,revision),
  CHECK((revision=1 AND previous_hash='') OR (revision>1 AND previous_hash ~ '^sha256:[a-f0-9]{64}$')),
  CHECK(record_hash='sha256:'||encode(sha256(record_bytes),'hex')),
  CHECK((record_json->>'schema_version'='xinsight.resource-access.internal.v1' AND record_json->>'synthetic'='true') IS TRUE),
  CHECK((record_json->>'record_ref'=record_ref AND record_json->>'revision'=revision::text
    AND COALESCE(record_json->>'previous_hash','')=previous_hash
    AND record_json#>>'{reporter,source_system_id}'=source_system_id::text
    AND record_json#>>'{reporter,source_instance}'=source_instance
    AND record_json#>>'{reporter,org_id}'=org_id AND record_json#>>'{reporter,project_id}'=project_id
    AND record_json#>>'{target,source_system_id}'=target_source_system_id::text
    AND record_json#>>'{target,source_instance}'=target_source_instance
    AND record_json#>>'{target,org_id}'=target_org_id AND record_json#>>'{target,project_id}'=target_project_id
    AND record_json->>'request_id'=request_id) IS TRUE)
);
CREATE INDEX resource_access_records_target ON resource_access_records
  (target_source_system_id,target_source_instance,target_org_id,target_project_id,request_id,received_at);
CREATE TRIGGER resource_access_records_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON resource_access_records
  FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
ALTER TABLE resource_access_records ENABLE ALWAYS TRIGGER resource_access_records_immutable;

CREATE TABLE runtime_reading_resource_members (
  reading_id text NOT NULL,
  request_ref text NOT NULL,
  source_system_id uuid NOT NULL,
  source_instance text NOT NULL,
  org_id text NOT NULL,
  project_id text NOT NULL,
  record_ref text NOT NULL,
  revision bigint NOT NULL,
  chain_state text NOT NULL CHECK(chain_state IN ('complete','missing_predecessor')),
  PRIMARY KEY(reading_id,source_system_id,source_instance,org_id,project_id,record_ref),
  FOREIGN KEY(reading_id,request_ref) REFERENCES runtime_reading_requests(reading_id,request_ref) ON DELETE CASCADE,
  FOREIGN KEY(source_system_id,source_instance,org_id,project_id,record_ref,revision)
    REFERENCES resource_access_records(source_system_id,source_instance,org_id,project_id,record_ref,revision) ON DELETE RESTRICT
);
CREATE INDEX runtime_reading_resource_members_request ON runtime_reading_resource_members(reading_id,request_ref,source_system_id,record_ref);
