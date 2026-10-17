-- Content registration, independent grants, fixed reading membership and review references.
-- Forward migration only; no implicit downgrade of nonempty content state.

-- Fold into migration39 before content readers are admitted. No execution here.
ALTER TABLE workbench_access_grants
 ADD COLUMN allow_content boolean NOT NULL DEFAULT false,
 ADD COLUMN allowed_content_purposes jsonb NOT NULL DEFAULT '[]'::jsonb,
 ADD COLUMN legacy_revision bigint NOT NULL DEFAULT 1,
 ADD COLUMN content_revision bigint NOT NULL DEFAULT 0;
-- Preserve the exact historical digest counter; old content has never existed.
UPDATE workbench_access_grants SET legacy_revision=revision;
ALTER TABLE workbench_access_grants ADD CONSTRAINT workbench_content_purposes_check CHECK (
 (NOT allow_content AND allowed_content_purposes='[]'::jsonb) OR
 (allow_content AND allowed_content_purposes='["human_review"]'::jsonb)
);
ALTER TABLE workbench_access_grants ADD CONSTRAINT workbench_domain_revisions_check CHECK (
 legacy_revision > 0 AND legacy_revision <= revision AND content_revision >= 0 AND content_revision <= revision
 AND (NOT allow_content OR content_revision > 0)
);
ALTER TABLE workbench_access_grants DROP CONSTRAINT workbench_access_grants_has_capability_check;
ALTER TABLE workbench_access_grants ADD CONSTRAINT workbench_access_grants_has_capability_check CHECK (
 allow_summary OR allow_details OR allow_coverage OR allow_preferences OR allow_source_runtime OR allow_source_attempt OR allow_source_capability OR allow_source_policy OR allow_source_destination OR allow_resource OR allow_budget_definition OR allow_budget_summary OR allow_budget_alert OR allow_content
);
CREATE FUNCTION advance_content_grant_domains() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE identity_changed boolean; legacy_changed boolean; content_changed boolean;
BEGIN
 IF TG_OP='INSERT' THEN
  -- Keep explicit restored revisions. Fresh C writes cannot claim epoch zero.
  IF NEW.allow_content AND NEW.content_revision=0 THEN NEW.content_revision=1; END IF;
  RETURN NEW;
 END IF;
 identity_changed := ROW(NEW.id,NEW.subject_id,NEW.source_system_id,NEW.source_project_ref,NEW.cost_project_ref,NEW.active)
  IS DISTINCT FROM ROW(OLD.id,OLD.subject_id,OLD.source_system_id,OLD.source_project_ref,OLD.cost_project_ref,OLD.active);
 legacy_changed := identity_changed OR ROW(NEW.allow_summary,NEW.allow_details,NEW.allow_coverage,NEW.allow_preferences,NEW.allow_source_context,NEW.allow_source_identity,NEW.allow_source_cost,NEW.allow_purpose_read,NEW.allow_purpose_review,NEW.allow_source_runtime,NEW.allow_source_attempt,NEW.allow_source_capability,NEW.allow_source_policy,NEW.allow_source_destination,NEW.allow_resource,NEW.allow_budget_definition,NEW.allow_budget_summary,NEW.allow_budget_alert) IS DISTINCT FROM ROW(OLD.allow_summary,OLD.allow_details,OLD.allow_coverage,OLD.allow_preferences,OLD.allow_source_context,OLD.allow_source_identity,OLD.allow_source_cost,OLD.allow_purpose_read,OLD.allow_purpose_review,OLD.allow_source_runtime,OLD.allow_source_attempt,OLD.allow_source_capability,OLD.allow_source_policy,OLD.allow_source_destination,OLD.allow_resource,OLD.allow_budget_definition,OLD.allow_budget_summary,OLD.allow_budget_alert);
 content_changed := identity_changed OR ROW(NEW.allow_content,NEW.allowed_content_purposes)
  IS DISTINCT FROM ROW(OLD.allow_content,OLD.allowed_content_purposes);
 -- An already incremented CLI value does not get incremented a second time.
 IF legacy_changed THEN NEW.legacy_revision=GREATEST(NEW.legacy_revision,OLD.legacy_revision+1);
 ELSE NEW.legacy_revision=GREATEST(NEW.legacy_revision,OLD.legacy_revision); END IF;
 IF content_changed THEN NEW.content_revision=GREATEST(NEW.content_revision,OLD.content_revision+1);
 ELSE NEW.content_revision=GREATEST(NEW.content_revision,OLD.content_revision); END IF;
 IF legacy_changed OR content_changed THEN NEW.revision=GREATEST(NEW.revision,OLD.revision+1);
 ELSE NEW.revision=GREATEST(NEW.revision,OLD.revision); END IF;
 RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION advance_content_grant_domains() FROM PUBLIC;
CREATE TRIGGER workbench_content_grant_domains BEFORE INSERT OR UPDATE ON workbench_access_grants
 FOR EACH ROW EXECUTE FUNCTION advance_content_grant_domains();
ALTER TABLE workbench_access_grants ENABLE ALWAYS TRIGGER workbench_content_grant_domains;

-- Merge into the single successor migration; no legacy registration hash changes.
ALTER TABLE source_fact_management_events DROP CONSTRAINT source_fact_management_events_registration_kind_check;
ALTER TABLE source_fact_management_events ADD CONSTRAINT source_fact_management_events_registration_kind_check CHECK(registration_kind IN ('live','offline','budget','content'));
CREATE TABLE source_content_collections (
 id uuid PRIMARY KEY,
 source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
 source_instance text NOT NULL,
 org_id text NOT NULL,
 project_id text NOT NULL,
 credential_id text NOT NULL,
 registration_hash text NOT NULL CHECK(registration_hash ~ '^sha256:[0-9a-f]{64}$'),
 content_scope_revision text NOT NULL CHECK(length(content_scope_revision) BETWEEN 1 AND 128),
 recovery_epoch text NOT NULL CHECK(recovery_epoch ~ '^[1-9][0-9]{0,18}$'),
 allowed_purposes jsonb NOT NULL CHECK(allowed_purposes='["human_review"]'::jsonb),
 coverage_from jsonb NOT NULL CHECK(jsonb_typeof(coverage_from)='object'),
 retention_floor jsonb NOT NULL DEFAULT '{}'::jsonb,
 coverage text NOT NULL DEFAULT 'unknown',
 gaps jsonb NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(gaps)='array'),
 cursor_value text NOT NULL DEFAULT '',
 last_sequence text NOT NULL DEFAULT '0' CHECK(last_sequence ~ '^(0|[1-9][0-9]{0,18})$' AND last_sequence::numeric<=9223372036854775807),
 high_watermark_sequence text NOT NULL DEFAULT '0' CHECK(high_watermark_sequence ~ '^(0|[1-9][0-9]{0,18})$' AND high_watermark_sequence::numeric<=9223372036854775807 AND high_watermark_sequence::numeric>=last_sequence::numeric),
 high_watermark text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT 'registered' CHECK(status IN ('registered','active','paused','revoked')),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 requires_attention boolean NOT NULL DEFAULT false,
 last_error text NOT NULL DEFAULT '',
 last_attempt_at timestamptz,
 last_success_at timestamptz,
 UNIQUE(source_system_id,source_instance,org_id,project_id,credential_id,registration_hash),
 UNIQUE(id,source_system_id,source_instance,org_id,project_id)
);
CREATE TABLE source_content_manifests (
 collection_id uuid NOT NULL,
 sequence numeric(20,0) NOT NULL CHECK(sequence>0 AND sequence<=9223372036854775807),
 source_system_id uuid NOT NULL,
 source_instance text NOT NULL,
 org_id text NOT NULL,
 project_id text NOT NULL,
 request_id text NOT NULL,
 source_fact_id text NOT NULL CHECK(source_fact_id ~ '^uf1_[a-f0-9]{64}$'),
 revision numeric(20,0) NOT NULL CHECK(revision>0 AND revision<=9223372036854775807),
 event_id text NOT NULL CHECK(event_id ~ '^uf1_[a-f0-9]{64}$'),
 previous_revision numeric(20,0),
 previous_hash text NOT NULL DEFAULT '',
 source_sha256 text NOT NULL CHECK(source_sha256 ~ '^sha256:[a-f0-9]{64}$'),
 source_bytes bytea NOT NULL CHECK(octet_length(source_bytes) BETWEEN 1 AND 65536),
 source_json jsonb NOT NULL CHECK(source_json=convert_from(source_bytes,'UTF8')::jsonb),
 content_scope_revision text NOT NULL,
 recovery_epoch text NOT NULL,
 availability jsonb NOT NULL CHECK(jsonb_typeof(availability)='object'),
 occurred_at timestamptz NOT NULL,
 recorded_at timestamptz NOT NULL,
 received_at timestamptz NOT NULL,
 PRIMARY KEY(source_system_id,source_fact_id,revision),
 UNIQUE(source_system_id,event_id),
 FOREIGN KEY(collection_id,source_system_id,source_instance,org_id,project_id) REFERENCES source_content_collections(id,source_system_id,source_instance,org_id,project_id) ON DELETE RESTRICT,
 CHECK(source_sha256='sha256:'||encode(sha256(source_bytes),'hex')),
 CHECK((revision=1 AND previous_revision IS NULL AND previous_hash='') OR (revision>1 AND previous_revision=revision-1 AND previous_hash ~ '^sha256:[a-f0-9]{64}$')),
 CHECK((source_json->>'schema_version'='unirouter.content-manifest.v1' AND source_json->>'profile'='unirouter.interaction-content.v1' AND source_json->>'source_instance'=source_instance AND source_json->>'org_id'=org_id AND source_json->>'project_id'=project_id AND source_json->>'request_id'=request_id AND source_json->>'source_fact_id'=source_fact_id AND source_json->>'event_id'=event_id AND source_json->>'revision'=revision::text) IS TRUE)
);
CREATE INDEX source_content_manifests_request ON source_content_manifests(source_system_id,source_instance,org_id,project_id,request_id,occurred_at,source_fact_id,revision DESC);
CREATE TRIGGER source_content_manifests_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON source_content_manifests FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
ALTER TABLE source_content_manifests ENABLE ALWAYS TRIGGER source_content_manifests_immutable;
-- Each authenticated receipt is immutable independently of deduplicated facts.
CREATE TABLE source_content_receipts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 collection_id uuid NOT NULL REFERENCES source_content_collections(id) ON DELETE RESTRICT,
 credential_id text NOT NULL CHECK(length(credential_id) BETWEEN 1 AND 128),
 registration_hash text NOT NULL CHECK(registration_hash ~ '^sha256:[0-9a-f]{64}$'),
 content_scope_revision text NOT NULL CHECK(length(content_scope_revision) BETWEEN 1 AND 128),
 recovery_epoch text NOT NULL CHECK(recovery_epoch ~ '^[1-9][0-9]{0,18}$'),
 coverage_from jsonb NOT NULL CHECK(jsonb_typeof(coverage_from)='object'),
 sequence numeric(20,0) NOT NULL CHECK(sequence>0 AND sequence<=9223372036854775807),
 source_system_id uuid NOT NULL,
 source_fact_id text NOT NULL,
 revision numeric(20,0) NOT NULL,
 source_sha256 text NOT NULL CHECK(source_sha256 ~ '^sha256:[0-9a-f]{64}$'),
 availability jsonb NOT NULL CHECK(jsonb_typeof(availability)='object'),
 received_at timestamptz NOT NULL,
 FOREIGN KEY(source_system_id,source_fact_id,revision) REFERENCES source_content_manifests(source_system_id,source_fact_id,revision) ON DELETE RESTRICT,
 UNIQUE(collection_id,credential_id,registration_hash,content_scope_revision,recovery_epoch,coverage_from,sequence),
 UNIQUE(collection_id,credential_id,registration_hash,content_scope_revision,recovery_epoch,coverage_from,source_system_id,source_fact_id,revision)
);
CREATE INDEX source_content_receipts_fact ON source_content_receipts(source_system_id,source_fact_id,revision,received_at);
CREATE TRIGGER source_content_receipts_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON source_content_receipts FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
ALTER TABLE source_content_receipts ENABLE ALWAYS TRIGGER source_content_receipts_immutable;
CREATE UNIQUE INDEX source_content_one_active_scope ON source_content_collections(source_system_id,source_instance,org_id,project_id) WHERE status='active';
CREATE FUNCTION guard_content_collection() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE administrator boolean;
BEGIN
 SELECT pg_has_role(current_user,relowner,'USAGE') INTO administrator FROM pg_class WHERE oid='public.source_content_collections'::regclass;
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'content registration history cannot be deleted'; END IF;
 IF TG_OP='INSERT' THEN
  IF NOT administrator THEN RAISE EXCEPTION 'content registration requires administrator'; END IF;
  RETURN NEW;
 END IF;
 IF ROW(NEW.id,NEW.source_system_id,NEW.source_instance,NEW.org_id,NEW.project_id) IS DISTINCT FROM ROW(OLD.id,OLD.source_system_id,OLD.source_instance,OLD.org_id,OLD.project_id) OR NEW.revision<>OLD.revision+1 OR (OLD.status='revoked' AND NEW.status<>'revoked') THEN RAISE EXCEPTION 'invalid content registration transition'; END IF;
 IF ROW(NEW.credential_id,NEW.registration_hash,NEW.content_scope_revision,NEW.recovery_epoch,NEW.allowed_purposes,NEW.coverage_from) IS DISTINCT FROM ROW(OLD.credential_id,OLD.registration_hash,OLD.content_scope_revision,OLD.recovery_epoch,OLD.allowed_purposes,OLD.coverage_from) AND (NOT administrator OR OLD.status<>'paused' OR NEW.status<>'paused') THEN RAISE EXCEPTION 'content rebinding requires paused administrator action'; END IF;
 IF ROW(NEW.credential_id,NEW.registration_hash,NEW.content_scope_revision,NEW.recovery_epoch,NEW.coverage_from) IS NOT DISTINCT FROM ROW(OLD.credential_id,OLD.registration_hash,OLD.content_scope_revision,OLD.recovery_epoch,OLD.coverage_from) AND (NEW.last_sequence::numeric<OLD.last_sequence::numeric OR NEW.high_watermark_sequence::numeric<OLD.high_watermark_sequence::numeric) THEN RAISE EXCEPTION 'content progress cannot rewind'; END IF;
 IF NOT administrator AND (NEW.status<>OLD.status OR OLD.status<>'active' OR OLD.requires_attention OR (OLD.requires_attention AND NOT NEW.requires_attention)) THEN RAISE EXCEPTION 'content control requires administrator'; END IF;
 RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_content_collection() FROM PUBLIC;
CREATE TRIGGER source_content_collection_management BEFORE INSERT OR UPDATE OR DELETE ON source_content_collections FOR EACH ROW EXECUTE FUNCTION guard_content_collection();
ALTER TABLE source_content_collections ENABLE ALWAYS TRIGGER source_content_collection_management;
-- Validates the immutable historical association, not today's credential state,
-- so restored old receipts remain valid after a later authorized rebind.
CREATE FUNCTION guard_content_receipt_identity() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE administrator boolean;
BEGIN
 SELECT pg_has_role(current_user,relowner,'USAGE') INTO administrator FROM pg_class WHERE oid='public.source_content_receipts'::regclass;
 IF NOT administrator AND NOT EXISTS(SELECT 1 FROM public.source_content_collections c WHERE c.id=NEW.collection_id
 AND c.status='active' AND NOT c.requires_attention
 AND ROW(c.credential_id,c.registration_hash,c.content_scope_revision,c.recovery_epoch,c.coverage_from)=ROW(NEW.credential_id,NEW.registration_hash,NEW.content_scope_revision,NEW.recovery_epoch,NEW.coverage_from)) THEN
 RAISE EXCEPTION 'content receipt requires the current active binding'; END IF;
 IF NOT EXISTS(SELECT 1 FROM public.source_content_manifests d JOIN public.source_content_collections c ON c.id=NEW.collection_id
 WHERE ROW(d.source_system_id,d.source_fact_id,d.revision)=ROW(NEW.source_system_id,NEW.source_fact_id,NEW.revision)
 AND ROW(c.source_system_id,c.source_instance,c.org_id,c.project_id)=ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id)
 AND d.source_sha256=NEW.source_sha256 AND NEW.received_at>=d.received_at
 AND NEW.sequence>(NEW.coverage_from->>'after_sequence')::numeric AND d.occurred_at>=(NEW.coverage_from->>'time')::timestamptz) THEN
 RAISE EXCEPTION 'content receipt differs from its fact or frozen coverage'; END IF;
 RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_content_receipt_identity() FROM PUBLIC;
CREATE TRIGGER source_content_receipt_identity BEFORE INSERT ON source_content_receipts FOR EACH ROW EXECUTE FUNCTION guard_content_receipt_identity();
ALTER TABLE source_content_receipts ENABLE ALWAYS TRIGGER source_content_receipt_identity;

ALTER TABLE runtime_readings ADD COLUMN content_authorization_revision text NOT NULL DEFAULT '' CHECK(content_authorization_revision='' OR content_authorization_revision ~ '^sha256:[0-9a-f]{64}$');
ALTER TABLE runtime_reading_requests ALTER COLUMN last_observed_at DROP NOT NULL;
ALTER TABLE runtime_reading_requests DROP CONSTRAINT runtime_reading_requests_observation_count_check;
ALTER TABLE runtime_reading_requests ADD CONSTRAINT runtime_reading_requests_observation_count_check CHECK(observation_count>=0);
ALTER TABLE runtime_reading_requests ADD COLUMN content_eligible_at timestamptz;
ALTER TABLE runtime_reading_requests ADD CONSTRAINT runtime_request_time_domains CHECK (
 (observation_count>0 AND last_observed_at IS NOT NULL AND content_eligible_at IS NULL) OR
 (observation_count=0 AND entry_at IS NULL AND last_observed_at IS NULL AND content_eligible_at IS NOT NULL)
);
CREATE TABLE runtime_reading_content_members (
 reading_id text NOT NULL,
 request_ref text NOT NULL,
 member_basis text NOT NULL CHECK(member_basis IN ('content_manifest','exact_execution_association')),
 eligible_at timestamptz NOT NULL,
 economic_fact_id uuid REFERENCES workbench_economic_facts(id) ON DELETE RESTRICT,
 collection_id uuid NOT NULL REFERENCES source_content_collections(id) ON DELETE RESTRICT,
 credential_id text NOT NULL CHECK(length(credential_id) BETWEEN 1 AND 128),
 registration_hash text NOT NULL CHECK(registration_hash ~ '^sha256:[0-9a-f]{64}$'),
 content_scope_revision text NOT NULL CHECK(length(content_scope_revision) BETWEEN 1 AND 128),
 recovery_epoch text NOT NULL CHECK(recovery_epoch ~ '^[1-9][0-9]{0,18}$'),
 coverage_from jsonb NOT NULL CHECK(jsonb_typeof(coverage_from)='object'),
 PRIMARY KEY(reading_id,request_ref),
 FOREIGN KEY(reading_id,request_ref) REFERENCES runtime_reading_requests(reading_id,request_ref) ON DELETE CASCADE,
 CHECK(member_basis<>'exact_execution_association' OR economic_fact_id IS NOT NULL)
);
CREATE TABLE runtime_reading_content_refs (
 receipt_id uuid REFERENCES source_content_receipts(id) ON DELETE RESTRICT,
 reading_id text NOT NULL,
 request_ref text NOT NULL,
 collection_id uuid NOT NULL REFERENCES source_content_collections(id) ON DELETE RESTRICT,
 source_system_id uuid NOT NULL,
 source_fact_id text NOT NULL,
 revision numeric(20,0) NOT NULL,
 content_scope_revision text NOT NULL,
 recovery_epoch text NOT NULL,
 frozen_ref jsonb NOT NULL CHECK(jsonb_typeof(frozen_ref)='object'),
 PRIMARY KEY(reading_id,source_system_id,source_fact_id,revision),
 FOREIGN KEY(reading_id,request_ref) REFERENCES runtime_reading_content_members(reading_id,request_ref) ON DELETE CASCADE,
 FOREIGN KEY(source_system_id,source_fact_id,revision) REFERENCES source_content_manifests(source_system_id,source_fact_id,revision) ON DELETE RESTRICT
);
-- Deferred across request/member insertion, checked again after member removal.
CREATE FUNCTION guard_runtime_content_eligibility() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE rid text; req text; pass integer;
BEGIN
 FOR pass IN 1..2 LOOP
  IF pass=1 AND TG_OP<>'DELETE' THEN rid=NEW.reading_id;req=NEW.request_ref;
  ELSIF pass=2 AND TG_OP<>'INSERT' THEN rid=OLD.reading_id;req=OLD.request_ref;
  ELSE CONTINUE; END IF;
  IF EXISTS(SELECT 1 FROM public.runtime_reading_requests q WHERE q.reading_id=rid AND q.request_ref=req AND q.observation_count=0
   AND NOT EXISTS(SELECT 1 FROM public.runtime_reading_content_members m JOIN public.runtime_readings r ON r.id=m.reading_id
   WHERE m.reading_id=q.reading_id AND m.request_ref=q.request_ref AND m.eligible_at=q.content_eligible_at
   AND EXISTS(SELECT 1 FROM public.source_content_collections c WHERE c.id=m.collection_id
    AND ROW(c.source_system_id,c.source_instance,c.org_id,c.project_id)=ROW(q.source_system_id,q.source_instance,q.org_id,q.project_id))
   AND r.content_authorization_revision ~ '^sha256:[0-9a-f]{64}$'
   AND m.eligible_at>=r.period_start AND m.eligible_at<r.period_end
   AND ((m.member_basis='content_manifest' AND EXISTS(
    SELECT 1 FROM public.runtime_reading_content_refs cr JOIN public.source_content_manifests d USING(source_system_id,source_fact_id,revision)
    LEFT JOIN public.source_content_receipts receipt ON receipt.id=cr.receipt_id
    WHERE cr.reading_id=m.reading_id AND cr.request_ref=m.request_ref
    AND ((cr.receipt_id IS NULL AND cr.collection_id=d.collection_id AND cr.content_scope_revision=d.content_scope_revision AND cr.recovery_epoch=d.recovery_epoch)
    OR (receipt.id IS NOT NULL AND receipt.collection_id=cr.collection_id AND receipt.content_scope_revision=cr.content_scope_revision AND receipt.recovery_epoch=cr.recovery_epoch
    AND ROW(receipt.source_system_id,receipt.source_fact_id,receipt.revision)=ROW(d.source_system_id,d.source_fact_id,d.revision)
    AND receipt.credential_id=m.credential_id AND receipt.registration_hash=m.registration_hash AND receipt.coverage_from=m.coverage_from AND receipt.received_at<=r.cutoff))
    AND cr.collection_id=m.collection_id AND cr.content_scope_revision=m.content_scope_revision AND cr.recovery_epoch=m.recovery_epoch
    AND ROW(q.source_system_id,q.source_instance,q.org_id,q.project_id,q.request_id)=ROW(d.source_system_id,d.source_instance,d.org_id,d.project_id,d.request_id)
    AND d.received_at<=r.cutoff AND d.occurred_at=m.eligible_at
    AND cr.frozen_ref->>'source_sha256'=d.source_sha256
   )) OR (m.member_basis='exact_execution_association' AND EXISTS(
    SELECT 1 FROM public.workbench_economic_facts f JOIN public.evidence_events e ON e.id=f.evidence_event_id
    WHERE f.id=m.economic_fact_id AND f.occurred_at=m.eligible_at AND f.projected_at<=r.cutoff AND e.ingested_at<=r.cutoff
    AND f.source_system_id=q.source_system_id AND f.source_version='unirouter.completed-execution.v1'
    AND f.source_ref='unirouter:'||q.source_instance||':completed_execution:'||q.request_id
    AND f.request_id=q.request_id AND f.source_project_ref='project-'||q.project_id AND e.org_ref='org-'||q.org_id
   ))))) THEN RAISE EXCEPTION 'zero-observation request requires valid content eligibility'; END IF;
 END LOOP;
 RETURN NULL;
END $$;
REVOKE ALL ON FUNCTION guard_runtime_content_eligibility() FROM PUBLIC;
CREATE CONSTRAINT TRIGGER runtime_content_request_eligibility AFTER INSERT OR UPDATE ON runtime_reading_requests DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION guard_runtime_content_eligibility();
ALTER TABLE runtime_reading_requests ENABLE ALWAYS TRIGGER runtime_content_request_eligibility;
CREATE CONSTRAINT TRIGGER runtime_content_member_eligibility AFTER INSERT OR UPDATE OR DELETE ON runtime_reading_content_members DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION guard_runtime_content_eligibility();
ALTER TABLE runtime_reading_content_members ENABLE ALWAYS TRIGGER runtime_content_member_eligibility;
CREATE CONSTRAINT TRIGGER runtime_content_ref_eligibility AFTER INSERT OR UPDATE OR DELETE ON runtime_reading_content_refs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION guard_runtime_content_eligibility();
ALTER TABLE runtime_reading_content_refs ENABLE ALWAYS TRIGGER runtime_content_ref_eligibility;

-- Merge into migration 39 after source_content_manifests. No content bytes.
CREATE TABLE case_content_links (
 case_id text NOT NULL REFERENCES investigation_cases(id) ON DELETE RESTRICT,
 finding_id text,
 source_system_id uuid NOT NULL,
 source_instance text NOT NULL,
 org_id text NOT NULL,
 project_id text NOT NULL,
 request_id text NOT NULL,
 source_fact_id text NOT NULL,
 revision numeric(20,0) NOT NULL CHECK(revision BETWEEN 1 AND 9223372036854775807),
 source_sha256 text NOT NULL CHECK(source_sha256 ~ '^sha256:[a-f0-9]{64}$'),
 relation text NOT NULL CHECK(relation IN ('supports','contradicts','context')),
 choice_sha256 text NOT NULL CHECK(choice_sha256 ~ '^sha256:[a-f0-9]{64}$'),
 reference_snapshot jsonb NOT NULL CHECK(jsonb_typeof(reference_snapshot)='object'),
 added_by_subject text NOT NULL CHECK(added_by_subject ~ '^xs_[a-z0-9]{16,64}$'),
 added_at timestamptz NOT NULL,
 UNIQUE NULLS NOT DISTINCT(case_id,finding_id,choice_sha256),
 FOREIGN KEY(case_id,finding_id) REFERENCES case_findings(case_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(source_system_id,source_fact_id,revision)
  REFERENCES source_content_manifests(source_system_id,source_fact_id,revision) ON DELETE RESTRICT,
 CHECK((ROW(source_system_id::text,source_instance,org_id,project_id,request_id,source_fact_id,revision::text,source_sha256,relation)
  =ROW(reference_snapshot#>>'{reference,source_system_id}',reference_snapshot#>>'{reference,source_instance}',reference_snapshot#>>'{reference,org_id}',reference_snapshot#>>'{reference,project_id}',reference_snapshot#>>'{reference,request_id}',reference_snapshot#>>'{reference,source_fact_id}',reference_snapshot#>>'{reference,revision}',reference_snapshot#>>'{reference,source_sha256}',reference_snapshot->>'relation')) IS TRUE),
 CHECK((reference_snapshot->>'scope'=CASE WHEN finding_id IS NULL THEN 'case' ELSE 'finding' END) IS TRUE),
 CHECK((reference_snapshot#>>'{reference,capture_status}' IN ('complete','partial')) IS TRUE)
);
CREATE INDEX case_content_links_finding ON case_content_links(case_id,finding_id);
CREATE TRIGGER case_content_links_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON case_content_links
 FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
ALTER TABLE case_content_links ENABLE ALWAYS TRIGGER case_content_links_immutable;
ALTER TABLE investigation_case_scope_labels DROP CONSTRAINT investigation_case_scope_labels_schema_version_check;
ALTER TABLE investigation_case_scope_labels ADD CONSTRAINT investigation_case_scope_labels_schema_version_check
 CHECK(schema_version IN ('xinsight.case-scope.v1','xinsight.case-scope.v2','xinsight.case-scope.v3'));
