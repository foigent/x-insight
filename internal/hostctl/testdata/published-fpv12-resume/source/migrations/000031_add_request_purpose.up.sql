-- Human interpretation is distinct from source facts and economic authority.
ALTER TABLE workbench_access_grants
  ADD COLUMN allow_purpose_read boolean NOT NULL DEFAULT false,
  ADD COLUMN allow_purpose_review boolean NOT NULL DEFAULT false;

-- A purpose-only grant edit must invalidate the existing generic grant digest
-- too. Legacy writers need not learn a second counter; no-op upgrades leave it.
CREATE FUNCTION advance_purpose_grant_revision() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
  IF ROW(NEW.allow_purpose_read,NEW.allow_purpose_review) IS DISTINCT FROM ROW(OLD.allow_purpose_read,OLD.allow_purpose_review)
    AND NEW.revision<=OLD.revision THEN NEW.revision=OLD.revision+1; END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION advance_purpose_grant_revision() FROM PUBLIC;
CREATE TRIGGER workbench_purpose_grant_revision BEFORE UPDATE ON workbench_access_grants
  FOR EACH ROW EXECUTE FUNCTION advance_purpose_grant_revision();
ALTER TABLE workbench_access_grants ENABLE ALWAYS TRIGGER workbench_purpose_grant_revision;

CREATE TABLE workbench_purpose_revisions (
  execution_id text NOT NULL,
  revision bigint NOT NULL CHECK(revision BETWEEN 1 AND 9007199254740991),
  economic_fact_id uuid NOT NULL REFERENCES workbench_economic_facts(id) ON DELETE RESTRICT,
  actor_subject text NOT NULL,
  idempotency_key text NOT NULL CHECK(octet_length(idempotency_key) BETWEEN 1 AND 160),
  command_sha256 text NOT NULL CHECK(command_sha256 ~ '^sha256:[a-f0-9]{64}$'),
  previous_sha256 text NOT NULL,
  event_sha256 text NOT NULL CHECK(event_sha256 ~ '^sha256:[a-f0-9]{64}$'),
  event_bytes bytea NOT NULL CHECK(octet_length(event_bytes) BETWEEN 1 AND 4194304),
  event_doc jsonb NOT NULL CHECK(event_doc=convert_from(event_bytes,'UTF8')::jsonb),
  recorded_at timestamptz NOT NULL,
  PRIMARY KEY(execution_id,revision),
  UNIQUE(actor_subject,idempotency_key),
  CHECK(event_sha256='sha256:'||encode(sha256(event_bytes),'hex')),
  CHECK((revision=1 AND previous_sha256='') OR (revision>1 AND previous_sha256 ~ '^sha256:[a-f0-9]{64}$')),
  CHECK(event_doc->>'execution_id'=execution_id AND (event_doc->>'revision')::bigint=revision
    AND event_doc->>'actor'=actor_subject AND event_doc->>'previous_hash'=previous_sha256)
);

CREATE TABLE workbench_purpose_members (
  reading_id text NOT NULL REFERENCES workbench_readings(id) ON DELETE RESTRICT,
  economic_fact_id uuid NOT NULL REFERENCES workbench_economic_facts(id) ON DELETE RESTRICT,
  execution_id text NOT NULL,
  purpose_revision bigint,
  status text NOT NULL CHECK(status IN ('unknown','unreviewed','conflict','adopted')),
  category text NOT NULL DEFAULT '',
  event_sha256 text NOT NULL DEFAULT '',
  PRIMARY KEY(reading_id,economic_fact_id),
  FOREIGN KEY(execution_id,purpose_revision) REFERENCES workbench_purpose_revisions(execution_id,revision) ON DELETE RESTRICT,
  CHECK((status='adopted')=(category IN ('information_query','content_transformation','software_check'))),
  CHECK(status='adopted' OR category=''),
  CHECK((purpose_revision IS NULL AND event_sha256='' AND status='unknown') OR (purpose_revision IS NOT NULL AND event_sha256 ~ '^sha256:[a-f0-9]{64}$'))
);
CREATE TABLE workbench_purpose_manifests (
  reading_id text PRIMARY KEY REFERENCES workbench_readings(id) ON DELETE RESTRICT,
  rule_version text NOT NULL CHECK(rule_version='xinsight.request-purpose-reading.v1'),
  status text NOT NULL CHECK(status IN ('available','unavailable')),
  authorization_sha256 text NOT NULL CHECK(authorization_sha256 ~ '^sha256:[a-f0-9]{64}$'),
  manifest_sha256 text NOT NULL CHECK(manifest_sha256 ~ '^sha256:[a-f0-9]{64}$')
);

CREATE FUNCTION guard_purpose_revision_append() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE previous_revision bigint; previous_hash text;
BEGIN
  PERFORM pg_advisory_xact_lock(hashtextextended('xinsight.purpose:'||NEW.execution_id,0));
  SELECT revision,event_sha256 INTO previous_revision,previous_hash
    FROM public.workbench_purpose_revisions WHERE execution_id=NEW.execution_id ORDER BY revision DESC LIMIT 1;
  IF NEW.revision<>COALESCE(previous_revision,0)+1 OR NEW.previous_sha256<>COALESCE(previous_hash,'')
    OR NOT EXISTS(SELECT 1 FROM public.workbench_economic_facts WHERE id=NEW.economic_fact_id AND execution_id=NEW.execution_id) THEN
    RAISE EXCEPTION 'purpose revision predecessor or execution anchor is invalid';
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_purpose_revision_append() FROM PUBLIC;
CREATE TRIGGER workbench_purpose_revision_append BEFORE INSERT ON workbench_purpose_revisions
  FOR EACH ROW EXECUTE FUNCTION guard_purpose_revision_append();
ALTER TABLE workbench_purpose_revisions ENABLE ALWAYS TRIGGER workbench_purpose_revision_append;

CREATE FUNCTION guard_purpose_reading_append() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
BEGIN
  IF TG_TABLE_NAME='workbench_purpose_manifests' THEN
    UPDATE public.workbench_readings SET membership_sha256=membership_sha256 WHERE id=NEW.reading_id;
  ELSE
    PERFORM 1 FROM public.workbench_readings WHERE id=NEW.reading_id FOR UPDATE;
    IF NOT FOUND OR EXISTS(SELECT 1 FROM public.workbench_purpose_manifests WHERE reading_id=NEW.reading_id) THEN
      RAISE EXCEPTION 'purpose reading is already frozen or unavailable';
    END IF;
  END IF;
  RETURN NEW;
END $$;
REVOKE ALL ON FUNCTION guard_purpose_reading_append() FROM PUBLIC;
CREATE TRIGGER workbench_purpose_members_append BEFORE INSERT ON workbench_purpose_members
  FOR EACH ROW EXECUTE FUNCTION guard_purpose_reading_append();
ALTER TABLE workbench_purpose_members ENABLE ALWAYS TRIGGER workbench_purpose_members_append;
CREATE TRIGGER workbench_purpose_manifests_finalize BEFORE INSERT ON workbench_purpose_manifests
  FOR EACH ROW EXECUTE FUNCTION guard_purpose_reading_append();
ALTER TABLE workbench_purpose_manifests ENABLE ALWAYS TRIGGER workbench_purpose_manifests_finalize;

CREATE TRIGGER workbench_purpose_revisions_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workbench_purpose_revisions
  FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
ALTER TABLE workbench_purpose_revisions ENABLE ALWAYS TRIGGER workbench_purpose_revisions_immutable;
CREATE TRIGGER workbench_purpose_members_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workbench_purpose_members
  FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
ALTER TABLE workbench_purpose_members ENABLE ALWAYS TRIGGER workbench_purpose_members_immutable;
CREATE TRIGGER workbench_purpose_manifests_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workbench_purpose_manifests
  FOR EACH STATEMENT EXECUTE FUNCTION reject_source_fact_mutation();
ALTER TABLE workbench_purpose_manifests ENABLE ALWAYS TRIGGER workbench_purpose_manifests_immutable;
