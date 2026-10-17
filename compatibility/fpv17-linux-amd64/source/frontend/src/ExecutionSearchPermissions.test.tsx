import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { type AccessPermission, type AccessRole, type AccessSessionResponse, investigationAPI, workspaceAPI } from "./api";
import { ExecutionSearchPage } from "./ExecutionSearchPage";
afterEach(() => { cleanup(); vi.restoreAllMocks(); window.history.replaceState(null, "", "/executions"); });
function setup(role: AccessRole, permissions: AccessPermission[]) {
  vi.spyOn(investigationAPI, "listSources").mockResolvedValue({ items: [] });
  vi.spyOn(investigationAPI, "search").mockResolvedValue({ items: [], page: { as_of: "2026-09-11T00:00:00Z", has_more: false, limit: 50 } } as never);
  return { session: { role, permissions, session_ref: "session", subject_id: "subject", created_at: "", last_seen_at: "", idle_expires_at: "", absolute_expires_at: "" }, csrf_token: "csrf" } as AccessSessionResponse;
}
it.each<[AccessRole, AccessPermission[]]>([
  ["workbench_purpose_reviewer", ["investigation.scoped.read", "workbench.purpose.review"]],
  ["workbench_investigation_reviewer", ["investigation.scoped.read", "investigation.scoped.write"]],
  ["workbench_investigation_reader", ["investigation.scoped.read"]],
  ["investigation_reader", ["evidence.read"]],
])("keeps the legacy save form hidden for %s", async (role, permissions) => {
  render(<ExecutionSearchPage navigate={vi.fn()} session={setup(role, permissions)} />);
  await screen.findByText("执行记录");
  expect(screen.queryByText("保存为调查入口")).toBeNull();
});
it("preserves legacy saving and hides it when the current session loses its write permission", async () => {
  const session = setup("investigator", ["evidence.read", "investigation.write"]);
  const save = vi.spyOn(workspaceAPI, "createSavedView").mockResolvedValue({} as never);
  const ui = render(<ExecutionSearchPage navigate={vi.fn()} session={session} />);
  const user = userEvent.setup();
  await user.click(screen.getByText("保存为调查入口"));
  await user.type(screen.getByLabelText("视图名称"), "my view");
  await user.click(screen.getByRole("button", { name: "保存视图" }));
  await screen.findByText("已保存；结果本身未被复制。");
  expect(save).toHaveBeenCalledTimes(1);
  expect(save.mock.calls[0][1]).not.toHaveProperty("asOf");
  ui.rerender(<ExecutionSearchPage navigate={vi.fn()} session={{ ...session, session: { ...session.session, permissions: ["evidence.read"] } }} />);
  expect(screen.queryByText("保存为调查入口")).toBeNull();
});
