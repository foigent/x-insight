-- Collection grants and XI reader field grants are different authorities.
-- Defaults do not change any existing grant revision or A0 authorization hash.
ALTER TABLE workbench_access_grants
  ADD COLUMN allow_source_context boolean NOT NULL DEFAULT false,
  ADD COLUMN allow_source_identity boolean NOT NULL DEFAULT false,
  ADD COLUMN allow_source_cost boolean NOT NULL DEFAULT false;

CREATE TABLE source_fact_streams (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  source_instance text NOT NULL CHECK(length(source_instance) BETWEEN 1 AND 128),
  org_id text NOT NULL CHECK(org_id ~ '^[1-9][0-9]{0,18}$'),
  project_id text NOT NULL CHECK(project_id ~ '^[1-9][0-9]{0,18}$'),
  profile text NOT NULL CHECK(profile IN ('unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1')),
  scope_generation text NOT NULL CHECK(scope_generation ~ '^[1-9][0-9]{0,18}$'),
  status text NOT NULL DEFAULT 'paused' CHECK(status IN ('active','paused','revoked')),
  cursor_value text NOT NULL DEFAULT '' CHECK(octet_length(cursor_value)<=8192),
  high_watermark text NOT NULL DEFAULT '' CHECK(octet_length(high_watermark)<=8192),
  revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
  last_error text NOT NULL DEFAULT '',
  last_received_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(source_system_id,source_instance,org_id,project_id,profile,scope_generation)
);

CREATE TABLE source_fact_documents (
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  source_instance text NOT NULL,
  org_id text NOT NULL,
  project_id text NOT NULL,
  request_id text NOT NULL,
  profile text NOT NULL CHECK(profile IN ('unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1')),
  stage text NOT NULL CHECK(stage IN ('authenticated','admission','dispatch','terminal')),
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
  CHECK((revision=1 AND previous_hash='') OR (revision>1 AND previous_hash ~ '^sha256:[a-f0-9]{64}$')),
  CHECK(source_hash='sha256:'||encode(sha256(source_bytes),'hex'))
);
CREATE INDEX source_fact_documents_reading_scope ON source_fact_documents(source_system_id,project_id,occurred_at,source_fact_id,revision DESC);
CREATE INDEX source_fact_documents_request ON source_fact_documents(source_system_id,source_instance,org_id,project_id,request_id,profile,stage,revision DESC);

CREATE TABLE workbench_source_members (
  reading_id text NOT NULL REFERENCES workbench_readings(id) ON DELETE RESTRICT,
  source_system_id uuid NOT NULL,
  event_id text NOT NULL,
  chain_state text NOT NULL CHECK(chain_state IN ('complete','missing_predecessor')),
  economic_state text NOT NULL CHECK(economic_state IN ('received','not_received','hash_mismatch','media_pricing_pending','media_not_eligible')),
  economic_fact_id uuid REFERENCES workbench_economic_facts(id) ON DELETE RESTRICT,
  PRIMARY KEY(reading_id,source_system_id,event_id),
  FOREIGN KEY(source_system_id,event_id) REFERENCES source_fact_documents(source_system_id,event_id) ON DELETE RESTRICT,
  CHECK((economic_state='received')=(economic_fact_id IS NOT NULL))
);
CREATE INDEX workbench_source_members_economic ON workbench_source_members(reading_id,economic_fact_id);

CREATE TABLE workbench_source_manifests (
  reading_id text PRIMARY KEY REFERENCES workbench_readings(id) ON DELETE RESTRICT,
  rule_version text NOT NULL CHECK(rule_version='xinsight.source-reading.v1'),
  authorization_sha256 text NOT NULL CHECK(authorization_sha256 ~ '^sha256:[a-f0-9]{64}$'),
  coverage jsonb NOT NULL CHECK(jsonb_typeof(coverage)='object'),
  manifest_sha256 text NOT NULL CHECK(manifest_sha256 ~ '^sha256:[a-f0-9]{64}$')
);

CREATE FUNCTION reject_source_fact_mutation() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
  RAISE EXCEPTION 'source documents and reading interpretations are immutable';
END $$;
REVOKE ALL ON FUNCTION reject_source_fact_mutation() FROM PUBLIC;
CREATE TRIGGER source_fact_documents_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON source_fact_documents
  FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
ALTER TABLE source_fact_documents ENABLE ALWAYS TRIGGER source_fact_documents_immutable;
CREATE TRIGGER workbench_source_members_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workbench_source_members
  FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
ALTER TABLE workbench_source_members ENABLE ALWAYS TRIGGER workbench_source_members_immutable;
CREATE TRIGGER workbench_source_manifests_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workbench_source_manifests
  FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
ALTER TABLE workbench_source_manifests ENABLE ALWAYS TRIGGER workbench_source_manifests_immutable;

CREATE FUNCTION guard_source_reading_append() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
  IF TG_TABLE_NAME='workbench_source_manifests' THEN
    -- A row lock alone does not invalidate an older repeatable-read snapshot.
    -- Create a new MVCC version without changing the reading's values, so a
    -- stale appender must serialize or fail before checking the manifest.
    UPDATE public.workbench_readings SET membership_sha256=membership_sha256 WHERE id=NEW.reading_id;
  ELSE
    PERFORM 1 FROM public.workbench_readings WHERE id=NEW.reading_id FOR UPDATE;
    IF NOT FOUND OR EXISTS(SELECT 1 FROM public.workbench_source_manifests WHERE reading_id=NEW.reading_id) THEN
      RAISE EXCEPTION 'source reading is already frozen or unavailable';
    END IF;
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_source_reading_append() FROM PUBLIC;
CREATE TRIGGER workbench_source_members_append BEFORE INSERT ON workbench_source_members
  FOR EACH ROW EXECUTE FUNCTION guard_source_reading_append();
ALTER TABLE workbench_source_members ENABLE ALWAYS TRIGGER workbench_source_members_append;
CREATE TRIGGER workbench_source_manifests_finalize BEFORE INSERT ON workbench_source_manifests
  FOR EACH ROW EXECUTE FUNCTION guard_source_reading_append();
ALTER TABLE workbench_source_manifests ENABLE ALWAYS TRIGGER workbench_source_manifests_finalize;

CREATE FUNCTION guard_source_stream_identity() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
  IF ROW(NEW.id,NEW.source_system_id,NEW.source_instance,NEW.org_id,NEW.project_id,NEW.profile,NEW.scope_generation)
    IS DISTINCT FROM ROW(OLD.id,OLD.source_system_id,OLD.source_instance,OLD.org_id,OLD.project_id,OLD.profile,OLD.scope_generation)
    OR (OLD.status='revoked' AND NEW.status<>'revoked') THEN
    RAISE EXCEPTION 'source scope changes require a new explicit registration';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_source_stream_identity() FROM PUBLIC;
CREATE TRIGGER source_fact_stream_identity BEFORE UPDATE ON source_fact_streams
  FOR EACH ROW EXECUTE FUNCTION guard_source_stream_identity();
ALTER TABLE source_fact_streams ENABLE ALWAYS TRIGGER source_fact_stream_identity;
