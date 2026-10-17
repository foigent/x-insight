import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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
    expect(screen.getByText("单部署空间")).toBeTruthy();
    expect(screen.getByText("仅元数据 · 不展示原文")).toBeTruthy();
    expect(screen.getByLabelText("访问状态").textContent).toContain("HTTPS · 产品 Session");
    expect(screen.getByRole("link", { name: /产品手册/ }).getAttribute("href")).toBe("/manual/zh-CN/");
    for (const label of ["直接证据", "推导", "声明", "已抑制", "缺口", "未知", "不适用"]) {
      expect(screen.getAllByText(label).length).toBeGreaterThan(0);
    }
    expect(screen.getByText("合成夹具")).toBeTruthy();
    expect(screen.getByText("存在证据缺口")).toBeTruthy();
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
    await user.click(screen.getByRole("link", { name: /Cases/ }));
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

  it("shows chronology separately from declared causality and exposes allowlisted source detail", async () => {
    const fetchMock = installAPI();
    window.history.replaceState(null, "", `/executions/${executionID}?as_of=${encodeURIComponent(asOf)}`);
    render(<App />);

    expect(await screen.findByRole("heading", { name: "执行路径调查" })).toBeTruthy();
    const journey = await screen.findByRole("list", { name: "执行路径摘要" });
    for (const label of ["发起者", "动作", "经过系统", "终点"]) {
      expect(within(journey).getByText(label)).toBeTruthy();
    }
    expect(within(journey).getByText("发起 AI execution 请求")).toBeTruthy();
    expect(within(journey).getByText("只使用来源声明父子边")).toBeTruthy();
    const crossExecutionJourney = await screen.findByRole("list", { name: "跨 execution 数据流路径" });
    expect(within(crossExecutionJourney).getAllByRole("button")).toHaveLength(3);
    expect(screen.getByRole("heading", { name: "跨系统 Trace Journey" })).toBeTruthy();
    expect(screen.getByText("agent-orchestrator → policy-router → model-provider")).toBeTruthy();
    expect(screen.getByText("3 executions")).toBeTruthy();
    expect(screen.getByText(/本 execution 显式因果边/)).toBeTruthy();
    expect(screen.getByText("本 execution 步骤 / Gaps")).toBeTruthy();
    fireEvent.click(await screen.findByRole("button", { name: /^Chronology/ }));
    const timeline = await screen.findByRole("list", { name: "按时间升序排列的 evidence events" });
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

    const crossExecutionJourney = await screen.findByRole("list", { name: "跨 execution 数据流路径" });
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
    fireEvent.click(await screen.findByRole("button", { name: /^Chronology/ }));
    const timeline = await screen.findByRole("list", { name: "按时间升序排列的 evidence events" });
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
    expect(await screen.findByLabelText("目标 Case")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "引用 Evidence 与相关 Gaps" }));
    expect(await screen.findByText("已加入 Case；Evidence 本体未被修改。")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: /单独引用全部 1 个 execution Gaps/ }));
    expect(await screen.findByText("全部 execution Gaps 已作为限制加入 Case。")).toBeTruthy();

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

  it("keeps a human-authored Finding as a hypothesis unless the investigator explicitly chooses unknown", async () => {
    const fetchMock = installAPI({ openCase: true, caseDetail: true });
    const user = userEvent.setup();
    window.history.replaceState(null, "", `/cases/${openCase.id}`);
    render(<App />);

    expect(await screen.findByRole("heading", { name: openCase.title })).toBeTruthy();
    expect(screen.getAllByText(/^Finding /)).toHaveLength(2);
    expect(screen.getByText(/Alert alert-fixture/)).toBeTruthy();
    await user.click(screen.getByText("添加 Finding", { selector: "summary" }));
    expect(screen.getByText("手工陈述不会被自动标为 Evidence-backed 或 deterministic。")).toBeTruthy();
    expect((screen.getByLabelText("分类") as HTMLSelectElement).value).toBe("hypothesis");
    await user.type(screen.getByLabelText("标题"), "Suspected fallback regression");
    await user.type(screen.getByLabelText("陈述"), "The bounded evidence suggests a change, but does not prove causation.");
    await user.click(screen.getByRole("button", { name: "添加 Finding" }));

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
    expect(screen.getByText("Demo data · synthetic")).toBeTruthy();
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
    expect(await screen.findByText("Backlog 只接受 UniRouter agent 的显式健康回执；不会从 X 数据量推断。")).toBeTruthy();
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

    expect(await screen.findByRole("heading", { name: "登录证据调查平面" })).toBeTruthy();
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
    expect(screen.getByRole("link", { name: /Executions/ }).getAttribute("aria-current")).toBeNull();
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
    expect(await screen.findByRole("heading", { name: "登录证据调查平面" })).toBeTruthy();
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
      expect(await screen.findByRole("heading", { name: "登录证据调查平面" })).toBeTruthy();
      expect(window.location.pathname).toBe("/login");
      expect(screen.queryByText("退出失败；当前 Session 仍保持登录。")).toBeNull();
    }
  });
});

interface APIOptions {
  sourceFailure?: boolean;
  emptySearch?: boolean;
  searchProblem?: boolean;
  pendingSearch?: Promise<ExecutionSearchResponse>;
  recoveredSuccess?: boolean;
  nullProjectionGaps?: boolean;
  sessionUnauthenticated?: boolean;
  operator?: boolean;
  platformAdmin?: boolean;
  openCase?: boolean;
  caseDetail?: boolean;
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
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url === "/api/v1/access/session") {
      if (options.sessionUnauthenticated) {
        return problemResponse(401, "authentication_required", "corr-access-401");
      }
      return jsonResponse(
        options.platformAdmin
          ? platformAdminSession
          : options.operator
            ? operatorSession
            : investigatorSession,
      );
    }
    if (url === "/api/v1/access/login") {
      return jsonResponse(investigatorSession);
    }
    if (url === "/api/v1/access/logout") {
      if (options.logoutUnauthorized) {
        return problemResponse(401, "authentication_required", "corr-logout-401");
      }
      return new Response(null, { status: 204 });
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
        case: openCase,
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

function jsonResponse(value: unknown): Response {
  return new Response(JSON.stringify(value), {
    status: 200,
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
