ALTER TABLE import_provenance
  DROP CONSTRAINT IF EXISTS import_provenance_integration_mode_check,
  DROP CONSTRAINT IF EXISTS import_provenance_assurance_mode_check,
  DROP CONSTRAINT IF EXISTS import_provenance_proof_requirements_check;

ALTER TABLE import_provenance
  ADD CONSTRAINT import_provenance_integration_mode_check CHECK (
    integration_mode IN (
      'sidecar',
      'completed_execution_pull',
      'connector_pull',
      'connector_bounded_offline',
      'synthetic_fixture'
    )
  ),
  ADD CONSTRAINT import_provenance_assurance_mode_check CHECK (
    assurance_mode IN (
      'operator_attested_sidecar',
      'contracted_pull',
      'bounded_offline_import',
      'synthetic_fixture'
    )
  ),
  ADD CONSTRAINT import_provenance_proof_requirements_check CHECK (
    (
      proof_class = 'synthetic_fixture'
      AND integration_mode = 'synthetic_fixture'
      AND assurance_mode = 'synthetic_fixture'
      AND source_artifact_sha256 IS NOT NULL
      AND source_payload_sha256 IS NULL
      AND approval_manifest_sha256 IS NULL
      AND exchange_id IS NULL
    ) OR (
      proof_class = 'operator_approved_integration'
      AND integration_mode = 'sidecar'
      AND assurance_mode = 'operator_attested_sidecar'
      AND source_artifact_sha256 IS NOT NULL
      AND source_payload_sha256 IS NULL
      AND approval_manifest_sha256 IS NOT NULL
      AND NULLIF(btrim(exchange_id), '') IS NOT NULL
    ) OR (
      proof_class = 'operator_approved_integration'
      AND integration_mode IN (
        'completed_execution_pull',
        'connector_pull'
      )
      AND assurance_mode = 'contracted_pull'
      AND source_artifact_sha256 IS NULL
      AND source_payload_sha256 IS NOT NULL
      AND approval_manifest_sha256 IS NULL
      AND exchange_id IS NULL
      AND source_watermark IS NULL
      AND import_watermark IS NULL
    ) OR (
      proof_class = 'operator_approved_integration'
      AND integration_mode = 'connector_bounded_offline'
      AND assurance_mode = 'bounded_offline_import'
      AND source_artifact_sha256 IS NULL
      AND source_payload_sha256 IS NOT NULL
      AND approval_manifest_sha256 IS NULL
      AND exchange_id IS NULL
      AND source_watermark IS NULL
      AND import_watermark IS NULL
    )
  );

CREATE TABLE IF NOT EXISTS capture_gap_resolutions (
  gap_id uuid PRIMARY KEY,
  state text NOT NULL,
  note text NOT NULL DEFAULT '',
  updated_by_subject text NOT NULL,
  updated_at timestamptz NOT NULL,
  version bigint NOT NULL DEFAULT 1,
  CONSTRAINT capture_gap_resolutions_state_check CHECK (
    state IN ('open', 'investigating', 'source_fixed', 'accepted_limitation', 'superseded')
  ),
  CONSTRAINT capture_gap_resolutions_note_check CHECK (octet_length(note) <= 4000),
  CONSTRAINT capture_gap_resolutions_subject_check CHECK (updated_by_subject ~ '^xs_[a-z0-9]{16,64}$'),
  CONSTRAINT capture_gap_resolutions_version_check CHECK (version > 0)
);

CREATE TABLE IF NOT EXISTS authoritative_sessions (
  id text PRIMARY KEY,
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  source_session_ref text NOT NULL,
  source_session_type text NOT NULL,
  observed_at timestamptz NOT NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  CONSTRAINT authoritative_sessions_id_check CHECK (id ~ '^xss_[a-f0-9]{64}$'),
  CONSTRAINT authoritative_sessions_ref_check CHECK (octet_length(source_session_ref) BETWEEN 1 AND 512),
  CONSTRAINT authoritative_sessions_type_check CHECK (octet_length(source_session_type) BETWEEN 1 AND 120),
  CONSTRAINT authoritative_sessions_metadata_check CHECK (
    jsonb_typeof(metadata) = 'object' AND octet_length(metadata::text) <= 8192
  ),
  CONSTRAINT authoritative_sessions_source_unique UNIQUE (source_system_id, source_session_ref)
);

CREATE INDEX IF NOT EXISTS idx_authoritative_sessions_observed
  ON authoritative_sessions (observed_at DESC, id);

CREATE TABLE IF NOT EXISTS session_execution_links (
  session_id text NOT NULL REFERENCES authoritative_sessions(id) ON DELETE RESTRICT,
  execution_id text NOT NULL,
  source_link_ref text NOT NULL,
  linked_at timestamptz NOT NULL,
  PRIMARY KEY (session_id, execution_id),
  CONSTRAINT session_execution_links_execution_check CHECK (
    execution_id ~ '^xe1_[a-f0-9]{64}$'
    OR execution_id ~ '^xe1_[A-Za-z0-9_-]{8,128}$'
  ),
  CONSTRAINT session_execution_links_source_ref_check CHECK (octet_length(source_link_ref) BETWEEN 1 AND 1024)
);

CREATE INDEX IF NOT EXISTS idx_session_execution_links_execution
  ON session_execution_links (execution_id, session_id);

CREATE TABLE IF NOT EXISTS evaluation_feedback (
  id text PRIMARY KEY,
  execution_id text NOT NULL,
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  evaluator_ref text NOT NULL,
  metric_name text NOT NULL,
  metric_value_decimal text,
  label text,
  explanation text NOT NULL DEFAULT '',
  observed_at timestamptz NOT NULL,
  evidence_event_ids uuid[] NOT NULL DEFAULT ARRAY[]::uuid[],
  CONSTRAINT evaluation_feedback_id_check CHECK (id ~ '^xef_[a-f0-9]{64}$'),
  CONSTRAINT evaluation_feedback_execution_check CHECK (
    execution_id ~ '^xe1_[a-f0-9]{64}$'
    OR execution_id ~ '^xe1_[A-Za-z0-9_-]{8,128}$'
  ),
  CONSTRAINT evaluation_feedback_metric_check CHECK (
    octet_length(metric_name) BETWEEN 1 AND 160
    AND (
      metric_value_decimal IS NULL
      OR metric_value_decimal ~ '^-?(0|[1-9][0-9]{0,17})(\.[0-9]{1,9})?$'
    )
  ),
  CONSTRAINT evaluation_feedback_value_check CHECK (
    metric_value_decimal IS NOT NULL OR (label IS NOT NULL AND octet_length(label) BETWEEN 1 AND 160)
  ),
  CONSTRAINT evaluation_feedback_explanation_check CHECK (octet_length(explanation) <= 4000)
);

CREATE INDEX IF NOT EXISTS idx_evaluation_feedback_execution
  ON evaluation_feedback (execution_id, observed_at, id);

CREATE TABLE IF NOT EXISTS investigation_alerts (
  id text PRIMARY KEY,
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  source_alert_ref text NOT NULL,
  title text NOT NULL,
  severity text NOT NULL,
  state text NOT NULL,
  execution_id text,
  observed_at timestamptz NOT NULL,
  CONSTRAINT investigation_alerts_id_check CHECK (id ~ '^xal_[a-f0-9]{64}$'),
  CONSTRAINT investigation_alerts_source_ref_check CHECK (octet_length(source_alert_ref) BETWEEN 1 AND 512),
  CONSTRAINT investigation_alerts_title_check CHECK (octet_length(title) BETWEEN 1 AND 240),
  CONSTRAINT investigation_alerts_severity_check CHECK (severity IN ('info', 'warning', 'error', 'critical')),
  CONSTRAINT investigation_alerts_state_check CHECK (state IN ('open', 'acknowledged', 'resolved')),
  CONSTRAINT investigation_alerts_source_unique UNIQUE (source_system_id, source_alert_ref)
);

CREATE INDEX IF NOT EXISTS idx_investigation_alerts_state
  ON investigation_alerts (state, severity, observed_at DESC, id);

CREATE TABLE IF NOT EXISTS investigation_alert_case_links (
  alert_id text PRIMARY KEY REFERENCES investigation_alerts(id) ON DELETE RESTRICT,
  case_id text NOT NULL REFERENCES investigation_cases(id) ON DELETE RESTRICT,
  linked_by_subject text NOT NULL,
  linked_at timestamptz NOT NULL,
  CONSTRAINT investigation_alert_case_links_subject_check CHECK (
    linked_by_subject ~ '^xs_[a-z0-9]{16,64}$'
  )
);

CREATE INDEX IF NOT EXISTS idx_investigation_alert_case_links_case
  ON investigation_alert_case_links (case_id, linked_at, alert_id);

CREATE TABLE IF NOT EXISTS execution_version_dimensions (
  execution_id text NOT NULL,
  dimension text NOT NULL,
  version_ref text NOT NULL,
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  evidence_event_id uuid,
  observed_at timestamptz NOT NULL,
  PRIMARY KEY (execution_id, dimension, version_ref),
  CONSTRAINT execution_version_dimensions_execution_check CHECK (
    execution_id ~ '^xe1_[a-f0-9]{64}$'
    OR execution_id ~ '^xe1_[A-Za-z0-9_-]{8,128}$'
  ),
  CONSTRAINT execution_version_dimensions_dimension_check CHECK (
    dimension IN ('prompt', 'application', 'policy', 'model', 'connector')
  ),
  CONSTRAINT execution_version_dimensions_ref_check CHECK (octet_length(version_ref) BETWEEN 1 AND 512)
);

CREATE INDEX IF NOT EXISTS idx_execution_version_dimensions_execution
  ON execution_version_dimensions (execution_id, dimension, observed_at DESC);

CREATE TABLE IF NOT EXISTS discovery_analysis_runs (
  id text PRIMARY KEY,
  method_version text NOT NULL,
  analysis_as_of timestamptz NOT NULL,
  sample_size integer NOT NULL,
  minimum_sample integer NOT NULL,
  input_digest text NOT NULL,
  generated_at timestamptz NOT NULL,
  CONSTRAINT discovery_analysis_runs_id_check CHECK (id ~ '^xdr_[a-f0-9]{64}$'),
  CONSTRAINT discovery_analysis_runs_method_check CHECK (octet_length(method_version) BETWEEN 1 AND 160),
  CONSTRAINT discovery_analysis_runs_sample_check CHECK (
    sample_size BETWEEN 0 AND 1000
    AND minimum_sample BETWEEN 20 AND 1000
  ),
  CONSTRAINT discovery_analysis_runs_digest_check CHECK (input_digest ~ '^sha256:[a-f0-9]{64}$'),
  CONSTRAINT discovery_analysis_runs_time_check CHECK (generated_at >= analysis_as_of),
  CONSTRAINT discovery_analysis_runs_identity_unique UNIQUE (
    method_version, analysis_as_of, minimum_sample, input_digest
  )
);

CREATE INDEX IF NOT EXISTS idx_discovery_analysis_runs_as_of
  ON discovery_analysis_runs (analysis_as_of DESC, generated_at DESC, id);

CREATE TABLE IF NOT EXISTS discovery_hypotheses (
  id text PRIMARY KEY,
  analysis_run_id text NOT NULL REFERENCES discovery_analysis_runs(id) ON DELETE RESTRICT,
  hypothesis_type text NOT NULL,
  title text NOT NULL,
  statement text NOT NULL,
  support_score_decimal text,
  status text NOT NULL,
  evidence_event_ids uuid[] NOT NULL DEFAULT ARRAY[]::uuid[],
  execution_ids text[] NOT NULL DEFAULT ARRAY[]::text[],
  limitations text[] NOT NULL DEFAULT ARRAY[]::text[],
  abstention_reason text,
  method_version text NOT NULL,
  generated_at timestamptz NOT NULL,
  CONSTRAINT discovery_hypotheses_id_check CHECK (id ~ '^xhy_[a-f0-9]{64}$'),
  CONSTRAINT discovery_hypotheses_type_check CHECK (
    hypothesis_type IN (
      'coverage_pattern',
      'process_pattern',
      'topic_cluster',
      'anomaly',
      'coalition',
      'authority_drift'
    )
  ),
  CONSTRAINT discovery_hypotheses_title_check CHECK (octet_length(title) BETWEEN 1 AND 240),
  CONSTRAINT discovery_hypotheses_statement_check CHECK (octet_length(statement) BETWEEN 1 AND 8000),
  CONSTRAINT discovery_hypotheses_support_score_check CHECK (
    support_score_decimal IS NULL
    OR support_score_decimal ~ '^(0(\.[0-9]{1,6})?|1(\.0{1,6})?)$'
  ),
  CONSTRAINT discovery_hypotheses_status_check CHECK (status IN ('candidate', 'reviewed', 'dismissed', 'abstained')),
  CONSTRAINT discovery_hypotheses_limit_check CHECK (
    cardinality(evidence_event_ids) <= 1000
    AND cardinality(execution_ids) <= 1000
    AND cardinality(limitations) BETWEEN 1 AND 32
  ),
  CONSTRAINT discovery_hypotheses_abstention_check CHECK (
    (status = 'abstained' AND abstention_reason IS NOT NULL AND support_score_decimal IS NULL)
    OR (status <> 'abstained' AND abstention_reason IS NULL AND support_score_decimal IS NOT NULL)
  ),
  CONSTRAINT discovery_hypotheses_method_check CHECK (octet_length(method_version) BETWEEN 1 AND 160)
);

CREATE INDEX IF NOT EXISTS idx_discovery_hypotheses_generated
  ON discovery_hypotheses (generated_at DESC, id);

CREATE INDEX IF NOT EXISTS idx_discovery_hypotheses_run
  ON discovery_hypotheses (analysis_run_id, hypothesis_type, id);
