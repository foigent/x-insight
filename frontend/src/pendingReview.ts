import { LocalizedError, t } from "./i18n";
import type { AssessmentCommand, ReviewCreationCommand } from "./assessmentTypes";

type ReviewCommand = AssessmentCommand | ReviewCreationCommand;
const prefix = "xinsight.pending-review.v1:";

// Keep only an unresolved finite command, never a session token or raw source
// payload. Session storage survives a refresh in this tab and is scoped to the
// human subject and operation resource. A receipt or definite rejection clears it.
export function readPendingReview<T extends ReviewCommand>(subject: string, resource: string): T | undefined {
  const raw = sessionStorage.getItem(prefix + subject + ":" + resource);
  if (!raw) return;
  if (new TextEncoder().encode(raw).length > 65536) throw new LocalizedError(() => t("m2956"));
  const value: unknown = JSON.parse(raw);
  if (!value || typeof value !== "object" || !("operation_id" in value) || typeof value.operation_id !== "string" || !("operation" in value) || !["record_review", "record_progress", "start_review", "continue_finding"].includes(String(value.operation))) throw new LocalizedError(() => t("m2957"));
  const command = value as ReviewCommand;
  const isCreation = command.operation === "start_review" || command.operation === "continue_finding";
  if ((resource === "creation") !== isCreation || !command.selection || !Array.isArray(command.selection.evidence) || !Array.isArray(command.selection.gaps) ||
      command.selection.evidence.some((e) => !e || typeof e.scope !== "string" || typeof e.evidence_event_id !== "string" || typeof e.relation !== "string" || typeof e.source_ref !== "string" || typeof e.event_hash !== "string") ||
      command.selection.gaps.some((g) => !g || typeof g.scope !== "string" || typeof g.gap_id !== "string" || typeof g.relation !== "string" || typeof g.gap_type !== "string" || typeof g.affected_claim !== "string") ||
      (isCreation && (!("finding" in command) || !command.finding || typeof command.finding.title !== "string" || typeof command.finding.statement !== "string" || typeof command.reason !== "string"))) {
    throw new LocalizedError(() => t("m2958"));
  }
  return value as T;
}

export function keepPendingReview(subject: string, resource: string, command: ReviewCommand) {
  const previous = readPendingReview(subject, resource);
  if (previous && JSON.stringify(previous) !== JSON.stringify(command)) throw new LocalizedError(() => t("m2959"));
  const raw = JSON.stringify(command);
  if (new TextEncoder().encode(raw).length > 65536) throw new LocalizedError(() => t("m2960"));
  keepPendingCommand(prefix + subject + ":" + resource, command);
}

export function clearPendingReview(subject: string, resource: string, command: ReviewCommand) {
  const key = prefix + subject + ":" + resource;
  clearPendingCommand(key, command);
}

// Shared finite-command storage for real mutation callers. Namespace and shape
// remain owned by each feature; the original review storage keys are retained.
export function readPendingCommand<T>(key: string, validate: (value: unknown) => value is T): T | undefined {
 const raw = sessionStorage.getItem(key); if (!raw) return;
 if (new TextEncoder().encode(raw).length > 65536) throw new LocalizedError(() => t("m2961"));
 const value: unknown = JSON.parse(raw); if (!validate(value)) throw new LocalizedError(() => t("m2962"));
 return value;
}
export function keepPendingCommand(key: string, command: unknown) {
 const raw = JSON.stringify(command); const previous = sessionStorage.getItem(key);
 if (previous && previous !== raw) throw new LocalizedError(() => t("m2963"));
 if (new TextEncoder().encode(raw).length > 65536) throw new LocalizedError(() => t("m2964"));
 sessionStorage.setItem(key, raw);
}
export function clearPendingCommand(key: string, command: unknown) {
 if (sessionStorage.getItem(key) === JSON.stringify(command)) sessionStorage.removeItem(key);
}
