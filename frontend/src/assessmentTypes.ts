import { localizedLabels, t } from "./i18n";
import type { ContentChoice } from "./content";
import type { CaseEvidenceLink, CaseFinding, CaseGapLink, FindingHistory, InvestigationCase } from "./api";

export type EvidenceChoice = Pick<CaseEvidenceLink, "evidence_event_id" | "relation" | "event_hash" | "source_ref"> & { scope: "finding" | "case" };
export type GapChoice = Pick<CaseGapLink, "gap_id" | "relation" | "affected_claim" | "gap_type"> & { scope: "finding" | "case" };
export interface ResourceChoice { scope: "finding" | "case"; reporter: import("./runtime").ResourceScope; target: import("./runtime").ResourceScope; request_id: string; record_ref: string; revision: number; record_hash: string; relation: "supports" | "contradicts" | "context" }
export interface BasisSelection { evidence: EvidenceChoice[]; gaps: GapChoice[]; resources?: ResourceChoice[]; contents?: ContentChoice[] }
export interface AdoptedGap {
  reference: GapChoice; resolution_recorded: boolean; resolution_version: number;
  state: string; note: string; updated_by_subject: string; updated_at: string | null;
}
export interface ExecutionContext {
  workbench_reading_id: string; execution_id: string; execution_as_of: string;
  runtime_reading_id: string; runtime_request_ref: string;
}
export interface ResolvedExecutionContext {
  context: ExecutionContext; runtime_cutoff: string; workbench_membership_ref: string; runtime_membership_ref: string;
  target: import("./runtime").ResourceScope; request_id: string; resources: ResourceChoice[]; contents?: ContentChoice[];
}
export interface ReviewBasis {
  resolved_execution_context?: ResolvedExecutionContext;
  schema: "xinsight.review-basis.v1" | "xinsight.review-basis.v2" | "xinsight.review-basis.v3"; content_basis_version?: "xinsight.review-content-basis.v1"; adopted_resources?: { reference: ResourceChoice; record: import("./runtime").ResourceRecord }[]; statement: string; finding_version: number;
  known: BasisSelection; selected: BasisSelection; adopted_gaps: AdoptedGap[]; digest: string;
}
export type Conclusion = "pending_review" | "supported" | "not_supported" | "insufficient_evidence";
export type ProgressState = "not_started" | "in_progress" | "ended";
export interface ReviewPayload { conclusion: Conclusion; reason: string; missing_evidence: string }
export interface FindingReference { case_id: string; finding_id: string }
export interface SealedFindingReference extends FindingReference { revision: number; digest: string }
export interface Closure {
  kind: "verified_remediation" | "ruled_out" | "duplicate" | "end_followup";
  explanation: string; scope: string; verified_at: string | null;
  verification_evidence: EvidenceChoice[]; verification_resources?: ResourceChoice[]; verification_contents?: ContentChoice[]; duplicate: FindingReference | null; remaining_unknowns: string;
}
export interface ProgressPayload { state: ProgressState; reason: string; closure: Closure | null }
export interface ReviewRecord extends ReviewPayload { basis: ReviewBasis; actor_subject_id: string; at: string }
export interface ProgressRecord extends ProgressPayload { basis: ReviewBasis; actor_subject_id: string; at: string }
export interface Assessment {
  case_id: string; finding_id: string; recorded: boolean; version: number;
  review: ReviewRecord | null; progress: ProgressRecord | null; updated_at: string | null; updated_by_subject: string;
}
export interface AssessmentCommand {
  operation: "record_review" | "record_progress"; operation_id: string;
  expected_version: number; expected_case_version: number; expected_basis_digest: string;
  selection: BasisSelection; review: ReviewPayload | null; progress: ProgressPayload | null;
}
export interface AssessmentAction {
  operation_id: string; operation: AssessmentCommand["operation"]; actor_subject_id: string; at: string;
  case_version: number; initial: boolean; command: AssessmentCommand; basis: ReviewBasis; previous: Assessment; current: Assessment;
}
export interface AssessmentHistory { items: AssessmentAction[]; through_version: number; next_before_version: number | null }
export interface ResourceUpdate { reference: ResourceChoice; state: "revised" | "withdrawn" | "unavailable"; latest_revision?: number; latest_hash?: string }
export interface BasisEvaluation { resource_updates?: ResourceUpdate[]; state: "unrecorded" | "unchanged" | "changed" | "unavailable"; current: ReviewBasis | null }
export interface AssessmentView {
  evaluated_at: string; assessment: Assessment; case_version: number; case_status: InvestigationCase["status"];
  review_basis: BasisEvaluation; progress_basis: BasisEvaluation; overdue: boolean;
  history: AssessmentHistory; continuations: ReviewCreationReceipt[]; allowed_actions: string[];
}
export interface ReviewCreationCommand {
  execution_context?: ExecutionContext;
  operation: "start_review" | "continue_finding"; operation_id: string; case_id: string; expected_case_version: number;
  new_case: { title: string; description: string } | null;
  finding: Pick<CaseFinding, "title" | "statement" | "classification">;
  selection: BasisSelection; expected_basis_digest: string; review: ReviewPayload | null; from: SealedFindingReference | null; reason: string;
}
export interface ReviewCreationReceipt {
  operation_id: string; operation: ReviewCreationCommand["operation"]; actor_subject_id: string; at: string;
  case_id: string; finding_id: string; case_version: number; command: ReviewCreationCommand; basis: ReviewBasis; action: AssessmentAction | null;
}

export const conclusionLabels: Record<Conclusion, string> = localizedLabels({}, {pending_review: "m0829", supported: "m0830", not_supported: "m0831", insufficient_evidence: "m0832"});
export const progressLabels: Record<ProgressState, string> = localizedLabels({}, {not_started: "m0826", in_progress: "m0827", ended: "m2841"});
export const closureLabels: Record<Closure["kind"], string> = localizedLabels({}, {verified_remediation: "m2842", ruled_out: "m2843", duplicate: "m2844", end_followup: "m2845"});
export const evidenceKey = (e: EvidenceChoice) => `${e.scope}:${e.evidence_event_id}:${e.relation}`;
export const gapKey = (g: GapChoice) => `${g.scope}:${g.gap_id}:${g.relation}`;

// Both protocols retain their own fixed history upper bounds. The display is
// ordered by the shared Case version; records are never copied between stores.
export function combinedFindingHistory(assessment: AssessmentHistory, responsibility: FindingHistory) {
  return [
    ...assessment.items.map((action) => ({ kind: "assessment" as const, version: action.case_version, action })),
    ...responsibility.items.map((action) => ({ kind: "responsibility" as const, version: action.case_version, action })),
  ].sort((a, b) => b.version - a.version || a.kind.localeCompare(b.kind));
}

export const resourceKey = (r: ResourceChoice) => JSON.stringify([r.scope,r.reporter.source_system_id,r.reporter.source_instance,r.reporter.org_id,r.reporter.project_id,r.record_ref,r.revision,r.relation]);
