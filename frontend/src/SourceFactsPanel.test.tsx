import { beforeEach as beforeLegacyLocale } from "vitest";
import { setLocale as setLegacyLocale } from "./i18n";
// These existing semantic assertions explicitly exercise the Chinese translation.
beforeLegacyLocale(() => setLegacyLocale("zh-CN"));
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { ApiRequestError, type WorkbenchReading, type WorkbenchSourceObservation, type WorkbenchSourceObservations, workbenchAPI } from "./api";
import { SourceFactCard, SourceFactsPanel } from "./SourceFactsPanel";

const reading: WorkbenchReading = {
  reading_id: "reading-a", source_manifest_ref: "sha256:manifest-a",
  contract_version: "xinsight.workbench.v1", metric_version: "xinsight.workbench.cost.v1",
  period: { start_inclusive: "2026-09-06T00:00:00Z", end_exclusive: "2026-09-10T00:00:00Z", timezone: "UTC" },
  cutoff: "2026-09-10T00:00:00Z", expires_at: "2026-09-10T00:30:00Z",
  money_basis: "gateway_charged", money_title: "网关扣费", currency: "CNY",
  membership_ref: "sha256:membership", interpretation_revision: "sha256:rule", authorization_revision: "sha256:authorization",
  dimensions: ["model", "source_project", "cost_project"],
  coverage: { status: "period_completeness_unknown", period_completeness: "unknown", projection_complete_for_members: true, known_items: 0, gaps: [] },
  source_coverage: { known_documents: 3, pending_predecessor: 1, economic_not_received: 3,
    economic_hash_conflicts: 0, media_pending: 1, not_captured: 1, profiles: [], gaps: ["cursor_expired"] },
};
const fact: WorkbenchSourceObservation = {
  source_system_id: "source", request_id: "request", profile: "unirouter.execution-identity.v1",
  stage: "terminal", source_fact_id: "fact", revision: "2", source_hash: "sha256:source", envelope_hash: "sha256:envelope",
  chain_state: "complete", economic_state: "not_received", capture_status: "available",
  occurred_at: "2026-09-06T01:00:00.000000Z", recorded_at: "2026-09-06T02:00:00.000000Z",
  identity: { owner_user_id: "9007199254740995", sponsor_user_id: "9007199254740993", lineage: ["agent-1", "agent-2"] },
};
const page = (items = [fact]): WorkbenchSourceObservations => ({ reading_id: reading.reading_id, manifest_ref: reading.source_manifest_ref!, items });
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it("shows persistent administrator attention without hiding readable history", async () => {
  vi.spyOn(workbenchAPI, "sourceFacts").mockResolvedValue(page());
  render(<SourceFactsPanel reading={{ ...reading, source_coverage: { ...reading.source_coverage!, requires_attention: true } }} onInvalidated={vi.fn()} />);
  expect(await screen.findByText("9007199254740995")).toBeTruthy();
  expect(screen.getByText(/自动重启不会恢复接收/)).toBeTruthy();
});

it("shows source-first records without monetary rows and preserves string identities", async () => {
  vi.spyOn(workbenchAPI, "sourceFacts").mockResolvedValue(page());
  render(<SourceFactsPanel reading={reading} onInvalidated={vi.fn()} />);
  expect(await screen.findByText("9007199254740995")).toBeTruthy();
  expect(screen.getByText("9007199254740993")).toBeTruthy();
  expect(screen.getByText(/尚未收到可关联的经济事实，不代表零费用/)).toBeTruthy();
  expect(screen.getByText(/账户归属和 Sponsor 不代表实际操作者/)).toBeTruthy();
  expect(screen.getByText(/建立本阅读时存在来源连接或游标缺口/)).toBeTruthy();
});

it("keeps pending ancestry and not-captured fields unknown", () => {
  render(<SourceFactCard fact={{ ...fact, identity: undefined, chain_state: "missing_predecessor", capture_status: "not_captured" }} />);
  expect(screen.getByText(/前驱尚未收齐/)).toBeTruthy();
  expect(screen.getByText(/原时点未采集/)).toBeTruthy();
  expect(screen.queryByText("9007199254740995")).toBeNull();
});

it.each(["priced", "unpriced"])("distinguishes %s cost basis and separate I-01 revision", (state) => {
  render(<SourceFactCard fact={{ ...fact, profile: "unirouter.cost-basis.v1", identity: undefined,
    cost_basis: { basis: { pricing_state: state, input_rate_micros_per_million: "0", pricing_version: "frozen-v1" },
      economic_reference: { source: "completed_execution", revision: "1", publication_status: "not_published_by_i01" } } }} />);
  expect(screen.getByText(state === "priced" ? "已定价（零费率亦为已定价）" : "未定价")).toBeTruthy();
  expect(screen.getByText(/原经济修订 1/)).toBeTruthy();
  expect(screen.getByText(/预期哈希不是已收到金额的证明/)).toBeTruthy();
});

it("rejects stale responses when a different reading is mounted", async () => {
  let resolve!: (p: WorkbenchSourceObservations) => void;
  vi.spyOn(workbenchAPI, "sourceFacts").mockImplementationOnce(() => new Promise(r => { resolve = r; }))
    .mockResolvedValueOnce({ reading_id: "reading-b", manifest_ref: "sha256:b", items: [] });
  const ui = render(<SourceFactsPanel key="a" reading={reading} onInvalidated={vi.fn()} />);
  ui.rerender(<SourceFactsPanel key="b" reading={{ ...reading, reading_id: "reading-b", source_manifest_ref: "sha256:b" }} onInvalidated={vi.fn()} />);
  await screen.findByText("当前范围没有可展示的来源字段明细。");
  await act(async () => { resolve(page()); });
  expect(screen.queryByText("9007199254740995")).toBeNull();
});

it("invalidates revoked readings and retries transient failures without losing the frozen identity", async () => {
  const failure = new ApiRequestError(409, { code: "workbench_access_changed" } as never);
  const api = vi.spyOn(workbenchAPI, "sourceFacts").mockRejectedValueOnce(new Error("offline"))
    .mockResolvedValueOnce({ ...page(), next_cursor: "next" }).mockRejectedValueOnce(failure);
  const invalidated = vi.fn(); const user = userEvent.setup();
  render(<SourceFactsPanel reading={reading} onInvalidated={invalidated} />);
  await user.click(await screen.findByRole("button", { name: "重试来源明细" }));
  expect(await screen.findByText("9007199254740995")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "加载更多来源记录" }));
  await waitFor(() => expect(invalidated).toHaveBeenCalledWith(failure));
  expect(api.mock.calls[2][1]).toBe("next");
  expect(screen.queryByText("9007199254740995")).toBeNull();
});

it("removes previous source details after the detail action is denied", async () => {
  vi.spyOn(workbenchAPI, "sourceFacts").mockResolvedValueOnce({ ...page(), next_cursor: "next" })
    .mockRejectedValueOnce(new ApiRequestError(403));
  const user = userEvent.setup();
  render(<SourceFactsPanel reading={reading} onInvalidated={vi.fn()} />);
  await screen.findByText("9007199254740995");
  await user.click(screen.getByRole("button", { name: "加载更多来源记录" }));
  await screen.findByText(/当前授权不包含来源字段明细/);
  expect(screen.queryByText("9007199254740995")).toBeNull();
});

it.each(["active", "paused", "revoked"] as const)("shows frozen %s collection independently from historical reading access", async state => {
  vi.spyOn(workbenchAPI, "sourceFacts").mockResolvedValue(page());
  render(<SourceFactsPanel reading={{...reading,source_coverage:{...reading.source_coverage!,collection_states:[state],gaps:["forward_only_capture","timeline_completeness_unknown"]}}} onInvalidated={vi.fn()} />);
  await screen.findByText("9007199254740995");
  expect(screen.getByLabelText("本阅读的采集注册状态").textContent).toContain({active:"已启用",paused:"已暂停",revoked:"已撤销"}[state]);
  if(state==="paused") expect(screen.getByText(/新事实不会通过这些注册接收/)).toBeTruthy();
  if(state==="revoked") expect(screen.getByText(/这不等于人员阅读权限被撤销/)).toBeTruthy();
  expect(screen.queryByText(/来源内容或修订发生冲突/)).toBeNull();
});

it("retains the old conflict explanation until a new reading is explicitly adopted", async () => {
  vi.spyOn(workbenchAPI,"sourceFacts").mockResolvedValue(page());
  const failed={...reading,source_coverage:{...reading.source_coverage!,collection_states:["active" as const],gaps:["source_fact_conflict"]}};
  const ui=render(<SourceFactsPanel key="old" reading={failed} onInvalidated={vi.fn()}/>);
  await screen.findByText("9007199254740995");
  expect(screen.getByText(/原合法记录与接收位置保持/)).toBeTruthy();
  vi.mocked(workbenchAPI.sourceFacts).mockResolvedValue({reading_id:"recovered",manifest_ref:"sha256:recovered",items:[fact]});
  ui.rerender(<SourceFactsPanel key="old" reading={failed} onInvalidated={vi.fn()}/>);
  expect(screen.getByText(/来源内容或修订发生冲突/)).toBeTruthy();
  ui.rerender(<SourceFactsPanel key="new" reading={{...failed,reading_id:"recovered",source_manifest_ref:"sha256:recovered",source_coverage:{...failed.source_coverage,gaps:[]}}} onInvalidated={vi.fn()}/>);
  await screen.findByText("9007199254740995");
  expect(screen.queryByText(/来源内容或修订发生冲突/)).toBeNull();
});

it("does not invent active collection for a historical reading without state observations", async () => {
  vi.spyOn(workbenchAPI,"sourceFacts").mockResolvedValue(page());
  render(<SourceFactsPanel reading={reading} onInvalidated={vi.fn()}/>);
  await screen.findByText("9007199254740995");
  expect(screen.getByText(/不能据此判断正在正常采集/)).toBeTruthy();
  expect(screen.queryByLabelText("本阅读的采集注册状态")).toBeNull();
});
