import { App } from "./App";
import * as content from "./content";
import { navigationOwner, readRuntimeNavigation, type RuntimeNavigation } from "./workbenchNavigation";
import { act, cleanup, fireEvent, render, screen, within, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { accessAPI, ApiRequestError, runtimeAPI, type AccessSessionResponse } from "./api";
import { ObservationCard, RuntimePage, RuntimeRequestDetail } from "./RuntimePage";
import type * as Runtime from "./runtime";
import observationSchema from "../../internal/sourcefacts/contract/observations/source-observation.schema.json";

const session: AccessSessionResponse = { session: { session_ref: "session", subject_id: "reader", role: "workbench_reader", permissions: ["workbench.summary.read", "workbench.details.read"], created_at: "", last_seen_at: "", idle_expires_at: "", absolute_expires_at: "" }, csrf_token: "csrf" };
function reading(id = "reading-a"): Runtime.Reading {
  const now = new Date().toISOString();
  return { reading_id: id, contract_version: "xinsight.runtime-reading.v1", metric_version: "xinsight.runtime.request-observations.v1", period: { start_inclusive: now, end_exclusive: now, timezone: "UTC" }, cutoff: now, expires_at: new Date(Date.now() + 30 * 60_000).toISOString(), authorization_revision: "sha256:auth", interpretation_revision: "sha256:interpretation", membership_ref: "sha256:membership", requests_with_entry_time: 4, requests_with_unknown_entry_time: 1, observation_count: 12, coverage: { period_completeness: "unknown", timeline_complete: false, time_basis: "trusted_request_started", unknown_entry_time_basis: "authorized_observation_in_period", missing_predecessors: 0, streams: [], gaps: [] } };
}
function summary(id: string, entry: Runtime.EntryTime): Runtime.Summary {
  return { reading_id: id, summary_version: "v1", interpretation_revision: "sha256:rule", entry_time: entry, requests: entry === "unknown" ? 1 : 4, outcomes: [{ outcome: "local_delivery_completed", requests: 3 }, { outcome: "technical_failure", requests: 1 }], diagnostic_failure: { state: "computed", numerator: 1, denominator: 4, excluded_requests: 0, basis: "diagnostic" }, coverage: reading().coverage };
}
function dimensions(id: string, dimension: Runtime.Dimension, entry: Runtime.EntryTime): Runtime.Dimensions {
  return { reading_id: id, dimensions_version: "v1", interpretation_revision: "sha256:rule", dimension, entry_time: entry, requests: 4, requests_with_values: 3, requests_without_values: 1, missing_predecessors: 1, groups_may_overlap: true, items: [{ value: "model-first", evidence_state: "partial", requests: 2, subjects: 3, observation_count: 3 }] };
}
const request: Runtime.Request = { request_ref: "ref-one", source_system_id: "source", source_instance: "instance", org_id: "2", project_id: "3", request_id: "request-one", entry_time_state: "observed", entry_at: "2026-09-10T00:00:00Z", last_observed_at: "2026-09-10T00:00:00Z", observation_count: 2 };
const observation: Runtime.Observation = { representation: "authorized_projection", request_ref: "ref-one", source_system_id: "source", source_hash: "sha256:source", envelope_hash: "sha256:envelope", chain_state: "complete", document: { schema_version: "v1", source_instance: "instance", org_id: "2", project_id: "3", request_id: "request-one", profile: "attempt", subject_kind: "provider_attempt", subject_id: "attempt-one", observation_kind: "provider_rejected", source_fact_id: "fact", revision: "2", event_id: "event", occurred_at: "2026-09-10T00:00:00Z", recorded_at: "2026-09-10T00:00:01Z", coverage: "observed", capture_expected: [], attempt: { attempt_id: "attempt-one", operation: "chat", provider: "rejected", local_delivery: "not_started", financial: "reserved", retry_authorized: true, evidence_ref: "attempt-one" } } };
beforeEach(() => {
  vi.spyOn(runtimeAPI, "createReading").mockResolvedValue(reading());
  vi.spyOn(runtimeAPI, "summary").mockImplementation(async (id, entry) => summary(id, entry));
  vi.spyOn(runtimeAPI, "dimensions").mockImplementation(async (id, dim, entry) => dimensions(id, dim, entry));
  vi.spyOn(runtimeAPI, "measurements").mockImplementation(async id => ({ reading_id: id, eligibility_version: "v1", interpretation_revision: "sha256:rule", items: [{ operation: "chat", stream_mode: "streaming", measurement_kind: "latency", measurement_version: "v1", completion_evidence: "local_completed", entry_time_state: "observed", observation_count: 1, excluded: { not_applicable: 0, not_observed: 2, legacy_unknown: 0, invalid: 0, missing_predecessor: 0, missing_context: 0, conflicting_context: 0, incompatible_context: 0 }, percentiles: { state: "computed", version: "v1", unit: "milliseconds", sample_count: 1, p50: "0", p95: "0" } }] }));
  vi.spyOn(runtimeAPI, "requests").mockImplementation(async id => ({ reading_id: id, items: [request] }));
  vi.spyOn(runtimeAPI, "observations").mockImplementation(async id => ({ reading_id: id, items: [observation] }));
  vi.spyOn(runtimeAPI, "resources").mockImplementation(async (id, ref) => ({ reading_id: id, request_ref: ref, resources_version: "xinsight.runtime.resources.v1", membership_ref: reading().membership_ref, coverage: "synthetic_internal_records_only", items: [] }));
  vi.spyOn(runtimeAPI, "associations").mockImplementation(async (id, ref) => ({ reading_id: id, request_ref: ref, associations_version: "xinsight.runtime.associations.v1", membership_ref: reading().membership_ref, basis: "same_source_request_identity", items: [] }));
  vi.spyOn(runtimeAPI, "current").mockResolvedValue({ version: "v1", evaluated_at: new Date().toISOString(), coverage: "lease_backed_inventory", uncovered: [], items: [] });
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.useRealTimers(); });

it("uses one independent cohort for outcome, dimension, measurement and request reads and keeps observed zero", async () => {
  const user = userEvent.setup();
  render(<RuntimePage session={session} />);
  expect(await screen.findByText("1 / 4")).toBeTruthy();
  expect(screen.getAllByText("0").length).toBeGreaterThanOrEqual(2);
  expect(screen.getByText(/独立请求 4；有可用维度值 3，其余 1/)).toBeTruthy();
  expect(runtimeAPI.current).not.toHaveBeenCalled();
  await user.selectOptions(screen.getByLabelText("请求时间口径"), "unknown");
  await waitFor(() => expect(runtimeAPI.measurements).toHaveBeenLastCalledWith("reading-a", "unknown", undefined, expect.any(AbortSignal)));
  expect(runtimeAPI.summary).toHaveBeenLastCalledWith("reading-a", "unknown", expect.any(AbortSignal));
  expect(runtimeAPI.dimensions).toHaveBeenLastCalledWith("reading-a", "operation", "unknown", undefined, expect.any(AbortSignal));
  expect(runtimeAPI.requests).toHaveBeenLastCalledWith("reading-a", "unknown", undefined, expect.any(AbortSignal));
  expect(screen.getByText(/没有可信开始时间/)).toBeTruthy();
});

it("rejects a late old-reading dimension response after applying new filters", async () => {
  let resolve!: (x: Runtime.Dimensions) => void;
  vi.mocked(runtimeAPI.dimensions).mockImplementationOnce(() => new Promise(r => { resolve = r; }));
  const user = userEvent.setup();
  render(<RuntimePage session={session} />);
  await screen.findByText("1 / 4");
  vi.mocked(runtimeAPI.createReading).mockResolvedValueOnce(reading("reading-b"));
  await user.type(screen.getByLabelText("来源项目"), "project-3, project-4");
  await user.click(screen.getByRole("button", { name: "读取历史" }));
  await screen.findByText("model-first");
  expect(runtimeAPI.createReading).toHaveBeenLastCalledWith(expect.objectContaining({ filters: { source_ids: [], source_projects: ["project-3", "project-4"], cost_projects: [] } }), "csrf", expect.any(AbortSignal));
  await act(async () => { resolve({ ...dimensions("reading-a", "operation", "known"), items: [{ value: "stale-secret-model", evidence_state: "partial", requests: 1, subjects: 1, observation_count: 1 }] }); });
  expect(screen.queryByText("stale-secret-model")).toBeNull();
});

it("retries the failed frozen cursor and removes the whole reading after a revoked page", async () => {
  vi.mocked(runtimeAPI.dimensions).mockResolvedValueOnce({ ...dimensions("reading-a", "operation", "known"), next_cursor: "cursor-2" }).mockRejectedValueOnce(new Error("offline")).mockRejectedValueOnce(new ApiRequestError(409));
  const user = userEvent.setup();
  render(<RuntimePage session={session} />);
  await user.click(await screen.findByRole("button", { name: "加载更多维度" }));
  expect(screen.getByText("model-first")).toBeTruthy();
  await user.click(await screen.findByRole("button", { name: "重试维度" }));
  await screen.findByText("阅读已失效，请重新读取。");
  expect(runtimeAPI.dimensions).toHaveBeenLastCalledWith("reading-a", "operation", "known", "cursor-2", expect.any(AbortSignal));
  expect(screen.queryByText("model-first")).toBeNull();
  expect(screen.queryByText("request-one")).toBeNull();
});

it("shows attempt evidence independently and does not display incomplete fields", async () => {
  const user = userEvent.setup();
  render(<RuntimePage session={session} />);
  await user.click(await screen.findByRole("button", { name: "request-one" }));
  expect(await screen.findByText(/一次尝试失败不等于请求失败/)).toBeTruthy();
  expect(runtimeAPI.observations).toHaveBeenCalledWith("reading-a", "observations", "ref-one", undefined, expect.any(AbortSignal));
  cleanup();
  render(<ObservationCard observation={{ ...observation, chain_state: "missing_predecessor", document: { ...observation.document, attempt: { ...observation.document.attempt!, operation: "must-not-be-displayed" } } }} timezone="UTC" />);
  expect(screen.getByText(/前驱尚未收齐/)).toBeTruthy();
  expect(screen.queryByText("must-not-be-displayed")).toBeNull();
});

it("explains a request-start-only reading without turning a client error into provider failure", async () => {
  const started: Runtime.Observation = { ...observation, document: { ...observation.document,
    profile: "unirouter.runtime-observation.v1", subject_kind: "request", subject_id: request.request_id,
    observation_kind: "request_started", attempt: undefined,
    runtime: { operation: "openai.chat_completions.v1", stream_mode: "not_observed", admission: "authenticated", provider: "not_started", local_delivery: "not_started", financial: "not_created", capture: "observed" },
  } };
  vi.mocked(runtimeAPI.observations).mockResolvedValue({ reading_id: "reading-a", items: [started] });
  render(<RuntimeRequestDetail reading={reading()} request={{ ...request, outcome: "unresolved", outcome_basis: "request_level_observation" }} view="story" onInvalidated={vi.fn()} />);
  expect(await screen.findByText(/已记录请求开始，但缺少结束证据/)).toBeTruthy();
  expect(screen.getByText(/该时点提供方状态为“未开始”/)).toBeTruthy();
  expect(screen.getByText(/保存的客户端请求或错误响应不证明模型调用/)).toBeTruthy();
  expect(screen.queryByText("提供方失败")).toBeNull();
});

it("keeps current inventory independent of denied historical reads and never interprets empty inventory as zero activity", async () => {
  vi.mocked(runtimeAPI.createReading).mockRejectedValueOnce(new ApiRequestError(403));
  const user = userEvent.setup();
  render(<RuntimePage session={session} />);
  await screen.findByText("当前没有这部分资料的读取权限。");
  await user.click(screen.getByRole("button", { name: "查看当前筛选的租约观察" }));
  expect(await screen.findByText(/不能据此断言没有运行中的请求/)).toBeTruthy();
  expect(runtimeAPI.current).toHaveBeenCalledOnce();
});

it("expires a visible lease using server time without polling or granting a live global count", async () => {
  vi.useFakeTimers();
  const now = Date.now();
  vi.mocked(runtimeAPI.current).mockResolvedValueOnce({ version: "v1", evaluated_at: new Date(now).toISOString(), coverage: "lease_backed_inventory", uncovered: [], items: [{ ref: "current-one", source_system_id: "source", source_instance: "instance", org_id: "2", project_id: "3", snapshot_observed_at: new Date(now).toISOString(), received_at: new Date(now).toISOString(), source_sha256: "sha256:source", current_state: "lease_valid_execution", entry: { request_id: "leased-request", family: "stream", subject_kind: "provider_attempt", subject_id: "attempt-one", operation: "chat", freshness: "lease_valid_execution", original_state: "streaming", last_observed_at: new Date(now).toISOString(), valid_until: new Date(now + 1000).toISOString(), evidence_ref: "lease" } }] });
  await act(async () => { render(<RuntimePage session={session} />); });
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "查看当前筛选的租约观察" })); });
  expect(screen.getByRole("heading", { name: "leased-request · 执行租约有效" })).toBeTruthy();
  await act(async () => { vi.advanceTimersByTime(1500); });
  expect(screen.getByRole("heading", { name: "leased-request · 当前状态未确认" })).toBeTruthy();
  expect(runtimeAPI.current).toHaveBeenCalledOnce();
});

it("switches to the capability topic without rebuilding the reading", async () => {
  const ui = render(<RuntimePage session={session} />);
  await screen.findByText("model-first");
  ui.rerender(<RuntimePage session={session} capabilities />);
  await waitFor(() => expect(runtimeAPI.dimensions).toHaveBeenLastCalledWith("reading-a", "selected_model", "known", undefined, expect.any(AbortSignal)));
  expect(screen.getByRole("heading", { name: "模型与能力" })).toBeTruthy();
  expect(runtimeAPI.createReading).toHaveBeenCalledOnce();
  expect(screen.queryByRole("heading", { name: "租约观察" })).toBeNull();
});

it("expires a reading by its server lifetime even when the workstation clock is behind", async () => {
  vi.useFakeTimers();
  const remote = Date.now() + 86_400_000;
  vi.mocked(runtimeAPI.createReading).mockResolvedValueOnce({ ...reading(), cutoff: new Date(remote).toISOString(), expires_at: new Date(remote + 1000).toISOString() });
  await act(async () => { render(<RuntimePage session={session} />); });
  expect(screen.getByText("model-first")).toBeTruthy();
  await act(async () => { vi.advanceTimersByTime(1500); });
  expect(screen.getByText("阅读已失效，请重新读取。")).toBeTruthy();
  expect(screen.queryByText("model-first")).toBeNull();
});

it("binds source associations to the frozen reading and opens the existing execution journey", async () => {
 const user=userEvent.setup(), navigate=vi.fn();
 vi.mocked(runtimeAPI.associations).mockResolvedValueOnce({ reading_id:"reading-a",request_ref:request.request_ref,associations_version:"xinsight.runtime.associations.v1",membership_ref:reading().membership_ref,basis:"same_source_request_identity",items:[{kind:"economic",economic:{economic_fact_id:"economic-one",execution_id:"xe1_existing",source_event_hash:"sha256:economic"}}] });
 render(<RuntimePage session={session} navigate={navigate} canInvestigate />);
 await user.click(await screen.findByRole("button",{name:"request-one"}));
 await user.click(await screen.findByRole("button",{name:"查看执行与来源轨迹"}));
 expect(navigate).toHaveBeenCalledWith(expect.stringMatching(/^\/executions\/xe1_existing\?as_of=/));
 expect(screen.getByText("economic-one")).toBeTruthy();
 cleanup();
 vi.mocked(runtimeAPI.associations).mockResolvedValueOnce({reading_id:"reading-a",request_ref:request.request_ref,associations_version:"xinsight.runtime.associations.v1",membership_ref:"sha256:wrong",basis:"same_source_request_identity",items:[]});
 render(<RuntimePage session={session} />);
 await user.click(await screen.findByRole("button",{name:"request-one"}));
 await screen.findByText("阅读已失效，请重新读取。");
 expect(screen.queryByRole("button",{name:"request-one"})).toBeNull();
});

it("keeps per-page lease deadlines through skewed server times, pagination and failed refresh", async () => {
  let clock = 0;
  vi.spyOn(performance, "now").mockImplementation(() => clock);
  const server = Date.now() + 86_400_000;
  const page = (ref: string, evaluated: number, lifetime: number, cursor?: string): Runtime.CurrentInventory => ({
    version: "v1", evaluated_at: new Date(evaluated).toISOString(), coverage: "lease_backed_inventory", uncovered: [], next_cursor: cursor,
    items: [{ ref, source_system_id: "source", source_instance: "instance", org_id: "2", project_id: "3", snapshot_observed_at: new Date(evaluated).toISOString(), received_at: new Date(evaluated).toISOString(), source_sha256: "sha256:source", current_state: "lease_valid_execution", entry: { request_id: ref, family: "stream", subject_kind: "provider_attempt", subject_id: "attempt", operation: "chat", freshness: "lease_valid_execution", original_state: "streaming", last_observed_at: new Date(evaluated).toISOString(), valid_until: new Date(evaluated + lifetime).toISOString(), evidence_ref: "lease" } }],
  });
  vi.mocked(runtimeAPI.current).mockResolvedValueOnce(page("page-one", server, 1000, "cursor-2"))
    .mockResolvedValueOnce(page("page-two", server - 3_600_000, 10_000, "cursor-3"))
    .mockRejectedValueOnce(new Error("network unavailable"))
    .mockRejectedValueOnce(new Error("refresh unavailable"));
  render(<RuntimePage session={session} />);
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "查看当前筛选的租约观察" })); });
  expect(screen.getByRole("heading", { name: "page-one · 执行租约有效" })).toBeTruthy();
  clock = 1500;
  vi.spyOn(Date, "now").mockReturnValue(server - 86_400_000 * 3);
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "加载更多租约观察" })); });
  expect(screen.getByRole("heading", { name: "page-one · 当前状态未确认" })).toBeTruthy();
  expect(screen.getByRole("heading", { name: "page-two · 执行租约有效" })).toBeTruthy();
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "加载更多租约观察" })); });
  expect(screen.getByRole("heading", { name: "page-two · 当前状态未确认" })).toBeTruthy();
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "刷新租约观察" })); });
  expect(screen.queryByRole("heading", { name: /执行租约有效/ })).toBeNull();
});


function resourcePage(id: string, ref: string): Runtime.Resources {
  const scope = { source_system_id: request.source_system_id, source_instance: request.source_instance, org_id: request.org_id, project_id: request.project_id };
  return { resources_version: "xinsight.runtime.resources.v1", reading_id: id, request_ref: ref, membership_ref: reading().membership_ref, coverage: "synthetic_internal_records_only", items: [{
    record: { schema_version: "xinsight.resource-access.internal.v1", synthetic: true, record_ref: "pc-read", revision: 1, reporter: scope, target: scope,
      request_id: request.request_id, application_ref: "synthetic-app", actor_ref: "synthetic-user", occurred_at: "2026-09-10T00:00:00Z", report_kind: "client_assertion",
      resource: { ref: "document-1", version: "v2", name: "资源测试资料", location: "pc", ownership: "unknown", classification: "unknown", device_management: "unknown" },
      action: "read", access_outcome: "read", access_decision: "unknown", withdrawn: false,
      input: { fragment_ref: "fragment-1", fragment_version: "v2.1", attempt_id: "attempt-1", attempt_kind: "provider_attempt", reported_input_ref: "input-2",
        transforms: [{ kind: "redact", input_version: "v2.1", output_version: "redacted-v1", receipt_ref: "redaction-claim" }] } },
    record_hash: "sha256:resource", chain_state: "complete", input_state: "reference_mismatch", content_verification: "not_performed", input_evidence_basis: "frozen_reference_witnesses", changed_since_reading: false, captured_input_evidence_refs: ["destination-witness"] }] };
}
it("keeps resource location, unknown ownership, reported redaction and input mismatch separate, then clears revoked data", async () => {
  vi.mocked(runtimeAPI.resources).mockImplementationOnce(async (id, ref) => ({ ...resourcePage(id, ref), next_cursor: "resource-next" })).mockRejectedValueOnce(new ApiRequestError(409));
  const user = userEvent.setup();
  render(<RuntimePage session={session} />);
  await user.click(await screen.findByRole("button", { name: request.request_id }));
  await user.click(await screen.findByRole("button", { name: "资源测试资料" }));
  await screen.findByText("资源测试资料 · 合成验证");
  expect(within(screen.getByRole("complementary",{name:"所选资源属性"})).getByText("输入引用不匹配")).toBeTruthy();
  expect(within(screen.getByRole("complementary",{name:"所选资源属性"})).getByText("个人电脑")).toBeTruthy();
  expect(screen.getByText("所有权").parentElement?.textContent).toBe("所有权未知");
  expect(screen.getByText("外发许可").parentElement?.textContent).toBe("外发许可未由本记录证明");
  expect(screen.getByText("内容与转换效果核验").parentElement?.textContent).toBe("内容与转换效果核验未执行独立核验");
  expect(screen.getByText(/脱敏：v2.1 → redacted-v1；依据 redaction-claim/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "加载更多资源记录" }));
  await screen.findByText("阅读已失效，请重新读取。");
  expect(screen.queryByText("资源测试资料 · 合成验证")).toBeNull();
  expect(runtimeAPI.resources).toHaveBeenLastCalledWith("reading-a", request.request_ref, "resource-next", expect.any(AbortSignal));
});
it("rejects a resource response with a different full target identity before displaying it", async () => {
  vi.mocked(runtimeAPI.resources).mockImplementationOnce(async (id, ref) => {
    const page = resourcePage(id, ref);page.items[0].record.target = { ...page.items[0].record.target, org_id: "another-org" };return page;
  });
  const user = userEvent.setup();
  render(<RuntimePage session={session} />);
  await user.click(await screen.findByRole("button", { name: request.request_id }));
  await screen.findByText("阅读已失效，请重新读取。");
  expect(screen.queryByText("资源测试资料 · 合成验证")).toBeNull();
});

it("keeps scope gaps visible before the result and measurement comparison", async () => {
  const value = reading();
  vi.mocked(runtimeAPI.createReading).mockResolvedValue({ ...value, coverage: { ...value.coverage, missing_predecessors: 2 } });
  render(<RuntimePage session={session} />);
  await screen.findByText("1 / 4");
  expect(screen.getByText(/2 条观察的前驱尚未收齐/).closest("details")).toBeNull();
  expect(screen.getByText(/进入时间未知 1 个请求/).closest("details")).toBeNull();
  const result = screen.getByRole("heading", { name: "请求结果" }).closest("section");
  const measure = screen.getByRole("heading", { name: "可比较测量" }).closest("section");
  expect(result?.parentElement).toBe(measure?.parentElement);
  expect(screen.getByText("筛选、阅读凭据与来源范围").closest("details")?.open).toBe(false);
});

it("inherits the absolute overview range without resetting it when other filters change", async () => {
  const user = userEvent.setup();
  const initialRequest: Runtime.ReadingRequest = { contract_version: "xinsight.runtime-reading.v1", metric_version: "xinsight.runtime.request-observations.v1", period: { start_inclusive: "2026-09-01T00:00:00Z", end_exclusive: "2026-09-08T00:00:00Z", display_timezone: "UTC" }, filters: { source_ids: ["source-a"], source_projects: ["project-a"], cost_projects: ["cost-a"] } };
  const ui = render(<RuntimePage session={session} initialRequest={initialRequest} />);
  await screen.findByText("1 / 4");
  expect(runtimeAPI.createReading).toHaveBeenCalledWith(initialRequest, session.csrf_token, expect.any(AbortSignal));
  expect(screen.getByDisplayValue("来源期间（固定起止）")).toBeTruthy();
  expect(screen.getByDisplayValue("project-a")).toBeTruthy();
  ui.rerender(<RuntimePage session={session} initialRequest={{ ...initialRequest, period: { relative_hours: 24, display_timezone: "UTC" } }} />);
  expect(runtimeAPI.createReading).toHaveBeenCalledOnce();
  await user.clear(screen.getByLabelText("来源项目"));
  await user.type(screen.getByLabelText("来源项目"), "project-b");
  await user.click(screen.getByRole("button", { name: "读取历史" }));
  await screen.findByText("1 / 4");
  expect(runtimeAPI.createReading).toHaveBeenLastCalledWith({ ...initialRequest, filters: { ...initialRequest.filters, source_projects: ["project-b"] } }, session.csrf_token, expect.any(AbortSignal));
  await user.selectOptions(screen.getByLabelText("时间范围"), "168");
  await user.click(screen.getByRole("button", { name: "读取历史" }));
  await screen.findByText("1 / 4");
  expect(runtimeAPI.createReading).toHaveBeenLastCalledWith(expect.objectContaining({ period: { relative_hours: 168, display_timezone: "UTC" } }), session.csrf_token, expect.any(AbortSignal));
});


it("compares two resources with the same record ref from different reporters and only opens the selected properties",async()=>{
 const page=resourcePage("reading-a",request.request_ref),first=page.items[0];
 const second:Runtime.ResourceItem={...first,record:{...first.record,reporter:{...first.record.reporter,source_instance:"other-reporter-instance"},resource:{...first.record.resource,name:"第二份资源",version:"v3",classification:"known",classification_ref:"内部资料"},access_decision:"deny",actor_ref:"second-actor",input:undefined},changed_since_reading:true,record_hash:"sha256:second"};
 page.items.push(second);vi.mocked(runtimeAPI.resources).mockResolvedValue(page);
 const user=userEvent.setup();render(<RuntimeRequestDetail reading={reading()} request={request} view="systems" onInvalidated={vi.fn()}/>);
 const firstRow=await screen.findByRole("button",{name:"资源测试资料"});
 const secondRow=screen.getByRole("button",{name:"第二份资源"});
 const list=screen.getByRole("table",{name:"本次阅读的资源清单 · 合成验证资料"});
 expect(within(list).getAllByRole("row")).toHaveLength(3);
 expect(within(list).getByRole("columnheader",{name:"来源读取许可"})).toBeTruthy();expect(within(list).getByRole("columnheader",{name:"敏感性分类"})).toBeTruthy();
 expect(within(secondRow.closest("tr")!).getByText("拒绝")).toBeTruthy();expect(within(secondRow.closest("tr")!).getByText("内部资料")).toBeTruthy();
 expect(within(list).getByText("存在后续修订，保留本次阅读")).toBeTruthy();
 const panel=screen.getByRole("complementary",{name:"所选资源属性"});
 expect(within(panel).queryByRole("heading")).toBeNull();
 await user.click(firstRow);
 expect(within(panel).getByRole("heading",{name:"资源测试资料 · 合成验证"})).toBeTruthy();
 expect(within(panel).getByText(/redaction-claim/)).toBeTruthy();
 await user.click(secondRow);
 expect(firstRow.getAttribute("aria-pressed")).toBe("false");expect(secondRow.getAttribute("aria-pressed")).toBe("true");
 expect(within(panel).getByRole("heading",{name:"第二份资源 · 合成验证"})).toBeTruthy();
 expect(within(panel).queryByText(/redaction-claim/)).toBeNull();
 expect(within(panel).getByText(/second-actor/)).toBeTruthy();
 expect(within(panel).getByText("sha256:second")).toBeTruthy();
 expect(runtimeAPI.resources).toHaveBeenCalledTimes(1);
});

it("drops selected resource properties immediately on reading change and does not select the same key in the new reading",async()=>{
 const firstPage=resourcePage("reading-a",request.request_ref);
 let resolve!:(page:Runtime.Resources)=>void;
 vi.mocked(runtimeAPI.resources).mockResolvedValueOnce(firstPage).mockImplementationOnce(()=>new Promise(r=>{resolve=r;}));
 const invalidated=vi.fn();const user=userEvent.setup();
 const view=render(<RuntimeRequestDetail reading={reading()} request={request} view="systems" onInvalidated={invalidated}/>);
 await user.click(await screen.findByRole("button",{name:"资源测试资料"}));
 expect(screen.getByRole("heading",{name:"资源测试资料 · 合成验证"})).toBeTruthy();
 view.rerender(<RuntimeRequestDetail reading={reading("reading-b")} request={request} view="systems" onInvalidated={invalidated}/>);
 expect(screen.queryByRole("heading",{name:"资源测试资料 · 合成验证"})).toBeNull();
 expect(screen.queryByRole("button",{name:"资源测试资料"})).toBeNull();
 const next=resourcePage("reading-b",request.request_ref);next.items[0].record.resource.name="新阅读资料";
 await act(async()=>resolve(next));
 const button=await screen.findByRole("button",{name:"新阅读资料"});
 expect(button.getAttribute("aria-pressed")).toBe("false");
 expect(within(screen.getByRole("complementary",{name:"所选资源属性"})).queryByRole("heading")).toBeNull();
 await user.click(button);expect(screen.getByRole("heading",{name:"新阅读资料 · 合成验证"})).toBeTruthy();
 expect(runtimeAPI.resources).toHaveBeenLastCalledWith("reading-b",request.request_ref,undefined,expect.any(AbortSignal));
});


it("keeps protocol and measurement codes in a disclosure while preserving measured zero and units",async()=>{
 vi.mocked(runtimeAPI.measurements).mockImplementation(async id=>({reading_id:id,eligibility_version:"v1",interpretation_revision:"rule",items:[{operation:"openai.chat_completions.v1",stream_mode:"non_streaming",measurement_kind:"request_response_complete",measurement_version:"1",completion_evidence:"protocol_response_written",entry_time_state:"observed",observation_count:1,excluded:{not_applicable:0,not_observed:0,legacy_unknown:0,invalid:0,missing_predecessor:0,missing_context:0,conflicting_context:0,incompatible_context:0},percentiles:{state:"computed",version:"1",unit:"milliseconds",sample_count:1,p50:"0",p95:"0"}}]}));
 render(<RuntimePage session={session}/>);
 const heading=await screen.findByRole("heading",{name:"对话生成 · 请求响应完成耗时"});const card=heading.closest("article")!;
 expect(within(card).getByText("非流式 · 协议响应已写出")).toBeTruthy();
 expect(within(card).getByText("P50（毫秒）").parentElement?.textContent).toBe("P50（毫秒）0");
 const code=within(card).getByText("openai.chat_completions.v1");const disclosure=code.closest("details")!;
 expect(disclosure.open).toBe(false);fireEvent.click(within(disclosure).getByText("排除项与测量口径"));expect(disclosure.open).toBe(true);
 expect(within(disclosure).getByText("request_response_complete / 1")).toBeTruthy();expect(within(disclosure).getByText("milliseconds")).toBeTruthy();
});

it("keeps observed values and unknown machine states distinct from verified completion",()=>{
 const measured:Runtime.Observation={...observation,document:{...observation.document,attempt:undefined,observation_kind:"response_measurement",runtime:{operation:"openai.chat_completions.v1",stream_mode:"non_streaming",admission:"admitted",provider:"future_provider_state",local_delivery:"write_started",financial:"pending",capture:"not_observed"},measurement:{kind:"request_response_complete",version:"1",value:"0",unit:"nanoseconds",state:"observed",start_boundary:"trusted_ingress",end_boundary:"protocol_response_written",resolution:"nanosecond_representation",source:"process_monotonic_clock"}}};
 render(<ObservationCard observation={measured} timezone="UTC"/>);
 expect(screen.getByText("响应耗时观察 · 修订 2")).toBeTruthy();expect(screen.getByText("未识别的状态，见原始字段")).toBeTruthy();expect(screen.getByText("已开始写入")).toBeTruthy();
 const code=screen.getByText("future_provider_state");const disclosure=code.closest("details")!;expect(disclosure.open).toBe(false);
 expect(screen.getByText("纳秒")).toBeTruthy();expect(within(disclosure).getByText("nanoseconds")).toBeTruthy();expect(within(disclosure).getByText("protocol_response_written")).toBeTruthy();
 expect(screen.queryByText("已完成")).toBeNull();
});


it("names every observation kind supported by the formal source schema without declaring its facts unknown", () => {
 const kinds = [...new Set(observationSchema.$defs.Observation.oneOf.map(item => item.properties.observation_kind.const))];
 expect(kinds).toHaveLength(30);
 const view = render(<ObservationCard observation={observation} timezone="UTC"/>);
 for (const kind of kinds) {
  view.rerender(<ObservationCard observation={{...observation,document:{...observation.document,observation_kind:kind}}} timezone="UTC"/>);
  const primary = screen.getByText(/ · 修订 2$/);
  expect(primary.textContent).not.toMatch(/未识别|待确认/);
  expect(primary.closest("details")).toBeNull();
  const raw = screen.getByText(kind, {selector:"dd"}).closest("details")!;
  expect(raw.open).toBe(false);
 }
});

it.each([
 ["financial_result","财务状态观察"], ["selection","能力选择观察"], ["local_delivery","本地交付观察"],
 ["dispatch_registered","目的地发送登记"], ["dispatch_result","目的地发送结果观察"], ["attempt_registered","尝试登记"],
 ["request_context_done","请求上下文结束"], ["input_result","输入政策结果观察"], ["output_result","输出政策结果观察"],
 ["state_reconciled","状态核对记录"], ["provider_outcome_unknown","提供方结果未知"],
])("renders the supported %s observation without promoting it to verified completion", (kind,label) => {
 render(<ObservationCard observation={{...observation,document:{...observation.document,observation_kind:kind}}} timezone="UTC"/>);
 expect(screen.getByText(`${label} · 修订 2`)).toBeTruthy();
 expect(screen.getByText("未开始")).toBeTruthy();
 expect(screen.queryByText("已完成")).toBeNull();
});

it("separates future unrecognized codes from a factual unknown and retains exact raw fields", () => {
 const future:Runtime.Observation={...observation,document:{...observation.document,observation_kind:"future_observation",attempt:{...observation.document.attempt!,operation:"future_operation",provider:"future_state",local_delivery:"unknown"},measurement:{kind:"future_measurement",version:"1",value:"0",unit:"nanoseconds",state:"observed",start_boundary:"trusted_ingress",end_boundary:"protocol_response_written",resolution:"nanosecond_representation",source:"process_monotonic_clock"}}};
 render(<ObservationCard observation={future} timezone="UTC"/>);
 expect(screen.getByText("未识别的观察类型，见原始字段 · 修订 2")).toBeTruthy();
 for(const text of ["未识别的操作类型，见原始字段","未识别的测量类型，见原始字段","未识别的状态，见原始字段"]) expect(screen.getByText(text)).toBeTruthy();
 expect(screen.getByText("未知",{selector:"dd"}).closest("details")).toBeNull();
 for(const code of ["future_observation","future_operation","future_state","future_measurement","unknown"]) {
  const raw=screen.getByText(code,{selector:"dd"});expect(raw.closest("details")!.open).toBe(false);
 }
 expect(screen.queryByText("状态待确认")).toBeNull();
 expect(screen.queryByText("已完成")).toBeNull();
});


function retainedRuntime():{navigation:RuntimeNavigation;value:Runtime.Reading;selected:Runtime.Request}{
 const value=reading("xrt_return");value.period.start_inclusive=new Date(Date.parse(value.period.end_exclusive)-86400000).toISOString();
 const selected={...request,request_ref:"xrr_selected"};
 return {value,selected,navigation:{version:1,owner:navigationOwner(session),readingID:value.reading_id,request:{contract_version:"xinsight.runtime-reading.v1",metric_version:"xinsight.runtime.request-observations.v1",period:{start_inclusive:value.period.start_inclusive,end_exclusive:value.period.end_exclusive,display_timezone:value.period.timezone},filters:{source_ids:["source"],source_projects:["project-3"],cost_projects:[]}},entry:"all",selectedRef:selected.request_ref,detailView:"contents"}};
}
it("returns from a content review after App remount to the original reading, exact request and content view without caching body",async()=>{
 const {value,selected}=retainedRuntime();
 const reviewer:AccessSessionResponse={...session,session:{...session.session,role:"workbench_investigation_reviewer",permissions:[...session.session.permissions,"investigation.scoped.read","investigation.scoped.write"]}};
 vi.spyOn(accessAPI,"session").mockResolvedValue(reviewer);vi.spyOn(window,"scrollTo").mockImplementation(()=>{});
 vi.mocked(runtimeAPI.observations).mockImplementation(async id=>({reading_id:id,items:[]}));
 vi.mocked(runtimeAPI.createReading).mockResolvedValue(value);vi.spyOn(runtimeAPI,"reading").mockResolvedValue(value);
 vi.mocked(runtimeAPI.requests).mockResolvedValue({reading_id:value.reading_id,items:[selected]});
 const pages=await import("./ProductPages");vi.spyOn(pages,"CaseDetailPage").mockImplementation(()=> <h2>Saved content review</h2>);
 const review=await import("./ReviewStart");vi.spyOn(review,"ReviewStart").mockImplementation(({navigate})=><button onClick={()=>navigate("/cases/xc_review#xf_selected")}>Open saved content review</button>);
 const bytes=new TextEncoder().encode("transient-body-secret");
 const reference:content.FrozenContentReference={source_system_id:selected.source_system_id,source_instance:selected.source_instance,org_id:selected.org_id,project_id:selected.project_id,request_id:selected.request_id,source_fact_id:"manifest-return",revision:"1",source_sha256:"sha256:manifest",content_ref:"body-return",capture_status:"complete",representation:{media_type:"text/plain",encoding:"identity",byte_domain:"stored_representation_before_http_content_encoding",saved_bytes:bytes.length,digest:"sha256:body"},expires_at:"2099-01-01T00:00:00.000000Z"};
 vi.spyOn(content,"fetchContentPage").mockResolvedValue({version:"xinsight.runtime-content.v1",reading_id:value.reading_id,request_ref:selected.request_ref,membership_ref:value.membership_ref,content_interpretation_revision:"xinsight.content-membership.v1",member_basis:"content_manifest",items:[{reference,availability:{state:"available",evaluated_at:"",retryable:false},newer_revision:false}]});
 const body=vi.spyOn(content,"fetchVerifiedContent").mockResolvedValue({reference,bytes,text:"transient-body-secret",previewOnly:false});
 window.history.replaceState(null,"","/workbench/runtime");
 let app=render(<App/>);await screen.findByRole("button",{name:"request-one"});
 fireEvent.change(screen.getByLabelText("来源项目"),{target:{value:"project-3"}});fireEvent.click(screen.getByRole("button",{name:"读取历史"}));
 await waitFor(()=>expect(runtimeAPI.createReading).toHaveBeenCalledTimes(2));await screen.findByRole("button",{name:"request-one"});
 fireEvent.change(screen.getByLabelText("请求时间口径"),{target:{value:"all"}});fireEvent.click(await screen.findByRole("button",{name:"request-one"}));
 fireEvent.click(await screen.findByRole("button",{name:/交互内容.*保存表示与人工复核/}));fireEvent.click(await screen.findByRole("button",{name:"读取并核验正文"}));
 fireEvent.click(await screen.findByRole("button",{name:"Open saved content review"}));await screen.findByRole("heading",{name:"Saved content review"});
 const stored=window.history.state.runtimeReading;
 expect(stored.readingID).toBe(value.reading_id);expect(stored.selectedRef).toBe(selected.request_ref);expect(stored.detailView).toBe("contents");expect(stored.entry).toBe("all");
 expect(stored.request.filters.source_projects).toEqual(["project-3"]);
 expect(Object.keys(stored).sort()).toEqual(["version","owner","readingID","request","entry","selectedRef","detailView"].sort());
 expect(JSON.stringify(window.history.state)).not.toMatch(/transient-body-secret|source_fact_id|body-return|membership_ref|permissions/);
 // Restore the selected member from a later existing list page, not a cached
 // request object or a new B observation query.
 vi.mocked(runtimeAPI.requests).mockImplementation(async(id,_entry,cursor)=>({reading_id:id,items:cursor?[selected]:[{...selected,request_ref:"xrr_other",request_id:"other"}],next_cursor:cursor?undefined:"page-two"}));
 app.unmount();app=render(<App/>);await screen.findByRole("heading",{name:"Saved content review"});
 fireEvent.click(screen.getByRole("link",{name:/返回运行与执行的原页面/}));await screen.findByRole("button",{name:"读取并核验正文"});
 expect(runtimeAPI.createReading).toHaveBeenCalledTimes(2);expect(runtimeAPI.reading).toHaveBeenCalledWith(value.reading_id,expect.any(AbortSignal));
 expect(runtimeAPI.requests).toHaveBeenCalledWith(value.reading_id,"all","page-two",expect.any(AbortSignal));
 expect(screen.getByLabelText("来源项目")).toHaveProperty("value","project-3");expect(screen.getByLabelText("请求时间口径")).toHaveProperty("value","all");
 expect(screen.getByRole("button",{name:/交互内容.*保存表示与人工复核/}).getAttribute("aria-current")).toBe("page");
 expect(screen.queryByText("transient-body-secret")).toBeNull();expect(body).toHaveBeenCalledTimes(1);
 expect(window.history.state.runtimeReading.readingID).toBe(value.reading_id);expect(window.history.state.runtimeReading.request.period.start_inclusive).toBe(value.period.start_inclusive);
});
it.each([403,409,410])("rejects persisted runtime reading on current authorization failure %i without replacement",async(status)=>{
 const {navigation}=retainedRuntime();vi.spyOn(accessAPI,"session").mockResolvedValue(session);vi.spyOn(runtimeAPI,"reading").mockRejectedValue(new ApiRequestError(status));
 const body=vi.spyOn(content,"fetchVerifiedContent");render(<RuntimePage session={session} navigation={navigation}/>);
 expect((await screen.findByRole("alert")).textContent).toContain("原阅读未恢复");expect(runtimeAPI.createReading).not.toHaveBeenCalled();expect(runtimeAPI.requests).not.toHaveBeenCalled();expect(body).not.toHaveBeenCalled();
});
it("rejects changed runtime owner or missing selected member instead of choosing another request",async()=>{
 const {navigation,value}=retainedRuntime();vi.spyOn(accessAPI,"session").mockResolvedValue(session);vi.spyOn(runtimeAPI,"reading").mockResolvedValue(value);
 let view=render(<RuntimePage session={session} navigation={{...navigation,owner:"other"}}/>);await screen.findByRole("alert");expect(runtimeAPI.reading).not.toHaveBeenCalled();view.unmount();
 vi.mocked(runtimeAPI.requests).mockResolvedValue({reading_id:value.reading_id,items:[]});
 view=render(<RuntimePage session={session} navigation={navigation}/>);await screen.findByRole("alert");expect(runtimeAPI.createReading).not.toHaveBeenCalled();expect(screen.queryByRole("heading",{name:"请求观察详情"})).toBeNull();
});
it("removes non-navigation payloads and rejects malformed runtime history without a fresh reading",async()=>{
 const {navigation}=retainedRuntime();
 const clean=readRuntimeNavigation({...navigation,body:"forbidden",selected:{request_id:"forbidden"},request:{...navigation.request,body:"forbidden"}});
 expect(JSON.stringify(clean)).not.toContain("forbidden");expect(()=>readRuntimeNavigation({...navigation,selectedRef:"../../bad"})).toThrow();
 render(<RuntimePage session={session} navigationInvalid/>);expect((await screen.findByRole("alert")).textContent).toContain("原阅读未恢复");expect(runtimeAPI.createReading).not.toHaveBeenCalled();
});


it.each(["expired", "period", "scope"])("does not restore a runtime reading with changed %s",async(kind)=>{
 const {navigation,value}=retainedRuntime();
 vi.spyOn(accessAPI,"session").mockResolvedValue(kind==="scope"?{...session,scope_revision:"changed"}:session);
 vi.spyOn(runtimeAPI,"reading").mockResolvedValue({...value,...(kind==="expired"?{expires_at:new Date(Date.now()-1000).toISOString()}:{}) ,period:kind==="period"?{...value.period,end_exclusive:new Date(Date.parse(value.period.end_exclusive)+1000).toISOString()}:value.period});
 render(<RuntimePage session={session} navigation={navigation}/>);
 expect((await screen.findByRole("alert")).textContent).toContain("原阅读未恢复");
 expect(runtimeAPI.createReading).not.toHaveBeenCalled();expect(runtimeAPI.requests).not.toHaveBeenCalled();
});
it("discards a late restored request page after scope changes and permits only an explicit new reading",async()=>{
 const {navigation,value,selected}=retainedRuntime();let resolve!:(page:Runtime.Page<Runtime.Request>)=>void;
 vi.spyOn(accessAPI,"session").mockResolvedValue(session);vi.spyOn(runtimeAPI,"reading").mockResolvedValue(value);
 vi.mocked(runtimeAPI.requests).mockImplementationOnce(()=>new Promise(done=>{resolve=done;}));
 const view=render(<RuntimePage session={session} navigation={navigation}/>);
 await waitFor(()=>expect(runtimeAPI.requests).toHaveBeenCalledOnce());
 const changed={...session,scope_revision:"changed"};vi.mocked(accessAPI.session).mockResolvedValue(changed);
 vi.mocked(runtimeAPI.reading).mockRejectedValue(new ApiRequestError(403));view.rerender(<RuntimePage session={changed} navigation={navigation}/>);
 await screen.findByRole("alert");await act(async()=>resolve({reading_id:value.reading_id,items:[selected]}));
 expect(screen.queryByRole("heading",{name:"请求观察详情"})).toBeNull();expect(runtimeAPI.createReading).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole("button",{name:"读取历史"}));await screen.findByRole("button",{name:"request-one"});
 expect(runtimeAPI.createReading).toHaveBeenCalledExactlyOnceWith(navigation.request,session.csrf_token,expect.any(AbortSignal));
});
