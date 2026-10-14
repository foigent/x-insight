import { t, tx, useLocale } from "./i18n";
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
  useLocale();
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
    <summary>{from ? t("m1849") : t("m1850")}</summary>
    {open ? <>
    {error ? <ErrorState error={error} /> : null}
    {conflict ? <p role="alert">{tx("m1851")}</p> : null}
    {denied ? <p role="alert">{tx("m1852")}</p> : null}
    {allowed && pending ? <div className="stack-form"><p role="alert">{tx("m1853")}</p><p><strong>{pending.finding.title}</strong> · {pending.case_id || pending.new_case?.title}</p><p>{pending.finding.statement}</p><p>{pending.reason}</p><SelectionSummary selection={pending.selection} /><button className="button button-primary" type="button" disabled={busy} onClick={() => void submit()}>{tx("m1854")}</button></div> : null}
    {allowed && receipt ? <div><p role="status">{tx("m1855")}</p><details className="technical-reference"><summary>{tx("m1856")}</summary><code>{receipt.case_id} / {receipt.finding_id}</code></details><button type="button" className="button button-primary" disabled={busy} onClick={() => void refreshReceipt()}>{tx("m1857")}</button></div> : null}
    {allowed && !pending && !receipt ? <form className="stack-form" onSubmit={(event) => void prepare(event)} onChangeCapture={invalidate}>
      {from ? <div><p>{tx("m1858", { value0: from.revision })}</p><details className="technical-reference"><summary>{tx("m1859")}</summary><code>{from.case_id} / {from.finding_id}</code></details></div> : null}
      <fieldset disabled={locked} className="stack-form"><legend>{tx("m1860")}</legend>
        <label>{t("m0990")}<select value={target} onChange={(event) => setTarget(event.target.value)}><option value="">{tx("m1861")}</option>{cases.map((c) => <option key={c.id} value={c.id}>{c.title}</option>)}{target && !cases.some((c) => c.id === target) ? <option value={target}>{target}</option> : null}</select></label>
        <label>{t("m1862")}<input value={target} onChange={(event) => setTarget(event.target.value)} /></label><p className="quiet-copy">{tx("m1863")}</p>
        {!target ? <><label>{t("m1864")}<input value={caseTitle} maxLength={160} onChange={(event) => setCaseTitle(event.target.value)} /></label><label>{t("m1865")}<textarea value={description} maxLength={4000} onChange={(event) => setDescription(event.target.value)} /></label></> : null}
        <label>{t("m1866")}<input value={title} maxLength={160} onChange={(event) => setTitle(event.target.value)} /></label>
        <label>{t("m1867")}<textarea value={statement} maxLength={8000} rows={3} onChange={(event) => setStatement(event.target.value)} /></label>
        <label>{from ? t("m1868") : t("m1869")}<textarea value={reason} maxLength={4000} rows={2} onChange={(event) => setReason(event.target.value)} /></label>
      </fieldset>
      <ReferencePicker available={available} selected={selection} onChange={setSelection} disabled={locked} editableRelations />
      {executionContext && !from ? <div><label><input type="checkbox" checked={linkExecution} disabled={locked} onChange={event => setLinkExecution(event.target.checked)} />{t("m1870")}</label><p className="quiet-copy">{tx("m1871")}</p></div> : null}
      {!from ? <ReviewFields value={review} onChange={setReview} disabled={locked} /> : null}
      <p className="quiet-copy">{tx("m1872")}</p>
      <button className="button button-secondary" disabled={locked || !title.trim() || !statement.trim() || !reason.trim() || (!target && !caseTitle.trim()) || (!from && !review.reason.trim())}>{busy ? t("m1150") : t("m1873")}</button>
      {preview ? <div className="stack-form"><p>{tx("m1876", { value0: preview.targetTitle, value1: preview.command.case_id ? t("m1874", { value0: preview.command.expected_case_version }) : t("m1875") })}</p><ReviewBasisView basis={preview.basis} label={t("m1877")} /><button className="button button-primary" type="button" disabled={busy} onClick={() => void submit()}>{from ? t("m1878") : t("m1879")}</button></div> : null}
    </form> : null}
    </> : null}
  </details>;
}
