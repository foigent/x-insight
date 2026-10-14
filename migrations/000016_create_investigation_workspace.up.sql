CREATE TABLE IF NOT EXISTS investigation_cases (
  id text PRIMARY KEY,
  title text NOT NULL,
  description text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'open',
  created_by_subject text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  sealed_at timestamptz,
  sealed_by_subject text,
  sealed_revision bigint,
  sealed_digest text,
  sealed_pack bytea,
  CONSTRAINT investigation_cases_id_check CHECK (id ~ '^xc_[A-Za-z0-9_-]{22}$'),
  CONSTRAINT investigation_cases_title_check CHECK (octet_length(title) BETWEEN 1 AND 160),
  CONSTRAINT investigation_cases_description_check CHECK (octet_length(description) <= 4000),
  CONSTRAINT investigation_cases_subject_check CHECK (
    created_by_subject ~ '^xs_[a-z0-9]{16,64}$'
    AND (sealed_by_subject IS NULL OR sealed_by_subject ~ '^xs_[a-z0-9]{16,64}$')
  ),
  CONSTRAINT investigation_cases_status_check CHECK (status IN ('open', 'sealed', 'archived')),
  CONSTRAINT investigation_cases_version_check CHECK (version > 0),
  CONSTRAINT investigation_cases_seal_check CHECK (
    (
      status = 'open'
      AND sealed_at IS NULL
      AND sealed_by_subject IS NULL
      AND sealed_revision IS NULL
      AND sealed_digest IS NULL
      AND sealed_pack IS NULL
    )
    OR (
      status IN ('sealed', 'archived')
      AND sealed_at IS NOT NULL
      AND sealed_by_subject IS NOT NULL
      AND sealed_revision IS NOT NULL
      AND sealed_revision > 0
      AND sealed_digest ~ '^sha256:[a-f0-9]{64}$'
      AND jsonb_typeof(convert_from(sealed_pack, 'UTF8')::jsonb) = 'object'
    )
  )
);

CREATE INDEX IF NOT EXISTS idx_investigation_cases_updated
  ON investigation_cases (updated_at DESC, id);

CREATE TABLE IF NOT EXISTS case_executions (
  case_id text NOT NULL REFERENCES investigation_cases(id) ON DELETE RESTRICT,
  execution_id text NOT NULL,
  as_of timestamptz NOT NULL,
  added_by_subject text NOT NULL,
  added_at timestamptz NOT NULL,
  PRIMARY KEY (case_id, execution_id, as_of),
  CONSTRAINT case_executions_execution_id_check CHECK (
    execution_id ~ '^xe1_[a-f0-9]{64}$'
    OR execution_id ~ '^xe1_[A-Za-z0-9_-]{8,128}$'
  ),
  CONSTRAINT case_executions_subject_check CHECK (added_by_subject ~ '^xs_[a-z0-9]{16,64}$')
);

CREATE INDEX IF NOT EXISTS idx_case_executions_execution
  ON case_executions (execution_id, as_of DESC, case_id);

CREATE TABLE IF NOT EXISTS case_findings (
  id text PRIMARY KEY,
  case_id text NOT NULL REFERENCES investigation_cases(id) ON DELETE RESTRICT,
  title text NOT NULL,
  statement text NOT NULL,
  classification text NOT NULL,
  status text NOT NULL DEFAULT 'open',
  created_by_subject text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  CONSTRAINT case_findings_id_check CHECK (id ~ '^xf_[A-Za-z0-9_-]{22}$'),
  CONSTRAINT case_findings_title_check CHECK (octet_length(title) BETWEEN 1 AND 160),
  CONSTRAINT case_findings_statement_check CHECK (octet_length(statement) BETWEEN 1 AND 8000),
  CONSTRAINT case_findings_classification_check CHECK (
    classification IN ('evidence_backed', 'deterministic_derived', 'hypothesis', 'unknown')
  ),
  CONSTRAINT case_findings_status_check CHECK (status IN ('open', 'accepted', 'rejected')),
  CONSTRAINT case_findings_subject_check CHECK (created_by_subject ~ '^xs_[a-z0-9]{16,64}$'),
  CONSTRAINT case_findings_version_check CHECK (version > 0),
  CONSTRAINT case_findings_case_id_id_unique UNIQUE (case_id, id)
);

CREATE INDEX IF NOT EXISTS idx_case_findings_case
  ON case_findings (case_id, created_at, id);

CREATE TABLE IF NOT EXISTS case_evidence_links (
  case_id text NOT NULL REFERENCES investigation_cases(id) ON DELETE RESTRICT,
  finding_id text,
  evidence_event_id uuid NOT NULL REFERENCES evidence_events(id) ON DELETE RESTRICT,
  relation text NOT NULL,
  event_hash_snapshot text NOT NULL,
  source_ref_snapshot text NOT NULL,
  added_by_subject text NOT NULL,
  added_at timestamptz NOT NULL,
  CONSTRAINT case_evidence_links_identity_unique UNIQUE NULLS NOT DISTINCT (
    case_id, finding_id, evidence_event_id, relation
  ),
  CONSTRAINT case_evidence_links_relation_check CHECK (relation IN ('supports', 'contradicts', 'context')),
  CONSTRAINT case_evidence_links_hash_check CHECK (event_hash_snapshot ~ '^sha256:[a-f0-9]{64}$'),
  CONSTRAINT case_evidence_links_source_ref_check CHECK (octet_length(source_ref_snapshot) BETWEEN 1 AND 2048),
  CONSTRAINT case_evidence_links_subject_check CHECK (added_by_subject ~ '^xs_[a-z0-9]{16,64}$'),
  CONSTRAINT case_evidence_links_finding_case_fk FOREIGN KEY (case_id, finding_id)
    REFERENCES case_findings(case_id, id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_case_evidence_links_finding
  ON case_evidence_links (finding_id, evidence_event_id);

CREATE TABLE IF NOT EXISTS case_gap_links (
  case_id text NOT NULL REFERENCES investigation_cases(id) ON DELETE RESTRICT,
  finding_id text,
  gap_id uuid NOT NULL REFERENCES capture_gaps(id) ON DELETE RESTRICT,
  relation text NOT NULL,
  affected_claim_snapshot text NOT NULL,
  gap_type_snapshot text NOT NULL,
  added_by_subject text NOT NULL,
  added_at timestamptz NOT NULL,
  CONSTRAINT case_gap_links_identity_unique UNIQUE NULLS NOT DISTINCT (
    case_id, finding_id, gap_id, relation
  ),
  CONSTRAINT case_gap_links_relation_check CHECK (relation IN ('limits', 'blocks', 'context')),
  CONSTRAINT case_gap_links_claim_check CHECK (octet_length(affected_claim_snapshot) BETWEEN 1 AND 2048),
  CONSTRAINT case_gap_links_type_check CHECK (octet_length(gap_type_snapshot) BETWEEN 1 AND 256),
  CONSTRAINT case_gap_links_subject_check CHECK (added_by_subject ~ '^xs_[a-z0-9]{16,64}$'),
  CONSTRAINT case_gap_links_finding_case_fk FOREIGN KEY (case_id, finding_id)
    REFERENCES case_findings(case_id, id) ON DELETE RESTRICT
);

CREATE INDEX IF NOT EXISTS idx_case_gap_links_finding
  ON case_gap_links (finding_id, gap_id);

CREATE TABLE IF NOT EXISTS case_annotations (
  id text PRIMARY KEY,
  case_id text NOT NULL REFERENCES investigation_cases(id) ON DELETE RESTRICT,
  target_kind text NOT NULL,
  target_ref text NOT NULL,
  body text NOT NULL,
  created_by_subject text NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT case_annotations_id_check CHECK (id ~ '^xn_[A-Za-z0-9_-]{22}$'),
  CONSTRAINT case_annotations_target_kind_check CHECK (
    target_kind IN ('case', 'execution', 'finding', 'evidence', 'gap')
  ),
  CONSTRAINT case_annotations_target_ref_check CHECK (octet_length(target_ref) BETWEEN 1 AND 256),
  CONSTRAINT case_annotations_body_check CHECK (octet_length(body) BETWEEN 1 AND 4000),
  CONSTRAINT case_annotations_subject_check CHECK (created_by_subject ~ '^xs_[a-z0-9]{16,64}$')
);

CREATE INDEX IF NOT EXISTS idx_case_annotations_case
  ON case_annotations (case_id, created_at, id);

CREATE TABLE IF NOT EXISTS saved_investigation_views (
  id text PRIMARY KEY,
  owner_subject text NOT NULL,
  name text NOT NULL,
  filters jsonb NOT NULL,
  presentation text NOT NULL,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  CONSTRAINT saved_investigation_views_id_check CHECK (id ~ '^xv_[A-Za-z0-9_-]{22}$'),
  CONSTRAINT saved_investigation_views_owner_check CHECK (owner_subject ~ '^xs_[a-z0-9]{16,64}$'),
  CONSTRAINT saved_investigation_views_name_check CHECK (octet_length(name) BETWEEN 1 AND 120),
  CONSTRAINT saved_investigation_views_filters_check CHECK (
    jsonb_typeof(filters) = 'object' AND octet_length(filters::text) <= 8192
  ),
  CONSTRAINT saved_investigation_views_presentation_check CHECK (
    presentation IN ('inbox', 'explorer', 'story', 'causal_graph', 'chronology', 'proof')
  ),
  CONSTRAINT saved_investigation_views_version_check CHECK (version > 0),
  CONSTRAINT saved_investigation_views_owner_name_unique UNIQUE (owner_subject, name)
);

CREATE INDEX IF NOT EXISTS idx_saved_investigation_views_owner
  ON saved_investigation_views (owner_subject, updated_at DESC, id);
