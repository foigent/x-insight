DROP INDEX IF EXISTS idx_capture_gaps_execution_snapshot;
DROP INDEX IF EXISTS idx_evidence_events_search_effective;
DROP INDEX IF EXISTS idx_evidence_events_execution_snapshot;
DROP INDEX IF EXISTS idx_evidence_events_execution_id_snapshot;
DROP INDEX IF EXISTS idx_evidence_events_import_provenance;
DROP INDEX IF EXISTS idx_import_provenance_source_imported;

ALTER TABLE evidence_events DROP COLUMN IF EXISTS import_provenance_id;

ALTER TABLE evidence_events DROP COLUMN IF EXISTS execution_id;

DROP TABLE IF EXISTS import_provenance;
