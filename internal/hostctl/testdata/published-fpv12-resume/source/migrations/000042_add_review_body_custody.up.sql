-- XI-owned review copies. Historical work-event ingest identities have no
-- review-body binding and therefore gain no body-write authority by upgrade.
-- A single XI deployment default. Suggested values live in the UI until an
-- administrator explicitly requests and confirms them; no row means deny.
CREATE TABLE review_capture_defaults (
  singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
  desired_revision bigint NOT NULL CHECK(desired_revision>0),
  desired_mode text NOT NULL CHECK(desired_mode IN ('disabled','full_content')),
  desired_retention_seconds bigint NOT NULL CHECK(desired_retention_seconds BETWEEN 1 AND 31536000),
  desired_project_quota_bytes bigint NOT NULL CHECK(desired_project_quota_bytes BETWEEN 1 AND 1099511627776),
  desired_max_item_bytes bigint NOT NULL CHECK(desired_max_item_bytes BETWEEN 0 AND 33554432),
  effective_revision bigint NOT NULL DEFAULT 0 CHECK(effective_revision>=0 AND effective_revision<=desired_revision),
  effective_mode text CHECK(effective_mode IN ('disabled','full_content')),
  effective_retention_seconds bigint CHECK(effective_retention_seconds BETWEEN 1 AND 31536000),
  effective_project_quota_bytes bigint CHECK(effective_project_quota_bytes BETWEEN 1 AND 1099511627776),
  effective_max_item_bytes bigint CHECK(effective_max_item_bytes BETWEEN 0 AND 33554432),
  requested_at timestamptz NOT NULL,
  effective_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CHECK((effective_revision=0)=(effective_mode IS NULL)),
  CHECK((effective_revision=0)=(effective_retention_seconds IS NULL)),
  CHECK((effective_revision=0)=(effective_project_quota_bytes IS NULL)),
  CHECK((effective_revision=0)=(effective_max_item_bytes IS NULL)),
  CHECK((effective_revision=0)=(effective_at IS NULL))
);

-- Exceptions are exact selectors. Empty category means every body kind; an
-- account selector always carries verified source authority and full scope.
CREATE TABLE review_capture_exceptions (
  policy_ref text PRIMARY KEY CHECK(policy_ref ~ '^xipe_[a-f0-9]{32}$'),
  selector_kind text NOT NULL CHECK(selector_kind IN ('tenant_category','source_project','source_project_category','account','account_category')),
  source_system_id uuid REFERENCES source_systems(id) ON DELETE RESTRICT,
  source_instance text NOT NULL DEFAULT '',
  org_id text NOT NULL DEFAULT '',
  project_id text NOT NULL DEFAULT '',
  account_authority text NOT NULL DEFAULT '',
  account_id text NOT NULL DEFAULT '',
  content_kind text NOT NULL DEFAULT '',
  desired_revision bigint NOT NULL CHECK(desired_revision>0),
  desired_mode text NOT NULL CHECK(desired_mode IN ('disabled','full_content')),
  desired_retention_seconds bigint NOT NULL CHECK(desired_retention_seconds BETWEEN 1 AND 31536000),
  desired_max_item_bytes bigint NOT NULL CHECK(desired_max_item_bytes BETWEEN 0 AND 33554432),
  effective_revision bigint NOT NULL DEFAULT 0 CHECK(effective_revision>=0 AND effective_revision<=desired_revision),
  effective_mode text CHECK(effective_mode IN ('disabled','full_content')),
  effective_retention_seconds bigint CHECK(effective_retention_seconds BETWEEN 1 AND 31536000),
  effective_max_item_bytes bigint CHECK(effective_max_item_bytes BETWEEN 0 AND 33554432),
  provenance text NOT NULL,
  requested_at timestamptz NOT NULL,
  effective_at timestamptz,
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE NULLS NOT DISTINCT(selector_kind,source_system_id,source_instance,org_id,project_id,account_authority,account_id,content_kind),
  CHECK((effective_revision=0)=(effective_mode IS NULL)),
  CHECK((effective_revision=0)=(effective_retention_seconds IS NULL)),
  CHECK((effective_revision=0)=(effective_max_item_bytes IS NULL)),
  CHECK((effective_revision=0)=(effective_at IS NULL)),
  CHECK((selector_kind='tenant_category' AND source_system_id IS NULL AND source_instance='' AND org_id='' AND project_id='' AND account_authority='' AND account_id='' AND content_kind<>'') OR
        (selector_kind IN ('source_project','source_project_category','account','account_category') AND source_system_id IS NOT NULL AND source_instance<>'' AND org_id<>'' AND project_id<>'' AND
         ((selector_kind='source_project' AND account_authority='' AND account_id='' AND content_kind='') OR
          (selector_kind='source_project_category' AND account_authority='' AND account_id='' AND content_kind<>'') OR
          (selector_kind='account' AND account_authority<>'' AND account_id<>'' AND content_kind='') OR
          (selector_kind='account_category' AND account_authority<>'' AND account_id<>'' AND content_kind<>''))))
);

CREATE TABLE review_capture_projects (
  xi_project_id uuid PRIMARY KEY,
  display_name text NOT NULL CHECK(length(display_name) BETWEEN 1 AND 128),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE review_capture_project_bindings (
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  source_instance text NOT NULL,
  org_id text NOT NULL,
  project_id text NOT NULL,
  xi_project_id uuid NOT NULL REFERENCES review_capture_projects(xi_project_id) ON DELETE RESTRICT,
  revision bigint NOT NULL CHECK(revision>0),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(source_system_id,source_instance,org_id,project_id)
);

-- The machine operation identity is installation + ref, as in UR. Scope is
-- immutable associated context. Unknown transport never implies effective.
CREATE TABLE review_capture_ur_operations (
  installation_id text NOT NULL,
  operation_ref text NOT NULL CHECK(operation_ref ~ '^xipw_[a-f0-9]{32}$'),
  source_instance text NOT NULL,
  org_id text NOT NULL,
  scope_kind text NOT NULL CHECK(scope_kind IN ('enterprise','project','user')),
  scope_id text NOT NULL,
  operator_subject text NOT NULL,
  request_sha256 text NOT NULL CHECK(request_sha256 ~ '^sha256:[a-f0-9]{64}$'),
  xi_desired_revision bigint NOT NULL CHECK(xi_desired_revision>0),
  ur_expected_ref text NOT NULL,
  ur_expected_version bigint NOT NULL CHECK(ur_expected_version>=0),
  ur_expected_vector jsonb NOT NULL,
  state text NOT NULL CHECK(state IN ('requested','unconfirmed','pending','effective','failed','drift')),
  ur_result jsonb,
  readback_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(installation_id,operation_ref)
);

CREATE TABLE review_body_ingest_bindings (
  service_identity_id text NOT NULL,
  collection_id uuid NOT NULL,
  status text NOT NULL CHECK(status IN ('active','revoked')),
  revision bigint NOT NULL CHECK(revision>0),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(service_identity_id,collection_id),
  FOREIGN KEY(service_identity_id,collection_id) REFERENCES work_event_ingest_bindings(service_identity_id,collection_id) ON DELETE RESTRICT
);

-- Mapped real projects share one quota row; otherwise a source-tuple digest
-- is a distinct namespace. Equal names/numeric IDs never merge implicitly.
CREATE TABLE review_body_project_usage (
  quota_scope text PRIMARY KEY CHECK(quota_scope ~ '^(project:[0-9a-f-]{36}|source:[a-f0-9]{64})$'),
  stored_bytes bigint NOT NULL DEFAULT 0 CHECK(stored_bytes>=0),
  reserved_bytes bigint NOT NULL DEFAULT 0 CHECK(reserved_bytes>=0)
);

CREATE TABLE review_body_operations (
  content_ref text PRIMARY KEY CHECK(content_ref ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$'),
  source_kind text NOT NULL CHECK(source_kind IN ('work_event','ur_c')),
  work_event_collection_id uuid REFERENCES work_event_collections(id) ON DELETE RESTRICT,
  content_collection_id uuid REFERENCES source_content_collections(id) ON DELETE RESTRICT,
  source_system_id uuid NOT NULL,
  source_instance text NOT NULL,
  org_id text NOT NULL,
  project_id text NOT NULL,
  quota_scope text NOT NULL REFERENCES review_body_project_usage(quota_scope) ON DELETE RESTRICT,
  event_ref text,
  event_revision bigint CHECK(event_revision>0),
  event_hash text CHECK(event_hash ~ '^sha256:[a-f0-9]{64}$'),
  source_fact_id text,
  source_revision numeric(20,0),
  source_sha256 text CHECK(source_sha256 ~ '^sha256:[a-f0-9]{64}$'),
  content_kind text NOT NULL,
  byte_domain text NOT NULL,
  digest text NOT NULL CHECK(digest ~ '^sha256:[a-f0-9]{64}$'),
  plain_bytes bigint NOT NULL CHECK(plain_bytes BETWEEN 0 AND 33554432),
  charged_bytes bigint NOT NULL CHECK(charged_bytes BETWEEN 16 AND 33554448),
  policy_ref text NOT NULL,
  policy_version text NOT NULL CHECK(length(policy_version) BETWEEN 1 AND 128),
  accepted_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL CHECK(expires_at>accepted_at),
  lease_expires_at timestamptz NOT NULL,
  state text NOT NULL CHECK(state IN ('reserved','accepted','gap','expired')),
  reason text NOT NULL DEFAULT '',
  FOREIGN KEY(source_system_id,source_instance,org_id,project_id,event_ref,event_revision) REFERENCES work_event_documents(source_system_id,source_instance,org_id,project_id,event_ref,revision) ON DELETE RESTRICT,
  FOREIGN KEY(source_system_id,source_fact_id,source_revision) REFERENCES source_content_manifests(source_system_id,source_fact_id,revision) ON DELETE RESTRICT,
  CHECK((source_kind='work_event' AND work_event_collection_id IS NOT NULL AND content_collection_id IS NULL AND
    event_ref IS NOT NULL AND event_revision IS NOT NULL AND event_hash IS NOT NULL AND
    source_fact_id IS NULL AND source_revision IS NULL AND source_sha256 IS NULL) OR
   (source_kind='ur_c' AND content_collection_id IS NOT NULL AND work_event_collection_id IS NULL AND
    event_ref IS NULL AND event_revision IS NULL AND event_hash IS NULL AND
    source_fact_id IS NOT NULL AND source_revision IS NOT NULL AND source_sha256 IS NOT NULL))
);
CREATE INDEX review_body_operations_expiry ON review_body_operations(state,expires_at,content_ref);
CREATE INDEX review_body_operations_event ON review_body_operations(source_system_id,source_instance,org_id,project_id,event_ref);

CREATE TABLE review_body_objects (
  content_ref text PRIMARY KEY REFERENCES review_body_operations(content_ref) ON DELETE RESTRICT,
  key_id text NOT NULL CHECK(key_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$'),
  nonce bytea NOT NULL CHECK(octet_length(nonce)=12),
  ciphertext bytea NOT NULL CHECK(octet_length(ciphertext) BETWEEN 16 AND 33554448)
);

-- Durable retry/skip state for the independent managed UR C review copy.
-- Manifest cursors may advance before the source bytes can be copied; this
-- table lets the existing importer revisit the exact immutable manifest.
CREATE TABLE review_body_copy_attempts (
  collection_id uuid NOT NULL REFERENCES source_content_collections(id) ON DELETE RESTRICT,
  content_ref text NOT NULL,
  source_system_id uuid NOT NULL,
  source_fact_id text NOT NULL,
  source_revision numeric(20,0) NOT NULL,
  source_sha256 text NOT NULL CHECK(source_sha256 ~ '^sha256:[a-f0-9]{64}$'),
  status text NOT NULL CHECK(status IN ('retry','excluded_policy','scope_lost','source_expired','integrity_gap','accepted')),
  reason text NOT NULL CHECK(length(reason) BETWEEN 1 AND 80),
  attempts integer NOT NULL CHECK(attempts BETWEEN 1 AND 1000000),
  next_attempt_at timestamptz,
  last_attempt_at timestamptz NOT NULL,
  PRIMARY KEY(collection_id,content_ref),
  FOREIGN KEY(source_system_id,source_fact_id,source_revision) REFERENCES source_content_manifests(source_system_id,source_fact_id,revision) ON DELETE RESTRICT,
  CHECK((status='retry')=(next_attempt_at IS NOT NULL))
);
CREATE INDEX review_body_copy_attempts_due ON review_body_copy_attempts(collection_id,status,next_attempt_at);

CREATE FUNCTION guard_review_body_authority() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
  IF NOT pg_has_role(current_user,(SELECT relowner FROM pg_class WHERE oid=TG_RELID),'USAGE') THEN
    RAISE EXCEPTION 'review body policy and binding require administrator';
  END IF;
  IF TG_OP='DELETE' THEN RAISE EXCEPTION 'review body authority history cannot be deleted'; END IF;
  IF TG_OP='UPDATE' THEN
    IF TG_TABLE_NAME IN ('review_capture_defaults','review_capture_exceptions') THEN
      IF NEW.desired_revision<>OLD.desired_revision+1 AND
         NOT (NEW.desired_revision=OLD.desired_revision AND NEW.effective_revision=NEW.desired_revision AND OLD.effective_revision<NEW.effective_revision) THEN
        RAISE EXCEPTION 'review body policy revision is invalid';
      END IF;
    ELSIF TG_TABLE_NAME='review_body_ingest_bindings' AND NEW.revision<>OLD.revision+1 THEN
      RAISE EXCEPTION 'review body authority revision is invalid';
    ELSIF TG_TABLE_NAME IN ('review_capture_projects','review_capture_project_bindings') THEN
      RAISE EXCEPTION 'review project identity is immutable';
    END IF;
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_review_body_authority() FROM PUBLIC;
CREATE TRIGGER review_capture_default_authority BEFORE INSERT OR UPDATE OR DELETE ON review_capture_defaults
  FOR EACH ROW EXECUTE FUNCTION guard_review_body_authority();
CREATE TRIGGER review_capture_exception_authority BEFORE INSERT OR UPDATE OR DELETE ON review_capture_exceptions
  FOR EACH ROW EXECUTE FUNCTION guard_review_body_authority();
CREATE TRIGGER review_capture_project_authority BEFORE INSERT OR UPDATE OR DELETE ON review_capture_projects
  FOR EACH ROW EXECUTE FUNCTION guard_review_body_authority();
CREATE TRIGGER review_capture_project_binding_authority BEFORE INSERT OR UPDATE OR DELETE ON review_capture_project_bindings
  FOR EACH ROW EXECUTE FUNCTION guard_review_body_authority();
CREATE TRIGGER review_body_binding_authority BEFORE UPDATE OR DELETE ON review_body_ingest_bindings
  FOR EACH ROW EXECUTE FUNCTION guard_review_body_authority();
ALTER TABLE review_capture_defaults ENABLE ALWAYS TRIGGER review_capture_default_authority;
ALTER TABLE review_capture_exceptions ENABLE ALWAYS TRIGGER review_capture_exception_authority;
ALTER TABLE review_capture_projects ENABLE ALWAYS TRIGGER review_capture_project_authority;
ALTER TABLE review_capture_project_bindings ENABLE ALWAYS TRIGGER review_capture_project_binding_authority;
ALTER TABLE review_body_ingest_bindings ENABLE ALWAYS TRIGGER review_body_binding_authority;
