-- Preserve schema24 identities and every snapshotted value before removing copies.
-- RunMigrations executes this whole migration in a transaction; any failure keeps
-- the old tables intact and leaves the migration ledger dirty for investigation.
LOCK TABLE workbench_economic_facts, workbench_readings, workbench_reading_members IN ACCESS EXCLUSIVE MODE;
LOCK TABLE evidence_events IN SHARE MODE;
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM workbench_reading_members m
    LEFT JOIN workbench_economic_facts f ON f.id=m.economic_fact_id
    LEFT JOIN evidence_events e ON e.id=f.evidence_event_id
    WHERE f.id IS NULL OR e.id IS NULL OR
      ROW(m.evidence_event_id, m.source_system_id, m.source_version, m.source_event_hash, m.event_hash, m.request_id, m.execution_id, m.model_ref, m.source_project_ref, m.cost_project_ref, m.terminal_status, m.occurred_at, m.gateway_charged_micros, m.calculated_reference_micros, m.currency)
      IS DISTINCT FROM ROW(f.evidence_event_id, f.source_system_id, f.source_version, f.source_event_hash, e.event_hash, f.request_id, f.execution_id, f.model_ref, f.source_project_ref, f.cost_project_ref, f.terminal_status, f.occurred_at, f.gateway_charged_micros, f.calculated_reference_micros, f.currency)
  ) THEN
    RAISE EXCEPTION 'workbench schema25: member differs from anchored fact/Evidence';
  END IF;
  IF EXISTS (
    SELECT 1 FROM workbench_readings r
    WHERE r.interpretation_revision <> 'sha256:' || encode(digest(
      'source_project=direct:project_ref;cost_project=unavailable;account=unknown;application=unknown;purpose=unknown','sha256'),'hex')
    OR r.membership_sha256 IS DISTINCT FROM (
      SELECT 'sha256:' || encode(digest(convert_to(COALESCE(string_agg(
        m.economic_fact_id::text || ':' || m.source_event_hash || ':' || m.event_hash,
        E'\n' ORDER BY m.ordinal),''),'UTF8'),'sha256'),'hex')
      FROM workbench_reading_members m WHERE m.reading_id=r.id
    )
    OR (r.coverage->>'known_items')::bigint IS DISTINCT FROM (
      SELECT count(*) FROM workbench_reading_members m WHERE m.reading_id=r.id
    )
  ) THEN
    RAISE EXCEPTION 'workbench schema25: unsupported interpretation or invalid membership';
  END IF;
  IF EXISTS (
    SELECT 1 FROM (
      SELECT ordinal,row_number() OVER (PARTITION BY reading_id ORDER BY occurred_at,economic_fact_id) expected
      FROM workbench_reading_members
    ) ordered WHERE ordinal<>expected
  ) THEN
    RAISE EXCEPTION 'workbench schema25: invalid member order';
  END IF;
END $$;

ALTER TABLE workbench_economic_facts ADD COLUMN event_hash text;
UPDATE workbench_economic_facts f SET event_hash=e.event_hash FROM evidence_events e WHERE e.id=f.evidence_event_id;
ALTER TABLE workbench_economic_facts ALTER COLUMN event_hash SET NOT NULL;
ALTER TABLE workbench_economic_facts ADD CONSTRAINT workbench_economic_facts_event_hash_check
  CHECK (event_hash ~ '^sha256:[a-f0-9]{64}$');

-- Runtime roles already lack UPDATE/DELETE/TRUNCATE. Guard ordinary owner SQL
-- too, including session_replication_role=replica. Superuser DDL remains trusted.
CREATE FUNCTION reject_workbench_fact_mutation() RETURNS trigger
LANGUAGE plpgsql SET search_path = pg_catalog AS $$
BEGIN
  RAISE EXCEPTION 'workbench economic facts are immutable; restore exact identities from backup';
END $$;
REVOKE ALL ON FUNCTION reject_workbench_fact_mutation() FROM PUBLIC;
CREATE TRIGGER workbench_economic_facts_immutable
  BEFORE UPDATE OR DELETE OR TRUNCATE ON workbench_economic_facts
  FOR EACH STATEMENT EXECUTE FUNCTION reject_workbench_fact_mutation();
ALTER TABLE workbench_economic_facts ENABLE ALWAYS TRIGGER workbench_economic_facts_immutable;

ALTER TABLE workbench_reading_members
  DROP COLUMN evidence_event_id,
  DROP COLUMN source_system_id,
  DROP COLUMN source_version,
  DROP COLUMN source_event_hash,
  DROP COLUMN event_hash,
  DROP COLUMN request_id,
  DROP COLUMN execution_id,
  DROP COLUMN model_ref,
  DROP COLUMN source_project_ref,
  DROP COLUMN cost_project_ref,
  DROP COLUMN terminal_status,
  DROP COLUMN occurred_at,
  DROP COLUMN gateway_charged_micros,
  DROP COLUMN calculated_reference_micros,
  DROP COLUMN currency;
