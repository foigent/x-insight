import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { App } from "./App";
import { AssuranceBadge } from "./status";
import type {
  AccessSessionResponse,
  ConnectorListResponse,
  EvidenceEventDetail,
  ExecutionDetail,
  ExecutionSearchResponse,
  Freshness,
  GapResponse,
  InvestigationProjection,
  PostureSnapshot,
  SourceListResponse,
  TraceJourney,
  TimelineResponse,
} from "./api";

const asOf = "2026-07-13T06:05:00.000Z";
const executionID = "xe1_demo_6d636f6c6c6973696f6e";
const asyncResultWait = { timeout: 5_000 };

const investigatorSession: AccessSessionResponse = {
  session: {
    session_ref: "xr_0123456789abcdefghijkl",
    subject_id: "xs_0123456789abcdef",
    role: "investigator",
    permissions: ["evidence.read", "evidence.cite", "investigation.write"],
    created_at: "2026-07-29T08:00:00Z",
    last_seen_at: "2026-07-29T08:10:00Z",
    idle_expires_at: "2026-07-29T08:40:00Z",
    absolute_expires_at: "2026-07-29T16:00:00Z",
  },
  csrf_token: "csrf-investigator-memory-only",
};

const operatorSession: AccessSessionResponse = {
  session: {
    ...investigatorSession.session,
    session_ref: "xr_zyxwvutsrqponmlkjihgfe",
    subject_id: "xs_fedcba9876543210",
    role: "operator",
    permissions: ["evidence.read", "access.sessions.read", "access.sessions.revoke", "connector.installations.manage"],
  },
  csrf_token: "csrf-operator-memory-only",
};

const platformAdminSession: AccessSessionResponse = {
  session: {
    ...investigatorSession.session,
    session_ref: "xr_platformadmin0000000001",
    subject_id: "xs_0011223344556677",
    role: "platform_admin",
    permissions: [
      "evidence.read",
      "service_identities.read",
      "service_identities.revoke",
      "connector.installations.manage",
    ],
  },
  csrf_token: "csrf-platform-admin-memory-only",
};

const workbenchSession: AccessSessionResponse = {
  session: {
    ...investigatorSession.session,
    session_ref: "xr_workbench00000000000001",
    subject_id: "xs_8899aabbccddeeff",
    role: "workbench_reader",
    permissions: [
      "workbench.summary.read",
      "workbench.details.read",
      "workbench.coverage.read",
      "workbench.preferences.write",
    ],
  },
  csrf_token: "csrf-workbench-memory-only",
};

const freshness: Freshness = {
  integration_mode: "synthetic_fixture",
  sync_state: "gap",
  timeline_complete: false,
  source_terminal_at: "2026-07-13T06:00:04.000Z",
  source_generated_at: "2026-07-13T06:00:05.000Z",
  imported_at: "2026-07-13T06:00:06.000Z",
  search_visible_at: "2026-07-13T06:00:07.000Z",
  source_revision: "source-r17",
  imported_revision: "import-r17",
  source_watermark: "terminal:17",
  import_watermark: "event:4",
};

const summary = {
  execution_id: executionID,
  request_id: "req-collusion-demo-001",
  source_system_id: "src-fixture-01",
  source_type: "unirouter",
  source_display_name: "UniRouter 合成夹具",
  outcome: "failure" as const,
  first_effective_at: "2026-07-13T06:00:01.000Z",
  last_effective_at: "2026-07-13T06:00:04.000Z",
  event_count: 6,
  gap_count: 1,
  has_gaps: true,
  payload_capture_modes: ["metadata_only"],
  proof_class: "synthetic_fixture" as const,
  assurance_mode: "synthetic_fixture" as const,
  freshness,
};

const searchResponse: ExecutionSearchResponse = {
  items: [summary],
  page: { as_of: asOf, count: 1, has_more: true, next_cursor: "cursor-page-2" },
};

const emptySearchResponse: ExecutionSearchResponse = {
  items: [],
  page: { as_of: asOf, count: 0, has_more: false },
};

const sourcesResponse: SourceListResponse = {
  items: [
    {
      source_system_id: "src-fixture-01",
      source_key: "fixture/unirouter",
      source_type: "unirouter",
      display_name: "UniRouter 合成夹具",
      source_version: "fixture-v1",
    },
  ],
};

const postureResponse: PostureSnapshot = {
  schema_version: "xinsight.posture.v1",
  as_of: asOf,
  from: "2026-07-12T06:05:00.000Z",
  to: asOf,
  risk: {
    open_total: 0,
    critical: 0,
    error: 0,
    warning: 0,
    info: 0,
    affected_executions: 0,
    unlinked: 0,
    items: [],
  },
  executions: {
    total: 1,
    success: 0,
    failure: 1,
    partial: 0,
    unknown: 0,
    with_gaps: 1,
    total_gaps: 1,
  },
  coverage: [
    {
      dimension: "temporal",
      label: "Direct time",
      available: true,
      numerator: 1,
      denominator: 1,
      ratio_decimal: "1.0000",
      unit: "executions",
      basis: "direct timestamps",
      limitation: "ingestion time excluded",
    },
    {
      dimension: "identity",
      label: "Identity",
      available: false,
      numerator: 0,
      denominator: 0,
      unit: "executions",
      basis: "identity group",
      limitation: "presence is not completeness",
    },
    {
      dimension: "outcome",
      label: "Outcome",
      available: true,
      numerator: 1,
      denominator: 1,
      ratio_decimal: "1.0000",
      unit: "executions",
      basis: "deterministic rollup",
      limitation: "terminal completeness remains separate",
    },
    {
      dimension: "causal",
      label: "Causal links",
      available: true,
      numerator: 3,
      denominator: 4,
      ratio_decimal: "0.7500",
      unit: "declared_parent_links",
      unresolved_count: 1,
      basis: "source-declared parents",
      limitation: "chronology does not create edges",
    },
  ],
  sources: [{
    source_system_id: "src-fixture-01",
    source_type: "unirouter",
    source_display_name: "UniRouter 合成夹具",
    execution_total: 1,
    failure_total: 1,
    executions_with_gaps: 1,
    last_effective_at: summary.last_effective_at,
  }],
  trend: {
    bucket_unit: "hour",
    items: [{
      bucket_start: "2026-07-13T06:00:00.000Z",
      execution_total: 1,
      failure_total: 1,
      partial_total: 0,
      executions_with_gaps: 1,
      open_alerts: 0,
    }],
    boundary: "fixed-window counts; absence is not health",
  },
  boundaries: ["fixed_snapshot", "coverage_is_not_ground_truth_completeness"],
};

const openCase = {
  id: "xc_AAAAAAAAAAAAAAAAAAAAAA",
  title: "Fallback investigation",
  description: "Evidence-backed review",
  status: "open" as const,
  created_by_subject: investigatorSession.session.subject_id,
  created_at: "2026-07-31T07:00:00Z",
  updated_at: "2026-07-31T07:30:00Z",
  version: 2,
};

const serviceIdentity = {
  id: "xsi_AAAAAAAAAAAAAAAAAAAAAA",
  display_name: "investigation-agent",
  status: "active",
  permissions: ["evidence.read", "evidence.cite"],
  created_by_subject: platformAdminSession.session.subject_id,
  created_at: "2026-07-31T07:00:00Z",
  updated_at: "2026-07-31T07:00:00Z",
  expires_at: "2026-08-01T07:00:00Z",
};

const executionDetail: ExecutionDetail = { summary, as_of: asOf, trace_ids: ["trace-demo-001"] };
const recoveredSummary = { ...summary, outcome: "success" as const };
const recoveredSearchResponse: ExecutionSearchResponse = {
  ...searchResponse,
  items: [recoveredSummary],
};
const recoveredExecutionDetail: ExecutionDetail = {
  ...executionDetail,
  summary: recoveredSummary,
};

const traceJourney: TraceJourney = {
  schema_version: "xinsight.trace-journey.v1",
  available: true,
  anchor_execution_id: executionID,
  source_system_id: "11111111-1111-4111-8111-111111111111",
  trace_id: "0123456789abcdef0123456789abcdef",
  as_of: asOf,
  nodes: [
    {
      execution_id: executionID,
      event_id: "11111111-1111-4111-8111-111111111111",
      request_id: "trace-root",
      source_type: "openinference",
      source_ref: "openinference:trace:root",
      event_hash: `sha256:${"1".repeat(64)}`,
      trace_id: "0123456789abcdef0123456789abcdef",
      span_id: "1111111111111111",
      actor_type: "human",
      actor_ref: "study-user",
      action: "agent.plan",
      service_name: "agent-orchestrator",
      provider: "provider-a",
      model: "model-a",
      outcome: "success",
      effective_at: "2026-07-13T06:00:01.000Z",
    },
    {
      execution_id: "xe1_demo_policy_router",
      event_id: "22222222-2222-4222-8222-222222222222",
      request_id: "trace-middle",
      source_type: "openinference",
      source_ref: "openinference:trace:middle",
      event_hash: `sha256:${"2".repeat(64)}`,
      trace_id: "0123456789abcdef0123456789abcdef",
      span_id: "2222222222222222",
      parent_span_id: "1111111111111111",
      action: "route.model_call",
      service_name: "policy-router",
      provider: "provider-b",
      model: "model-b",
      outcome: "success",
      effective_at: "2026-07-13T06:00:02.000Z",
    },
    {
      execution_id: "xe1_demo_model_provider",
      event_id: "33333333-3333-4333-8333-333333333333",
      request_id: "trace-leaf",
      source_type: "openinference",
      source_ref: "openinference:trace:leaf",
      event_hash: `sha256:${"3".repeat(64)}`,
      trace_id: "0123456789abcdef0123456789abcdef",
      span_id: "3333333333333333",
      parent_span_id: "2222222222222222",
      action: "chat.completions",
      service_name: "model-provider",
      provider: "provider-b",
      model: "model-b",
      outcome: "success",
      effective_at: "2026-07-13T06:00:03.000Z",
    },
  ],
  edges: [
    {
      from_event_id: "11111111-1111-4111-8111-111111111111",
      to_event_id: "22222222-2222-4222-8222-222222222222",
      edge_basis: "source_declared_parent_span_across_execution",
    },
    {
      from_event_id: "22222222-2222-4222-8222-222222222222",
      to_event_id: "33333333-3333-4333-8333-333333333333",
      edge_basis: "source_declared_parent_span_across_execution",
    },
  ],
  integrity: {
    node_count: 3,
    execution_count: 3,
    declared_parent_count: 2,
    resolved_parent_count: 2,
    unresolved_parent_count: 0,
    ambiguous_parent_count: 0,
    cycle_detected: false,
    truncated: false,
    source_timeline_complete: false,
    visible_parent_resolution_complete: true,
  },
  evidence_boundary: "source-declared trace/span correlation across the same source, organization and project",
};

const timelineResponse: TimelineResponse = {
  execution_id: executionID,
  total_event_count: 6,
  page: { as_of: asOf, count: 6, has_more: false },
  freshness,
  items: [
    {
      event_id: "event-route",
      event_type: "route.selected",
      stage: "routing",
      severity: "info",
      effective_at: "2026-07-13T06:00:01.000Z",
      occurred_at: "2026-07-13T06:00:03.000Z",
      ingested_at: "2026-07-13T06:00:06.000Z",
      source_ref: "fixture://route",
      source_type: "unirouter",
      decision_result: "allow",
      field_provenance: {
        correlation: "direct",
        identity: "derived",
        capability: "declared",
        decision: "suppressed",
        usage: "unknown",
        outcome: "not_applicable",
        payload_policy: "gap",
      },
    },
    {
      event_id: "event-provider-a",
      event_type: "provider.request.attempted",
      stage: "provider",
      severity: "warning",
      effective_at: "2026-07-13T06:00:02.000Z",
      occurred_at: "2026-07-13T05:59:59.000Z",
      ingested_at: "2026-07-13T06:00:06.000Z",
      declared_parent_event_id: "event-route",
      declared_parent_resolved: true,
      source_ref: "fixture://provider-a",
      source_type: "unirouter",
      field_provenance: { correlation: "direct", capability: "declared" },
    },
    {
      event_id: "event-retry",
      event_type: "retry.attempt.scheduled",
      stage: "routing",
      severity: "info",
      effective_at: "2026-07-13T06:00:02.500Z",
      ingested_at: "2026-07-13T06:00:06.000Z",
      declared_parent_event_id: "event-route",
      declared_parent_resolved: true,
      source_ref: "fixture://retry",
      source_type: "unirouter",
      decision_result: "allowed",
      field_provenance: { decision: "declared" },
    },
    {
      event_id: "event-fallback",
      event_type: "fallback.route.selected",
      stage: "routing",
      severity: "info",
      effective_at: "2026-07-13T06:00:03.000Z",
      ingested_at: "2026-07-13T06:00:06.000Z",
      declared_parent_event_id: "event-retry",
      declared_parent_resolved: true,
      source_ref: "fixture://fallback",
      source_type: "unirouter",
      decision_result: "selected",
      field_provenance: { decision: "declared" },
    },
    {
      event_id: "event-provider-b",
      event_type: "provider.response.accepted",
      stage: "provider",
      severity: "info",
      effective_at: "2026-07-13T06:00:03.500Z",
      ingested_at: "2026-07-13T06:00:06.000Z",
      source_ref: "fixture://provider-b",
      source_type: "unirouter",
      field_provenance: { outcome: "direct" },
    },
    {
      event_id: "event-gap",
      event_type: "gap.capture.detected",
      stage: "gap",
      severity: "error",
      effective_at: "2026-07-13T06:00:04.000Z",
      ingested_at: "2026-07-13T06:00:06.000Z",
      declared_parent_event_id: "event-not-present",
      declared_parent_resolved: false,
      source_ref: "fixture://gap",
      source_type: "unirouter",
      field_provenance: {},
    },
  ],
};

const recoveredTimelineResponse: TimelineResponse = {
  ...timelineResponse,
  items: timelineResponse.items.map((item) =>
    item.event_id === "event-provider-a"
      ? { ...item, outcome_status: "failure" }
      : item,
  ),
};

const eventDetail: EvidenceEventDetail & { raw_source_excerpt?: string } = {
  execution_id: executionID,
  event_id: "event-route",
  event_type: "route.selected",
  stage: "routing",
  severity: "info",
  capture_mode: "metadata_only",
  occurred_at: "2026-07-13T06:00:03.000Z",
  ingested_at: "2026-07-13T06:00:06.000Z",
  correlation: { request_id: "req-collusion-demo-001", trace_id: "trace-demo-001" },
  identity: { actor_type: "agent", actor_ref: "agent:sanitized" },
  capability: { capability_type: "chat", requested_model: "sanitized-model" },
  decision: { decision_kind: "route", decision_result: "allow", policy_ref: "policy:sanitized" },
  usage: {
    total_tokens: 31,
    cost_micros: "7",
    currency: "CNY",
    amount_scale: 1000000,
    money_schema: "micro-cny.v1",
    latency_ms: 120,
    usage_source: "source_declared",
  },
  outcome: { outcome_status: "failure", error_code: "fixture_failure" },
  payload_policy: { payload_capture_mode: "metadata_only", redaction_state: "not_captured" },
  field_provenance: {
    correlation: "direct",
    identity: "derived",
    capability: "declared",
    decision: "suppressed",
    usage: "unknown",
    outcome: "not_applicable",
    payload_policy: "gap",
  },
  source: {
    source_system_id: "src-fixture-01",
    adapter_manifest_id: "manifest-fixture-v1",
    schema_version: "xinsight.evidence.v1",
    source_type: "unirouter",
    source_version: "fixture-v1",
    source_event_id: "source-event-route",
    source_event_type: "route.selected",
    source_ref: "fixture://route",
    event_hash: "sha256:event-safe-demo",
    source_event_hash: "sha256:source-safe-demo",
    fidelity_level: "F2",
  },
  raw_source_excerpt: "TOP-SECRET-HOSTILE-PROMPT",
};

const gapResponse: GapResponse = {
  execution_id: executionID,
  total_gap_count: 1,
  page: { as_of: asOf, count: 1, has_more: false },
  freshness,
  items: [
    {
      gap_id: "gap-01",
      evidence_event_id: "event-gap",
      gap_type: "provider_response_not_captured",
      candidate_gap_types: ["transport_loss", "source_suppressed"],
      affected_claim: "无法确认 provider response 内容",
      gap_reason: "来源未提供该字段",
      severity: "error",
      recommendation: "检查来源 capture policy",
      created_at: "2026-07-13T06:00:04.000Z",
    },
  ],
};

beforeEach(() => {
  window.history.replaceState(null, "", "/executions");
  Object.defineProperty(window, "scrollTo", { configurable: true, value: vi.fn() });
  Object.defineProperty(window.HTMLElement.prototype, "scrollIntoView", { configurable: true, value: vi.fn() });
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  const environment = globalThis as typeof globalThis & {
    jsdom?: { reconfigure(options: { url: string }): void };
  };
  environment.jsdom?.reconfigure({ url: "https://x-insight.local/" });
});

describe("PRD2 investigation UI", () => {
  it("renders the bounded-offline assurance mode as an explicit product label", () => {
    render(<AssuranceBadge assuranceMode="bounded_offline_import" />);
    expect(screen.getByText("受限离线导入")).toBeTruthy();
    expect(screen.getByText("bounded_offline_import")).toBeTruthy();
  });

  it("renders bounded results, zh-CN scope copy and all seven semantic labels", async () => {
    installAPI();
    render(<App />);

    expect(await screen.findByRole("button", { name: "req-collusion-demo-001" }, asyncResultWait)).toBeTruthy();
    expect(screen.getAllByText("工作空间")[0]).toBeTruthy();
    expect(screen.getByText("按当前授权范围展示资料")).toBeTruthy();
    expect(screen.getByLabelText("访问状态").textContent).toContain("HTTPS · 产品 Session");
    expect(screen.getByRole("link", { name: /产品手册/ }).getAttribute("href")).toBe("/manual/zh-CN/");
    for (const label of ["直接证据", "推导", "声明", "已抑制", "缺口", "未知", "不适用"]) {
      expect(screen.getAllByText(label).length).toBeGreaterThan(0);
    }
    expect(screen.getByText("合成夹具")).toBeTruthy();
    expect(screen.getByText("存在证据缺口")).toBeTruthy();
  });

  it.each(["/inbox", "/cases/xc_demo?queue_return=%2Finvestigation%2Fqueue"])("retains a valid origin topic on shared execution details: %s", async origin => {
    installAPI();
    window.history.replaceState({workbenchOrigin:origin}, "", `/executions/${executionID}?as_of=${encodeURIComponent(asOf)}`);
    render(<App/>);
    await screen.findByRole("heading",{name:"执行路径调查",level:1},asyncResultWait);
    if(origin==="/inbox") expect(screen.queryByRole("link",{name:/← 返回.*的原页面/})).toBeNull();
    else expect(screen.getByRole("link",{name:/← 返回.*的原页面/}).getAttribute("href")).toBe(origin);
    expect(window.history.state?.workbenchOrigin).toBe(origin);
    expect(screen.getByRole("button",{name:origin==="/inbox"?"← 返回总览":"← 返回安全与复核"})).toBeTruthy();
    const primary=within(screen.getByRole("navigation",{name:"产品功能"}));
    expect(primary.getByRole("link",{name:origin==="/inbox"?/总览/:/安全与复核/}).getAttribute("aria-current")).toBe("page");
    expect(primary.getByRole("link",{name:/运行与执行/}).getAttribute("aria-current")).toBeNull();
    fireEvent.click(screen.getByRole("button",{name:origin==="/inbox"?"← 返回总览":"← 返回安全与复核"}));
    expect(window.location.pathname+window.location.search).toBe(origin);
  });

  it("uses canonical execution navigation for a direct link or invalid external origin",async()=>{
    installAPI();
    window.history.replaceState({workbenchOrigin:"https://external.example/path"},"",`/executions/${executionID}`);
    render(<App/>);
    await screen.findByRole("heading",{name:"执行路径调查",level:1},asyncResultWait);
    expect(screen.queryByRole("link",{name:/← 返回.*的原页面/})).toBeNull();
    expect(screen.getByRole("link",{name:/运行与执行/}).getAttribute("aria-current")).toBe("page");
  });

  it("uses an explicit compact navigation toggle and closes it after route navigation", async () => {
    installAPI({ openCase: true });
    const user = userEvent.setup();
    window.history.replaceState(null, "", "/inbox");
    render(<App />);

    expect(await screen.findByRole("heading", { name: "运行态势" })).toBeTruthy();
    const toggle = screen.getByRole("button", { name: "菜单" });
    expect(toggle.getAttribute("aria-expanded")).toBe("false");
    await user.click(toggle);
    expect(screen.getByRole("button", { name: "关闭" }).getAttribute("aria-expanded")).toBe("true");
    await user.click(screen.getByRole("link", { name: /安全与复核/ }));
    await user.click(screen.getByRole("link", { name: "调查案件" }));
    expect(await screen.findByRole("heading", { name: "调查案件" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "菜单" }).getAttribute("aria-expanded")).toBe("false");
  });

  it("renders one fixed posture scope without turning missing coverage into health", async () => {
    const fetchMock = installAPI();
    window.history.replaceState(null, "", "/inbox");
    render(<App />);

    expect(await screen.findByRole("heading", { name: "运行态势" })).toBeTruthy();
    expect(await screen.findByRole("heading", { name: "AI Runtime Path" }, asyncResultWait)).toBeTruthy();
    expect(screen.getByText("N/A")).toBeTruthy();
    expect(screen.getByText("MaaS 指标不在这里重复成为第二套运营真相。")).toBeTruthy();
    expect(await screen.findByText("agent-orchestrator", {}, asyncResultWait)).toBeTruthy();

    const postureCall = fetchMock.mock.calls.find(([input]) => String(input).startsWith("/api/v1/posture?"));
    expect(postureCall).toBeDefined();
    const params = new URL(String(postureCall![0]), "https://x-insight.local").searchParams;
    expect(params.get("as_of")).toBe(params.get("to"));
    const requestedAsOf = Date.parse(params.get("as_of") ?? "");
    const requestedFrom = Date.parse(params.get("from") ?? "");
    expect(requestedAsOf - requestedFrom).toBe(24 * 60 * 60 * 1000);
  });

  it("does not attach an unrelated execution path to an unlinked Alert", async () => {
    const unlinkedPosture: PostureSnapshot = {
      ...postureResponse,
      risk: {
        ...postureResponse.risk,
        open_total: 1,
        warning: 1,
        unlinked: 1,
        items: [{
          id: `xal_${"a".repeat(64)}`,
          source_system_id: "src-fixture-01",
          source_alert_ref: "fixture:unlinked-alert",
          title: "Unlinked identity alert",
          severity: "warning",
          state: "open",
          observed_at: asOf,
        }],
      },
    };
    const fetchMock = installAPI({ posture: unlinkedPosture });
    const user = userEvent.setup();
    window.history.replaceState(null, "", "/inbox");
    render(<App />);

    expect(await screen.findByText("Unlinked identity alert")).toBeTruthy();
    expect(await screen.findByText("没有可锚定的 execution")).toBeTruthy();
    expect(fetchMock.mock.calls.map(([input]) => String(input)).some((url) => url.includes("/trace-journey?"))).toBe(false);
    await user.click(screen.getByRole("button", { name: /从最高优先级证据开始/ }));
    expect(await screen.findByRole("heading", { name: "可审查的假设，不是系统事实" })).toBeTruthy();
    expect(window.location.pathname).toBe("/discovery");
  });

  it("applies only the accepted bounded filters and preserves cursor paging", async () => {
    const fetchMock = installAPI();
    const user = userEvent.setup();
    render(<App />);

    await screen.findByRole("button", { name: "req-collusion-demo-001" }, asyncResultWait);
    await user.selectOptions(screen.getByLabelText("Request ID 匹配方式"), "prefix");
    await user.type(screen.getByLabelText("Request ID"), "req");
    await user.selectOptions(screen.getByLabelText("来源"), "src-fixture-01");
    await user.selectOptions(screen.getByLabelText("结果"), "failure");
    await user.selectOptions(screen.getByLabelText("Capture gaps"), "true");
    fireEvent.change(screen.getByLabelText("开始时间（本地）"), { target: { value: "2026-07-12T08:00" } });
    fireEvent.change(screen.getByLabelText("结束时间（本地）"), { target: { value: "2026-07-13T08:00" } });
    await user.click(screen.getByRole("button", { name: "查询" }));

    await waitFor(() => {
      const url = searchURLs(fetchMock).at(-1);
      expect(url).toBeDefined();
      const params = new URL(url!, "http://x-insight.local").searchParams;
      expect(params.get("request_id_prefix")).toBe("req");
      expect(params.has("request_id")).toBe(false);
      expect(params.get("source_system_id")).toBe("src-fixture-01");
      expect(params.get("outcome")).toBe("failure");
      expect(params.get("has_gaps")).toBe("true");
      expect(params.has("from")).toBe(true);
      expect(params.has("to")).toBe(true);
      expect(params.get("limit")).toBe("50");
    });

    await user.click(screen.getByRole("button", { name: "下一页" }));
    await waitFor(() => {
      const url = searchURLs(fetchMock).at(-1)!;
      expect(new URL(url, "http://x-insight.local").searchParams.get("cursor")).toBe("cursor-page-2");
    });
    expect(screen.getByText("第 2 页")).toBeTruthy();
    expect((screen.getByRole("button", { name: "上一页" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("returns from a selected execution to the same filtered second page and restores browser history", async () => {
    const fetchMock = installAPI({ responseFor: url => {
      if (!/^\/api\/v1\/executions\?/.test(url)) return;
      const params = new URL(url, window.location.origin).searchParams;
      return jsonResponse({ ...searchResponse, items: params.has("cursor") ? searchResponse.items : [{ ...searchResponse.items[0], execution_id: "xe1_first_page", request_id: "req-first-page" }], page: { ...searchResponse.page, has_more: !params.has("cursor"), next_cursor: params.has("cursor") ? undefined : "cursor-page-2" } });
    }});
    window.history.replaceState(null, "", `/executions?request_id_prefix=req&as_of=${encodeURIComponent(asOf)}`);
    const user = userEvent.setup(); render(<App/>);
    await user.click(await screen.findByRole("button", { name: "下一页" }));
    await user.click(await screen.findByRole("button", { name: "req-collusion-demo-001" }));
    const preview = await screen.findByRole("complementary", { name: "所选运行摘要" });
    await within(preview).findByRole("heading", { name: "req-collusion-demo-001" });
    const origin = window.location.pathname + window.location.search;
    await user.click(within(preview).getByRole("button", { name: "进入完整执行详情" }));
    await screen.findByRole("heading", { name: "执行路径调查" });
    expect(window.history.state?.workbenchOrigin).toBe(origin);
    await user.click(screen.getByRole("button", { name: "← 返回运行与执行" }));
    await screen.findByRole("complementary", { name: "所选运行摘要" });
    expect(window.location.pathname + window.location.search).toBe(origin);
    expect(screen.getByText("第 2 页")).toBeTruthy();
    expect(new URL(searchURLs(fetchMock).at(-1)!, window.location.origin).searchParams.get("cursor")).toBe("cursor-page-2");
    await user.click(screen.getByRole("button", { name: "上一页" }));
    await screen.findByRole("button", { name: "req-first-page" });
    act(() => window.history.back());
    await screen.findByRole("button", { name: "req-collusion-demo-001" });
    expect(window.location.pathname + window.location.search).toBe(origin);
  });

  it("keeps a copied cursor link bounded when its previous-page history is missing", async () => {
    const fetchMock = installAPI();
    window.history.replaceState(null, "", `/executions?as_of=${encodeURIComponent(asOf)}&cursor=cursor-page-2&selected=${executionID}`);
    const user = userEvent.setup(); render(<App/>);
    await screen.findByRole("complementary", { name: "所选运行摘要" });
    expect(screen.getByText("此链接没有可用的上一页游标记录；无法恢复页号或推算上一页。")).toBeTruthy();
    expect((screen.getByRole("button", { name: "上一页" }) as HTMLButtonElement).disabled).toBe(true);
    expect(screen.queryByText("第 2 页")).toBeNull();
    await user.click(screen.getByRole("button", { name: "从此截止第一页重新读取" }));
    await screen.findByRole("button", { name: "req-collusion-demo-001" });
    const query = new URL(searchURLs(fetchMock).at(-1)!, window.location.origin).searchParams;
    expect(query.has("cursor")).toBe(false); expect(query.get("as_of")).toBe(asOf);
    expect(screen.queryByRole("complementary", { name: "所选运行摘要" })).toBeNull();
  });

  it.each([false, true])("follows cutoff, event and view history without replacing an off-batch deep link (projection unavailable: %s)", async projectionUnavailable => {
    const nextAsOf = "2026-07-13T06:06:00.000Z";
    const fetchMock = installAPI({ responseFor: url => {
      const parsed = new URL(url, window.location.origin);
      if (parsed.pathname === `/api/v1/executions/${executionID}`) return jsonResponse({ ...executionDetail, as_of: parsed.searchParams.get("as_of") ?? asOf });
      if (projectionUnavailable && parsed.pathname.endsWith("/investigation")) return problemResponse(503, "database_unavailable", "projection-unavailable");
      if (parsed.pathname.endsWith("/events/off-batch")) return jsonResponse({ ...eventDetail, event_id: "off-batch", source: { ...eventDetail.source, source_ref: "source:off-batch" } });
      if (parsed.pathname.endsWith("/events/missing-event")) return problemResponse(404, "event_not_found", "missing-deep-link");
    }});
    const first = `/executions/${executionID}?as_of=${encodeURIComponent(asOf)}&evidence=off-batch`;
    window.history.replaceState(null, "", first); render(<App/>);
    const direct = await screen.findByRole("region", { name: "链接指定的证据事件" });
    expect(await within(direct).findByText("source:off-batch")).toBeTruthy();
    expect(new URLSearchParams(window.location.search).get("evidence")).toBe("off-batch");
    const target = `/executions/${executionID}?as_of=${encodeURIComponent(nextAsOf)}&evidence=event-route&view=chronology`;
    act(() => { window.history.pushState(window.history.state, "", target); window.dispatchEvent(new PopStateEvent("popstate")); });
    await screen.findByRole("list", { name: "按时间升序排列的证据事件" });
    await waitFor(() => expect(fetchMock.mock.calls.some(([url]) => String(url).includes("/events/event-route?") && String(url).includes(encodeURIComponent(nextAsOf)))).toBe(true));
    expect(new URLSearchParams(window.location.search).get("as_of")).toBe(nextAsOf);
    act(() => window.history.back());
    await screen.findByRole("region", { name: "链接指定的证据事件" });
    expect(window.location.pathname + window.location.search).toBe(first);
    act(() => { window.history.pushState(window.history.state, "", first.replace("off-batch", "missing-event")); window.dispatchEvent(new PopStateEvent("popstate")); });
    expect(await screen.findByText("未找到该证据事件")).toBeTruthy();
    expect(new URLSearchParams(window.location.search).get("evidence")).toBe("missing-event");
    expect(fetchMock.mock.calls.filter(([url]) => String(url).includes("/events/missing-event?")).length).toBe(1);
  });

  it.each(["cutoff", "execution"])("does not append a late timeline page after moving to another %s", async change => {
    const nextAsOf = "2026-07-13T06:06:00.000Z";
    const targetID = change === "execution" ? "xe1_other_execution" : executionID;
    let resolve!: (response: Response) => void;
    installAPI({ responseFor: url => {
      const parsed = new URL(url, window.location.origin), cutoff = parsed.searchParams.get("as_of") ?? asOf;
      if ([executionID, targetID].some(id => parsed.pathname === `/api/v1/executions/${id}`)) return jsonResponse({ ...executionDetail, summary: { ...executionDetail.summary, execution_id: parsed.pathname.split("/").at(-1)! }, as_of: cutoff });
      if (parsed.pathname.endsWith("/timeline")) {
        if (parsed.searchParams.has("cursor")) return new Promise(r => { resolve = r; });
        return jsonResponse({ ...timelineResponse, items: [timelineResponse.items[0]], page: { as_of: cutoff, count: 1, has_more: cutoff === asOf, next_cursor: cutoff === asOf ? "late-page" : undefined } });
      }
    }});
    window.history.replaceState(null, "", `/executions/${executionID}?as_of=${encodeURIComponent(asOf)}&view=chronology`);
    const user = userEvent.setup(); render(<App/>);
    await user.click(await screen.findByRole("button", { name: "加载更多时间线" }));
    act(() => { window.history.pushState(null, "", `/executions/${targetID}?as_of=${encodeURIComponent(nextAsOf)}&view=chronology`); window.dispatchEvent(new PopStateEvent("popstate")); });
    const list = await screen.findByRole("list", { name: "按时间升序排列的证据事件" });
    await waitFor(() => expect(screen.queryByRole("button", { name: /加载更多时间线|正在加载…/ })).toBeNull());
    await act(async () => resolve(jsonResponse({ ...timelineResponse, items: [{ ...timelineResponse.items[0], event_id: "late-stale-event" }], page: { as_of: asOf, count: 1, has_more: false } })));
    expect(within(list).getAllByRole("listitem").length).toBe(1);
    expect(new URLSearchParams(window.location.search).get("as_of")).toBe(nextAsOf);
  });

  it.each(["list", "detail"])("recovers a previous %s while the next reading is still pending", async kind => {
    const nextAsOf = "2026-07-13T06:06:00.000Z";
    let resolve!: (response: Response) => void;
    installAPI({ responseFor: url => {
      const parsed = new URL(url, window.location.origin);
      const target = kind === "list" ? "/api/v1/executions" : `/api/v1/executions/${executionID}`;
      if (parsed.pathname === target && parsed.searchParams.get("as_of") === nextAsOf) return new Promise(r => { resolve = r; });
    }});
    const path = kind === "list" ? "/executions" : `/executions/${executionID}`;
    const first = `${path}?as_of=${encodeURIComponent(asOf)}`;
    window.history.replaceState(null, "", first); render(<App/>);
    const result = () => screen.findByRole(kind === "list" ? "button" : "heading", { name: "req-collusion-demo-001" });
    await result();
    act(() => { window.history.pushState(null, "", `${path}?as_of=${encodeURIComponent(nextAsOf)}`); window.dispatchEvent(new PopStateEvent("popstate")); });
    await screen.findByText(kind === "list" ? "正在查询 executions" : "正在固定执行快照");
    act(() => window.history.back());
    await result();
    await act(async () => resolve(jsonResponse(kind === "list" ? { ...searchResponse, items: [{ ...searchResponse.items[0], request_id: "stale-pending-reading" }], page: { ...searchResponse.page, as_of: nextAsOf } } : { ...executionDetail, summary: { ...executionDetail.summary, request_id: "stale-pending-reading" }, as_of: nextAsOf })));
    expect(screen.queryByText("stale-pending-reading")).toBeNull();
    expect(window.location.pathname + window.location.search).toBe(first);
  });

  it("keeps unsent filter input when the first response fixes the reading cutoff", async () => {
    let resolve!: (response: ExecutionSearchResponse) => void;
    installAPI({ pendingSearch: new Promise(r => { resolve = r; }) });
    render(<App/>); await screen.findByText("正在查询 executions");
    fireEvent.change(screen.getByLabelText("Request ID"), { target: { value: "unsent-filter-draft" } });
    await act(async () => resolve(searchResponse));
    await screen.findByRole("button", { name: "req-collusion-demo-001" });
    expect((screen.getByLabelText("Request ID") as HTMLInputElement).value).toBe("unsent-filter-draft");
    expect(new URLSearchParams(window.location.search).get("as_of")).toBe(asOf);
    expect(new URLSearchParams(window.location.search).has("request_id")).toBe(false);
  });

  it("ignores a late preview response after another request is selected", async () => {
    const otherID = "xe1_preview_other"; let resolve!: (response: Response) => void;
    installAPI({ responseFor: url => {
      const path = new URL(url, window.location.origin).pathname;
      if (path === "/api/v1/executions") return jsonResponse({ ...searchResponse, items: [...searchResponse.items, { ...searchResponse.items[0], execution_id: otherID, request_id: "req-other-preview" }], page: { ...searchResponse.page, count: 2, has_more: false } });
      if (path === `/api/v1/executions/${executionID}`) return new Promise(r => { resolve = r; });
      if (path === `/api/v1/executions/${otherID}`) return jsonResponse({ ...executionDetail, summary: { ...executionDetail.summary, execution_id: otherID, request_id: "req-other-preview" } });
    }});
    const user = userEvent.setup(); render(<App/>);
    await user.click(await screen.findByRole("button", { name: "req-collusion-demo-001" }));
    await screen.findByText("正在读取所选运行");
    await user.click(screen.getByRole("button", { name: "req-other-preview" }));
    const preview = screen.getByRole("complementary", { name: "所选运行摘要" });
    await within(preview).findByRole("heading", { name: "req-other-preview" });
    await act(async () => resolve(jsonResponse(executionDetail)));
    expect(within(preview).queryByRole("heading", { name: "req-collusion-demo-001" })).toBeNull();
    expect(new URLSearchParams(window.location.search).get("selected")).toBe(otherID);
  });

  it("preserves a scoped reading on normal focus and explicitly recovers an old cursor after a scope change", async () => {
    let scope = "scope-a", newScopeCursorAttempts = 0;
    const scopedSession: AccessSessionResponse = { ...investigatorSession, scope_revision: scope, session: { ...investigatorSession.session, role: "workbench_investigation_reader", permissions: ["investigation.scoped.read", "workbench.summary.read"] } };
    const fetchMock = installAPI({ responseFor: url => {
      if (url === "/api/v1/access/session") return jsonResponse({ ...scopedSession, scope_revision: scope });
      if (/^\/api\/v1\/executions\?/.test(url) && scope === "scope-b" && new URL(url, window.location.origin).searchParams.has("cursor")) { newScopeCursorAttempts++; return problemResponse(400, "cursor_mismatch", "scope-cursor-changed"); }
    }});
    window.history.replaceState(null, "", `/executions?as_of=${encodeURIComponent(asOf)}&cursor=scope-a-cursor&selected=${executionID}`);
    const user = userEvent.setup(); render(<App/>);
    const preview = await screen.findByRole("complementary", { name: "所选运行摘要" });
    await within(preview).findByRole("heading", { name: "req-collusion-demo-001" });
    const beforeSearches = searchURLs(fetchMock).length;
    fireEvent(window, new Event("focus"));
    await waitFor(() => expect(fetchMock.mock.calls.filter(([url]) => url === "/api/v1/access/session").length).toBeGreaterThanOrEqual(3));
    expect(screen.getByRole("complementary", { name: "所选运行摘要" })).toBe(preview);
    expect(searchURLs(fetchMock).length).toBe(beforeSearches);
    scope = "scope-b"; fireEvent(window, new Event("focus"));
    await screen.findByText("分页状态不匹配");
    expect(screen.queryByRole("complementary", { name: "所选运行摘要" })).toBeNull();
    expect(newScopeCursorAttempts).toBe(1);
    expect(new URLSearchParams(window.location.search).get("as_of")).toBe(asOf);
    await user.click(screen.getByRole("button", { name: "从此截止第一页重新读取" }));
    await screen.findByRole("button", { name: "req-collusion-demo-001" });
    expect(newScopeCursorAttempts).toBe(1);
    const restored = new URL(searchURLs(fetchMock).at(-1)!, window.location.origin).searchParams;
    expect(restored.has("cursor")).toBe(false); expect(restored.get("as_of")).toBe(asOf);
    await user.click(screen.getByRole("button", { name: "req-collusion-demo-001" }));
    await user.click(await screen.findByRole("button", { name: "进入完整执行详情" }));
    await screen.findByRole("button", { name: "← 返回运行与执行" });
    expect(screen.getByRole("link", { name: /← 返回运行与执行的原页面/ }).getAttribute("href")).toContain("selected=" + executionID);
  });

  it("shows chronology separately from declared causality and exposes allowlisted source detail", async () => {
    const fetchMock = installAPI();
    window.history.replaceState(null, "", `/executions/${executionID}?as_of=${encodeURIComponent(asOf)}`);
    render(<App />);

    expect(await screen.findByRole("heading", { name: "执行路径调查" })).toBeTruthy();
    const journey = await screen.findByRole("list", { name: "执行路径摘要" });
    for (const label of ["发起者", "动作", "经过系统", "终点"]) {
      expect(within(journey).getByText(label)).toBeTruthy();
    }
    expect(within(journey).getByText("发起 AI 执行请求")).toBeTruthy();
    expect(within(journey).getByText("只使用来源声明父子边")).toBeTruthy();
    const crossExecutionJourney = await screen.findByRole("list", { name: "跨执行数据流路径" });
    expect(within(crossExecutionJourney).getAllByRole("button")).toHaveLength(3);
    expect(screen.getByRole("heading", { name: "跨系统执行路径" })).toBeTruthy();
    expect(screen.getByText("agent-orchestrator → policy-router → model-provider")).toBeTruthy();
    expect(screen.getByText("3 次执行")).toBeTruthy();
    expect(screen.getByText(/本次执行显式因果边/)).toBeTruthy();
    expect(screen.getByText("本次执行步骤／缺口")).toBeTruthy();
    fireEvent.click(await screen.findByRole("button", { name: /^时间线/ }));
    const timeline = await screen.findByRole("list", { name: "按时间升序排列的证据事件" });
    const nodes = within(timeline).getAllByRole("button");
    expect(nodes).toHaveLength(6);
    expect(nodes[0]?.textContent).toContain("路由已选择");
    expect(nodes[1]?.textContent).toContain("上游模型调用已尝试");
    expect(nodes[2]?.textContent).toContain("retry.attempt.scheduled");
    expect(nodes[2]?.textContent).toContain("decision: allowed");
    expect(nodes[3]?.textContent).toContain("fallback.route.selected");
    expect(nodes[3]?.textContent).toContain("decision: selected");
    expect(nodes[4]?.textContent).toContain("上游响应已接受");
    expect(nodes[5]?.textContent).toContain("检测到证据缺口");
    expect(within(timeline).getAllByText("声明父节点")).toHaveLength(4);
    expect(within(timeline).getByText("未解析；不推断因果")).toBeTruthy();

    expect(await screen.findByText("sha256:event-safe-demo")).toBeTruthy();
    expect(screen.getByText("xinsight.evidence.v1")).toBeTruthy();
    expect(screen.getByText("F2")).toBeTruthy();
    expect(screen.getByText("CNY 0.000007 (7 micro-CNY)")).toBeTruthy();
    expect(screen.getByText("精确成本（micro-CNY）")).toBeTruthy();
    expect(screen.getByText("provider_response_not_captured")).toBeTruthy();
    expect(screen.getByText("候选原因（未确认）")).toBeTruthy();
    expect(screen.queryByText("TOP-SECRET-HOSTILE-PROMPT")).toBeNull();
    expect(document.body.textContent).not.toContain("is_stream");
    expect(document.body.textContent).not.toContain("route_index");
    expect(fetchMock.mock.calls.map(([input]) => String(input)).join("\n")).not.toContain("raw");
  });

  it("exposes every visible branch beyond the path summary without adding a causal edge", async () => {
    const branch = { ...traceJourney.nodes[2], event_id: "44444444-4444-4444-8444-444444444444", execution_id: "xe1_demo_parallel_branch", service_name: "parallel-provider", span_id: "4444444444444444", parent_span_id: traceJourney.nodes[0].span_id };
    installAPI({ traceJourney: { ...traceJourney, nodes: [...traceJourney.nodes, branch], integrity: { ...traceJourney.integrity, execution_count: 4, declared_parent_count: 3, resolved_parent_count: 3 }, edges: [...traceJourney.edges, { ...traceJourney.edges[0], from_event_id: traceJourney.nodes[0].event_id, to_event_id: branch.event_id }, { ...traceJourney.edges[0], from_event_id: branch.event_id, to_event_id: "55555555-5555-4555-8555-555555555555" }] } });
    window.history.replaceState(null, "", `/executions/${executionID}?as_of=${encodeURIComponent(asOf)}`);
    render(<App />);
    fireEvent.click(await screen.findByText("全部可见分支与关系（4 个节点）"));
    const graph = screen.getByRole("group", { name: "跨系统全部可见节点与声明关系" });
    expect(graph.querySelectorAll("svg [role=button]")).toHaveLength(4);
    expect(within(graph).getAllByLabelText("来源声明的跨系统关系")).toHaveLength(3);
    const mobile = graph.querySelector<HTMLDivElement>(".graph-accessible-list")!;
    expect(within(mobile).getAllByRole("button")).toHaveLength(4);
    expect(within(mobile).getAllByRole("listitem")).toHaveLength(3);
    expect(graph.textContent).not.toContain("55555555-5555-4555-8555-555555555555");
    fireEvent.click(within(mobile).getByRole("button", { name: /parallel-provider/ }));
    expect(window.location.pathname).toBe(`/executions/${branch.execution_id}`);
    expect(new URLSearchParams(window.location.search).get("as_of")).toBe(asOf);
  });

  it("shows observed trace nodes without inventing arrows when no parent edge resolves", async () => {
    installAPI({
      traceJourney: {
        ...traceJourney,
        edges: [],
        integrity: {
          ...traceJourney.integrity,
          resolved_parent_count: 0,
          unresolved_parent_count: 2,
          visible_parent_resolution_complete: false,
        },
      },
    });
    window.history.replaceState(null, "", `/executions/${executionID}?as_of=${encodeURIComponent(asOf)}`);
    render(<App />);

    const crossExecutionJourney = await screen.findByRole("list", { name: "跨执行数据流路径" });
    expect(within(crossExecutionJourney).queryAllByLabelText("来源声明父子关系")).toHaveLength(0);
    expect(within(crossExecutionJourney).getAllByLabelText("未声明节点关系")).toHaveLength(2);
    expect(screen.getByText("agent-orchestrator · policy-router · model-provider")).toBeTruthy();
    expect(screen.getByText("可见父子关系未闭合")).toBeTruthy();
  });

  it("shows unique call-log success while retaining the failed attempt timeline", async () => {
    installAPI({ recoveredSuccess: true });
    window.history.replaceState(null, "", `/executions/${executionID}?as_of=${encodeURIComponent(asOf)}`);
    render(<App />);

    expect(await screen.findByRole("heading", { name: "执行路径调查" })).toBeTruthy();
    expect((await screen.findAllByText("成功")).length).toBeGreaterThan(0);
    fireEvent.click(await screen.findByRole("button", { name: /^时间线/ }));
    const timeline = await screen.findByRole("list", { name: "按时间升序排列的证据事件" });
    const nodes = within(timeline).getAllByRole("button");
    expect(nodes[1]?.textContent).toContain("上游模型调用已尝试");
    expect(nodes[1]?.textContent).toContain("失败");
    expect(nodes[1]?.textContent).toContain("failure");
    expect(nodes[2]?.textContent).toContain("retry.attempt.scheduled");
    expect(nodes[3]?.textContent).toContain("fallback.route.selected");
  });

  it("adds a fixed Evidence citation and explicit execution Gaps to an open Case", async () => {
    const fetchMock = installAPI({ openCase: true });
    const user = userEvent.setup();
    window.history.replaceState(null, "", `/executions/${executionID}?as_of=${encodeURIComponent(asOf)}`);
    render(<App />);

    expect(await screen.findByRole("heading", { name: "执行路径调查" })).toBeTruthy();
    expect(await screen.findByLabelText("目标案件")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "引用证据与相关缺口" }));
    expect(await screen.findByText("已加入案件；证据本体未被修改。")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: /单独引用全部 1 个执行缺口/ }));
    expect(await screen.findByText("全部执行缺口已作为限制加入案件。")).toBeTruthy();

    const mutationCalls = fetchMock.mock.calls.filter(([input]) =>
      String(input).startsWith(`/api/v1/cases/${openCase.id}/`),
    );
    expect(mutationCalls.map(([input]) => String(input))).toEqual([
      `/api/v1/cases/${openCase.id}/executions`,
      `/api/v1/cases/${openCase.id}/evidence`,
      `/api/v1/cases/${openCase.id}/executions`,
      `/api/v1/cases/${openCase.id}/gaps`,
    ]);
    for (const [, init] of mutationCalls) {
      expect(init?.headers).toMatchObject({
        "X-CSRF-Token": investigatorSession.csrf_token,
      });
    }
  });

  it("shows a fixed discovery run with sample provenance and explicit abstention", async () => {
    const fetchMock = installAPI({ discovery: true });
    window.history.replaceState(null, "", "/discovery");
    render(<App />);

    expect(await screen.findByRole("heading", { name: "可审查的假设，不是系统事实" })).toBeTruthy();
    expect(await screen.findByText("Observed 4 execution records; minimum is 100.")).toBeTruthy();
    await userEvent.click(screen.getByText("限制与方法"));
    expect(screen.getByText("100")).toBeTruthy();
    expect(screen.getByText("xinsight.discovery.deterministic.v1")).toBeTruthy();
    expect(screen.getByText(`sha256:${"a".repeat(64)}`)).toBeTruthy();
    const alertCall = fetchMock.mock.calls.find(([input]) => String(input).startsWith("/api/v1/alerts?"));
    expect(alertCall).toBeDefined();
    expect(new URL(String(alertCall![0]), "https://x-insight.local").searchParams.get("as_of")).toBe(asOf);
  });

  it.each([
    ["open", "进行中"], ["sealed", "已封存"], ["archived", "已归档"], ["future_status", "状态待确认"],
  ])("shows the exact case state %s without assuming sealing", async (status, label) => {
    installAPI({ openCase: true, caseDetail: true, caseStatus: status });
    window.history.replaceState(null, "", `/cases/${openCase.id}`);
    render(<App />);
    expect(await screen.findByText(`调查案件 · ${label}`)).toBeTruthy();
    const raw = screen.getByText(status, { selector: "code" });
    const detail = raw.closest("details")!;
    expect(detail.open).toBe(false);
    fireEvent.click(within(detail).getByText("案件状态代码"));
    expect(detail.open).toBe(true);
    expect(raw.textContent).toBe(status);
    if (status !== "sealed") expect(screen.queryByText("调查案件 · 已封存")).toBeNull();
  });

  it("keeps a human-authored Finding as a hypothesis unless the investigator explicitly chooses unknown", async () => {
    const fetchMock = installAPI({ openCase: true, caseDetail: true });
    const user = userEvent.setup();
    window.history.replaceState(null, "", `/cases/${openCase.id}`);
    render(<App />);

    expect(await screen.findByRole("heading", { name: openCase.title })).toBeTruthy();
    expect(screen.getAllByText(/^调查发现 /)).toHaveLength(2);
    expect(screen.getByText("原始分类：假设")).toBeTruthy();
    expect(screen.getByText("hypothesis", { selector: "code" }).closest("details")?.open).toBe(false);
    expect(screen.getByText(/请勿粘贴原始提示词、模型回复、访问凭据/)).toBeTruthy();
    expect(screen.getByText(/Alert alert-fixture/)).toBeTruthy();
    await user.click(screen.getByText("添加调查发现", { selector: "summary" }));
    expect(screen.getByText("手工陈述不会自动成为有证据支持的判断或按确定规则推导的结论。")).toBeTruthy();
    expect((screen.getByLabelText("分类") as HTMLSelectElement).value).toBe("hypothesis");
    await user.type(screen.getByLabelText("标题"), "Suspected fallback regression");
    await user.type(screen.getByLabelText("陈述"), "The bounded evidence suggests a change, but does not prove causation.");
    await user.click(screen.getByRole("button", { name: "添加调查发现" }));

    await waitFor(() => {
      const mutation = fetchMock.mock.calls.find(([input]) =>
        String(input) === `/api/v1/cases/${openCase.id}/findings`,
      );
      expect(mutation).toBeDefined();
      expect(JSON.parse(String(mutation![1]?.body))).toMatchObject({
        classification: "hypothesis",
      });
    });
  });

  it("renders unproved visible comparison values as Partial instead of observed", async () => {
    installAPI({ comparison: true });
    const user = userEvent.setup();
    window.history.replaceState(null, "", "/compare");
    render(<App />);

    const selectors = await screen.findAllByRole("group");
    expect(selectors).toHaveLength(2);
    await user.type(within(selectors[0]!).getByLabelText("Execution ID"), executionID);
    await user.type(within(selectors[0]!).getByLabelText("as_of (UTC RFC3339)"), asOf);
    await user.type(within(selectors[1]!).getByLabelText("Execution ID"), "xe1_demo_right");
    await user.type(within(selectors[1]!).getByLabelText("as_of (UTC RFC3339)"), asOf);
    await user.click(screen.getByRole("button", { name: "比较固定快照" }));

    const result = await screen.findByRole("table", { name: "Left 与 Right 的同维度值" });
    expect(within(result).getByText("Partial")).toBeTruthy();
    expect(within(result).getByText("Missing")).toBeTruthy();
    expect(within(result).getAllByText("unavailable")).toHaveLength(2);
  });

  it("keeps a partially unavailable Inbox useful without presenting failed reads as empty", async () => {
    installAPI({ savedViewFailure: true });
    window.history.replaceState(null, "", "/inbox");
    render(<App />);

    expect(await screen.findByText(/部分区域暂不可用：views/)).toBeTruthy();
    expect(screen.getByRole("button", { name: /req-collusion-demo-001/ })).toBeTruthy();
    expect(screen.getByText("Saved Views 暂不可用；没有把读取失败显示为空列表。")).toBeTruthy();
  });

  it("requires an explicit confirmation before linking an Alert to a Case", async () => {
    const fetchMock = installAPI({ discovery: true, discoveryAlert: true, openCase: true });
    const user = userEvent.setup();
    window.history.replaceState(null, "", "/discovery");
    render(<App />);

    const selector = await screen.findByLabelText("目标 Case");
    await user.selectOptions(selector, openCase.id);
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith("/case"))).toBe(false);
    await user.click(screen.getByRole("button", { name: "确认关联" }));

    await waitFor(() => {
      const mutation = fetchMock.mock.calls.find(([input]) =>
        String(input) === "/api/v1/alerts/alert-fixture-1/case",
      );
      expect(mutation).toBeDefined();
      expect(mutation![1]?.headers).toMatchObject({
        "X-CSRF-Token": investigatorSession.csrf_token,
      });
      expect(JSON.parse(String(mutation![1]?.body))).toEqual({ case_id: openCase.id });
    });
    expect(await screen.findByRole("button", { name: "打开 Case →" })).toBeTruthy();
  });

  it("requires a second action before revoking a Service identity", async () => {
    const fetchMock = installAPI({ platformAdmin: true, serviceIdentity: true });
    const user = userEvent.setup();
    window.history.replaceState(null, "", "/access/service-identities");
    render(<App />);

    await user.click(await screen.findByRole("button", { name: "准备撤销" }));
    expect(screen.getByText("撤销后该 credential 立即失效。此操作不会删除历史 Evidence。")).toBeTruthy();
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith("/revoke"))).toBe(false);
    await user.click(screen.getByRole("button", { name: "确认撤销" }));

    await waitFor(() => {
      const mutation = fetchMock.mock.calls.find(([input]) =>
        String(input) === `/api/v1/service-identities/${serviceIdentity.id}/revoke`,
      );
      expect(mutation).toBeDefined();
      expect(mutation![1]?.headers).toMatchObject({
        "X-CSRF-Token": platformAdminSession.csrf_token,
      });
      expect(JSON.parse(String(mutation![1]?.body))).toEqual({
        reason: "revoked_by_platform_admin",
      });
    });
    expect(await screen.findByText("revoked")).toBeTruthy();
  });

  it("treats a legacy null projection gap list as empty instead of crashing", async () => {
    installAPI({ recoveredSuccess: true, nullProjectionGaps: true });
    window.history.replaceState(null, "", `/executions/${executionID}?as_of=${encodeURIComponent(asOf)}`);
    render(<App />);

    expect(await screen.findByRole("heading", { name: "执行路径调查" })).toBeTruthy();
    expect(await screen.findByRole("heading", { name: "证据覆盖与完整性" }, asyncResultWait)).toBeTruthy();
    expect(screen.getByText("演示数据 · 合成资料")).toBeTruthy();
  });

  it("keeps source-filter failure degraded while showing an honest empty result", async () => {
    installAPI({ sourceFailure: true, emptySearch: true });
    render(<App />);

    expect(await screen.findByText("当前条件下没有 execution")).toBeTruthy();
    expect(screen.getByText(/来源选项暂时不可用/)).toBeTruthy();
    expect(screen.getByText(/空结果不等于来源完整/)).toBeTruthy();
  });

  it("renders a fresh clean baseline as zero historical executions without claiming health", async () => {
    installAPI({ emptySearch: true });
    render(<App />);

    expect(await screen.findByText("当前条件下没有 execution")).toBeTruthy();
    expect(screen.getByText(/空结果不等于来源完整/)).toBeTruthy();
    expect(screen.queryByText("req-collusion-demo-001")).toBeNull();
  });

  it("renders the only post-reset Connector as inactive with a monotonic empty checkpoint", async () => {
    installAPI({
      connectors: {
        items: [{
          connector_key: "unirouter:clean-baseline",
          source_system_id: "11111111-1111-1111-1111-111111111111",
          source_key: "source-system:11111111-1111-1111-1111-111111111111",
          source_type: "unirouter",
          source_version: "unirouter.execution-evidence.bundle.v2",
          source_display_name: "UniRouter",
          source_status: "active",
          adapter_manifest_id: "22222222-2222-4222-8222-222222222224",
          adapter_id: "adapter-unirouter:22222222-2222-4222-8222-222222222224",
          adapter_version: "unirouter.completed-execution.v1",
          manifest_active: true,
          fidelity_level: "F2",
          ingestion_modes: ["api_import"],
          payload_behavior: "metadata_only_default",
          idempotency_strategy: "source_ref_event_type_source_event_id",
          cursor_strategy: "source_ref",
          trust_state: "inactive",
          checkpoint: {
            epoch: 7,
            cursor_key: "unirouter.completed-execution.v1",
            cursor_present: true,
            cursor_digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            high_watermark_present: true,
            high_watermark_digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
            worker_status: "inactive",
            import_status: "idle",
            consecutive_failures: 0,
            pages_committed: 0,
            items_received: 0,
            items_committed: 0,
            duplicate_noop: 0,
            error_present: false,
          },
          delivery_state_boundary: "mutable_checkpoint_status_not_event_evidence",
        }],
        installations: [],
        installation_control: {
          enabled: false,
          connector_type: "unirouter",
          event_scope: ["model.request.completed"],
          ordinary_sync_requires_assurance: false,
        },
        delivery_state_boundary: "checkpoint status is mutable and excluded from Evidence identity",
      },
    });
    window.history.replaceState(null, "", "/connectors");
    render(<App />);

    expect(await screen.findByRole("heading", { name: "连接器信任状态" })).toBeTruthy();
    expect((await screen.findAllByText("inactive")).length).toBe(2);
    expect(await screen.findByText("7")).toBeTruthy();
    expect(screen.getAllByText("0").length).toBeGreaterThanOrEqual(3);
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "查看该来源的执行" }));
    await screen.findByRole("heading", { name: "执行调查" });
    expect(new URLSearchParams(window.location.search).get("source_system_id")).toBe("11111111-1111-1111-1111-111111111111");
    await user.click(await screen.findByRole("button", { name: "req-collusion-demo-001" }));
    await user.click(await screen.findByRole("button", { name: "进入完整执行详情" }));
    await user.click(await screen.findByRole("link", { name: "← 返回来源与覆盖的原页面" }));
    expect(await screen.findByRole("heading", { name: "连接器信任状态" })).toBeTruthy();
    expect(window.location.pathname).toBe("/connectors");
  });

  it("creates a bounded install invitation and renders abnormal installation state", async () => {
    installAPI({
      operator: true,
      connectors: {
        items: [],
        installations: [{
          schema_version: "xinsight.connector-installation.v1",
          installation_id: "xci_0123456789abcdefghijkl",
          deployment_id: "xid_0123456789abcdefghijkl",
          connector_type: "unirouter",
          requested_event_scope: ["model.request.completed"],
          final_event_scope: ["model.request.completed"],
          project_id: "project.fixture",
          status: "abnormal",
          desired_state: "active",
          observed_state: "abnormal",
          status_reason_code: "permission_denied",
          created_at: asOf,
          updated_at: asOf,
          version: 3,
        }],
        installation_control: {
          enabled: true,
          connector_type: "unirouter",
          event_scope: ["model.request.completed"],
          ordinary_sync_requires_assurance: false,
        },
        delivery_state_boundary: "checkpoint status is mutable and excluded from Evidence identity",
      },
    });
    window.history.replaceState(null, "", "/connectors");
    render(<App />);

    expect(await screen.findByText("permission_denied")).toBeTruthy();
    expect(await screen.findByText("积压状态来自 UniRouter 的明确回报。")).toBeTruthy();
    await userEvent.click(screen.getByText("兼容安装邀请"));
    await userEvent.click(screen.getByRole("button", { name: "创建安装邀请" }));
    expect(await screen.findByText("邀请已就绪")).toBeTruthy();
    expect(screen.getByRole("link", { name: "在 UniRouter 中继续" }).getAttribute("href")).toContain(
      "https://unirouter.local/connectors/x-insight/install#",
    );
  });

  it("maps the stable problem contract to safe Chinese guidance with correlation ID", async () => {
    installAPI({ searchProblem: true });
    render(<App />);

    expect((await screen.findByRole("alert")).textContent).toContain("调查服务暂时过载");
    expect(screen.getByRole("alert").textContent).toContain("corr-safe-503");
    expect(screen.getByRole("button", { name: "重试" })).toBeTruthy();
    expect(document.body.textContent).not.toContain("SELECT executions");
  });

  it("exposes a loading state before the fixed snapshot resolves", async () => {
    let resolveSearch!: (value: ExecutionSearchResponse) => void;
    const pendingSearch = new Promise<ExecutionSearchResponse>((resolve) => {
      resolveSearch = resolve;
    });
    installAPI({ pendingSearch });
    render(<App />);

    expect(await screen.findByText("正在查询 executions")).toBeTruthy();
    resolveSearch(searchResponse);
    expect(await screen.findByRole("button", { name: "req-collusion-demo-001" }, asyncResultWait)).toBeTruthy();
  });

  it("keeps unauthenticated users on product login and enters investigation after login", async () => {
    const fetchMock = installAPI({ sessionUnauthenticated: true });
    const user = userEvent.setup();
    window.history.replaceState(null, "", "/login");
    render(<App />);

    expect(await screen.findByRole("heading", { name: "登录工作台" })).toBeTruthy();
    await user.type(screen.getByLabelText("用户名"), "investigator.one");
    await user.type(screen.getByLabelText("密码"), "test password");
    await user.click(screen.getByRole("button", { name: "登录" }));

    expect(await screen.findByRole("heading", { name: "运行态势" }, asyncResultWait)).toBeTruthy();
    expect(await screen.findByText("req-collusion-demo-001", {}, asyncResultWait)).toBeTruthy();
    expect(window.location.pathname).toBe("/inbox");
    const loginCall = fetchMock.mock.calls.find(([input]) => String(input) === "/api/v1/access/login");
    expect(loginCall).toBeDefined();
    expect(loginCall?.[1]?.credentials).toBe("same-origin");
    expect(String(loginCall?.[1]?.body)).toContain(`"username":"investigator.one"`);
    expect(document.body.textContent).not.toContain("test password");
  });

  it("routes a workbench-only session to its authorized home without probing Evidence pages", async () => {
    const fetchMock = installAPI({ workbench: true });
    window.history.replaceState(null, "", "/inbox");
    render(<App />);

    expect(await screen.findByRole("heading", { name: "总览" })).toBeTruthy();
    expect(window.location.pathname).toBe("/inbox");
    expect(screen.queryByText(workbenchSession.session.subject_id)).toBeNull();
    expect(screen.queryByText(workbenchSession.session.role)).toBeNull();
    expect(screen.queryByText("管理")).toBeNull();
    expect(screen.queryByRole("link", { name: /运行态势/ })).toBeNull();
    expect(fetchMock.mock.calls.some(([input]) => String(input).startsWith("/api/v1/posture"))).toBe(false);
    await waitFor(() => expect(fetchMock.mock.calls.some(([input]) => String(input) === "/api/v1/workbench/readings")).toBe(true));
    expect(fetchMock.mock.calls.some(([input]) => String(input).startsWith("/api/v1/posture"))).toBe(false);
  });

  it("keeps an account without a scope on the access page and reports failed logout", async () => {
    const session: AccessSessionResponse = {
      ...investigatorSession,
      scope_revision: "no_authorized_scope",
      session: {
        ...investigatorSession.session,
        role: "workbench_investigation_reader",
        permissions: ["investigation.scoped.read", "workbench.summary.read"],
      },
    };
    const fetchMock = installAPI({ responseFor: url => {
      if (url === "/api/v1/access/session") return jsonResponse(session);
      if (url === "/api/v1/access/logout") return problemResponse(503, "unavailable", "logout-failed");
    }});
    const user = userEvent.setup();
    window.history.replaceState(null, "", "/workbench/cost");
    render(<App />);

    expect(await screen.findByRole("heading", { name: "当前没有可访问的资料范围" })).toBeTruthy();
    expect(screen.queryByRole("navigation")).toBeNull();
    expect(fetchMock.mock.calls.some(([input]) => /\/api\/v1\/(?:workbench|posture|executions)/.test(String(input)))).toBe(false);
    await user.click(screen.getByRole("button", { name: "退出登录" }));
    expect((await screen.findByRole("alert")).textContent).toContain("退出失败；当前账户仍保持登录。");
    expect(screen.getByRole("heading", { name: "当前没有可访问的资料范围" })).toBeTruthy();
  });

  it("disables credential entry and never calls login from a non-HTTPS origin", async () => {
    const environment = globalThis as typeof globalThis & {
      jsdom?: { reconfigure(options: { url: string }): void };
    };
    environment.jsdom?.reconfigure({ url: "http://x-insight.local/login" });
    const fetchMock = installAPI({ sessionUnauthenticated: true });
    render(<App />);

    expect(await screen.findByText(/非 HTTPS 入口已禁用登录/)).toBeTruthy();
    expect((screen.getByLabelText("用户名") as HTMLInputElement).disabled).toBe(true);
    expect((screen.getByLabelText("密码") as HTMLInputElement).disabled).toBe(true);
    const submit = screen.getByRole("button", { name: "登录" }) as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    fireEvent.submit(submit.closest("form")!);
    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([input]) => String(input) === "/api/v1/access/login")).toBe(false);
    });
  });

  it("shows operator-only Session metadata and sends CSRF on revoke", async () => {
    const fetchMock = installAPI({ operator: true });
    const user = userEvent.setup();
    window.history.replaceState(null, "", "/access/sessions");
    render(<App />);

    expect(await screen.findByRole("heading", { name: "Session 管理" })).toBeTruthy();
    expect(await screen.findByText("xs_0123456789abcdef")).toBeTruthy();
    await user.click(screen.getAllByRole("button", { name: "撤销 Session" })[0]);
    await waitFor(() => {
      const revokeCall = fetchMock.mock.calls.find(([input]) => String(input).endsWith("/revoke"));
      expect(revokeCall?.[1]?.headers).toMatchObject({
        "X-CSRF-Token": operatorSession.csrf_token,
      });
    });
    expect(screen.getAllByRole("link", { current: "page" })).toHaveLength(1);
    expect(screen.getByRole("link", { name: /Access Sessions/ }).getAttribute("aria-current")).toBe("page");
    expect(screen.getByRole("link", { name: /运行与执行/ }).getAttribute("aria-current")).toBeNull();
  });

  it("treats successful self-revoke as immediate local logout", async () => {
    installAPI({ operator: true });
    const user = userEvent.setup();
    window.history.replaceState(null, "", "/access/sessions");
    render(<App />);

    const sessionList = await screen.findByRole("list", { name: "活动 Sessions" });
    const currentSubject = await within(sessionList).findByText(operatorSession.session.subject_id);
    const currentItem = currentSubject.closest("li");
    expect(currentItem).toBeTruthy();
    await user.click(within(currentItem!).getByRole("button", { name: "撤销 Session" }));
    expect(await screen.findByRole("heading", { name: "登录工作台" })).toBeTruthy();
    expect(window.location.pathname).toBe("/login");
  });

  it("logs out on success and treats logout 401 as global session invalidation", async () => {
    for (const logoutUnauthorized of [false, true]) {
      cleanup();
      window.history.replaceState(null, "", "/executions");
      installAPI({ logoutUnauthorized });
      const user = userEvent.setup();
      render(<App />);

      expect(await screen.findByRole("button", { name: "退出登录" })).toBeTruthy();
      await user.click(screen.getByRole("button", { name: "退出登录" }));
      expect(await screen.findByRole("heading", { name: "登录工作台" })).toBeTruthy();
      expect(window.location.pathname).toBe("/login");
      expect(screen.queryByText("退出失败；当前账户仍保持登录。")).toBeNull();
    }
  });
});

interface APIOptions {
  responseFor?: (url: string, init?: RequestInit) => Response | Promise<Response> | undefined;
  sourceFailure?: boolean;
  emptySearch?: boolean;
  searchProblem?: boolean;
  pendingSearch?: Promise<ExecutionSearchResponse>;
  recoveredSuccess?: boolean;
  nullProjectionGaps?: boolean;
  sessionUnauthenticated?: boolean;
  operator?: boolean;
  platformAdmin?: boolean;
  workbench?: boolean;
  openCase?: boolean;
  caseDetail?: boolean;
  caseStatus?: string;
  discovery?: boolean;
  discoveryAlert?: boolean;
  comparison?: boolean;
  serviceIdentity?: boolean;
  savedViewFailure?: boolean;
  logoutUnauthorized?: boolean;
  traceJourney?: TraceJourney;
  posture?: PostureSnapshot;
  connectors?: ConnectorListResponse;
}

function installAPI(options: APIOptions = {}) {
  let loggedIn = !options.sessionUnauthenticated;
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const override = options.responseFor?.(url, init);
    if (override) return override;
    if (url === "/api/v1/access/session") {
      if (!loggedIn) {
        return problemResponse(401, "authentication_required", "corr-access-401");
      }
      return jsonResponse(
        options.platformAdmin
          ? platformAdminSession
          : options.operator
            ? operatorSession
            : options.workbench
              ? workbenchSession
              : investigatorSession,
      );
    }
    if (url === "/api/v1/access/login") {
      loggedIn = true;
      return jsonResponse(investigatorSession);
    }
    if (url === "/api/v1/access/logout") {
      if (options.logoutUnauthorized) {
        return problemResponse(401, "authentication_required", "corr-logout-401");
      }
      return new Response(null, { status: 204 });
    }
    if (url === "/api/v1/workbench/readings") {
      return jsonResponse({
        reading_id: "xwr_0123456789abcdefghijkl",
        contract_version: "xinsight.workbench.v1",
        metric_version: "xinsight.workbench.cost.v1",
        period: { start_inclusive: "2026-07-28T08:00:00Z", end_exclusive: "2026-07-29T08:00:00Z", display_timezone: "UTC" },
        cutoff: "2026-07-29T08:00:00Z",
        expires_at: "2026-07-29T08:30:00Z",
        money_basis: "gateway_charged",
        money_title: "网关扣费",
        currency: "CNY",
        membership_ref: `sha256:${"1".repeat(64)}`,
        interpretation_revision: `sha256:${"2".repeat(64)}`,
        authorization_revision: `sha256:${"3".repeat(64)}`,
        dimensions: ["model", "source_project", "cost_project"],
        coverage: { status: "period_completeness_unknown", period_completeness: "unknown", projection_complete_for_members: true, known_items: 0, gaps: ["source_period_completeness_not_asserted"] },
      }, { status: 201 });
    }
    if (/^\/api\/v1\/workbench\/readings\/[^/]+\/summary$/.test(url)) {
      return jsonResponse({ reading_id: "xwr_0123456789abcdefghijkl", request_count: 0, gateway_charged_micros: "0", calculated_reference_micros: "0", selected_amount_micros: "0", money_basis: "gateway_charged", currency: "CNY", request_total_complete: false, amount_complete: false, coverage: { status: "period_completeness_unknown", period_completeness: "unknown", projection_complete_for_members: true, known_items: 0, gaps: ["source_period_completeness_not_asserted"] } });
    }
    if (url.includes("/api/v1/workbench/readings/") && url.includes("/contributions?")) {
      return jsonResponse({ reading_id: "xwr_0123456789abcdefghijkl", dimension: "source_project", mode: "top_n", rows: [], other_known_micros: "0", unassigned_micros: "0", unassigned_scope: "reading_constant", denominator_micros: "0", is_last_page: true, authorized_group_count: 0 });
    }
    if (url.includes("/api/v1/workbench/readings/") && url.includes("/series?")) {
      return jsonResponse({ reading_id: "xwr_0123456789abcdefghijkl", metric: "amount", unit: "micros", bucket_seconds: 3600, points: [], coverage: { status: "period_completeness_unknown", period_completeness: "unknown", projection_complete_for_members: true, known_items: 0, gaps: ["source_period_completeness_not_asserted"] } });
    }
    if (/^\/api\/v1\/workbench\/readings\/[^/]+\/sources$/.test(url)) {
      return jsonResponse({ reading_id: "xwr_0123456789abcdefghijkl", items: [] });
    }
    if (url === "/api/v1/workbench/saved-filters") {
      return jsonResponse({ items: [] });
    }
    if (url === "/api/v1/access/sessions") {
      return jsonResponse({ items: [investigatorSession.session, operatorSession.session] });
    }
    if (url.startsWith("/api/v1/posture?")) {
      return jsonResponse(options.posture ?? postureResponse);
    }
    if (url.startsWith("/api/v1/access/sessions/") && url.endsWith("/revoke")) {
      if (init?.headers && !(init.headers as Record<string, string>)["X-CSRF-Token"]) {
        return problemResponse(403, "csrf_validation_failed", "corr-access-403");
      }
      return new Response(null, { status: 204 });
    }
    if (url === "/api/v1/cases") {
      return jsonResponse({ items: options.openCase ? [openCase] : [] });
    }
    if (options.caseDetail && url === `/api/v1/cases/${openCase.id}`) {
      const findingID = "xcf_0123456789abcdefghijkl";
      return jsonResponse({
        case: { ...openCase, status: options.caseStatus ?? openCase.status },
        executions: [],
        findings: [{
          id: findingID,
          case_id: openCase.id,
          title: "Bounded hypothesis",
          statement: "The cited evidence supports investigation, not causation.",
          classification: "hypothesis",
          created_by_subject: "xs_0123456789abcdef",
          created_at: asOf,
        }],
        evidence: [{
          case_id: openCase.id,
          finding_id: findingID,
          evidence_event_id: "evt-fixture-1",
          event_hash: `sha256:${"1".repeat(64)}`,
          source_ref: "fixture:evidence:1",
          relation: "supports",
          linked_by_subject: "xs_0123456789abcdef",
          linked_at: asOf,
        }],
        gaps: [{
          case_id: openCase.id,
          finding_id: findingID,
          gap_id: "gap-fixture-1",
          gap_type: "missing_parent",
          affected_claim: "The parent action cannot be proven.",
          relation: "limits",
          linked_by_subject: "xs_0123456789abcdef",
          linked_at: asOf,
        }],
        alerts: [{
          alert_id: "alert-fixture-1",
          case_id: openCase.id,
          alert_state: "open",
          linked_by_subject: "xs_0123456789abcdef",
          linked_at: asOf,
        }],
        annotations: [],
      });
    }
    if (options.openCase && url.startsWith(`/api/v1/cases/${openCase.id}/`)) {
      if (init?.method !== "POST") {
        throw new Error(`Unexpected Case mutation method: ${init?.method}`);
      }
      return jsonResponse({ accepted: true });
    }
    if (url.startsWith("/api/v1/discovery/hypotheses?")) {
      return jsonResponse({
        as_of: asOf,
        boundary: "hypotheses are not M0 facts or automatic enforcement",
        items: options.discovery ? [{
          id: `xhy_${"b".repeat(64)}`,
          analysis_run_id: `xdr_${"c".repeat(64)}`,
          analysis_as_of: asOf,
          sample_size: 100,
          input_digest: `sha256:${"a".repeat(64)}`,
          hypothesis_type: "coverage_pattern",
          title: "Coverage pattern",
          statement: "Insufficient bounded evidence for coverage_pattern.",
          status: "abstained",
          evidence_event_ids: [],
          execution_ids: [],
          limitations: ["The deterministic evidence threshold was not met."],
          abstention_reason: "Observed 4 execution records; minimum is 100.",
          method_version: "xinsight.discovery.deterministic.v1",
          generated_at: asOf,
        }] : [],
      });
    }
    if (url.startsWith("/api/v1/alerts?")) {
      return jsonResponse({
        as_of: asOf,
        items: options.discoveryAlert ? [{
          id: "alert-fixture-1",
          source_system_id: "src-fixture-01",
          title: "Observed provider failure",
          severity: "warning",
          state: "open",
          observed_at: asOf,
        }] : [],
      });
    }
    if (url === "/api/v1/alerts/alert-fixture-1/case") {
      return jsonResponse({
        alert_id: "alert-fixture-1",
        case_id: openCase.id,
        alert_state: "open",
        linked_by_subject: "xs_0123456789abcdef",
        linked_at: asOf,
      });
    }
    if (options.comparison && url.startsWith("/api/v1/compare?")) {
      return jsonResponse({
        left: { execution_id: executionID },
        right: { execution_id: "xe1_demo_right" },
        left_as_of: asOf,
        right_as_of: asOf,
        dimensions: [
          {
            dimension: "provider_path",
            left: { state: "partial", value: "router → provider-a", unit: "ordered_path" },
            right: { state: "observed", value: "router → provider-b", unit: "ordered_path" },
            relation: "unavailable",
          },
          {
            dimension: "cost_micros",
            left: { state: "missing" },
            right: { state: "observed", value: "17", unit: "micro-CNY" },
            relation: "unavailable",
          },
        ],
      });
    }
    if (url === "/api/v1/connectors") {
      return jsonResponse(options.connectors ?? {
        items: [],
        installations: [],
        installation_control: {
          enabled: false,
          connector_type: "unirouter",
          event_scope: ["model.request.completed"],
          ordinary_sync_requires_assurance: false,
        },
        delivery_state_boundary: "checkpoint status is mutable and excluded from Evidence identity",
      });
    }
    if (url === "/api/v1/connectors/invitations") {
      if (init?.method !== "POST" || !(init.headers as Record<string, string>)["X-CSRF-Token"]) {
        return problemResponse(403, "csrf_validation_failed", "corr-connector-403");
      }
      return jsonResponse({
        invitation: {
          schema_version: "xinsight.connector-installation-invitation.v1",
          invitation_id: "xii_0123456789abcdefghijkl",
          installation_id: "xci_0123456789abcdefghijkl",
          deployment_id: "xid_0123456789abcdefghijkl",
          audience: "unirouter",
          xinsight_origin: "https://xinsight.local",
          requested_event_scope: ["model.request.completed"],
          expires_at: "2026-08-11T03:15:00Z",
          install_url: "https://unirouter.local/connectors/x-insight/install#invitation=opaque",
        },
        installation: {
          schema_version: "xinsight.connector-installation.v1",
          installation_id: "xci_0123456789abcdefghijkl",
          deployment_id: "xid_0123456789abcdefghijkl",
          connector_type: "unirouter",
          requested_event_scope: ["model.request.completed"],
          status: "pending",
          desired_state: "active",
          observed_state: "awaiting_consent",
          created_at: asOf,
          updated_at: asOf,
          version: 1,
        },
      });
    }
    if (url === "/api/v1/saved-views") {
      if (options.savedViewFailure) {
        return problemResponse(503, "database_unavailable", "corr-view-503");
      }
      return jsonResponse({ items: [] });
    }
    if (url === "/api/v1/service-identities") {
      return jsonResponse({ items: options.serviceIdentity ? [serviceIdentity] : [] });
    }
    if (url === `/api/v1/service-identities/${serviceIdentity.id}/revoke`) {
      return jsonResponse({
        ...serviceIdentity,
        status: "revoked",
        updated_at: "2026-07-31T08:00:00Z",
        revoked_at: "2026-07-31T08:00:00Z",
        revoke_reason: "revoked_by_platform_admin",
      });
    }
    if (url.startsWith("/api/v1/sources")) {
      if (options.sourceFailure) {
        return problemResponse(503, "database_unavailable", "corr-source-503");
      }
      return jsonResponse(sourcesResponse);
    }
    if (/^\/api\/v1\/executions\?/.test(url)) {
      if (options.searchProblem) {
        return problemResponse(503, "temporarily_overloaded", "corr-safe-503");
      }
      if (options.pendingSearch) {
        return jsonResponse(await options.pendingSearch);
      }
      return jsonResponse(options.emptySearch
        ? emptySearchResponse
        : options.recoveredSuccess
          ? recoveredSearchResponse
          : searchResponse);
    }
    if (url.includes("/investigation?")) {
      const projection = testProjection(Boolean(options.recoveredSuccess));
      return jsonResponse(options.nullProjectionGaps ? { ...projection, gaps: null } : projection);
    }
    if (url.includes("/trace-journey?")) {
      return jsonResponse(options.traceJourney ?? traceJourney);
    }
    if (url.includes("/timeline?")) {
      return jsonResponse(options.recoveredSuccess ? recoveredTimelineResponse : timelineResponse);
    }
    if (url.includes("/gaps?")) {
      return jsonResponse(gapResponse);
    }
    if (url.includes("/events/")) {
      const eventID = decodeURIComponent(url.split("/events/")[1]?.split("?")[0] ?? "event-route");
      return jsonResponse({ ...eventDetail, event_id: eventID });
    }
    if (url.startsWith(`/api/v1/executions/${executionID}`)) {
      return jsonResponse(options.recoveredSuccess ? recoveredExecutionDetail : executionDetail);
    }
    throw new Error(`Unhandled test request: ${url}`);
  });
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function testProjection(recovered: boolean): InvestigationProjection {
  const sourceTimeline = recovered ? recoveredTimelineResponse : timelineResponse;
  const nodes = sourceTimeline.items.map((item) => {
    const kind = item.event_id === "event-route"
      ? "route_selected"
      : item.event_id === "event-provider-a"
        ? "provider_failed"
        : item.event_id === "event-retry"
          ? "retry_scheduled"
          : item.event_id === "event-fallback"
            ? "fallback_selected"
            : item.event_id === "event-provider-b"
              ? "provider_succeeded"
              : "capture_gap";
    return {
      event_id: item.event_id,
      event_type: item.event_type,
      source_event_type: item.event_type,
      kind,
      proof_basis: "fixture_exact_event_type_mapping",
      stage: item.stage,
      severity: item.severity,
      effective_at: item.effective_at,
      declared_parent_event_id: item.declared_parent_event_id,
      provider: item.event_id === "event-provider-a" ? "provider-a" : "provider-b",
      model: "fixture-model",
      requested_model: "fixture-requested-model",
      outcome_status: item.outcome_status,
      status_code: item.event_id === "event-provider-a" ? 502 : 200,
      latency_ms: 120,
      total_tokens: item.event_id === "event-provider-b" ? 31 : undefined,
      cost_micros: item.event_id === "event-provider-b" ? "7" : undefined,
      citation: {
        evidence_event_id: item.event_id,
        source_ref: item.source_ref,
        event_hash: "sha256:event-safe-demo",
        deep_link: `/executions/${encodeURIComponent(summary.execution_id)}?as_of=${encodeURIComponent(asOf)}&evidence=${encodeURIComponent(item.event_id)}`,
      },
    };
  });
  return {
    schema_version: "xinsight.investigation-projection.v1",
    execution: recovered ? recoveredSummary : summary,
    as_of: asOf,
    presentation_mode: "causal_trace",
    summary_claims: recovered
      ? [{
          code: "primary_provider_failed_then_fallback_succeeded",
          evidence_event_ids: ["event-provider-a", "event-retry", "event-fallback", "event-provider-b"],
          derivation_basis: "fixture_declared_parent_path",
        }]
      : [],
    stage_coverage: [
      "identity",
      "routing",
      "provider",
      "retry_fallback",
      "billing",
      "usage",
      "logging",
    ].map((stage) => ({
      stage,
      status: stage === "identity" || stage === "logging" ? "unknown" : "present",
      evidence_event_ids: stage === "identity" || stage === "logging" ? undefined : ["event-route"],
    })),
    nodes,
    edges: [
      { from: "event-route", to: "event-provider-a", edge_basis: "source_declared_parent_span" },
      { from: "event-route", to: "event-retry", edge_basis: "source_declared_parent_span" },
      { from: "event-retry", to: "event-fallback", edge_basis: "source_declared_parent_span" },
    ],
    gaps: gapResponse.items,
    freshness,
    integrity: {
      source_profile_complete: false,
      import_complete: false,
      projection_complete: true,
      node_count: nodes.length,
      total_event_count: nodes.length,
      resolved_parent_count: 3,
      parent_eligible_count: 4,
      unresolved_parent_count: 1,
      causal_complete: false,
      truncated: false,
    },
    completeness: [
      { axis: "source", status: "gap", basis: "fixture source capture is incomplete" },
      { axis: "import", status: "gap", basis: "fixture import is incomplete" },
      { axis: "projection", status: "present", basis: "all fixture events project deterministically" },
      { axis: "causal", status: "gap", basis: "one fixture parent is unresolved" },
    ],
    unprovable_actions: [{
      code: "root_cause_not_provable",
      statement: "The fixture does not support a root-cause claim.",
      gap_ids: gapResponse.items.map((gap) => gap.gap_id),
    }],
  };
}

function searchURLs(fetchMock: ReturnType<typeof vi.fn>): string[] {
  return fetchMock.mock.calls
    .map(([input]) => String(input))
    .filter((url) => /^\/api\/v1\/executions\?/.test(url));
}

function jsonResponse(value: unknown, init: ResponseInit = {}): Response {
  return new Response(JSON.stringify(value), {
    ...init,
    status: init.status ?? 200,
    headers: { "Content-Type": "application/json" },
  });
}

function problemResponse(status: number, code: string, correlationID: string): Response {
  return new Response(
    JSON.stringify({
      type: "https://x-insight.local/problems/investigation",
      title: "SELECT executions internal detail must not render",
      status,
      code,
      correlation_id: correlationID,
      retryable: true,
    }),
    { status, headers: { "Content-Type": "application/problem+json" } },
  );
}

it("keeps runtime as the origin of a linked execution detail", async () => {
 const session={...investigatorSession,session:{...investigatorSession.session,permissions:[...investigatorSession.session.permissions,"workbench.summary.read","workbench.details.read"]}};
 installAPI({responseFor:url=>url==="/api/v1/access/session"?jsonResponse(session):undefined});
 window.history.replaceState({workbenchOrigin:"/workbench/runtime?view=capabilities"},"",`/executions/${executionID}?as_of=${encodeURIComponent(asOf)}`);
 render(<App />);
 const link=await screen.findByRole("link",{name:/← 返回运行与执行的原页面/});
 expect(link.getAttribute("href")).toBe("/workbench/runtime?view=capabilities");
 expect(screen.queryByRole("link",{name:/← 返回费用与使用/})).toBeNull();
});


it("keeps readable investigation labels separate from complete technical references and unknown basis codes",async()=>{
 const projection={...testProjection(false),presentation_mode:"terminal_snapshot",summary_claims:[],edges:[],completeness:[
  {axis:"source",status:"complete",basis:"source_profile_declares_timeline_complete"},
  {axis:"import",status:"incomplete",basis:"future_basis_code"},
 ]};
 installAPI({responseFor:url=>url.includes("/investigation?")?jsonResponse(projection):undefined});
 window.history.replaceState(null,"","/executions/"+executionID+"?as_of="+encodeURIComponent(asOf));
 render(<App/>);await screen.findByRole("heading",{name:"执行路径调查"});
 await screen.findByRole("heading",{name:"终态快照：当前来源没有提供执行链路"});
 const tabs=screen.getByRole("navigation",{name:"执行详情视图"});
 for(const label of ["执行概况","因果图","时间线","证据依据","系统与数据"])expect(within(tabs).getByRole("button",{name:new RegExp("^"+label)})).toBeTruthy();
 const refs=screen.getByText("完整执行引用与状态代码").closest("details")!;
 expect(refs.open).toBe(false);expect(within(refs).getByText(executionID)).toBeTruthy();
 fireEvent.click(within(refs).getByText("完整执行引用与状态代码"));
 expect(refs.open).toBe(true);expect(within(refs).getByText(executionID)).toBeTruthy();
 const known=screen.getByText("source_profile_declares_timeline_complete",{exact:false}).closest("details")!;
 const unknown=screen.getByText("future_basis_code",{exact:false}).closest("details")!;
 expect(known.open).toBe(false);expect(unknown.open).toBe(false);
 expect(screen.getByText("来源声明时间线完整。")).toBeTruthy();
 expect(screen.getByText("完整性依据尚无可读解释，请查看原始代码。")).toBeTruthy();
 fireEvent.click(within(unknown).getByText("完整性代码"));expect(unknown.open).toBe(true);
 expect(within(unknown).getByText(/future_basis_code/)).toBeTruthy();
 expect(screen.getByRole("tab",{name:"事实"})).toBeTruthy();
 expect(new URLSearchParams(window.location.search).get("as_of")).toBe(asOf);
});


it.each([["warning","警告"],["error","错误"]] as const)("retains the %s severity CSS code while displaying %s",async(severity,label)=>{
 const projection=testProjection(false);
 projection.nodes[0]={...projection.nodes[0],severity};
 installAPI({responseFor:url=>url.includes("/investigation?")?jsonResponse(projection):undefined});
 window.history.replaceState(null,"","/executions/"+executionID+"?as_of="+encodeURIComponent(asOf)+"&evidence="+encodeURIComponent(projection.nodes[0].event_id));
 render(<App/>);
 const inspector=await screen.findByRole("region",{name:"节点调查"});
 await waitFor(()=>expect(inspector.querySelector(".severity-chip")?.textContent).toBe(label));
 const badge=inspector.querySelector(".severity-chip")!;
 expect(badge.classList.contains("severity-"+severity)).toBe(true);
 expect(badge.classList.contains("severity-"+label)).toBe(false);
});


it.each([false,true])("keeps fixed evidence capture separate from the explicit content view (deep link: %s)",async(deep)=>{
 const session={...investigatorSession,session:{...investigatorSession.session,permissions:[...investigatorSession.session.permissions,"workbench.summary.read","workbench.details.read"]}};
 const fetchMock=installAPI({responseFor:url=>url==="/api/v1/access/session"?jsonResponse(session):undefined});
 window.history.replaceState({workbenchOrigin:"/workbench/cost?view=usage"},"",`/executions/${executionID}?as_of=${encodeURIComponent(asOf)}&evidence=event-route${deep?"&view=contents":""}`);
 const user=userEvent.setup();render(<App/>);
 await screen.findByText("证据记录的采集模式");
 expect(screen.getByText("仅描述当前固定证据快照，不代表独立内容源的保存或可读状态。")).toBeTruthy();
 expect(screen.queryByText("本页不提供原文查看器")).toBeNull();
 if(!deep)await user.click(screen.getByRole("button",{name:/交互内容.*保存表示与人工复核/}));
 expect(await screen.findByRole("button",{name:"读取相关内容清单"})).toBeTruthy();
 const query=new URLSearchParams(window.location.search);expect(query.get("view")).toBe("contents");expect(query.get("as_of")).toBe(asOf);expect(query.get("evidence")).toBe("event-route");
 expect(window.history.state.workbenchOrigin).toBe("/workbench/cost?view=usage");
 expect(fetchMock.mock.calls.some(([input])=>String(input).startsWith("/api/v1/runtime/"))).toBe(false);
 await user.click(screen.getByRole("button",{name:/系统与数据.*资源、输入与目的地/}));expect(await screen.findByRole("button",{name:"读取相关运行与资源"})).toBeTruthy();
 await user.click(screen.getByRole("button",{name:/交互内容.*保存表示与人工复核/}));expect(await screen.findByRole("button",{name:"读取相关内容清单"})).toBeTruthy();
 expect(new URLSearchParams(window.location.search).get("as_of")).toBe(asOf);
 expect(fetchMock.mock.calls.some(([input])=>String(input).startsWith("/api/v1/runtime/"))).toBe(false);
});

it("counts retained content/resource citations even without evidence or execution links",async()=>{
 installAPI({responseFor:url=>url===`/api/v1/cases/${openCase.id}`?jsonResponse({case:openCase,executions:[],findings:[],evidence:[],gaps:[],contents:[{reference:{scope:"finding",reference:{source_system_id:"source",source_instance:"instance",org_id:"7",project_id:"9",request_id:"request",source_fact_id:"fact",revision:"1",source_sha256:"sha256:manifest",capture_status:"complete",content_ref:"body",expires_at:"2099-01-01T00:00:00Z"},location:{kind:"json_pointer",pointer:"/value"},relation:"supports"}}],resources:[1,2].map(id=>({reference:{scope:"finding",reporter:{source_system_id:"source",source_instance:"instance",org_id:"7",project_id:"9"},target:{source_system_id:"source",source_instance:"instance",org_id:"7",project_id:"9"},request_id:"request",record_ref:`resource-${id}`,revision:1,record_hash:"sha256:record",relation:"supports"}})),alerts:[],annotations:[]}):undefined});
 window.history.replaceState(null,"",`/cases/${openCase.id}`);render(<App/>);
 expect(await screen.findByText("0 条证据 · 0 条缺口 · 2 条资源 · 1 条内容")).toBeTruthy();expect(screen.getByText("已保留复核引用，尚未关联执行。")).toBeTruthy();expect(screen.queryByText(/尚未链接证据/)).toBeNull();
});
