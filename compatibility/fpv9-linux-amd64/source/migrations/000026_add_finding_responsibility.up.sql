CREATE TABLE case_finding_followups (
  case_id text NOT NULL,
  finding_id text NOT NULL,
  assignee_subject_id text,
  assignee_name text NOT NULL DEFAULT '',
  due_at timestamptz,
  due_timezone text,
  version bigint NOT NULL CHECK (version > 0),
  updated_at timestamptz NOT NULL,
  updated_by_subject text NOT NULL CHECK (updated_by_subject ~ '^xs_[a-z0-9]{16,64}$'),
  PRIMARY KEY (case_id, finding_id),
  FOREIGN KEY (case_id, finding_id) REFERENCES case_findings(case_id, id) ON DELETE RESTRICT,
  CHECK ((assignee_subject_id IS NULL AND assignee_name = '') OR
    (assignee_subject_id IS NOT NULL AND assignee_subject_id ~ '^xs_[a-z0-9]{16,64}$' AND octet_length(assignee_name) BETWEEN 1 AND 64)),
  CHECK ((due_at IS NULL AND due_timezone IS NULL) OR
    (due_at IS NOT NULL AND due_timezone IS NOT NULL AND octet_length(due_timezone) BETWEEN 1 AND 128))
);

CREATE TABLE case_finding_actions (
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
  FOREIGN KEY (case_id, finding_id) REFERENCES case_finding_followups(case_id, finding_id) ON DELETE RESTRICT,
  CHECK (COALESCE(
    receipt->>'operation' = 'update_followup'
    AND receipt->>'operation_id' = operation_id::text
    AND receipt->>'actor_subject_id' = actor_subject_id
    AND receipt#>>'{current,case_id}' = case_id
    AND receipt#>>'{current,finding_id}' = finding_id
    AND receipt#>>'{previous,case_id}' = case_id
    AND receipt#>>'{previous,finding_id}' = finding_id
    AND (receipt#>>'{current,version}')::bigint = result_version
    AND (receipt->>'expected_version')::bigint = result_version - 1
    AND (receipt#>>'{previous,version}')::bigint = result_version - 1
    AND (receipt->>'case_version')::bigint = (receipt->>'expected_case_version')::bigint + 1
    AND octet_length(receipt->>'reason') BETWEEN 1 AND 4000,
    false))
);

COMMENT ON TABLE case_finding_followups IS 'Current human responsibility; does not grant access or assert review/control outcomes';
COMMENT ON TABLE case_finding_actions IS 'Append-only human responsibility receipts; projection and receipt commit atomically';
