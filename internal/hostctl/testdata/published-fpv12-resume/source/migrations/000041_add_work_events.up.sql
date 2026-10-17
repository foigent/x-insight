-- Independent agent/runtime work facts. Existing UR profiles and the test-only
-- resource-action v1 are deliberately unchanged.
CREATE TABLE work_event_collections (
  id uuid PRIMARY KEY,
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  source_instance text NOT NULL,
  org_id text NOT NULL,
  project_id text NOT NULL,
  profile text NOT NULL CHECK(profile='xinsight.work-event.v1'),
  scope_generation text NOT NULL,
  status text NOT NULL DEFAULT 'staged' CHECK(status IN ('staged','active','paused','revoked')),
  cursor_value text NOT NULL DEFAULT '',
  high_watermark text NOT NULL DEFAULT '',
  revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
  requires_attention boolean NOT NULL DEFAULT false,
  last_error text NOT NULL DEFAULT '',
  last_received_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE(source_system_id,source_instance,org_id,project_id,scope_generation)
);
CREATE INDEX work_event_collections_source ON work_event_collections(source_system_id,status);

-- The HTTP workspace role may take a row lock for admission, but it cannot
-- mutate registration authority. The importer retains its separate progress
-- columns; only the administrator may change scope, status or generation.
CREATE FUNCTION guard_work_event_collection() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
  IF TG_OP='DELETE' THEN
    RAISE EXCEPTION 'work event collection history cannot be deleted';
  END IF;
  IF current_user='xinsight_workspace' THEN
    RAISE EXCEPTION 'work event collection changes require administrator or importer';
  END IF;
  IF ROW(NEW.id,NEW.source_system_id,NEW.source_instance,NEW.org_id,NEW.project_id,NEW.profile,NEW.scope_generation)
     IS DISTINCT FROM
     ROW(OLD.id,OLD.source_system_id,OLD.source_instance,OLD.org_id,OLD.project_id,OLD.profile,OLD.scope_generation) THEN
    RAISE EXCEPTION 'work event collection identity is immutable';
  END IF;
  IF NOT pg_has_role(current_user,(SELECT relowner FROM pg_class WHERE oid='public.work_event_collections'::regclass),'USAGE') AND
     NEW.status IS DISTINCT FROM OLD.status THEN
    RAISE EXCEPTION 'work event collection authority changes require administrator';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_work_event_collection() FROM PUBLIC;
CREATE TRIGGER work_event_collection_authority BEFORE UPDATE OR DELETE ON work_event_collections
  FOR EACH ROW EXECUTE FUNCTION guard_work_event_collection();
ALTER TABLE work_event_collections ENABLE ALWAYS TRIGGER work_event_collection_authority;

CREATE FUNCTION guard_workspace_source_status() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
  IF current_user='xinsight_workspace' THEN
    RAISE EXCEPTION 'source status changes require administrator or importer';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_workspace_source_status() FROM PUBLIC;
CREATE TRIGGER source_systems_workspace_lock_guard BEFORE UPDATE ON source_systems
  FOR EACH ROW EXECUTE FUNCTION guard_workspace_source_status();
ALTER TABLE source_systems ENABLE ALWAYS TRIGGER source_systems_workspace_lock_guard;

ALTER TABLE service_identities DROP CONSTRAINT service_identities_permissions_check;
ALTER TABLE service_identities ADD CONSTRAINT service_identities_permissions_check CHECK (
  (created_by_subject='system' AND permissions=ARRAY['connector.control.poll','connector.control.ack']::text[])
  OR (created_by_subject='root' AND permissions=ARRAY['work_event.ingest']::text[])
  OR (created_by_subject<>'system' AND cardinality(permissions) BETWEEN 1 AND 2
      AND permissions <@ ARRAY['evidence.read','evidence.cite']::text[])
);

CREATE TABLE work_event_ingest_bindings (
  service_identity_id text NOT NULL REFERENCES service_identities(id) ON DELETE RESTRICT,
  collection_id uuid NOT NULL REFERENCES work_event_collections(id) ON DELETE RESTRICT,
  producer_project_key text NOT NULL CHECK(length(producer_project_key) BETWEEN 1 AND 256),
  project_label text NOT NULL CHECK(length(project_label) BETWEEN 1 AND 128),
  status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','revoked')),
  revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(service_identity_id,collection_id),
  UNIQUE(service_identity_id,producer_project_key)
);
CREATE INDEX work_event_ingest_bindings_collection ON work_event_ingest_bindings(collection_id,status);

-- An optional, administrator-paired UR target belongs to one collection.
-- A producer may report only this exact target scope for relations.
CREATE TABLE work_event_ur_targets (
  collection_id uuid PRIMARY KEY REFERENCES work_event_collections(id) ON DELETE RESTRICT,
  target_source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  target_source_instance text NOT NULL,
  target_org_id text NOT NULL,
  target_project_id text NOT NULL,
  target_label text NOT NULL CHECK(length(target_label) BETWEEN 1 AND 128),
  status text NOT NULL DEFAULT 'active' CHECK(status IN ('active','revoked')),
  revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE FUNCTION guard_work_event_ingest_authority() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
  IF NOT pg_has_role(current_user,(SELECT relowner FROM pg_class WHERE oid=TG_RELID),'USAGE') THEN
    RAISE EXCEPTION 'work event ingest authority changes require administrator';
  END IF;
  IF TG_OP='DELETE' THEN
    RAISE EXCEPTION 'work event ingest authority history cannot be deleted';
  END IF;
  IF NEW.revision<>OLD.revision+1 OR (OLD.status='revoked' AND NEW.status<>'revoked') THEN
    RAISE EXCEPTION 'work event ingest authority revision is invalid';
  END IF;
  IF TG_TABLE_NAME='work_event_ingest_bindings' AND
     ROW(NEW.service_identity_id,NEW.collection_id,NEW.producer_project_key,NEW.project_label,NEW.created_at)
     IS DISTINCT FROM ROW(OLD.service_identity_id,OLD.collection_id,OLD.producer_project_key,OLD.project_label,OLD.created_at) THEN
    RAISE EXCEPTION 'work event ingest binding identity is immutable';
  END IF;
  IF TG_TABLE_NAME='work_event_ur_targets' AND
     ROW(NEW.collection_id,NEW.target_source_system_id,NEW.target_source_instance,NEW.target_org_id,
         NEW.target_project_id,NEW.target_label,NEW.created_at)
     IS DISTINCT FROM ROW(OLD.collection_id,OLD.target_source_system_id,OLD.target_source_instance,OLD.target_org_id,
                          OLD.target_project_id,OLD.target_label,OLD.created_at) THEN
    RAISE EXCEPTION 'work event UR target identity is immutable';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_work_event_ingest_authority() FROM PUBLIC;
CREATE TRIGGER work_event_ingest_binding_authority BEFORE UPDATE OR DELETE ON work_event_ingest_bindings
  FOR EACH ROW EXECUTE FUNCTION guard_work_event_ingest_authority();
ALTER TABLE work_event_ingest_bindings ENABLE ALWAYS TRIGGER work_event_ingest_binding_authority;
CREATE TRIGGER work_event_ur_target_authority BEFORE UPDATE OR DELETE ON work_event_ur_targets
  FOR EACH ROW EXECUTE FUNCTION guard_work_event_ingest_authority();
ALTER TABLE work_event_ur_targets ENABLE ALWAYS TRIGGER work_event_ur_target_authority;

CREATE TABLE work_event_documents (
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  source_instance text NOT NULL,
  org_id text NOT NULL,
  project_id text NOT NULL,
  event_ref text NOT NULL,
  revision bigint NOT NULL CHECK(revision BETWEEN 1 AND 1000000),
  previous_hash text NOT NULL DEFAULT '',
  event_hash text NOT NULL CHECK(event_hash ~ '^sha256:[a-f0-9]{64}$'),
  occurred_at timestamptz NOT NULL,
  recorded_at timestamptz NOT NULL,
  received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  subject_kind text NOT NULL CHECK(subject_kind IN ('task','message','model_call','tool_call','resource_action')),
  lifecycle_state text NOT NULL CHECK(lifecycle_state IN ('started','completed','failed','unknown')),
  task_ref text NOT NULL DEFAULT '',
  run_id text NOT NULL,
  model_call_id text NOT NULL DEFAULT '',
  tool_call_id text NOT NULL,
  action_kind text NOT NULL,
  resource_ref text NOT NULL,
  resource_name text NOT NULL,
  actor_kind text NOT NULL,
  actor_ref text NOT NULL,
  content_state text NOT NULL,
  event_bytes bytea NOT NULL CHECK(octet_length(event_bytes) BETWEEN 1 AND 8192),
  event_json jsonb NOT NULL CHECK(event_json=convert_from(event_bytes,'UTF8')::jsonb),
  PRIMARY KEY(source_system_id,source_instance,org_id,project_id,event_ref,revision),
  CHECK((revision=1 AND previous_hash='') OR (revision>1 AND previous_hash ~ '^sha256:[a-f0-9]{64}$')),
  CHECK(event_hash='sha256:'||encode(sha256(event_bytes),'hex')),
  CHECK((event_json->>'schema_version'='xinsight.work-event.v1' AND
    event_json#>>'{reporter,source_system_id}'=source_system_id::text AND
    event_json#>>'{reporter,source_instance}'=source_instance AND
    event_json#>>'{reporter,org_id}'=org_id AND
    event_json#>>'{reporter,project_id}'=project_id AND
    event_json->>'event_ref'=event_ref AND
    event_json->>'revision'=revision::text AND
    COALESCE(event_json->>'previous_hash','')=previous_hash AND
    event_json->>'subject_kind'=subject_kind AND
    event_json->>'lifecycle_state'=lifecycle_state AND
    COALESCE(event_json->>'task_ref','')=task_ref AND
    COALESCE(event_json->>'run_id','')=run_id AND
    COALESCE(event_json->>'model_call_id','')=model_call_id AND
    COALESCE(event_json->>'tool_call_id','')=tool_call_id AND
    event_json->>'action_kind'=action_kind AND
    COALESCE(event_json#>>'{resource,ref}','')=resource_ref AND
    COALESCE(event_json#>>'{resource,name}','')=resource_name AND
    event_json#>>'{actor,kind}'=actor_kind AND
    COALESCE(event_json#>>'{actor,ref}','')=actor_ref AND
    event_json->>'content_state'=content_state) IS TRUE)
);
CREATE INDEX work_event_documents_activity ON work_event_documents
  (source_system_id,project_id,occurred_at DESC,event_ref,revision DESC);
CREATE INDEX work_event_documents_resource ON work_event_documents
  (source_system_id,project_id,resource_ref,occurred_at DESC);
CREATE TRIGGER work_event_documents_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON work_event_documents
  FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
ALTER TABLE work_event_documents ENABLE ALWAYS TRIGGER work_event_documents_immutable;

-- Reporter assertions are separate from the original event and cannot confer
-- XI verification. Target access is checked on every read.
CREATE TABLE work_event_relations (
  reporter_source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  reporter_source_instance text NOT NULL,
  reporter_org_id text NOT NULL,
  reporter_project_id text NOT NULL,
  relation_ref text NOT NULL,
  event_ref text NOT NULL,
  event_revision bigint NOT NULL CHECK(event_revision BETWEEN 1 AND 1000000),
  event_hash text NOT NULL CHECK(event_hash ~ '^sha256:[a-f0-9]{64}$'),
  target_source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  target_source_instance text NOT NULL,
  target_org_id text NOT NULL,
  target_project_id text NOT NULL,
  target_request_id text NOT NULL,
  model_call_id text NOT NULL,
  evidence_ref text NOT NULL,
  evidence_sha256 text NOT NULL,
  recorded_at timestamptz NOT NULL,
  received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  relation_hash text NOT NULL CHECK(relation_hash ~ '^sha256:[a-f0-9]{64}$'),
  relation_bytes bytea NOT NULL CHECK(octet_length(relation_bytes) BETWEEN 1 AND 8192),
  relation_json jsonb NOT NULL CHECK(relation_json=convert_from(relation_bytes,'UTF8')::jsonb),
  PRIMARY KEY(reporter_source_system_id,reporter_source_instance,reporter_org_id,reporter_project_id,relation_ref),
  CHECK(relation_hash='sha256:'||encode(sha256(relation_bytes),'hex')),
  CHECK((relation_json->>'schema_version'='xinsight.work-event-relation.v1' AND
    relation_json#>>'{reporter,source_system_id}'=reporter_source_system_id::text AND
    relation_json#>>'{reporter,source_instance}'=reporter_source_instance AND
    relation_json#>>'{reporter,org_id}'=reporter_org_id AND
    relation_json#>>'{reporter,project_id}'=reporter_project_id AND
    relation_json->>'relation_ref'=relation_ref AND
    relation_json->>'event_ref'=event_ref AND
    relation_json->>'event_revision'=event_revision::text AND
    relation_json->>'event_hash'=event_hash AND
    relation_json#>>'{ur_target,source_system_id}'=target_source_system_id::text AND
    relation_json#>>'{ur_target,source_instance}'=target_source_instance AND
    relation_json#>>'{ur_target,org_id}'=target_org_id AND
    relation_json#>>'{ur_target,project_id}'=target_project_id AND
    relation_json->>'ur_request_id'=target_request_id) IS TRUE)
);
CREATE INDEX work_event_relations_event ON work_event_relations
  (reporter_source_system_id,reporter_source_instance,reporter_org_id,reporter_project_id,event_ref);
CREATE TRIGGER work_event_relations_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON work_event_relations
  FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
ALTER TABLE work_event_relations ENABLE ALWAYS TRIGGER work_event_relations_immutable;
