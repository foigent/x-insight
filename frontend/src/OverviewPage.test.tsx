import { beforeEach as beforeLegacyLocale } from "vitest";
import { setLocale as setLegacyLocale } from "./i18n";
// These existing semantic assertions explicitly exercise the Chinese translation.
beforeLegacyLocale(() => setLegacyLocale("zh-CN"));
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import { acceptScopeSession, accessAPI, ApiRequestError, investigationQueueAPI, runtimeAPI, workActivityAPI, workbenchAPI, type AccessSessionResponse, type WorkbenchContributions, type WorkbenchReading, type WorkbenchSummary, type WorkbenchSourceStatuses, type InvestigationQueuePage } from "./api";
import { OverviewPage, overviewMoney, overviewShare, type OverviewContext } from "./OverviewPage";
import type * as Runtime from "./runtime";

const coverage = { status: "scope_limited_unknown", period_completeness: "unknown", projection_complete_for_members: true, known_items: 2, gaps: ["资料不全"] } as const;
const period = { start_inclusive: "2026-09-01T00:00:00Z", end_exclusive: "2026-09-08T00:00:00Z", timezone: "Asia/Shanghai" };
const context: OverviewContext = { hours: 168, contribution: "source_project", request: { contract_version: "xinsight.workbench.v1", page: "cost", metric_version: "xinsight.workbench.cost.v1", period: { ...period, display_timezone: period.timezone }, money_basis: "gateway_charged", currency: "CNY", filters: { source_ids: ["source-visible"], source_projects: ["project-a"], cost_projects: ["cost-a"] } } };
const session = { session: { subject_id: "subject-a", session_ref: "session-a", role: "workbench_reader", permissions: ["workbench.summary.read"] }, csrf_token: "fixture-only" } as AccessSessionResponse;
function reading(id = "reading-a", lifetime = 60000): WorkbenchReading { const now = Date.now(); return { reading_id: id, contract_version: "xinsight.workbench.v1", metric_version: "xinsight.workbench.cost.v1", period, cutoff: new Date(now).toISOString(), expires_at: new Date(now + lifetime).toISOString(), money_basis: "gateway_charged", money_title: "网关已计金额", currency: "CNY", membership_ref: "members-a", authorization_revision: "scope-a", interpretation_revision: "interpret-a", dimensions: ["model", "application", "purpose", "source_project"], coverage: { ...coverage, gaps: [...coverage.gaps] } }; }
const summary = { reading_id: "reading-a", money_basis: "gateway_charged", currency: "CNY", request_count: 2, request_total_complete: false, selected_amount_micros: "0", gateway_charged_micros: "0", calculated_reference_micros: "0", amount_complete: false, purpose_known_requests: 0, actor_known_requests: 0, coverage: { ...coverage, gaps: [...coverage.gaps] } } as WorkbenchSummary;
function contribution(dimension: WorkbenchContributions["dimension"]): WorkbenchContributions { return { reading_id: "reading-a", dimension, mode: "top_n", order_by: dimension === "source_project" ? "amount" : "requests", denominator_requests: 2, unassigned_requests: 0, other_known_requests: 0, rows: [{ ref: `${dimension}-a`, label: `可见${dimension}`, request_count: 2, selected_amount_micros: "0", gateway_charged_micros: "0", calculated_reference_micros: "0", share: { numerator: "0", denominator: "0", unit: "micros" }, drilldown_allowed: true }], other_known_micros: "0", unassigned_micros: "0", unassigned_scope: "reading_constant", denominator_micros: "0", is_last_page: true, authorized_group_count: 1 }; }
function seriesPoint(hour: number, value: string, complete: boolean, endHour = hour + 1) { const start = Date.parse(period.start_inclusive); return { start_inclusive: new Date(start + hour * 3600000).toISOString(), end_exclusive: new Date(start + endHour * 3600000).toISOString(), known_value: value, complete }; }
const runtimeCoverage: Runtime.Coverage = { period_completeness: "unknown", timeline_complete: false, time_basis: "entry", unknown_entry_time_basis: "separate", missing_predecessors: 0, gaps: ["运行资料不全"], streams: [] };
const runtimeReading = (): Runtime.Reading => ({ ...reading("runtime-a"), contract_version: "xinsight.runtime-reading.v1", metric_version: "xinsight.runtime.request-observations.v1", requests_with_entry_time: 3, requests_with_unknown_entry_time: 1, observation_count: 5, coverage: runtimeCoverage });
const runtimeSummary: Runtime.Summary = { reading_id: "runtime-a", summary_version: "v1", interpretation_revision: "i1", entry_time: "known", requests: 3, outcomes: [{ outcome: "technical_failure", requests: 1 }], diagnostic_failure: { numerator: 1, denominator: 2, excluded_requests: 1, state: "computed", basis: "runtime" }, coverage: runtimeCoverage };
const queue = { items: [], total: 7, evaluated_at: "2026-09-08T01:00:00Z", version: "v1", filters: { mode: "current", period: "24h", sort: "created_desc" }, coverage: "authorized", statistics: {current: 7, created: 0, reassessment: 0, unassigned: 0, overdue: 0, basis_unrecorded: 0, basis_changed: 0, basis_unavailable: 0} } as InvestigationQueuePage;

beforeEach(() => {
  vi.spyOn(workbenchAPI, "reading").mockImplementation(async () => reading());
  vi.spyOn(workbenchAPI, "createReading").mockImplementation(async () => reading());
  vi.spyOn(workbenchAPI, "summary").mockResolvedValue(summary);
  vi.spyOn(workbenchAPI, "contributions").mockImplementation(async (_id, dimension) => contribution(dimension));
  vi.spyOn(workbenchAPI, "series").mockImplementation(async (id, _signal, bucket, metric = "amount") => ({ reading_id: id, metric, unit: metric === "requests" ? "request" : "micros", bucket_seconds: bucket ?? 3600, points: [{ start_inclusive: period.start_inclusive, end_exclusive: period.end_exclusive, known_value: metric === "requests" ? "2" : "0", complete: false }], coverage: { ...coverage, gaps: [...coverage.gaps] } }));
  vi.spyOn(workbenchAPI, "sources").mockResolvedValue({ reading_id: "reading-a", items: [{ source_system_id: "source-visible", display_name: "已授权来源", status: "disconnected", last_received_at: null, scope_label: "project_restricted", gaps: ["同步中断"] }] } as WorkbenchSourceStatuses);
  vi.spyOn(workbenchAPI, "recentRequests").mockResolvedValue({ version: "xinsight.overview.recent-requests.v1", scope_basis: "economic_reading_members", detail_scope: "currently_authorized_details_only", reading_id: "reading-a", membership_ref: "members-a", cutoff: new Date().toISOString(), items: [] });
  vi.spyOn(runtimeAPI, "createReading").mockImplementation(async () => runtimeReading());
  vi.spyOn(runtimeAPI, "reading").mockImplementation(async () => runtimeReading());
  vi.spyOn(runtimeAPI, "summary").mockResolvedValue(runtimeSummary);
  vi.spyOn(runtimeAPI, "requests").mockImplementation(async id => ({ reading_id: id, items: [] }));
  vi.spyOn(investigationQueueAPI, "list").mockResolvedValue(queue);
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.useRealTimers(); vi.unstubAllGlobals(); acceptScopeSession(null); window.history.replaceState(null,"","/"); });
function renderPage(current = session) { const navigate = vi.fn(); const onContext = vi.fn(); return { ...render(<OverviewPage session={current} navigate={navigate} onContext={onContext} context={context} />), navigate, onContext }; }
async function openRecent() { fireEvent.click(await screen.findByText(/近期运行与费用记录/, { selector: "summary" })); }
function fullSession(): AccessSessionResponse { return { ...session, session: { ...session.session, permissions: ["workbench.summary.read", "workbench.details.read", "workbench.coverage.read", "investigation.scoped.read"] } }; }

describe("combined overview within existing scope", () => {
  it("shows source-reported read and change results while keeping economic counts separate", async () => {
    vi.spyOn(workActivityAPI, "scopes").mockResolvedValue({ version: "xinsight.work-activity-scopes.v1", items: [{
      source_id: "source-visible", source_label: "Agent", source_instance: "instance", org_id: "7", project_id: "9", project_label: "Project",
    }] });
    vi.spyOn(workActivityAPI, "summary").mockResolvedValue({ version: "xinsight.work-activity-summary.v2",
      from: period.start_inclusive, to: period.end_exclusive, cutoff: period.end_exclusive,
      reported_actions: 15, reported_resource_reads: 9, reported_resource_changes: 3,
      resource_read_outcomes: { completed: 3, failed: 2, optional_not_found: 1, unknown: 3 },
      resource_change_outcomes: { completed: 1, failed: 1, optional_not_found: 0, unknown: 1 },
      reported_model_events: 2, reported_linked_events: 1, reported_unlinked_events: 1,
      sources_needing_attention: 1, coverage: ["source_period_completeness_not_asserted"],
    });
    const activityContext = { ...context, request: { ...context.request, filters: { ...context.request.filters, source_projects: ["project-9"], cost_projects: [] } } };
    const navigate = vi.fn();
    render(<OverviewPage session={fullSession()} navigate={navigate} onContext={vi.fn()} context={activityContext} />);
    await waitFor(() => {
      expect(screen.getByRole("group", { name: /资源读取 9 次尝试：来源报告完成 3，失败 2，可选不存在 1，未确认 3/ })).toBeTruthy();
      expect(screen.getByRole("group", { name: /资源写入或修改 3 次尝试：来源报告完成 1，失败 1，可选不存在 0，未确认 1/ })).toBeTruthy();
      expect(within(screen.getByText("已记录模型请求").closest("article")!).getByText("2")).toBeTruthy();
    });
    const change = await screen.findByRole("group", { name: /资源写入或修改 3 次尝试/ });
    expect(within(change).getByText("失败")).toBeTruthy();
    expect(within(change).getByText("未确认")).toBeTruthy();
    expect(within(change).queryByText("可选缺失")).toBeNull();
    const activityHelp = screen.getByLabelText("工作活动说明").closest("details")!;
    expect(activityHelp.open).toBe(false);
    fireEvent.click(within(activityHelp).getByLabelText("工作活动说明"));
    expect(activityHelp.open).toBe(true);
    expect(within(activityHelp).getByText(/可选缺失 0 次/)).toBeTruthy();
    const sourceSettings = screen.getByText("选择活动来源", { selector: "summary" }).closest("details")!;
    expect(sourceSettings.open).toBe(false);
    fireEvent.click(within(sourceSettings).getByText("选择活动来源"));
    fireEvent.change(within(sourceSettings).getByLabelText("工作活动来源"), { target: { value: "source-visible:9" } });
    await waitFor(() => expect(workActivityAPI.summary).toHaveBeenLastCalledWith(expect.objectContaining({ source_ids: ["source-visible"], project_ids: ["9"] }), expect.any(AbortSignal)));
    await waitFor(() => expect((screen.getByRole("button", { name: "查看该范围全部活动" }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByRole("button", { name: "查看该范围全部活动" }));
    const activityPath = navigate.mock.calls.at(-1)?.[0] as string;
    const query = new URLSearchParams(activityPath.split("?")[1]);
    expect(activityPath).toMatch(/^\/work\/activity\?/);
    expect(Object.fromEntries(query)).toMatchObject({ from: period.start_inclusive, to: period.end_exclusive, source_ids: "source-visible", project_ids: "9" });
  });
  it("keeps missing and inconsistent resource outcomes distinct from confirmed zero", async () => {
    vi.spyOn(workActivityAPI, "scopes").mockResolvedValue({ version: "xinsight.work-activity-scopes.v1", items: [] });
    vi.spyOn(workActivityAPI, "summary").mockResolvedValue({ version: "xinsight.work-activity-summary.v2",
      from: period.start_inclusive, to: period.end_exclusive, cutoff: period.end_exclusive,
      reported_actions: 4, reported_resource_reads: 3, reported_resource_changes: 1,
      resource_change_outcomes: { completed: 0, failed: 0, optional_not_found: 0, unknown: 0 },
      reported_model_events: 0, reported_linked_events: 0, reported_unlinked_events: 0,
      sources_needing_attention: 1, coverage: ["source_period_completeness_not_asserted"],
    });
    const current = { ...context, request: { ...context.request, filters: { source_ids: [], source_projects: [], cost_projects: [] } } };
    render(<OverviewPage session={fullSession()} navigate={vi.fn()} onContext={vi.fn()} context={current} />);
    expect(await screen.findByText("结果分布未提供；不能把尝试当作已完成。")).toBeTruthy();
    expect(screen.getByText(/结果分型合计 0 与尝试数 1 不一致/)).toBeTruthy();
    expect(screen.getByRole("group", { name: /资源读取 3 次尝试：结果分布未提供/ })).toBeTruthy();
    expect(screen.getByRole("group", { name: /资源写入或修改 1 次尝试/ })).toBeTruthy();
  });
  it("explains known coverage limits and keeps exact machine codes in closed details", async () => {
    vi.mocked(investigationQueueAPI.list).mockResolvedValue({ ...queue, coverage: "retained_investigation_records; security_source_coverage_unconfirmed" });
    vi.mocked(runtimeAPI.summary).mockResolvedValue({ ...runtimeSummary, coverage: { ...runtimeCoverage, gaps: ["forward_only_capture", "timeline_completeness_unknown", "new_source_limit"] } });
    renderPage(fullSession());
    expect(await screen.findByText(/仅统计已保存且当前可见的调查记录；安全相关来源的覆盖范围尚未确认/)).toBeTruthy();
    expect(await screen.findByText(/仅从启用采集后向前接收，未补采此前记录；尚不能确认请求时间线完整/)).toBeTruthy();
    for (const code of ["retained_investigation_records", "security_source_coverage_unconfirmed", "forward_only_capture", "timeline_completeness_unknown", "new_source_limit"]) {
      const detail = screen.getByText(code, { selector: "code" }).closest("details")!;
      expect(detail.open).toBe(false);
    }
    const detail = screen.getByText("new_source_limit", { selector: "code" }).closest("details")!;
    fireEvent.click(within(detail).getByText("范围与采集状态码"));
    expect(detail.open).toBe(true);
    expect(screen.getByText("new_source_limit", { selector: "code" }).textContent).toBe("new_source_limit");
  });
  it("summary-only never queries runtime, queue, source coverage or global evidence", async () => {
    const fetch = vi.spyOn(globalThis, "fetch");
    renderPage();
    expect(await screen.findByText("可见model")).toBeTruthy();
    expect(runtimeAPI.createReading).not.toHaveBeenCalled();
    expect(investigationQueueAPI.list).not.toHaveBeenCalled();
    expect(workbenchAPI.sources).not.toHaveBeenCalled();
    expect(fetch).not.toHaveBeenCalled();
    expect(screen.getAllByText("无运行详情读取权限").length).toBeGreaterThan(0);
    expect(screen.getAllByText(/当前账户不能读取来源覆盖/).length).toBeGreaterThan(0);
    expect(screen.queryByRole("combobox", { name: "来源" })).toBeNull();
  });
  it("selects only authorized source names and can clear applied filters", async () => {
    renderPage(fullSession());
    await screen.findByRole("option", { name: "来源 · 已授权来源" });
    const source = screen.getByRole("combobox", { name: "来源" }) as HTMLSelectElement;
    expect(source.value).toBe("source-visible");
    expect(screen.getByText(/生效范围：来源 已授权来源；来源项目 project-a；费用项目 cost-a/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "清除项目与来源筛选" }));
    await waitFor(() => expect(workbenchAPI.createReading).toHaveBeenLastCalledWith(expect.objectContaining({ filters: { source_ids: [], source_projects: [], cost_projects: [] } }), session.csrf_token, expect.any(AbortSignal)));
    expect((screen.getByLabelText("来源筛选") as HTMLInputElement).value).toBe("");
    fireEvent.change(source, { target: { value: "source-visible" } });
    fireEvent.click(screen.getByRole("button", { name: "应用并读取" }));
    await waitFor(() => expect(workbenchAPI.createReading).toHaveBeenLastCalledWith(expect.objectContaining({ filters: { source_ids: ["source-visible"], source_projects: [], cost_projects: [] } }), session.csrf_token, expect.any(AbortSignal)));
  });
  it("requests series keeps economic membership, units and incomplete coverage distinct", async () => {
    renderPage();
    expect(await screen.findByRole("group", { name: /模型已知请求时段/ })).toBeTruthy();
    expect(workbenchAPI.series).toHaveBeenCalledWith("reading-a", expect.any(AbortSignal), expect.any(Number), "requests");
    expect(screen.getByLabelText(/：2 条已知请求，仅为已观察值，时段完整性未知/)).toBeTruthy();
    expect(screen.queryByText("0.0%")).toBeNull();
    expect(await screen.findByText(/总金额为零或负值，无法绘制金额构成/)).toBeTruthy();
    expect(screen.getAllByText("¥0.000000").length).toBeGreaterThan(0);
    fireEvent.change(screen.getByLabelText("趋势指标"), { target: { value: "amount" } });
    expect(screen.getByLabelText(/：网关已计费用 ¥0.000000，仅为已观察值/)).toBeTruthy();
    expect(screen.getByText(/仅1个时段有可计算的已知值/)).toBeTruthy();
    expect(screen.getByText(/0 仅表示已记录数为 0，不证明时段无活动/)).toBeTruthy();
  });
  it("keeps module explanations closed while source and completeness status stay visible", async () => {
    renderPage(fullSession());
    await screen.findByText("可见model");
    for (const label of ["摘要说明", "趋势说明", "工作活动说明", "费用构成说明", "模型请求说明", "AI 用途说明"]) {
      expect(screen.getByLabelText(label).closest("details")?.open).toBe(false);
    }
    expect(screen.getByText(/所选期间完整性未知/)).toBeTruthy();
    expect(screen.getByText(/有来源断开或未接入/)).toBeTruthy();
    const costHelp = screen.getByLabelText("费用构成说明").closest("details")!;
    fireEvent.click(within(costHelp).getByLabelText("费用构成说明"));
    expect(costHelp.open).toBe(true);
    expect(within(costHelp).getByText(/其他已知组.*未归属/)).toBeTruthy();
  });
  it("plots recorded zero and adjacent observed segments while preserving coverage warnings and gaps", async () => {
    vi.mocked(workbenchAPI.series).mockImplementation(async (id, _signal, bucket, metric = "amount") => ({ reading_id: id, metric, unit: metric === "requests" ? "request" : "micros", bucket_seconds: bucket ?? 3600,
      points: metric === "requests" ? [seriesPoint(0, "0", true), seriesPoint(1, "4", true), seriesPoint(2, "0", false), seriesPoint(3, "3", true), seriesPoint(5, "2", true)] : [seriesPoint(0, "0", false)], coverage: { ...coverage, gaps: [...coverage.gaps] } }));
    renderPage();
    const trend = await screen.findByRole("group", { name: /模型已知请求时段/ });
    await within(trend).findByLabelText(/0 条已知请求，时段资料完整/);
    const dots = [...trend.querySelectorAll<HTMLButtonElement>(".overview-trend-point")];
    expect(dots).toHaveLength(5);
    expect(dots[0].style.top).toBe("152px");
    expect(Number.parseFloat(dots[0].style.left)).toBeCloseTo(100 / 12, 2);
    expect(Number.parseFloat(dots[4].style.left)).toBeCloseTo(1100 / 12, 2);
    expect(trend.querySelectorAll(".overview-trend-segment")).toHaveLength(3);
    expect(trend.querySelector(".overview-trend-segment")?.getAttribute("data-from")).toBe("0");
    expect(trend.querySelector(".overview-trend-segment")?.getAttribute("data-to")).toBe("1");
    const partialZero = within(trend).getByLabelText(/：0 条已知请求，仅为已观察值，时段完整性未知/);
    expect(partialZero.getAttribute("aria-label")).toContain(" 至 ");
    expect(screen.getByText(/完整性未知/, { selector: ".overview-chart-caption" })).toBeTruthy();
  });
  it("uses a signed amount axis for refunds rather than flattening negatives to zero", async () => {
    vi.mocked(workbenchAPI.series).mockImplementation(async (id, _signal, bucket, metric = "amount") => ({ reading_id: id, metric, unit: metric === "requests" ? "request" : "micros", bucket_seconds: bucket ?? 3600,
      points: metric === "amount" ? [seriesPoint(0, "-500000", true), seriesPoint(1, "0", true), seriesPoint(2, "500000", true)] : [seriesPoint(0, "1", false)], coverage: { ...coverage, gaps: [...coverage.gaps] } }));
    renderPage();
    fireEvent.change(screen.getByLabelText("趋势指标"), { target: { value: "amount" } });
    const trend = await screen.findByRole("group", { name: /已知费用时段/ });
    await within(trend).findByLabelText(/-¥0.500000，时段资料完整/);
    expect([...trend.querySelectorAll<HTMLButtonElement>(".overview-trend-point")].map(dot => dot.style.top)).toEqual(["152px", "82px", "12px"]);
    expect(trend.querySelectorAll(".overview-trend-segment")).toHaveLength(2);
    expect(screen.getByText("-¥0.500000")).toBeTruthy();
  });
  it("keeps keyboard trend focus above hover and clears it when the series changes", async () => {
    vi.mocked(workbenchAPI.series).mockImplementation(async (id, _signal, bucket, metric = "amount") => ({ reading_id: id, metric, unit: metric === "requests" ? "request" : "micros", bucket_seconds: bucket ?? 3600,
      points: metric === "requests" ? [seriesPoint(0, "2", false), seriesPoint(1, "3", false)] : [seriesPoint(0, "0", true)], coverage: { ...coverage, gaps: [...coverage.gaps] } }));
    renderPage();
    const trend = await screen.findByRole("group", { name: /模型已知请求时段/ });
    await within(trend).findByLabelText(/2 条已知请求/);
    const [first, second] = [...trend.querySelectorAll<HTMLButtonElement>(".overview-trend-point")];
    act(() => first.focus());
    fireEvent.mouseEnter(second);
    expect(trend.querySelectorAll(".overview-trend-segment")).toHaveLength(1);
    expect(screen.getByText(/2 条已知请求，仅为已观察值/, { selector: "output" })).toBeTruthy();
    act(() => first.blur());
    expect(screen.getByText(/3 条已知请求，仅为已观察值/, { selector: "output" })).toBeTruthy();
    fireEvent.change(screen.getByLabelText("趋势指标"), { target: { value: "amount" } });
    expect(screen.getByText("聚焦数据点查看准确时间和值", { selector: "output" })).toBeTruthy();
    expect(screen.getByLabelText(/¥0.000000，时段资料完整/)).toBeTruthy();
  });
  it.each([
    { basis: "gateway_charged" as const, title: "网关已计费用趋势", help: /网关已计费用可能有冲回/ },
    { basis: "calculated_reference" as const, title: "计算参考费用趋势", help: /计算参考费用可能有冲回/ },
  ])("names the $basis amount trend from the selected money basis", async ({ basis, title, help: helpPattern }) => {
    vi.mocked(workbenchAPI.createReading).mockImplementation(async () => ({ ...reading(), money_basis: basis, money_title: basis === "gateway_charged" ? "网关已计金额" : "计算参考费用" }));
    const selected = { ...context, request: { ...context.request, money_basis: basis } };
    render(<OverviewPage session={session} navigate={vi.fn()} onContext={vi.fn()} context={selected} />);
    fireEvent.change(screen.getByLabelText("趋势指标"), { target: { value: "amount" } });
    expect(await screen.findByRole("heading", { name: title })).toBeTruthy();
    const help = screen.getByLabelText("趋势说明").closest("details")!;
    expect(help.open).toBe(false);
    fireEvent.click(within(help).getByLabelText("趋势说明"));
    expect(help.open).toBe(true);
    expect(within(help).getByText(helpPattern)).toBeTruthy();
  });
  it("loads each allowed surface in its own reading and keeps current queue independent", async () => {
    renderPage(fullSession());
    expect(await screen.findByText("已授权来源")).toBeTruthy();
    const attention = screen.getByText(/待关注与复核/, { selector: "summary" }).closest("details")!;
    expect(attention.open).toBe(false);
    fireEvent.click(within(attention).getByText(/待关注与复核/, { selector: "summary" }));
    expect(attention.open).toBe(true);
    expect(await screen.findByText("技术失败")).toBeTruthy();
    expect(workbenchAPI.createReading).toHaveBeenCalledWith(context.request, session.csrf_token, expect.any(AbortSignal));
    expect(runtimeAPI.createReading).toHaveBeenCalledWith(expect.objectContaining({ period: context.request.period, filters: context.request.filters }), session.csrf_token, expect.any(AbortSignal));
    expect(investigationQueueAPI.list).toHaveBeenCalledWith({ mode: "current", period: "24h", sort: "created_desc" }, undefined, expect.any(AbortSignal));
    expect(screen.getByText(/来源已断开/)).toBeTruthy();
    expect(screen.queryByText("source-visible")).toBeNull();
  });
  it("keeps independent source failure unavailable while economic amounts remain visible", async () => {
    vi.mocked(workbenchAPI.sources).mockRejectedValue(new ApiRequestError(403));
    renderPage(fullSession());
    await screen.findByText("可见model");
    const card = screen.getByText("来源与资料范围").closest("article")!;
    expect(within(card).getByText("当前账户无权读取此部分。")).toBeTruthy();
    expect(screen.getAllByText("¥0.000000").length).toBeGreaterThan(0);
  });
  it("does not turn economic failure into a zero or hide independent runtime", async () => {
    vi.mocked(workbenchAPI.createReading).mockRejectedValue(new ApiRequestError(503));
    renderPage(fullSession());
    await screen.findByText("技术失败");
    const card = screen.getByText("已记录模型请求").closest("article")!;
    expect(within(card).getByText("—")).toBeTruthy();
    expect(screen.queryByText("¥0.000000")).toBeNull();
    expect(workbenchAPI.series).not.toHaveBeenCalled();
  });
  it("derives the visible economic completeness note from the returned summary", async () => {
    vi.mocked(workbenchAPI.summary).mockResolvedValue({ ...summary, request_total_complete: true, amount_complete: true });
    renderPage();
    const note = await screen.findByText(/费用请求总数已确认/);
    expect(note.textContent).toContain("所选期间完整性未知");
    expect(note.textContent).not.toContain("请求总数未确认完整");
  });
  it("preserves fixed nondefault dates and all filters for topic navigation and remount", async () => {
    const view = renderPage();
    await screen.findByText("可见model");
    fireEvent.click(screen.getByRole("button", { name: "查看整个当前范围的请求专题" }));
    expect(view.navigate).toHaveBeenCalledWith("/workbench/cost?view=usage", undefined, undefined, expect.objectContaining({
      readingID: "reading-a", dimension: "source_project", activeBucket: "",
      request: expect.objectContaining({ filters: context.request.filters, period: { start_inclusive: period.start_inclusive, end_exclusive: period.end_exclusive, display_timezone: period.timezone } }),
    }));
    const saved = view.onContext.mock.calls.at(-1)![0];
    expect(saved.request).toEqual(context.request);
    view.unmount();
    render(<OverviewPage session={session} navigate={vi.fn()} onContext={vi.fn()} context={saved} />);
    await screen.findByText("可见model");
    expect(workbenchAPI.createReading).toHaveBeenLastCalledWith(context.request, session.csrf_token, expect.any(AbortSignal));
    expect((screen.getByLabelText("期间") as HTMLSelectElement).value).toBe("168");
    expect((screen.getByLabelText("来源筛选") as HTMLInputElement).value).toBe("source-visible");
  });
  it("carries an exact group selection and treats a focused trend period as context only", async () => {
    const view = renderPage(fullSession());
    const model = (await screen.findByRole("heading", { name: "模型请求分布" })).closest("article")!;
    await within(model).findByRole("button", { name: "查看可见model的同一阅读依据" });
    fireEvent.click(within(model).getByRole("button", { name: "查看可见model的同一阅读依据" }));
    expect(view.navigate).toHaveBeenLastCalledWith("/workbench/cost?view=usage", undefined, undefined, expect.objectContaining({
      dimension: "model", selectedRef: "model-a", readingID: "reading-a",
      request: expect.objectContaining({ filters: context.request.filters }),
    }));
    const trend = screen.getByRole("group", { name: /模型已知请求时段/ });
    fireEvent.focus(within(trend).getByLabelText(/2 条已知请求，仅为已观察值，时段完整性未知/));
    expect(document.querySelector(".overview-trend-tooltip")?.textContent).toContain("2 条已知请求");
    fireEvent.click(screen.getByRole("button", { name: "查看整个当前范围的请求专题" }));
    expect(view.navigate).toHaveBeenLastCalledWith("/workbench/cost?view=usage", undefined, undefined, expect.objectContaining({
      dimension: "source_project", selectedRef: undefined, readingID: "reading-a", activeBucket: expect.stringContaining("2 条已知请求"),
      request: expect.objectContaining({ period: { start_inclusive: period.start_inclusive, end_exclusive: period.end_exclusive, display_timezone: period.timezone } }),
    }));
  });
  it("clears on scope invalidation and suppresses late responses", async () => {
    let resolve!: (value: WorkbenchSummary) => void;
    vi.mocked(workbenchAPI.summary).mockReturnValue(new Promise(done => { resolve = done; }));
    renderPage();
    await screen.findByText("可见model");
    act(() => window.dispatchEvent(new Event("xinsight:scope-changed")));
    await act(async () => { resolve(summary); });
    expect(screen.queryByText("可见model")).toBeNull();
    expect(screen.queryByText("¥0.000000")).toBeNull();
    expect(screen.getByRole("alert").textContent).toContain("旧资料已移除");
  });
  it("does not extend leases with delayed summary data after expiry", async () => {
    vi.useFakeTimers();
    let resolve!: (value: WorkbenchSummary) => void;
    vi.mocked(workbenchAPI.createReading).mockImplementation(async () => reading("reading-a", 1000));
    vi.mocked(workbenchAPI.summary).mockReturnValue(new Promise(done => { resolve = done; }));
    renderPage();
    await act(async () => { await Promise.resolve(); });
    expect(screen.getByText("可见model")).toBeTruthy();
    await act(async () => { vi.advanceTimersByTime(1200); });
    await act(async () => { resolve(summary); });
    expect(screen.queryByText("可见model")).toBeNull();
    expect(screen.queryByText("¥0.000000")).toBeNull();
    expect(screen.getAllByText("本次费用与使用读取已过期，请重新读取。").length).toBeGreaterThan(0);
  });
  it("new filters cancel the earlier request without resurrecting its members", async () => {
    let resolve!: (value: WorkbenchSummary) => void;
    vi.mocked(workbenchAPI.summary).mockReturnValueOnce(new Promise(done => { resolve = done; })).mockResolvedValue({ ...summary, request_count: 9 });
    renderPage();
    await screen.findByText("可见model");
    fireEvent.change(screen.getByLabelText("来源筛选"), { target: { value: "source-b" } });
    fireEvent.click(screen.getByRole("button", { name: "应用并读取" }));
    await waitFor(() => expect(workbenchAPI.createReading).toHaveBeenCalledTimes(2));
    await act(async () => { resolve({ ...summary, request_count: 999 }); });
    expect(screen.queryByText("999")).toBeNull();
    expect(screen.getByText("9")).toBeTruthy();
  });
});
it("formats zero amounts and invalid ratios without inventing percentages", () => {
  expect(overviewMoney("0")).toBe("¥0.000000");
  expect(overviewMoney("-1")).toBe("-¥0.000001");
  expect(overviewShare("0", "0")).toBe("不可计算");
  expect(overviewShare("0", "100")).toBe("0%");
  expect(overviewShare("bad", "100")).toBe("不可计算");
});

// These cases use the actual HTTP request layer; only fetch returns synthetic
// HTTP responses. This exercises scope events which API-method mocks miss.
function http(body: unknown, status = 200, revision = "scope-a") { return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json", "X-XInsight-Scope-Revision": revision } }); }
function problem(status: number, code: string) { return { type: "about:blank", title: code, status, code, correlation_id: "fixture", retryable: false }; }
function actualTransport(options: { runtimeDenied?: boolean; economicDenied?: boolean; runtimeSummaryResponse?: () => Promise<Response>; defer?: Map<string, (response: Response) => void>; recentResponse?: unknown; runtimeReadingID?: string } = {}) {
  vi.restoreAllMocks();
  const scoped = { ...fullSession(), scope_revision: "scope-a" };
  acceptScopeSession(scoped);
  vi.spyOn(accessAPI, "session").mockResolvedValue(scoped);
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
  let runtimePeriod = runtimeReading().period;
  const runtimeFixture = () => ({ ...runtimeReading(), reading_id: options.runtimeReadingID ?? "runtime-a", period: runtimePeriod });
  const fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const path = String(input), url = new URL(path, "https://x-insight.local");
    if(path === "/api/v1/access/providers") return http({local_account_state:"active",oidc_runtime_state:"not_configured",boundary:"Fixture provider configuration"});
    if (path === "/api/v1/workbench/readings") return options.economicDenied ? http(problem(403, "workbench_scope_denied"), 403) : http(reading());
    if (path === "/api/v1/workbench/readings/reading-a") return http(reading());
    if (path === "/api/v1/runtime/readings") {
      if (init?.body) {
        const requested = JSON.parse(String(init.body)) as Runtime.ReadingRequest;
        runtimePeriod = { start_inclusive: requested.period.start_inclusive!, end_exclusive: requested.period.end_exclusive!, timezone: requested.period.display_timezone };
      }
      return options.runtimeDenied ? http(problem(403, "workbench_scope_denied"), 403) : http(runtimeFixture());
    }
    if (path === `/api/v1/runtime/readings/${options.runtimeReadingID ?? "runtime-a"}`) return http(runtimeFixture());
    if (path.startsWith("/api/v1/investigation/queue?")) return http(queue);
    if (path.startsWith("/api/v1/runtime/readings/") && path.includes("/summary")) return options.runtimeSummaryResponse ? options.runtimeSummaryResponse() : http(runtimeSummary);
    if (path.endsWith("/summary")) return http(summary);
    if (path.endsWith("/sources")) return http({ reading_id: "reading-a", items: [] });
    if (path.includes("/series?")) {
      const metric = url.searchParams.get("metric") ?? "amount";
      if (metric === "requests" && options.defer) return new Promise(resolve => options.defer!.set("series", resolve));
      return http({ reading_id: "reading-a", metric, unit: metric === "requests" ? "request" : "micros", bucket_seconds: 3600, points: [], coverage });
    }
    if (path.includes("/contributions?")) {
      const dimension = url.searchParams.get("dimension") as WorkbenchContributions["dimension"];
      if (dimension === "model" && options.defer) return new Promise(resolve => options.defer!.set("model", resolve));
      return http(contribution(dimension));
    }
    if (path.includes("/recent-requests?")) return http(options.recentResponse ?? { version: "xinsight.overview.recent-requests.v1", scope_basis: "economic_reading_members", detail_scope: "currently_authorized_details_only", reading_id: "reading-a", membership_ref: "members-a", cutoff: new Date().toISOString(), runtime_reading_id: url.searchParams.get("runtime_reading_id") || undefined, items: [] });
    throw new Error(`Unexpected synthetic URL: ${path}`);
  });
  vi.stubGlobal("fetch", fetch);
  return { scoped, fetch };
}
it("actual transport keeps a current-scope runtime denial local without remounting the overview", async () => {
  const { fetch } = actualTransport({ runtimeDenied: true });
  const changed = vi.fn(); window.addEventListener("xinsight:scope-changed", changed);
  try {
    window.history.replaceState(null, "", "/inbox");
    render(<App />);
    await screen.findByText("可见model");
    expect(screen.getAllByText("当前账户无权读取此部分。").length).toBeGreaterThan(0);
    expect(screen.getAllByText("¥0.000000").length).toBeGreaterThan(0);
    expect(changed).not.toHaveBeenCalled();
    expect(fetch.mock.calls.filter(([path]) => path === "/api/v1/runtime/readings")).toHaveLength(1);
    expect(fetch.mock.calls.filter(([path]) => path === "/api/v1/workbench/readings")).toHaveLength(1);
    expect(fetch.mock.calls.some(([path]) => String(path).includes("/posture"))).toBe(false);
  } finally { window.removeEventListener("xinsight:scope-changed", changed); }
});
it("carries both overview readings into a scoped execution and restores them on return", async () => {
  const executionID = "xe1_" + "a".repeat(64);
  const { fetch } = actualTransport({ recentResponse: { version: "xinsight.overview.recent-requests.v1", scope_basis: "economic_reading_members", detail_scope: "currently_authorized_details_only", reading_id: "reading-a", membership_ref: "members-a", cutoff: new Date().toISOString(), runtime_reading_id: "runtime-a", runtime_membership_ref: "members-a", runtime_cutoff: new Date().toISOString(), items: [
    { request_id: "linked-overview-request", execution_id: executionID, evidence_event_id: "event-a", source_system_id: "source-visible", occurred_at: "2026-09-08T00:00:00Z", gateway_charged_micros: "0", calculated_reference_micros: "1600000", purpose_state: "unknown", account_state: "unknown", application_state: "unknown", investigation_available: true, runtime: { match_state: "matched", request_ref: "xrr_a", outcome: "local_delivery_completed" } }
  ] } });
  window.history.replaceState(null, "", "/inbox");
  render(<App />);
  await openRecent();
  fireEvent.click(await screen.findByRole("tab", { name: "费用记录" }));
  await screen.findByText("linked-overview-request");
  fireEvent.click(screen.getByRole("button", { name: "查看执行" }));
  expect(window.history.state.workbenchOrigin).toBe("/inbox");
  expect(window.history.state.workbenchReading).toMatchObject({ readingID: "reading-a", related: { executionID, readingID: "runtime-a" } });
  fireEvent.click(screen.getByRole("button", { name: "← 返回总览" }));
  await waitFor(() => expect(fetch.mock.calls.some(([path]) => path === "/api/v1/workbench/readings/reading-a")).toBe(true));
  expect(fetch.mock.calls.some(([path]) => path === "/api/v1/runtime/readings/runtime-a")).toBe(true);
});
it("opens an unpriced overview request in the same runtime reading and restores overview on return", async () => {
  const readingID = "xrt_" + "d".repeat(64);
  actualTransport({ runtimeReadingID: readingID });
  const ref = "xrr_" + "d".repeat(64);
  const secondRef = "xrr_" + "e".repeat(64);
  const first = { request_ref: ref, source_system_id: "source-visible", source_instance: "new-ur", org_id: "2", project_id: "2", request_id: "overview-runtime-only", entry_at: "2026-09-05T00:00:00Z", entry_time_state: "observed", last_observed_at: "2026-09-05T00:00:00Z", observation_count: 4, outcome: "unresolved", outcome_basis: "request_level_observation" } as const;
  vi.spyOn(runtimeAPI, "requests").mockResolvedValue({ reading_id: readingID, items: [first, { ...first, request_ref: secondRef, request_id: "overview-runtime-second" }] });
  window.history.replaceState(null, "", "/inbox");
  render(<App />);
  expect(await screen.findByText("overview-runtime-only")).toBeTruthy();
  fireEvent.click(within(screen.getByText("overview-runtime-only").closest("tr")!).getByRole("button", { name: "查看运行依据" }));
  expect(window.history.state.runtimeReading).toMatchObject({ readingID, selectedRef: ref, entry: "all" });
  expect(await screen.findByRole("heading", { name: "请求观察详情" })).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "← 返回总览" }));
  expect(await screen.findByText("overview-runtime-only")).toBeTruthy();
  fireEvent.click(within(screen.getByText("overview-runtime-second").closest("tr")!).getByRole("button", { name: "查看运行依据" }));
  expect(window.history.state.runtimeReading).toMatchObject({ readingID, selectedRef: secondRef, entry: "all" });
  expect(within((await screen.findByRole("heading", { name: "请求观察详情" })).closest("section")!).getByText("overview-runtime-second")).toBeTruthy();
  cleanup(); // Simulate a page refresh: the history reference must revalidate and restore the selected request.
  render(<App />);
  expect(within((await screen.findByRole("heading", { name: "请求观察详情" })).closest("section")!).getByText("overview-runtime-second")).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "← 返回总览" }));
  expect(await screen.findByText("overview-runtime-second")).toBeTruthy();
});
it.each([410, 503])("actual transport handles economic child %s before a late member response", async status => {
  const deferred = new Map<string, (response: Response) => void>();
  const { scoped } = actualTransport({ defer: deferred });
  renderPage(scoped);
  await screen.findByText("技术失败");
  await waitFor(() => expect(deferred.size).toBe(2));
  expect(screen.getAllByText("¥0.000000").length).toBeGreaterThan(0);
  await act(async () => { deferred.get("series")!(http(problem(status, status === 410 ? "workbench_reading_expired" : "workbench_unavailable"), status)); });
  await act(async () => { deferred.get("model")!(http(contribution("model"))); });
  expect(screen.getByText("技术失败")).toBeTruthy();
  expect(within(screen.getByText("当前复核待办").closest("article")!).getByText("7")).toBeTruthy();
  if (status === 410) {
    expect(screen.queryByText("¥0.000000")).toBeNull();
    expect(screen.queryByText("可见model")).toBeNull();
    expect(screen.queryByText("reading-a")).toBeNull();
    expect(screen.getAllByText("本次读取已失效，已移除相关资料，请重新读取。").length).toBeGreaterThan(0);
  } else {
    expect(screen.getAllByText("¥0.000000").length).toBeGreaterThan(0);
    expect(screen.getByText("可见model")).toBeTruthy();
    expect(screen.queryByText("本次读取已失效，已移除相关资料，请重新读取。")).toBeNull();
  }
});


it("actual App keeps authorized runtime and queue when cost qualification is denied, then clears them on revision change", async () => {
  const { scoped, fetch } = actualTransport({ economicDenied: true });
  const changed=vi.fn();window.addEventListener("xinsight:scope-changed",changed);
  window.history.replaceState(null,"","/inbox");
  try {
    render(<App />);
    const runtimeMetric=await screen.findByText("运行本地交付完成");
    await waitFor(()=>expect(within(runtimeMetric.closest("article")!).getByText("0 / 3")).toBeTruthy());
    expect(within(screen.getByText("当前复核待办").closest("article")!).getByText("7")).toBeTruthy();
    expect(screen.getAllByText("当前账户无权读取此部分。").length).toBeGreaterThan(0);
    expect(screen.queryByText("正在验证登录状态…")).toBeNull();
    expect(changed).not.toHaveBeenCalled();
    expect(fetch.mock.calls.filter(([path])=>path==="/api/v1/workbench/readings")).toHaveLength(1);
    expect(fetch.mock.calls.filter(([path])=>path==="/api/v1/runtime/readings")).toHaveLength(1);
    expect(fetch.mock.calls.filter(([path])=>String(path).startsWith("/api/v1/investigation/queue?"))).toHaveLength(1);
    expect(accessAPI.session).toHaveBeenCalledTimes(2);
    // Exercise the real transport's invalidation, not a synthetic window event.
    let confirm!:(value:AccessSessionResponse)=>void;
    vi.mocked(accessAPI.session).mockImplementationOnce(()=>new Promise(resolve=>{confirm=resolve;}));
    fetch.mockResolvedValueOnce(http(problem(403,"workbench_scope_denied"),403,"scope-b"));
    await act(async()=>{await expect(workbenchAPI.createReading(context.request,scoped.csrf_token)).rejects.toMatchObject({status:403});});
    expect(changed).toHaveBeenCalledOnce();
    expect(screen.queryByText("运行本地交付完成")).toBeNull();
    expect(screen.queryByText("当前复核待办")).toBeNull();
    expect(screen.getByText("正在验证登录状态…")).toBeTruthy();
    const revoked={...scoped,scope_revision:"no_authorized_scope"};
    vi.mocked(accessAPI.session).mockResolvedValue(revoked);
    await act(async()=>confirm(revoked));
    expect(screen.getByText("当前没有可访问的资料范围")).toBeTruthy();
    expect(fetch.mock.calls.filter(([path])=>path==="/api/v1/workbench/readings")).toHaveLength(2);
    expect(accessAPI.session).toHaveBeenCalledTimes(4);
  } finally { window.removeEventListener("xinsight:scope-changed",changed); }
});


it.each(["session-first","runtime-first"] as const)("waits for the complete scope before first-login business reads (%s)",async(order)=>{
 let finishRuntime!:(value:Response)=>void;
 const {scoped,fetch}=actualTransport({economicDenied:true,runtimeSummaryResponse:()=>new Promise(resolve=>{finishRuntime=resolve;})});
 acceptScopeSession(null);
 vi.spyOn(accessAPI,"login").mockResolvedValue({...scoped,scope_revision:undefined});
 let finishBootstrap!:(value:AccessSessionResponse)=>void,finishRefresh!:(value:AccessSessionResponse)=>void;
 vi.mocked(accessAPI.session).mockRejectedValueOnce(new ApiRequestError(401)).mockImplementationOnce(()=>new Promise(resolve=>{finishBootstrap=resolve;})).mockImplementationOnce(()=>new Promise(resolve=>{finishRefresh=resolve;}));
 const changed=vi.fn();window.addEventListener("xinsight:scope-changed",changed);
 window.history.replaceState(null,"","/login");
 try {
  render(<App/>);await screen.findByRole("heading",{name:"登录工作台"});
  fireEvent.change(screen.getByLabelText("用户名"),{target:{value:"scoped.fixture"}});
  fireEvent.change(screen.getByLabelText("密码"),{target:{value:"fixture-password"}});
  fireEvent.click(screen.getByRole("button",{name:"登录"}));
  await waitFor(()=>expect(finishBootstrap).toBeTypeOf("function"));
  expect(screen.queryByRole("heading",{name:"总览"})).toBeNull();
  expect((screen.getByLabelText("密码") as HTMLInputElement).value).toBe("");
  expect(fetch.mock.calls.some(([path])=>String(path).includes("/readings"))).toBe(false);
  await act(async()=>finishBootstrap(scoped));
  await waitFor(()=>expect(finishRuntime).toBeTypeOf("function"));
  expect(finishRefresh).toBeTypeOf("function");
  if(order==="session-first"){
   await act(async()=>finishRefresh(scoped));await act(async()=>finishRuntime(http(runtimeSummary)));
  }else{
   await act(async()=>finishRuntime(http(runtimeSummary)));await act(async()=>finishRefresh(scoped));
  }
  const metric=screen.getByText("运行本地交付完成");expect(within(metric.closest("article")!).getByText("0 / 3")).toBeTruthy();
  expect(within(screen.getByText("当前复核待办").closest("article")!).getByText("7")).toBeTruthy();
  expect(screen.getAllByText("当前账户无权读取此部分。").length).toBeGreaterThan(0);
  for(const path of ["/api/v1/workbench/readings","/api/v1/runtime/readings"]){
   const calls=fetch.mock.calls.filter(([value])=>value===path);expect(calls).toHaveLength(1);
   expect(new Headers(calls[0][1]?.headers).get("X-XInsight-Scope-Revision")).toBe("scope-a");
   expect(calls[0][1]?.signal?.aborted).toBe(false);
  }
  expect(changed).not.toHaveBeenCalled();expect(accessAPI.session).toHaveBeenCalledTimes(3);
  // A later 401 still removes mounted authorized content and returns to login.
  fetch.mockResolvedValueOnce(http(problem(401,"authentication_required"),401));
  await act(async()=>{await expect(workbenchAPI.createReading(context.request,scoped.csrf_token)).rejects.toMatchObject({status:401});});
  expect(screen.getByRole("heading",{name:"登录工作台"})).toBeTruthy();expect(screen.queryByText("运行本地交付完成")).toBeNull();
 }finally{window.removeEventListener("xinsight:scope-changed",changed);}
});

it.each(["401","unavailable","missing-revision","revoked"] as const)("does not mount business data when first-login session resolution is %s",async(outcome)=>{
 const {scoped,fetch}=actualTransport();acceptScopeSession(null);
 const current={...scoped,scope_revision:outcome==="missing-revision"?undefined:"no_authorized_scope"};
 vi.spyOn(accessAPI,"login").mockResolvedValue({...scoped,scope_revision:undefined});
 vi.mocked(accessAPI.session).mockResolvedValue(current).mockRejectedValueOnce(new ApiRequestError(401)).mockImplementationOnce(async()=>{
  if(outcome==="401")throw new ApiRequestError(401);
  if(outcome==="unavailable")throw new Error("Session unavailable");
  return current;
 });
 window.history.replaceState(null,"","/login");render(<App/>);await screen.findByRole("heading",{name:"登录工作台"});
 fireEvent.change(screen.getByLabelText("用户名"),{target:{value:"scoped.fixture"}});fireEvent.change(screen.getByLabelText("密码"),{target:{value:"fixture-password"}});fireEvent.click(screen.getByRole("button",{name:"登录"}));
 await screen.findByText(outcome==="revoked"?"当前没有可访问的资料范围":outcome==="401"?"登录状态已失效，请重新登录。":"暂时无法确认可访问范围，请稍后重新登录。");
 expect(fetch.mock.calls.some(([path])=>String(path).includes("/readings"))).toBe(false);
 expect(screen.queryByRole("heading",{name:"总览"})).toBeNull();
});

describe("overview comparison and recent requests", () => {
  it("shows an unpriced unresolved runtime request first and opens its frozen request ref", async () => {
    const ref = "xrr_" + "a".repeat(64);
    vi.mocked(workbenchAPI.recentRequests).mockResolvedValue({ version: "xinsight.overview.recent-requests.v1", scope_basis: "economic_reading_members", detail_scope: "currently_authorized_details_only", reading_id: "reading-a", membership_ref: "members-a", cutoff: new Date().toISOString(), runtime_reading_id: "runtime-a", items: [] });
    vi.mocked(runtimeAPI.requests).mockResolvedValue({ reading_id: "runtime-a", items: [{ request_ref: ref, source_system_id: "source-visible", source_instance: "new-ur", org_id: "2", project_id: "2", request_id: "unpriced-failure", entry_at: "2026-09-25T08:20:41Z", entry_time_state: "observed", last_observed_at: "2026-09-25T08:20:41Z", observation_count: 4, outcome: "unresolved", outcome_basis: "request_level_observation" }] });
    const input = { ...context, request: { ...context.request, filters: { ...context.request.filters, cost_projects: [] } } };
    const navigate = vi.fn();
    render(<OverviewPage session={fullSession()} navigate={navigate} onContext={vi.fn()} context={input} />);
    expect(screen.getByText(/近期运行与费用记录/, { selector: "summary" }).closest("details")?.open).toBe(false);
    await openRecent();
    expect(await screen.findByText("unpriced-failure")).toBeTruthy();
    expect(screen.getByRole("tab", { name: "运行请求" }).getAttribute("aria-selected")).toBe("true");
    expect(within(screen.getByRole("tabpanel", { name: "近期运行请求" })).getByText("结果未决")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "查看运行依据" }));
    expect(navigate).toHaveBeenCalledWith("/workbench/runtime", ref);
    expect(runtimeAPI.requests).toHaveBeenCalledWith("runtime-a", "all", undefined, expect.any(AbortSignal));
    fireEvent.click(screen.getByRole("tab", { name: "费用记录" }));
    expect(await screen.findByText("当前明细授权范围内没有近期经济请求。")).toBeTruthy();
    expect(screen.queryByText("unpriced-failure")).toBeNull();
  });
  it("keeps runtime requests visible when the independent economic reading fails", async () => {
    vi.mocked(workbenchAPI.createReading).mockRejectedValueOnce(new ApiRequestError(503));
    vi.mocked(runtimeAPI.requests).mockResolvedValue({ reading_id: "runtime-a", items: [{ request_ref: "xrr_" + "b".repeat(64), source_system_id: "source-visible", source_instance: "new-ur", org_id: "2", project_id: "2", request_id: "runtime-without-economic", entry_time_state: "observed", entry_at: "2026-09-25T08:20:41Z", last_observed_at: "2026-09-25T08:20:41Z", observation_count: 4, outcome: "unresolved", outcome_basis: "request_level_observation" }] });
    renderPage(fullSession());
    await openRecent();
    expect(await screen.findByText("runtime-without-economic")).toBeTruthy();
    fireEvent.click(screen.getByRole("tab", { name: "费用记录" }));
    expect(within(screen.getByRole("tabpanel", { name: "近期费用记录" })).getByText("此部分暂不可用，请稍后重新读取。")).toBeTruthy();
  });
  it("does not wait for a pending economic read before showing a frozen runtime request", async () => {
    vi.mocked(workbenchAPI.createReading).mockReturnValueOnce(new Promise(() => {}));
    vi.mocked(runtimeAPI.requests).mockResolvedValue({ reading_id: "runtime-a", items: [{ request_ref: "xrr_" + "c".repeat(64), source_system_id: "source-visible", source_instance: "new-ur", org_id: "2", project_id: "2", request_id: "runtime-while-cost-pending", entry_time_state: "observed", entry_at: "2026-09-25T08:20:41Z", last_observed_at: "2026-09-25T08:20:41Z", observation_count: 4, outcome: "unresolved", outcome_basis: "request_level_observation" }] });
    renderPage(fullSession());
    await openRecent();
    expect(await screen.findByText("runtime-while-cost-pending")).toBeTruthy();
    expect(screen.getByRole("tab", { name: "运行请求" }).getAttribute("aria-selected")).toBe("true");
  });
  it("loads request rankings for model and purpose plus an independent amount ranking for source project", async () => {
    vi.mocked(workbenchAPI.contributions).mockImplementation(async (_id, dimension, options) => ({ ...contribution(dimension), order_by: options?.orderBy }));
    renderPage(fullSession());
    await screen.findByRole("heading", { name: "模型请求分布" });
    await screen.findByText("可见model");
    expect(workbenchAPI.contributions).toHaveBeenCalledWith("reading-a", "model", { topN: 5, orderBy: "requests" }, expect.any(AbortSignal));
    expect(workbenchAPI.contributions).toHaveBeenCalledWith("reading-a", "purpose", { topN: 5, orderBy: "requests" }, expect.any(AbortSignal));
    expect(workbenchAPI.contributions).toHaveBeenCalledWith("reading-a", "source_project", { topN: 5, orderBy: "amount" }, expect.any(AbortSignal));
    expect(screen.getByRole("heading", { name: "AI 用途" })).toBeTruthy();
    expect(screen.getByRole("heading", { name: "项目费用构成" })).toBeTruthy();
    expect(screen.queryByLabelText("构成指标")).toBeNull();
  });
  it("draws project amounts against the full denominator and keeps exact group navigation", async () => {
    vi.mocked(workbenchAPI.contributions).mockImplementation(async (_id, dimension, options) => {
      const base = contribution(dimension);
      if (dimension !== "source_project") return { ...base, order_by: options?.orderBy };
      const first = { ...base.rows[0], selected_amount_micros: "500000", share: { numerator: "500000", denominator: "1000000", unit: "micros" as const } };
      return { ...base, order_by: "amount", denominator_micros: "1000000", rows: [first, { ...first, ref: "project-b", label: "组二", selected_amount_micros: "250000" }], other_known_micros: "150000", unassigned_micros: "100000" };
    });
    const view = renderPage(fullSession());
    const card = (await screen.findByRole("heading", { name: "项目费用构成" })).closest("article")!;
    await within(card).findByText("组二");
    expect(card.querySelectorAll(".overview-donut-arc")).toHaveLength(4);
    expect(within(card).getByRole("group", { name: /来源项目费用构成；分组、其他与未归属/ })).toBeTruthy();
    expect(within(card).getByText("¥1.000000")).toBeTruthy();
    expect(within(card).getByText("其他已知组")).toBeTruthy();
    expect(within(card).getByText("未归属")).toBeTruthy();
    expect(within(card).getByText("50%")).toBeTruthy();
    fireEvent.focus(card.querySelector(".overview-donut-arc")!);
    expect(card.querySelector(".overview-donut-readout")?.textContent).toContain("¥0.500000");
    fireEvent.click(within(card).getByRole("button", { name: "查看可见source_project的同一阅读依据" }));
    expect(view.navigate).toHaveBeenLastCalledWith("/workbench/cost", undefined, undefined, expect.objectContaining({ dimension: "source_project", selectedRef: "source_project-a", readingID: "reading-a" }));
  });
  it("keeps keyboard focus readout above hover and restores the hovered amount after blur", async () => {
    vi.mocked(workbenchAPI.contributions).mockImplementation(async (_id, dimension, options) => {
      const base = contribution(dimension);
      if (dimension !== "source_project") return { ...base, order_by: options?.orderBy };
      const first = { ...base.rows[0], label: "project-9011", selected_amount_micros: "300000" };
      return { ...base, order_by: "amount", denominator_micros: "330000", rows: [first, { ...first, ref: "project-9007", label: "project-9007", selected_amount_micros: "30000" }] };
    });
    renderPage(fullSession());
    const card = (await screen.findByRole("heading", { name: "项目费用构成" })).closest("article")!;
    const focusedButton = await within(card).findByRole("button", { name: "查看project-9011的同一阅读依据" });
    const otherRow = within(card).getByText("project-9007").closest("li")!;
    const readout = card.querySelector(".overview-donut-readout")!;
    fireEvent.mouseEnter(otherRow);
    expect(readout.textContent).toContain("project-9007 · ¥0.030000");
    act(() => focusedButton.focus());
    expect(document.activeElement).toBe(focusedButton);
    fireEvent.mouseEnter(otherRow);
    expect(readout.textContent).toContain("project-9011 · ¥0.300000");
    act(() => focusedButton.blur());
    expect(readout.textContent).toContain("project-9007 · ¥0.030000");
    fireEvent.mouseLeave(otherRow);
    expect(readout.textContent).toContain("指向或聚焦色段");
  });
  it("names the amount ring for the selected application dimension", async () => {
    vi.mocked(workbenchAPI.contributions).mockImplementation(async (_id, dimension, options) => {
      const base = contribution(dimension);
      return dimension === "application" ? { ...base, order_by: "amount", denominator_micros: "1000000", rows: [{ ...base.rows[0], selected_amount_micros: "1000000" }] } : { ...base, order_by: options?.orderBy };
    });
    renderPage(fullSession());
    fireEvent.change(screen.getByLabelText("费用贡献维度"), { target: { value: "application" } });
    expect(await screen.findByRole("heading", { name: "应用费用构成" })).toBeTruthy();
    expect(screen.queryByRole("heading", { name: "项目费用构成" })).toBeNull();
    expect(await screen.findByRole("group", { name: /应用费用构成；分组、其他与未归属/ })).toBeTruthy();
  });
  it.each([
    { total: "0", amount: "0", other: "0", expected: /总金额为零或负值/ },
    { total: "100", amount: "-10", other: "110", expected: /金额含负值或冲回/ },
    { total: "100", amount: "50", other: "0", expected: /金额合计和总金额不一致/ },
    { total: "unknown", amount: "50", other: "0", expected: /金额资料不可计算/ },
  ])("does not draw a misleading donut for $total total and $amount group", async ({ total, amount, other, expected }) => {
    vi.mocked(workbenchAPI.contributions).mockImplementation(async (_id, dimension, options) => {
      const base = contribution(dimension);
      return dimension === "source_project" ? { ...base, order_by: "amount", denominator_micros: total, rows: [{ ...base.rows[0], selected_amount_micros: amount }], other_known_micros: other } : { ...base, order_by: options?.orderBy };
    });
    renderPage(fullSession());
    const card = (await screen.findByRole("heading", { name: "项目费用构成" })).closest("article")!;
    expect(await within(card).findByText(expected)).toBeTruthy();
    expect(card.querySelector(".overview-donut-arc")).toBeNull();
    expect(within(card).getAllByText(amount === "-10" ? "-¥0.000010" : overviewMoney(amount)).length).toBeGreaterThan(0);
  });
  it("compares equal request counts with equal bar lengths on one zero-based scale", async () => {
    vi.mocked(workbenchAPI.contributions).mockImplementation(async (_id, dimension, options) => {
      const base = contribution(dimension);
      if (dimension !== "model") return { ...base, order_by: options?.orderBy };
      return { ...base, order_by: "requests", rows: [{ ...base.rows[0], request_count: 1 }, { ...base.rows[0], ref: "model-b", label: "第二模型", request_count: 1 }] };
    });
    renderPage(fullSession());
    const card = (await screen.findByRole("heading", { name: "模型请求分布" })).closest("article")!;
    await within(card).findByText("第二模型");
    const bars = [...card.querySelectorAll<HTMLElement>(".overview-contribution-bar i")];
    expect(bars).toHaveLength(2);
    expect(bars[0].style.width).toBe("100%");
    expect(bars[1].style.width).toBe(bars[0].style.width);
    expect(within(card).getAllByText("50%")).toHaveLength(2);
    expect(within(card).getAllByText("¥0.000000")).toHaveLength(2);
  });
  it("does not draw request shares when a negative other count cancels the total", async () => {
    vi.mocked(workbenchAPI.contributions).mockImplementation(async (_id, dimension, options) => {
      const base = contribution(dimension);
      return dimension === "model" ? { ...base, order_by: "requests", other_known_requests: -1, unassigned_requests: 1 } : { ...base, order_by: options?.orderBy };
    });
    renderPage(fullSession());
    const card = (await screen.findByRole("heading", { name: "模型请求分布" })).closest("article")!;
    expect(await within(card).findByText(/分组、其他与未归属请求数合计和当前范围不一致/)).toBeTruthy();
    expect(card.querySelector(".overview-contribution-bar i")).toBeNull();
    expect(within(card).getByText(/占比暂不可用/)).toBeTruthy();
  });
  it("does not report zero adopted purposes when the reading has no purpose dimension", async () => {
    vi.mocked(workbenchAPI.createReading).mockResolvedValue({ ...reading(), dimensions: ["model", "source_project"] });
    const view = renderPage(fullSession());
    const purpose = (await screen.findByRole("heading", { name: "AI 用途" })).closest("section")!;
    expect(within(purpose).getByText(/本次阅读未提供用途维度/)).toBeTruthy();
    expect(within(purpose).queryByText(/已采用用途 0 \/ 2 条/)).toBeNull();
    expect(view.onContext.mock.calls.at(-1)?.[0].usageDimension).toBe("source_project");
    expect(workbenchAPI.contributions).not.toHaveBeenCalledWith("reading-a", "purpose", expect.anything(), expect.anything());
  });
  it("shows actual known denominators, zero amount as non-computable share, and no fabricated application identity", async () => {
    renderPage(fullSession());
    const model = (await screen.findByRole("heading", { name: "模型请求分布" })).closest("article")!;
    await within(model).findByText("可见model");
    expect(within(model).getByText("100%")).toBeTruthy();
    expect(within(model).getByText("¥0.000000")).toBeTruthy();
    fireEvent.click(screen.getByLabelText("工作活动说明"));
    expect(within(screen.getByText("已记录应用组").closest("article")!).getByText("1")).toBeTruthy();
    expect(screen.getByText(/总金额为零或负值，无法绘制金额构成/)).toBeTruthy();
  });
  it("pages the exact authorized recent economic members and keeps unmatched runtime distinct", async () => {
    const first = { version: "xinsight.overview.recent-requests.v1" as const, scope_basis: "economic_reading_members" as const, detail_scope: "currently_authorized_details_only" as const,
      reading_id: "reading-a", membership_ref: "members-a", cutoff: new Date().toISOString(), runtime_reading_id: "runtime-a", runtime_membership_ref: "members-a", runtime_cutoff: new Date().toISOString(), next_cursor: "next", items: [
        { request_id: "request-a", execution_id: "xe1_" + "a".repeat(64), evidence_event_id: "event-a", source_system_id: "source-visible", model: "model-a", source_project: "project-a", occurred_at: "2026-09-08T00:00:00Z", gateway_charged_micros: "0", calculated_reference_micros: "1600000", purpose_state: "adopted", purpose_category: "information_query", account_state: "unknown", application_state: "unknown", runtime: { match_state: "matched" as const, request_ref: "xrr_a", outcome: "local_delivery_completed" }, investigation_available: true }
      ] };
    vi.mocked(workbenchAPI.recentRequests).mockImplementation(async (_id, _runtime, cursor) => cursor ? { ...first, next_cursor: undefined, items: [{ ...first.items[0], request_id: "request-b", evidence_event_id: "event-b", runtime: { match_state: "not_in_runtime_reading" } }] } : first);
    const view = renderPage(fullSession());
    await openRecent();
    fireEvent.click(screen.getByRole("tab", { name: "费用记录" }));
    expect(await screen.findByText("request-a")).toBeTruthy();
    expect(screen.getAllByText("本地交付完成").length).toBeGreaterThan(0);
    expect(screen.getAllByText("¥0.000000").length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole("button", { name: "查看执行" }));
    expect(view.navigate).toHaveBeenCalledWith(expect.stringContaining("/executions/xe1_"));
    expect(view.onContext.mock.calls.at(-1)?.[0]).toMatchObject({ economicReadingID: "reading-a", runtimeReadingID: "runtime-a" });
    fireEvent.click(screen.getByRole("button", { name: "加载更多近期请求" }));
    expect(await screen.findByText("request-b")).toBeTruthy();
    expect(screen.getByText("本次运行阅读未匹配")).toBeTruthy();
    expect(workbenchAPI.recentRequests).toHaveBeenCalledWith("reading-a", "runtime-a", "next", expect.any(AbortSignal));
  });
  it("restores the same reading IDs without extending their absolute expiry", async () => {
    const saved = { ...context, economicReadingID: "reading-a", runtimeReadingID: "runtime-a" };
    render(<OverviewPage session={fullSession()} navigate={vi.fn()} onContext={vi.fn()} context={saved} />);
    await screen.findByText("可见model");
    expect(workbenchAPI.reading).toHaveBeenCalledWith("reading-a", expect.any(AbortSignal));
    expect(runtimeAPI.reading).toHaveBeenCalledWith("runtime-a", expect.any(AbortSignal));
    expect(workbenchAPI.createReading).not.toHaveBeenCalled();
  });
  it("keeps the economic page when its separate runtime link expires", async () => {
    vi.mocked(workbenchAPI.recentRequests).mockImplementation(async (_id, runtimeID) => {
      if (runtimeID) throw new ApiRequestError(410, problem(410, "workbench_reading_expired"));
      return { version: "xinsight.overview.recent-requests.v1", scope_basis: "economic_reading_members", detail_scope: "currently_authorized_details_only", reading_id: "reading-a", membership_ref: "members-a", cutoff: new Date().toISOString(), items: [
        { request_id: "economic-only", execution_id: "xe1_" + "a".repeat(64), evidence_event_id: "event-a", source_system_id: "source-visible", occurred_at: "2026-09-08T00:00:00Z", gateway_charged_micros: "0", calculated_reference_micros: "1600000", purpose_state: "unknown", account_state: "unknown", application_state: "unknown", runtime: { match_state: "unavailable" } }
      ] };
    });
    const view = renderPage(fullSession());
    await openRecent();
    fireEvent.click(screen.getByRole("tab", { name: "费用记录" }));
    expect(await screen.findByText("economic-only")).toBeTruthy();
    expect(screen.getByText(/运行关联暂不可用；下表仍按经济资料显示/)).toBeTruthy();
    expect(workbenchAPI.recentRequests).toHaveBeenCalledWith("reading-a", undefined, undefined, expect.any(AbortSignal));
    await waitFor(() => expect(view.onContext.mock.calls.at(-1)?.[0].runtimeReadingID).toBe("runtime-a"));
  });
  it("does not paint a late recent page after applying a different scope", async () => {
    let release!: (value: Awaited<ReturnType<typeof workbenchAPI.recentRequests>>) => void;
    vi.mocked(workbenchAPI.recentRequests).mockReturnValueOnce(new Promise(done => { release = done; }));
    renderPage(fullSession());
    await waitFor(() => expect(workbenchAPI.recentRequests).toHaveBeenCalledTimes(1));
    fireEvent.change(screen.getByLabelText("来源筛选"), { target: { value: "other-source" } });
    fireEvent.click(screen.getByRole("button", { name: "应用并读取" }));
    await waitFor(() => expect(workbenchAPI.createReading).toHaveBeenCalledTimes(2));
    await act(async () => release({ version: "xinsight.overview.recent-requests.v1", scope_basis: "economic_reading_members", detail_scope: "currently_authorized_details_only", reading_id: "reading-a", membership_ref: "members-a", cutoff: new Date().toISOString(), runtime_reading_id: "runtime-a", items: [
      { request_id: "late-old-scope", execution_id: "xe1_" + "a".repeat(64), evidence_event_id: "event-old", source_system_id: "source-visible", occurred_at: "2026-09-08T00:00:00Z", gateway_charged_micros: "0", calculated_reference_micros: "0", purpose_state: "unknown", account_state: "unknown", application_state: "unknown", runtime: { match_state: "unavailable" } }
    ] }));
    expect(screen.queryByText("late-old-scope")).toBeNull();
  });
});
