CREATE TABLE workbench_economic_facts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  source_version text NOT NULL,
  source_ref text NOT NULL,
  source_event_hash text NOT NULL,
  evidence_event_id uuid NOT NULL REFERENCES evidence_events(id) ON DELETE RESTRICT,
  request_id text NOT NULL,
  execution_id text NOT NULL,
  model_ref text,
  source_project_ref text,
  cost_project_ref text,
  terminal_status text NOT NULL,
  occurred_at timestamptz NOT NULL,
  gateway_charged_micros numeric(30,0) NOT NULL,
  calculated_reference_micros numeric(30,0) NOT NULL,
  currency text NOT NULL,
  amount_scale integer NOT NULL,
  projected_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT workbench_economic_facts_business_identity UNIQUE (
    source_system_id, source_version, source_ref
  ),
  CONSTRAINT workbench_economic_facts_source_hash_check CHECK (
    source_event_hash ~ '^sha256:[a-f0-9]{64}$'
  ),
  CONSTRAINT workbench_economic_facts_execution_check CHECK (
    execution_id ~ '^xe1_[a-f0-9]{64}$'
  ),
  CONSTRAINT workbench_economic_facts_money_check CHECK (
    gateway_charged_micros >= 0
    AND calculated_reference_micros >= 0
    AND currency = 'CNY'
    AND amount_scale = 1000000
  ),
  CONSTRAINT workbench_economic_facts_terminal_check CHECK (
    terminal_status IN ('settled', 'cancelled')
  )
);

CREATE INDEX workbench_economic_facts_period
  ON workbench_economic_facts (occurred_at, id);
CREATE INDEX workbench_economic_facts_source_period_scope
  ON workbench_economic_facts (source_system_id, occurred_at, source_project_ref, cost_project_ref, id);

-- Rebuild the bounded local projection for qualifying V1 Evidence that was
-- accepted before this table existed. This is not source-history backfill:
-- every value is read from XI's already-persisted immutable Evidence row.
DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM evidence_events
    WHERE source_version = 'unirouter.completed-execution.v1'
      AND event_type IN ('billing.execution.settled', 'billing.execution.cancelled')
      AND source_event_hash ~ '^sha256:[a-f0-9]{64}$'
      AND request_id IS NOT NULL
      AND execution_id ~ '^xe1_[a-f0-9]{64}$'
      AND occurred_at IS NOT NULL
      AND usage_data ->> 'cost_micros' ~ '^(0|[1-9][0-9]{0,29})$'
      AND usage_data #>> '{extensions,calculated_cost_micros}' ~ '^(0|[1-9][0-9]{0,29})$'
      AND usage_data ->> 'currency' = 'CNY'
      AND usage_data ->> 'amount_scale' = '1000000'
      AND outcome #>> '{extensions,terminal_status}' IN ('settled', 'cancelled')
    GROUP BY source_system_id, source_version, source_ref
    HAVING count(DISTINCT jsonb_build_array(
      source_event_hash, request_id, execution_id, capability ->> 'model', project_ref,
      outcome #>> '{extensions,terminal_status}', occurred_at,
      usage_data ->> 'cost_micros',
      usage_data #>> '{extensions,calculated_cost_micros}',
      usage_data ->> 'currency', usage_data ->> 'amount_scale'
    )) > 1
  ) THEN
    RAISE EXCEPTION 'workbench economic fact identity conflicts with immutable source content';
  END IF;
END $$;

INSERT INTO workbench_economic_facts (
  source_system_id, source_version, source_ref, source_event_hash,
  evidence_event_id, request_id, execution_id, model_ref, source_project_ref,
  cost_project_ref, terminal_status, occurred_at, gateway_charged_micros,
  calculated_reference_micros, currency, amount_scale, projected_at
)
SELECT DISTINCT ON (source_system_id, source_version, source_ref)
  source_system_id, source_version, source_ref, source_event_hash,
  id, request_id, execution_id, capability ->> 'model', project_ref,
  NULL, outcome #>> '{extensions,terminal_status}', occurred_at,
  (usage_data ->> 'cost_micros')::numeric,
  (usage_data #>> '{extensions,calculated_cost_micros}')::numeric,
  usage_data ->> 'currency', (usage_data ->> 'amount_scale')::integer,
  now()
FROM evidence_events
WHERE source_version = 'unirouter.completed-execution.v1'
  AND event_type IN ('billing.execution.settled', 'billing.execution.cancelled')
  AND source_event_hash ~ '^sha256:[a-f0-9]{64}$'
  AND request_id IS NOT NULL
  AND execution_id ~ '^xe1_[a-f0-9]{64}$'
  AND occurred_at IS NOT NULL
  AND usage_data ->> 'cost_micros' ~ '^(0|[1-9][0-9]{0,29})$'
  AND usage_data #>> '{extensions,calculated_cost_micros}' ~ '^(0|[1-9][0-9]{0,29})$'
  AND usage_data ->> 'currency' = 'CNY'
  AND usage_data ->> 'amount_scale' = '1000000'
  AND outcome #>> '{extensions,terminal_status}' IN ('settled', 'cancelled')
ORDER BY source_system_id, source_version, source_ref, id
ON CONFLICT (source_system_id, source_version, source_ref) DO NOTHING;

CREATE TABLE workbench_access_grants (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  subject_id text NOT NULL,
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  source_project_ref text,
  cost_project_ref text,
  allow_summary boolean NOT NULL DEFAULT false,
  allow_details boolean NOT NULL DEFAULT false,
  allow_coverage boolean NOT NULL DEFAULT false,
  allow_preferences boolean NOT NULL DEFAULT false,
  active boolean NOT NULL DEFAULT true,
  revision bigint NOT NULL DEFAULT 1,
  updated_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT workbench_access_grants_subject_check CHECK (
    subject_id ~ '^xs_[a-z0-9]{16,64}$'
  ),
  CONSTRAINT workbench_access_grants_revision_check CHECK (revision > 0),
  CONSTRAINT workbench_access_grants_has_capability_check CHECK (
    allow_summary OR allow_details OR allow_coverage OR allow_preferences
  ),
  CONSTRAINT workbench_access_grants_scope_unique UNIQUE NULLS NOT DISTINCT (
    subject_id, source_system_id, source_project_ref, cost_project_ref
  )
);

CREATE INDEX workbench_access_grants_subject_active
  ON workbench_access_grants (subject_id, active, source_system_id, id);

CREATE TABLE workbench_readings (
  id text PRIMARY KEY,
  owner_subject text NOT NULL,
  contract_version text NOT NULL,
  metric_version text NOT NULL,
  period_start timestamptz NOT NULL,
  period_end timestamptz NOT NULL,
  display_timezone text NOT NULL,
  cutoff timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  money_basis text NOT NULL,
  currency text NOT NULL,
  filters jsonb NOT NULL,
  authorization_revision text NOT NULL,
  interpretation_revision text NOT NULL,
  membership_sha256 text NOT NULL,
  coverage jsonb NOT NULL,
  created_at timestamptz NOT NULL,
  CONSTRAINT workbench_readings_id_check CHECK (id ~ '^xwr_[A-Za-z0-9_-]{22}$'),
  CONSTRAINT workbench_readings_owner_check CHECK (owner_subject ~ '^xs_[a-z0-9]{16,64}$'),
  CONSTRAINT workbench_readings_version_check CHECK (
    contract_version = 'xinsight.workbench.v1'
    AND metric_version = 'xinsight.workbench.cost.v1'
  ),
  CONSTRAINT workbench_readings_period_check CHECK (
    period_start < period_end AND period_end <= cutoff AND cutoff < expires_at
  ),
  CONSTRAINT workbench_readings_money_check CHECK (
    money_basis IN ('gateway_charged', 'calculated_reference') AND currency = 'CNY'
  ),
  CONSTRAINT workbench_readings_filters_check CHECK (jsonb_typeof(filters) = 'object'),
  CONSTRAINT workbench_readings_coverage_check CHECK (jsonb_typeof(coverage) = 'object'),
  CONSTRAINT workbench_readings_digest_check CHECK (
    authorization_revision ~ '^sha256:[a-f0-9]{64}$'
    AND interpretation_revision ~ '^sha256:[a-f0-9]{64}$'
    AND membership_sha256 ~ '^sha256:[a-f0-9]{64}$'
  )
);

CREATE INDEX workbench_readings_owner_expiry
  ON workbench_readings (owner_subject, expires_at, id);

CREATE TABLE workbench_reading_members (
  reading_id text NOT NULL REFERENCES workbench_readings(id) ON DELETE CASCADE,
  ordinal bigint NOT NULL,
  economic_fact_id uuid NOT NULL REFERENCES workbench_economic_facts(id) ON DELETE RESTRICT,
  evidence_event_id uuid NOT NULL REFERENCES evidence_events(id) ON DELETE RESTRICT,
  source_system_id uuid NOT NULL REFERENCES source_systems(id) ON DELETE RESTRICT,
  source_version text NOT NULL,
  source_event_hash text NOT NULL,
  event_hash text NOT NULL,
  request_id text NOT NULL,
  execution_id text NOT NULL,
  model_ref text,
  source_project_ref text,
  cost_project_ref text,
  terminal_status text NOT NULL,
  occurred_at timestamptz NOT NULL,
  gateway_charged_micros numeric(30,0) NOT NULL,
  calculated_reference_micros numeric(30,0) NOT NULL,
  currency text NOT NULL,
  PRIMARY KEY (reading_id, ordinal),
  CONSTRAINT workbench_reading_members_fact_unique UNIQUE (reading_id, economic_fact_id),
  CONSTRAINT workbench_reading_members_hashes_check CHECK (
    source_event_hash ~ '^sha256:[a-f0-9]{64}$'
    AND event_hash ~ '^sha256:[a-f0-9]{64}$'
  ),
  CONSTRAINT workbench_reading_members_amounts_check CHECK (
    gateway_charged_micros >= 0
    AND calculated_reference_micros >= 0
    AND currency = 'CNY'
  )
);

CREATE INDEX workbench_reading_members_source_project
  ON workbench_reading_members (reading_id, source_project_ref, ordinal);
CREATE INDEX workbench_reading_members_cost_project
  ON workbench_reading_members (reading_id, cost_project_ref, ordinal);
CREATE INDEX workbench_reading_members_model
  ON workbench_reading_members (reading_id, model_ref, ordinal);

CREATE TABLE workbench_saved_filters (
  id text PRIMARY KEY,
  owner_subject text NOT NULL,
  name text NOT NULL,
  config_version text NOT NULL,
  config jsonb NOT NULL,
  content_sha256 text NOT NULL,
  revision bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL,
  CONSTRAINT workbench_saved_filters_id_check CHECK (id ~ '^xwf_[A-Za-z0-9_-]{22}$'),
  CONSTRAINT workbench_saved_filters_owner_check CHECK (owner_subject ~ '^xs_[a-z0-9]{16,64}$'),
  CONSTRAINT workbench_saved_filters_name_check CHECK (octet_length(name) BETWEEN 1 AND 120),
  CONSTRAINT workbench_saved_filters_version_check CHECK (config_version = 'xinsight.workbench.v1'),
  CONSTRAINT workbench_saved_filters_config_check CHECK (jsonb_typeof(config) = 'object'),
  CONSTRAINT workbench_saved_filters_hash_check CHECK (content_sha256 ~ '^sha256:[a-f0-9]{64}$'),
  CONSTRAINT workbench_saved_filters_revision_check CHECK (revision > 0),
  CONSTRAINT workbench_saved_filters_owner_name_unique UNIQUE (owner_subject, name)
);

CREATE INDEX workbench_saved_filters_owner_updated
  ON workbench_saved_filters (owner_subject, updated_at DESC, id);

CREATE TABLE workbench_saved_filter_operations (
  subject_id text NOT NULL,
  operation_type text NOT NULL,
  target_id text NOT NULL,
  idempotency_key text NOT NULL,
  content_sha256 text NOT NULL,
  result jsonb,
  created_at timestamptz NOT NULL,
  PRIMARY KEY (subject_id, operation_type, idempotency_key),
  CONSTRAINT workbench_saved_filter_operations_subject_check CHECK (subject_id ~ '^xs_[a-z0-9]{16,64}$'),
  CONSTRAINT workbench_saved_filter_operations_type_check CHECK (operation_type IN ('create','update')),
  CONSTRAINT workbench_saved_filter_operations_target_check CHECK (target_id ~ '^xwf_[A-Za-z0-9_-]{22}$'),
  CONSTRAINT workbench_saved_filter_operations_hash_check CHECK (content_sha256 ~ '^sha256:[a-f0-9]{64}$'),
  CONSTRAINT workbench_saved_filter_operations_result_check CHECK (result IS NULL OR jsonb_typeof(result)='object')
);
