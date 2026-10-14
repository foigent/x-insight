import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  ApiRequestError,
  accessAPI,
  acceptScopeSession,
  type AccessSessionResponse,
  type Problem,
  type WorkbenchContributions,
  type WorkbenchDetails,
  type WorkbenchSavedFilter,
  type WorkbenchCoverage,
  type WorkbenchReading,
  type WorkbenchReadingRequest,
  type WorkbenchSeries,
  type WorkbenchSummary,
  workbenchAPI,
} from "./api";
import { App } from "./App";
import { WorkbenchPage, type WorkbenchSnapshot } from "./WorkbenchPage";
import { navigationOwner } from "./workbenchNavigation";

const now = "2026-09-09T12:00:00Z";
const coverage: WorkbenchCoverage = {
  status: "period_completeness_unknown",
  period_completeness: "unknown",
  projection_complete_for_members: true,
  known_items: 2,
  gaps: ["source_period_completeness_not_asserted"],
};
const reading: WorkbenchReading = {
  reading_id: "xwr_AAAAAAAAAAAAAAAAAAAAAA",
  contract_version: "xinsight.workbench.v1",
  metric_version: "xinsight.workbench.cost.v1",
  period: { start_inclusive: "2026-09-08T12:00:00Z", end_exclusive: now, timezone: "UTC" },
  cutoff: now,
  expires_at: "2026-09-09T12:30:00Z",
  money_basis: "gateway_charged",
  money_title: "网关扣费",
  currency: "CNY",
  membership_ref: `sha256:${"a".repeat(64)}`,
  interpretation_revision: `sha256:${"b".repeat(64)}`,
  authorization_revision: `sha256:${"c".repeat(64)}`,
  dimensions: ["model", "source_project", "cost_project"],
  coverage,
};
const summary: WorkbenchSummary = {
  reading_id: reading.reading_id,
  money_basis: "gateway_charged",
  currency: "CNY",
  request_count: 2,
  request_total_complete: false,
  selected_amount_micros: "17",
  gateway_charged_micros: "17",
  calculated_reference_micros: "23",
  amount_complete: false,
  purpose_known_requests: 0,
  actor_known_requests: 0,
  coverage,
};
const topContributions: WorkbenchContributions = {
  reading_id: reading.reading_id,
  dimension: "source_project",
  mode: "top_n",
  rows: [{
    ref: "c291cmNlX3Byb2plY3QAcHJvamVjdC0yMDI",
    label: "project-202",
    request_count: 2,
    selected_amount_micros: "17",
    gateway_charged_micros: "17",
    calculated_reference_micros: "23",
    share: { numerator: "17", denominator: "17", unit: "micros" },
    drilldown_allowed: true,
  }],
  other_known_micros: "0",
  unassigned_micros: "0",
  unassigned_scope: "reading_constant",
  denominator_micros: "17",
  is_last_page: true,
  authorized_group_count: 1,
};
const series: WorkbenchSeries = {
  reading_id: reading.reading_id,
  metric: "amount",
  unit: "micros",
  bucket_seconds: 3600,
  points: [{ start_inclusive: "2026-09-09T11:00:00Z", end_exclusive: now, known_value: "17", complete: false }],
  coverage,
};
const session: AccessSessionResponse = {
  session: {
    session_ref: "xr_AAAAAAAAAAAAAAAAAAAAAA",
    subject_id: "xs_8899aabbccddeeff",
    role: "workbench_reader",
    permissions: ["workbench.summary.read", "workbench.details.read", "workbench.coverage.read", "workbench.preferences.write"],
    created_at: now,
    last_seen_at: now,
    idle_expires_at: "2026-09-09T12:30:00Z",
    absolute_expires_at: "2026-09-09T20:00:00Z",
  },
  csrf_token: "csrf-workbench-test",
};

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("WorkbenchPage", () => {
  it("makes adopted purpose and request counts primary in usage without losing micro amounts", async () => {
    mockCoreReading(); mockOptional();
    vi.mocked(workbenchAPI.createReading).mockResolvedValue({ ...reading, purpose: { status: "available", rule_version: "v1", manifest_ref: "fixture" }, dimensions: [...reading.dimensions, "purpose"] });
    vi.mocked(workbenchAPI.summary).mockResolvedValue({ ...summary, purpose_known_requests: 1 });
    vi.mocked(workbenchAPI.contributions).mockResolvedValue({ ...topContributions, dimension: "purpose", order_by: "requests", denominator_requests: 2, unassigned_requests: 1, other_known_requests: 0, rows: [{ ...topContributions.rows[0], label: "software_check", request_count: 1 }] });
    render(<WorkbenchPage session={session} navigate={vi.fn()} usage />);
    expect(await screen.findByRole("heading", { name: "当前范围的用途请求分布" })).toBeTruthy();
    expect(workbenchAPI.contributions).toHaveBeenCalledWith(reading.reading_id, "purpose", { topN: 10, orderBy: "requests" });
    expect(workbenchAPI.series).toHaveBeenCalledWith(reading.reading_id, undefined, 3600, "requests");
    expect(screen.getByText("1 / 2")).toBeTruthy();
    expect(screen.getAllByText("¥0.000017").length).toBeGreaterThan(0);
    expect(screen.getByText("软件实现与检查")).toBeTruthy();
    expect(screen.getByText(/不等于全部 AI 工作/)).toBeTruthy();
  });

  it("defaults to evidenced source projects when purpose is unavailable and application has no attribution", async () => {
    mockCoreReading(); mockOptional();
    vi.mocked(workbenchAPI.createReading).mockResolvedValue({ ...reading, purpose: { status: "unavailable", rule_version: "v1", manifest_ref: "fixture" }, dimensions: [...reading.dimensions, "application"] });
    vi.mocked(workbenchAPI.contributions).mockResolvedValue({ ...topContributions, dimension: "source_project", order_by: "requests", denominator_requests: 2, unassigned_requests: 0, other_known_requests: 0 });
    render(<WorkbenchPage session={session} navigate={vi.fn()} usage />);
    expect(await screen.findByRole("heading", { name: "当前范围的来源项目请求分布" })).toBeTruthy();
    expect(workbenchAPI.contributions).toHaveBeenCalledWith(reading.reading_id, "source_project", { topN: 10, orderBy: "requests" });
    expect(screen.getByText(/本阅读未提供完整用途维度/)).toBeTruthy();
    expect(screen.queryByRole("option", { name: "请求用途" })).toBeNull();
    expect(screen.getByRole("option", { name: "应用" })).toBeTruthy();
  });

  it("defaults to account only when the summary has known account attribution", async () => {
    mockCoreReading(); mockOptional();
    vi.mocked(workbenchAPI.createReading).mockResolvedValue({ ...reading, dimensions: [...reading.dimensions, "application", "account"] });
    vi.mocked(workbenchAPI.summary).mockResolvedValue({ ...summary, actor_known_requests: 1 });
    vi.mocked(workbenchAPI.contributions).mockResolvedValue({ ...topContributions, dimension: "account", order_by: "requests", denominator_requests: 2, unassigned_requests: 1, other_known_requests: 0 });
    render(<WorkbenchPage session={session} navigate={vi.fn()} usage />);
    expect(await screen.findByRole("heading", { name: "当前范围的账户归属请求分布" })).toBeTruthy();
    expect(workbenchAPI.contributions).toHaveBeenCalledWith(reading.reading_id, "account", { topN: 10, orderBy: "requests" });
  });

  it("keeps source issue counts visible while the usage source facts stay collapsed", async () => {
    mockCoreReading(); mockOptional();
    vi.mocked(workbenchAPI.sources).mockRejectedValue(new ApiRequestError(403));
    vi.spyOn(workbenchAPI,"sourceFacts").mockRejectedValue(new ApiRequestError(403));
    vi.mocked(workbenchAPI.createReading).mockResolvedValue({ ...reading, source_coverage: { known_documents: 2, pending_predecessor: 1, economic_not_received: 0, economic_hash_conflicts: 0, media_pending: 0, not_captured: 0, profiles: [], gaps: [], collection_states: ["active"] } });
    render(<WorkbenchPage session={session} navigate={vi.fn()} usage />);
    const disclosure=(await screen.findByText(/来源依据与限制 · 2 份已授权记录 · 来源级状态未能读取；1 修订待前驱/)).closest("details")!;
    expect(disclosure.open).toBe(false);
    expect(disclosure.querySelector(".source-facts-panel")).toBeTruthy();
  });

  it("inherits the absolute overview period once and only changes it on user selection", async () => {
    mockCoreReading(); mockOptional();
    const user = userEvent.setup();
    const initialConfig: WorkbenchReadingRequest = { contract_version: "xinsight.workbench.v1", page: "cost", metric_version: "xinsight.workbench.cost.v1", period: { start_inclusive: "2026-09-01T00:00:00Z", end_exclusive: "2026-09-08T00:00:00Z", display_timezone: "UTC" }, money_basis: "gateway_charged", currency: "CNY", filters: { source_ids: ["source-a"], source_projects: ["project-a"], cost_projects: ["cost-a"] } };
    const ui = render(<WorkbenchPage session={session} navigate={vi.fn()} initialConfig={initialConfig} />);
    await screen.findByText("project-202");
    expect(workbenchAPI.createReading).toHaveBeenCalledWith(initialConfig, session.csrf_token);
    expect(screen.getByDisplayValue("来源期间（固定起止）")).toBeTruthy();
    expect(screen.getByDisplayValue("已选来源 · source-a")).toBeTruthy();
    expect(screen.getByText(/费用项目：cost-a/)).toBeTruthy();
    ui.rerender(<WorkbenchPage session={session} navigate={vi.fn()} initialConfig={{ ...initialConfig, period: { relative_hours: 24, display_timezone: "UTC" } }} />);
    expect(workbenchAPI.createReading).toHaveBeenCalledTimes(1);
    await user.click(screen.getByRole("button", { name: "应用范围" }));
    await waitFor(() => expect(workbenchAPI.createReading).toHaveBeenCalledTimes(2));
    expect(workbenchAPI.createReading).toHaveBeenLastCalledWith(initialConfig, session.csrf_token);
    await user.selectOptions(screen.getByLabelText("期间"), "168");
    await user.click(screen.getByRole("button", { name: "应用范围" }));
    await waitFor(() => expect(workbenchAPI.createReading).toHaveBeenCalledTimes(3));
    expect(workbenchAPI.createReading).toHaveBeenLastCalledWith({ ...initialConfig, period: { relative_hours: 168, display_timezone: "UTC" } }, session.csrf_token);
  });


  it("does not compute a share from zero while preserving observed zero amounts", async () => {
    mockCoreReading(); mockOptional();
    vi.mocked(workbenchAPI.summary).mockResolvedValue({ ...summary, selected_amount_micros: "0" });
    vi.mocked(workbenchAPI.contributions).mockResolvedValue({ ...topContributions, denominator_micros: "0", rows: [{ ...topContributions.rows[0], selected_amount_micros: "0", share: { numerator: "0", denominator: "0", unit: "micros" } }] });
    vi.mocked(workbenchAPI.series).mockResolvedValue({ ...series, points: [{ ...series.points[0], known_value: "0" }] });
    render(<WorkbenchPage session={session} navigate={vi.fn()} />);
    expect(await screen.findByText("不可计算")).toBeTruthy();
    expect(screen.queryByText("0.0%")).toBeNull();
    expect(screen.getAllByText("¥0.000000").length).toBeGreaterThan(0);
    expect(screen.getByText(/期间完整性未知，不能把空桶当作真实零/)).toBeTruthy();
    expect(screen.queryByRole("region", { name: "趋势时间桶" })).toBeNull();
  });


  it("restores the same reading and selected contribution only after current authorization succeeds", async () => {
    const create=mockCoreReading();
    vi.spyOn(workbenchAPI,"sources").mockResolvedValue({reading_id:reading.reading_id,items:[]});
    vi.spyOn(workbenchAPI,"savedFilters").mockResolvedValue({items:[]});
    vi.spyOn(workbenchAPI,"details").mockResolvedValue({reading_id:reading.reading_id,contribution_ref:topContributions.rows[0].ref,rows:[]});
    vi.spyOn(accessAPI,"session").mockResolvedValue(session);
    let complete!: (value:WorkbenchReading)=>void;
    const validate=vi.spyOn(workbenchAPI,"reading").mockImplementation(()=>new Promise(resolve=>{complete=resolve}));
    let retained:WorkbenchSnapshot|null=null;
    const keep=(value:WorkbenchSnapshot|null)=>{retained=value};
    const user=userEvent.setup();
    const first=render(<WorkbenchPage session={session} navigate={vi.fn()} onSnapshot={keep}/>);
    await screen.findAllByText("¥0.000017");
    await user.selectOptions(screen.getByLabelText("贡献维度"),"model");
    await user.selectOptions(screen.getByLabelText("贡献模式"),"top5");
    await user.click(screen.getByRole("button",{name:/project-202/}));
    await screen.findByRole("complementary",{name:"所选贡献依据"});
    await waitFor(()=>expect(retained?.selected?.label).toBe("project-202"));
    first.unmount();
    render(<WorkbenchPage session={session} navigate={vi.fn()} snapshot={retained} onSnapshot={keep}/>);
    expect(screen.queryByText("project-202")).toBeNull();
    expect(screen.queryByText("¥0.000017")).toBeNull();
    complete(reading);
    await screen.findByRole("complementary",{name:"所选贡献依据"});
    expect(screen.getByLabelText("贡献维度")).toHaveProperty("value","model");
    expect(screen.getByLabelText("贡献模式")).toHaveProperty("value","top5");
    expect(create).toHaveBeenCalledTimes(1);
    expect(validate).toHaveBeenCalledWith(reading.reading_id);
    expect((retained as WorkbenchSnapshot|null)?.reading.reading_id).toBe(reading.reading_id);
  });

  it.each(["grant", "role", "version"])("discards retained values when the current %s changes", async kind => {
    mockCoreReading();
    vi.spyOn(workbenchAPI,"sources").mockResolvedValue({reading_id:reading.reading_id,items:[]});
    vi.spyOn(workbenchAPI,"savedFilters").mockResolvedValue({items:[]});
    let retained:WorkbenchSnapshot|null=null;
    const keep=(value:WorkbenchSnapshot|null)=>{retained=value};
    const first=render(<WorkbenchPage session={session} navigate={vi.fn()} onSnapshot={keep}/>);
    await waitFor(()=>expect(retained).not.toBeNull());
    first.unmount();
    vi.spyOn(accessAPI,"session").mockResolvedValue(kind==="role"?{...session,session:{...session.session,permissions:["workbench.summary.read"]}}:session);
    const validate=vi.spyOn(workbenchAPI,"reading");
    if(kind==="grant")validate.mockRejectedValue(problem(409,"workbench_access_changed"));
    else validate.mockResolvedValue(kind==="version"?{...reading,interpretation_revision:"changed"}:reading);
    render(<WorkbenchPage session={session} navigate={vi.fn()} snapshot={retained} onSnapshot={keep}/>);
    await screen.findByRole("alert");
    expect(screen.queryByText("project-202")).toBeNull();
    expect(screen.queryByText("¥0.000017")).toBeNull();
    expect(retained).toBeNull();
    expect(screen.getByRole("button",{name:"按当前权限重新建立阅读"})).toBeTruthy();
  });

  it("keeps core reading visible when optional source and preference endpoints fail", async () => {
    mockCoreReading();
    vi.spyOn(workbenchAPI, "sources").mockRejectedValue(problem(403, "permission_denied"));
    vi.spyOn(workbenchAPI, "savedFilters").mockRejectedValue(problem(503, "workbench_unavailable"));

    render(<WorkbenchPage session={session} navigate={vi.fn()} />);

    expect((await screen.findAllByText("¥0.000017")).length).toBeGreaterThan(0);
    expect(screen.getByText(/当前授权不包含来源级状态/)).toBeTruthy();
    expect(screen.getByText(/个人筛选暂时不可用/)).toBeTruthy();
    expect(screen.getByText(/来源的接收进度不能证明已完整采集该期间/)).toBeTruthy();
    expect(screen.getByText(/未归属（本次范围固定）/)).toBeTruthy();
  });

  it("creates a new reading for basis changes but not for presentation dimensions", async () => {
    const create = mockCoreReading();
    vi.spyOn(workbenchAPI, "sources").mockResolvedValue({ reading_id: reading.reading_id, items: [] });
    vi.spyOn(workbenchAPI, "savedFilters").mockResolvedValue({ items: [] });
    const user = userEvent.setup();
    render(<WorkbenchPage session={session} navigate={vi.fn()} />);
    await screen.findAllByText("¥0.000017");

    await user.selectOptions(screen.getByDisplayValue("来源项目"), "model");
    await waitFor(() => expect(workbenchAPI.contributions).toHaveBeenLastCalledWith(
      reading.reading_id, "model", { topN: 10 },
    ));
    expect(create).toHaveBeenCalledTimes(1);

    await user.click(screen.getByRole("button", { name: "计算参考值" }));
    await waitFor(() => expect(create).toHaveBeenCalledTimes(2));
    expect(create.mock.calls[1]?.[0].money_basis).toBe("calculated_reference");
  });

  it("saves the displayed reading configuration rather than unapplied filter edits", async () => {
    mockCoreReading();
    vi.spyOn(workbenchAPI, "sources").mockResolvedValue({ reading_id: reading.reading_id, items: [] });
    vi.spyOn(workbenchAPI, "savedFilters").mockResolvedValue({ items: [] });
    const save = vi.spyOn(workbenchAPI, "createSavedFilter").mockRejectedValue(problem(503,"unavailable"));
    const user = userEvent.setup();
    render(<WorkbenchPage session={session} navigate={vi.fn()} />);
    await screen.findAllByText("¥0.000017");
    await user.selectOptions(screen.getByLabelText("期间"), "720");
    await user.type(screen.getByPlaceholderText("全部已授权项目"), "unapplied-project");
    await user.click(screen.getByRole("button", {name:"保存当前筛选"}));
    expect(save.mock.calls[0]?.[0].config.period.relative_hours).toBe(24);
    expect(save.mock.calls[0]?.[0].config.filters.source_projects).toEqual([]);
  });

  it("opens and pages restricted evidence, returns, saves, updates and reopens", async () => {
    const create=mockCoreReading();
    vi.spyOn(workbenchAPI,"sources").mockResolvedValue({reading_id:reading.reading_id,items:[]});
    vi.spyOn(workbenchAPI,"savedFilters").mockResolvedValue({items:[]});
    const row={request_id:"req-17",execution_id:"xe1_test",evidence_event_id:"event-17",event_hash:"sha256:test",source_system_id:"source",source_version:"v1",terminal_status:"settled" as const,occurred_at:now,gateway_charged_micros:"17",calculated_reference_micros:"23",account_state:"unknown" as const,application_state:"unknown" as const,purpose_state:"unknown" as const};
    vi.spyOn(workbenchAPI,"details").mockImplementation(async(_id,ref,cursor):Promise<WorkbenchDetails>=>({reading_id:reading.reading_id,contribution_ref:ref,rows:[{...row,request_id:cursor?"req-18":"req-17",evidence_event_id:cursor?"event-18":"event-17"}],next_cursor:cursor?undefined:"next"}));
    vi.spyOn(workbenchAPI,"createSavedFilter").mockImplementation(async command=>({id:"saved-1",owner_subject:session.session.subject_id,config_version:"xinsight.workbench.v1",name:command.name,config:command.config,revision:1,created_at:now,updated_at:now} as WorkbenchSavedFilter));
    vi.spyOn(workbenchAPI,"updateSavedFilter").mockImplementation(async(id,command)=>({id,owner_subject:session.session.subject_id,config_version:"xinsight.workbench.v1",name:command.name,config:command.config,revision:2,created_at:now,updated_at:now} as WorkbenchSavedFilter));
    const user=userEvent.setup();render(<WorkbenchPage session={session} navigate={vi.fn()}/>);
    await screen.findAllByText("¥0.000017");
    await user.click(screen.getByRole("button",{name:/project-202/}));
    expect(await screen.findByText("req-17")).toBeTruthy();
    await user.click(screen.getByRole("button",{name:"加载更多依据"}));
    expect(await screen.findByText("req-18")).toBeTruthy();
    expect(screen.queryByRole("button",{name:"查看调查证据"})).toBeNull();
    await user.click(screen.getByRole("button",{name:"← 返回费用贡献"}));
    expect(create).toHaveBeenCalledTimes(1);
    await user.click(screen.getByRole("button",{name:"保存当前筛选"}));
    expect(await screen.findByText("修订 1")).toBeTruthy();
    await user.click(screen.getByRole("button",{name:"更新"}));
    expect(await screen.findByText("修订 2")).toBeTruthy();
    await user.click(screen.getByRole("button",{name:"重开"}));
    await waitFor(()=>expect(create).toHaveBeenCalledTimes(2));
  });

  it("explains revoked reading errors without showing stale amounts", async()=>{
    vi.spyOn(workbenchAPI,"createReading").mockRejectedValue(problem(409,"workbench_access_changed"));
    render(<WorkbenchPage session={session} navigate={vi.fn()}/>);
    expect(await screen.findByRole("alert")).toHaveProperty("textContent","权限已变化，请重新读取当前范围。");
    expect(screen.queryByText("¥0.000017")).toBeNull();
  });

  it("keeps denominator and unassigned constants while appending full pages", async () => {
    vi.spyOn(workbenchAPI, "createReading").mockResolvedValue(reading);
    vi.spyOn(workbenchAPI, "summary").mockResolvedValue(summary);
    vi.spyOn(workbenchAPI, "series").mockResolvedValue(series);
    vi.spyOn(workbenchAPI, "sources").mockResolvedValue({ reading_id: reading.reading_id, items: [] });
    vi.spyOn(workbenchAPI, "savedFilters").mockResolvedValue({ items: [] });
    vi.spyOn(workbenchAPI, "contributions").mockImplementation(async (_id, dimension, options = {}) => {
      if (options.cursor) {
        return fullPage(dimension, "project-2", "7", undefined);
      }
      if (options.limit) {
        return fullPage(dimension, "project-1", "10", "page-2");
      }
      return topContributions;
    });
    const user = userEvent.setup();
    render(<WorkbenchPage session={session} navigate={vi.fn()} />);
    await screen.findAllByText("¥0.000017");

    await user.selectOptions(screen.getByDisplayValue("Top 10"), "all");
    expect(await screen.findByText("project-1")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "加载更多贡献" }));
    expect(await screen.findByText("project-2")).toBeTruthy();
    expect(screen.getByText(/当前范围已知金额 ¥0.000017/)).toBeTruthy();
    expect(screen.getByText(/未归属（本次范围固定） ¥0.000000/)).toBeTruthy();
  });
  it.each([[24,3600],[72,10800],[168,21600],[720,86400]])("uses bounded %s-hour overview and preserves hourly reading identity",async(hours,seconds)=>{
    mockCoreReading();mockOptional();
    vi.mocked(workbenchAPI.createReading).mockResolvedValue({...reading,period:{...reading.period,start_inclusive:new Date(Date.parse(now)-hours*3600000).toISOString()}});
    const user=userEvent.setup();render(<WorkbenchPage session={session} navigate={vi.fn()}/>);
    await screen.findAllByText("¥0.000017");
    expect(workbenchAPI.series).toHaveBeenLastCalledWith(reading.reading_id,undefined,seconds);
    await user.selectOptions(screen.getByLabelText("趋势粒度"),"hourly");
    expect(workbenchAPI.series).toHaveBeenLastCalledWith(reading.reading_id,undefined,3600);
    expect(workbenchAPI.createReading).toHaveBeenCalledTimes(1);
  });

  it("shows history separately from profile gaps and respects reading timezone",async()=>{
    mockCoreReading();mockOptional();
    vi.mocked(workbenchAPI.summary).mockResolvedValue({...summary,coverage:{...coverage,status:"observed_with_gaps",capture_started_at:"2026-09-09T00:00:00Z",received_watermark:"opaque-should-not-be-parsed",gaps:["period_precedes_capture_start","source_profile_capture_gaps_present","future_gap"]}});
    vi.mocked(workbenchAPI.createReading).mockResolvedValue({...reading,period:{...reading.period,timezone:"America/New_York"}});
    render(<WorkbenchPage session={session} navigate={vi.fn()}/>);
    expect(await screen.findByText(/该部分历史未确认覆盖/)).toBeTruthy();
    expect(screen.getByText(/这与采集起点之前的历史缺口分别计算/)).toBeTruthy();
    expect(screen.getByText(/存在尚未解释的覆盖限制/)).toBeTruthy();
    expect(screen.getByText(/2026.*09.*08.*GMT-4.*20:00/)).toBeTruthy();
    expect(screen.queryByText("opaque-should-not-be-parsed")).toBeNull();
  });

  it("hides whole-source fields even if supplied with restricted coverage",async()=>{
    mockCoreReading();mockOptional();
    vi.mocked(workbenchAPI.summary).mockResolvedValue({...summary,coverage:{...coverage,status:"scope_limited_unknown",capture_started_at:"2001-01-01T00:00:00Z",received_watermark:"secret-position",gaps:["period_precedes_capture_start","source_profile_capture_gaps_present"]}});
    render(<WorkbenchPage session={session} navigate={vi.fn()}/>);
    expect(await screen.findByText("当前授权不足以判断整源覆盖。")).toBeTruthy();
    expect(screen.queryByText(/该部分历史未确认覆盖/)).toBeNull();
    expect(screen.queryByText(/2001/)).toBeNull();
    expect(screen.queryByText(/消费位置$/)).toBeNull();
  });

  it.each([[410,"workbench_reading_expired"],[409,"workbench_access_changed"],[403,"workbench_scope_denied"],[404,"workbench_record_not_found"]])("clears all reading amounts on %s and recovers explicitly",async(status,code)=>{
    mockCoreReading();mockOptional();const user=userEvent.setup();render(<WorkbenchPage session={session} navigate={vi.fn()}/>);
    await screen.findAllByText("¥0.000017");
    vi.mocked(workbenchAPI.contributions).mockRejectedValueOnce(problem(status as number,code as string));
    await user.selectOptions(screen.getByLabelText("贡献维度"),"model");
    await screen.findByRole("alert");expect(screen.queryByText("project-202")).toBeNull();expect(screen.queryByText("¥0.000017")).toBeNull();
    await user.click(screen.getByRole("button",{name:"按当前权限重新建立阅读"}));
    await screen.findAllByText("¥0.000017");expect(workbenchAPI.createReading).toHaveBeenCalledTimes(2);
  });

  it.each(["dimension","mode","series"]) ("keeps the successful %s view on ordinary failure and retries atomically",async(kind)=>{
    mockCoreReading();mockOptional();const user=userEvent.setup();render(<WorkbenchPage session={session} navigate={vi.fn()}/>);
    await screen.findAllByText("¥0.000017");
    const label=kind==="dimension"?"贡献维度":kind==="mode"?"贡献模式":"趋势粒度";
    const before=kind==="dimension"?"source_project":kind==="mode"?"top10":"overview";
    const after=kind==="dimension"?"model":kind==="mode"?"all":"hourly";
    if(kind==="series")vi.mocked(workbenchAPI.series).mockRejectedValueOnce(problem(503,"unavailable"));
    else vi.mocked(workbenchAPI.contributions).mockRejectedValueOnce(problem(503,"unavailable"));
    await user.selectOptions(screen.getByLabelText(label),after);
    await screen.findByRole("alert");expect(screen.getByLabelText(label)).toHaveProperty("value",before);expect(screen.getByText("project-202")).toBeTruthy();
    await user.click(screen.getByRole("button",{name:"重试更新"}));
    await waitFor(()=>expect(screen.getByLabelText(label)).toHaveProperty("value",after));expect(screen.queryByRole("alert")).toBeNull();
  });

  it("shows detail pagination errors, retains the cursor on ordinary failure and clears on expiry",async()=>{
    mockCoreReading();mockOptional();const user=userEvent.setup();
    vi.spyOn(workbenchAPI,"details").mockResolvedValue({reading_id:reading.reading_id,contribution_ref:"ref",rows:[],next_cursor:"cursor"});
    render(<WorkbenchPage session={session} navigate={vi.fn()}/>);await screen.findAllByText("¥0.000017");
    await user.click(screen.getByRole("button",{name:/project-202/}));
    vi.mocked(workbenchAPI.details).mockRejectedValueOnce(problem(503,"unavailable"));
    await user.click(screen.getByRole("button",{name:"加载更多依据"}));
    await screen.findByRole("alert");expect(screen.getByRole("button",{name:"← 返回费用贡献"})).toBeTruthy();
    vi.mocked(workbenchAPI.details).mockRejectedValueOnce(problem(410,"workbench_reading_expired"));
    await user.click(screen.getByRole("button",{name:"重试更新"}));
    expect(await screen.findByRole("button",{name:"按当前权限重新建立阅读"})).toBeTruthy();
    expect(screen.queryByRole("button",{name:"← 返回费用贡献"})).toBeNull();expect(screen.queryByText("¥0.000017")).toBeNull();
  });

  it("does not swallow reading invalidation from optional source reads",async()=>{
    mockCoreReading();mockOptional();vi.mocked(workbenchAPI.sources).mockRejectedValue(problem(409,"workbench_access_changed"));
    render(<WorkbenchPage session={session} navigate={vi.fn()}/>);await screen.findByRole("alert");
    expect(screen.queryByText("¥0.000017")).toBeNull();
  });

  it("does not let a response from an unmounted reading replace the next reading",async()=>{
    mockCoreReading();mockOptional();const user=userEvent.setup();
    const {unmount}=render(<WorkbenchPage session={session} navigate={vi.fn()}/>);await screen.findAllByText("¥0.000017");
    let finish!:(value:WorkbenchContributions)=>void;
    vi.mocked(workbenchAPI.contributions).mockReturnValueOnce(new Promise(resolve=>{finish=resolve}));
    await user.selectOptions(screen.getByLabelText("贡献维度"),"model");unmount();
    render(<WorkbenchPage session={session} navigate={vi.fn()}/>);await screen.findAllByText("¥0.000017");finish({...topContributions,rows:[{...topContributions.rows[0],label:"obsolete"}]});
    await waitFor(()=>expect(screen.getByText("project-202")).toBeTruthy());expect(screen.queryByText("obsolete")).toBeNull();
  });

  it("keeps repeated DST hours distinguishable and zero buckets at zero height",async()=>{
    mockCoreReading();mockOptional();
    vi.mocked(workbenchAPI.createReading).mockResolvedValue({...reading,period:{...reading.period,timezone:"America/New_York"}});
    vi.mocked(workbenchAPI.series).mockResolvedValue({...series,points:[{start_inclusive:"2026-11-01T05:30:00Z",end_exclusive:"2026-11-01T06:30:00Z",known_value:"0",complete:false},{start_inclusive:"2026-11-01T06:30:00Z",end_exclusive:"2026-11-01T07:00:00Z",known_value:"9007199254740993",complete:false}]});
    render(<WorkbenchPage session={session} navigate={vi.fn()}/>);await screen.findAllByText("¥0.000017");
    const zero=screen.getByLabelText(/GMT-4 01:30 → .*GMT-5 01:30/);expect((zero.querySelector('i') as HTMLElement).style.height).toBe('0%');
    const last=screen.getByLabelText(/GMT-5 01:30 → .*GMT-5 02:00/);expect(last.getAttribute('data-end')).toBe('2026-11-01T07:00:00Z');expect(last.getAttribute('aria-label')).toContain('¥9007199254.740993');
  });

});

function mockCoreReading() {
  const create = vi.spyOn(workbenchAPI, "createReading").mockResolvedValue(reading);
  vi.spyOn(workbenchAPI, "summary").mockResolvedValue(summary);
  vi.spyOn(workbenchAPI, "contributions").mockResolvedValue(topContributions);
  vi.spyOn(workbenchAPI, "series").mockResolvedValue(series);
  return create;
}

function fullPage(
  dimension: WorkbenchContributions["dimension"],
  label: string,
  amount: string,
  nextCursor: string | undefined,
): WorkbenchContributions {
  return {
    ...topContributions,
    dimension,
    mode: "full",
    rows: [{ ...topContributions.rows[0], ref: label, label, selected_amount_micros: amount, share: { numerator: amount, denominator: "17", unit: "micros" } }],
    next_cursor: nextCursor,
    is_last_page: nextCursor === undefined,
  };
}

function problem(status: number, code: string) {
  const body: Problem = {
    type: `https://x-insight.local/problems/${code}`,
    title: "Optional endpoint unavailable",
    status,
    code,
    correlation_id: `xc_${"a".repeat(32)}`,
    retryable: status >= 500,
  };
  return new ApiRequestError(status, body);
}

function mockOptional(){vi.spyOn(workbenchAPI,"sources").mockResolvedValue({reading_id:reading.reading_id,items:[]});vi.spyOn(workbenchAPI,"savedFilters").mockResolvedValue({items:[]})}

it("blocks scope edits until retained reading restoration finishes after details arrive",async()=>{
 mockCoreReading();mockOptional();
 const old={...reading,expires_at:"2099-09-09T12:30:00Z"};
 const fixed=scopeRequest();
 vi.spyOn(accessAPI,"session").mockResolvedValue(session);
 vi.spyOn(workbenchAPI,"reading").mockResolvedValue(old);
 vi.spyOn(workbenchAPI,"details").mockResolvedValue({reading_id:reading.reading_id,contribution_ref:topContributions.rows[0].ref,rows:[]});
 let finish!:(value:WorkbenchSummary)=>void;
 vi.mocked(workbenchAPI.summary).mockReturnValueOnce(new Promise(resolve=>{finish=resolve}));
 const user=userEvent.setup(),keep=vi.fn();
 render(<WorkbenchPage session={session} navigate={vi.fn()} onNavigation={keep} navigation={{version:1,owner:navigationOwner(session),readingID:reading.reading_id,request:fixed,dimension:"source_project",mode:"top10",resolution:"overview",activeBucket:"",selectedRef:topContributions.rows[0].ref,contributionPages:1,detailPages:1}}/>);
 await waitFor(()=>expect(workbenchAPI.summary).toHaveBeenCalledWith(reading.reading_id,expect.any(AbortSignal)));
 expect(workbenchAPI.details).toHaveBeenCalledOnce();
 const period=screen.getByLabelText("期间") as HTMLSelectElement;
 const project=screen.getByLabelText("来源项目") as HTMLInputElement;
 for(const name of ["期间","来源","来源项目"]) expect(screen.getByLabelText(name)).toHaveProperty("disabled",true);
 expect(screen.getByRole("button",{name:"应用范围"})).toHaveProperty("disabled",true);
 await user.selectOptions(period,"24");await user.type(project,"-draft");
 expect(period.value).toBe("inherited");expect(project.value).toBe("project-202");
 await act(async()=>finish(summary));
 await waitFor(()=>expect(screen.getByRole("button",{name:"应用范围"})).toHaveProperty("disabled",false));
 expect(period.disabled).toBe(false);expect(project.disabled).toBe(false);
 expect(period.value).toBe("inherited");expect(project.value).toBe("project-202");
 expect(workbenchAPI.createReading).not.toHaveBeenCalled();
 expect(keep).toHaveBeenLastCalledWith(expect.objectContaining({readingID:old.reading_id,request:fixed,selectedRef:topContributions.rows[0].ref}));
 await user.selectOptions(period,"24");await user.clear(project);await user.type(project,"project-new");
 await user.click(screen.getByRole("button",{name:"应用范围"}));
 await waitFor(()=>expect(workbenchAPI.createReading).toHaveBeenCalledOnce());
 expect(workbenchAPI.createReading).toHaveBeenLastCalledWith({...fixed,period:{relative_hours:24,display_timezone:"UTC"},filters:{...fixed.filters,source_projects:["project-new"]}},session.csrf_token);
 await waitFor(()=>expect(screen.getByRole("button",{name:"应用范围"})).toHaveProperty("disabled",false));
 expect(period.value).toBe("24");expect(project.value).toBe("project-new");
});

it.each(["success","failure"] as const)("blocks scope edits during a new reading and re-enables them after %s",async outcome=>{
 mockCoreReading();mockOptional();
 const fixed=scopeRequest();
 let finish!:(value:WorkbenchSummary)=>void,reject!:(reason:unknown)=>void;
 vi.mocked(workbenchAPI.summary).mockReturnValueOnce(new Promise((resolve,fail)=>{finish=resolve;reject=fail}));
 const user=userEvent.setup();
 render(<WorkbenchPage session={session} navigate={vi.fn()} initialConfig={fixed}/>);
 await waitFor(()=>expect(workbenchAPI.summary).toHaveBeenCalledOnce());
 for(const name of ["期间","来源","来源项目"]) expect(screen.getByLabelText(name)).toHaveProperty("disabled",true);
 await user.selectOptions(screen.getByLabelText("期间"),"72");await user.type(screen.getByLabelText("来源项目"),"-draft");
 expect(screen.getByLabelText("期间")).toHaveProperty("value","inherited");
 expect(screen.getByLabelText("来源项目")).toHaveProperty("value","project-202");
 await act(async()=>{if(outcome==="success")finish(summary);else reject(problem(503,"unavailable"))});
 await waitFor(()=>expect(screen.getByRole("button",{name:"应用范围"})).toHaveProperty("disabled",false));
 expect(screen.getByLabelText("期间")).toHaveProperty("disabled",false);expect(screen.getByLabelText("来源项目")).toHaveProperty("disabled",false);
 if(outcome==="failure")expect(screen.getByRole("alert")).toBeTruthy();
 await user.selectOptions(screen.getByLabelText("期间"),"72");
 await user.click(screen.getByRole("button",{name:"应用范围"}));
 await waitFor(()=>expect(workbenchAPI.createReading).toHaveBeenCalledTimes(2));
 expect(workbenchAPI.createReading).toHaveBeenLastCalledWith({...fixed,period:{relative_hours:72,display_timezone:"UTC"}},session.csrf_token);
 await waitFor(()=>expect(screen.getByRole("button",{name:"应用范围"})).toHaveProperty("disabled",false));
 expect(screen.getByLabelText("期间")).toHaveProperty("value","72");
});

it("keeps an available source selector disabled consistently during a busy view update",async()=>{
 mockCoreReading();mockOptional();
 vi.mocked(workbenchAPI.sources).mockResolvedValue({reading_id:reading.reading_id,items:[{source_system_id:"source-a",display_name:"已授权来源A",status:"observed",capture_started_at:null,last_attempt_at:null,last_received_at:null,scope_label:"whole_source",gaps:[]}]});
 const user=userEvent.setup();
 render(<WorkbenchPage session={session} navigate={vi.fn()} initialConfig={scopeRequest()}/>);
 const source=screen.getByLabelText("来源") as HTMLSelectElement;
 await waitFor(()=>expect(source.disabled).toBe(false));
 let finish!:(value:WorkbenchContributions)=>void;
 vi.mocked(workbenchAPI.contributions).mockReturnValueOnce(new Promise(resolve=>{finish=resolve}));
 await user.selectOptions(screen.getByLabelText("贡献维度"),"model");
 for(const name of ["期间","来源","来源项目"])expect(screen.getByLabelText(name)).toHaveProperty("disabled",true);
 await user.selectOptions(source,"source-a");
 expect(source.value).toBe("");
 await act(async()=>finish({...topContributions,dimension:"model"}));
 await waitFor(()=>expect(source.disabled).toBe(false));
 await user.selectOptions(source,"source-a");
 await user.click(screen.getByRole("button",{name:"应用范围"}));
 await waitFor(()=>expect(workbenchAPI.createReading).toHaveBeenCalledTimes(2));
 expect(workbenchAPI.createReading).toHaveBeenLastCalledWith({...scopeRequest(),filters:{...scopeRequest().filters,source_ids:["source-a"]}},session.csrf_token);
 await waitFor(()=>expect(source.disabled).toBe(false));
 expect(source.value).toBe("source-a");
});

function scopeRequest():WorkbenchReadingRequest {
 return {contract_version:"xinsight.workbench.v1",page:"cost",metric_version:"xinsight.workbench.cost.v1",period:{start_inclusive:reading.period.start_inclusive,end_exclusive:reading.period.end_exclusive,display_timezone:"UTC"},money_basis:"gateway_charged",currency:"CNY",filters:{source_ids:[],source_projects:["project-202"],cost_projects:[]}};
}

it("settles a scoped cost reading after exact optional preference 403 without another reading",async()=>{
 const current:AccessSessionResponse={...session,scope_revision:"scope-content",session:{...session.session,role:"workbench_investigation_reviewer",permissions:[...session.session.permissions,"investigation.scoped.read"]}};
 const create=mockCoreReading();vi.spyOn(accessAPI,"session").mockResolvedValue(current);vi.spyOn(workbenchAPI,"sources").mockResolvedValue({reading_id:reading.reading_id,items:[]});
 // Exercise the real savedFilters -> requestAccessJSON -> App notification path.
 const fetch=vi.fn().mockImplementation(async()=>new Response(JSON.stringify({type:"urn:test",title:"Denied",status:403,code:"workbench_scope_denied",correlation_id:"test",retryable:false}),{status:403,headers:{"Content-Type":"application/problem+json","X-XInsight-Scope-Revision":"scope-content"}}));vi.stubGlobal("fetch",fetch);
 window.history.replaceState(null,"","/workbench/cost");
 try{render(<App/>);await screen.findByText(/当前授权不包含个人筛选/);expect(screen.getByText("project-202")).toBeTruthy();await act(async()=>{});expect(create).toHaveBeenCalledOnce();expect(fetch).toHaveBeenCalledOnce();expect(screen.queryByText("正在验证登录状态…")).toBeNull();}
 finally{cleanup();acceptScopeSession(null);vi.unstubAllGlobals();window.history.replaceState(null,"","/");}
});

it("carries the applied project scope when switching from cost to request-ranked usage",async()=>{
 mockCoreReading();mockOptional();vi.spyOn(accessAPI,"session").mockResolvedValue(session);
 window.history.replaceState(null,"","/workbench/cost");
 try{
  render(<App/>);await screen.findByText("project-202");
  const user=userEvent.setup();await user.type(screen.getByLabelText("来源项目"),"project-new");
  await user.click(screen.getByRole("button",{name:"应用范围"}));
  await waitFor(()=>expect(workbenchAPI.createReading).toHaveBeenCalledTimes(2));
  await user.click(screen.getByRole("link",{name:/使用与目的/}));
  expect(await screen.findByRole("heading",{name:"使用与目的"})).toBeTruthy();
  await waitFor(()=>expect(workbenchAPI.createReading).toHaveBeenCalledTimes(3));
  expect(workbenchAPI.createReading).toHaveBeenLastCalledWith(expect.objectContaining({filters:{source_ids:[],source_projects:["project-new"],cost_projects:[]}}),session.csrf_token);
  expect(workbenchAPI.contributions).toHaveBeenLastCalledWith(reading.reading_id,"source_project",{topN:10,orderBy:"requests"});
 }finally{cleanup();acceptScopeSession(null);window.history.replaceState(null,"","/")}
});
