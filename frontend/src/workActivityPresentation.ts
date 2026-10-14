import { t } from "./i18n";
import type { WorkActivityEvent, WorkActivityPage } from "./api";

export type WorkActivityItem = WorkActivityPage["items"][number];

export function groupActivityPage(items: WorkActivityItem[]) {
  const groups = new Map<string, { key: string; runID?: string; items: WorkActivityItem[] }>();
  for (const item of items) {
    const { reporter, run_id: runID, event_ref: eventRef } = item.event;
    const scope = [reporter.source_system_id, reporter.source_instance, reporter.org_id, reporter.project_id];
    const key = JSON.stringify(runID ? [...scope, "run", runID] : [...scope, "event", eventRef]);
    const group = groups.get(key) ?? { key, runID, items: [] };
    group.items.push(item);
    groups.set(key, group);
  }
  return [...groups.values()];
}

export function resourceOperation(event: WorkActivityEvent): "read" | "change" | "other" | undefined {
  if (event.subject_kind !== "resource_action") return;
  if (["resource.read", "resource.read.failed", "resource.read.not_found", "resource.read.unconfirmed"].includes(event.action_kind)) return "read";
  if (["resource.write", "resource.write.failed", "resource.edit", "resource.edit.failed"].includes(event.action_kind)) return "change";
  return "other";
}

function resourceOutcome(event: WorkActivityEvent): "completed" | "failed" | "optional_not_found" | "unknown" {
  if (event.action_kind === "resource.read.not_found" && event.lifecycle_state === "failed") return "optional_not_found";
  if (event.action_kind.endsWith(".failed") || event.lifecycle_state === "failed") return "failed";
  if (event.lifecycle_state === "completed" && ["resource.read", "resource.write", "resource.edit"].includes(event.action_kind)) return "completed";
  return "unknown";
}

export function workEventLabel(event: WorkActivityEvent): string {
  switch (event.action_kind) {
    case "task.started": return t("m2988");
    case "task.completed": return t("m2989");
    case "task.failed": return t("m2990");
    case "task.ended_observed": return t("m2991");
    case "message.received_observed": return t("m2992");
    case "message.sent_observed": return t("m2993");
    case "model.requested": return t("m1056");
    case "model.started": return t("m2994");
    case "model.completed": return t("m2995");
    case "model.failed": return t("m2996");
    case "model.input_observed": return t("m2997");
    case "model.output_observed": return t("m2998");
    case "tool.invoked": return t("m2999");
    case "tool.completed": return t("m3000");
    case "tool.failed": return t("m3001");
    case "resource.read": return resourceOutcome(event) === "completed" ? t("m3002") : resourceOutcome(event) === "failed" ? t("m3003") : t("m3004");
    case "resource.read.failed": return t("m3003");
    case "resource.read.not_found": return resourceOutcome(event) === "optional_not_found" ? t("m3005") : t("m3006");
    case "resource.read.unconfirmed": return t("m3007");
    case "resource.write": return resourceOutcome(event) === "completed" ? t("m3008") : resourceOutcome(event) === "failed" ? t("m1996") : t("m3009");
    case "resource.write.failed": return t("m1996");
    case "resource.edit": return resourceOutcome(event) === "completed" ? t("m3010") : resourceOutcome(event) === "failed" ? t("m3011") : t("m3012");
    case "resource.edit.failed": return t("m3011");
    default: return ({ task: t("m3013"), message: t("m3014"), model_call: t("m3015"), tool_call: t("m3016"), resource_action: t("m3017") } as Record<string,string>)[event.subject_kind] ?? t("m0156");
  }
}

export function workEventObject(event: WorkActivityEvent): string {
  if (event.subject_kind === "task") return t("m3018");
  if (event.subject_kind === "message") return t("m3019");
  if (event.subject_kind === "model_call") return t("m2579");
  if (event.subject_kind === "tool_call") {
    return event.resource?.name_state === "readable" ? t("m3021", { value0: event.resource.name ?? t("m3020") }) : t("m3022");
  }
  return event.resource?.name_state === "readable" ? event.resource.name ?? t("m3023") :
    event.resource?.name_state === "hidden" ? t("m3024") : t("m3025");
}

export function workEventResultSummary(event: WorkActivityEvent): string {
  if (event.subject_kind === "task") {
    if (event.action_kind === "task.completed") return t("m3026");
    if (event.action_kind === "task.failed") return t("m3027");
    return t("m3028");
  }
  if (event.subject_kind === "resource_action") {
    const outcome = resourceOutcome(event);
    if (outcome === "optional_not_found") return t("m3029");
    if (outcome === "failed") return t("m3030");
    if (outcome === "completed") return t("m1369");
    return t("m3031");
  }
  if (event.subject_kind === "message") return t("m3032");
  if (event.action_kind.endsWith(".failed") || event.lifecycle_state === "failed") return t("m3030");
  if (event.lifecycle_state === "completed") return t("m3033");
  return t("m3034");
}

export function workEventResultNote(event: WorkActivityEvent): string {
  if (event.subject_kind === "task") return t("m3035");
  if (event.subject_kind === "resource_action") {
    const outcome = resourceOutcome(event);
    if (event.action_kind === "resource.read.not_found") {
      if (outcome === "optional_not_found") return t("m3036");
      return t("m3037");
    }
    if (resourceOperation(event) === "other") return t("m3038");
    if (outcome === "unknown") return t("m3039");
    if (outcome === "failed") return t("m3040");
    if (resourceOperation(event) === "read") return t("m3041");
    return t("m3042");
  }
  if (event.subject_kind === "tool_call") return t("m3043");
  if (event.subject_kind === "model_call") return t("m3044");
  return t("m3045");
}

export function groupWorkRun(items: WorkActivityItem[]) {
  const reads: WorkActivityItem[] = [];
  const changes: WorkActivityItem[] = [];
  const otherResources: WorkActivityItem[] = [];
  const modelCalls = new Map<string, WorkActivityItem[]>();
  const modelObservations: WorkActivityItem[] = [];
  const other: WorkActivityItem[] = [];
  for (const item of items) {
    const event = item.event;
    if (event.subject_kind === "resource_action") {
      const operation = resourceOperation(event);
      (operation === "read" ? reads : operation === "change" ? changes : otherResources).push(item);
    } else if (event.subject_kind === "model_call") {
      if (!event.model_call_id) modelObservations.push(item);
      else modelCalls.set(event.model_call_id, [...(modelCalls.get(event.model_call_id) ?? []), item]);
    } else other.push(item);
  }
  return { reads, changes, otherResources, modelCalls: [...modelCalls].map(([id, events]) => ({ id, events })), modelObservations, other };
}
