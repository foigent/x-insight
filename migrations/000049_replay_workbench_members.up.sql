-- Facts present before this migration are the Workbench baseline. New facts
-- are stamped with the publication of the transaction that projects them,
-- which can differ from the publication of the underlying Evidence event.
ALTER TABLE workbench_economic_facts ADD COLUMN publication_id uuid
  REFERENCES evidence_publications DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE workbench_economic_facts ALTER COLUMN publication_id
  SET DEFAULT NULLIF(current_setting('xinsight.evidence_publication_id', true), '')::uuid;

CREATE FUNCTION guard_workbench_projection_publication() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE active_id uuid := NULLIF(current_setting('xinsight.evidence_publication_id', true), '')::uuid;
BEGIN
  IF EXISTS (SELECT 1 FROM new_facts) AND (
    active_id IS NULL
    OR EXISTS (SELECT 1 FROM new_facts WHERE publication_id IS DISTINCT FROM active_id)
    OR EXISTS (SELECT 1 FROM public.evidence_publications WHERE snapshot_id=active_id)
  ) THEN
    RAISE EXCEPTION 'Workbench economic facts require their own unpublished transaction publication';
  END IF;
  RETURN NULL;
END $$;
REVOKE ALL ON FUNCTION guard_workbench_projection_publication() FROM PUBLIC;
CREATE TRIGGER workbench_projection_publication
  AFTER INSERT ON workbench_economic_facts
  REFERENCING NEW TABLE AS new_facts
  FOR EACH STATEMENT EXECUTE FUNCTION guard_workbench_projection_publication();
ALTER TABLE workbench_economic_facts ENABLE ALWAYS TRIGGER workbench_projection_publication;

CREATE FUNCTION reject_evidence_publication_mutation() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
  RAISE EXCEPTION 'evidence publications are immutable; restore exact identities from backup';
END $$;
REVOKE ALL ON FUNCTION reject_evidence_publication_mutation() FROM PUBLIC;
CREATE TRIGGER evidence_publications_immutable
  BEFORE UPDATE OR DELETE OR TRUNCATE ON evidence_publications
  FOR EACH STATEMENT EXECUTE FUNCTION reject_evidence_publication_mutation();
ALTER TABLE evidence_publications ENABLE ALWAYS TRIGGER evidence_publications_immutable;

ALTER TABLE workbench_readings
  ADD COLUMN projection_publication_id uuid REFERENCES evidence_publications,
  ADD COLUMN summary_scopes jsonb,
  ADD COLUMN gateway_total_micros text,
  ADD COLUMN calculated_total_micros text,
  ADD CONSTRAINT workbench_readings_replay_scope_check CHECK (
    (projection_publication_id IS NULL AND summary_scopes IS NULL)
    OR (projection_publication_id IS NOT NULL AND summary_scopes IS NOT NULL
      AND jsonb_typeof(summary_scopes)='array' AND jsonb_array_length(summary_scopes)>0)
  ),
  ADD CONSTRAINT workbench_readings_replay_totals_check CHECK (
    (gateway_total_micros IS NULL AND calculated_total_micros IS NULL)
    OR (projection_publication_id IS NOT NULL
      AND gateway_total_micros IS NOT NULL AND calculated_total_micros IS NOT NULL
      AND gateway_total_micros ~ '^(0|[1-9][0-9]{0,35})$'
      AND calculated_total_micros ~ '^(0|[1-9][0-9]{0,35})$')
  );

CREATE FUNCTION guard_workbench_reading_totals() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
  IF OLD.gateway_total_micros IS NOT NULL AND
     ROW(OLD.gateway_total_micros,OLD.calculated_total_micros) IS DISTINCT FROM
     ROW(NEW.gateway_total_micros,NEW.calculated_total_micros) THEN
    RAISE EXCEPTION 'Workbench reading totals are already frozen';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_workbench_reading_totals() FROM PUBLIC;
CREATE TRIGGER workbench_reading_totals_immutable
  BEFORE UPDATE OF gateway_total_micros,calculated_total_micros ON workbench_readings
  FOR EACH ROW EXECUTE FUNCTION guard_workbench_reading_totals();
ALTER TABLE workbench_readings ENABLE ALWAYS TRIGGER workbench_reading_totals_immutable;

ALTER TABLE workbench_reading_members RENAME TO workbench_legacy_reading_members;
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='xinsight_workspace') THEN
    REVOKE INSERT ON workbench_legacy_reading_members FROM xinsight_workspace;
  END IF;
END $$;

-- Creation can aggregate the resolved facts directly. Read consumers that
-- need position or legacy membership use the narrower relation below.
CREATE VIEW workbench_replayed_reading_facts WITH (security_invoker=true) AS
SELECT r.id AS reading_id,f.*
FROM workbench_readings r
JOIN evidence_publications anchor ON anchor.snapshot_id=r.projection_publication_id
CROSS JOIN LATERAL (
  SELECT g.source_system_id,
         bool_or(g.source_project_ref IS NULL AND g.cost_project_ref IS NULL) AS whole_source,
         jsonb_agg(jsonb_build_object(
           'source_project_ref',g.source_project_ref,'cost_project_ref',g.cost_project_ref
         )) AS restricted_scopes
  FROM jsonb_to_recordset(r.summary_scopes)
    AS g(source_system_id uuid, source_project_ref text, cost_project_ref text)
  WHERE COALESCE(NULLIF(r.filters->'source_ids','null'::jsonb),'[]'::jsonb)='[]'::jsonb
    OR r.filters->'source_ids' ? g.source_system_id::text
  GROUP BY g.source_system_id
) source_scope
JOIN workbench_economic_facts f
  ON f.source_system_id=source_scope.source_system_id
    AND f.occurred_at>=r.period_start AND f.occurred_at<r.period_end AND f.currency=r.currency
    AND (f.publication_id IS NULL OR f.publication_id NOT IN (
      SELECT snapshot_id FROM evidence_publications WHERE revision>anchor.revision
    ))
    AND (COALESCE(NULLIF(r.filters->'source_projects','null'::jsonb),'[]'::jsonb)='[]'::jsonb
      OR r.filters->'source_projects' ? f.source_project_ref)
    AND (COALESCE(NULLIF(r.filters->'cost_projects','null'::jsonb),'[]'::jsonb)='[]'::jsonb
      OR r.filters->'cost_projects' ? f.cost_project_ref)
    AND (source_scope.whole_source OR EXISTS (
      SELECT 1 FROM jsonb_to_recordset(source_scope.restricted_scopes)
        AS g(source_project_ref text, cost_project_ref text)
      WHERE (g.source_project_ref IS NULL OR g.source_project_ref=f.source_project_ref)
        AND (g.cost_project_ref IS NULL OR g.cost_project_ref=f.cost_project_ref)
    ));

CREATE VIEW workbench_reading_members WITH (security_invoker=true) AS
SELECT m.reading_id,m.ordinal,m.economic_fact_id
FROM workbench_legacy_reading_members m
UNION ALL
SELECT reading_id,row_number() OVER (PARTITION BY reading_id ORDER BY occurred_at,id),id
FROM workbench_replayed_reading_facts;
