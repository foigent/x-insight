import { beforeEach as beforeLegacyLocale } from "vitest";
import { setLocale as setLegacyLocale } from "./i18n";
// These existing semantic assertions explicitly exercise the Chinese translation.
beforeLegacyLocale(() => setLegacyLocale("zh-CN"));
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { ApiRequestError, type AccessSessionResponse, type PurposeEvent, type PurposeView, workbenchAPI } from "./api";
import { PurposePanel } from "./PurposePanel";

const session: AccessSessionResponse = { session: { session_ref: "session", subject_id: "reviewer", role: "workbench_purpose_reviewer", permissions: ["workbench.summary.read", "workbench.purpose.review"], created_at: "2026-09-11T00:00:00Z", last_seen_at: "2026-09-11T00:00:00Z", idle_expires_at: "2026-09-12T00:00:00Z", absolute_expires_at: "2026-09-12T00:00:00Z" }, csrf_token: "csrf", scope_revision: "scope-1" };
const basis = { evidence_event_id: "event", event_hash: "sha256:event" };
const pending: PurposeView = { execution_id: "execution", vocabulary: "xinsight.request-purpose.dev.v1", can_review: true, state: { revision: 2, status: "conflict", claims: ["software_check", "content_transformation"].map((category, i) => ({ claim_id: `claim-${i}`, category: category as "software_check" | "content_transformation", vocabulary: "xinsight.request-purpose.dev.v1", method: "human_declared", basis_refs: [basis], reason: `original-${i}`, declared_by: "original-author", declared_at: "2026-09-11T00:00:00Z", review_state: "pending" })) }, history: [] };
function event(): PurposeEvent { return { execution_id: "execution", revision: 3, actor: "reviewer", recorded_at: "2026-09-11T01:00:00Z", command: { action: "accept", expected_revision: 2, idempotency_key: "key", claim_id: "claim-0", reason: "reviewed", reject_claim_ids: ["claim-1"] }, previous_hash: "sha256:previous", event_hash: "sha256:next", state: { ...pending.state, revision: 3, status: "adopted", category: "software_check", adopted_claim_id: "claim-0", claims: pending.state.claims.map((claim, i) => ({ ...claim, review_state: i === 0 ? "accepted" : "rejected" })) } }; }
afterEach(() => { cleanup(); vi.restoreAllMocks(); });
async function expand() { const user = userEvent.setup(); await user.click(screen.getByText("请求用途 · 声明与修订")); return user; }

it("keeps the frozen reading read-only until the user explicitly reads current state", async () => {
  const api = vi.spyOn(workbenchAPI, "purpose").mockResolvedValueOnce({ ...pending, reading_id: "frozen", can_review: false }).mockResolvedValueOnce(pending);
  render(<PurposePanel executionID="execution" readingID="frozen" basis={basis} session={session} />);
  expect(api).not.toHaveBeenCalled();
  const user = await expand();
  await screen.findByText(/本阅读的解释/);
  expect(api.mock.calls[0].slice(0, 3)).toEqual(["execution", "frozen", undefined]);
  expect(screen.queryByRole("textbox")).toBeNull();
  await user.click(screen.getByRole("button", { name: "读取当前判断" }));
  await screen.findByRole("textbox");
  expect(api.mock.calls[1][1]).toBeUndefined();
});

it("resolves only explicitly selected opposition and preserves human attribution", async () => {
  vi.spyOn(workbenchAPI, "purpose").mockResolvedValue(pending);
  const write = vi.spyOn(workbenchAPI, "purposeCommand").mockResolvedValue(event());
  render(<PurposePanel executionID="execution" basis={basis} session={session} />);
  const user = await expand();
  await user.type(await screen.findByRole("textbox"), "reviewed");
  await user.click(within(screen.getByRole("region", { name: "内容撰写与转换 待复核" })).getByRole("checkbox"));
  await user.click(within(screen.getByRole("region", { name: "软件实现与检查 待复核" })).getByRole("button", { name: "接受并采用" }));
  await screen.findByText(/操作已记录/);
  expect(write.mock.calls[0][1]).toMatchObject({ action: "accept", claim_id: "claim-0", expected_revision: 2, reject_claim_ids: ["claim-1"], reason: "reviewed" });
  expect(write.mock.calls[0][2]).toBe("csrf");
  expect(screen.getAllByText(/original-author/)).toHaveLength(2);
  expect(screen.queryByRole("checkbox")).toBeNull();
  expect((screen.getByRole("textbox") as HTMLTextAreaElement).value).toBe("");
});

it("retains a draft and its idempotency key on an uncertain write, but uses the refreshed revision after conflict", async () => {
  vi.spyOn(workbenchAPI, "purpose").mockResolvedValueOnce(pending).mockResolvedValue({ ...pending, state: { ...pending.state, revision: 4 } });
  const write = vi.spyOn(workbenchAPI, "purposeCommand").mockRejectedValueOnce(new Error("connection lost")).mockRejectedValueOnce(new ApiRequestError(409)).mockResolvedValue({ ...event(), revision: 5 });
  render(<PurposePanel executionID="execution" basis={basis} session={session} />);
  const user = await expand();
  await user.type(await screen.findByRole("textbox"), "retain this reason");
  const declare = screen.getByRole("button", { name: "提交人工声明（待复核）" });
  await user.click(declare); await screen.findByText(/用途请求暂未完成/);
  await user.click(declare); await screen.findByText(/用途判断已变化/);
  expect(write.mock.calls[1][1].idempotency_key).toBe(write.mock.calls[0][1].idempotency_key);
  expect((screen.getByRole("textbox") as HTMLTextAreaElement).value).toBe("retain this reason");
  await user.click(screen.getByRole("button", { name: "读取当前判断" }));
  await screen.findByText(/修订 4/);
  await user.click(declare); await screen.findByText(/操作已记录/);
  expect(write.mock.calls[2][1].expected_revision).toBe(4);
  expect(write.mock.calls[2][1].idempotency_key).not.toBe(write.mock.calls[0][1].idempotency_key);
  expect(write.mock.calls[0][1]).toMatchObject({ category: "software_check", vocabulary: "xinsight.request-purpose.dev.v1", basis_refs: [basis] });
});

it("clears revoked data and drafts and ignores an already pending response", async () => {
  vi.spyOn(workbenchAPI, "purpose").mockResolvedValue(pending);
  let resolve!: (e: PurposeEvent) => void;
  vi.spyOn(workbenchAPI, "purposeCommand").mockImplementation(() => new Promise(r => { resolve = r; }));
  const ui = render(<PurposePanel executionID="execution" basis={basis} session={session} />);
  const user = await expand();
  await user.type(await screen.findByRole("textbox"), "private draft");
  await user.click(screen.getByRole("button", { name: "提交人工声明（待复核）" }));
  ui.rerender(<PurposePanel executionID="execution" basis={basis} session={{ ...session, scope_revision: "revoked" }} />);
  await act(async () => resolve(event()));
  expect(screen.queryByText(/操作已记录/)).toBeNull();
  expect(screen.queryByText("original-0")).toBeNull();
  expect(screen.queryByRole("textbox")).toBeNull();
});

it("pages immutable history and treats authorization revision failure as invalidation", async () => {
  const history = event();
  const read = vi.spyOn(workbenchAPI, "purpose").mockResolvedValueOnce({ ...pending, history: [history], next_before_revision: 3 })
    .mockResolvedValueOnce({ ...pending, history: [{ ...history, revision: 2 }], next_before_revision: 2 })
    .mockRejectedValueOnce(new ApiRequestError(409, { code: "workbench_access_changed" } as never));
  const invalidated = vi.fn();
  render(<PurposePanel executionID="execution" basis={basis} session={session} onInvalidated={invalidated} />);
  const user = await expand();
  await user.click(await screen.findByText("操作历史（1 条已载入）"));
  await user.click(screen.getByRole("button", { name: "更早的用途历史" }));
  await screen.findByText("操作历史（2 条已载入）");
  expect(read.mock.calls[1][2]).toBe(3);
  await user.click(screen.getByRole("button", { name: "更早的用途历史" }));
  await waitFor(() => expect(invalidated).toHaveBeenCalledTimes(1));
  expect(screen.queryByText("original-0")).toBeNull();
});

it("does not expose review controls without both the role permission and request grant", async () => {
  vi.spyOn(workbenchAPI, "purpose").mockResolvedValue({ ...pending, can_review: false });
  render(<PurposePanel executionID="execution" basis={basis} session={session} />);
  await expand(); await screen.findByText("当前权限仅可阅读用途依据。");
  expect(screen.queryByRole("textbox")).toBeNull();
});
