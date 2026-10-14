-- D receives authenticated current snapshots. No event/feed/economic table is
-- repurposed, and all existing viewer grants default to no budget access.
ALTER TABLE workbench_access_grants
  ADD COLUMN allow_budget_definition boolean NOT NULL DEFAULT false,
  ADD COLUMN allow_budget_summary boolean NOT NULL DEFAULT false,
  ADD COLUMN allow_budget_alert boolean NOT NULL DEFAULT false;
ALTER TABLE workbench_access_grants DROP CONSTRAINT workbench_access_grants_has_capability_check;
ALTER TABLE workbench_access_grants ADD CONSTRAINT workbench_access_grants_has_capability_check CHECK (
  allow_summary OR allow_details OR allow_coverage OR allow_preferences
  OR allow_source_runtime OR allow_source_attempt OR allow_source_capability
  OR allow_source_policy OR allow_source_destination OR allow_resource
  OR allow_budget_definition OR allow_budget_summary OR allow_budget_alert
);
CREATE FUNCTION advance_budget_grant_revision() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
  IF ROW(NEW.allow_budget_definition,NEW.allow_budget_summary,NEW.allow_budget_alert)
     IS DISTINCT FROM ROW(OLD.allow_budget_definition,OLD.allow_budget_summary,OLD.allow_budget_alert)
     AND NEW.revision<=OLD.revision THEN NEW.revision=OLD.revision+1; END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION advance_budget_grant_revision() FROM PUBLIC;
CREATE TRIGGER workbench_budget_grant_revision BEFORE UPDATE ON workbench_access_grants
  FOR EACH ROW EXECUTE FUNCTION advance_budget_grant_revision();
ALTER TABLE workbench_access_grants ENABLE ALWAYS TRIGGER workbench_budget_grant_revision;


CREATE TABLE source_budget_collections (
  id uuid PRIMARY KEY,
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  source_instance text NOT NULL CHECK(length(source_instance) BETWEEN 1 AND 128),
  org_id text NOT NULL CHECK(org_id ~ '^[1-9][0-9]{0,18}$'),
  project_id text NOT NULL CHECK(project_id ~ '^[1-9][0-9]{0,18}$'),
  transport text NOT NULL DEFAULT 'live' CHECK(transport IN ('live','bounded_offline')),
  scope_generation text NOT NULL CHECK(scope_generation ~ '^[1-9][0-9]{0,18}$'),
  profiles text[] NOT NULL CHECK((array_ndims(profiles)=1 AND array_lower(profiles,1)=1 AND cardinality(profiles) BETWEEN 1 AND 3 AND profiles <@ ARRAY['unirouter.budget-definition.v1','unirouter.budget-period-summary.v1','unirouter.budget-alert.v1']::text[] AND array_position(profiles,NULL) IS NULL AND (cardinality(profiles)<2 OR profiles[1]<>profiles[2]) AND (cardinality(profiles)<3 OR (profiles[1]<>profiles[3] AND profiles[2]<>profiles[3])))),
  status text NOT NULL DEFAULT 'paused' CHECK(status IN ('paused','active','revoked')),
  revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
  requires_attention boolean NOT NULL DEFAULT false,
  last_error text NOT NULL DEFAULT '',
  access_generation text NOT NULL DEFAULT '' CHECK(access_generation ~ '^([1-9][0-9]{0,18})?$'),
  catalog_revision text NOT NULL DEFAULT '' CHECK(catalog_revision ~ '^([1-9][0-9]{0,18})?$'),
  granted_profiles text[] NOT NULL DEFAULT '{}' CHECK(cardinality(granted_profiles)=0 OR (array_ndims(granted_profiles)=1 AND array_lower(granted_profiles,1)=1 AND cardinality(granted_profiles) BETWEEN 1 AND 3 AND granted_profiles <@ ARRAY['unirouter.budget-definition.v1','unirouter.budget-period-summary.v1','unirouter.budget-alert.v1']::text[] AND array_position(granted_profiles,NULL) IS NULL AND (cardinality(granted_profiles)<2 OR granted_profiles[1]<>granted_profiles[2]) AND (cardinality(granted_profiles)<3 OR (granted_profiles[1]<>granted_profiles[3] AND granted_profiles[2]<>granted_profiles[3])))),
  cycle bigint NOT NULL DEFAULT 0 CHECK(cycle>=0),
  visible_cycle bigint NOT NULL DEFAULT 0 CHECK(visible_cycle BETWEEN 0 AND cycle),
  catalog_id uuid,
  catalog_cursor text NOT NULL DEFAULT '' CHECK(octet_length(catalog_cursor)<=4096),
  entry_index integer NOT NULL DEFAULT 0 CHECK(entry_index BETWEEN 0 AND 50),
  alerts_cursor text NOT NULL DEFAULT '' CHECK(octet_length(alerts_cursor)<=4096),
  last_attempt_at timestamptz,
  last_success_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CHECK(granted_profiles <@ profiles)
);

ALTER TABLE source_fact_management_events DROP CONSTRAINT source_fact_management_events_registration_kind_check;
ALTER TABLE source_fact_management_events ADD CONSTRAINT source_fact_management_events_registration_kind_check
  CHECK(registration_kind IN ('live','offline','budget'));

CREATE FUNCTION guard_budget_collection() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE administrator boolean;
BEGIN
  SELECT pg_has_role(current_user,relowner,'USAGE') INTO administrator
  FROM pg_class WHERE oid='public.source_budget_collections'::regclass;
  IF TG_OP='DELETE' THEN RAISE EXCEPTION 'budget collection history cannot be deleted'; END IF;
  IF TG_OP='INSERT' THEN
    IF NOT administrator THEN RAISE EXCEPTION 'budget registration requires administrator'; END IF;
    RETURN NEW;
  END IF;
  IF ROW(NEW.id,NEW.source_system_id,NEW.source_instance,NEW.org_id,NEW.project_id,NEW.scope_generation,NEW.transport,NEW.profiles,NEW.created_at)
     IS DISTINCT FROM ROW(OLD.id,OLD.source_system_id,OLD.source_instance,OLD.org_id,OLD.project_id,OLD.scope_generation,OLD.transport,OLD.profiles,OLD.created_at)
     OR NEW.revision<>OLD.revision+1 OR (OLD.status='revoked' AND NEW.status<>'revoked') THEN
    RAISE EXCEPTION 'budget identity is immutable and state requires next revision';
  END IF;
  IF NOT administrator AND (NEW.status<>OLD.status OR OLD.status<>'active' OR OLD.requires_attention) THEN
    RAISE EXCEPTION 'budget management requires administrator';
  END IF;
  IF NEW.cycle<OLD.cycle OR (OLD.access_generation<>'' AND (NEW.access_generation='' OR NULLIF(NEW.access_generation,'')::numeric<NULLIF(OLD.access_generation,'')::numeric))
     OR (OLD.catalog_revision<>'' AND (NEW.catalog_revision='' OR NULLIF(NEW.catalog_revision,'')::numeric<NULLIF(OLD.catalog_revision,'')::numeric)) THEN
    RAISE EXCEPTION 'budget collection cannot rewind source authority';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_budget_collection() FROM PUBLIC;
CREATE TRIGGER source_budget_collection_guard BEFORE INSERT OR UPDATE OR DELETE ON source_budget_collections
  FOR EACH ROW EXECUTE FUNCTION guard_budget_collection();
ALTER TABLE source_budget_collections ENABLE ALWAYS TRIGGER source_budget_collection_guard;

CREATE TABLE source_budget_catalogs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  collection_id uuid NOT NULL REFERENCES source_budget_collections(id),
  cycle bigint NOT NULL CHECK(cycle>0),
  input_cursor text NOT NULL CHECK(octet_length(input_cursor)<=4096),
  catalog_bytes bytea NOT NULL CHECK(octet_length(catalog_bytes) BETWEEN 1 AND 65536),
  catalog_sha256 text NOT NULL CHECK(catalog_sha256='sha256:'||encode(sha256(catalog_bytes),'hex')),
  received_at timestamptz NOT NULL,
  UNIQUE(collection_id,cycle,input_cursor)
);
ALTER TABLE source_budget_collections ADD CONSTRAINT source_budget_pending_catalog_fk
  FOREIGN KEY(catalog_id) REFERENCES source_budget_catalogs(id);

CREATE TABLE source_budget_catalog_entries (
  collection_id uuid NOT NULL REFERENCES source_budget_collections(id),
  cycle bigint NOT NULL CHECK(cycle>0),
  budget_ref text NOT NULL,
  catalog_id uuid NOT NULL REFERENCES source_budget_catalogs(id),
  origin text NOT NULL CHECK(origin IN ('gateway_project','enterprise_project')),
  definition_revision text NOT NULL CHECK(definition_revision ~ '^[1-9][0-9]{0,18}$'),
  binding_revision text NOT NULL CHECK(binding_revision ~ '^([1-9][0-9]{0,18})?$'),
  PRIMARY KEY(collection_id,cycle,budget_ref)
);

CREATE TABLE source_budget_readings (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  source_system_id uuid NOT NULL REFERENCES source_systems(id),
  source_instance text NOT NULL,
  org_id text NOT NULL,
  project_id text NOT NULL,
  reading_id text NOT NULL CHECK(reading_id ~ '^ur-budget-reading-[a-f0-9]{32}$'),
  budget_ref text NOT NULL,
  reading_cutoff timestamptz NOT NULL,
  observed_at timestamptz NOT NULL,
  definition_revision text NOT NULL,
  binding_revision text NOT NULL,
  access_generation text NOT NULL,
  catalog_revision text NOT NULL,
  profiles text[] NOT NULL CHECK((array_ndims(profiles)=1 AND array_lower(profiles,1)=1 AND cardinality(profiles) BETWEEN 1 AND 3 AND profiles <@ ARRAY['unirouter.budget-definition.v1','unirouter.budget-period-summary.v1','unirouter.budget-alert.v1']::text[] AND array_position(profiles,NULL) IS NULL AND (cardinality(profiles)<2 OR profiles[1]<>profiles[2]) AND (cardinality(profiles)<3 OR (profiles[1]<>profiles[3] AND profiles[2]<>profiles[3])))),
  reading_bytes bytea NOT NULL CHECK(octet_length(reading_bytes) BETWEEN 1 AND 212992),
  reading_sha256 text NOT NULL CHECK(reading_sha256='sha256:'||encode(sha256(reading_bytes),'hex')),
  received_at timestamptz NOT NULL,
  UNIQUE(source_system_id,reading_id)
);
CREATE INDEX source_budget_readings_latest ON source_budget_readings
  (source_system_id,source_instance,org_id,project_id,budget_ref,reading_cutoff DESC,reading_id DESC);
CREATE TABLE source_budget_receipts (
  collection_id uuid NOT NULL REFERENCES source_budget_collections(id),
  source_reading_id uuid NOT NULL REFERENCES source_budget_readings(id),
  catalog_id uuid NOT NULL REFERENCES source_budget_catalogs(id),
  alerts_input_cursor text NOT NULL DEFAULT '' CHECK(octet_length(alerts_input_cursor)<=4096),
  received_at timestamptz NOT NULL,
  PRIMARY KEY(collection_id,source_reading_id,catalog_id)
);

CREATE TRIGGER source_budget_catalogs_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON source_budget_catalogs
  FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
CREATE TRIGGER source_budget_entries_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON source_budget_catalog_entries
  FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
CREATE TRIGGER source_budget_readings_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON source_budget_readings
  FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
CREATE TRIGGER source_budget_receipts_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON source_budget_receipts
  FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
ALTER TABLE source_budget_catalogs ENABLE ALWAYS TRIGGER source_budget_catalogs_immutable;
ALTER TABLE source_budget_catalog_entries ENABLE ALWAYS TRIGGER source_budget_entries_immutable;
ALTER TABLE source_budget_readings ENABLE ALWAYS TRIGGER source_budget_readings_immutable;
ALTER TABLE source_budget_receipts ENABLE ALWAYS TRIGGER source_budget_receipts_immutable;

CREATE TABLE budget_readings (
  id text PRIMARY KEY CHECK(id ~ '^xbd_[A-Za-z0-9_-]{22}$'),
  owner_subject text NOT NULL,
  contract_version text NOT NULL,
  cutoff timestamptz NOT NULL,
  expires_at timestamptz NOT NULL CHECK(expires_at>cutoff),
  filters jsonb NOT NULL,
  authorization_revision text NOT NULL,
  interpretation_revision text NOT NULL,
  membership_sha256 text NOT NULL CHECK(membership_sha256 ~ '^sha256:[a-f0-9]{64}$'),
  budget_count bigint NOT NULL DEFAULT 0 CHECK(budget_count>=0),
  sealed boolean NOT NULL DEFAULT false
);
CREATE INDEX budget_readings_owner ON budget_readings(owner_subject,expires_at,id);
CREATE TABLE budget_reading_members (
  reading_id text NOT NULL REFERENCES budget_readings(id),
  source_reading_id uuid NOT NULL REFERENCES source_budget_readings(id),
  collection_id uuid NOT NULL REFERENCES source_budget_collections(id),
  receipt_catalog_id uuid NOT NULL REFERENCES source_budget_catalogs(id),
  ordinal bigint NOT NULL CHECK(ordinal>0),
  profiles text[] NOT NULL CHECK((array_ndims(profiles)=1 AND array_lower(profiles,1)=1 AND cardinality(profiles) BETWEEN 1 AND 3 AND profiles <@ ARRAY['unirouter.budget-definition.v1','unirouter.budget-period-summary.v1','unirouter.budget-alert.v1']::text[] AND array_position(profiles,NULL) IS NULL AND (cardinality(profiles)<2 OR profiles[1]<>profiles[2]) AND (cardinality(profiles)<3 OR (profiles[1]<>profiles[3] AND profiles[2]<>profiles[3])))),
  source_state text NOT NULL CHECK(source_state IN ('active','paused','bounded_offline')),
  PRIMARY KEY(reading_id,source_reading_id),
  UNIQUE(reading_id,ordinal)
  ,FOREIGN KEY(collection_id,source_reading_id,receipt_catalog_id) REFERENCES source_budget_receipts(collection_id,source_reading_id,catalog_id)
);

CREATE FUNCTION guard_budget_reading() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
  IF TG_OP='DELETE' THEN RAISE EXCEPTION 'budget reading is immutable'; END IF;
  IF OLD.sealed OR NOT NEW.sealed OR ROW(NEW.id,NEW.owner_subject,NEW.contract_version,NEW.cutoff,NEW.expires_at,NEW.filters,NEW.authorization_revision,NEW.interpretation_revision)
     IS DISTINCT FROM ROW(OLD.id,OLD.owner_subject,OLD.contract_version,OLD.cutoff,OLD.expires_at,OLD.filters,OLD.authorization_revision,OLD.interpretation_revision) THEN
    RAISE EXCEPTION 'budget reading can only be sealed once';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_budget_reading() FROM PUBLIC;
CREATE TRIGGER budget_readings_seal BEFORE UPDATE OR DELETE ON budget_readings
  FOR EACH ROW EXECUTE FUNCTION guard_budget_reading();
ALTER TABLE budget_readings ENABLE ALWAYS TRIGGER budget_readings_seal;
CREATE FUNCTION guard_budget_reading_member() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE closed boolean;
BEGIN
  IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'budget reading membership is immutable'; END IF;
  SELECT sealed INTO closed FROM public.budget_readings WHERE id=NEW.reading_id FOR SHARE;
  IF closed IS DISTINCT FROM false THEN RAISE EXCEPTION 'budget reading is already sealed'; END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_budget_reading_member() FROM PUBLIC;
CREATE TRIGGER budget_reading_members_immutable BEFORE INSERT OR UPDATE OR DELETE ON budget_reading_members
  FOR EACH ROW EXECUTE FUNCTION guard_budget_reading_member();
ALTER TABLE budget_reading_members ENABLE ALWAYS TRIGGER budget_reading_members_immutable;

-- Offline authority is fixed to one collection, target and byte-identical bundle.
-- The importer can append its receipt but cannot register or widen permission.
CREATE TABLE source_budget_offline_authorizations (
  collection_id uuid PRIMARY KEY REFERENCES source_budget_collections(id),
  authorization_bytes bytea NOT NULL CHECK(octet_length(authorization_bytes) BETWEEN 1 AND 65536),
  authorization_sha256 text NOT NULL CHECK(authorization_sha256='sha256:'||encode(sha256(authorization_bytes),'hex')),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE FUNCTION guard_budget_offline_registration() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
  IF TG_OP<>'INSERT' OR NOT EXISTS(SELECT 1 FROM pg_class WHERE oid='public.source_budget_offline_authorizations'::regclass AND pg_has_role(current_user,relowner,'USAGE')) THEN
    RAISE EXCEPTION 'offline budget authorization requires administrator and is immutable';
  END IF;
  IF NOT EXISTS(SELECT 1 FROM public.source_budget_collections WHERE id=NEW.collection_id AND transport='bounded_offline') THEN
    RAISE EXCEPTION 'offline budget authorization requires an offline collection';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_budget_offline_registration() FROM PUBLIC;
CREATE TRIGGER source_budget_offline_authorizations_guard BEFORE INSERT OR UPDATE OR DELETE ON source_budget_offline_authorizations
  FOR EACH ROW EXECUTE FUNCTION guard_budget_offline_registration();
ALTER TABLE source_budget_offline_authorizations ENABLE ALWAYS TRIGGER source_budget_offline_authorizations_guard;
CREATE TABLE source_budget_offline_imports (
  collection_id uuid PRIMARY KEY REFERENCES source_budget_offline_authorizations(collection_id),
  catalog_id uuid NOT NULL REFERENCES source_budget_catalogs(id),
  bundle_bytes bytea NOT NULL CHECK(octet_length(bundle_bytes) BETWEEN 1 AND 10780672),
  bundle_sha256 text NOT NULL CHECK(bundle_sha256='sha256:'||encode(sha256(bundle_bytes),'hex')),
  source_reading_count integer NOT NULL CHECK(source_reading_count BETWEEN 0 AND 50),
  received_at timestamptz NOT NULL
);
CREATE TRIGGER source_budget_offline_imports_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON source_budget_offline_imports
  FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
ALTER TABLE source_budget_offline_imports ENABLE ALWAYS TRIGGER source_budget_offline_imports_immutable;
