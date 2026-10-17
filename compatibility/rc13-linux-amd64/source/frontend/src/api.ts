export type SemanticState =
  | "direct"
  | "derived"
  | "declared"
  | "suppressed"
  | "gap"
  | "not_applicable"
  | "unknown";

export type ProofClass =
  | "operator_approved_integration"
  | "synthetic_fixture"
  | "unclassified";

export type AssuranceMode =
  | "operator_attested_sidecar"
  | "contracted_pull"
  | "bounded_offline_import"
  | "synthetic_fixture"
  | "unclassified";

export type SyncState = "syncing" | "current" | "stale" | "gap";
export type ExecutionOutcome = "success" | "failure" | "partial" | "unknown";

export interface ProblemField {
  name: string;
  reason: string;
}

export interface Problem {
  type: string;
  title: string;
  status: number;
  code: string;
  correlation_id: string;
  retryable: boolean;
  fields?: ProblemField[];
}

export class ApiRequestError extends Error {
  readonly status: number;
  readonly problem?: Problem;

  constructor(status: number, problem?: Problem) {
    super(problem?.title ?? `HTTP ${status}`);
    this.name = "ApiRequestError";
    this.status = status;
    this.problem = problem;
  }
}

export class ApiResponseError extends Error {
  readonly path: string;

  constructor(path: string, reason: string) {
    super(`Invalid API response for ${path}: ${reason}`);
    this.name = "ApiResponseError";
    this.path = path;
  }
}

export function isAbortError(error: unknown): boolean {
  return typeof error === "object"
    && error !== null
    && "name" in error
    && (error as { name?: unknown }).name === "AbortError";
}

export type AccessRole =
  | "investigator"
  | "operator"
  | "access_operator"
  | "platform_admin";
export type AccessPermission =
  | "evidence.read"
  | "evidence.cite"
  | "investigation.write"
  | "access.sessions.read"
  | "access.sessions.revoke"
  | "service_identities.read"
  | "service_identities.revoke"
  | "connector.installations.manage";

export interface AccessSession {
  session_ref: string;
  subject_id: string;
  role: AccessRole;
  permissions: AccessPermission[];
  created_at: string;
  last_seen_at: string;
  idle_expires_at: string;
  absolute_expires_at: string;
}

export interface AccessSessionResponse {
  session: AccessSession;
  csrf_token: string;
}

export interface AccessSessionListResponse {
  items: AccessSession[];
}

export interface Page {
  as_of: string;
  count: number;
  has_more: boolean;
  next_cursor?: string;
}

export interface Freshness {
  integration_mode:
    | "sidecar"
    | "completed_execution_pull"
    | "connector_pull"
    | "connector_bounded_offline"
    | "synthetic_fixture";
  sync_state: SyncState;
  timeline_complete: boolean;
  source_terminal_at?: string;
  source_generated_at?: string;
  imported_at?: string;
  search_visible_at?: string;
  source_revision?: string;
  imported_revision?: string;
  source_watermark?: string;
  import_watermark?: string;
  last_successful_sync_at?: string;
}

export interface ExecutionSummary {
  execution_id: string;
  request_id: string;
  source_system_id: string;
  source_type: string;
  source_display_name?: string;
  outcome: ExecutionOutcome;
  first_effective_at: string;
  last_effective_at: string;
  event_count: number;
  gap_count: number;
  has_gaps: boolean;
  payload_capture_modes?: string[];
  proof_class: ProofClass;
  assurance_mode: AssuranceMode;
  freshness: Freshness;
}

export interface ExecutionSearchResponse {
  items: ExecutionSummary[];
  page: Page;
}

export interface ExecutionDetail {
  summary: ExecutionSummary;
  as_of: string;
  trace_ids?: string[];
}

export interface FieldProvenance {
  correlation?: SemanticState;
  identity?: SemanticState;
  capability?: SemanticState;
  decision?: SemanticState;
  usage?: SemanticState;
  outcome?: SemanticState;
  payload_policy?: SemanticState;
}

export interface TimelineNode {
  event_id: string;
  event_type: string;
  stage: string;
  severity: "info" | "warning" | "error";
  effective_at: string;
  occurred_at?: string;
  observed_at?: string;
  ingested_at: string;
  declared_parent_event_id?: string;
  declared_parent_resolved?: boolean;
  source_ref: string;
  source_type: string;
  decision_result?: string;
  outcome_status?: ExecutionOutcome;
  field_provenance: FieldProvenance;
}

export interface TimelineResponse {
  execution_id: string;
  total_event_count: number;
  items: TimelineNode[];
  page: Page;
  freshness: Freshness;
}

export interface CorrelationGroup {
  request_id?: string;
  trace_id?: string;
  span_id?: string;
  parent_span_id?: string;
  run_id?: string;
  session_id?: string;
  upstream_request_id?: string;
}

export interface IdentityGroup {
  actor_type?: string;
  actor_ref?: string;
  owner_ref?: string;
  org_id?: string;
  project_id?: string;
  team_ref?: string;
  api_key_ref?: string;
  end_user_ref?: string;
  key_type?: string;
}

export interface CapabilityGroup {
  capability_type?: string;
  target_ref?: string;
  model?: string;
  requested_model?: string;
  provider?: string;
  channel?: string;
}

export interface DecisionGroup {
  decision_kind?: string;
  decision_result?: string;
  decision_reason?: string;
  policy_ref?: string;
  rule_ref?: string;
}

export interface UsageGroup {
  prompt_tokens?: number;
  completion_tokens?: number;
  total_tokens?: number;
  estimated_tokens?: number;
  cost_cents?: number;
  cost_micros?: string;
  currency?: "CNY";
  amount_scale?: 1000000;
  money_schema?: "micro-cny.v1";
  latency_ms?: number;
  ttft_ms?: number;
  usage_source?: string;
  usage_confidence?: string;
}

export interface OutcomeGroup {
  outcome_status?: ExecutionOutcome;
  status_code?: number;
  error_code?: string;
  failure_kind?: string;
}

export interface PayloadPolicyGroup {
  payload_capture_mode?: string;
  redaction_state?: string;
  payload_ref?: string;
  retention_policy_ref?: string;
}

export interface EventSource {
  source_system_id: string;
  adapter_manifest_id: string;
  schema_version: string;
  source_type: string;
  source_version?: string;
  source_event_id?: string;
  source_event_type?: string;
  source_ref: string;
  event_hash: string;
  source_event_hash?: string;
  fidelity_level: "F0" | "F1" | "F2" | "F3";
}

export interface EvidenceEventDetail {
  execution_id: string;
  event_id: string;
  event_type: string;
  stage: string;
  severity: "info" | "warning" | "error";
  capture_mode: string;
  occurred_at?: string;
  observed_at?: string;
  ingested_at: string;
  payload_capture_mode?: string;
  correlation?: CorrelationGroup;
  identity?: IdentityGroup;
  capability?: CapabilityGroup;
  decision?: DecisionGroup;
  usage?: UsageGroup;
  outcome?: OutcomeGroup;
  payload_policy?: PayloadPolicyGroup;
  field_provenance: FieldProvenance;
  source: EventSource;
}

export interface CaptureGap {
  gap_id: string;
  evidence_event_id?: string;
  gap_type: string;
  candidate_gap_types?: string[];
  affected_claim: string;
  gap_reason: string;
  severity: "info" | "warning" | "error";
  recommendation?: string;
  created_at: string;
  resolution_state?: string;
  resolution_note?: string;
  resolution_updated_at?: string;
}

export interface GapResponse {
  execution_id: string;
  total_gap_count: number;
  items: CaptureGap[];
  page: Page;
  freshness: Freshness;
}

export type PresentationMode = "causal_trace" | "chronology" | "terminal_snapshot";
export type CoverageStatus = "present" | "gap" | "not_applicable" | "unknown";

export interface EvidenceCitation {
  evidence_event_id: string;
  source_ref: string;
  event_hash: string;
  deep_link: string;
}

export interface InvestigationClaim {
  code:
    | "provider_failed_then_retry_scheduled"
    | "primary_provider_failed_then_fallback_succeeded"
    | "execution_completed_successfully"
    | string;
  evidence_event_ids: string[];
  derivation_basis: string;
}

export interface StageCoverage {
  stage:
    | "identity"
    | "routing"
    | "provider"
    | "retry_fallback"
    | "billing"
    | "usage"
    | "logging"
    | string;
  status: CoverageStatus;
  evidence_event_ids?: string[];
  gap_ids?: string[];
}

export interface InvestigationNode {
  event_id: string;
  event_type: string;
  source_event_type?: string;
  kind:
    | "identity_bound"
    | "route_selected"
    | "provider_failed"
    | "provider_succeeded"
    | "retry_scheduled"
    | "fallback_selected"
    | "billing_recorded"
    | "execution_completed"
    | "capture_gap"
    | "evidence_observed"
    | string;
  proof_basis: string;
  stage: string;
  severity: "info" | "warning" | "error";
  effective_at: string;
  declared_parent_event_id?: string;
  actor_type?: string;
  actor_ref?: string;
  org_id?: string;
  project_id?: string;
  provider?: string;
  model?: string;
  requested_model?: string;
  decision_kind?: string;
  decision_result?: string;
  decision_reason?: string;
  outcome_status?: ExecutionOutcome;
  status_code?: number;
  latency_ms?: number;
  total_tokens?: number;
  cost_micros?: string;
  citation: EvidenceCitation;
}

export interface InvestigationEdge {
  from: string;
  to: string;
  edge_basis: "source_declared_parent_span" | string;
}

export interface InvestigationIntegrity {
  source_profile_complete: boolean;
  import_complete: boolean;
  projection_complete: boolean;
  node_count: number;
  total_event_count: number;
  resolved_parent_count: number;
  parent_eligible_count: number;
  unresolved_parent_count: number;
  causal_complete: boolean;
  truncated: boolean;
}

export interface CompletenessAxis {
  axis: "source" | "import" | "projection" | "causal" | string;
  status: "complete" | "incomplete" | "not_available" | string;
  basis: string;
}

export interface UnprovableAction {
  code: string;
  statement: string;
  gap_ids?: string[];
}

export interface InvestigationProjection {
  schema_version: "xinsight.investigation-projection.v1" | string;
  execution: ExecutionSummary;
  as_of: string;
  presentation_mode: PresentationMode;
  summary_claims: InvestigationClaim[];
  stage_coverage: StageCoverage[];
  nodes: InvestigationNode[];
  edges: InvestigationEdge[];
  gaps: CaptureGap[];
  freshness: Freshness;
  integrity: InvestigationIntegrity;
  completeness: CompletenessAxis[];
  unprovable_actions: UnprovableAction[];
}

export interface TraceJourneyNode {
  execution_id: string;
  event_id: string;
  request_id: string;
  source_type: string;
  source_ref: string;
  event_hash: string;
  trace_id: string;
  span_id: string;
  parent_span_id?: string;
  actor_type?: string;
  actor_ref?: string;
  org_id?: string;
  project_id?: string;
  action?: string;
  service_name?: string;
  provider?: string;
  model?: string;
  outcome?: ExecutionOutcome;
  effective_at: string;
}

export interface TraceJourneyEdge {
  from_event_id: string;
  to_event_id: string;
  edge_basis: "source_declared_parent_span_across_execution";
}

export interface TraceJourney {
  schema_version: "xinsight.trace-journey.v1" | string;
  available: boolean;
  unavailable_reason?: string;
  anchor_execution_id: string;
  source_system_id?: string;
  trace_id?: string;
  as_of: string;
  nodes: TraceJourneyNode[];
  edges: TraceJourneyEdge[];
  integrity: {
    node_count: number;
    execution_count: number;
    declared_parent_count: number;
    resolved_parent_count: number;
    unresolved_parent_count: number;
    ambiguous_parent_count: number;
    cycle_detected: boolean;
    truncated: boolean;
    source_timeline_complete: boolean;
    visible_parent_resolution_complete: boolean;
  };
  evidence_boundary: string;
}

export interface SourceOption {
  source_system_id: string;
  source_key: string;
  source_type: string;
  display_name: string;
  source_version?: string;
}

export interface SourceListResponse {
  items: SourceOption[];
}

export interface SearchFilters {
  asOf?: string;
  requestID?: string;
  requestIDPrefix?: string;
  sourceSystemID?: string;
  outcome?: ExecutionOutcome;
  hasGaps?: boolean;
  from?: string;
  to?: string;
}

export interface ConnectorCheckpointStatus {
  epoch: number;
  epoch_operation_id?: string;
  cursor_key: string;
  cursor_present: boolean;
  cursor_digest?: string;
  high_watermark_present: boolean;
  high_watermark_digest?: string;
  worker_status: string;
  import_status: string;
  last_import_started_at?: string;
  last_import_completed_at?: string;
  last_poll_failed_at?: string;
  consecutive_failures: number;
  pages_committed: number;
  items_received: number;
  items_committed: number;
  duplicate_noop: number;
  error_present: boolean;
  updated_at?: string;
}

export interface ConnectorTrust {
  connector_key: string;
  source_system_id: string;
  source_key: string;
  source_type: string;
  source_version?: string;
  source_display_name: string;
  source_status: string;
  adapter_manifest_id: string;
  adapter_id: string;
  adapter_version: string;
  manifest_active: boolean;
  fidelity_level: string;
  ingestion_modes: string[];
  payload_behavior: string;
  idempotency_strategy: string;
  cursor_strategy?: string;
  trust_state: "inactive" | "degraded" | "not_observed" | "syncing" | "reported_current" | string;
  checkpoint: ConnectorCheckpointStatus;
  delivery_state_boundary: string;
}

export interface ConnectorListResponse {
  items: ConnectorTrust[];
  installations: ConnectorInstallation[];
  installation_control: ConnectorInstallationControl;
  delivery_state_boundary: string;
}

export interface ConnectorInstallation {
  schema_version: "xinsight.connector-installation.v1";
  installation_id: string;
  deployment_id: string;
  connector_type: "unirouter";
  requested_event_scope: string[];
  final_event_scope?: string[];
  project_id?: string;
  source_instance_sha256?: string;
  installation_acknowledgement_sha256?: string;
  status: "pending" | "connected" | "paused" | "abnormal" | "disconnected";
  desired_state: "active" | "paused" | "disconnected";
  observed_state: string;
  status_reason_code?: string;
  last_agent_seen_at?: string;
  created_at: string;
  updated_at: string;
  version: number;
}

export interface ConnectorInstallationControl {
  enabled: boolean;
  connector_type: "unirouter";
  event_scope: string[];
  ordinary_sync_requires_assurance: false;
}

export interface ConnectorInvitation {
  schema_version: "xinsight.connector-installation-invitation.v1";
  invitation_id: string;
  installation_id: string;
  deployment_id: string;
  audience: "unirouter";
  xinsight_origin: string;
  requested_event_scope: string[];
  expires_at: string;
  install_url: string;
}

export interface ConnectorInvitationResponse {
  invitation: ConnectorInvitation;
  installation: ConnectorInstallation;
}

export interface InvestigationCase {
  id: string;
  title: string;
  description: string;
  status: "open" | "sealed" | "archived";
  created_by_subject: string;
  created_at: string;
  updated_at: string;
  version: number;
  sealed_at?: string;
  sealed_by_subject?: string;
  sealed_revision?: number;
  sealed_digest?: string;
}

export interface CaseExecution {
  case_id: string;
  execution_id: string;
  as_of: string;
  added_by_subject: string;
  added_at: string;
}

export interface CaseFinding {
  id: string;
  case_id: string;
  title: string;
  statement: string;
  classification: "evidence_backed" | "deterministic_derived" | "hypothesis" | "unknown";
  status: string;
  created_by_subject: string;
  created_at: string;
  updated_at: string;
  version: number;
}

export interface CaseEvidenceLink {
  case_id: string;
  finding_id?: string;
  evidence_event_id: string;
  relation: "supports" | "contradicts" | "context";
  event_hash: string;
  source_ref: string;
  added_by_subject: string;
  added_at: string;
}

export interface CaseGapLink {
  case_id: string;
  finding_id?: string;
  gap_id: string;
  relation: "limits" | "blocks" | "context";
  affected_claim: string;
  gap_type: string;
  added_by_subject: string;
  added_at: string;
}

export interface CaseAnnotation {
  id: string;
  case_id: string;
  target_kind: string;
  target_ref: string;
  body: string;
  created_by_subject: string;
  created_at: string;
}

export interface AlertCaseLink {
  alert_id: string;
  case_id: string;
  alert_state: "open" | "acknowledged" | "resolved";
  linked_by_subject: string;
  linked_at: string;
}

export interface CaseRecord {
  case: InvestigationCase;
  executions: CaseExecution[];
  findings: CaseFinding[];
  evidence: CaseEvidenceLink[];
  gaps: CaseGapLink[];
  alerts: AlertCaseLink[];
  annotations: CaseAnnotation[];
}

export interface CaseListResponse {
  items: InvestigationCase[];
}

export interface SavedView {
  id: string;
  owner_subject: string;
  name: string;
  filters: Record<string, unknown>;
  presentation: string;
  created_at: string;
  updated_at: string;
  version: number;
}

export interface SavedViewListResponse {
  items: SavedView[];
}

export interface ServiceIdentity {
  id: string;
  display_name: string;
  status: string;
  permissions: string[];
  created_by_subject: string;
  created_at: string;
  updated_at: string;
  expires_at?: string;
  revoked_at?: string;
  revoke_reason?: string;
  last_used_at?: string;
}

export interface ServiceIdentityListResponse {
  items: ServiceIdentity[];
}

export interface AuthoritativeSession {
  session_id: string;
  source_system_id: string;
  source_type: string;
  source_display_name: string;
  source_session_ref: string;
  source_session_type: string;
  observed_at: string;
  execution_count: number;
  metadata: Record<string, unknown>;
}

export interface AuthoritativeSessionListResponse {
  items: AuthoritativeSession[];
  identity_boundary: string;
}

export interface SessionExecutionLink {
  execution_id: string;
  source_link_ref: string;
  linked_at: string;
}

export interface AuthoritativeSessionDetail {
  session: AuthoritativeSession;
  executions: SessionExecutionLink[];
}

export interface ComparisonValue {
  state: "observed" | "partial" | "missing";
  value?: string;
  unit?: string;
  evidence_event_ids?: string[];
}

export interface ComparisonDimension {
  dimension: string;
  left: ComparisonValue;
  right: ComparisonValue;
  relation: "equal" | "different" | "unavailable";
}

export interface ExecutionComparison {
  schema_version: string;
  left_as_of: string;
  right_as_of: string;
  left: ExecutionSummary;
  right: ExecutionSummary;
  dimensions: ComparisonDimension[];
}

export interface EvaluationFeedback {
  id: string;
  execution_id: string;
  source_system_id: string;
  evaluator_ref: string;
  metric_name: string;
  metric_value_decimal?: string;
  label?: string;
  explanation?: string;
  observed_at: string;
  evidence_event_ids: string[];
}

export interface ExecutionVersionDimension {
  execution_id: string;
  dimension: string;
  version_ref: string;
  source_system_id: string;
  evidence_event_id?: string;
  observed_at: string;
}

export interface InvestigationAlert {
  id: string;
  source_system_id: string;
  source_alert_ref: string;
  title: string;
  severity: "info" | "warning" | "error" | "critical";
  state: "open" | "acknowledged" | "resolved";
  execution_id?: string;
  case_id?: string;
  observed_at: string;
}

export interface PostureRiskSummary {
  open_total: number;
  critical: number;
  error: number;
  warning: number;
  info: number;
  affected_executions: number;
  unlinked: number;
  items: InvestigationAlert[];
}

export interface PostureExecutionSummary {
  total: number;
  success: number;
  failure: number;
  partial: number;
  unknown: number;
  with_gaps: number;
  total_gaps: number;
}

export interface PostureCoverage {
  dimension: "temporal" | "identity" | "outcome" | "causal";
  label: string;
  available: boolean;
  numerator: number;
  denominator: number;
  ratio_decimal?: string;
  unit: string;
  unresolved_count?: number;
  ambiguous_count?: number;
  basis: string;
  limitation: string;
}

export interface PostureSourceSummary {
  source_system_id: string;
  source_type: string;
  source_display_name: string;
  execution_total: number;
  failure_total: number;
  executions_with_gaps: number;
  last_effective_at: string;
}

export interface PostureTrendBucket {
  bucket_start: string;
  execution_total: number;
  failure_total: number;
  partial_total: number;
  executions_with_gaps: number;
  open_alerts: number;
}

export interface PostureSnapshot {
  schema_version: "xinsight.posture.v1";
  as_of: string;
  from: string;
  to: string;
  source_system_id?: string;
  risk: PostureRiskSummary;
  executions: PostureExecutionSummary;
  coverage: PostureCoverage[];
  sources: PostureSourceSummary[];
  trend: {
    bucket_unit: "hour" | "day";
    items: PostureTrendBucket[];
    boundary: string;
  };
  boundaries: string[];
}

export interface PostureFilters {
  asOf?: string;
  from?: string;
  to?: string;
  sourceSystemID?: string;
}

export interface DiscoveryHypothesis {
  id: string;
  analysis_run_id: string;
  analysis_as_of: string;
  sample_size: number;
  input_digest: string;
  hypothesis_type:
    | "coverage_pattern"
    | "process_pattern"
    | "topic_cluster"
    | "anomaly"
    | "coalition"
    | "authority_drift";
  title: string;
  statement: string;
  support_score_decimal?: string;
  status: "candidate" | "reviewed" | "dismissed" | "abstained";
  evidence_event_ids: string[];
  execution_ids: string[];
  limitations: string[];
  abstention_reason?: string;
  method_version: string;
  generated_at: string;
}

export interface AccessProviderCapabilities {
  local_account_state: string;
  oidc_contract: string;
  oidc_runtime_state: string;
  boundary: string;
}

async function requestJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, {
    method: "GET",
    headers: { Accept: "application/json, application/problem+json" },
    cache: "no-store",
    credentials: "same-origin",
    signal,
  });
  const decoded = await decodeResponseJSON(response);
  if (!response.ok) {
    const problem = isProblem(decoded) ? decoded : undefined;
    if (response.status === 401) {
      window.dispatchEvent(new Event("xinsight:unauthorized"));
    }
    throw new ApiRequestError(response.status, problem);
  }
  return requireJSONObject<T>(path, response, decoded);
}

async function requestAccessJSON<T>(
  path: string,
  options: {
    method?: "GET" | "POST";
    body?: unknown;
    csrfToken?: string;
    notifyUnauthorized?: boolean;
    allowEmptyResponse?: boolean;
    signal?: AbortSignal;
  } = {},
): Promise<T> {
  const method = options.method ?? "GET";
  const headers: Record<string, string> = {
    Accept: "application/json, application/problem+json",
  };
  if (method === "POST") {
    headers["Content-Type"] = "application/json";
  }
  if (options.csrfToken) {
    headers["X-CSRF-Token"] = options.csrfToken;
  }
  const response = await fetch(path, {
    method,
    headers,
    cache: "no-store",
    credentials: "same-origin",
    body: method === "POST" ? JSON.stringify(options.body ?? {}) : undefined,
    signal: options.signal,
  });
  const decoded = await decodeResponseJSON(response);
  if (!response.ok) {
    const problem = isProblem(decoded) ? decoded : undefined;
    if (response.status === 401 && options.notifyUnauthorized !== false) {
      window.dispatchEvent(new Event("xinsight:unauthorized"));
    }
    throw new ApiRequestError(response.status, problem);
  }
  if (options.allowEmptyResponse && decoded === undefined) {
    return undefined as T;
  }
  return requireJSONObject<T>(path, response, decoded);
}

async function decodeResponseJSON(response: Response): Promise<unknown> {
  const text = await response.text();
  if (text === "") {
    return undefined;
  }
  try {
    return JSON.parse(text);
  } catch {
    return Symbol.for("xinsight.invalid-json-response");
  }
}

function requireJSONObject<T>(path: string, response: Response, decoded: unknown): T {
  const contentType = response.headers.get("content-type")?.toLowerCase() ?? "";
  if (!contentType.includes("application/json") && !contentType.includes("application/problem+json")) {
    throw new ApiResponseError(path, "missing JSON content type");
  }
  if (decoded === Symbol.for("xinsight.invalid-json-response")) {
    throw new ApiResponseError(path, "malformed JSON body");
  }
  if (typeof decoded !== "object" || decoded === null || Array.isArray(decoded)) {
    throw new ApiResponseError(path, "expected a JSON object body");
  }
  return decoded as T;
}

function isProblem(value: unknown): value is Problem {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  const candidate = value as Partial<Problem>;
  return (
    typeof candidate.status === "number" &&
    typeof candidate.code === "string" &&
    typeof candidate.correlation_id === "string"
  );
}

function setIfPresent(params: URLSearchParams, key: string, value: string | undefined) {
  if (value !== undefined && value !== "") {
    params.set(key, value);
  }
}

export const investigationAPI = {
  getPosture(filters: PostureFilters = {}, signal?: AbortSignal) {
    const params = new URLSearchParams();
    setIfPresent(params, "as_of", filters.asOf);
    setIfPresent(params, "from", filters.from);
    setIfPresent(params, "to", filters.to);
    setIfPresent(params, "source_system_id", filters.sourceSystemID);
    const query = params.size > 0 ? `?${params.toString()}` : "";
    return requestJSON<PostureSnapshot>(`/api/v1/posture${query}`, signal);
  },

  search(filters: SearchFilters, cursor?: string, signal?: AbortSignal) {
    const params = new URLSearchParams();
    setIfPresent(params, "request_id", filters.requestID);
    setIfPresent(params, "as_of", filters.asOf);
    setIfPresent(params, "request_id_prefix", filters.requestIDPrefix);
    setIfPresent(params, "source_system_id", filters.sourceSystemID);
    setIfPresent(params, "outcome", filters.outcome);
    if (filters.hasGaps !== undefined) {
      params.set("has_gaps", String(filters.hasGaps));
    }
    setIfPresent(params, "from", filters.from);
    setIfPresent(params, "to", filters.to);
    params.set("limit", "50");
    setIfPresent(params, "cursor", cursor);
    return requestJSON<ExecutionSearchResponse>(`/api/v1/executions?${params.toString()}`, signal);
  },

  listSources(signal?: AbortSignal) {
    return requestJSON<SourceListResponse>("/api/v1/sources?limit=100", signal);
  },

  getExecution(executionID: string, asOf?: string, signal?: AbortSignal) {
    const params = new URLSearchParams();
    setIfPresent(params, "as_of", asOf);
    const query = params.size > 0 ? `?${params.toString()}` : "";
    return requestJSON<ExecutionDetail>(`/api/v1/executions/${encodeURIComponent(executionID)}${query}`, signal);
  },

  getInvestigation(executionID: string, asOf: string, signal?: AbortSignal) {
    const params = new URLSearchParams({ as_of: asOf });
    return requestJSON<Omit<InvestigationProjection, "gaps"> & { gaps: CaptureGap[] | null }>(
      `/api/v1/executions/${encodeURIComponent(executionID)}/investigation?${params.toString()}`,
      signal,
    ).then((projection) => ({
      ...projection,
      gaps: projection.gaps ?? [],
      summary_claims: projection.summary_claims.map((claim) => ({
        ...claim,
        derivation_basis: claim.derivation_basis ?? "deterministic_projection",
      })),
      nodes: projection.nodes.map((node) => ({
        ...node,
        proof_basis: node.proof_basis ?? "direct",
        citation: {
          ...node.citation,
          deep_link:
            node.citation.deep_link ??
            `/executions/${encodeURIComponent(executionID)}?as_of=${encodeURIComponent(asOf)}&evidence=${encodeURIComponent(node.event_id)}`,
        },
      })),
      completeness: projection.completeness ?? [],
      unprovable_actions: projection.unprovable_actions ?? [],
      integrity: {
        ...projection.integrity,
        unresolved_parent_count: projection.integrity.unresolved_parent_count ?? 0,
        causal_complete:
          projection.integrity.causal_complete ??
          ((projection.integrity.unresolved_parent_count ?? 0) === 0 && !projection.integrity.truncated),
      },
    }));
  },

  getTraceJourney(executionID: string, asOf: string, signal?: AbortSignal) {
    const params = new URLSearchParams({ as_of: asOf });
    return requestJSON<TraceJourney>(
      `/api/v1/executions/${encodeURIComponent(executionID)}/trace-journey?${params.toString()}`,
      signal,
    );
  },

  listTimeline(executionID: string, asOf: string, cursor?: string, signal?: AbortSignal) {
    const params = new URLSearchParams({ as_of: asOf, limit: "100" });
    setIfPresent(params, "cursor", cursor);
    return requestJSON<TimelineResponse>(
      `/api/v1/executions/${encodeURIComponent(executionID)}/timeline?${params.toString()}`,
      signal,
    );
  },

  getEvent(executionID: string, eventID: string, asOf: string, signal?: AbortSignal) {
    const params = new URLSearchParams({ as_of: asOf });
    return requestJSON<EvidenceEventDetail>(
      `/api/v1/executions/${encodeURIComponent(executionID)}/events/${encodeURIComponent(eventID)}?${params.toString()}`,
      signal,
    );
  },

  listGaps(executionID: string, asOf: string, cursor?: string, signal?: AbortSignal) {
    const params = new URLSearchParams({ as_of: asOf, limit: "50" });
    setIfPresent(params, "cursor", cursor);
    return requestJSON<GapResponse>(
      `/api/v1/executions/${encodeURIComponent(executionID)}/gaps?${params.toString()}`,
      signal,
    );
  },

  listConnectors(signal?: AbortSignal) {
    return requestJSON<ConnectorListResponse>("/api/v1/connectors", signal);
  },

  listSessions(signal?: AbortSignal) {
    return requestJSON<AuthoritativeSessionListResponse>("/api/v1/sessions?limit=100", signal);
  },

  getSession(sessionID: string, signal?: AbortSignal) {
    return requestJSON<AuthoritativeSessionDetail>(
      `/api/v1/sessions/${encodeURIComponent(sessionID)}`,
      signal,
    );
  },

  compare(
    left: string,
    leftAsOf: string,
    right: string,
    rightAsOf: string,
    signal?: AbortSignal,
  ) {
    const params = new URLSearchParams({
      left,
      left_as_of: leftAsOf,
      right,
      right_as_of: rightAsOf,
    });
    return requestJSON<ExecutionComparison>(`/api/v1/compare?${params.toString()}`, signal);
  },

  listFeedback(executionID: string, asOf: string, signal?: AbortSignal) {
    const params = new URLSearchParams({ as_of: asOf });
    return requestJSON<{
      execution_id: string;
      as_of: string;
      items: EvaluationFeedback[];
      boundary: string;
    }>(`/api/v1/executions/${encodeURIComponent(executionID)}/feedback?${params.toString()}`, signal);
  },

  listVersions(executionID: string, asOf: string, signal?: AbortSignal) {
    const params = new URLSearchParams({ as_of: asOf });
    return requestJSON<{
      execution_id: string;
      as_of: string;
      items: ExecutionVersionDimension[];
    }>(`/api/v1/executions/${encodeURIComponent(executionID)}/versions?${params.toString()}`, signal);
  },

  listAlerts(asOf?: string, signal?: AbortSignal) {
    const params = new URLSearchParams({ limit: "100" });
    setIfPresent(params, "as_of", asOf);
    return requestJSON<{ as_of: string; items: InvestigationAlert[] }>(
      `/api/v1/alerts?${params.toString()}`,
      signal,
    );
  },

  listHypotheses(asOf?: string, signal?: AbortSignal) {
    const params = new URLSearchParams({ limit: "100" });
    setIfPresent(params, "as_of", asOf);
    return requestJSON<{
      as_of: string;
      items: DiscoveryHypothesis[];
      boundary: string;
    }>(`/api/v1/discovery/hypotheses?${params.toString()}`, signal);
  },
};

export const workspaceAPI = {
  createConnectorInvitation(csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<ConnectorInvitationResponse>("/api/v1/connectors/invitations", {
      method: "POST",
      body: {},
      csrfToken,
      signal,
    });
  },
  listCases(signal?: AbortSignal) {
    return requestAccessJSON<CaseListResponse>("/api/v1/cases", { signal });
  },

  getCase(caseID: string, signal?: AbortSignal) {
    return requestAccessJSON<CaseRecord>(`/api/v1/cases/${encodeURIComponent(caseID)}`, { signal });
  },

  createCase(title: string, description: string, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<InvestigationCase>("/api/v1/cases", {
      method: "POST",
      csrfToken,
      body: { title, description },
      signal,
    });
  },

  addExecution(caseID: string, executionID: string, asOf: string, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<CaseExecution>(
      `/api/v1/cases/${encodeURIComponent(caseID)}/executions`,
      {
        method: "POST",
        csrfToken,
        body: { execution_id: executionID, as_of: asOf },
        signal,
      },
    );
  },

  addEvidence(
    caseID: string,
    citation: EvidenceCitation,
    csrfToken: string,
    signal?: AbortSignal,
  ) {
    return requestAccessJSON<CaseEvidenceLink>(
      `/api/v1/cases/${encodeURIComponent(caseID)}/evidence`,
      {
        method: "POST",
        csrfToken,
        body: {
          evidence_event_id: citation.evidence_event_id,
          relation: "supports",
          event_hash: citation.event_hash,
          source_ref: citation.source_ref,
        },
        signal,
      },
    );
  },

  addGap(caseID: string, gap: CaptureGap, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<CaseGapLink>(
      `/api/v1/cases/${encodeURIComponent(caseID)}/gaps`,
      {
        method: "POST",
        csrfToken,
        body: {
          gap_id: gap.gap_id,
          relation: "limits",
          affected_claim: gap.affected_claim,
          gap_type: gap.gap_type,
        },
        signal,
      },
    );
  },

  createFinding(
    caseID: string,
    title: string,
    statement: string,
    classification: CaseFinding["classification"],
    csrfToken: string,
    signal?: AbortSignal,
  ) {
    return requestAccessJSON<CaseFinding>(
      `/api/v1/cases/${encodeURIComponent(caseID)}/findings`,
      {
        method: "POST",
        csrfToken,
        body: { title, statement, classification },
        signal,
      },
    );
  },

  createAnnotation(
    caseID: string,
    targetKind: string,
    targetRef: string,
    body: string,
    csrfToken: string,
    signal?: AbortSignal,
  ) {
    return requestAccessJSON<CaseAnnotation>(
      `/api/v1/cases/${encodeURIComponent(caseID)}/annotations`,
      {
        method: "POST",
        csrfToken,
        body: { target_kind: targetKind, target_ref: targetRef, body },
        signal,
      },
    );
  },

  sealCase(caseID: string, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<InvestigationCase>(
      `/api/v1/cases/${encodeURIComponent(caseID)}/seal`,
      { method: "POST", csrfToken, body: {}, signal },
    );
  },

  listSavedViews(signal?: AbortSignal) {
    return requestAccessJSON<SavedViewListResponse>("/api/v1/saved-views", { signal });
  },

  createSavedView(
    name: string,
    filters: SearchFilters,
    presentation: string,
    csrfToken: string,
    signal?: AbortSignal,
  ) {
    return requestAccessJSON<SavedView>("/api/v1/saved-views", {
      method: "POST",
      csrfToken,
      body: { name, filters: wireSearchFilters(filters), presentation },
      signal,
    });
  },

  listServiceIdentities(signal?: AbortSignal) {
    return requestAccessJSON<ServiceIdentityListResponse>("/api/v1/service-identities", { signal });
  },

  revokeServiceIdentity(id: string, reason: string, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<ServiceIdentity>(
      `/api/v1/service-identities/${encodeURIComponent(id)}/revoke`,
      { method: "POST", csrfToken, body: { reason }, signal },
    );
  },

  resolveGap(gapID: string, state: string, note: string, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<{
      gap_id: string;
      state: string;
      note: string;
      updated_by_subject: string;
      updated_at: string;
      version: number;
    }>(`/api/v1/gaps/${encodeURIComponent(gapID)}/resolution`, {
      method: "POST",
      csrfToken,
      body: { state, note },
      signal,
    });
  },

  linkAlertToCase(alertID: string, caseID: string, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<AlertCaseLink>(`/api/v1/alerts/${encodeURIComponent(alertID)}/case`, {
      method: "POST",
      csrfToken,
      body: { case_id: caseID },
      signal,
    });
  },
};

export const accessAPI = {
  login(username: string, password: string, signal?: AbortSignal) {
    return requestAccessJSON<AccessSessionResponse>("/api/v1/access/login", {
      method: "POST",
      body: { username, password },
      notifyUnauthorized: false,
      signal,
    });
  },

  session(signal?: AbortSignal) {
    return requestAccessJSON<AccessSessionResponse>("/api/v1/access/session", {
      notifyUnauthorized: false,
      signal,
    });
  },

  logout(csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<void>("/api/v1/access/logout", {
      method: "POST",
      csrfToken,
      allowEmptyResponse: true,
      signal,
    });
  },

  listSessions(signal?: AbortSignal) {
    return requestAccessJSON<AccessSessionListResponse>("/api/v1/access/sessions", { signal });
  },

  revokeSession(sessionRef: string, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<void>(
      `/api/v1/access/sessions/${encodeURIComponent(sessionRef)}/revoke`,
      {
        method: "POST",
        csrfToken,
        allowEmptyResponse: true,
        signal,
      },
    );
  },

  providerCapabilities(signal?: AbortSignal) {
    return requestAccessJSON<AccessProviderCapabilities>("/api/v1/access/providers", {
      notifyUnauthorized: false,
      signal,
    });
  },
};

function wireSearchFilters(filters: SearchFilters): Record<string, string | boolean> {
  const output: Record<string, string | boolean> = {};
  if (filters.requestID) output.request_id = filters.requestID;
  if (filters.requestIDPrefix) output.request_id_prefix = filters.requestIDPrefix;
  if (filters.sourceSystemID) output.source_system_id = filters.sourceSystemID;
  if (filters.outcome) output.outcome = filters.outcome;
  if (filters.hasGaps !== undefined) output.has_gaps = filters.hasGaps;
  if (filters.from) output.from = filters.from;
  if (filters.to) output.to = filters.to;
  return output;
}
