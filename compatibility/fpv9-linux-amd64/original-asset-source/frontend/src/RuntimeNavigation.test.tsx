import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { App } from "./App";
import { RuntimePage } from "./RuntimePage";
import { accessAPI, runtimeAPI, ApiRequestError, acceptScopeSession, scopeSessionKey, workbenchAPI, investigationQueueAPI } from "./api";

// Keep the actual App navigation and RuntimePage. Execution rendering is outside
// these two counterexamples; its marker verifies the real route was entered.
vi.mock("./ExecutionDetailPage", () => ({
  ExecutionDetailPage: () => <h2>Manager execution destination</h2>,
}));

const base = Date.parse("2026-09-13T00:00:00Z");
const session = {
  session: { session_ref: "xr_manager_probe", subject_id: "xs_manager_probe", role: "scoped_reader",
    permissions: ["workbench.summary.read", "workbench.details.read", "investigation.scoped.read"],
    created_at: "", last_seen_at: "", idle_expires_at: "", absolute_expires_at: "" },
  csrf_token: "synthetic-manager-csrf", scope_revision: "sha256:manager-scope",
};
const coverage = { period_completeness: "unknown", timeline_complete: false, time_basis: "trusted_request_started", unknown_entry_time_basis: "authorized_observation_in_period", missing_predecessors: 0, streams: [], gaps: [] };
const request = { request_ref: "xrr_" + "a".repeat(64), source_system_id: "source", source_instance: "instance", org_id: "2", project_id: "3", request_id: "manager-request", entry_time_state: "unknown", last_observed_at: new Date(base).toISOString(), observation_count: 1 };
let creates = 0;
const readings = new Map<string, ReturnType<typeof reading>>();
function reading() {
  return { reading_id: "xrt_" + String(++creates).padStart(22, "0"), contract_version: "xinsight.runtime-reading.v1", metric_version: "xinsight.runtime.request-observations.v1", period: { start_inclusive: new Date(base - 168 * 3600000).toISOString(), end_exclusive: new Date(base).toISOString(), timezone: "UTC" }, cutoff: new Date(base).toISOString(), expires_at: new Date(base + 1800000).toISOString(), authorization_revision: "sha256:auth", interpretation_revision: "sha256:interpretation", membership_ref: "sha256:membership", requests_with_entry_time: 0, requests_with_unknown_entry_time: 1, observation_count: 1, coverage };
}
beforeEach(() => {
  // The server fixture has a fixed cutoff; keep wall time in the same clock
  // domain while leaving real navigation timers and monotonic probes intact.
  vi.spyOn(Date, "now").mockReturnValue(base);
  creates = 0;
  readings.clear();
  acceptScopeSession(null);
  vi.spyOn(window, "scrollTo").mockImplementation(() => {});
  vi.spyOn(accessAPI, "session").mockResolvedValue(session as any);
  vi.spyOn(runtimeAPI, "createReading").mockImplementation(async () => { const value = reading(); readings.set(value.reading_id, value); return value as any; });
  vi.spyOn(runtimeAPI, "reading").mockImplementation(async id => readings.get(id) as any);
  vi.spyOn(runtimeAPI, "summary").mockImplementation(async (id, entry) => ({ reading_id: id, summary_version: "v1", interpretation_revision: "sha256:interpretation", entry_time: entry, requests: 1, outcomes: [], diagnostic_failure: { state: "not_computable", numerator: 0, denominator: 0, excluded_requests: 1, basis: "diagnostic" }, coverage }));
  vi.spyOn(runtimeAPI, "dimensions").mockImplementation(async (id, dimension, entry) => ({ reading_id: id, dimensions_version: "v1", interpretation_revision: "sha256:interpretation", dimension, entry_time: entry, requests: 1, requests_with_values: 0, requests_without_values: 1, missing_predecessors: 0, groups_may_overlap: true, items: [] }));
  vi.spyOn(runtimeAPI, "measurements").mockImplementation(async id => ({ reading_id: id, eligibility_version: "v1", interpretation_revision: "sha256:interpretation", items: [] }));
  vi.spyOn(runtimeAPI, "requests").mockImplementation(async id => ({ reading_id: id, items: [request] } as any));
  vi.spyOn(runtimeAPI, "observations").mockImplementation(async id => ({ reading_id: id, items: [] }));
  vi.spyOn(runtimeAPI, "resources").mockImplementation(async (id, ref) => ({ reading_id: id, request_ref: ref, resources_version: "xinsight.runtime.resources.v1", membership_ref: "sha256:membership", coverage: "synthetic_internal_records_only", items: [] }));
  vi.spyOn(runtimeAPI, "associations").mockImplementation(async (id, ref) => ({ reading_id: id, request_ref: ref, associations_version: "xinsight.runtime.associations.v1", membership_ref: "sha256:membership", basis: "same_source_request_identity", items: [{ kind: "economic", economic: { economic_fact_id: "economic-one", execution_id: "xe1_demo_6d636f6c6c6973696f6e", source_event_hash: "sha256:economic" } }] }));
});
afterEach(() => { cleanup(); acceptScopeSession(null); vi.restoreAllMocks(); vi.useRealTimers(); });

it("does not revive a lease whose response arrived after its server deadline", async () => {
  let clock = 0;
  vi.spyOn(performance, "now").mockImplementation(() => clock);
  vi.mocked(runtimeAPI.createReading).mockRejectedValueOnce(new ApiRequestError(403));
  let resolve!: (value: any) => void;
  vi.spyOn(runtimeAPI, "current").mockImplementation(() => new Promise(r => { resolve = r; }));
  await act(async () => { render(<RuntimePage session={session as any} />); });
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "查看当前筛选的租约观察" })); });
  // Request began at 0 ms. Server evaluated at t0 with only 1 s of lease left.
  // Network delayed this otherwise valid response by 5 s before it arrived.
  clock = 5000;
  await act(async () => { resolve({ version: "v1", evaluated_at: new Date(base).toISOString(), coverage: "lease_backed_inventory", uncovered: [], items: [{ ref: "current-one", source_system_id: "source", source_instance: "instance", org_id: "2", project_id: "3", snapshot_observed_at: new Date(base).toISOString(), received_at: new Date(base).toISOString(), source_sha256: "sha256:source", current_state: "lease_valid_execution", entry: { request_id: "delayed-request", family: "stream", subject_kind: "provider_attempt", subject_id: "attempt-one", operation: "chat", freshness: "lease_valid_execution", original_state: "streaming", last_observed_at: new Date(base).toISOString(), valid_until: new Date(base + 1000).toISOString(), evidence_ref: "lease" } }] }); });
  const actual = screen.getByRole("heading", { name: /delayed-request/ }).textContent;
  console.log("lease response after 5000 ms; valid for 1000 ms; displayed:", actual);
  expect(actual).toBe("delayed-request · 当前状态未确认");
});

it.each(["/workbench/runtime", "/workbench/runtime?view=capabilities"])("returns from execution to the same reading, filters and cohort: %s", async path => {
  window.history.replaceState(null, "", path);
  render(<App />);
  await screen.findByRole("button", { name: "manager-request" });
  fireEvent.change(screen.getByLabelText("时间范围"), { target: { value: "168" } });
  fireEvent.change(screen.getByLabelText("来源项目"), { target: { value: "project-3" } });
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "读取历史" })); });
  await act(async () => { fireEvent.change(screen.getByLabelText("请求时间口径"), { target: { value: "unknown" } }); });
  const before = { creates: vi.mocked(runtimeAPI.createReading).mock.calls.length, period: (screen.getByLabelText("时间范围") as HTMLSelectElement).value, project: (screen.getByLabelText("来源项目") as HTMLInputElement).value, cohort: (screen.getByLabelText("请求时间口径") as HTMLSelectElement).value };
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "manager-request" })); });
  fireEvent.click(await screen.findByRole("button", { name: "查看执行与来源轨迹" }));
  expect(screen.getByRole("heading", { name: "Manager execution destination" })).toBeTruthy();
  await act(async () => { fireEvent.click(screen.getByRole("link", { name: /返回.*原页面/ })); });
  await screen.findByRole("button", { name: "manager-request" });
  const after = { creates: vi.mocked(runtimeAPI.createReading).mock.calls.length, period: (screen.getByLabelText("时间范围") as HTMLSelectElement).value, project: (screen.getByLabelText("来源项目") as HTMLInputElement).value, cohort: (screen.getByLabelText("请求时间口径") as HTMLSelectElement).value };
  console.log("runtime return:", JSON.stringify({ before, after }));
  expect(after).toEqual(before);
  expect(runtimeAPI.reading).toHaveBeenCalledWith([...readings.keys()][1], expect.any(AbortSignal));
  expect(screen.getByRole("heading", { name: "请求观察详情" })).toBeTruthy();
  expect(window.location.pathname + window.location.search).toBe(path);
  // Exercise native history in both directions, then browser-back to the same
  // saved selection. This also rechecks the same server reading each time.
  await act(async () => { window.history.back(); });
  await screen.findByRole("heading", { name: "Manager execution destination" });
  await act(async () => { window.history.forward(); });
  await screen.findByRole("heading", { name: "请求观察详情" });
  expect((screen.getByLabelText("请求时间口径") as HTMLSelectElement).value).toBe("unknown");
  fireEvent.click(await screen.findByRole("button", { name: "查看执行与来源轨迹" }));
  await screen.findByRole("heading", { name: "Manager execution destination" });
  await act(async () => { window.history.back(); });
  await screen.findByRole("heading", { name: "请求观察详情" });
  expect(runtimeAPI.createReading).toHaveBeenCalledTimes(before.creates);
  expect((screen.getByLabelText("来源项目") as HTMLInputElement).value).toBe("project-3");
});

// Navigation must not silently replace a revoked, expired or mismatched reading.
it.each([403, 409, 410])("invalidates a saved reading on return with %i", async status => {
  window.history.replaceState(null, "", "/workbench/runtime");
  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "manager-request" }));
  fireEvent.click(await screen.findByRole("button", { name: "查看执行与来源轨迹" }));
  await screen.findByRole("heading", { name: "Manager execution destination" });
  vi.mocked(runtimeAPI.reading).mockRejectedValueOnce(new ApiRequestError(status));
  await act(async () => { window.history.back(); });
  await screen.findByRole("alert");
  expect(screen.queryByRole("button", { name: "manager-request" })).toBeNull();
  expect(runtimeAPI.createReading).toHaveBeenCalledOnce();
  // Leaving/returning again keeps the failure; it cannot bootstrap a new view.
  await act(async () => { window.history.forward(); });
  await screen.findByRole("heading", { name: "Manager execution destination" });
  await act(async () => { window.history.back(); });
  await screen.findByRole("alert");
  expect(runtimeAPI.createReading).toHaveBeenCalledOnce();
});

it("retains the original monotonic deadline while an execution is open", async () => {
  let clock = 0;
  vi.spyOn(performance, "now").mockImplementation(() => clock);
  window.history.replaceState(null, "", "/workbench/runtime");
  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "manager-request" }));
  fireEvent.click(await screen.findByRole("button", { name: "查看执行与来源轨迹" }));
  await screen.findByRole("heading", { name: "Manager execution destination" });
  clock = 1800001;
  await act(async () => { window.history.back(); });
  await screen.findByText("阅读已失效，请重新读取。");
  expect(runtimeAPI.reading).not.toHaveBeenCalled();
  expect(runtimeAPI.createReading).toHaveBeenCalledOnce();
});

it("keeps overview's fixed seven-day scope through runtime, execution, topic return and overview return", async () => {
  const fixed = { start_inclusive: new Date(base - 168 * 3600000).toISOString(), end_exclusive: new Date(base).toISOString(), display_timezone: "UTC" };
  const filters = { source_ids: ["visible-source"], source_projects: ["project-3"], cost_projects: ["cost-3"] };
  const overview = { hours: 168, contribution: "source_project", request: { contract_version: "xinsight.workbench.v1", page: "cost", metric_version: "xinsight.workbench.cost.v1", period: fixed, money_basis: "gateway_charged", currency: "CNY", filters } };
  const wbCoverage = { status: "scope_limited_unknown", period_completeness: "unknown", projection_complete_for_members: true, known_items: 2, gaps: [] };
  vi.spyOn(workbenchAPI,"createReading").mockResolvedValue({ ...overview.request, reading_id:"reading-overview", period:{...fixed,timezone:"UTC"}, cutoff:new Date(base).toISOString(), expires_at:new Date(base+1800000).toISOString(), membership_ref:"members",authorization_revision:"auth",interpretation_revision:"interpretation",money_title:"网关已计金额",dimensions:[],coverage:wbCoverage } as any);
  vi.spyOn(workbenchAPI,"summary").mockResolvedValue({ reading_id:"reading-overview",request_count:2,selected_amount_micros:"0",amount_complete:false,request_total_complete:false,coverage:wbCoverage } as any);
  vi.spyOn(workbenchAPI,"series").mockImplementation(async(id,_signal,bucket,metric="amount")=>({reading_id:id,metric,unit:metric==="requests"?"request":"micros",bucket_seconds:bucket??3600,points:[],coverage:wbCoverage}) as any);
  vi.spyOn(investigationQueueAPI,"list").mockResolvedValue({items:[],total:0,evaluated_at:new Date(base).toISOString(),coverage:"authorized"} as any);
  window.history.replaceState({overviewContext:overview,overviewIdentity:scopeSessionKey(session as any)},"","/inbox");
  render(<App/>);
  await screen.findByRole("heading",{name:"总览"});
  await act(async()=>fireEvent.click(screen.getByRole("button",{name:"查看受影响请求与可比时延"})));
  await screen.findByRole("button",{name:"manager-request"});
  expect(vi.mocked(runtimeAPI.createReading).mock.calls.at(-1)?.[0]).toMatchObject({period:fixed,filters});
  expect((screen.getByLabelText("时间范围") as HTMLSelectElement).value).toBe("inherited");
  fireEvent.click(screen.getByRole("button",{name:"manager-request"}));
  fireEvent.click(await screen.findByRole("button",{name:"查看执行与来源轨迹"}));
  await screen.findByRole("heading",{name:"Manager execution destination"});
  const beforeReturn=vi.mocked(runtimeAPI.createReading).mock.calls.length;
  fireEvent.click(screen.getByRole("link",{name:/返回.*原页面/}));
  await screen.findByRole("heading",{name:"请求观察详情"});
  expect(runtimeAPI.createReading).toHaveBeenCalledTimes(beforeReturn);
  expect((screen.getByLabelText("来源项目") as HTMLInputElement).value).toBe("project-3");
  expect((screen.getByLabelText("时间范围") as HTMLSelectElement).value).toBe("inherited");
  fireEvent.click(screen.getByRole("button",{name:"← 返回总览"}));
  await screen.findByRole("heading",{name:"总览"});
  expect((screen.getByLabelText("期间") as HTMLSelectElement).value).toBe("168");
  expect((screen.getByLabelText("来源筛选") as HTMLInputElement).value).toBe("visible-source");
  expect(vi.mocked(workbenchAPI.createReading).mock.calls.at(-1)?.[0]).toMatchObject({period:fixed,filters});
});
