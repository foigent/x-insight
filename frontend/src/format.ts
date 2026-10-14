import { localizedLabels, getLocale, t } from "./i18n";
import type { TimelineNode } from "./api";

const dateTimeFormatter = () => new Intl.DateTimeFormat(getLocale(), {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
});

const shortTimeFormatter = () => new Intl.DateTimeFormat(getLocale(), {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  fractionalSecondDigits: 3,
  hour12: false,
});

const eventTitles: Record<string, string> = localizedLabels({}, {"auth.model.rejected": "m2906", "auth.request.accepted": "m2907", "gateway.request.accepted": "m2908", "route.selected": "m2909", "provider.request.attempted": "m2910", "provider.response.accepted": "m2911", "billing.freeze.succeeded": "m2912", "billing.settled": "m2913", "billing.execution.settled": "m2914", "billing.execution.cancelled": "m2915", "usage.tokens.recorded": "m2916", "call.log.recorded": "m2917", "payload.policy.applied": "m2918", "gap.capture.detected": "m0931"});

export function formatDateTime(value: string | undefined): string {
  if (!value) {
    return t("m0100");
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return dateTimeFormatter().format(date);
}

export function formatShortTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return shortTimeFormatter().format(date);
}

export function formatCanonicalTime(value: string | undefined): string {
  if (!value) {
    return t("m0100");
  }
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toISOString();
}

export function shortRef(value: string, head = 11, tail = 7): string {
  if (value.length <= head + tail + 1) {
    return value;
  }
  return `${value.slice(0, head)}…${value.slice(-tail)}`;
}

export function eventTitle(node: Pick<TimelineNode, "event_type" | "stage">): string {
  return eventTitles[node.event_type] ?? stageTitle(node.stage);
}

function stageTitle(stage: string): string {
  const titles: Record<string, string> = {
    gateway: t("m2919"),
    identity: t("m2920"),
    routing: t("m2921"),
    billing: t("m2922"),
    provider: t("m2923"),
    usage: t("m2924"),
    logging: t("m2925"),
    payload: t("m0557"),
    gap: t("m0521"),
    external: t("m2926"),
  };
  return titles[stage] ?? t("m0475");
}

export function toLocalInputValue(value: string | undefined): string {
  if (!value) {
    return "";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "";
  }
  const offset = date.getTimezoneOffset() * 60_000;
  return new Date(date.getTime() - offset).toISOString().slice(0, 16);
}

export function localInputToUTC(value: string): string | undefined {
  if (!value) {
    return undefined;
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return undefined;
  }
  return date.toISOString();
}

export function displayValue(value: unknown): string {
  if (value === undefined || value === null || value === "") {
    return t("m0100");
  }
  if (typeof value === "boolean") {
    return value ? "true" : "false";
  }
  if (typeof value === "number") {
    return new Intl.NumberFormat(getLocale()).format(value);
  }
  return String(value);
}

export function formatMicroCNY(value: string): string {
  if (!/^(0|[1-9][0-9]{0,18})$/.test(value)) {
    return value;
  }
  const micros = BigInt(value);
  const whole = micros / 1_000_000n;
  const fraction = (micros % 1_000_000n).toString().padStart(6, "0");
  return `CNY ${whole}.${fraction} (${value} micro-CNY)`;
}

export const detailFieldLabels: Record<string, string> = localizedLabels({request_id: "Request ID", trace_id: "Trace ID", span_id: "Span ID", run_id: "Run ID", session_id: "Session ID", provider: "Provider", channel: "Channel", prompt_tokens: "Prompt Tokens", completion_tokens: "Completion Tokens", total_tokens: "Total Tokens", estimated_tokens: "Estimated Tokens", ttft_ms: "TTFT（ms）"}, {parent_span_id: "m2927", upstream_request_id: "m2928", actor_type: "m2929", actor_ref: "m2930", owner_ref: "m2931", org_id: "m1059", project_id: "m1060", team_ref: "m2932", api_key_ref: "m2933", end_user_ref: "m2934", key_type: "m2419", capability_type: "m2935", target_ref: "m2936", model: "m1055", requested_model: "m1056", decision_kind: "m2937", decision_result: "m2938", decision_reason: "m1061", policy_ref: "m2939", rule_ref: "m2940", cost_cents: "m2941", cost_micros: "m2942", currency: "m2943", amount_scale: "m2944", money_schema: "m2945", latency_ms: "m2946", usage_source: "m2947", usage_confidence: "m2948", outcome_status: "m2949", status_code: "m2950", error_code: "m2951", failure_kind: "m2952", payload_capture_mode: "m2953", redaction_state: "m0561", payload_ref: "m2954", retention_policy_ref: "m2955"});
