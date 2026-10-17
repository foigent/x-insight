import type { TimelineNode } from "./api";

const dateTimeFormatter = new Intl.DateTimeFormat("zh-CN", {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
});

const shortTimeFormatter = new Intl.DateTimeFormat("zh-CN", {
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  fractionalSecondDigits: 3,
  hour12: false,
});

const eventTitles: Record<string, string> = {
  "auth.model.rejected": "模型权限被拒绝",
  "auth.request.accepted": "请求准入完成",
  "gateway.request.accepted": "网关已接受请求",
  "route.selected": "路由已选择",
  "provider.request.attempted": "上游模型调用已尝试",
  "provider.response.accepted": "上游响应已接受",
  "billing.freeze.succeeded": "计费冻结成功",
  "billing.settled": "计费已结算",
  "billing.execution.settled": "执行计费已结算",
  "billing.execution.cancelled": "执行计费已取消",
  "usage.tokens.recorded": "用量已记录",
  "call.log.recorded": "调用日志已记录",
  "payload.policy.applied": "载荷策略已应用",
  "gap.capture.detected": "检测到证据缺口",
};

export function formatDateTime(value: string | undefined): string {
  if (!value) {
    return "未提供";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return dateTimeFormatter.format(date);
}

export function formatShortTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return shortTimeFormatter.format(date);
}

export function formatCanonicalTime(value: string | undefined): string {
  if (!value) {
    return "未提供";
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
    gateway: "网关证据",
    identity: "身份上下文",
    routing: "路由决策",
    billing: "计费决策",
    provider: "模型提供方交互",
    usage: "用量记录",
    logging: "日志记录",
    payload: "载荷策略",
    gap: "证据缺口",
    external: "外部来源事件",
  };
  return titles[stage] ?? "证据事件";
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
    return "未提供";
  }
  if (typeof value === "boolean") {
    return value ? "true" : "false";
  }
  if (typeof value === "number") {
    return new Intl.NumberFormat("zh-CN").format(value);
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

export const detailFieldLabels: Record<string, string> = {
  request_id: "Request ID",
  trace_id: "Trace ID",
  span_id: "Span ID",
  parent_span_id: "声明 Parent Span",
  run_id: "Run ID",
  session_id: "Session ID",
  upstream_request_id: "上游 Request ID",
  actor_type: "Actor 类型",
  actor_ref: "Actor 引用",
  owner_ref: "Owner 引用",
  org_id: "组织",
  project_id: "项目",
  team_ref: "团队引用",
  api_key_ref: "API Key 引用",
  end_user_ref: "最终用户引用",
  key_type: "Key 类型",
  capability_type: "能力类型",
  target_ref: "目标引用",
  model: "实际模型",
  requested_model: "请求模型",
  provider: "Provider",
  channel: "Channel",
  decision_kind: "决策类型",
  decision_result: "决策结果",
  decision_reason: "决策原因",
  policy_ref: "策略引用",
  rule_ref: "规则引用",
  prompt_tokens: "Prompt Tokens",
  completion_tokens: "Completion Tokens",
  total_tokens: "Total Tokens",
  estimated_tokens: "Estimated Tokens",
  cost_cents: "成本（分）",
  cost_micros: "精确成本（micro-CNY）",
  currency: "币种",
  amount_scale: "金额缩放因子",
  money_schema: "金额语义版本",
  latency_ms: "延迟（ms）",
  ttft_ms: "TTFT（ms）",
  usage_source: "用量来源",
  usage_confidence: "用量置信说明",
  outcome_status: "结果状态",
  status_code: "状态码",
  error_code: "错误码",
  failure_kind: "失败类型",
  payload_capture_mode: "载荷捕获模式",
  redaction_state: "脱敏状态",
  payload_ref: "载荷引用",
  retention_policy_ref: "保留策略引用",
};
