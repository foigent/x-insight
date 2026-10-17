import { contentKey } from "./content";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiRequestError, assessmentAPI, isAbortError, workspaceAPI, type AccessSessionResponse, type CaseRecord, type InvestigationCase } from "./api";
import { resourceKey, type ExecutionContext, BasisSelection, ReviewBasis, ReviewCreationCommand, ReviewCreationReceipt, ReviewPayload, SealedFindingReference } from "./assessmentTypes";
import { ErrorState } from "./Feedback";
import { ReferencePicker, ReviewBasisView, ReviewFields, SelectionSummary } from "./ReviewBasisView";
import { clearPendingReview, keepPendingReview, readPendingReview } from "./pendingReview";

interface Props { executionContext?: ExecutionContext; session: AccessSessionResponse; available: BasisSelection; defaultCase?: InvestigationCase; from?: SealedFindingReference; navigate: (path: string) => void }
const initialReview: ReviewPayload = { conclusion: "pending_review", reason: "", missing_evidence: "" };

export function newFindingReferences(record: CaseRecord): BasisSelection {
  return {
    contents: [...new Map((record.contents??[]).map(r=>{const ref={...r.reference,scope:"finding" as const};return [contentKey(ref),ref];})).values()],
    resources: [...new Map((record.resources??[]).map(r=>{const ref={...r.reference,scope:"finding" as const};return [resourceKey(ref),ref];})).values()],
    evidence: [...new Map(record.evidence.map((e) => [e.evidence_event_id, { scope: "finding" as const, evidence_event_id: e.evidence_event_id, relation: e.relation, event_hash: e.event_hash, source_ref: e.source_ref }])).values()],
    gaps: [...new Map(record.gaps.map((g) => [g.gap_id, { scope: "finding" as const, gap_id: g.gap_id, relation: g.relation, affected_claim: g.affected_claim, gap_type: g.gap_type }])).values()],
  };
}

export function ReviewStart({ executionContext, session, available, defaultCase, from, navigate }: Props) {
  const [open, setOpen] = useState(false);
  const [linkExecution, setLinkExecution] = useState(true);
  const [cases, setCases] = useState<InvestigationCase[]>([]);
  const [target, setTarget] = useState(defaultCase?.id ?? "");
  const [caseTitle, setCaseTitle] = useState("");
  const [description, setDescription] = useState("");
  const [title, setTitle] = useState("");
  const [statement, setStatement] = useState("");
  const [selection, setSelection] = useState<BasisSelection>({ evidence: [], gaps: [] });
  const [review, setReview] = useState(initialReview);
  const [reason, setReason] = useState("");
  const [preview, setPreview] = useState<{ command: ReviewCreationCommand; basis: ReviewBasis; targetTitle: string }>();
  const [pending, setPending] = useState<ReviewCreationCommand>();
  const [receipt, setReceipt] = useState<ReviewCreationReceipt>();
  const [error, setError] = useState<unknown>();
  const [conflict, setConflict] = useState(false);
  const [denied, setDenied] = useState(false);
  const [busy, setBusy] = useState(false);
  const life = useRef(new AbortController());
  const generation = useRef(0);
  const subject = session.session.subject_id;
  const recoverySubject=session.session.permissions.includes("investigation.scoped.read")?`${subject}:scope:${session.scope_revision ?? "unverified-session"}`:subject;
  const resource = "creation";
  const writable = (session.session.permissions.includes("investigation.write") || session.session.permissions.includes("investigation.scoped.write"));
  const allowed = writable && !denied;

  useEffect(() => {
    const token = ++generation.current; const controller = new AbortController(); life.current = controller;
    try { const saved = readPendingReview<ReviewCreationCommand>(recoverySubject, resource); setPending(saved); if (saved) setOpen(true); }
    catch (caught) { setError(caught); }
    if (writable && open) void workspaceAPI.listCases(controller.signal).then((value) => { if (token === generation.current) setCases(value.items.filter((c) => c.status === "open")); }).catch((caught) => { if (token === generation.current && !isAbortError(caught)) setError(caught); });
    return () => { generation.current++; controller.abort(); };
  }, [subject, recoverySubject, writable, open]);

  useEffect(() => { setPreview(undefined); setReceipt(undefined); setSelection({evidence:[],gaps:[]}); setCases([]); }, [recoverySubject]);

  const locked = busy || Boolean(pending) || Boolean(receipt);
  function invalidate() { if (!locked) { setPreview(undefined); setConflict(false); } }
  function failure(caught: unknown, token: number) {
    if (token !== generation.current || isAbortError(caught)) return;
    if (caught instanceof ApiRequestError && [401, 403, 404].includes(caught.status)) { setDenied(true); setPreview(undefined); setCases([]); }
    setError(caught);
  }

  async function prepare(event: FormEvent) {
    event.preventDefault();
    if (locked || !allowed || !title.trim() || !statement.trim() || !reason.trim() || (!target && !caseTitle.trim()) || (!from && !review.reason.trim())) return;
    const token = generation.current; setBusy(true); setError(undefined); setPreview(undefined); setConflict(false);
    try {
      const currentCase = target ? (await workspaceAPI.getCase(target.trim(), life.current.signal)).case : undefined;
      if (token !== generation.current) return;
      const command: ReviewCreationCommand = {
        operation: from ? "continue_finding" : "start_review", operation_id: crypto.randomUUID(), case_id: currentCase?.id ?? "", expected_case_version: currentCase?.version ?? 0,
        new_case: currentCase ? null : { title: caseTitle.trim(), description }, finding: { title: title.trim(), statement: statement.trim(), classification: "hypothesis" },
        ...(executionContext && linkExecution && !from && ((selection.resources?.length??0)+(selection.contents?.length??0)>0) ? { execution_context: executionContext } : {}),
        selection, expected_basis_digest: "", review: from ? null : review, from: from ?? null, reason: reason.trim(),
      };
      const basis = await assessmentAPI.previewCreation(command, session.csrf_token, life.current.signal);
      if (command.execution_context) {
        const resolved = basis.resolved_execution_context?.context;
        if (!resolved || resolved.execution_id !== command.execution_context.execution_id || resolved.workbench_reading_id !== command.execution_context.workbench_reading_id || resolved.runtime_reading_id !== command.execution_context.runtime_reading_id || resolved.runtime_request_ref !== command.execution_context.runtime_request_ref || resolved.execution_as_of !== command.execution_context.execution_as_of) throw new ApiRequestError(409);
        if (command.selection.contents?.length) {
          const keys = (items: BasisSelection["contents"]) => JSON.stringify((items??[]).map(contentKey).sort());
          if (keys(basis.resolved_execution_context?.contents) !== keys(command.selection.contents) || keys(basis.selected.contents) !== keys(command.selection.contents)) throw new ApiRequestError(409);
        }
        command.execution_context = resolved;
      }
      if (token === generation.current) setPreview({ command: { ...command, selection: basis.selected, expected_basis_digest: basis.digest }, basis, targetTitle: currentCase?.title ?? caseTitle.trim() });
    } catch (caught) { failure(caught, token); }
    finally { if (token === generation.current) setBusy(false); }
  }

  async function openReceipt(value: ReviewCreationReceipt, token: number) {
    await workspaceAPI.getCase(value.case_id, life.current.signal);
    if (token === generation.current) navigate(`/cases/${value.case_id}#${value.finding_id}`);
  }

  async function submit() {
    const command = pending ?? preview?.command;
    if (!command || busy || receipt || !allowed) return;
    const token = generation.current; setBusy(true); setError(undefined);
    let confirmed = false;
    try {
      keepPendingReview(recoverySubject, resource, command); setPending(command);
      const value = await assessmentAPI.create(command, session.csrf_token, life.current.signal);
      // Receipt cleanup is bound to the original subject and exact command,
      // even if navigation finished while the transport ignored cancellation.
      confirmed = true; clearPendingReview(recoverySubject, resource, command);
      if (token !== generation.current) return;
      setReceipt(value); setPending(undefined); setPreview(undefined);
      await openReceipt(value, token);
    } catch (caught) {
      if (token !== generation.current || isAbortError(caught)) return;
      if (!confirmed && caught instanceof ApiRequestError && [400, 409, 413, 415].includes(caught.status)) {
        clearPendingReview(recoverySubject, resource, command); setPending(undefined); setPreview(undefined); setConflict(caught.status === 409);
      }
      failure(caught, token);
    } finally { if (token === generation.current) setBusy(false); }
  }

  async function refreshReceipt() {
    if (!receipt || busy) return; const token = generation.current; setBusy(true);
    try { await openReceipt(receipt, token); } catch (caught) { failure(caught, token); }
    finally { if (token === generation.current) setBusy(false); }
  }

  if (!writable) return null;
  return <details className="inline-editor review-start" open={open} onToggle={(event) => setOpen(event.currentTarget.open)}>
    <summary>{from ? "从封存调查发现续办" : "从所选依据开始复核"}</summary>
    {open ? <>
    {error ? <ErrorState error={error} /> : null}
    {conflict ? <p role="alert">案件或采用依据已变化，本次创建未保存。请重新预览并核对后提交。</p> : null}
    {denied ? <p role="alert">当前无法访问该记录或执行操作。请核对会话与权限；待确认操作保留在本标签页中。</p> : null}
    {allowed && pending ? <div className="stack-form"><p role="alert">尚未确认创建结果。本标签页保留了原操作，刷新后仍可原样重试，不会创建第二套记录。</p><p><strong>{pending.finding.title}</strong> · {pending.case_id || pending.new_case?.title}</p><p>{pending.finding.statement}</p><p>{pending.reason}</p><SelectionSummary selection={pending.selection} /><button className="button button-primary" type="button" disabled={busy} onClick={() => void submit()}>原样重试创建</button></div> : null}
    {allowed && receipt ? <div><p role="status">创建已确认。当前记录尚未打开，无需重新提交。</p><details className="technical-reference"><summary>创建记录引用</summary><code>{receipt.case_id} / {receipt.finding_id}</code></details><button type="button" className="button button-primary" disabled={busy} onClick={() => void refreshReceipt()}>重新读取并打开记录</button></div> : null}
    {allowed && !pending && !receipt ? <form className="stack-form" onSubmit={(event) => void prepare(event)} onChangeCapture={invalidate}>
      {from ? <div><p>续办自封存版本 {from.revision}。新调查发现从未复核、责任与进度未记录开始。</p><details className="technical-reference"><summary>续办来源引用</summary><code>{from.case_id} / {from.finding_id}</code></details></div> : null}
      <fieldset disabled={locked} className="stack-form"><legend>调查范围与判断</legend>
        <label>目标案件<select value={target} onChange={(event) => setTarget(event.target.value)}><option value="">新建案件</option>{cases.map((c) => <option key={c.id} value={c.id}>{c.title}</option>)}{target && !cases.some((c) => c.id === target) ? <option value={target}>{target}</option> : null}</select></label>
        <label>已有案件标识（可直接输入；留空为新建）<input value={target} onChange={(event) => setTarget(event.target.value)} /></label><p className="quiet-copy">列表仅包含最近一批开放案件。预览会重新读取所选对象及其版本。</p>
        {!target ? <><label>新案件标题<input value={caseTitle} maxLength={160} onChange={(event) => setCaseTitle(event.target.value)} /></label><label>案件说明<textarea value={description} maxLength={4000} onChange={(event) => setDescription(event.target.value)} /></label></> : null}
        <label>调查发现标题<input value={title} maxLength={160} onChange={(event) => setTitle(event.target.value)} /></label>
        <label>明确的判断陈述<textarea value={statement} maxLength={8000} rows={3} onChange={(event) => setStatement(event.target.value)} /></label>
        <label>{from ? "续办原因" : "开始复核的原因"}<textarea value={reason} maxLength={4000} rows={2} onChange={(event) => setReason(event.target.value)} /></label>
      </fieldset>
      <ReferencePicker available={available} selected={selection} onChange={setSelection} disabled={locked} editableRelations />
      {executionContext && !from ? <div><label><input type="checkbox" checked={linkExecution} disabled={locked} onChange={event => setLinkExecution(event.target.checked)} />同时保留本次执行阅读关联</label><p className="quiet-copy">勾选资源或内容后，将本次执行一并关联到案件。两份阅读分别核对，截止时间见预览；失效后需重新读取。取消此项可仅复核所选资源或内容。</p></div> : null}
      {!from ? <ReviewFields value={review} onChange={setReview} disabled={locked} /> : null}
      <p className="quiet-copy">说明与依据会进入历史及封存包；不要粘贴受限来源原文或凭据。</p>
      <button className="button button-secondary" disabled={locked || !title.trim() || !statement.trim() || !reason.trim() || (!target && !caseTitle.trim()) || (!from && !review.reason.trim())}>{busy ? "正在读取…" : "预览采用依据"}</button>
      {preview ? <div className="stack-form"><p>目标：{preview.targetTitle} · {preview.command.case_id ? `预期案件版本 ${preview.command.expected_case_version}` : "将与调查发现一次创建"}</p><ReviewBasisView basis={preview.basis} label="服务端读取的采用依据" /><button className="button button-primary" type="button" disabled={busy} onClick={() => void submit()}>{from ? "确认原子续办" : "确认开始复核"}</button></div> : null}
    </form> : null}
    </> : null}
  </details>;
}
