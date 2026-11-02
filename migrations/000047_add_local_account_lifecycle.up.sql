-- Local people and their credentials have one durable authority. Keep secrets
-- outside public: existing evidence reader/importer SELECT grants are public-only.
CREATE SCHEMA xinsight_accounts;
REVOKE ALL ON SCHEMA xinsight_accounts FROM PUBLIC;
CREATE TABLE xinsight_accounts.authority (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 initialized boolean NOT NULL DEFAULT false,
 legacy_sha256 text NOT NULL DEFAULT '',
 initialized_at timestamptz,
 ticket_generation uuid NOT NULL DEFAULT gen_random_uuid(),
 last_restore_operation text NOT NULL DEFAULT ''
);
INSERT INTO xinsight_accounts.authority(singleton) VALUES(true);
CREATE TABLE xinsight_accounts.accounts (
 subject_id text PRIMARY KEY CHECK(subject_id ~ '^xs_[a-z0-9]{16,64}$'),
 username text NOT NULL UNIQUE CHECK(username ~ '^[a-z0-9][a-z0-9._-]{2,63}$'),
 role text NOT NULL CHECK(role IN ('investigator','operator','access_operator','platform_admin','workbench_reader','investigation_reader','workbench_investigation_reader','workbench_investigation_reviewer','workbench_purpose_reviewer')),
 disabled boolean NOT NULL DEFAULT false,
 needs_activation boolean NOT NULL DEFAULT false,
 password_hash text,
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(needs_activation OR password_hash IS NOT NULL)
);
CREATE TABLE xinsight_accounts.password_tickets (
 subject_id text PRIMARY KEY REFERENCES xinsight_accounts.accounts(subject_id),
 verifier_sha256 text NOT NULL UNIQUE CHECK(verifier_sha256 ~ '^[a-f0-9]{64}$'),
 purpose text NOT NULL CHECK(purpose IN ('activation','recovery')),
 account_revision bigint NOT NULL CHECK(account_revision>0),
 expires_at timestamptz NOT NULL,
 consumed_at timestamptz,
 created_by text NOT NULL,
 ticket_generation uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
REVOKE ALL ON ALL TABLES IN SCHEMA xinsight_accounts FROM PUBLIC;
-- This is the product workspace writer, never the evidence reader/importer.
-- It owns no DDL, root files, importer authority or raw grant-table writes.
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='xinsight_workspace') THEN
 GRANT USAGE ON SCHEMA xinsight_accounts TO xinsight_workspace;
 GRANT SELECT ON xinsight_accounts.authority TO xinsight_workspace;
 GRANT SELECT,INSERT,UPDATE ON xinsight_accounts.accounts,xinsight_accounts.password_tickets TO xinsight_workspace;
END IF; END $$;

-- Narrow project-metadata grant control for the native account page. A live
-- owner is rechecked inside the transaction; no wildcard, body or advanced grant
-- can be created or overwritten here. Existing grant locking/domain triggers run.
CREATE FUNCTION xinsight_accounts.set_project_read_grant(
 p_actor text,p_actor_revision bigint,p_subject text,p_source uuid,p_project text,
 p_id uuid,p_expected bigint,p_capabilities text[],p_revoke boolean,p_check boolean)
RETURNS jsonb LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $$
DECLARE g public.workbench_access_grants; before_row jsonb; result_row jsonb;
BEGIN
 -- Same order as scoped publication: grant table/domain before account guard.
 IF p_check THEN
  LOCK TABLE public.workbench_access_grants IN ACCESS SHARE MODE;
  PERFORM pg_advisory_xact_lock_shared(6361703472894002481);
 ELSE
  LOCK TABLE public.workbench_access_grants IN ROW EXCLUSIVE MODE;
  PERFORM pg_advisory_xact_lock(6361703472894002481);
 END IF;
 PERFORM pg_advisory_xact_lock_shared(6361703472894002482);
 IF NOT EXISTS(SELECT 1 FROM xinsight_accounts.authority WHERE singleton AND initialized) OR NOT EXISTS(SELECT 1 FROM xinsight_accounts.accounts WHERE subject_id=p_actor AND role='platform_admin' AND NOT disabled AND NOT needs_activation AND revision=p_actor_revision) THEN
  RAISE EXCEPTION USING ERRCODE='42501',MESSAGE='account_management_denied'; END IF;
 IF p_project IS NULL OR p_project='' OR p_project='*' OR length(p_project)>128 OR p_expected<0
 OR NOT ARRAY['details','coverage']::text[] <@ p_capabilities
 OR p_capabilities IS NULL OR cardinality(p_capabilities)=0 OR NOT p_capabilities <@ ARRAY['summary','details','coverage','source_context','source_identity','source_cost','source_runtime','source_attempt','source_capability','source_policy','source_destination']::text[] THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='invalid_project_metadata_scope'; END IF;
 IF NOT EXISTS(SELECT 1 FROM xinsight_accounts.accounts WHERE subject_id=p_subject AND NOT disabled AND role IN ('platform_admin','workbench_reader','workbench_investigation_reader','workbench_investigation_reviewer')) THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='invalid_project_reader'; END IF;
 IF NOT EXISTS(SELECT 1 FROM public.source_systems WHERE id=p_source) THEN
  RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='source_not_registered'; END IF;
 IF NOT EXISTS(SELECT 1 FROM public.source_fact_streams WHERE source_system_id=p_source AND 'project-'||project_id=p_project)
 AND NOT EXISTS(SELECT 1 FROM public.source_content_collections WHERE source_system_id=p_source AND 'project-'||project_id=p_project)
 AND NOT EXISTS(SELECT 1 FROM public.workbench_economic_facts WHERE source_system_id=p_source AND source_project_ref=p_project)
 AND NOT EXISTS(SELECT 1 FROM public.workbench_access_grants WHERE source_system_id=p_source AND source_project_ref=p_project) THEN
 RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='project_not_registered_for_source'; END IF;
 SELECT * INTO g FROM public.workbench_access_grants WHERE id=p_id;
 IF FOUND THEN
  IF g.revision<>p_expected OR g.subject_id<>p_subject OR g.source_system_id<>p_source OR g.source_project_ref IS DISTINCT FROM p_project OR g.cost_project_ref IS NOT NULL THEN
   RAISE EXCEPTION USING ERRCODE='40001',MESSAGE='grant_revision_or_scope_changed'; END IF;
  IF g.allow_content OR g.allow_purpose_read OR g.allow_purpose_review OR g.allow_resource OR g.allow_budget_definition OR g.allow_budget_summary OR g.allow_budget_alert OR g.allow_preferences THEN
   RAISE EXCEPTION USING ERRCODE='22023',MESSAGE='advanced_grant_requires_existing_native_authority'; END IF;
  before_row:=to_jsonb(g);
 ELSE
  IF p_expected<>0 OR p_revoke THEN RAISE EXCEPTION USING ERRCODE='40001',MESSAGE='grant_not_found'; END IF;
  IF EXISTS(SELECT 1 FROM public.workbench_access_grants WHERE subject_id=p_subject AND source_system_id=p_source AND source_project_ref=p_project AND cost_project_ref IS NULL) THEN
   RAISE EXCEPTION USING ERRCODE='40001',MESSAGE='grant_scope_already_exists'; END IF;
 END IF;
 IF p_check THEN RETURN jsonb_build_object('check_only',true,'changed',false,'before',before_row,'subject_id',p_subject,'source_system_id',p_source,'source_project_ref',p_project,'capabilities',p_capabilities,'allow_content',false,'revoke',p_revoke); END IF;
 IF before_row IS NULL THEN
  INSERT INTO public.workbench_access_grants(id,subject_id,source_system_id,source_project_ref,cost_project_ref,allow_summary,allow_details,allow_coverage,allow_preferences,active,revision,allow_source_context,allow_source_identity,allow_source_cost,allow_source_runtime,allow_source_attempt,allow_source_capability,allow_source_policy,allow_source_destination)
   VALUES(p_id,p_subject,p_source,p_project,NULL,'summary'=ANY(p_capabilities),'details'=ANY(p_capabilities),'coverage'=ANY(p_capabilities),false,true,1,'source_context'=ANY(p_capabilities),'source_identity'=ANY(p_capabilities),'source_cost'=ANY(p_capabilities),'source_runtime'=ANY(p_capabilities),'source_attempt'=ANY(p_capabilities),'source_capability'=ANY(p_capabilities),'source_policy'=ANY(p_capabilities),'source_destination'=ANY(p_capabilities));
 END IF;
 UPDATE public.workbench_access_grants SET active=NOT p_revoke,
  allow_summary='summary'=ANY(p_capabilities),allow_details='details'=ANY(p_capabilities),allow_coverage='coverage'=ANY(p_capabilities),
  allow_source_context='source_context'=ANY(p_capabilities),allow_source_identity='source_identity'=ANY(p_capabilities),allow_source_cost='source_cost'=ANY(p_capabilities),
  allow_source_runtime='source_runtime'=ANY(p_capabilities),allow_source_attempt='source_attempt'=ANY(p_capabilities),allow_source_capability='source_capability'=ANY(p_capabilities),
  allow_source_policy='source_policy'=ANY(p_capabilities),allow_source_destination='source_destination'=ANY(p_capabilities),
  revision=CASE WHEN before_row IS NULL THEN revision ELSE revision+1 END,updated_at=clock_timestamp()
 WHERE id=p_id RETURNING to_jsonb(workbench_access_grants) INTO result_row;
 INSERT INTO public.access_audit_records(id,occurred_at,actor_kind,actor_ref,action,resource_kind,resource_ref,result,correlation_id,details)
 VALUES('xaa_'||translate(rtrim(encode(uuid_send(gen_random_uuid()),'base64'),'='),'+/','-_'),clock_timestamp(),'human',p_actor,'access.project_metadata_grant','workbench_grant',p_id::text,'accepted','xa_'||replace(p_id::text,'-',''),jsonb_build_object('before',before_row,'after',result_row));
 RETURN jsonb_build_object('check_only',false,'changed',true,'before',before_row,'after',result_row);
END $$;
REVOKE ALL ON FUNCTION xinsight_accounts.set_project_read_grant(text,bigint,text,uuid,text,uuid,bigint,text[],boolean,boolean) FROM PUBLIC;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='xinsight_workspace') THEN
 GRANT EXECUTE ON FUNCTION xinsight_accounts.set_project_read_grant(text,bigint,text,uuid,text,uuid,bigint,text[],boolean,boolean) TO xinsight_workspace;
END IF; END $$;
