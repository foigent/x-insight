import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { useState } from "react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiRequestError, assessmentAPI, workspaceAPI, type AccessSessionResponse, type CaseRecord, type FindingReview } from "./api";
import type { AssessmentAction, AssessmentCommand, AssessmentView, BasisSelection, ReviewBasis, ReviewCreationCommand, ReviewCreationReceipt } from "./assessmentTypes";
import { ReferencePicker, ReviewBasisView, ReviewFields } from "./ReviewBasisView";
import { FindingLifecycle } from "./FindingLifecycle";
import { ReviewStart } from "./ReviewStart";
import { clearPendingReview, keepPendingReview, readPendingReview } from "./pendingReview";

const subject = "xs_1111111111111111";
const session = { session: { subject_id: subject, permissions: ["evidence.read", "investigation.write"] }, csrf_token: "fixture-csrf" } as AccessSessionResponse;
const readonly = { session: { subject_id: subject, permissions: ["evidence.read"] }, csrf_token: "fixture-csrf" } as AccessSessionResponse;
const now = "2026-09-10T00:00:00Z";
const record: CaseRecord = { case: { id: "case-a", title: "Case A", description: "Bounded fixture", status: "open", created_by_subject: subject, created_at: now, updated_at: now, version: 2 }, findings: [{ id: "finding-a", case_id: "case-a", title: "Claim A", statement: "Only this execution", classification: "hypothesis", status: "open", version: 1, created_by_subject: subject, created_at: now, updated_at: now }], evidence: [{ case_id: "case-a", finding_id: "finding-a", evidence_event_id: "event-a", relation: "supports", event_hash: "sha256:" + "a".repeat(64), source_ref: "fixture:a", added_by_subject: subject, added_at: now }], gaps: [], annotations: [], alerts: [], executions: [] };
const basis: ReviewBasis = { schema: "xinsight.review-basis.v1", statement: "Only this execution", finding_version: 1, known: { evidence: [], gaps: [] }, selected: { evidence: [], gaps: [] }, adopted_gaps: [], digest: "sha256:" + "b".repeat(64) };
const current = (version = 0): AssessmentView => ({ evaluated_at: now, assessment: { case_id: "case-a", finding_id: "finding-a", recorded: version > 0, version, review: null, progress: null, updated_at: null, updated_by_subject: "" }, case_version: version + 2, case_status: "open", review_basis: { state: "unrecorded", current: null }, progress_basis: { state: "unrecorded", current: null }, overdue: false, history: { items: [], through_version: version, next_before_version: null }, continuations: [], allowed_actions: ["record_review", "record_progress"] });
const responsibility = (): FindingReview => ({ evaluated_at: now, followup: { case_id: "case-a", finding_id: "finding-a", recorded: false, version: 0, assignee_subject_id: null, assignee_name: "", due_at: null, due_timezone: null, updated_at: null, updated_by_subject: "" }, case_version: 2, case_status: "open", history: { items: [], through_version: 0, next_before_version: null }, allowed_actions: ["update_followup"], assignee_state: "unassigned" });
function mocks(value = current()) {
  const get = vi.spyOn(assessmentAPI, "get").mockResolvedValue(value);
  vi.spyOn(workspaceAPI, "getFindingReview").mockResolvedValue(responsibility());
  vi.spyOn(workspaceAPI, "getCase").mockResolvedValue(record);
  vi.spyOn(workspaceAPI, "listCases").mockResolvedValue({ items: [record.case] });
  vi.spyOn(assessmentAPI, "preview").mockImplementation(async (_case, _finding, selected) => ({ ...basis, selected }));
  vi.spyOn(assessmentAPI, "previewCreation").mockImplementation(async (command) => ({ ...basis, selected: command.selection }));
  return get;
}
function receipt(command: AssessmentCommand, version = 1): AssessmentAction { return { operation_id: command.operation_id, operation: command.operation, actor_subject_id: subject, at: now, case_version: version + 2, initial: false, command, basis, previous: current().assessment, current: current(version).assessment }; }
function creation(command: ReviewCreationCommand): ReviewCreationReceipt { return { operation_id: command.operation_id, operation: command.operation, actor_subject_id: subject, at: now, case_id: "case-new", finding_id: "finding-new", case_version: 2, command, basis, action: null }; }
afterEach(() => { cleanup(); sessionStorage.clear(); vi.restoreAllMocks(); });

async function editReview() {
  const user = userEvent.setup(); await user.click(screen.getByText("复核与跟进"));
  await user.click(await screen.findByRole("button", { name: "记录复核结论" }));
  await user.type(await screen.findByLabelText("复核理由"), "Human bounded review");
  await user.click(screen.getByRole("button", { name: "预览本次依据" }));
  await screen.findByRole("button", { name: "确认保存复核" }); return user;
}

describe("Finding lifecycle recovery", () => {
  it("retries the exact command after unknown outcome, then reads current state instead of installing an old receipt", async () => {
    const get = mocks(); const changed = vi.fn();
    const save = vi.spyOn(assessmentAPI, "apply").mockRejectedValueOnce(new TypeError("unknown network outcome")).mockImplementation(async (_c, _f, command) => receipt(command));
    render(<FindingLifecycle record={record} findingID="finding-a" session={session} onChanged={changed} navigate={vi.fn()} />);
    const user = await editReview(); await user.click(screen.getByRole("button", { name: "确认保存复核" }));
    expect(await screen.findByRole("button", { name: "原样重试复核操作" })).toBeTruthy();
    const original = save.mock.calls[0][2]; expect(readPendingReview(subject, "case-a:finding-a")).toEqual(original);
    get.mockResolvedValue(current(3)); await user.click(screen.getByRole("button", { name: "原样重试复核操作" }));
    expect(await screen.findByText(/动作已保存为复核版本 1；当前版本 3/)).toBeTruthy();
    expect(save.mock.calls[1][2]).toEqual(original); expect(readPendingReview(subject, "case-a:finding-a")).toBeUndefined(); expect(changed).toHaveBeenCalledOnce();
  });

  it("requires an explicit reread after a conflict and does not resubmit stale versions", async () => {
    const get = mocks(); const save = vi.spyOn(assessmentAPI, "apply").mockRejectedValueOnce(new ApiRequestError(409));
    render(<FindingLifecycle record={record} findingID="finding-a" session={session} onChanged={vi.fn()} navigate={vi.fn()} />);
    const user = await editReview(); await user.click(screen.getByRole("button", { name: "确认保存复核" }));
    expect(await screen.findByText(/本次修改未保存/)).toBeTruthy(); expect(screen.queryByRole("button", { name: "确认保存复核" })).toBeNull(); expect(readPendingReview(subject, "case-a:finding-a")).toBeUndefined();
    get.mockResolvedValue(current(2)); await user.click(screen.getByRole("button", { name: "重新读取并核对" }));
    await waitFor(() => expect((screen.getByLabelText("复核理由") as HTMLTextAreaElement).value).toBe(""));
    await user.type(screen.getByLabelText("复核理由"), "Updated review after reread"); await user.click(screen.getByRole("button", { name: "预览本次依据" }));
    save.mockImplementation(async (_c, _f, command) => receipt(command, 3));
    await user.click(await screen.findByRole("button", { name: "确认保存复核" }));
    await waitFor(() => expect(save).toHaveBeenCalledTimes(2)); expect(save.mock.calls[1][2].expected_version).toBe(2); expect(save.mock.calls[1][2].operation_id).not.toBe(save.mock.calls[0][2].operation_id);
  });

  it("keeps a confirmed save separate from a failed current-state refresh", async () => {
    const get = mocks(); const save = vi.spyOn(assessmentAPI, "apply").mockImplementation(async (_c, _f, command) => { get.mockRejectedValueOnce(new TypeError("read unavailable")); return receipt(command); });
    render(<FindingLifecycle record={record} findingID="finding-a" session={session} onChanged={vi.fn()} navigate={vi.fn()} />);
    const user = await editReview(); await user.click(screen.getByRole("button", { name: "确认保存复核" }));
    expect(await screen.findByText(/动作已确认保存为复核版本 1/)).toBeTruthy(); expect(screen.queryByRole("button", { name: "原样重试复核操作" })).toBeNull();
    get.mockResolvedValue(current(2)); await user.click(screen.getByRole("button", { name: "重新读取当前记录" }));
    expect(await screen.findByText(/当前版本 2/)).toBeTruthy(); expect(save).toHaveBeenCalledOnce();
  });

  it("clears protected data on permission loss and never gives the reader a retry control", async () => {
    const value = current(1); value.assessment.review = { conclusion: "supported", reason: "Protected adopted conclusion", missing_evidence: "", basis, actor_subject_id: subject, at: now };
    const get = mocks(value);
    const rendered = render(<FindingLifecycle record={record} findingID="finding-a" session={session} onChanged={vi.fn()} navigate={vi.fn()} />);
    const user = userEvent.setup(); await user.click(screen.getByText("复核与跟进")); await screen.findByText("Protected adopted conclusion");
    get.mockRejectedValue(new ApiRequestError(403)); await user.click(screen.getByRole("button", { name: "重新读取当前记录" }));
    await waitFor(() => expect(screen.queryByText("Protected adopted conclusion")).toBeNull());
    rendered.unmount(); const command = { operation: "record_review", operation_id: "pending-id", expected_version: 0, expected_case_version: 2, expected_basis_digest: basis.digest, selection: basis.selected, review: { conclusion: "pending_review", reason: "old request", missing_evidence: "" }, progress: null } as AssessmentCommand;
    keepPendingReview(subject, "case-a:finding-a", command); get.mockResolvedValue({ ...current(), allowed_actions: [] });
    render(<FindingLifecycle record={record} findingID="finding-a" session={readonly} onChanged={vi.fn()} navigate={vi.fn()} />);
    await user.click(screen.getByText("复核与跟进")); await screen.findByText(/复核：未记录/);
    expect(screen.queryByRole("button", { name: "原样重试复核操作" })).toBeNull(); expect(screen.queryByRole("button", { name: "记录复核结论" })).toBeNull();
  });

  it("ignores a late read belonging to a previous Finding even when transport cancellation is ignored", async () => {
    mocks(); let finish!: (v: AssessmentView) => void; const late = new Promise<AssessmentView>((resolve) => { finish = resolve; });
    vi.mocked(assessmentAPI.get).mockImplementation((_case, finding) => finding === "finding-a" ? late : Promise.resolve(current(7)));
    const rendered = render(<FindingLifecycle record={record} findingID="finding-a" session={session} onChanged={vi.fn()} navigate={vi.fn()} />);
    const user = userEvent.setup(); await user.click(screen.getByText("复核与跟进"));
    rendered.rerender(<FindingLifecycle record={record} findingID="finding-b" session={session} onChanged={vi.fn()} navigate={vi.fn()} />);
    await screen.findByText(/复核版本 7/); await act(async () => { finish(current(1)); await late; }); expect(screen.queryByText(/复核版本 1/)).toBeNull();
  });

  it("records explicit progress and ends follow-up with remaining unknowns without changing the review", async () => {
    const v = current(1); v.assessment.progress = { state: "in_progress", reason: "Working", closure: null, basis, actor_subject_id: subject, at: now }; v.assessment.review = { conclusion: "supported", reason: "Original review", missing_evidence: "", basis, actor_subject_id: subject, at: now };
    mocks(v); const save = vi.spyOn(assessmentAPI, "apply").mockImplementation(async (_c, _f, command) => receipt(command, 2));
    render(<FindingLifecycle record={record} findingID="finding-a" session={session} onChanged={vi.fn()} navigate={vi.fn()} />);
    const user = userEvent.setup(); await user.click(screen.getByText("复核与跟进")); await user.click(await screen.findByRole("button", { name: "记录跟进进度" }));
    await user.type(screen.getByLabelText("进度变更理由"), "Bounded effort ended"); await user.type(screen.getByLabelText("结束依据说明"), "No further work in this attempt"); await user.type(screen.getByLabelText("剩余未知"), "Other executions were not observed");
    await user.click(screen.getByRole("button", { name: "预览本次依据" })); await user.click(await screen.findByRole("button", { name: "确认保存进度" }));
    await waitFor(() => expect(save).toHaveBeenCalledOnce()); expect(save.mock.calls[0][2].review).toBeNull(); expect(save.mock.calls[0][2].progress?.closure?.remaining_unknowns).toContain("Other executions"); expect(screen.getByText("Original review")).toBeTruthy();
  });
});

describe("Explicit review basis and closure choices", () => {
  const evidence = { scope: "finding" as const, evidence_event_id: "event-a", relation: "supports" as const, event_hash: "sha256:" + "a".repeat(64), source_ref: "fixture:a" };
  const gap = { scope: "finding" as const, gap_id: "gap-a", relation: "limits" as const, gap_type: "missing_trace", affected_claim: "Effect was not captured" };

  it("selects, changes relation and removes only adopted references, preserving the original available links", async () => {
    const available: BasisSelection = { evidence: [evidence], gaps: [gap] };
    function Editor() {
      const [selected, setSelected] = useState<BasisSelection>({ evidence: [], gaps: [] });
      const [review, setReview] = useState({ conclusion: "pending_review" as const, reason: "", missing_evidence: "" } as import("./assessmentTypes").ReviewPayload);
      return <><ReferencePicker available={available} selected={selected} onChange={setSelected} editableRelations /><ReviewFields value={review} onChange={setReview} /><output>{JSON.stringify({ selected, review })}</output></>;
    }
    render(<Editor />); const user = userEvent.setup();
    await user.click(screen.getByRole("checkbox", { name: /fixture:a/ }));
    await user.selectOptions(screen.getByLabelText("引用关系"), "contradicts");
    await user.click(screen.getByRole("checkbox", { name: /Effect was not captured/ }));
    await user.selectOptions(screen.getByLabelText("结论"), "insufficient_evidence");
    await user.type(screen.getByLabelText(/缺少什么/), "A post-action trace");
    let value = JSON.parse(screen.getByRole("status").textContent!);
    expect(value.selected.evidence[0].relation).toBe("contradicts"); expect(value.selected.gaps).toHaveLength(1); expect(value.review.missing_evidence).toBe("A post-action trace");
    await user.click(screen.getByRole("checkbox", { name: /fixture:a/ })); await user.click(screen.getByRole("checkbox", { name: /Effect was not captured/ }));
    value = JSON.parse(screen.getByRole("status").textContent!); expect(value.selected).toEqual({ evidence: [], gaps: [] }); expect(available.evidence[0].relation).toBe("supports"); expect(available.gaps).toHaveLength(1);
  });

  it("shows immutable adopted Gap contents separately from absence of a resolution", () => {
    const absent = { ...gap, gap_id: "gap-b" };
    render(<ReviewBasisView basis={{ ...basis, known: { evidence: [evidence], gaps: [gap, absent] }, selected: { evidence: [evidence], gaps: [gap, absent] }, adopted_gaps: [{ reference: gap, resolution_recorded: true, resolution_version: 3, state: "investigating", note: "Exact old adopted note", updated_by_subject: subject, updated_at: now }, { reference: absent, resolution_recorded: false, resolution_version: 0, state: "", note: "", updated_by_subject: "", updated_at: null }] }} />);
    expect(screen.getByText("Exact old adopted note")).toBeTruthy(); expect(screen.getByText(/解决记录版本 3/)).toBeTruthy(); expect(screen.getByText(/当时未记录解决说明/)).toBeTruthy();
  });

  it("requires selected verification evidence and explicit human scope/time for a remediation record", async () => {
    const selected = { evidence: [evidence], gaps: [] };
    const v = current(1); v.assessment.progress = { state: "in_progress", reason: "Work began", closure: null, basis: { ...basis, selected }, actor_subject_id: subject, at: now };
    mocks(v); const save = vi.spyOn(assessmentAPI, "apply").mockImplementation(async (_c, _f, c) => receipt(c, 2));
    render(<FindingLifecycle record={record} findingID="finding-a" session={session} onChanged={vi.fn()} navigate={vi.fn()} />);
    const user = userEvent.setup(); await user.click(screen.getByText("复核与跟进")); await user.click(await screen.findByRole("button", { name: "记录跟进进度" }));
    await user.selectOptions(screen.getByLabelText("结束类型"), "verified_remediation");
    await user.type(screen.getByLabelText("进度变更理由"), "Verification complete"); await user.type(screen.getByLabelText("结束依据说明"), "Human checked the result"); await user.type(screen.getByLabelText("人工核验覆盖范围"), "One execution only");
    fireEvent.change(screen.getByLabelText("人工核验时间（本机时区）"), { target: { value: "2026-09-09T12:30" } });
    const pickers = screen.getAllByRole("group", { name: /本次采用的证据/ });
    await user.click(within(pickers[1]).getByRole("checkbox"));
    await user.click(screen.getByRole("button", { name: "预览本次依据" })); await user.click(await screen.findByRole("button", { name: "确认保存进度" }));
    await waitFor(() => expect(save).toHaveBeenCalledOnce()); const closure = save.mock.calls[0][2].progress?.closure;
    expect(closure?.scope).toBe("One execution only"); expect(closure?.verified_at).toMatch(/2026-09-09T/); expect(closure?.verification_evidence).toEqual([evidence]); expect(save.mock.calls[0][2].review).toBeNull();
  });

  it("reads the exact duplicate destination and shows its statement before submission", async () => {
    const v = current(1); v.assessment.progress = { state: "not_started", reason: "Explicit", closure: null, basis, actor_subject_id: subject, at: now };
    mocks(v); const other = { ...record, case: { ...record.case, id: "case-b" }, findings: [{ ...record.findings[0], id: "finding-b", case_id: "case-b", title: "Other judgment", statement: "The exact destination claim" }] };
    vi.mocked(workspaceAPI.getCase).mockImplementation(async (id) => id === "case-b" ? other : record);
    const save = vi.spyOn(assessmentAPI, "apply").mockImplementation(async (_c, _f, c) => receipt(c, 2));
    render(<FindingLifecycle record={record} findingID="finding-a" session={session} onChanged={vi.fn()} navigate={vi.fn()} />);
    const user = userEvent.setup(); await user.click(screen.getByText("复核与跟进")); await user.click(await screen.findByRole("button", { name: "记录跟进进度" }));
    await user.selectOptions(screen.getByLabelText("进度"), "ended"); await user.selectOptions(screen.getByLabelText("结束类型"), "duplicate");
    await user.type(screen.getByLabelText("进度变更理由"), "Same claim"); await user.type(screen.getByLabelText("结束依据说明"), "Matching source and scope"); await user.type(screen.getByLabelText("并入 Case ID"), "case-b"); await user.type(screen.getByLabelText("并入 Finding ID"), "finding-b");
    await user.click(screen.getByRole("button", { name: "预览本次依据" })); await screen.findByText(/The exact destination claim/); expect(save).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "确认保存进度" })); await waitFor(() => expect(save).toHaveBeenCalledOnce()); expect(save.mock.calls[0][2].progress?.closure?.duplicate).toEqual({ case_id: "case-b", finding_id: "finding-b" });
  });
});

async function prepareCreation(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByText("从所选依据开始复核"));
  await user.type(screen.getByLabelText("新案件标题"), "Atomic Case"); await user.type(screen.getByLabelText("调查发现标题"), "New assertion"); await user.type(screen.getByLabelText("明确的判断陈述"), "Only this execution");
  await user.type(screen.getByLabelText("开始复核的原因"), "Human starts investigation"); await user.type(screen.getByLabelText("复核理由"), "Awaiting corroboration");
  await user.click(screen.getByRole("button", { name: "预览采用依据" })); await screen.findByRole("button", { name: "确认开始复核" });
}

describe("Atomic review creation", () => {
  it("uses one formal command and preserves unknown creation identity across remounts", async () => {
    mocks(); const navigate = vi.fn(); const save = vi.spyOn(assessmentAPI, "create").mockRejectedValueOnce(new TypeError("lost response")).mockImplementation(async (c) => creation(c));
    const rendered = render(<ReviewStart session={session} available={{ evidence: [], gaps: [] }} navigate={navigate} />);
    const user = userEvent.setup(); await prepareCreation(user); await user.click(screen.getByRole("button", { name: "确认开始复核" }));
    await screen.findByRole("button", { name: "原样重试创建" }); const original = save.mock.calls[0][0];
    rendered.unmount(); render(<ReviewStart session={session} available={{ evidence: [], gaps: [] }} navigate={navigate} />);
    await user.click(await screen.findByRole("button", { name: "原样重试创建" })); await waitFor(() => expect(navigate).toHaveBeenCalledWith("/cases/case-new#finding-new"));
    expect(save.mock.calls[1][0]).toEqual(original); expect(readPendingReview(subject, "creation")).toBeUndefined();
  });

  it("does not call creation on preview failure or silently retry with changed basis", async () => {
    mocks(); vi.mocked(assessmentAPI.previewCreation).mockRejectedValueOnce(new ApiRequestError(409)); const save = vi.spyOn(assessmentAPI, "create");
    render(<ReviewStart session={session} available={{ evidence: [], gaps: [] }} navigate={vi.fn()} />);
    const user = userEvent.setup(); await user.click(screen.getByText("从所选依据开始复核")); await user.type(screen.getByLabelText("新案件标题"), "Case"); await user.type(screen.getByLabelText("调查发现标题"), "Finding"); await user.type(screen.getByLabelText("明确的判断陈述"), "Statement"); await user.type(screen.getByLabelText("开始复核的原因"), "Reason"); await user.type(screen.getByLabelText("复核理由"), "Pending");
    await user.click(screen.getByRole("button", { name: "预览采用依据" })); await waitFor(() => expect(assessmentAPI.previewCreation).toHaveBeenCalledOnce()); expect(screen.queryByRole("button", { name: "确认开始复核" })).toBeNull(); expect(save).not.toHaveBeenCalled();
  });

  it("uses the latest existing Case version and keeps a confirmed receipt when its current read fails", async () => {
    mocks(); const navigate = vi.fn(); const save = vi.spyOn(assessmentAPI, "create").mockImplementation(async (c) => { vi.mocked(workspaceAPI.getCase).mockRejectedValueOnce(new TypeError("read unavailable")); return creation(c); });
    render(<ReviewStart session={session} available={{ evidence: [], gaps: [] }} defaultCase={record.case} navigate={navigate} />);
    const user = userEvent.setup(); await user.click(screen.getByText("从所选依据开始复核")); await user.type(screen.getByLabelText("调查发现标题"), "Finding"); await user.type(screen.getByLabelText("明确的判断陈述"), "Statement"); await user.type(screen.getByLabelText("开始复核的原因"), "Reason"); await user.type(screen.getByLabelText("复核理由"), "Pending");
    await user.click(screen.getByRole("button", { name: "预览采用依据" })); await user.click(await screen.findByRole("button", { name: "确认开始复核" }));
    await screen.findByRole("button", { name: "重新读取并打开记录" }); expect(save.mock.calls[0][0].expected_case_version).toBe(2); expect(save.mock.calls[0][0].new_case).toBeNull(); expect(navigate).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "重新读取并打开记录" })); await waitFor(() => expect(navigate).toHaveBeenCalledOnce()); expect(save).toHaveBeenCalledOnce();
  });

  it("does not expose any creation form to the investigation reader", () => {
    mocks(); render(<ReviewStart session={readonly} available={{ evidence: [], gaps: [] }} navigate={vi.fn()} />); expect(screen.queryByText("从所选依据开始复核")).toBeNull(); expect(screen.queryByRole("button")).toBeNull();
  });

  it("does not let a late receipt clear a different newer pending command", () => {
    const first: ReviewCreationCommand = { operation: "start_review", operation_id: "one", case_id: "", expected_case_version: 0, new_case: { title: "Case", description: "" }, finding: { title: "Pending", statement: "Claim", classification: "hypothesis" }, selection: { evidence: [], gaps: [] }, expected_basis_digest: basis.digest, review: { conclusion: "pending_review", reason: "Start", missing_evidence: "" }, from: null, reason: "Start" }; const second = { ...first, operation_id: "two" };
    keepPendingReview(subject, "creation", first); expect(() => keepPendingReview(subject, "creation", second)).toThrow(/待确认/);
    clearPendingReview(subject, "creation", first); keepPendingReview(subject, "creation", second); clearPendingReview(subject, "creation", first); expect(readPendingReview(subject, "creation")).toEqual(second);
  });

  it("retains malformed pending data and reports recovery failure instead of rendering a broken creation", () => {
    const key = "xinsight.pending-review.v1:" + subject + ":creation";
    sessionStorage.setItem(key, JSON.stringify({ operation: "start_review", operation_id: "one" }));
    expect(() => readPendingReview(subject, "creation")).toThrow(/内容不完整/); expect(sessionStorage.getItem(key)).not.toBeNull();
  });
});

it("shows current withdrawal separately from unchanged historical adoption",async()=>{
 const v=current(1);
 const scope={source_system_id:"11111111-1111-4111-8111-111111111111",source_instance:"app",org_id:"1",project_id:"2"};
 const reference={scope:"finding" as const,reporter:scope,target:scope,request_id:"request-1",record_ref:"report-1",revision:1,record_hash:"sha256:"+"c".repeat(64),relation:"supports" as const};
 v.review_basis={state:"changed",current:basis,resource_updates:[{reference,state:"withdrawn",latest_revision:2,latest_hash:"sha256:"+"d".repeat(64)}]};
 mocks(v);const apply=vi.spyOn(assessmentAPI,"apply");
 render(<FindingLifecycle record={record} findingID="finding-a" session={session} onChanged={vi.fn()} navigate={vi.fn()}/>);
 await userEvent.click(screen.getByText("复核与跟进"));
 expect(await screen.findByText(/后续修订 2 已声明撤回/)).toBeTruthy();expect(screen.getByText(/已采用修订 1/)).toBeTruthy();expect(apply).not.toHaveBeenCalled();expect(v.review_basis.current?.digest).toBe(basis.digest);
});


it("discloses exact resource hashes and unknown relation codes without using them as checkbox labels",async()=>{
 const hash="sha256:"+"c".repeat(64);
 const resource={record_ref:"resource-reference",revision:7,record_hash:hash,relation:"future_relation",request_id:"request-original",reporter:{source_system_id:"source",source_instance:"instance",org_id:"org",project_id:"project"},target:{source_system_id:"target",source_instance:"instance",org_id:"org",project_id:"project"}};
 const available={evidence:[],gaps:[],resources:[resource]} as unknown as BasisSelection;
 const onChange=vi.fn();render(<ReferencePicker available={available} selected={{evidence:[],gaps:[]}} onChange={onChange}/>);
 const checkbox=screen.getByRole("checkbox",{name:"资源 resource-reference · 修订 7 · 关系待确认"});
 const details=screen.getByText("资源引用详情").closest("details")!;
 expect(details.open).toBe(false);expect(within(details).getByText(hash)).toBeTruthy();expect(within(details).getByText("future_relation")).toBeTruthy();
 const user=userEvent.setup();await user.click(screen.getByText("资源引用详情"));expect(details.open).toBe(true);
 await user.click(checkbox);expect(onChange.mock.calls[0][0].resources).toEqual([resource]);
});
