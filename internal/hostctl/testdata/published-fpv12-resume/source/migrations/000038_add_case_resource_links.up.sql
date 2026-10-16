-- Immutable resource citations retain their original record even after reading
-- expiry, withdrawal, a newer revision or a later grant change.
CREATE TABLE case_resource_links (
 case_id text NOT NULL REFERENCES investigation_cases(id) ON DELETE RESTRICT,
 finding_id text,
 source_system_id uuid NOT NULL,
 source_instance text NOT NULL,
 org_id text NOT NULL,
 project_id text NOT NULL,
 record_ref text NOT NULL,
 revision bigint NOT NULL,
 relation text NOT NULL CHECK(relation IN ('supports','contradicts','context')),
 reference_snapshot jsonb NOT NULL CHECK(jsonb_typeof(reference_snapshot)='object'),
 added_by_subject text NOT NULL CHECK(added_by_subject ~ '^xs_[a-z0-9]{16,64}$'),
 added_at timestamptz NOT NULL,
 UNIQUE NULLS NOT DISTINCT(case_id,finding_id,source_system_id,source_instance,org_id,project_id,record_ref,revision,relation),
 FOREIGN KEY(case_id,finding_id) REFERENCES case_findings(case_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(source_system_id,source_instance,org_id,project_id,record_ref,revision)
 REFERENCES resource_access_records(source_system_id,source_instance,org_id,project_id,record_ref,revision) ON DELETE RESTRICT,
 CHECK((ROW(source_system_id::text,source_instance,org_id,project_id,record_ref,revision::text,relation)
 =ROW(reference_snapshot#>>'{reporter,source_system_id}',reference_snapshot#>>'{reporter,source_instance}',reference_snapshot#>>'{reporter,org_id}',reference_snapshot#>>'{reporter,project_id}',reference_snapshot->>'record_ref',reference_snapshot->>'revision',reference_snapshot->>'relation')) IS TRUE)
);
CREATE INDEX case_resource_links_finding ON case_resource_links(case_id,finding_id);
CREATE TRIGGER case_resource_links_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON case_resource_links
 FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
ALTER TABLE case_resource_links ENABLE ALWAYS TRIGGER case_resource_links_immutable;
ALTER TABLE investigation_case_scope_labels DROP CONSTRAINT investigation_case_scope_labels_schema_version_check;
ALTER TABLE investigation_case_scope_labels ADD CONSTRAINT investigation_case_scope_labels_schema_version_check
 CHECK(schema_version IN ('xinsight.case-scope.v1','xinsight.case-scope.v2'));
