import { beforeEach as beforeLegacyLocale } from "vitest";
import { setLocale as setLegacyLocale } from "./i18n";
// These existing semantic assertions explicitly exercise the Chinese translation.
beforeLegacyLocale(() => setLegacyLocale("zh-CN"));
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiRequestError, workspaceAPI, type AccessSessionResponse, type FindingFollowupAction, type FindingReview } from "./api";
import { FindingResponsibility } from "./FindingResponsibility";

const session = { csrf_token: "synthetic-csrf" } as AccessSessionResponse;
const review = (version = 0, name = ""): FindingReview => ({
  evaluated_at: "2026-09-09T12:00:00Z", case_version: version + 2, case_status: "open", assignee_state: version ? "active" : "not_recorded", allowed_actions: ["update_followup"],
  followup: { case_id: "case-a", finding_id: "finding-a", recorded: version > 0, version, assignee_subject_id: version ? "xs_3333333333333333" : null, assignee_name: name, due_at: null, due_timezone: null, updated_at: null, updated_by_subject: "" },
  history: { items: [], through_version: version, next_before_version: null },
});

function directory() {
  return vi.spyOn(workspaceAPI, "assigneeCandidates").mockResolvedValue({ items: [{ subject_id: "xs_3333333333333333", display_name: "Local owner", state: "active" }], total: 1, complete: true, basis: "loaded_local_account_configuration" });
}

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

describe("Finding responsibility", () => {
  it("retries the same operation after an unknown result and reads current state separately", async () => {
    const user = userEvent.setup(); const get = vi.spyOn(workspaceAPI, "getFindingReview").mockResolvedValue(review()); directory();
    const receipt = { current: review(1, "Old receipt owner").followup } as FindingFollowupAction;
    const save = vi.spyOn(workspaceAPI, "updateFindingFollowup").mockRejectedValueOnce(new TypeError("network result unknown")).mockResolvedValueOnce(receipt);
    render(<FindingResponsibility caseID="case-a" findingID="finding-a" caseStatus="open" session={session} onChanged={vi.fn()} />);
    await user.click(screen.getByText("责任与期限"));
    await user.click(await screen.findByRole("button", { name: "登记或修改责任" }));
    await user.selectOptions(await screen.findByLabelText("负责人"), "xs_3333333333333333");
    await user.type(screen.getByLabelText("修改依据"), "Manual responsibility decision");
    await user.click(screen.getByRole("button", { name: "保存责任记录" }));
    expect(await screen.findByRole("button", { name: "原样重试同一操作" })).toBeTruthy();
    expect(screen.getByText(/尚未确认保存结果/)).toBeTruthy();
    expect((screen.getByLabelText("修改依据") as HTMLTextAreaElement).disabled).toBe(true);
    get.mockResolvedValue(review(3, "Current owner"));
    await user.click(screen.getByRole("button", { name: "原样重试同一操作" }));
    expect(await screen.findByText(/当前版本 3/)).toBeTruthy();
    expect(screen.queryByText(/Old receipt owner/)).toBeNull();
    expect(save.mock.calls[0][2]).toEqual(save.mock.calls[1][2]);
    expect(save.mock.calls[0][2].expected_case_version).toBe(2);
  });

  it("keeps a conflict unsaved and creates a new operation only after an explicit reread", async () => {
    const user = userEvent.setup(); const get = vi.spyOn(workspaceAPI, "getFindingReview").mockResolvedValue(review()); directory();
    const save = vi.spyOn(workspaceAPI, "updateFindingFollowup").mockRejectedValueOnce(new ApiRequestError(409)).mockResolvedValue({ current: review(3).followup } as FindingFollowupAction);
    render(<FindingResponsibility caseID="case-a" findingID="finding-a" caseStatus="open" session={session} onChanged={vi.fn()} />);
    await user.click(screen.getByText("责任与期限")); await user.click(await screen.findByRole("button", { name: "登记或修改责任" }));
    await user.type(await screen.findByLabelText("修改依据"), "Old draft"); await user.click(screen.getByRole("button", { name: "保存责任记录" }));
    await screen.findByText(/记录版本已变化，本次修改未保存/);
    expect(screen.queryByRole("button", { name: "原样重试同一操作" })).toBeNull();
    expect(screen.queryByRole("button", { name: "保存责任记录" })).toBeNull();
    expect(screen.queryByText(/尚未确认保存结果/)).toBeNull();
    expect(save).toHaveBeenCalledTimes(1);
    // A change to another Finding advances only the Case version.
    get.mockResolvedValue({ ...review(), case_version: 3 }); await user.click(screen.getByRole("button", { name: "重新读取并核对" }));
    await waitFor(() => expect((screen.getByLabelText("修改依据") as HTMLTextAreaElement).value).toBe(""));
    await user.type(screen.getByLabelText("修改依据"), "New informed decision"); await user.click(screen.getByRole("button", { name: "保存责任记录" }));
    await waitFor(() => expect(save).toHaveBeenCalledTimes(2));
    expect(save.mock.calls[1][2].expected_version).toBe(0);
    expect(save.mock.calls[1][2].expected_case_version).toBe(3);
    expect(save.mock.calls[1][2].operation_id).not.toBe(save.mock.calls[0][2].operation_id);
  });

  it("ignores a late response from a previous Finding even if transport cancellation is ignored", async () => {
    const user = userEvent.setup(); let finish!: (value: FindingReview) => void;
    const late = new Promise<FindingReview>((resolve) => { finish = resolve; });
    vi.spyOn(workspaceAPI, "getFindingReview").mockImplementation((_case, finding) => finding === "finding-a" ? late : Promise.resolve(review(2, "Finding B owner")));
    const view = render(<FindingResponsibility caseID="case-a" findingID="finding-a" caseStatus="open" session={session} onChanged={vi.fn()} />);
    await user.click(screen.getByText("责任与期限"));
    view.rerender(<FindingResponsibility caseID="case-b" findingID="finding-b" caseStatus="open" session={session} onChanged={vi.fn()} />);
    expect(await screen.findByText(/Finding B owner/)).toBeTruthy();
    await act(async () => { finish(review(1, "Finding A stale owner")); await late; });
    expect(screen.queryByText(/Finding A stale owner/)).toBeNull();
  });
  it("distinguishes a confirmed receipt from a failed current-state refresh", async () => {
    const user = userEvent.setup();
    const get = vi.spyOn(workspaceAPI, "getFindingReview").mockResolvedValue(review()); directory();
    const save = vi.spyOn(workspaceAPI, "updateFindingFollowup").mockResolvedValue({ current: review(1).followup } as FindingFollowupAction);
    render(<FindingResponsibility caseID="case-a" findingID="finding-a" caseStatus="open" session={session} onChanged={vi.fn()} />);
    await user.click(screen.getByText("责任与期限"));
    await user.click(await screen.findByRole("button", { name: "登记或修改责任" }));
    await user.type(await screen.findByLabelText("修改依据"), "Save then refresh");
    get.mockRejectedValueOnce(new TypeError("current read unavailable"));
    await user.click(screen.getByRole("button", { name: "保存责任记录" }));
    expect(await screen.findByText(/动作已确认保存为责任版本 1，但当前记录读取失败/)).toBeTruthy();
    get.mockResolvedValue(review(2, "Current owner"));
    expect(screen.queryByRole("button", { name: "原样重试同一操作" })).toBeNull();
    expect(screen.queryByText(/尚未确认保存结果/)).toBeNull();
    await user.click(screen.getByRole("button", { name: "重新读取当前记录" }));
    expect(await screen.findByText(/当前版本 2/)).toBeTruthy();
    expect(save).toHaveBeenCalledTimes(1);
  });

  it("removes displayed responsibility after current authorization is denied", async () => {
    const user = userEvent.setup();
    const get = vi.spyOn(workspaceAPI, "getFindingReview").mockResolvedValue(review(1, "Previously visible owner"));
    render(<FindingResponsibility caseID="case-a" findingID="finding-a" caseStatus="open" session={session} onChanged={vi.fn()} />);
    await user.click(screen.getByText("责任与期限"));
    expect(await screen.findByText(/Previously visible owner/)).toBeTruthy();
    get.mockRejectedValue(new ApiRequestError(403));
    await user.click(screen.getByRole("button", { name: "登记或修改责任" }));
    await waitFor(() => expect(screen.queryByText(/Previously visible owner/)).toBeNull());
    expect(screen.queryByLabelText("负责人")).toBeNull();
  });

  it("rereads a sealed Case without reopening an editor or resubmitting", async () => {
    const user = userEvent.setup(); const get = vi.spyOn(workspaceAPI, "getFindingReview").mockResolvedValue(review()); directory();
    const save = vi.spyOn(workspaceAPI, "updateFindingFollowup").mockRejectedValue(new ApiRequestError(409, { code: "case_sealed" } as never));
    const changed = vi.fn();
    render(<FindingResponsibility caseID="case-a" findingID="finding-a" caseStatus="open" session={session} onChanged={changed} />);
    await user.click(screen.getByText("责任与期限")); await user.click(await screen.findByRole("button", { name: "登记或修改责任" }));
    await user.type(await screen.findByLabelText("修改依据"), "A concurrent seal wins"); await user.click(screen.getByRole("button", { name: "保存责任记录" }));
    await screen.findByText(/Case 已封存，本次修改未保存/);
    get.mockResolvedValue({ ...review(), case_status: "sealed", allowed_actions: [] });
    await user.click(screen.getByRole("button", { name: "重新读取并核对" }));
    await waitFor(() => expect(screen.queryByLabelText("修改依据")).toBeNull());
    expect(save).toHaveBeenCalledTimes(1); expect(changed).toHaveBeenCalledTimes(1);
  });

  it.each([401, 403])("clears confirmed-save recovery after access loss (%s)", async (status) => {
    const user = userEvent.setup(); const get = vi.spyOn(workspaceAPI, "getFindingReview").mockResolvedValue(review(1, "Owner before access loss")); directory();
    const save = vi.spyOn(workspaceAPI, "updateFindingFollowup").mockResolvedValue({ current: review(2).followup } as FindingFollowupAction);
    render(<FindingResponsibility caseID="case-a" findingID="finding-a" caseStatus="open" session={session} onChanged={vi.fn()} />);
    await user.click(screen.getByText("责任与期限")); await user.click(await screen.findByRole("button", { name: "登记或修改责任" }));
    await user.type(await screen.findByLabelText("修改依据"), "Saved before access loss");
    get.mockRejectedValueOnce(new TypeError("read unavailable"));
    await user.click(screen.getByRole("button", { name: "保存责任记录" }));
    await screen.findByRole("button", { name: "重新读取当前记录" });
    get.mockRejectedValue(new ApiRequestError(status)); await user.click(screen.getByRole("button", { name: "重新读取当前记录" }));
    await waitFor(() => expect(screen.queryByText(/Owner before access loss/)).toBeNull());
    expect(screen.queryByText(/动作已确认保存/)).toBeNull(); expect(screen.queryByLabelText("修改依据")).toBeNull();
    expect(save).toHaveBeenCalledTimes(1);
  });

});
