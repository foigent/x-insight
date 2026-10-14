import { beforeEach as beforeLegacyLocale } from "vitest";
import { setLocale as setLegacyLocale } from "./i18n";
// These existing semantic assertions explicitly exercise the Chinese translation.
beforeLegacyLocale(() => setLegacyLocale("zh-CN"));
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { investigationAPI, type AccessSessionResponse, type ExecutionDetail, type InvestigationProjection } from "./api";
import { ExecutionDetailPage } from "./ExecutionDetailPage";

const asOf = "2026-09-20T00:00:00Z";
const freshness = { integration_mode: "synthetic_fixture", sync_state: "current", timeline_complete: false } as const;
const detail: ExecutionDetail = { as_of: asOf, summary: { execution_id: "execution-a", request_id: "request-a", source_system_id: "source-a", source_type: "fixture", outcome: "unknown", first_effective_at: asOf, last_effective_at: asOf, event_count: 0, gap_count: 0, has_gaps: false, proof_class: "synthetic_fixture", assurance_mode: "synthetic_fixture", freshness } };
const projection: InvestigationProjection = { as_of: asOf, nodes: [] } as unknown as InvestigationProjection;
const session: AccessSessionResponse = { session: { subject_id: "subject-a", session_ref: "session-a", role: "workbench_reader", permissions: [], created_at: asOf, last_seen_at: asOf, idle_expires_at: asOf, absolute_expires_at: asOf }, csrf_token: "test" };

function mount(currentDetail = detail) {
  vi.spyOn(investigationAPI, "getExecution").mockResolvedValue(currentDetail);
  vi.spyOn(investigationAPI, "getTraceJourney").mockRejectedValue(new Error("unavailable"));
  vi.spyOn(investigationAPI, "listTimeline").mockResolvedValue({ execution_id: "execution-a", total_event_count: 0, items: [], page: { as_of: asOf, count: 0, has_more: false }, freshness });
  vi.spyOn(investigationAPI, "listGaps").mockResolvedValue({ execution_id: "execution-a", total_gap_count: 0, items: [], page: { as_of: asOf, count: 0, has_more: false }, freshness });
  window.history.replaceState(null, "", "/executions/execution-a?view=chronology");
  render(<ExecutionDetailPage executionID="execution-a" navigate={vi.fn()} session={session} />);
}
function contextValue(label: string) { return screen.getByText(label).parentElement?.querySelector("dd")?.textContent; }
afterEach(() => { cleanup(); vi.restoreAllMocks(); window.history.replaceState(null, "", "/"); });

it("distinguishes a pending projection from a loaded first page without the fields", async () => {
  let resolve!: (value: InvestigationProjection) => void;
  const pending = vi.spyOn(investigationAPI, "getInvestigation").mockReturnValue(new Promise(done => { resolve = done; }));
  mount();
  await screen.findByText("来源中的发起者");
  expect(contextValue("来源中的发起者")).toBe("正在读取过程投影");
  await act(async () => resolve(projection));
  await waitFor(() => expect(contextValue("来源中的发起者")).toBe("当前已加载的过程证据未显示"));
  expect(contextValue("已记录模型")).toBe("当前已加载的过程证据未显示");
  expect(pending).toHaveBeenCalledOnce();
});

it("does not infer source absence from a failed projection", async () => {
  vi.spyOn(investigationAPI, "getInvestigation").mockRejectedValue(new Error("projection unavailable"));
  mount();
  await waitFor(() => expect(contextValue("来源中的发起者")).toBe("过程投影未能读取，暂无法判断"));
  expect(contextValue("已记录模型")).toBe("过程投影未能读取，暂无法判断");
});

it("keeps withheld fields and a cutoff mismatch distinct from source absence", async () => {
  vi.spyOn(investigationAPI, "getInvestigation").mockResolvedValue(projection);
  mount({ ...detail, summary: { ...detail.summary, view_boundary: { schema_version: "v1", representation: "authorized_projection", withheld: ["actor_ref"] } } });
  await waitFor(() => expect(contextValue("来源中的发起者")).toBe("当前授权视图未显示"));
  cleanup();
  vi.mocked(investigationAPI.getInvestigation).mockResolvedValue({ ...projection, as_of: "2026-09-19T00:00:00Z" });
  mount();
  await waitFor(() => expect(contextValue("来源中的发起者")).toBe("过程投影与执行快照截止不一致，暂无法判断"));
});

it("does not present a source success as local delivery or business success", async () => {
  vi.spyOn(investigationAPI, "getInvestigation").mockResolvedValue(projection);
  mount({ ...detail, summary: { ...detail.summary, outcome: "success" } });
  expect(await screen.findByText("原执行证据结果：")).toBeTruthy();
  expect(screen.getByText(/上述结果来自原执行证据/)).toBeTruthy();
  expect(screen.getByText(/不代表运行本地交付完成或业务成功/)).toBeTruthy();
});
