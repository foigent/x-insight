-- Explicit B grants default to denied. Existing A reading and money authority
-- remain unchanged; runtime observations have a separate identity and table.
ALTER TABLE workbench_access_grants
  ADD COLUMN allow_source_runtime boolean NOT NULL DEFAULT false,
  ADD COLUMN allow_source_attempt boolean NOT NULL DEFAULT false,
  ADD COLUMN allow_source_capability boolean NOT NULL DEFAULT false,
  ADD COLUMN allow_source_policy boolean NOT NULL DEFAULT false,
  ADD COLUMN allow_source_destination boolean NOT NULL DEFAULT false;

CREATE FUNCTION advance_observation_grant_revision() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
  IF ROW(NEW.allow_source_runtime,NEW.allow_source_attempt,NEW.allow_source_capability,NEW.allow_source_policy,NEW.allow_source_destination)
    IS DISTINCT FROM ROW(OLD.allow_source_runtime,OLD.allow_source_attempt,OLD.allow_source_capability,OLD.allow_source_policy,OLD.allow_source_destination)
    AND NEW.revision<=OLD.revision THEN NEW.revision=OLD.revision+1; END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION advance_observation_grant_revision() FROM PUBLIC;
CREATE TRIGGER workbench_observation_grant_revision BEFORE UPDATE ON workbench_access_grants
  FOR EACH ROW EXECUTE FUNCTION advance_observation_grant_revision();
ALTER TABLE workbench_access_grants ENABLE ALWAYS TRIGGER workbench_observation_grant_revision;

ALTER TABLE source_fact_streams DROP CONSTRAINT source_fact_streams_profile_check;
ALTER TABLE source_fact_streams ADD CONSTRAINT source_fact_streams_profile_check CHECK(profile IN (
  'unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1',
  'unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1',
  'unirouter.policy-observation.v1','unirouter.destination-observation.v1'));

ALTER TABLE source_fact_offline_authorizations DROP CONSTRAINT source_fact_offline_authorizations_profile_check;
ALTER TABLE source_fact_offline_authorizations ADD CONSTRAINT source_fact_offline_authorizations_profile_check CHECK(profile IN (
  'unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1',
  'unirouter.runtime-observation.v1','unirouter.attempt-observation.v1','unirouter.capability-snapshot.v1',
  'unirouter.policy-observation.v1','unirouter.destination-observation.v1'));

CREATE TABLE source_observation_documents (
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  source_instance text NOT NULL,
  org_id text NOT NULL,
  project_id text NOT NULL,
  request_id text NOT NULL,
  profile text NOT NULL CHECK(profile IN ('unirouter.runtime-observation.v1','unirouter.attempt-observation.v1',
    'unirouter.capability-snapshot.v1','unirouter.policy-observation.v1','unirouter.destination-observation.v1')),
  subject_kind text NOT NULL CHECK(subject_kind IN ('request','provider_attempt','media_job','media_attempt','policy')),
  subject_id text NOT NULL,
  observation_kind text NOT NULL,
  source_fact_id text NOT NULL CHECK(source_fact_id ~ '^uf1_[a-f0-9]{64}$'),
  revision bigint NOT NULL CHECK(revision>0),
  event_id text NOT NULL CHECK(event_id ~ '^uf1_[a-f0-9]{64}$'),
  previous_hash text NOT NULL DEFAULT '',
  source_hash text NOT NULL CHECK(source_hash ~ '^sha256:[a-f0-9]{64}$'),
  envelope_hash text NOT NULL CHECK(envelope_hash ~ '^sha256:[a-f0-9]{64}$'),
  source_bytes bytea NOT NULL CHECK(octet_length(source_bytes) BETWEEN 1 AND 65536),
  envelope_bytes bytea NOT NULL CHECK(octet_length(envelope_bytes) BETWEEN 1 AND 131072),
  source_json jsonb NOT NULL CHECK(source_json=convert_from(source_bytes,'UTF8')::jsonb),
  occurred_at timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL,
  received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(source_system_id,event_id),
  UNIQUE(source_system_id,source_fact_id,revision),
  CHECK((revision=1 AND previous_hash='') OR (revision>1 AND previous_hash ~ '^sha256:[0-9a-f]{64}$')),
  CHECK(source_hash='sha256:'||encode(sha256(source_bytes),'hex')),
  CHECK((source_json->>'schema_version'='unirouter.source-observation.v1') IS TRUE),
  CHECK((source_json->>'source_instance'=source_instance AND source_json->>'org_id'=org_id
    AND source_json->>'project_id'=project_id AND source_json->>'request_id'=request_id
    AND source_json->>'profile'=profile AND source_json->>'subject_kind'=subject_kind
    AND source_json->>'subject_id'=subject_id AND source_json->>'observation_kind'=observation_kind
    AND source_json->>'source_fact_id'=source_fact_id AND source_json->>'event_id'=event_id
    AND source_json->>'revision'=revision::text) IS TRUE)
);
CREATE INDEX source_observation_documents_scope ON source_observation_documents
  (source_system_id,project_id,occurred_at,source_fact_id,revision DESC);
CREATE INDEX source_observation_documents_request ON source_observation_documents
  (source_system_id,source_instance,org_id,project_id,request_id,profile,subject_kind,subject_id,observation_kind,revision DESC);
CREATE TRIGGER source_observation_documents_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON source_observation_documents
  FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
ALTER TABLE source_observation_documents ENABLE ALWAYS TRIGGER source_observation_documents_immutable;

-- Runtime membership is separate from the existing economic reading. B never
-- inserts a workbench_economic_fact or reinterprets an old reading.
CREATE TABLE runtime_readings (
  id text PRIMARY KEY CHECK(id ~ '^xrt_[A-Za-z0-9_-]{22}$'),
  owner_subject text NOT NULL,
  contract_version text NOT NULL,
  metric_version text NOT NULL,
  period_start timestamptz NOT NULL,
  period_end timestamptz NOT NULL,
  display_timezone text NOT NULL,
  cutoff timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  filters jsonb NOT NULL,
  authorization_revision text NOT NULL,
  interpretation_revision text NOT NULL,
  membership_sha256 text NOT NULL CHECK(membership_sha256 ~ '^sha256:[a-f0-9]{64}$'),
  known_entry_requests bigint NOT NULL DEFAULT 0 CHECK(known_entry_requests>=0),
  unknown_entry_requests bigint NOT NULL DEFAULT 0 CHECK(unknown_entry_requests>=0),
  observation_count bigint NOT NULL DEFAULT 0 CHECK(observation_count>=0),
  coverage jsonb NOT NULL,
  CHECK(period_start<period_end AND period_end<=cutoff AND expires_at>cutoff)
);
CREATE INDEX runtime_readings_owner ON runtime_readings(owner_subject,expires_at,id);

CREATE TABLE runtime_reading_requests (
  reading_id text NOT NULL REFERENCES runtime_readings(id) ON DELETE CASCADE,
  request_ref text NOT NULL CHECK(request_ref ~ '^xrr_[a-f0-9]{64}$'),
  ordinal bigint NOT NULL CHECK(ordinal>0),
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  source_instance text NOT NULL,
  org_id text NOT NULL,
  project_id text NOT NULL,
  request_id text NOT NULL,
  entry_at timestamptz,
  last_observed_at timestamptz NOT NULL,
  observation_count bigint NOT NULL CHECK(observation_count>0),
  PRIMARY KEY(reading_id,request_ref),
  UNIQUE(reading_id,ordinal),
  UNIQUE(reading_id,source_system_id,source_instance,org_id,project_id,request_id)
);
CREATE TABLE runtime_reading_members (
  reading_id text NOT NULL,
  request_ref text NOT NULL,
  source_system_id uuid NOT NULL,
  event_id text NOT NULL,
  chain_state text NOT NULL CHECK(chain_state IN ('complete','missing_predecessor')),
  PRIMARY KEY(reading_id,source_system_id,event_id),
  FOREIGN KEY(reading_id,request_ref) REFERENCES runtime_reading_requests(reading_id,request_ref) ON DELETE CASCADE,
  FOREIGN KEY(source_system_id,event_id) REFERENCES source_observation_documents(source_system_id,event_id) ON DELETE RESTRICT
);
CREATE INDEX runtime_reading_members_request ON runtime_reading_members(reading_id,request_ref,source_system_id,event_id);

-- Associations freeze in the same snapshot as the runtime request membership.
-- A source field grant is independent of B observation grants; money is linked,
-- never copied or recomputed here.
CREATE TABLE runtime_reading_source_members (
  reading_id text NOT NULL,
  request_ref text NOT NULL,
  source_system_id uuid NOT NULL,
  event_id text NOT NULL,
  chain_state text NOT NULL CHECK(chain_state IN ('complete','missing_predecessor')),
  PRIMARY KEY(reading_id,source_system_id,event_id),
  FOREIGN KEY(reading_id,request_ref) REFERENCES runtime_reading_requests(reading_id,request_ref) ON DELETE CASCADE,
  FOREIGN KEY(source_system_id,event_id) REFERENCES source_fact_documents(source_system_id,event_id) ON DELETE RESTRICT
);
CREATE INDEX runtime_reading_source_members_request ON runtime_reading_source_members(reading_id,request_ref,source_system_id,event_id);
CREATE TABLE runtime_reading_economic_members (
  reading_id text NOT NULL,
  request_ref text NOT NULL,
  economic_fact_id uuid NOT NULL REFERENCES workbench_economic_facts(id) ON DELETE RESTRICT,
  PRIMARY KEY(reading_id,economic_fact_id),
  UNIQUE(reading_id,request_ref),
  FOREIGN KEY(reading_id,request_ref) REFERENCES runtime_reading_requests(reading_id,request_ref) ON DELETE CASCADE
);

-- Replaceable lease inventory, separate from immutable events and their cursor.
-- Readers use the last completed enumeration; pages in progress stay hidden.
-- Each page retains its own observation time; enumeration is not simultaneous.
CREATE TABLE source_runtime_snapshots (
  stream_id uuid PRIMARY KEY REFERENCES source_fact_streams(id) ON DELETE RESTRICT,
  revision bigint NOT NULL CHECK(revision>0),
  generation bigint NOT NULL CHECK(generation>0),
  page_ordinal bigint NOT NULL CHECK(page_ordinal>0),
  cursor_value text NOT NULL DEFAULT '',
  visible_generation bigint,
  requires_attention boolean NOT NULL DEFAULT false,
  last_error text NOT NULL DEFAULT '',
  last_attempt_at timestamptz NOT NULL,
  last_success_at timestamptz,
  CHECK(visible_generation IS NULL OR (visible_generation>0 AND visible_generation<generation))
);
CREATE TABLE source_runtime_snapshot_pages (
  stream_id uuid NOT NULL REFERENCES source_runtime_snapshots(stream_id) ON DELETE CASCADE,
  generation bigint NOT NULL CHECK(generation>0),
  ordinal bigint NOT NULL CHECK(ordinal>0),
  request_cursor_sha256 text NOT NULL CHECK(request_cursor_sha256 ~ '^sha256:[a-f0-9]{64}$'),
  source_sha256 text NOT NULL CHECK(source_sha256 ~ '^sha256:[a-f0-9]{64}$'),
  source_bytes bytea NOT NULL,
  source_json jsonb NOT NULL,
  observed_at timestamptz NOT NULL,
  received_at timestamptz NOT NULL,
  PRIMARY KEY(stream_id,generation,ordinal),
  UNIQUE(stream_id,generation,request_cursor_sha256)
);
