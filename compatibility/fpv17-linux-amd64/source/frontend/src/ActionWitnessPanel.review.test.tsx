import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { acceptScopeSession, ApiRequestError, runtimeAPI, type AccessSessionResponse } from "./api";
import { ActionWitnessPanel } from "./ActionWitnessPanel";
import * as content from "./content";
import type { ActionWitnesses, Reading, Request } from "./runtime";

// Component regressions only. These mocks do not qualify an external work chain.
const session = { session: { subject_id: "review-reader", session_ref: "session", role: "workbench_investigation_reader", permissions: ["workbench.details.read"] }, scope_revision: "scope" } as AccessSessionResponse;
const reading = { reading_id: "reading", membership_ref: "members" } as Reading;
const scope = { source_system_id: "source", source_instance: "instance", org_id: "org", project_id: "project" };
const request = { ...scope, request_ref: "request-ref", request_id: "request", observation_count: 1, content_eligible_at: "2026-09-25T00:00:00Z" } as Request;
const digest = `sha256:${"a".repeat(64)}`;

function readItem(name: string, id: string): ActionWitnesses["items"][number] {
  return {
    record_hash: `sha256:${id.repeat(64)}`, chain_state: "complete", changed_since_reading: false,
    trigger_request_ref: request.request_ref, final_request_ref: "",
    resolved_content_links: [{ purpose: "result", request_ref: request.request_ref }],
    record: {
      schema_version: "xinsight.resource-action.v1", environment: "test", record_ref: `read-${id}`, revision: 1,
      reporter: { ...scope, source_system_id: "reporter" }, target: scope, request_id: request.request_id,
      occurred_at: "2026-09-25T00:00:00Z", run_id: "run", tool_call_id: `call-${id}`, action_kind: "read",
      object: { ref: `file-${id}`, version: "version-1", name }, tool_result: { state: "completed" },
      native_evidence: { trace_id: "trace", session_id: "native-session", manifest_sha256: digest, events_sha256: digest, call_seq: 1, call_sha256: digest },
      acceptance: { state: "not_checked", goal_state: "not_checked", hash_scope: "unknown" }, withdrawn: false,
      content_links: [{ ...scope, request_id: request.request_id, purpose: "result", extent: "full_file", source_fact_id: `fact-${id}`, revision: "1", content_ref: `body-${id}`, source_sha256: digest, json_pointer: "/text" }],
    },
  };
}

function actionPage(items: ActionWitnesses["items"]): ActionWitnesses {
  return { action_witnesses_version: "xinsight.runtime.action-witnesses.v1", reading_id: reading.reading_id, request_ref: request.request_ref, membership_ref: reading.membership_ref, coverage: "test_development_action_witnesses_only", items };
}

function contentFor(item: ActionWitnesses["items"][number], text: string): content.VerifiedContent {
  const link = item.record.content_links[0], raw = JSON.stringify({ text }), bytes = new TextEncoder().encode(raw);
  const reference: content.FrozenContentReference = { ...link, capture_status: "complete", expires_at: "2099-09-25T00:00:00Z", representation: { media_type: "application/json", encoding: "identity", byte_domain: "stored_representation_before_http_content_encoding", saved_bytes: bytes.length, digest } };
  return { reference, bytes, text: raw, previewOnly: false };
}

function setContentPage(value: content.VerifiedContent) {
  vi.spyOn(content, "fetchContentPage").mockResolvedValue({ version: "xinsight.runtime-content.v1", reading_id: reading.reading_id, request_ref: request.request_ref, membership_ref: reading.membership_ref, content_interpretation_revision: "xinsight.content-membership.v1", member_basis: "content_manifest", items: [{ reference: value.reference, availability: { state: "available", evaluated_at: "", retryable: false }, newer_revision: false }] });
}

beforeEach(() => acceptScopeSession(session));
afterEach(() => { cleanup(); acceptScopeSession(null); vi.restoreAllMocks(); });

it("offers a separate readable control for each actual input object, independent of fixture filenames", async () => {
  const items = [readItem("预算说明.md", "a"), readItem("业务规则.md", "b")];
  vi.spyOn(runtimeAPI, "actionWitnesses").mockResolvedValue(actionPage(items));
  render(<ActionWitnessPanel reading={reading} request={request} session={session} onInvalidated={vi.fn()} />);
  expect(await screen.findByRole("button", { name: /预算说明\.md/ })).toBeTruthy();
  expect(screen.getByRole("button", { name: /业务规则\.md/ })).toBeTruthy();
});

it("labels repeated reads by native call sequence even when the page arrives out of order", async () => {
  const early=readItem("report.md", "a"), late=readItem("report.md", "b");
  early.record.object.ref=late.record.object.ref="same-report";
  early.record.native_evidence.call_seq=17;
  late.record.native_evidence.call_seq=29;
  const values=[early,late].map((item,i)=>contentFor(item,`read-${i}`));
  vi.spyOn(runtimeAPI,"actionWitnesses").mockResolvedValue(actionPage([late,early]));
  vi.spyOn(content,"fetchContentPage").mockResolvedValue({version:"xinsight.runtime-content.v1",reading_id:reading.reading_id,request_ref:request.request_ref,membership_ref:reading.membership_ref,content_interpretation_revision:"xinsight.content-membership.v1",member_basis:"content_manifest",items:values.map(value=>({reference:value.reference,availability:{state:"available",evaluated_at:"",retryable:false},newer_revision:false}))});
  const fetchBody=vi.spyOn(content,"fetchVerifiedContent").mockImplementation(async (_reading,_request,reference)=>values.find(value=>value.reference.source_fact_id===reference.source_fact_id)!);
  render(<ActionWitnessPanel reading={reading} request={request} session={session} onInvalidated={vi.fn()} />);
  fireEvent.click(await screen.findByRole("button",{name:"查看第 1 次读取 report.md"}));
  await waitFor(()=>expect(fetchBody).toHaveBeenCalledOnce());
  expect(fetchBody.mock.calls[0][2].source_fact_id).toBe(early.record.content_links[0].source_fact_id);
  expect(screen.getByRole("button",{name:"查看第 2 次读取 report.md"})).toBeTruthy();
});

it("uses exact time and record reference when repeated names have incomparable runs or native sequences", async () => {
  const crossRun=readItem("report.md","a"), otherRun=readItem("report.md","b");
  otherRun.record.run_id="another-run";
  const sameRun=readItem("notes.md","c"), ambiguous=readItem("notes.md","d");
  sameRun.record.object.ref=ambiguous.record.object.ref="same-notes";
  sameRun.record.native_evidence.call_seq=ambiguous.record.native_evidence.call_seq=7;
  vi.spyOn(runtimeAPI,"actionWitnesses").mockResolvedValue(actionPage([crossRun,otherRun,sameRun,ambiguous]));
  render(<ActionWitnessPanel reading={reading} request={request} session={session} onInvalidated={vi.fn()} />);
  expect(await screen.findByRole("button",{name:"查看读取 report.md（2026-09-25T00:00:00Z；运行 run；记录 read-a）"})).toBeTruthy();
  expect(screen.getByRole("button",{name:"查看读取 report.md（2026-09-25T00:00:00Z；运行 another-run；记录 read-b）"})).toBeTruthy();
  expect(screen.getByRole("button",{name:"查看读取 notes.md（2026-09-25T00:00:00Z；运行 run；记录 read-c）"})).toBeTruthy();
  expect(screen.getByRole("button",{name:"查看读取 notes.md（2026-09-25T00:00:00Z；运行 run；记录 read-d）"})).toBeTruthy();
  expect(screen.queryByRole("button",{name:/查看第 .* 次读取/})).toBeNull();
});

it("does not reuse ordinal labels across two runs of the same named file", async () => {
  const a=readItem("report.md","a"), b=readItem("report.md","b");
  const c=readItem("report.md","c"), d=readItem("report.md","d");
  for(const item of [a,b,c,d])item.record.object.ref="same-report";
  b.record.native_evidence.call_seq=2;
  c.record.run_id=d.record.run_id="other-run";
  d.record.native_evidence.call_seq=2;
  vi.spyOn(runtimeAPI,"actionWitnesses").mockResolvedValue(actionPage([b,d,a,c]));
  render(<ActionWitnessPanel reading={reading} request={request} session={session} onInvalidated={vi.fn()} />);
  expect(await screen.findByRole("button",{name:"查看读取 report.md（2026-09-25T00:00:00Z；运行 run；记录 read-a）"})).toBeTruthy();
  expect(screen.getByRole("button",{name:"查看读取 report.md（2026-09-25T00:00:00Z；运行 run；记录 read-b）"})).toBeTruthy();
  expect(screen.getByRole("button",{name:"查看读取 report.md（2026-09-25T00:00:00Z；运行 other-run；记录 read-c）"})).toBeTruthy();
  expect(screen.getByRole("button",{name:"查看读取 report.md（2026-09-25T00:00:00Z；运行 other-run；记录 read-d）"})).toBeTruthy();
  expect(screen.queryByRole("button",{name:/查看第 .* 次读取/})).toBeNull();
});

it("keeps readable action metadata and reading context when only content access is forbidden", async () => {
  const item = readItem("input.md", "a"), value = contentFor(item, "private-input");
  vi.spyOn(runtimeAPI, "actionWitnesses").mockResolvedValue(actionPage([item]));
  setContentPage(value);
  vi.spyOn(content, "fetchVerifiedContent").mockRejectedValue(new ApiRequestError(403));
  const invalidated = vi.fn();
  render(<ActionWitnessPanel reading={reading} request={request} session={session} onInvalidated={invalidated} />);
  fireEvent.click(await screen.findByRole("button", { name: /查看.*(?:读取资料|input\.md)/ }));
  await waitFor(() => expect(content.fetchVerifiedContent).toHaveBeenCalledOnce());
  await waitFor(() => expect(screen.getByRole("button", { name: /查看.*(?:读取资料|input\.md)/ })).not.toHaveProperty("disabled", true));
  expect(invalidated).not.toHaveBeenCalled();
  expect(screen.queryByText(/private-input/)).toBeNull();
});

it.each(["xinsight:scope-changed", "xinsight:unauthorized"])("discards a late input body after %s", async event => {
  const item = readItem("input.md", "a"), value = contentFor(item, "late-revoked-input");
  vi.spyOn(runtimeAPI, "actionWitnesses").mockResolvedValue(actionPage([item]));
  setContentPage(value);
  let finish!: (value: content.VerifiedContent) => void, signal!: AbortSignal;
  vi.spyOn(content, "fetchVerifiedContent").mockImplementation((_r, _q, _f, _s, s) => { signal = s; return new Promise(resolve => { finish = resolve; }); });
  render(<ActionWitnessPanel reading={reading} request={request} session={session} onInvalidated={vi.fn()} />);
  fireEvent.click(await screen.findByRole("button", { name: /查看.*(?:读取资料|input\.md)/ }));
  await waitFor(() => expect(content.fetchVerifiedContent).toHaveBeenCalledOnce());
  act(() => window.dispatchEvent(new Event(event)));
  expect(signal.aborted).toBe(true);
  await act(async () => finish(value));
  expect(screen.queryByText(/late-revoked-input/)).toBeNull();
  expect(screen.queryByRole("button", { name: /查看.*(?:读取资料|input\.md)/ })).toBeNull();
});

it("removes displayed input at the existing content expiry while keeping action metadata", async () => {
  const item = readItem("input.md", "a"), value = contentFor(item, "short-lived-input");
  value.reference.expires_at = new Date(Date.now() + 500).toISOString();
  vi.spyOn(runtimeAPI, "actionWitnesses").mockResolvedValue(actionPage([item]));
  setContentPage(value);
  vi.spyOn(content, "fetchVerifiedContent").mockResolvedValue(value);
  const invalidated = vi.fn();
  render(<ActionWitnessPanel reading={reading} request={request} session={session} onInvalidated={invalidated} />);
  fireEvent.click(await screen.findByRole("button", { name: /查看.*(?:读取资料|input\.md)/ }));
  expect(await screen.findByText(/short-lived-input/)).toBeTruthy();
  await waitFor(() => expect(screen.queryByText(/short-lived-input/)).toBeNull(), { timeout: 1500 });
  expect(invalidated).not.toHaveBeenCalled();
  expect(screen.getByRole("button", { name: /查看.*(?:读取资料|input\.md)/ })).toBeTruthy();
});

it("keeps an XI copy past original UR expiry and removes it at the verified XI deadline", async () => {
  const item=readItem("input.md","a"), value=contentFor(item,"independent-XI-input");
  value.reference.expires_at=new Date(Date.now()-1000).toISOString();
  value.readableUntil=new Date(Date.now()+500).toISOString();
  vi.spyOn(runtimeAPI,"actionWitnesses").mockResolvedValue(actionPage([item]));
  setContentPage(value);
  vi.spyOn(content,"fetchVerifiedContent").mockResolvedValue(value);
  const invalidated=vi.fn();
  render(<ActionWitnessPanel reading={reading} request={request} session={session} onInvalidated={invalidated}/>);
  fireEvent.click(await screen.findByRole("button",{name:/查看.*input\.md/}));
  expect(await screen.findByText(/independent-XI-input/)).toBeTruthy();
  await waitFor(()=>expect(screen.queryByText(/independent-XI-input/)).toBeNull(),{timeout:1500});
  expect(invalidated).not.toHaveBeenCalled();
  expect(screen.getByRole("button",{name:/查看.*input\.md/})).toBeTruthy();
});

it("bounds the XI copy display by the current reading deadline", async () => {
  const item=readItem("input.md","a"), value=contentFor(item,"reading-bounded-XI-input");
  value.reference.expires_at=new Date(Date.now()-1000).toISOString();
  value.readableUntil=new Date(Date.now()+60000).toISOString();
  const shortReading={...reading,expires_at:new Date(Date.now()+500).toISOString()};
  vi.spyOn(runtimeAPI,"actionWitnesses").mockResolvedValue(actionPage([item]));
  setContentPage(value);
  vi.spyOn(content,"fetchVerifiedContent").mockResolvedValue(value);
  render(<ActionWitnessPanel reading={shortReading} request={request} session={session} onInvalidated={vi.fn()}/>);
  fireEvent.click(await screen.findByRole("button",{name:/查看.*input\.md/}));
  expect(await screen.findByText(/reading-bounded-XI-input/)).toBeTruthy();
  await waitFor(()=>expect(screen.queryByText(/reading-bounded-XI-input/)).toBeNull(),{timeout:1500});
});

it("shows each provider attempt separately and reads only the selected exact body", async () => {
  const item = readItem("input.md", "a"), value = contentFor(item, "unused");
  vi.spyOn(runtimeAPI, "actionWitnesses").mockResolvedValue(actionPage([item]));
  const raw = JSON.stringify({ messages: [{ role: "user", content: "second-attempt-message" }] });
  const bytes = new TextEncoder().encode(raw);
  const refs = ["client", "attempt-one", "attempt-two"].map(id => ({ ...value.reference, source_fact_id: id, content_ref: `body-${id}`, representation: { ...value.reference.representation!, saved_bytes: bytes.length } }));
  vi.spyOn(content, "fetchContentPage").mockResolvedValue({ version: "xinsight.runtime-content.v1", reading_id: reading.reading_id, request_ref: request.request_ref, membership_ref: reading.membership_ref, content_interpretation_revision: "xinsight.content-membership.v1", member_basis: "content_manifest", items: refs.map((reference, i) => ({ reference, observation: { protocol: "openai.chat_completions.v1", observation_point: i ? "provider_request" : "client_request", attempt_id: i ? `attempt-${i}` : undefined, content_item_id: reference.content_ref!, occurred_at: "2026-09-25T00:00:00Z", capture_reason: "none" }, availability: { state: "available", evaluated_at: "", retryable: false }, newer_revision: false })) });
  const fetchBody = vi.spyOn(content, "fetchVerifiedContent").mockResolvedValue({ reference: refs[2], bytes, text: raw, previewOnly: false });
  render(<ActionWitnessPanel reading={reading} request={request} session={session} onInvalidated={vi.fn()} />);
  fireEvent.click(await screen.findByText("查看已关联的模型请求"));
  fireEvent.click(screen.getByRole("button", { name: "查看调用 request 的提交内容" }));
  const buttons = await screen.findAllByRole("button", { name: "读取并核验正文" });
  expect(buttons).toHaveLength(2);
  expect(screen.queryByText("客户输入（改写前）")).toBeNull();
  expect(fetchBody).not.toHaveBeenCalled();
  fireEvent.click(buttons[1]);
  expect(await screen.findByText("second-attempt-message")).toBeTruthy();
  expect(fetchBody.mock.calls[0][2]).toEqual(refs[2]);
  expect(screen.getByRole("region", { name: "结构化内容" })).toBeTruthy();
});

it.each(["failed", "unknown"] as const)("shows the reported %s action with an unchecked file result even without a final request", async state => {
  const item = readItem("report.md", "a");
  item.record.action_kind = "edit";
  item.record.tool_result.state = state;
  vi.spyOn(runtimeAPI, "actionWitnesses").mockResolvedValue(actionPage([item]));
  render(<ActionWitnessPanel reading={reading} request={request} session={session} onInvalidated={vi.fn()} />);
  const table = await screen.findByRole("table", { name: "读取与修改结果" });
  expect(table.textContent).toContain(state === "failed" ? "运行时报告失败" : "运行时结果未确认");
  expect(table.textContent).toContain("文件结果尚未独立检查");
  expect(table.textContent).not.toContain("确认内容变化");
  expect(screen.queryByRole("button", { name: "查看最后一次调用的提交内容" })).toBeNull();
});
