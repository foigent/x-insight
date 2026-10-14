import type {SetupPreview,SetupCreated,SetupStatus} from "./ConnectorSetupPage";
import type { BudgetRequest, BudgetReading, BudgetItems } from "./budget";
import type * as Runtime from "./runtime";
import type { AssessmentAction, AssessmentCommand, AssessmentHistory, AssessmentView, BasisSelection, ReviewBasis, ReviewCreationCommand, ReviewCreationReceipt } from "./assessmentTypes";

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
  | "platform_admin"
  | "workbench_reader"
  | "investigation_reader"
 | "workbench_investigation_reader"
 | "workbench_investigation_reviewer"
 | "workbench_purpose_reviewer";
export type AccessPermission =
 | "workbench.purpose.review"
 | "investigation.scoped.read"
 | "investigation.scoped.cite"
 | "investigation.scoped.write"
  | "evidence.read"
  | "evidence.cite"
  | "investigation.write"
  | "investigation.preferences.write"
  | "access.sessions.read"
  | "access.sessions.revoke"
  | "service_identities.read"
  | "service_identities.revoke"
  | "connector.installations.manage"
  | "review.policy.manage"
  | "workbench.summary.read"
  | "workbench.details.read"
  | "workbench.coverage.read"
  | "workbench.preferences.write";

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
 scope_revision?: string;
  session: AccessSession;
  csrf_token: string;
}

export interface ReviewCaptureSettings {
  mode: "disabled" | "full_content";
  retention_seconds: number;
  project_quota_bytes: number;
  max_item_bytes: number;
}
export interface ReviewCaptureDefault {
  desired: ReviewCaptureSettings;
  desired_revision: number;
  effective?: ReviewCaptureSettings;
  effective_revision: number;
  requested_at: string;
  effective_at?: string;
}
export type ReviewCaptureDefaultView =
  | { state: "unconfigured"; suggested: ReviewCaptureSettings }
  | { state: "configured" | "pending" | "effective"; policy: ReviewCaptureDefault; drift: boolean };
export interface ReviewCaptureProjectChoice {
  source_system_id: string; source_instance: string; org_id: string; project_id: string;
  source_name: string; source_type: string; xi_project_id?: string; xi_project_name?: string;
}
export interface ReviewCaptureXIProject { id: string; display_name: string }
export interface ReviewCaptureProjectCatalog {
  items: ReviewCaptureProjectChoice[]; projects: ReviewCaptureXIProject[];
}
export interface ReviewCaptureException {
  policy_ref?: string;
  selector_kind: "tenant_category" | "source_project" | "source_project_category" | "account" | "account_category";
  source_system_id?: string; source_instance?: string; org_id?: string; project_id?: string;
  account_authority?: string; account_id?: string; content_kind?: string;
  mode: "disabled" | "full_content"; retention_seconds: number; max_item_bytes: number; provenance?: string;
}
export interface ReviewCaptureExceptionState extends ReviewCaptureException {
  policy_ref: string; desired_revision: number; effective_revision: number; effective_mode?: string;
}
export interface VerifiedReviewAccount {
  source_system_id: string; source_instance: string; org_id: string; project_id: string;
  authority: "ur_key_owner"; account_id: string;
}
export interface ReviewCaptureExceptionCatalog {
  items: ReviewCaptureExceptionState[]; verified_accounts: VerifiedReviewAccount[];
  items_truncated: boolean; accounts_truncated: boolean;
}
export interface URPolicyConfig {
  mode: "disabled" | "metadata_only" | "full_content";
  supported_protocols: string[]; observation_points: string[]; allowed_purposes: string[];
  retention_seconds?: number; quota_bytes?: number; max_saved_bytes?: number;
  on_limit?: string; redaction_ref?: { id: string; version: string };
}
export interface URPolicyLayer {
  policy_ref: string; explicit: boolean;
  desired?: { policy_version: string; config: URPolicyConfig };
  effective?: { policy_version: string; config: URPolicyConfig };
  desired_execution_storage_mode?: string; effective_execution_storage_mode?: string;
  project_quota_bytes?: number; application_status?: string; application_reason?: string;
  desired_default_project_quota_bytes?: number; effective_default_project_quota_bytes?: number;
}
export interface URPolicyView {
  schema_version: "unirouter.payload-policy.v2"; source_instance: string; org_id: string; project_id: string;
  enterprise: URPolicyLayer; project?: URPolicyLayer; user?: URPolicyLayer; user_id?: string;
  effective?: { effective: { policy_version: string; config: URPolicyConfig }; project_quota_bytes: number; execution_storage_mode: string } | null;
  version_vector: string; storage_status: string; deployment_mode: string; full_content_enabled: boolean;
  limits?: { max_saved_bytes: number };
  policy_capabilities?: { supported_protocols: string[]; observation_points: string[] };
}
export interface URPolicyUsers {
  schema_version: "unirouter.xinsight.policy-users.v1"; project_id: string;
  users: Array<{ user_id: string; display_name: string }>; next_cursor: string;
}
export interface URPolicyScopes {
  schema_version: "unirouter.xinsight.policy-scopes.v1"; installation_id: string; source_instance: string; org_id: string;
  grants: Array<{ grant_id: string; revision: string; target_kind: string; coverage: string; project_id: string; user_id?: string; actions: string[] }>;
  projects: Array<{ project_id: string; display_name: string }>;
  next_cursor: string;
}
export interface UROperation {
  installation_id: string; operation_ref: string; source_instance: string; org_id: string; scope_kind: string; scope_id: string;
  operator_subject: string; request_sha256: string; xi_desired_revision: number; ur_expected_ref: string;
  ur_expected_version: number; ur_expected_vector: unknown; state: "requested" | "unconfirmed" | "pending" | "effective" | "failed" | "drift";
  ur_result?: unknown; readback_at?: string;
}
export interface URPolicyControlResponse { state: string; ur: { scopes?: URPolicyScopes; users?: URPolicyUsers; view?: URPolicyView; operation?: UROperation; last_operation?: UROperation; readback_status?: "found" | "not_found" | "unavailable" | "error" } }
export interface URJointXI { settings: ReviewCaptureSettings; expectedRevision: number }
export const reviewPolicyAPI = {
  current(signal?: AbortSignal) {
    return requestJSON<ReviewCaptureDefaultView>("/api/v1/review-capture/default", signal);
  },
  apply(settings: ReviewCaptureSettings, expectedRevision: number, csrfToken: string) {
    return requestAccessJSON<ReviewCaptureDefaultView>("/api/v1/review-capture/default", {
      method: "PUT", body: { settings, expected_revision: expectedRevision }, csrfToken,
    });
  },
  confirm(desiredRevision: number, csrfToken: string) {
    return requestAccessJSON<ReviewCaptureDefaultView>("/api/v1/review-capture/default/confirm", {
      method: "POST", body: { desired_revision: desiredRevision }, csrfToken,
    });
  },
  projects(signal?: AbortSignal) {
    return requestJSON<ReviewCaptureProjectCatalog>("/api/v1/review-capture/projects", signal);
  },
  bindProject(choice: ReviewCaptureProjectChoice, xiProjectID: string, displayName: string, csrfToken: string) {
    return requestAccessJSON<ReviewCaptureProjectCatalog>("/api/v1/review-capture/projects", {
      method: "POST", body: { source_system_id: choice.source_system_id, source_instance: choice.source_instance,
        org_id: choice.org_id, project_id: choice.project_id, xi_project_id: xiProjectID, display_name: displayName }, csrfToken,
    });
  },
  exceptions(signal?: AbortSignal) {
    return requestJSON<ReviewCaptureExceptionCatalog>("/api/v1/review-capture/exceptions", signal);
  },
  applyException(exception: ReviewCaptureException, expectedRevision: number, csrfToken: string) {
    return requestAccessJSON<ReviewCaptureExceptionState>("/api/v1/review-capture/exceptions", {
      method: "PUT", body: { exception, expected_revision: expectedRevision }, csrfToken,
    });
  },
  confirmException(policyRef: string, desiredRevision: number, csrfToken: string) {
    return requestAccessJSON<ReviewCaptureExceptionState>("/api/v1/review-capture/exceptions/confirm", {
      method: "POST", body: { policy_ref: policyRef, desired_revision: desiredRevision }, csrfToken,
    });
  },
  urScopes(cursor = "", signal?: AbortSignal) {
    const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
    return requestJSON<URPolicyControlResponse>(`/api/v1/review-capture/unirouter/scopes${query}`, signal);
  },
  urProject(projectID: string, delegationID: string, signal?: AbortSignal) {
    return requestJSON<URPolicyControlResponse>(`/api/v1/review-capture/unirouter/projects/${encodeURIComponent(projectID)}?delegation_id=${encodeURIComponent(delegationID)}`, signal);
  },
  urEnterprise(orgID: string, delegationID: string, signal?: AbortSignal) {
    return requestJSON<URPolicyControlResponse>(`/api/v1/review-capture/unirouter/organizations/${encodeURIComponent(orgID)}?delegation_id=${encodeURIComponent(delegationID)}`, signal);
  },
  urUsers(projectID: string, cursor = "", signal?: AbortSignal) {
    const query = cursor ? `?cursor=${encodeURIComponent(cursor)}` : "";
    return requestJSON<URPolicyControlResponse>(`/api/v1/review-capture/unirouter/projects/${encodeURIComponent(projectID)}/users${query}`, signal);
  },
  urUser(projectID: string, userID: string, delegationID: string, signal?: AbortSignal) {
    return requestJSON<URPolicyControlResponse>(`/api/v1/review-capture/unirouter/projects/${encodeURIComponent(projectID)}/users/${encodeURIComponent(userID)}?delegation_id=${encodeURIComponent(delegationID)}`, signal);
  },
  urApply(projectID: string, delegationID: string, delegationRevision: string, operationRef: string,
    policy: Record<string, unknown>, csrfToken: string, retryUnconfirmed = false, joint?: URJointXI) {
    return requestAccessJSON<URPolicyControlResponse>(`/api/v1/review-capture/unirouter/projects/${encodeURIComponent(projectID)}`, {
      method: "PUT", body: { delegation_id: delegationID, delegation_revision: delegationRevision,
        operation_ref: operationRef, policy, retry_unconfirmed: retryUnconfirmed,
        ...(joint ? { xi_settings: joint.settings, xi_expected_revision: joint.expectedRevision } : {}) }, csrfToken,
    });
  },
  urApplyEnterprise(orgID: string, delegationID: string, delegationRevision: string, operationRef: string,
    policy: Record<string, unknown>, csrfToken: string, retryUnconfirmed = false, joint?: URJointXI) {
    return requestAccessJSON<URPolicyControlResponse>(`/api/v1/review-capture/unirouter/organizations/${encodeURIComponent(orgID)}`, {
      method: "PUT", body: { delegation_id: delegationID, delegation_revision: delegationRevision,
        operation_ref: operationRef, policy, retry_unconfirmed: retryUnconfirmed,
        ...(joint ? { xi_settings: joint.settings, xi_expected_revision: joint.expectedRevision } : {}) }, csrfToken,
    });
  },
  urApplyUser(projectID: string, userID: string, delegationID: string, delegationRevision: string, operationRef: string,
    policy: Record<string, unknown>, csrfToken: string, retryUnconfirmed = false, joint?: URJointXI) {
    return requestAccessJSON<URPolicyControlResponse>(`/api/v1/review-capture/unirouter/projects/${encodeURIComponent(projectID)}/users/${encodeURIComponent(userID)}`, {
      method: "PUT", body: { delegation_id: delegationID, delegation_revision: delegationRevision,
        operation_ref: operationRef, policy, retry_unconfirmed: retryUnconfirmed,
        ...(joint ? { xi_settings: joint.settings, xi_expected_revision: joint.expectedRevision } : {}) }, csrfToken,
    });
  },
  urOperation(operationRef: string, signal?: AbortSignal) {
    return requestJSON<URPolicyControlResponse>(`/api/v1/review-capture/unirouter/operations/${encodeURIComponent(operationRef)}`, signal);
  },
};

export interface WorkInstallCandidate { project_key: string; label: string }
export interface WorkInstallURTarget { source_system_id: string; source_instance: string; org_id: string; project_id: string }
export interface WorkInstallMapping {
  project_key: string; xi_project_id?: string; xi_project_name?: string; ur_target?: WorkInstallURTarget;
}
export interface WorkInstallBinding {
  project_key: string; project_label: string;
  project_id: string; collection_id: string; xi_project_id: string; xi_project_name: string;
  ur_target?: WorkInstallURTarget;
}
export interface WorkInstallStatus {
  metadata_pending: number; metadata_blocked: number; body_pending: number;
  gaps: Array<{ code: string; occurrences: number }>;
}
export interface WorkInstallInvitation {
  invitation_id: string; invitation_token?: string; display_name: string; source_instance: string;
  state: "invited" | "candidates" | "approved" | "expired";
  projects?: WorkInstallCandidate[]; bindings?: WorkInstallBinding[];
  future_agents_allowed: boolean; future_candidates?: WorkInstallCandidate[];
  excluded_future_project_keys?: string[];
  source_system_id?: string; service_identity_id?: string; expires_at: string;
  latest_status?: WorkInstallStatus; status_received_at?: string;
}
export interface WorkInstallResponse { schema_version: string; action: string; state: string;
  invitation?: WorkInstallInvitation; items?: WorkInstallInvitation[]; next_cursor?: string }
export const workInstallAPI = {
  list(signal?: AbortSignal, after?: string) {
    const query = after ? `?after=${encodeURIComponent(after)}` : "";
    return requestJSON<WorkInstallResponse>(`/api/v1/work-event-install/invitations${query}`, signal);
  },
  issue(displayName: string, expiresInSeconds: number, csrfToken: string) {
    return requestAccessJSON<WorkInstallResponse>("/api/v1/work-event-install/invitations", {
      method: "POST", body: { display_name: displayName, expires_in_seconds: expiresInSeconds }, csrfToken,
    });
  },
  read(invitationID: string, signal?: AbortSignal) {
    return requestJSON<WorkInstallResponse>(`/api/v1/work-event-install/invitations/${encodeURIComponent(invitationID)}`, signal);
  },
  approve(invitationID: string, mappings: WorkInstallMapping[], excludedProjectKeys: string[], futureAgentsAllowed: boolean, csrfToken: string) {
    return requestAccessJSON<WorkInstallResponse>(`/api/v1/work-event-install/invitations/${encodeURIComponent(invitationID)}/approve`, {
      method: "POST", body: { mappings, excluded_project_keys: excludedProjectKeys, future_agents_allowed: futureAgentsAllowed }, csrfToken,
    });
  },
  decideFuture(invitationID: string, mappings: WorkInstallMapping[], excludedProjectKeys: string[], csrfToken: string) {
    return requestAccessJSON<WorkInstallResponse>(`/api/v1/work-event-install/invitations/${encodeURIComponent(invitationID)}/future-decisions`, {
      method: "POST", body: { mappings, excluded_project_keys: excludedProjectKeys }, csrfToken,
    });
  },
};

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

export interface AuthorizedProjection { schema_version: string; representation: "authorized_projection"; withheld: string[]; }

export interface ExecutionSummary {
 view_boundary?: AuthorizedProjection;
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
 view_boundary?: AuthorizedProjection;
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
 contents?: import("./content").ContentLink[];
 resources?: {case_id: string; finding_id?: string; reference: import("./assessmentTypes").ResourceChoice; added_by_subject: string; added_at: string}[];
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

export interface FindingFollowup {
  case_id: string;
  finding_id: string;
  recorded: boolean;
  version: number;
  assignee_subject_id: string | null;
  assignee_name: string;
  due_at: string | null;
  due_timezone: string | null;
  updated_at: string | null;
  updated_by_subject: string;
}

export interface FindingFollowupPayload {
  assignee_subject_id: string | null;
  due_at: string | null;
  due_timezone: string | null;
  reason: string;
}

export interface FindingFollowupCommand {
  operation: "update_followup";
  operation_id: string;
  expected_version: number;
  expected_case_version: number;
  payload: FindingFollowupPayload;
}

export interface FindingFollowupAction {
  operation_id: string;
  operation: "update_followup";
  actor_subject_id: string;
  at: string;
  expected_version: number;
  expected_case_version: number;
  case_version: number;
  reason: string;
  previous: FindingFollowup;
  current: FindingFollowup;
}

export interface FindingHistory {
  items: FindingFollowupAction[];
  through_version: number;
  next_before_version: number | null;
}

export interface FindingReview {
	evaluated_at: string;
  followup: FindingFollowup;
  case_version: number;
  case_status: "open" | "sealed" | "archived";
  history: FindingHistory;
  allowed_actions: string[];
  assignee_state: "not_recorded" | "unassigned" | "active" | "disabled" | "not_found" | "unavailable";
}

export interface LocalAssignee {
  subject_id: string;
  display_name: string;
  state: "active" | "disabled";
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

export type WorkbenchMoneyBasis = "gateway_charged" | "calculated_reference";
export interface WorkbenchReadingRequest {
  contract_version: "xinsight.workbench.v1";
  page: "cost";
  metric_version: "xinsight.workbench.cost.v1";
  period: {
    relative_hours?: number;
    start_inclusive?: string;
    end_exclusive?: string;
    display_timezone: string;
  };
  money_basis: WorkbenchMoneyBasis;
  currency: "CNY";
  filters: { source_ids: string[]; source_projects: string[]; cost_projects: string[] };
}
export interface WorkbenchCoverage {
  status:
    | "period_completeness_unknown"
    | "scope_limited_unknown"
    | "not_connected"
    | "disconnected"
    | "observed_with_gaps"
    | "observed";
  period_completeness: "unknown";
  projection_complete_for_members: boolean;
  capture_started_at?: string;
  last_received_at?: string;
  received_watermark?: string;
  projection_watermark?: string;
  known_items: number;
  gaps: string[];
}
export interface WorkbenchReading {
  reading_id: string;
  contract_version: "xinsight.workbench.v1";
  metric_version: "xinsight.workbench.cost.v1";
  period: { start_inclusive: string; end_exclusive: string; timezone: string };
  cutoff: string;
  expires_at: string;
  money_basis: WorkbenchMoneyBasis;
  money_title: string;
  currency: "CNY";
  membership_ref: string;
  purpose?: { status: "available" | "unavailable"; rule_version: string; manifest_ref: string };
  source_manifest_ref?: string;
  interpretation_revision: string;
  authorization_revision: string;
  dimensions: Array<"model" | "source_project" | "cost_project" | "application" | "account" | "purpose">;
  coverage: WorkbenchCoverage;
  source_coverage?: WorkbenchSourceCoverage;
}
export interface WorkbenchSummary {
  reading_id: string;
  money_basis: WorkbenchMoneyBasis;
  currency: "CNY";
  request_count: number;
  request_total_complete: boolean;
  selected_amount_micros: string;
  gateway_charged_micros: string;
  calculated_reference_micros: string;
  amount_complete: boolean;
  purpose_known_requests: number;
  actor_known_requests: number;
  coverage: WorkbenchCoverage;
  source_coverage?: WorkbenchSourceCoverage;
}
export interface WorkbenchContribution {
  ref: string;
  label: string;
  request_count: number;
  selected_amount_micros: string;
  gateway_charged_micros: string;
  calculated_reference_micros: string;
  share: { numerator: string; denominator: string; unit: "micros" };
  drilldown_allowed: boolean;
}
export interface WorkbenchContributions {
  order_by?: "amount" | "requests";
  denominator_requests?: number;
  unassigned_requests?: number;
  other_known_requests?: number;
  reading_id: string;
  dimension: "model" | "source_project" | "cost_project" | "application" | "account" | "purpose";
  mode: "top_n" | "full";
  rows: WorkbenchContribution[];
  other_known_micros: string;
  unassigned_micros: string;
  unassigned_scope: "reading_constant";
  denominator_micros: string;
  next_cursor?: string;
  is_last_page: boolean;
  authorized_group_count: number;
}
export interface WorkbenchDetail {
 investigation_available?: boolean;
  request_id: string;
  execution_id: string;
  evidence_event_id: string;
  event_hash: string;
  source_system_id: string;
  source_version: string;
  model?: string;
  source_project?: string;
  cost_project?: string;
  terminal_status: "settled" | "cancelled";
  occurred_at: string;
  gateway_charged_micros: string;
  calculated_reference_micros: string;
  account_state: "unknown" | "known_account_owner";
  application_state: "unknown" | "known";
  purpose_state: "unknown" | "unreviewed" | "conflict" | "adopted" | "unavailable";
  source_facts?: WorkbenchSourceObservation[];
}
export interface WorkbenchDetails {
  reading_id: string;
  contribution_ref: string;
  rows: WorkbenchDetail[];
  next_cursor?: string;
}

export interface OverviewRecentRequest {
  investigation_available?: boolean;
  request_id: string; execution_id: string; evidence_event_id: string; source_system_id: string;
  model?: string; source_project?: string; cost_project?: string; occurred_at: string;
  gateway_charged_micros: string; calculated_reference_micros: string;
  purpose_state: string; purpose_category?: string; account_state: string; application_state: string; application_id?: string;
  runtime: { match_state: "unavailable" | "not_in_runtime_reading" | "ambiguous" | "no_request_observation" | "matched"; request_ref?: string; entry_at?: string; outcome?: string };
}
export interface OverviewRecentRequests {
  version: "xinsight.overview.recent-requests.v1"; scope_basis: "economic_reading_members";
  detail_scope: "currently_authorized_details_only"; reading_id: string; membership_ref: string; cutoff: string;
  runtime_reading_id?: string; runtime_membership_ref?: string; runtime_cutoff?: string;
  items: OverviewRecentRequest[]; next_cursor?: string;
}
export interface WorkbenchSourceCoverage {
  known_documents: number;
  pending_predecessor: number;
  economic_not_received: number;
  economic_hash_conflicts: number;
  media_pending: number;
  not_captured: number;
  profiles: string[];
  gaps: string[];
  requires_attention?: boolean;
  collection_states?: ("active" | "paused" | "revoked")[];
}
export interface WorkbenchSourceObservation {
  source_system_id: string;
  request_id: string;
  profile: string;
  stage: string;
  source_fact_id: string;
  revision: string;
  source_hash: string;
  envelope_hash: string;
  chain_state: "complete" | "missing_predecessor";
  economic_state: "received" | "not_received" | "hash_mismatch" | "media_pricing_pending" | "media_not_eligible" | "not_evaluated";
  capture_status: "available" | "not_captured";
  occurred_at: string;
  recorded_at: string;
  context?: Record<string, unknown>;
  identity?: Record<string, unknown>;
  cost_basis?: Record<string, unknown>;
}
export interface WorkbenchSourceObservations {
  reading_id: string;
  manifest_ref: string;
  items: WorkbenchSourceObservation[];
  next_cursor?: string;
}
export interface WorkbenchSourceStatus {
	 source_facts?: WorkbenchSourceFactsStatus[];
  source_system_id: string;
  display_name: string;
  status: "scope_limited_unknown" | "not_connected" | "disconnected" | "observed_with_gaps" | "observed";
  capture_started_at: string | null;
  last_attempt_at: string | null;
  last_received_at: string | null;
  received_watermark?: string;
  projection_watermark?: string;
  scope_label: "whole_source" | "project_restricted";
  gaps: string[];
}
export interface WorkbenchSourceFactsStatus {
  stream_id: string; profile: string; source_instance: string;
  org_id: string; project_id: string; scope_generation: string;
  status: "active" | "paused" | "revoked"; requires_attention: boolean;
  last_error?: string; last_received_at: string | null;
}
export interface WorkbenchSourceStatuses { reading_id: string; items: WorkbenchSourceStatus[] }
export interface WorkbenchSeries {
  reading_id: string;
  metric: "amount" | "requests";
  unit: "micros" | "request";
  bucket_seconds: number;
  points: Array<{ start_inclusive: string; end_exclusive: string; known_value: string; complete: boolean }>;
  coverage: WorkbenchCoverage;
}
export interface WorkbenchSavedFilterCommand {
  name: string;
  config: WorkbenchReadingRequest;
  expected_revision: number;
  idempotency_key: string;
}
export interface WorkbenchSavedFilter {
  id: string;
  owner_subject: string;
  name: string;
  config: WorkbenchReadingRequest;
  revision: number;
  created_at: string;
  updated_at: string;
}
export interface WorkbenchSavedFilterList { items: WorkbenchSavedFilter[] }

let scopedRevision: string | undefined;
let scopeIdentity = "";
let scopeGeneration=0;
// Activity timestamps and CSRF refreshes do not change the authorization identity.
export function scopeSessionKey(current: AccessSessionResponse | null): string {
 return current ? JSON.stringify([current.session.subject_id, current.session.session_ref,
  current.session.role, [...current.session.permissions].sort(), current.scope_revision]) : "";
}
export function acceptScopeSession(current: AccessSessionResponse | null){
 const next=scopeSessionKey(current);
 if(scopeIdentity!==next){scopeGeneration++;scopeIdentity=next;}
 scopedRevision=current?.scope_revision;
}
export function currentScopeSessionKey(){return scopeIdentity;}
function invalidateScope(){scopeGeneration++;window.dispatchEvent(new Event("xinsight:scope-changed"));}

async function requestJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  return requestAccessJSON<T>(path, { signal });
}

async function requestAccessJSON<T>(
  path: string,
  options: {
    method?: "GET" | "POST" | "PUT" | "DELETE";
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
  if (method === "POST" || method === "PUT" || method === "DELETE") {
    headers["Content-Type"] = "application/json";
  }
  if (options.csrfToken) {
    headers["X-CSRF-Token"] = options.csrfToken;
  }
  const scopedRequest=scopedRevision!==undefined&&!path.startsWith("/api/v1/access/");
  const generation=scopeGeneration;
  if(scopedRequest)headers["X-XInsight-Scope-Revision"]=scopedRevision!;
  const response = await fetch(path, {
    method,
    headers,
    cache: "no-store",
    credentials: "same-origin",
    body: method === "POST" || method === "PUT" || method === "DELETE" ? JSON.stringify(options.body ?? {}) : undefined,
    signal: options.signal,
  });
  const decoded = await decodeResponseJSON(response);
  if(scopedRequest){
    if(generation!==scopeGeneration)throw new DOMException("Authorization changed","AbortError");
    const next=response.headers.get("X-XInsight-Scope-Revision");
    // Cost/runtime qualification and optional personal-filter access can be absent
    // within an otherwise valid scope. Only this precise denial with the unchanged server revision is
    // local; missing/changed revisions and other 403 codes still invalidate.
    const qualificationPath=(method==="GET"&&path==="/api/v1/workbench/saved-filters")||(method==="POST"&&path==="/api/v1/workbench/readings")||path.startsWith("/api/v1/workbench/budgets/")||path.startsWith("/api/v1/runtime/");
    const currentQualificationDenial=qualificationPath&&response.status===403&&next===scopedRevision&&isProblem(decoded)&&decoded.code==="workbench_scope_denied";
    if((next&&next!==scopedRevision)||(response.status===403&&!currentQualificationDenial)||(isProblem(decoded)&&decoded.code==="scope_revision_changed")){
      invalidateScope();throw new ApiRequestError(response.status===200?409:response.status,isProblem(decoded)?decoded:undefined);
    }
  }
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

const findingAssessmentPath = (caseID: string, findingID: string) => `/api/v1/cases/${encodeURIComponent(caseID)}/findings/${encodeURIComponent(findingID)}`;
const reviewCreationPath = (command: ReviewCreationCommand) => command.from
  ? `${findingAssessmentPath(command.from.case_id, command.from.finding_id)}/continuations` : "/api/v1/security/reviews";

export const assessmentAPI = {
  get(caseID: string, findingID: string, signal?: AbortSignal) {
    return requestAccessJSON<AssessmentView>(`${findingAssessmentPath(caseID, findingID)}/assessment`, { signal });
  },
  history(caseID: string, findingID: string, through: number, before: number, signal?: AbortSignal) {
    const params = new URLSearchParams({ through_version: String(through), before_version: String(before), limit: "20" });
    return requestAccessJSON<AssessmentHistory>(`${findingAssessmentPath(caseID, findingID)}/assessment/history?${params}`, { signal });
  },
  preview(caseID: string, findingID: string, selection: BasisSelection, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<ReviewBasis>(`${findingAssessmentPath(caseID, findingID)}/assessment/preview`, { method: "POST", body: selection, csrfToken, signal });
  },
  apply(caseID: string, findingID: string, command: AssessmentCommand, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<AssessmentAction>(`${findingAssessmentPath(caseID, findingID)}/assessment-actions`, { method: "POST", body: command, csrfToken, signal });
  },
  previewCreation(command: ReviewCreationCommand, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<ReviewBasis>(`${reviewCreationPath(command)}/preview`, { method: "POST", body: command, csrfToken, signal });
  },
  create(command: ReviewCreationCommand, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<ReviewCreationReceipt>(reviewCreationPath(command), { method: "POST", body: command, csrfToken, signal });
  },
};

export const workspaceAPI = {
  getFindingReview(caseID: string, findingID: string, signal?: AbortSignal) {
    return requestAccessJSON<FindingReview>(`/api/v1/cases/${encodeURIComponent(caseID)}/findings/${encodeURIComponent(findingID)}/review`, { signal });
  },
  getFindingHistory(caseID: string, findingID: string, through: number, before: number, signal?: AbortSignal) {
    const params = new URLSearchParams({ through_version: String(through), before_version: String(before), limit: "20" });
    return requestAccessJSON<FindingHistory>(`/api/v1/cases/${encodeURIComponent(caseID)}/findings/${encodeURIComponent(findingID)}/review/history?${params}`, { signal });
  },
  updateFindingFollowup(caseID: string, findingID: string, command: FindingFollowupCommand, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<FindingFollowupAction>(`/api/v1/cases/${encodeURIComponent(caseID)}/findings/${encodeURIComponent(findingID)}/review-actions`, { method: "POST", body: command, csrfToken, signal });
  },
  assigneeCandidates(caseID: string, findingID: string, signal?: AbortSignal) {
    const params = new URLSearchParams({ case_id: caseID, finding_id: findingID });
    return requestAccessJSON<{ items: LocalAssignee[]; total: number; complete: boolean; basis: string }>(`/api/v1/security/assignee-candidates?${params}`, { signal });
  },
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

export const workbenchAPI = {
  recentRequests(id: string, runtimeReadingID?: string, cursor?: string, signal?: AbortSignal) {
    const query = new URLSearchParams({ limit: "10" });
    if (runtimeReadingID) query.set("runtime_reading_id", runtimeReadingID);
    if (cursor) query.set("cursor", cursor);
    return requestJSON<OverviewRecentRequests>(`/api/v1/workbench/readings/${encodeURIComponent(id)}/recent-requests?${query}`, signal);
  },
  purpose(executionID: string, readingID?: string, before?: number, signal?: AbortSignal) {
    const query = new URLSearchParams();
    if (readingID) query.set("reading_id", readingID);
    if (before) query.set("before_revision", String(before));
    return requestAccessJSON<PurposeView>(`/api/v1/workbench/executions/${encodeURIComponent(executionID)}/purpose?${query}`, { signal });
  },
  purposeCommand(executionID: string, command: PurposeCommand, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<PurposeEvent>(`/api/v1/workbench/executions/${encodeURIComponent(executionID)}/purpose`, { method: "POST", body: command, csrfToken, signal });
  },
  reading(id: string, signal?: AbortSignal) {
    return requestJSON<WorkbenchReading>(`/api/v1/workbench/readings/${encodeURIComponent(id)}`, signal);
  },
  createReading(request: WorkbenchReadingRequest, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<WorkbenchReading>("/api/v1/workbench/readings", {
      method: "POST", body: request, csrfToken, signal,
    });
  },
  summary(id: string, signal?: AbortSignal) {
    return requestJSON<WorkbenchSummary>(
      `/api/v1/workbench/readings/${encodeURIComponent(id)}/summary`, signal,
    );
  },
  contributions(
    id: string,
    dimension: "model" | "source_project" | "cost_project" | "application" | "account" | "purpose",
    options: { topN?: number; limit?: number; cursor?: string; orderBy?: "amount" | "requests" } = {},
    signal?: AbortSignal,
  ) {
    const query = new URLSearchParams({ dimension });
    if (options.orderBy) query.set("order_by", options.orderBy);
    if (options.limit) query.set("limit", String(options.limit));
    else query.set("top_n", String(options.topN ?? 10));
    if (options.cursor) query.set("cursor", options.cursor);
    return requestJSON<WorkbenchContributions>(
      `/api/v1/workbench/readings/${encodeURIComponent(id)}/contributions?${query}`, signal,
    );
  },
  details(id: string, ref: string, cursor?: string, signal?: AbortSignal) {
    const query = new URLSearchParams({ contribution_ref: ref, limit: "50" });
    if (cursor) query.set("cursor", cursor);
    return requestJSON<WorkbenchDetails>(
      `/api/v1/workbench/readings/${encodeURIComponent(id)}/details?${query}`, signal,
    );
  },
  sourceFacts(id: string, cursor?: string, signal?: AbortSignal) {
    const query = new URLSearchParams({ limit: "50" });
    if (cursor) query.set("cursor", cursor);
    return requestJSON<WorkbenchSourceObservations>(
      `/api/v1/workbench/readings/${encodeURIComponent(id)}/source-facts?${query}`, signal,
    );
  },
  sources(id: string, signal?: AbortSignal) {
    return requestJSON<WorkbenchSourceStatuses>(
      `/api/v1/workbench/readings/${encodeURIComponent(id)}/sources`, signal,
    );
  },
  series(id: string, signal?: AbortSignal, bucketSeconds = 3600, metric: "amount" | "requests" = "amount") {
    return requestJSON<WorkbenchSeries>(
      `/api/v1/workbench/readings/${encodeURIComponent(id)}/series?metric=${metric}&bucket_seconds=${bucketSeconds}`, signal,
    );
  },
  savedFilters(signal?: AbortSignal) {
    return requestJSON<WorkbenchSavedFilterList>("/api/v1/workbench/saved-filters", signal);
  },
  createSavedFilter(command: WorkbenchSavedFilterCommand, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<WorkbenchSavedFilter>("/api/v1/workbench/saved-filters", {
      method: "POST", body: command, csrfToken, signal,
    });
  },
  updateSavedFilter(id: string, command: WorkbenchSavedFilterCommand, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<WorkbenchSavedFilter>(
      `/api/v1/workbench/saved-filters/${encodeURIComponent(id)}`,
      { method: "PUT", body: command, csrfToken, signal },
    );
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

export interface InvestigationQueueFilters {
 mode: string; period: string; sort: string; from?: string; to?: string;
 assignee?: string; progress?: string; conclusion?: string; basis?: string; case_status?: string; search?: string;
}
export interface InvestigationQueueItem {
  resource_updates?: import("./assessmentTypes").ResourceUpdate[];
 finding: CaseFinding; case_title: string; case_status: string; assessment_version: number;
 followup: FindingFollowup; progress: string; conclusion: string; basis: string; review_basis: string; progress_basis: string;
 basis_revision: string; overdue: boolean; assignee_state: string;
}
export interface InvestigationQueuePage {
 items: InvestigationQueueItem[]; total: number; evaluated_at: string; version: string; filters: InvestigationQueueFilters; next_cursor?: string; coverage: string;
 statistics: { current: number; created: number; reassessment: number; unassigned: number; overdue: number; basis_unrecorded: number; basis_changed: number; basis_unavailable: number };
}
export interface InvestigationQueueFilter {
 id: string; owner_subject: string; schema_version: string; name: string; filters: InvestigationQueueFilters;
 revision: number; deleted: boolean; created_at: string; updated_at: string;
}
export interface QueueFilterCommand {
 operation_id: string; expected_revision: number; schema_version: string; name: string; filters: InvestigationQueueFilters;
}
export function queueFilterQuery(filters: InvestigationQueueFilters) {
 const params = new URLSearchParams();
 for (const [key, value] of Object.entries(filters)) if (value) params.set(key, value);
 return params;
}
export const investigationQueueAPI = {
 list(filters: InvestigationQueueFilters, cursor?: string, signal?: AbortSignal) {
  const params = queueFilterQuery(filters); params.set("limit", "50"); if (cursor) params.set("cursor", cursor);
  return requestAccessJSON<InvestigationQueuePage>(`/api/v1/investigation/queue?${params}`, { signal });
 },
 filters(signal?: AbortSignal) { return requestAccessJSON<{items: InvestigationQueueFilter[]}>("/api/v1/investigation/queue-filters", { signal }); },
 save(kind: "create" | "update" | "delete", id: string, command: QueueFilterCommand, csrfToken: string, signal?: AbortSignal) {
  return requestAccessJSON<InvestigationQueueFilter>("/api/v1/investigation/queue-filters" + (kind === "create" ? "" : `/${encodeURIComponent(id)}`), { method: kind === "create" ? "POST" : kind === "update" ? "PUT" : "DELETE", body: command, csrfToken, signal });
 },
};

export type PurposeCategory = "information_query" | "content_transformation" | "software_check";
export type PurposeAction = "declare" | "accept" | "modify" | "reject" | "withdraw_declaration" | "withdraw_adoption";
export interface PurposeBasis { evidence_event_id: string; event_hash: string }
export interface PurposeCommand {
  action: PurposeAction; expected_revision: number; idempotency_key: string; reason: string;
  claim_id?: string; category?: PurposeCategory; vocabulary?: string; basis_refs?: PurposeBasis[];
  reject_claim_ids?: string[]; tags?: PurposeCategory[];
}
export interface PurposeClaim {
  claim_id: string; category: PurposeCategory; vocabulary: string; method: "human_declared";
  basis_refs: PurposeBasis[]; reason: string; declared_by: string; declared_at: string;
  review_state: "pending" | "accepted" | "rejected" | "withdrawn" | "modified"; revises?: string; tags?: PurposeCategory[];
}
export interface PurposeState { revision: number; claims: PurposeClaim[]; adopted_claim_id?: string; status: "unknown" | "unreviewed" | "conflict" | "adopted"; category?: PurposeCategory }
export interface PurposeEvent { execution_id: string; revision: number; actor: string; recorded_at: string; command: PurposeCommand; new_claim_id?: string; previous_hash: string; event_hash: string; state: PurposeState }
export interface PurposeView { execution_id: string; reading_id?: string; state: PurposeState; history: PurposeHistoryEntry[]; next_before_revision?: number; can_review: boolean; vocabulary: string }

export type PurposeHistoryEntry = Omit<PurposeEvent, "execution_id" | "state">;


function runtimeQuery(entry: Runtime.EntryTime, cursor?: string) {
  const query = new URLSearchParams({ entry_time: entry, limit: "50" });
  if (cursor) query.set("cursor", cursor);
  return query;
}
export const runtimeAPI = {
  executionRequest(id: string, executionID: string, signal?: AbortSignal) {
    return requestJSON<Runtime.ExecutionRequest>(`/api/v1/runtime/readings/${encodeURIComponent(id)}/executions/${encodeURIComponent(executionID)}`, signal);
  },
  resources(id: string, requestRef: string, cursor?: string, signal?: AbortSignal) {
    const query = new URLSearchParams({ limit: "50" });
    if (cursor) query.set("cursor", cursor);
    return requestJSON<Runtime.Resources>(`/api/v1/runtime/readings/${encodeURIComponent(id)}/requests/${encodeURIComponent(requestRef)}/resources?${query}`, signal);
  },
  actionWitnesses(id: string, finalRequestRef: string, cursor?: string, signal?: AbortSignal) {
    const query = new URLSearchParams({ limit: "50" });
    if (cursor) query.set("cursor", cursor);
    return requestJSON<Runtime.ActionWitnesses>(`/api/v1/runtime/readings/${encodeURIComponent(id)}/requests/${encodeURIComponent(finalRequestRef)}/action-witnesses?${query}`, signal);
  },
  associations(id: string, requestRef: string, cursor?: string, signal?: AbortSignal) {
    const query = new URLSearchParams({ limit: "50" });
    if (cursor) query.set("cursor", cursor);
    return requestJSON<Runtime.Associations>(`/api/v1/runtime/readings/${encodeURIComponent(id)}/requests/${encodeURIComponent(requestRef)}/associations?${query}`, signal);
  },
  createReading(request: Runtime.ReadingRequest, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<Runtime.Reading>("/api/v1/runtime/readings", { method: "POST", body: request, csrfToken, signal });
  },
  reading(id: string, signal?: AbortSignal) {
    return requestJSON<Runtime.Reading>(`/api/v1/runtime/readings/${encodeURIComponent(id)}`, signal);
  },
  summary(id: string, entry: Runtime.EntryTime, signal?: AbortSignal) {
    return requestJSON<Runtime.Summary>(`/api/v1/runtime/readings/${encodeURIComponent(id)}/summary?entry_time=${entry}`, signal);
  },
  requests(id: string, entry: Runtime.EntryTime, cursor?: string, signal?: AbortSignal) {
    return requestJSON<Runtime.Page<Runtime.Request>>(`/api/v1/runtime/readings/${encodeURIComponent(id)}/requests?${runtimeQuery(entry, cursor)}`, signal);
  },
  dimensions(id: string, dimension: Runtime.Dimension, entry: Runtime.EntryTime, cursor?: string, signal?: AbortSignal) {
    return requestJSON<Runtime.Dimensions>(`/api/v1/runtime/readings/${encodeURIComponent(id)}/dimensions/${dimension}?${runtimeQuery(entry, cursor)}`, signal);
  },
  measurements(id: string, entry: Runtime.EntryTime, cursor?: string, signal?: AbortSignal) {
    return requestJSON<Runtime.Measurements>(`/api/v1/runtime/readings/${encodeURIComponent(id)}/measurements?${runtimeQuery(entry, cursor)}`, signal);
  },
  observations(id: string, collection: Runtime.Collection, requestRef?: string, cursor?: string, signal?: AbortSignal) {
    const query = new URLSearchParams({ limit: "50" });
    if (cursor) query.set("cursor", cursor);
    const part = requestRef ? `requests/${encodeURIComponent(requestRef)}` : collection;
    return requestJSON<Runtime.Page<Runtime.Observation>>(`/api/v1/runtime/readings/${encodeURIComponent(id)}/${part}?${query}`, signal);
  },
  current(filters: Runtime.ReadingRequest["filters"], cursor?: string, signal?: AbortSignal) {
    const query = new URLSearchParams({ limit: "50" });
    for (const key of ["source_ids", "source_projects", "cost_projects"] as const) {
      if (filters[key].length) query.set(key, filters[key].join(","));
    }
    if (cursor) query.set("cursor", cursor);
    return requestJSON<Runtime.CurrentInventory>(`/api/v1/runtime/current?${query}`, signal);
  },
};

export const budgetAPI = {
  history(id: string, ref: string, cursor?: string, signal?: AbortSignal) {
    const query = new URLSearchParams({ limit: "25" });
    if (cursor) query.set("cursor", cursor);
    return requestJSON<BudgetItems>(`/api/v1/workbench/budgets/readings/${encodeURIComponent(id)}/items/${encodeURIComponent(ref)}/history?${query}`, signal);
  },
  createReading(request: BudgetRequest, csrfToken: string, signal?: AbortSignal) {
    return requestAccessJSON<BudgetReading>("/api/v1/workbench/budgets/readings", { method: "POST", body: request, csrfToken, signal });
  },
  reading(id: string, signal?: AbortSignal) {
    return requestJSON<BudgetReading>(`/api/v1/workbench/budgets/readings/${encodeURIComponent(id)}`, signal);
  },
  items(id: string, cursor?: string, signal?: AbortSignal) {
    const query = new URLSearchParams({ limit: "25" });
    if (cursor) query.set("cursor", cursor);
    return requestJSON<BudgetItems>(`/api/v1/workbench/budgets/readings/${encodeURIComponent(id)}/items?${query}`, signal);
  },
};

export interface WorkActivityEvent {
  schema_version: "xinsight.work-event.v1";
  reporter: { source_system_id: string; source_instance: string; org_id: string; project_id: string };
  event_ref: string;
  revision: number;
  previous_hash?: string;
  occurred_at: string;
  recorded_at: string;
  subject_kind: "task" | "message" | "model_call" | "tool_call" | "resource_action";
  lifecycle_state: "started" | "completed" | "failed" | "unknown";
  task_ref?: string;
  run_id?: string;
  model_call_id?: string;
  tool_call_id?: string;
  actor: { kind: "human" | "agent" | "service" | "unknown"; ref?: string };
  action_kind: string;
  resource?: { ref: string; name_state: "readable" | "hidden"; name?: string };
  content_state: "not_captured" | "pending" | "referenced";
  content_refs: Array<{ kind: string; ref: string; sha256: string }>;
}

export interface WorkActivityPage {
  version: "xinsight.work-activity.v1";
  cutoff: string;
  items: Array<{ event: WorkActivityEvent; event_hash: string; received_at: string }>;
  next_cursor?: string;
}

export interface WorkActivityDetail {
  version: "xinsight.work-activity-detail.v2";
  item: WorkActivityPage["items"][number];
  relations: Array<{
    relation: { relation_ref: string; ur_request_id: string; model_call_id: string; event_revision: number; event_hash: string;
      ur_target: { source_system_id: string; source_instance: string; org_id: string; project_id: string } };
    state: "source_asserted_unverified" | "ur_identifier_matched";
    binding_state: "current_revision" | "prior_revision";
    runtime_request_ref?: string;
    observed_at?: string;
  }>;
  run_items?: WorkActivityPage["items"];
  run_items_more?: boolean;
  run_cutoff?: string;
  run_items_next_cursor?: string;
  run_summary?: {
    resource_read_outcomes: { completed: number; failed: number; optional_not_found: number; unknown: number };
    resource_change_outcomes: { completed: number; failed: number; optional_not_found: number; unknown: number };
    distinct_resources: number;
    model_calls: number;
    tool_calls: number;
    unlinked_model_observations: number;
    task_completed: number;
    task_failed: number;
    task_unknown: number;
  };
  review_bodies: Array<{ kind: string; ref: string; sha256: string; state: "source_reference_only" | "content_not_authorized" | "pending" | "available" | "expired" | "gap" | "copy_missing"; can_read: boolean }>;
}

export interface WorkActivityScopes {
  version: "xinsight.work-activity-scopes.v1";
  items: Array<{ source_id: string; source_label: string; project_label?: string; source_instance: string; org_id: string; project_id: string }>;
}

export interface WorkActivitySummary {
  version: "xinsight.work-activity-summary.v2";
  from: string;
  to: string;
  cutoff: string;
  reported_actions: number;
  reported_resource_reads: number;
  reported_resource_changes: number;
  resource_read_outcomes?: { completed: number; failed: number; optional_not_found: number; unknown: number };
  resource_change_outcomes?: { completed: number; failed: number; optional_not_found: number; unknown: number };
  reported_model_events: number;
  reported_linked_events: number;
  reported_unlinked_events: number;
  sources_needing_attention: number;
  coverage: string[];
}

export const workActivityAPI = {
  summary(query: { from: string; to: string; source_ids: string[]; project_ids: string[] }, signal?: AbortSignal) {
    const params = new URLSearchParams({ from: query.from, to: query.to, view: "reading_v2" });
    if (query.source_ids.length) params.set("source_ids", query.source_ids.join(","));
    if (query.project_ids.length) params.set("project_ids", query.project_ids.join(","));
    return requestJSON<WorkActivitySummary>(`/api/v1/work-activity/summary?${params}`, signal);
  },
  scopes(signal?: AbortSignal) {
    return requestJSON<WorkActivityScopes>("/api/v1/work-activity/scopes", signal);
  },
  list(query: { from: string; to: string; source_id?: string; project_id?: string; source_ids?: string[]; project_ids?: string[]; resource_ref?: string; resource_name?: string; cursor?: string }, signal?: AbortSignal) {
    const params = new URLSearchParams({ from: query.from, to: query.to, limit: "50" });
    for (const key of ["source_id", "project_id", "resource_ref", "resource_name", "cursor"] as const) {
      if (query[key]) params.set(key, query[key]);
    }
    if (query.source_ids?.length) params.set("source_ids", query.source_ids.join(","));
    if (query.project_ids?.length) params.set("project_ids", query.project_ids.join(","));
    return requestJSON<WorkActivityPage>(`/api/v1/work-activity?${params}`, signal);
  },
  detail(identity: { source: string; instance: string; org: string; project: string; event: string }, signal?: AbortSignal, runCursor?: string) {
    const parts = [identity.source, identity.instance, identity.org, identity.project, identity.event].map(encodeURIComponent);
    const query = new URLSearchParams({ view: "reading_v2" });
    if (runCursor) query.set("run_cursor", runCursor);
    return requestJSON<WorkActivityDetail>(`/api/v1/work-activity/events/${parts.join("/")}?${query}`, signal);
  },
};

export const connectorSetupAPI={
 preview(signal?:AbortSignal){return requestJSON<SetupPreview>("/api/v1/connector-setups/preview",signal)},
 create(body:{schema_version:"xinsight.connector-setup-consent.v1";request_id:string;expected_custody_revision:number;custody:SetupPreview["custody"]["settings"]},csrfToken:string){return requestAccessJSON<SetupCreated>("/api/v1/connector-setups",{method:"POST",body,csrfToken})},
 status(id:string,signal?:AbortSignal){return requestJSON<SetupStatus>(`/api/v1/connector-setups/${encodeURIComponent(id)}`,signal)},
 recover(id:string,requestID:string,csrfToken:string){return requestAccessJSON<{schema_version:"xinsight.connector-control-recovery.v1";installation_id:string;recovery_id:string;expires_at:string;recover_url:string}>(`/api/v1/connector-setups/${encodeURIComponent(id)}/credential-recovery`,{method:"POST",body:{request_id:requestID},csrfToken})}
};
