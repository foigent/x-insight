import type { WorkbenchReading, WorkbenchReadingRequest, WorkbenchSourceObservation } from "./api";

export type EntryTime = "known" | "unknown" | "all";
export type Dimension = "operation" | "selected_model" | "selected_provider" | "required_capability" | "enabled_capability" | "destination_provider" | "destination_origin";
export type Collection = "observations" | "runtime" | "attempts" | "capabilities" | "policies" | "destinations";
export interface ReadingRequest {
  contract_version: "xinsight.runtime-reading.v1";
  metric_version: "xinsight.runtime.request-observations.v1";
  period: WorkbenchReadingRequest["period"];
  filters: WorkbenchReadingRequest["filters"];
}
export interface Coverage {
  period_completeness: string;
  timeline_complete: boolean;
  time_basis: string;
  unknown_entry_time_basis: string;
  missing_predecessors: number;
  gaps: string[];
  streams: { source_system_id: string; source_instance: string; org_id: string; project_id: string; profile: string; state: string; scope_generation: string; received_watermark?: string; requires_attention: boolean }[];
}
export interface Reading extends Pick<WorkbenchReading, "reading_id" | "period" | "cutoff" | "expires_at" | "authorization_revision" | "interpretation_revision" | "membership_ref"> {
  contract_version: ReadingRequest["contract_version"];
  metric_version: ReadingRequest["metric_version"];
  requests_with_entry_time: number;
  requests_with_unknown_entry_time: number;
  observation_count: number;
  coverage: Coverage;
}
export interface Page<T> { reading_id: string; items: T[]; next_cursor?: string }
export interface Request {
  request_ref: string; source_system_id: string; source_instance: string; org_id: string; project_id: string; request_id: string;
  member_basis?: string; content_eligible_at?: string;
  entry_at?: string; entry_time_state: string; last_observed_at: string; observation_count: number;
  outcome?: string; outcome_basis?: "request_level_observation" | "no_request_observation";
}
export interface Summary {
  reading_id: string; summary_version: string; interpretation_revision: string; entry_time: EntryTime; requests: number;
  outcomes: { outcome: string; requests: number }[];
  diagnostic_failure: { numerator: number; denominator: number; excluded_requests: number; state: string; basis: string };
  coverage: Coverage;
}
export interface DimensionGroup { value: string; evidence_state: string; requests: number; subjects: number; observation_count: number }
export interface Dimensions extends Page<DimensionGroup> {
  dimensions_version: string; interpretation_revision: string; dimension: Dimension; entry_time: EntryTime;
  requests: number; requests_with_values: number; requests_without_values: number; missing_predecessors: number; groups_may_overlap: true;
}
export interface MeasurementGroup {
  operation: string; stream_mode: string; measurement_kind: string; measurement_version: string; completion_evidence: string; entry_time_state: string;
  observation_count: number;
  excluded: { not_applicable: number; not_observed: number; legacy_unknown: number; invalid: number; missing_predecessor: number; missing_context: number; conflicting_context: number; incompatible_context: number };
  percentiles: { version: string; state: string; unit: string; sample_count: number; p50?: string; p95?: string };
}
export interface Measurements extends Page<MeasurementGroup> { eligibility_version: string; interpretation_revision: string }
interface ProfileReference { id: string; version: string; sha256?: string }
export interface ObservationDocument {
  schema_version: string; source_instance: string; org_id: string; project_id: string; request_id: string;
  profile: string; subject_kind: string; subject_id: string; observation_kind: string; source_fact_id: string; revision: string; event_id: string;
  previous_revision?: string; previous_hash?: string; correction_reason?: string;
  occurred_at: string; recorded_at: string; coverage: string; capture_expected: string[];
  runtime?: { operation: string; stream_mode: string; admission: string; provider: string; local_delivery: string; financial: string; capture: string; evidence_ref?: string; context_done_reason?: string };
  attempt?: { attempt_id: string; operation: string; route_sequence?: string; route_total?: string; provider: string; local_delivery: string; financial: string; retry_authorized: boolean; http_status?: string; evidence_ref: string };
  capability?: {
    model_ref: string; provider: string; operation: string; surface: string; protocol: ProfileReference; execution_profile: ProfileReference;
    release_ref?: string; slot_version?: string; binding_ref?: string; probe_ref?: string; approval_ref?: string; evidence_state: string;
    required_capabilities: string[];
    capabilities: { capability_id: string; support_state: string; evidence_state: string; evidence_ref_sha256: string; verified_at: string; revision: string }[];
    capability_evidence_hash?: string;
  };
  policy?: { detector_outcomes: string[]; stage_decisions: string[]; policy_ref: string; policy_version: string; detector_ref: string; detector_version: string; stage: string; mode: string; configured_action: string; outcome: string; failure_mode: string; degraded: boolean; mandatory: boolean; effects: string[]; evidence_state: string };
  destination?: { provider: string; origin: string; region?: string; region_source: string; dispatch: string; policy_evidence: string; input_binding_state: string; input_binding_ref?: string };
  measurement?: { kind: string; version: string; value?: string; unit: string; state: string; start_boundary: string; end_boundary: string; resolution: string; source: string; evidence_ref?: string };
}
export interface Observation {
  representation: "authorized_projection"; request_ref: string; source_system_id: string; source_hash: string; envelope_hash: string; chain_state: string; document: ObservationDocument;
}
export interface CurrentItem {
  ref: string; source_system_id: string; source_instance: string; org_id: string; project_id: string;
  snapshot_observed_at: string; received_at: string; source_sha256: string; current_state: string;
  entry: { request_id: string; family: string; subject_kind: string; subject_id: string; operation: string; freshness: string; original_state: string; last_observed_at: string; valid_until?: string; lease_generation?: string; evidence_ref: string; checkpoint_ref?: string };
}
export interface CurrentInventory { version: string; evaluated_at: string; coverage: string; uncovered: string[]; items: CurrentItem[]; next_cursor?: string }

export type Association = { kind: "source"; source: WorkbenchSourceObservation } | { kind: "economic"; economic: { economic_fact_id: string; execution_id: string; source_event_hash: string } };
export interface Associations extends Page<Association> {
 request_ref: string; associations_version: "xinsight.runtime.associations.v1"; membership_ref: string; basis: "same_source_request_identity";
}

export interface ResourceScope { source_system_id: string; source_instance: string; org_id: string; project_id: string }
export interface ResourceRecord {
  schema_version: "xinsight.resource-access.internal.v1"; synthetic: true;
  record_ref: string; revision: number; previous_hash?: string; reporter: ResourceScope; target: ResourceScope;
  request_id: string; application_ref: string; actor_ref: string; occurred_at: string;
  report_kind: "source_audit" | "client_assertion";
  resource: { ref: string; version: string; name: string; location: "enterprise_service" | "pc";
    ownership: "known" | "unknown"; owner_ref?: string; classification: "known" | "unknown"; classification_ref?: string;
    device_management: "managed" | "unmanaged" | "unknown" };
  action: "read" | "search" | "download"; access_outcome: "read" | "denied" | "failed" | "unknown";
  access_decision: "allow" | "deny" | "unknown"; decision_ref?: string; policy_version?: string;
  input?: { fragment_ref: string; fragment_version: string; attempt_id: string; attempt_kind: "provider_attempt" | "media_attempt";
    reported_input_ref: string; transforms: { kind: "extract" | "summarize" | "redact" | "translate" | "truncate"; input_version: string; output_version: string; receipt_ref: string }[] };
  withdrawn: boolean;
}
export interface ResourceItem {
  record: ResourceRecord; record_hash: string; chain_state: "complete" | "missing_predecessor";
  input_state: "not_confirmed" | "reference_matched" | "reference_mismatch" | "conflicting_references" | "withdrawn" | "reassessment_required" | "missing_predecessor";
  content_verification: "not_performed"; input_evidence_basis: "frozen_reference_witnesses";
  changed_since_reading: boolean; captured_input_evidence_refs: string[];
}
export interface Resources extends Page<ResourceItem> {
  resources_version: "xinsight.runtime.resources.v1"; request_ref: string; membership_ref: string;
  coverage: "synthetic_internal_records_only";
}

export interface ActionContentLink {
  purpose: "call" | "result" | "before" | "after"; extent: "full_file" | "fragment" | "unknown";
  source_system_id: string; source_instance: string; org_id: string; project_id: string; request_id: string;
  source_fact_id: string; revision: string; content_ref: string; source_sha256: string; json_pointer: string;
}
export interface ActionWitnessRecord {
  schema_version: "xinsight.resource-action.v1"; environment: "test" | "development";
  record_ref: string; revision: number; reporter: ResourceScope; target: ResourceScope; request_id: string;
  final_request_id?: string; related_request_ids?: string[]; occurred_at: string; run_id: string; tool_call_id: string;
  action_kind: "read" | "edit"; object: { ref: string; version: string; name: string };
  tool_result: { state: "completed" | "failed" | "unknown"; result_sha256?: string };
  native_evidence: { trace_id: string; session_id: string; manifest_sha256: string; events_sha256: string; call_seq: number; call_sha256: string; result_seq?: number; result_sha256?: string };
  relationship?: { receipt_ref: string; receipt_sha256: string };
  acceptance: { state: "confirmed" | "failed" | "not_checked"; goal_state: "passed" | "failed" | "not_checked"; hash_scope: "full_file" | "fragment" | "unknown"; before_sha256?: string; after_sha256?: string; checked_at?: string; checker_ref?: string; before_receipt_sha256?: string; after_receipt_sha256?: string };
  content_links: ActionContentLink[]; withdrawn: boolean;
}
export interface ActionWitnesses extends Page<{record: ActionWitnessRecord; record_hash: string; chain_state: "complete" | "missing_predecessor"; changed_since_reading: boolean; trigger_request_ref: string; final_request_ref: string; resolved_content_links: {purpose: ActionContentLink["purpose"]; request_ref: string}[]}> {
  action_witnesses_version: "xinsight.runtime.action-witnesses.v1"; request_ref: string; membership_ref: string;
  coverage: "test_development_action_witnesses_only";
}

export interface ExecutionRequest { version: "xinsight.runtime.execution-request.v1"; reading_id: string; membership_ref: string; execution_id: string; request: Request }
