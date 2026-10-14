ALTER TABLE source_fact_streams ADD COLUMN requires_attention boolean NOT NULL DEFAULT false;

CREATE TABLE source_fact_offline_authorizations (
  id text PRIMARY KEY CHECK(id ~ '^[a-z0-9][a-z0-9._:-]{0,127}$'),
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  source_instance text NOT NULL CHECK(length(source_instance) BETWEEN 1 AND 128),
  org_id text NOT NULL CHECK(org_id ~ '^[1-9][0-9]{0,18}$'),
  project_id text NOT NULL CHECK(project_id ~ '^[1-9][0-9]{0,18}$'),
  profile text NOT NULL CHECK(profile IN ('unirouter.execution-context.v1','unirouter.execution-identity.v1','unirouter.cost-basis.v1')),
  authorization_sha256 text NOT NULL CHECK(authorization_sha256 ~ '^sha256:[0-9a-f]{64}$'),
  target_ref text NOT NULL CHECK(length(target_ref) BETWEEN 1 AND 128),
  product_version text NOT NULL CHECK(length(product_version) BETWEEN 1 AND 128),
  product_commit text NOT NULL CHECK(length(product_commit) BETWEEN 1 AND 128),
  not_before timestamptz NOT NULL,
  expires_at timestamptz NOT NULL CHECK(expires_at>not_before),
  status text NOT NULL DEFAULT 'disabled' CHECK(status IN ('active','disabled','revoked')),
  revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE source_fact_management_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  registration_kind text NOT NULL CHECK(registration_kind IN ('live','offline')),
  registration_id text NOT NULL,
  revision bigint NOT NULL CHECK(revision>0),
  action text NOT NULL CHECK(action IN ('register','state_change','acknowledge')),
  status text NOT NULL,
  actor text NOT NULL,
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE FUNCTION guard_source_management_event() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
  RAISE EXCEPTION 'source management history is append-only';
END $$;
REVOKE ALL ON FUNCTION guard_source_management_event() FROM PUBLIC;
CREATE TRIGGER source_fact_management_events_immutable BEFORE UPDATE OR DELETE ON source_fact_management_events
  FOR EACH ROW EXECUTE FUNCTION guard_source_management_event();
ALTER TABLE source_fact_management_events ENABLE ALWAYS TRIGGER source_fact_management_events_immutable;

CREATE FUNCTION guard_managed_source_stream() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE administrator boolean;
BEGIN
  SELECT pg_has_role(current_user,relowner,'USAGE') INTO administrator
    FROM pg_class WHERE oid='public.source_fact_streams'::regclass;
  IF TG_OP='UPDATE' THEN
    IF OLD.requires_attention AND NOT NEW.requires_attention AND NOT administrator THEN
      RAISE EXCEPTION 'source attention requires administrator acknowledgement';
    END IF;
    IF NEW.status<>OLD.status OR (OLD.requires_attention AND NOT NEW.requires_attention) THEN
      IF NOT administrator OR NEW.revision<>OLD.revision+1 THEN
        RAISE EXCEPTION 'source management requires administrator and next revision';
      END IF;
    END IF;
    IF NOT administrator AND OLD.requires_attention AND
       ROW(NEW.cursor_value,NEW.high_watermark,NEW.last_error) IS DISTINCT FROM
       ROW(OLD.cursor_value,OLD.high_watermark,OLD.last_error) THEN
      RAISE EXCEPTION 'source requiring attention cannot advance or clear its observation';
    END IF;
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_managed_source_stream() FROM PUBLIC;
CREATE TRIGGER source_fact_stream_management BEFORE UPDATE ON source_fact_streams
  FOR EACH ROW EXECUTE FUNCTION guard_managed_source_stream();
ALTER TABLE source_fact_streams ENABLE ALWAYS TRIGGER source_fact_stream_management;

CREATE FUNCTION guard_source_offline_authorization() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
	IF NOT pg_has_role(current_user,(SELECT relowner FROM pg_class WHERE oid='public.source_fact_offline_authorizations'::regclass),'USAGE') THEN
		RAISE EXCEPTION 'offline authorization changes require administrator';
	END IF;
  IF TG_OP='DELETE' THEN
    RAISE EXCEPTION 'offline authorization history cannot be deleted';
  END IF;
  IF ROW(NEW.id,NEW.source_system_id,NEW.source_instance,NEW.org_id,NEW.project_id,NEW.profile,
         NEW.authorization_sha256,NEW.target_ref,NEW.product_version,NEW.product_commit,NEW.not_before,NEW.expires_at,NEW.created_at)
     IS DISTINCT FROM
     ROW(OLD.id,OLD.source_system_id,OLD.source_instance,OLD.org_id,OLD.project_id,OLD.profile,
         OLD.authorization_sha256,OLD.target_ref,OLD.product_version,OLD.product_commit,OLD.not_before,OLD.expires_at,OLD.created_at)
     OR (OLD.status='revoked' AND NEW.status<>'revoked')
     OR NEW.revision<>OLD.revision+1 THEN
    RAISE EXCEPTION 'offline authority changes require a new registration or next status revision';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_source_offline_authorization() FROM PUBLIC;
CREATE TRIGGER source_fact_offline_authorization_identity BEFORE UPDATE OR DELETE ON source_fact_offline_authorizations
  FOR EACH ROW EXECUTE FUNCTION guard_source_offline_authorization();
ALTER TABLE source_fact_offline_authorizations ENABLE ALWAYS TRIGGER source_fact_offline_authorization_identity;
