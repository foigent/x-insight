import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { acceptScopeSession, scopeSessionKey, workActivityAPI, type AccessSessionResponse, type WorkActivityDetail, type WorkActivityPage as ActivityPage } from "./api";
import { WorkActivityPage } from "./WorkActivityPage";

const session = { session: { subject_id: "reader", session_ref: "one", role: "workbench_reader", permissions: ["workbench.details.read"] }, scope_revision: "scope-one" } as AccessSessionResponse;
const reporter = { source_system_id: "source", source_instance: "instance", org_id: "7", project_id: "9" };
const at = new Date().toISOString();
const originalScrollIntoView = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "scrollIntoView");
function item(ref: string, action: string, resourceRef: string) {
  return { event: { schema_version: "xinsight.work-event.v1", reporter, event_ref: ref, revision: 1, occurred_at: at, recorded_at: at,
    subject_kind: "resource_action", lifecycle_state: "completed", run_id: "run", actor: { kind: "agent", ref: "agent" },
    action_kind: action, resource: { ref: resourceRef, name_state: "readable", name: "same-name.txt" }, content_state: "not_captured", content_refs: [] },
    event_hash: "sha256:" + "e".repeat(64), received_at: at } as ActivityPage["items"][number];
}

afterEach(() => { cleanup(); acceptScopeSession(null); vi.restoreAllMocks(); window.history.replaceState({}, "", window.location.href);
  if (originalScrollIntoView) Object.defineProperty(HTMLElement.prototype, "scrollIntoView", originalScrollIntoView);
  else Reflect.deleteProperty(HTMLElement.prototype, "scrollIntoView");
});

it("keeps the full-run aggregate expandable and loads bounded event pages in place", async () => {
  acceptScopeSession(session);
  window.history.replaceState({ workActivityContext: { owner: scopeSessionKey(session),
    applied: { from: new Date(Date.now() - 60_000).toISOString(), to: new Date().toISOString(), sourceIDs: [], projectIDs: [], resourceName: "" },
    selected: { source: "source", instance: "instance", org: "7", project: "9", event: "read" } } }, "", window.location.href);
  vi.spyOn(workActivityAPI, "scopes").mockResolvedValue({ version: "xinsight.work-activity-scopes.v1", items: [
    { source_id: "source", source_label: "Fixture", source_instance: "instance", org_id: "7", project_id: "9", project_label: "Project" },
  ] });
  const primary = item("read", "resource.read", "file-a");
  primary.event.actor = { kind: "human", ref: "alice" };
  vi.spyOn(workActivityAPI, "list").mockResolvedValue({ version: "xinsight.work-activity.v1", cutoff: at, items: [primary] });
  const summary = { resource_read_outcomes: { completed: 102, failed: 0, optional_not_found: 0, unknown: 0 },
    resource_change_outcomes: { completed: 1, failed: 0, optional_not_found: 0, unknown: 0 }, distinct_resources: 103,
    model_calls: 0, tool_calls: 0, unlinked_model_observations: 0, task_completed: 2, task_failed: 1, task_unknown: 3 };
  const detail = (rows: ActivityPage["items"], more: boolean, next?: string): WorkActivityDetail => ({
    version: "xinsight.work-activity-detail.v2", item: primary, relations: [], review_bodies: [],
    run_items: rows, run_items_more: more, run_items_next_cursor: next, run_cutoff: at, run_summary: summary,
  });
  const first = detail([primary, item("write", "resource.write", "file-b")], true, "cursor");
  const second = detail([item("last", "resource.read", "file-c")], false);
  const write = item("write", "resource.write", "file-b");
  let resolveWrite!: (value: WorkActivityDetail) => void;
  const pendingWrite = new Promise<WorkActivityDetail>(resolve => { resolveWrite = resolve; });
  const readDetail = vi.spyOn(workActivityAPI, "detail").mockImplementation(async (requested, _signal, cursor) => cursor ? second : requested.event === "write" ? pendingWrite : first);
  render(<WorkActivityPage session={session} navigate={vi.fn()} />);
  const context = await screen.findByRole("complementary", { name: "来源上下文" });
  expect(within(context).getByText("alice（人员）")).toBeTruthy();
  expect(within(context).getByText("来源未提供")).toBeTruthy();
  fireEvent.click(screen.getByText("运行汇总、分类与读取依据"));
  const resources = (await screen.findByText("资源对象")).closest("div")!;
  expect(within(resources).getByText("103")).toBeTruthy();
  expect(screen.queryByText(/人类发起人/)).toBeNull();
  expect(screen.getByText(/当前已载入 2 条事件，仍有后续页/)).toBeTruthy();
  expect(screen.getByText(/来源报告完成 102/)).toBeTruthy();
  expect(screen.getByText("正常结束 2")).toBeTruthy();
  expect(screen.getByText(/报告失败 1 · 结束未确认 3/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "加载下一页运行事件" }));
  expect(await screen.findByText(/当前已载入 3 条事件（本次受权运行已载入完毕）/)).toBeTruthy();
  expect(readDetail.mock.calls.at(-1)?.[2]).toBe("cursor");
  fireEvent.click(screen.getByRole("button", { name: "← 返回当前筛选列表" }));
  expect(screen.getByRole("button", { name: "查看工作过程" })).toBeTruthy();
  const result = screen.getByText("来源报告完成", { selector: ".activity-record-result strong" });
  expect(result.parentElement?.querySelector("details")?.open).toBe(false);
  expect(within(result.parentElement!).getByText(/内容未独立核验/)).toBeTruthy();
  const scrollIntoView = vi.fn();
  Object.defineProperty(HTMLElement.prototype, "scrollIntoView", { configurable: true, value: scrollIntoView });
  fireEvent.click(screen.getByRole("button", { name: "查看工作过程" }));
  const landing = await screen.findByRole("article", { name: "工作过程详情" });
  expect(document.activeElement).toBe(landing);
  expect(scrollIntoView).toHaveBeenCalledWith({ block: "start" });
  fireEvent.click(screen.getByRole("button", { name: "查看工作过程" }));
  const beforeRun = scrollIntoView.mock.calls.length;
  const runRail = await screen.findByRole("complementary", { name: "同次运行过程" });
  vi.spyOn(landing, "getBoundingClientRect").mockReturnValue({ height: 960 } as DOMRect);
  fireEvent.click(within(runRail).getAllByRole("button").find(button => !button.hasAttribute("disabled"))!);
  expect(landing.style.minHeight).toBe("960px");
  expect(screen.queryByRole("complementary", { name: "来源上下文" })).toBeNull();
  await act(async () => resolveWrite({ ...first, item: write }));
  expect(landing.style.minHeight).toBe("");
  expect(document.activeElement).toBe(landing);
  expect(scrollIntoView.mock.calls.length).toBeGreaterThan(beforeRun + 1);
  readDetail.mockRejectedValueOnce(new Error("no longer readable"));
  const nextRail = screen.getByRole("complementary", { name: "同次运行过程" });
  fireEvent.click(within(nextRail).getAllByRole("button").find(button => !button.hasAttribute("disabled"))!);
  expect(await screen.findByRole("alert")).toHaveProperty("textContent", "当前无权读取或资料已变更，请返回列表重新查询。");
  expect(landing.style.minHeight).toBe("");
  expect(document.activeElement).toBe(landing);
});

it("rejects a continuation for the same event ref in a different reporter", async () => {
  acceptScopeSession(session);
  window.history.replaceState({ workActivityContext: { owner: scopeSessionKey(session),
    applied: { from: new Date(Date.now() - 60_000).toISOString(), to: new Date().toISOString(), sourceIDs: [], projectIDs: [], resourceName: "" },
    selected: { source: "source", instance: "instance", org: "7", project: "9", event: "read" } } }, "", window.location.href);
  vi.spyOn(workActivityAPI, "scopes").mockResolvedValue({ version: "xinsight.work-activity-scopes.v1", items: [
    { source_id: "source", source_label: "Fixture", source_instance: "instance", org_id: "7", project_id: "9", project_label: "Project" },
  ] });
  const primary = item("read", "resource.read", "file-a");
  vi.spyOn(workActivityAPI, "list").mockResolvedValue({ version: "xinsight.work-activity.v1", cutoff: at, items: [primary] });
  const summary = { resource_read_outcomes: { completed: 1, failed: 0, optional_not_found: 0, unknown: 0 },
    resource_change_outcomes: { completed: 0, failed: 0, optional_not_found: 0, unknown: 0 }, distinct_resources: 1,
    model_calls: 0, tool_calls: 0, unlinked_model_observations: 0, task_completed: 0, task_failed: 0, task_unknown: 0 };
  const first: WorkActivityDetail = { version: "xinsight.work-activity-detail.v2", item: primary, relations: [], review_bodies: [],
    run_items: [primary], run_items_more: true, run_items_next_cursor: "cursor", run_cutoff: at, run_summary: summary };
  const wrong = item("read", "resource.read", "file-other");
  wrong.event.reporter = { ...reporter, source_system_id: "another-source" };
  const next: WorkActivityDetail = { ...first, item: wrong, run_items: [item("extra", "resource.read", "file-extra")],
    run_items_more: false, run_items_next_cursor: undefined };
  let resolveLate: (value: WorkActivityDetail) => void = () => {};
  const lateNext = new Promise<WorkActivityDetail>(resolve => { resolveLate = resolve; });
  vi.spyOn(workActivityAPI, "detail").mockImplementation(async (_selected, _signal, cursor) => cursor ? lateNext : first);
  render(<WorkActivityPage session={session} navigate={vi.fn()} />);
  expect(await screen.findByText(/当前已载入 1 条事件，仍有后续页/)).toBeTruthy();
  fireEvent.click(screen.getByRole("button", { name: "加载下一页运行事件" }));
  expect(await screen.findByRole("button", { name: "正在读取后续事件…" })).toBeTruthy();
  resolveLate(next);
  expect(await screen.findByText(/后续运行资料不可读或阅读范围已变化/)).toBeTruthy();
  expect(screen.getByText(/当前已载入 1 条事件，仍有后续页/)).toBeTruthy();
});
