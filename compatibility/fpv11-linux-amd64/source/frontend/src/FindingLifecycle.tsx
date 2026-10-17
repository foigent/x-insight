import { contentKey, type ContentChoice } from "./content";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiRequestError, assessmentAPI, isAbortError, workspaceAPI, type AccessSessionResponse, type CaseRecord, type FindingReview } from "./api";
import { closureLabels, combinedFindingHistory, conclusionLabels, evidenceKey, resourceKey, type ResourceChoice, progressLabels, type AssessmentAction, type AssessmentCommand, type AssessmentView, type BasisSelection, type Closure, type EvidenceChoice, type ProgressPayload, type ProgressState, type ReviewBasis, type ReviewPayload } from "./assessmentTypes";
import { FindingResponsibility } from "./FindingResponsibility";
import { ErrorState } from "./Feedback";
import { formatDateTime } from "./format";
import { ReferencePicker, ReviewBasisView, ReviewFields, SelectionSummary, ResourceUpdates } from "./ReviewBasisView";
import { ReviewStart } from "./ReviewStart";
import { clearPendingReview, keepPendingReview, readPendingReview } from "./pendingReview";

interface Props { record: CaseRecord; findingID: string; session: AccessSessionResponse; onChanged: () => void; navigate: (path: string) => void }

function findingReferences(record: CaseRecord, findingID: string): BasisSelection {
  return {
    contents: record.contents?.filter(r=>!r.finding_id||r.finding_id===findingID).map(r=>r.reference),
    resources: record.resources?.filter(r=>!r.finding_id||r.finding_id===findingID).map(r=>r.reference),
    evidence: record.evidence.filter((e) => !e.finding_id || e.finding_id === findingID).map((e) => ({ scope: e.finding_id ? "finding" : "case", evidence_event_id: e.evidence_event_id, relation: e.relation, event_hash: e.event_hash, source_ref: e.source_ref })),
    gaps: record.gaps.filter((g) => !g.finding_id || g.finding_id === findingID).map((g) => ({ scope: g.finding_id ? "finding" : "case", gap_id: g.gap_id, relation: g.relation, affected_claim: g.affected_claim, gap_type: g.gap_type })),
  };
}

export function FindingLifecycle({ record, findingID, session, onChanged, navigate }: Props) {
  const caseID = record.case.id;
  const [open, setOpen] = useState(false);
  const [view, setView] = useState<AssessmentView>();
  const [responsibility, setResponsibility] = useState<FindingReview>();
  const [pickerRecord, setPickerRecord] = useState(record);
  const [mode, setMode] = useState<"record_review" | "record_progress">();
  const [base, setBase] = useState<AssessmentView>();
  const [selection, setSelection] = useState<BasisSelection>({ evidence: [], gaps: [] });
  const [review, setReview] = useState<ReviewPayload>({ conclusion: "pending_review", reason: "", missing_evidence: "" });
  const [progress, setProgress] = useState<ProgressState>("not_started");
  const [reason, setReason] = useState("");
  const [closureKind, setClosureKind] = useState<Closure["kind"]>("end_followup");
  const [explanation, setExplanation] = useState("");
  const [scope, setScope] = useState("");
  const [verifiedAt, setVerifiedAt] = useState("");
  const [contentVerification,setContentVerification]=useState<ContentChoice[]>([]);
  const [resourceVerification,setResourceVerification]=useState<ResourceChoice[]>([]);
  const [verification, setVerification] = useState<EvidenceChoice[]>([]);
  const [duplicateCase, setDuplicateCase] = useState("");
  const [duplicateFinding, setDuplicateFinding] = useState("");
  const [remainingUnknowns, setRemainingUnknowns] = useState("");
  const [preview, setPreview] = useState<{ basis: ReviewBasis; command: AssessmentCommand; duplicateTitle?: string; duplicateStatement?: string }>();
  const [pending, setPending] = useState<AssessmentCommand>();
  const [saved, setSaved] = useState<AssessmentAction>();
  const [busy, setBusy] = useState(false);
  const [denied, setDenied] = useState(false);
  const [conflict, setConflict] = useState(false);
  const [error, setError] = useState<unknown>();
  const [notice, setNotice] = useState("");
  const generation = useRef(0); const sequence = useRef(0); const life = useRef(new AbortController());
  const subject = session.session.subject_id;
  const recoverySubject=session.session.permissions.includes("investigation.scoped.read")?`${subject}:scope:${session.scope_revision ?? "unverified-session"}`:subject; const resource = `${caseID}:${findingID}`;
  const canWrite = (session.session.permissions.includes("investigation.write") || session.session.permissions.includes("investigation.scoped.write"));

  function failed(caught: unknown, token: number) {
    if (token !== generation.current || isAbortError(caught)) return;
    if (caught instanceof ApiRequestError && [401, 403, 404].includes(caught.status)) { setView(undefined); setResponsibility(undefined); setMode(undefined); setPreview(undefined); setDenied(true); setNotice(""); }
    setError(caught);
  }

  async function read() {
    const token = generation.current; const request = ++sequence.current;
    const [assessment, old] = await Promise.all([assessmentAPI.get(caseID, findingID, life.current.signal), workspaceAPI.getFindingReview(caseID, findingID, life.current.signal)]);
    if (token !== generation.current || request !== sequence.current) return;
    setView(assessment); setResponsibility(old); setError(undefined); return assessment;
  }

  useEffect(() => {
    const token = ++generation.current; const controller = new AbortController(); life.current = controller;
    setSelection({evidence:[],gaps:[]}); setContentVerification([]); setResourceVerification([]); setVerification([]);
    setView(undefined); setResponsibility(undefined); setMode(undefined); setBase(undefined); setPreview(undefined); setSaved(undefined); setBusy(false); setError(undefined); setDenied(false); setConflict(false); setNotice("");
    try { setPending(readPendingReview<AssessmentCommand>(recoverySubject, resource)); } catch (caught) { setError(caught); }
    if (open) void read().catch((caught) => failed(caught, token));
    return () => { generation.current++; controller.abort(); };
    // The panel lifetime follows its resource, permission session and seal state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, caseID, findingID, recoverySubject, record.case.status]);

  async function changed() {
    onChanged(); if (!open) return; const token = generation.current;
    try { await read(); } catch (caught) { failed(caught, token); }
  }

  async function edit(nextMode: "record_review" | "record_progress") {
    if (busy || pending || saved || denied) return;
    const token = generation.current; setBusy(true); setNotice("");
    try {
      const [current, latestRecord] = await Promise.all([read(), workspaceAPI.getCase(caseID, life.current.signal)]);
      if (!current || token !== generation.current) return;
      if (!current.allowed_actions.includes(nextMode)) { setMode(undefined); onChanged(); return; }
      setPickerRecord(latestRecord); setBase(current); setMode(nextMode); setPreview(undefined); setConflict(false); setReason(""); setExplanation(""); setScope(""); setVerifiedAt(""); setVerification([]); setResourceVerification([]); setDuplicateCase(""); setDuplicateFinding(""); setRemainingUnknowns("");
      const old = current.assessment;
      setSelection((nextMode === "record_review" ? old.review?.basis.selected : old.progress?.basis.selected ?? old.review?.basis.selected) ?? { evidence: [], gaps: [] });
      setReview({ conclusion: old.review?.conclusion ?? "pending_review", reason: "", missing_evidence: old.review?.missing_evidence ?? "" });
      setProgress(!old.progress ? "not_started" : old.progress.state === "in_progress" ? "ended" : "in_progress");
    } catch (caught) { failed(caught, token); }
    finally { if (token === generation.current) setBusy(false); }
  }

  const draftLocked = busy || Boolean(pending) || Boolean(saved) || conflict;
  function invalidate() { if (!draftLocked) setPreview(undefined); }

  async function prepare(event: FormEvent) {
    event.preventDefault(); if (!base || !mode || draftLocked || denied) return;
    const token = generation.current; setBusy(true); setError(undefined); setPreview(undefined);
    try {
      let payload: ProgressPayload | null = null; let duplicateTitle: string | undefined; let duplicateStatement: string | undefined;
      if (mode === "record_progress") {
        let closure: Closure | null = null;
        if (progress === "ended") {
          closure = { kind: closureKind, explanation, scope: "", verified_at: null, verification_evidence: [], duplicate: null, remaining_unknowns: "" };
          if (closureKind === "verified_remediation") { closure.verification_contents=contentVerification.filter(v=>selection.contents?.some(c=>contentKey(c)===contentKey(v))); closure.verification_resources=resourceVerification.filter(v=>selection.resources?.some(r=>resourceKey(r)===resourceKey(v))); closure.scope = scope; closure.verified_at = new Date(verifiedAt).toISOString(); closure.verification_evidence = verification.filter((v) => selection.evidence.some((e) => evidenceKey(e) === evidenceKey(v))); }
          if (closureKind === "end_followup") closure.remaining_unknowns = remainingUnknowns;
          if (closureKind === "duplicate") {
            const other = await workspaceAPI.getCase(duplicateCase.trim(), life.current.signal);
            const target = other.findings.find((f) => f.id === duplicateFinding.trim());
            if (!target) throw new Error("并入 Finding 不属于所选 Case，请重新核对。");
            closure.duplicate = { case_id: other.case.id, finding_id: target.id }; duplicateTitle = target.title; duplicateStatement = target.statement;
          }
        }
        payload = { state: progress, reason, closure };
      }
      const basis = await assessmentAPI.preview(caseID, findingID, selection, session.csrf_token, life.current.signal);
      if (token !== generation.current) return;
      setPreview({ basis, duplicateTitle, duplicateStatement, command: { operation: mode, operation_id: crypto.randomUUID(), expected_version: base.assessment.version, expected_case_version: base.case_version, expected_basis_digest: basis.digest, selection: basis.selected, review: mode === "record_review" ? review : null, progress: payload } });
    } catch (caught) { failed(caught, token); }
    finally { if (token === generation.current) setBusy(false); }
  }

  async function finish(receipt: AssessmentAction) {
    const current = await read(); if (!current) return;
    setNotice(`动作已保存为复核版本 ${receipt.current.version}；当前版本 ${current.assessment.version}。`); setSaved(undefined); setPending(undefined); setMode(undefined); setPreview(undefined); setConflict(false); onChanged();
  }

  async function submit() {
    const command = pending ?? preview?.command;
    if (!command || busy || saved || denied || !canWrite) return;
    const token = generation.current; setBusy(true); setError(undefined); let confirmed = false;
    try {
      keepPendingReview(recoverySubject, resource, command); setPending(command);
      const receipt = await assessmentAPI.apply(caseID, findingID, command, session.csrf_token, life.current.signal);
      confirmed = true; clearPendingReview(recoverySubject, resource, command);
      if (token !== generation.current) return;
      setSaved(receipt); setPending(undefined); await finish(receipt);
    } catch (caught) {
      if (token !== generation.current || isAbortError(caught)) return;
      if (!confirmed && caught instanceof ApiRequestError && [400, 409, 413, 415].includes(caught.status)) { clearPendingReview(recoverySubject, resource, command); setPending(undefined); setPreview(undefined); setConflict(caught.status === 409); }
      failed(caught, token);
    } finally { if (token === generation.current) setBusy(false); }
  }

  async function refresh() {
    if (busy) return; const token = generation.current; setBusy(true);
    try { if (saved) await finish(saved); else await read(); } catch (caught) { failed(caught, token); }
    finally { if (token === generation.current) setBusy(false); }
  }

  async function more(kind: "assessment" | "responsibility") {
    if (!view || !responsibility || busy) return; const token = generation.current; const request = sequence.current; setBusy(true);
    try {
      if (kind === "assessment" && view.history.next_before_version) {
        const h = await assessmentAPI.history(caseID, findingID, view.history.through_version, view.history.next_before_version, life.current.signal);
        if (token === generation.current && request === sequence.current) setView({ ...view, history: { ...h, items: [...view.history.items, ...h.items] } });
      } else if (kind === "responsibility" && responsibility.history.next_before_version) {
        const h = await workspaceAPI.getFindingHistory(caseID, findingID, responsibility.history.through_version, responsibility.history.next_before_version, life.current.signal);
        if (token === generation.current && request === sequence.current) setResponsibility({ ...responsibility, history: { ...h, items: [...responsibility.history.items, ...h.items] } });
      }
    } catch (caught) { failed(caught, token); }
    finally { if (token === generation.current) setBusy(false); }
  }

  const states: ProgressState[] = !base?.assessment.progress ? ["not_started"] : base.assessment.progress.state === "not_started" ? ["in_progress", "ended"] : base.assessment.progress.state === "in_progress" ? ["ended"] : ["in_progress"];
  return <div className="finding-lifecycle">
    <FindingResponsibility caseID={caseID} findingID={findingID} caseStatus={record.case.status} session={session} onChanged={() => void changed()} showHistory={false} progressState={view ? view.assessment.progress?.state ?? null : undefined} />
    <details className="inline-editor" open={open} onToggle={(event) => setOpen(event.currentTarget.open)}><summary>复核与跟进</summary>
      {error ? <ErrorState error={error} /> : null}
      {notice ? <p role="status">{notice}</p> : null}
      {open && !view && !error ? <p>正在读取复核、跟进与责任历史…</p> : null}
      {view ? <div className="stack-form">
        <p><strong>复核：{view.assessment.review ? conclusionLabels[view.assessment.review.conclusion] : "未记录"}</strong> · 跟进：{view.assessment.progress ? progressLabels[view.assessment.progress.state] : "未记录"}</p>
        {view.overdue ? <p role="note">跟进期限已过；记录尚未结束。</p> : !view.assessment.progress ? <p className="quiet-copy">跟进进度未记录，不从责任或期限推定。</p> : null}
        <p className="quiet-copy">核对时间：{formatDateTime(view.evaluated_at)} · 复核版本 {view.assessment.version} · 案件版本 {view.case_version}</p>
        <ResourceUpdates updates={[...(view.review_basis.resource_updates ?? []), ...(view.progress_basis.resource_updates ?? [])]} />
        {view.review_basis.state === "changed" ? <p role="note">相关依据已变化，待重新评估。原结论与跟进记录保留，尚未自动重开。</p> : view.review_basis.state === "unavailable" ? <p role="alert">当前无法完整读取采用依据，不能判定为“没有变化”。</p> : null}
        {view.assessment.review ? <><p>{view.assessment.review.reason}</p>{view.assessment.review.missing_evidence ? <p>缺少：{view.assessment.review.missing_evidence}</p> : null}<small>记录于 {formatDateTime(view.assessment.review.at)}</small><details><summary>记录人标识</summary><code>{view.assessment.review.actor_subject_id}</code></details><ReviewBasisView basis={view.assessment.review.basis} /></> : null}
        {view.review_basis.state === "changed" && view.review_basis.current ? <ReviewBasisView basis={view.review_basis.current} label="当前依据（不改写历史）" /> : null}
        {view.assessment.progress ? <><p>{view.assessment.progress.reason}</p>{view.assessment.progress.closure ? <ClosureView closure={view.assessment.progress.closure} navigate={navigate} /> : null}<ReviewBasisView basis={view.assessment.progress.basis} label="当时跟进动作采用的依据" /></> : null}
        {["changed", "unavailable"].includes(view.progress_basis.state) ? <p role="note">跟进动作的依据当前{view.progress_basis.state === "changed" ? "已有变化" : "无法完整读取"}，保留原动作供核对。</p> : null}
        <div className="button-cluster"><button className="button button-secondary" type="button" disabled={busy} onClick={() => void refresh()}>重新读取当前记录</button>
          {!mode && !pending && !saved && view.allowed_actions.includes("record_review") ? <button className="button button-secondary" type="button" disabled={busy} onClick={() => void edit("record_review")}>记录复核结论</button> : null}
          {!mode && !pending && !saved && view.allowed_actions.includes("record_progress") ? <button className="button button-secondary" type="button" disabled={busy} onClick={() => void edit("record_progress")}>记录跟进进度</button> : null}</div>
        {pending && !denied && canWrite ? <div><p role="alert">尚未确认保存结果。原操作保留在本标签页，刷新后仍可原样重试。</p><p>{pending.review?.reason ?? pending.progress?.reason}</p><SelectionSummary selection={pending.selection} /><button className="button button-primary" type="button" disabled={busy} onClick={() => void submit()}>原样重试复核操作</button></div> : null}
        {saved ? <p role="alert">动作已确认保存为复核版本 {saved.current.version}，但当前记录尚未读取成功。请重新读取，无需再次提交。</p> : null}
        {conflict ? <div><p role="alert">记录、案件或依据已变化，本次修改未保存。请重新读取并核对后再决定。</p><button className="button button-secondary" type="button" disabled={busy} onClick={() => void edit(mode ?? "record_review")}>重新读取并核对</button></div> : null}
        {mode && base && !pending && !saved && canWrite ? <form className="stack-form" onSubmit={(event) => void prepare(event)} onChangeCapture={invalidate}>
          <ReferencePicker available={findingReferences(pickerRecord, findingID)} selected={selection} onChange={setSelection} disabled={draftLocked} />
          {mode === "record_review" ? <ReviewFields value={review} onChange={setReview} disabled={draftLocked} /> : <fieldset className="stack-form" disabled={draftLocked}><legend>跟进记录</legend>
            <label>进度<select value={progress} onChange={(event) => setProgress(event.target.value as ProgressState)}>{states.map((state) => <option key={state} value={state}>{progressLabels[state]}</option>)}</select></label>
            <label>进度变更理由<textarea rows={2} maxLength={4000} value={reason} onChange={(event) => setReason(event.target.value)} /></label>
            {progress === "ended" ? <><label>结束类型<select value={closureKind} onChange={(event) => setClosureKind(event.target.value as Closure["kind"])}>{Object.entries(closureLabels).map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select></label>
              <label>结束依据说明<textarea rows={3} maxLength={4000} value={explanation} onChange={(event) => setExplanation(event.target.value)} /></label>
              {closureKind === "end_followup" ? <label>剩余未知<textarea rows={2} maxLength={4000} value={remainingUnknowns} onChange={(event) => setRemainingUnknowns(event.target.value)} /></label> : null}
              {closureKind === "verified_remediation" ? <><label>人工核验覆盖范围<textarea maxLength={2000} value={scope} onChange={(event) => setScope(event.target.value)} /></label><label>人工核验时间（本机时区）<input type="datetime-local" value={verifiedAt} onChange={(event) => setVerifiedAt(event.target.value)} /></label><p>从本次采用的 Evidence、资源或内容中勾选修复后的核验引用；这不表示 XI 执行了修复。</p><ReferencePicker available={{ evidence: selection.evidence, gaps: [], resources:selection.resources,contents:selection.contents }} selected={{ evidence: verification, gaps: [], resources:resourceVerification,contents:contentVerification }} onChange={(v) => {setVerification(v.evidence);setResourceVerification(v.resources??[]);setContentVerification(v.contents??[]);}} disabled={draftLocked} /></> : null}
              {closureKind === "duplicate" ? <><label>并入 Case ID<input value={duplicateCase} onChange={(event) => setDuplicateCase(event.target.value)} /></label><label>并入 Finding ID<input value={duplicateFinding} onChange={(event) => setDuplicateFinding(event.target.value)} /></label><p>预览将读取该对象，请核对其陈述并说明为何是同一判断。</p></> : null}
              {closureKind === "ruled_out" ? <p>须已有当前依据下“证据不支持”的复核及反证；依据变化时请先重新复核。</p> : null}
            </> : null}
          </fieldset>}
          {!conflict ? <button className="button button-secondary" disabled={draftLocked || (mode === "record_review" ? !review.reason.trim() : !reason.trim())}>预览本次依据</button> : null}
          {preview ? <div>{preview.duplicateTitle ? <p>并入对象：<strong>{preview.duplicateTitle}</strong><br />{preview.duplicateStatement}</p> : null}<ReviewBasisView basis={preview.basis} label="服务端读取的本次依据" /><button className="button button-primary" type="button" disabled={busy} onClick={() => void submit()}>确认保存{mode === "record_review" ? "复核" : "进度"}</button></div> : null}
          <button className="button button-secondary" type="button" disabled={busy} onClick={() => { setMode(undefined); setPreview(undefined); setConflict(false); }}>取消编辑</button>
        </form> : null}
        {responsibility ? <section className="finding-timeline"><h4>复核、进度与责任历史</h4><p className="quiet-copy">复核截至版本 {view.history.through_version}，责任截至版本 {responsibility.history.through_version}。按 案件版本排序，各自固定分页范围。</p>
          {combinedFindingHistory(view.history, responsibility.history).map((item) => <article className="responsibility-history" key={`${item.kind}:${item.action.actor_subject_id}:${item.action.operation_id}`}><p>案件版本 {item.version} · {item.kind === "responsibility" ? "责任与期限" : item.action.operation === "record_review" ? "复核结论" : "跟进进度"} · {formatDateTime(item.action.at)}</p><details><summary>记录人标识</summary><code>{item.action.actor_subject_id}</code></details>
            {item.kind === "responsibility" ? <><p>{item.action.reason}</p><p>{item.action.previous.recorded ? item.action.previous.assignee_name || "未分配" : "未记录"} → {item.action.current.assignee_name || "未分配"}</p><p>{item.action.previous.due_at ?? "未设期限"} → {item.action.current.due_at ?? "未设期限"} · {item.action.current.due_timezone}</p></> : <><p>{item.action.command.review ? `${conclusionLabels[item.action.command.review.conclusion]} · ${item.action.command.review.reason}` : `${progressLabels[item.action.command.progress!.state]} · ${item.action.command.progress!.reason}`}</p>{item.action.command.progress?.closure ? <ClosureView closure={item.action.command.progress.closure} navigate={navigate} /> : null}<ReviewBasisView basis={item.action.basis} /></>}</article>)}
          {view.history.next_before_version ? <button className="button button-secondary" type="button" disabled={busy} onClick={() => void more("assessment")}>读取更早复核与进度</button> : <p className="quiet-copy">此范围内复核与进度历史已完整显示。</p>}
          {responsibility.history.next_before_version ? <button className="button button-secondary" type="button" disabled={busy} onClick={() => void more("responsibility")}>读取更早责任历史</button> : <p className="quiet-copy">此范围内责任历史已完整显示。</p>}
        </section> : null}
        {view.continuations.map((r) => <div key={r.operation_id}><p>{r.command.from ? "封存续办" : "首次组合复核"} · {r.command.reason}</p><details><summary>案件与发现引用</summary><button type="button" className="back-link" onClick={() => navigate(`/cases/${r.case_id}#${r.finding_id}`)}>{r.case_id} / {r.finding_id}</button>{r.command.from ? <> · 来源 <button className="back-link" type="button" onClick={() => navigate(`/cases/${r.command.from!.case_id}#${r.command.from!.finding_id}`)}>{r.command.from.case_id} · 版本 {r.command.from.revision}</button><br /><code>{r.command.from.digest}</code></> : null}</details></div>)}
        {view.allowed_actions.includes("continue_finding") && record.case.sealed_revision && record.case.sealed_digest ? <ReviewStart session={session} available={{...findingReferences(record,findingID),contents:findingReferences(record,findingID).contents?.map(c=>({...c,scope:"finding" as const})),resources:record.resources?.filter(r=>!r.finding_id||r.finding_id===findingID).map(r=>({...r.reference,scope:"finding" as const})),evidence:findingReferences(record,findingID).evidence.map(e=>({...e,scope:"finding" as const})),gaps:findingReferences(record,findingID).gaps.map(g=>({...g,scope:"finding" as const}))}} from={{ case_id: caseID, finding_id: findingID, revision: record.case.sealed_revision, digest: record.case.sealed_digest }} navigate={navigate} /> : null}
      </div> : null}
    </details>
  </div>;
}

function ClosureView({ closure, navigate }: { closure: Closure; navigate: (path: string) => void }) {
  return <div className="review-closure"><strong>{closureLabels[closure.kind]}</strong><p>{closure.explanation}</p>{closure.scope ? <p>人工核验范围：{closure.scope} · {closure.verified_at ? formatDateTime(closure.verified_at) : ""}。此记录不表示 XI 执行了修复。</p> : null}{closure.remaining_unknowns ? <p>剩余未知：{closure.remaining_unknowns}</p> : null}{closure.duplicate ? <button className="back-link" type="button" onClick={() => navigate(`/cases/${closure.duplicate!.case_id}#${closure.duplicate!.finding_id}`)}>查看并入对象 {closure.duplicate.case_id} / {closure.duplicate.finding_id}</button> : null}{(closure.verification_evidence.length || closure.verification_resources?.length || closure.verification_contents?.length) ? <SelectionSummary selection={{ evidence: closure.verification_evidence, gaps: [],resources:closure.verification_resources,contents:closure.verification_contents }} /> : null}</div>;
}
