CREATE TABLE evidence_publications (
  snapshot_id uuid PRIMARY KEY,
  revision bigint NOT NULL UNIQUE CHECK (revision >= 0)
);

-- Rows from the predecessor have no publication_id and belong to the baseline.
INSERT INTO evidence_publications (snapshot_id, revision) VALUES (gen_random_uuid(), 0);

ALTER TABLE evidence_events ADD COLUMN publication_id uuid REFERENCES evidence_publications DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE capture_gaps ADD COLUMN publication_id uuid REFERENCES evidence_publications DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE import_provenance ADD COLUMN publication_id uuid REFERENCES evidence_publications DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE import_provenance_events ADD COLUMN publication_id uuid REFERENCES evidence_publications DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE evidence_events ALTER COLUMN publication_id SET DEFAULT NULLIF(current_setting('xinsight.evidence_publication_id', true), '')::uuid;
ALTER TABLE capture_gaps ALTER COLUMN publication_id SET DEFAULT NULLIF(current_setting('xinsight.evidence_publication_id', true), '')::uuid;
ALTER TABLE import_provenance ALTER COLUMN publication_id SET DEFAULT NULLIF(current_setting('xinsight.evidence_publication_id', true), '')::uuid;
ALTER TABLE import_provenance_events ALTER COLUMN publication_id SET DEFAULT NULLIF(current_setting('xinsight.evidence_publication_id', true), '')::uuid;

ALTER TABLE case_executions ADD COLUMN evidence_revision uuid REFERENCES evidence_publications;
ALTER TABLE case_executions DROP CONSTRAINT case_executions_pkey;
ALTER TABLE case_executions ADD CONSTRAINT case_executions_reference_key
  UNIQUE NULLS NOT DISTINCT (case_id, execution_id, as_of, evidence_revision);
