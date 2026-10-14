DROP TABLE work_event_relations;
DROP TABLE work_event_documents;
DROP TABLE work_event_ur_targets;
DROP TABLE work_event_ingest_bindings;
DROP TABLE work_event_collections;
DROP FUNCTION guard_work_event_ingest_authority();
DROP FUNCTION guard_work_event_collection();
DROP TRIGGER source_systems_workspace_lock_guard ON source_systems;
DROP FUNCTION guard_workspace_source_status();
ALTER TABLE service_identities DROP CONSTRAINT service_identities_permissions_check;
ALTER TABLE service_identities ADD CONSTRAINT service_identities_permissions_check CHECK (
  (created_by_subject='system' AND permissions=ARRAY['connector.control.poll','connector.control.ack']::text[])
  OR (created_by_subject<>'system' AND cardinality(permissions) BETWEEN 1 AND 2
      AND permissions <@ ARRAY['evidence.read','evidence.cite']::text[])
);
