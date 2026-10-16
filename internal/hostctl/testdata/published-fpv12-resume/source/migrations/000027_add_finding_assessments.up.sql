CREATE TABLE case_finding_assessments (
  case_id text NOT NULL,
  finding_id text NOT NULL,
  version bigint NOT NULL CHECK (version > 0),
  record jsonb NOT NULL CHECK (jsonb_typeof(record) = 'object'),
  duplicate_case_id text,
  duplicate_finding_id text,
  PRIMARY KEY (case_id, finding_id),
  FOREIGN KEY (case_id, finding_id) REFERENCES case_findings(case_id, id) ON DELETE RESTRICT,
  FOREIGN KEY (duplicate_case_id, duplicate_finding_id) REFERENCES case_findings(case_id, id) ON DELETE RESTRICT,
  CHECK ((duplicate_case_id IS NULL) = (duplicate_finding_id IS NULL)),
  CHECK (duplicate_case_id IS NULL OR (case_id, finding_id) <> (duplicate_case_id, duplicate_finding_id)),
  CHECK (COALESCE(record->>'case_id' = case_id AND record->>'finding_id' = finding_id
    AND (record->>'version')::bigint = version AND record->>'recorded' = 'true', false)),
  CHECK (COALESCE((duplicate_case_id IS NULL AND (record#>>'{progress,closure,kind}' IS DISTINCT FROM 'duplicate')) OR
    (record#>>'{progress,state}' = 'ended' AND record#>>'{progress,closure,kind}' = 'duplicate'
     AND record#>>'{progress,closure,duplicate,case_id}' = duplicate_case_id
     AND record#>>'{progress,closure,duplicate,finding_id}' = duplicate_finding_id), false))
);

CREATE TABLE case_finding_assessment_actions (
  case_id text NOT NULL,
  finding_id text NOT NULL,
  actor_subject_id text NOT NULL CHECK (actor_subject_id ~ '^xs_[a-z0-9]{16,64}$'),
  operation_id uuid NOT NULL,
  request_sha256 text NOT NULL CHECK (request_sha256 ~ '^[a-f0-9]{64}$'),
  result_version bigint NOT NULL CHECK (result_version > 0),
  created_at timestamptz NOT NULL,
  receipt jsonb NOT NULL CHECK (jsonb_typeof(receipt) = 'object'),
  PRIMARY KEY (case_id, finding_id, actor_subject_id, operation_id),
  UNIQUE (case_id, finding_id, result_version),
  FOREIGN KEY (case_id, finding_id) REFERENCES case_finding_assessments(case_id, finding_id) ON DELETE RESTRICT,
  CHECK (COALESCE(receipt->>'operation' IN ('record_review', 'record_progress')
    AND receipt->>'operation_id' = operation_id::text
    AND receipt->>'actor_subject_id' = actor_subject_id
    AND receipt#>>'{current,case_id}' = case_id AND receipt#>>'{current,finding_id}' = finding_id
    AND receipt#>>'{previous,case_id}' = case_id AND receipt#>>'{previous,finding_id}' = finding_id
    AND (receipt#>>'{current,version}')::bigint = result_version
    AND (receipt#>>'{previous,version}')::bigint = result_version - 1
    AND (receipt#>>'{command,expected_version}')::bigint = result_version - 1
    AND (receipt->>'case_version')::bigint = (receipt#>>'{command,expected_case_version}')::bigint + 1, false))
);

-- This database is the deployment boundary. The key exists before any generated
-- Finding identity; a transaction advisory lock serializes first-use retries.
CREATE TABLE finding_review_creations (
  actor_subject_id text NOT NULL CHECK (actor_subject_id ~ '^xs_[a-z0-9]{16,64}$'),
  operation text NOT NULL CHECK (operation IN ('start_review','continue_finding')),
  operation_id uuid NOT NULL,
  request_sha256 text NOT NULL CHECK (request_sha256 ~ '^[a-f0-9]{64}$'),
  case_id text NOT NULL,
  finding_id text NOT NULL,
  source_case_id text,
  source_finding_id text,
  created_at timestamptz NOT NULL,
  receipt jsonb NOT NULL CHECK (jsonb_typeof(receipt) = 'object'),
  PRIMARY KEY (actor_subject_id, operation, operation_id),
  UNIQUE (case_id, finding_id),
  FOREIGN KEY (case_id, finding_id) REFERENCES case_findings(case_id, id) ON DELETE RESTRICT,
  FOREIGN KEY (source_case_id, source_finding_id) REFERENCES case_findings(case_id, id) ON DELETE RESTRICT,
  CHECK ((operation = 'start_review' AND source_case_id IS NULL AND source_finding_id IS NULL) OR
    (operation = 'continue_finding' AND source_case_id IS NOT NULL AND source_finding_id IS NOT NULL)),
  CHECK (COALESCE(receipt->>'operation' = operation AND receipt->>'operation_id' = operation_id::text
    AND receipt->>'actor_subject_id' = actor_subject_id
    AND receipt->>'case_id' = case_id AND receipt->>'finding_id' = finding_id, false))
);

CREATE INDEX finding_review_continuations ON finding_review_creations(source_case_id, source_finding_id)
  WHERE source_case_id IS NOT NULL;
COMMENT ON TABLE case_finding_assessments IS 'Finite human review and progress projection, independent of responsibility and source control';
COMMENT ON TABLE case_finding_assessment_actions IS 'Append-only receipts with the actual adopted evidence and Gap resolution contents';
COMMENT ON TABLE finding_review_creations IS 'Atomic start and sealed continuation receipts; no placeholder or pending creation rows';
