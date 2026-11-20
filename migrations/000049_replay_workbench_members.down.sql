DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM workbench_readings WHERE projection_publication_id IS NOT NULL) THEN
    RAISE EXCEPTION 'schema 49 has versioned Workbench readings; restore the predecessor backup to downgrade';
  END IF;
END $$;

DROP VIEW workbench_reading_members;
DROP VIEW workbench_replayed_reading_facts;
ALTER TABLE workbench_legacy_reading_members RENAME TO workbench_reading_members;

DROP TRIGGER workbench_reading_totals_immutable ON workbench_readings;
DROP FUNCTION guard_workbench_reading_totals();
ALTER TABLE workbench_readings DROP COLUMN calculated_total_micros, DROP COLUMN gateway_total_micros,
  DROP COLUMN summary_scopes, DROP COLUMN projection_publication_id;
DROP TRIGGER evidence_publications_immutable ON evidence_publications;
DROP FUNCTION reject_evidence_publication_mutation();
DROP TRIGGER workbench_projection_publication ON workbench_economic_facts;
DROP FUNCTION guard_workbench_projection_publication();
ALTER TABLE workbench_economic_facts DROP COLUMN publication_id;
