ALTER TABLE case_executions DROP CONSTRAINT case_executions_reference_key;
-- Rollback refuses to collapse multiple versions of the same reference.
ALTER TABLE case_executions ADD PRIMARY KEY (case_id, execution_id, as_of);
ALTER TABLE case_executions DROP COLUMN evidence_revision;
ALTER TABLE import_provenance_events DROP COLUMN publication_id;
ALTER TABLE import_provenance DROP COLUMN publication_id;
ALTER TABLE capture_gaps DROP COLUMN publication_id;
ALTER TABLE evidence_events DROP COLUMN publication_id;
DROP TABLE evidence_publications;
