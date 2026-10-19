-- Preserve existing automation. Only connections created after this migration
-- default to manual. NULL cadence means preserve the worker's existing interval.
CREATE TABLE connector_sync_controls (
 installation_id text PRIMARY KEY REFERENCES connector_installations(id) ON DELETE RESTRICT,
 auto_sync boolean NOT NULL DEFAULT false,
 setup_used boolean NOT NULL DEFAULT false,
 cadence_seconds integer CHECK(cadence_seconds BETWEEN 5 AND 3600),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 request_id text NOT NULL DEFAULT '', request_body jsonb,
 updated_by text NOT NULL DEFAULT '', updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 last_checked_at timestamptz, last_data_at timestamptz,
 last_error text NOT NULL DEFAULT '' CHECK(length(last_error)<=64)
);
INSERT INTO connector_sync_controls(installation_id,auto_sync) SELECT id,true FROM connector_installations;
CREATE FUNCTION public.xinsight_init_connector_sync() RETURNS trigger LANGUAGE plpgsql
 SECURITY DEFINER SET search_path=pg_catalog AS $$ BEGIN
 INSERT INTO public.connector_sync_controls(installation_id) VALUES(NEW.id); RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION public.xinsight_init_connector_sync() FROM PUBLIC;
CREATE TRIGGER connector_sync_default AFTER INSERT ON connector_installations
 FOR EACH ROW EXECUTE FUNCTION public.xinsight_init_connector_sync();

-- One reusable slot per connection/worker class, not one row per HTTP request.
CREATE TABLE connector_sync_slots (
 installation_id text NOT NULL REFERENCES connector_sync_controls(installation_id),
 worker text NOT NULL CHECK(worker IN ('feed','source','activation','projects','credentials')),
 owner text NOT NULL CHECK(length(owner)<=256), token text NOT NULL CHECK(length(token)=32),
 revision bigint NOT NULL, busy boolean NOT NULL, started_at timestamptz NOT NULL,
 ended_at timestamptz, last_error text NOT NULL DEFAULT '',
 PRIMARY KEY(installation_id,worker)
);
CREATE TABLE connector_sync_runs (
 installation_id text NOT NULL REFERENCES connector_sync_controls(installation_id),
 request_id text NOT NULL, revision bigint NOT NULL,
 started_at timestamptz NOT NULL, deadline timestamptz NOT NULL,
 pages integer NOT NULL DEFAULT 0 CHECK(pages BETWEEN 0 AND 32),
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 64),
 state text NOT NULL CHECK(state IN ('running','completed','empty','partial','error','cancelled','expired','interrupted')),
 observed_items bigint NOT NULL DEFAULT 0 CHECK(observed_items>=0),
 results jsonb NOT NULL DEFAULT '{}'::jsonb CHECK(jsonb_typeof(results)='object' AND octet_length(results::text)<=16384),
 updated_by text NOT NULL,
 PRIMARY KEY(installation_id,request_id),
 CHECK(deadline>started_at AND deadline<=started_at+interval '60 seconds')
);
CREATE UNIQUE INDEX connector_sync_one_run ON connector_sync_runs(installation_id) WHERE state='running';
REVOKE ALL ON connector_sync_controls,connector_sync_slots,connector_sync_runs FROM PUBLIC;

-- Expose only the verified ownership projection to importer. Never infer a
-- connection by endpoint or name, and reject ambiguous ownership in the caller.
CREATE FUNCTION public.xinsight_sync_scope(p_source text,p_org text,p_project text,p_feed boolean)
 RETURNS SETOF text LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT DISTINCT i.id FROM public.connector_installations i
 JOIN public.connector_installation_source_bindings b ON b.installation_id=i.id
 WHERE i.source_instance=p_source AND i.desired_state='active' AND i.status='connected'
 AND ((NOT p_feed AND EXISTS(SELECT 1 FROM public.connector_source_enrollments e
   WHERE e.installation_id=i.id AND e.source_instance=p_source AND e.org_id=p_org
   AND e.project_id=p_project AND e.status IN ('active','requires_migration')))
 OR (p_feed AND EXISTS(SELECT 1 FROM public.adapter_import_states s
   WHERE s.activation_source_instance=p_source AND s.cursor_key='unirouter.completed-execution.v1'
   AND (s.metadata->>'installation_id'=i.id OR EXISTS(SELECT 1 FROM public.connector_receiver_adoptions a
     WHERE a.installation_id=i.id AND a.checkpoint_state_id=s.id AND a.status='confirmed')))));
$$;
REVOKE ALL ON FUNCTION public.xinsight_sync_scope(text,text,text,boolean) FROM PUBLIC;

CREATE FUNCTION public.xinsight_sync_receiver(p_verifier text)
 RETURNS SETOF text LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 SELECT DISTINCT i.id FROM public.connector_installations i
 JOIN public.connector_installation_source_bindings b ON b.installation_id=i.id
 JOIN public.connector_control_operations o ON o.installation_id=i.id
 WHERE i.desired_state='active' AND i.status IN ('pending','connected') AND o.status='completed'
 AND ((o.phase='register_receiver' AND o.request_payload->'request'->'receiver_registration'->>'verifier_sha256'=p_verifier)
 OR (o.phase='bind_existing_receiver' AND o.request_payload->'request'->'candidate'->'receiver_registration'->>'verifier_sha256'=p_verifier));
$$;
REVOKE ALL ON FUNCTION public.xinsight_sync_receiver(text) FROM PUBLIC;
-- Applicability comes from verified, retained bindings and registrations, not a
-- fixed assumption that every connection has all worker classes.
CREATE FUNCTION public.xinsight_sync_units(p_installation text)
 RETURNS TABLE(unit text,worker text) LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog AS $$
 WITH bindings AS (SELECT b.installation_id FROM public.connector_installation_source_bindings b
 JOIN public.connector_installations i ON i.id=b.installation_id
 WHERE i.id=p_installation AND i.desired_state='active' AND i.status IN ('pending','connected')),
 scopes AS (SELECT DISTINCT e.source_instance,e.org_id,e.project_id
 FROM public.connector_source_enrollments e JOIN bindings b USING(installation_id)
 WHERE e.installation_id=p_installation AND e.status IN ('active','requires_migration'))
 SELECT 'activation','activation' FROM bindings
 UNION SELECT 'projects','projects' FROM bindings
 UNION SELECT 'feed','feed' FROM public.connector_installations i WHERE i.id=p_installation AND EXISTS(SELECT 1 FROM bindings)
 AND (EXISTS(SELECT 1 FROM public.xinsight_sync_scope(i.source_instance,'','',true) x WHERE x=p_installation) OR EXISTS(SELECT 1 FROM public.connector_control_operations o WHERE o.installation_id=i.id AND o.status='completed' AND o.phase IN ('register_receiver','bind_existing_receiver')))
 UNION SELECT 'facts:'||s.id::text,'source' FROM public.source_fact_streams s JOIN scopes e USING(source_instance,org_id,project_id) WHERE s.status<>'revoked'
 UNION SELECT 'runtime:'||s.id::text,'source' FROM public.source_fact_streams s JOIN scopes e USING(source_instance,org_id,project_id) WHERE s.status<>'revoked' AND s.profile='unirouter.runtime-observation.v1'
 UNION SELECT 'budget:'||s.id::text,'source' FROM public.source_budget_collections s JOIN scopes e USING(source_instance,org_id,project_id) WHERE s.status<>'revoked' AND s.transport='live'
 UNION SELECT 'content:'||s.id::text,'source' FROM public.source_content_collections s JOIN scopes e USING(source_instance,org_id,project_id) WHERE s.status<>'revoked';
$$;
REVOKE ALL ON FUNCTION public.xinsight_sync_units(text) FROM PUBLIC;
DO $$ DECLARE r text; BEGIN
 FOREACH r IN ARRAY ARRAY['xinsight_workspace','xinsight_importer'] LOOP
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname=r) THEN
 EXECUTE format('GRANT SELECT ON connector_sync_controls,connector_sync_slots,connector_sync_runs TO %I',r);
 EXECUTE format('GRANT EXECUTE ON FUNCTION public.xinsight_sync_scope(text,text,text,boolean) TO %I',r);
 EXECUTE format('GRANT EXECUTE ON FUNCTION public.xinsight_sync_units(text) TO %I',r);
 EXECUTE format('GRANT EXECUTE ON FUNCTION public.xinsight_sync_receiver(text) TO %I',r);
 END IF; END LOOP;
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='xinsight_workspace') THEN
 -- The custody role locks the same live permit and records successful data
 -- time; it cannot change Auto, cadence, revision, slots or run admission.
 GRANT UPDATE(last_data_at) ON connector_sync_controls TO xinsight_workspace;
 END IF;
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='xinsight_importer') THEN
 GRANT UPDATE(last_checked_at,last_data_at,last_error) ON connector_sync_controls TO xinsight_importer;
 GRANT INSERT ON connector_sync_slots TO xinsight_importer;
 GRANT UPDATE(owner,token,revision,busy,started_at,ended_at,last_error) ON connector_sync_slots TO xinsight_importer;
 GRANT UPDATE(pages,attempts,state,results,observed_items) ON connector_sync_runs TO xinsight_importer;
 END IF;
END $$;
