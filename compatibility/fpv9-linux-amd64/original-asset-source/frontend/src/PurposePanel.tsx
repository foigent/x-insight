import { useEffect, useRef, useState } from "react";
import { ApiRequestError, type AccessSessionResponse, type PurposeAction, type PurposeBasis, type PurposeCategory, type PurposeCommand, type PurposeView, workbenchAPI } from "./api";
import "./purpose.css";

export const purposeLabels: Record<string, string> = {
  information_query: "信息查询与解答", content_transformation: "内容撰写与转换", software_check: "软件实现与检查",
  unknown: "用途未知", unreviewed: "待复核", conflict: "用途冲突", adopted: "已采用",
};
const reviewLabels: Record<string, string> = { pending: "待复核", accepted: "已接受", modified: "已修订", rejected: "已拒绝", withdrawn: "已撤回" };
const actionLabels: Record<PurposeAction, string> = { declare: "人工声明", accept: "接受并采用", modify: "修改并采用", reject: "拒绝声明", withdraw_declaration: "撤回声明", withdraw_adoption: "撤回采用" };

export function PurposePanel({ executionID, readingID, basis, session, onInvalidated }: {
  executionID: string; readingID?: string; basis: PurposeBasis; session: AccessSessionResponse;
  onInvalidated?: (failure: unknown) => void;
}) {
  const [open, setOpen] = useState(false);
  const [current, setCurrent] = useState(!readingID);
  const [view, setView] = useState<PurposeView | null>(null);
  const [reason, setReason] = useState("");
  const [category, setCategory] = useState<PurposeCategory>("software_check");
  const [rejectIDs, setRejectIDs] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const sequence = useRef(0);
  const abort = useRef<AbortController | null>(null);
  const retry = useRef<{ body: string; key: string } | null>(null);
  const invalidated = useRef(onInvalidated); invalidated.current = onInvalidated;
  const identity = `${session.session.subject_id}|${session.session.role}|${session.session.permissions.join(",")}|${session.scope_revision ?? ""}`;

  function failureText(failure: unknown) {
    if (failure instanceof ApiRequestError) {
      if ([401, 403, 404, 410].includes(failure.status) || ["workbench_access_changed", "scope_revision_changed"].includes(failure.problem?.code ?? "")) {
        setView(null); setReason(""); setRejectIDs([]); retry.current = null;
        if (failure.problem?.code === "workbench_access_changed" || failure.problem?.code === "scope_revision_changed" || failure.status === 401) invalidated.current?.(failure);
        return "当前用途依据不可访问，或授权已变化。请按当前权限重新读取；已有金额读取能力不因此扩大。";
      }
      if (failure.status === 409) return "用途判断已变化或本次操作发生冲突。未提交理由已保留，请读取当前判断后重新核对。";
      return failure.problem?.title || "用途请求失败，可以重试。";
    }
    return "用途请求暂未完成。未提交理由已保留，请核对结果后重试。";
  }

  async function load(readCurrent = current, before?: number) {
    const version = ++sequence.current;
    abort.current?.abort(); const controller = new AbortController(); abort.current = controller;
    setBusy(true); setError("");
    try {
      const next = await workbenchAPI.purpose(executionID, readCurrent ? undefined : readingID, before, controller.signal);
      if (version !== sequence.current || controller.signal.aborted) return;
      if (next.execution_id !== executionID || (!readCurrent && next.reading_id !== readingID)) throw new Error("Purpose reading does not match selection");
      setView(previous => before && previous ? { ...next, history: [...previous.history, ...next.history] } : next);
      setCurrent(readCurrent); setRejectIDs([]);
    } catch (failure) { if (version === sequence.current && !controller.signal.aborted) setError(failureText(failure)); }
    finally { if (version === sequence.current) setBusy(false); }
  }

  useEffect(() => {
    const clear = () => { ++sequence.current; abort.current?.abort(); setView(null); setReason(""); setRejectIDs([]); setBusy(false); retry.current = null; };
    clear(); setCurrent(!readingID); setOpen(false); setError(""); setNotice("");
    window.addEventListener("xinsight:scope-changed", clear); window.addEventListener("xinsight:unauthorized", clear);
    return () => { ++sequence.current; abort.current?.abort(); window.removeEventListener("xinsight:scope-changed", clear); window.removeEventListener("xinsight:unauthorized", clear); };
  }, [identity, executionID, readingID]);

  async function act(action: PurposeAction, claimID?: string) {
    if (!current || !view?.can_review || !session.session.permissions.includes("workbench.purpose.review") || !reason.trim()) return;
    const draft: Omit<PurposeCommand, "idempotency_key"> = { action, expected_revision: view.state.revision, reason: reason.trim() };
    if (claimID) draft.claim_id = claimID;
    if (action === "declare" || action === "modify") Object.assign(draft, { category, vocabulary: "xinsight.request-purpose.dev.v1", basis_refs: [basis] });
    if (action === "accept" || action === "modify") draft.reject_claim_ids = rejectIDs.filter(id => id !== claimID);
    const body = JSON.stringify(draft);
    if (retry.current?.body !== body) retry.current = { body, key: crypto.randomUUID() };
    const command = { ...draft, idempotency_key: retry.current.key };
    const version = ++sequence.current; abort.current?.abort(); const controller = new AbortController(); abort.current = controller;
    setBusy(true); setError(""); setNotice("");
    try {
      const event = await workbenchAPI.purposeCommand(executionID, command, session.csrf_token, controller.signal);
      if (version !== sequence.current || controller.signal.aborted) return;
      if (event.execution_id !== executionID) throw new Error("Purpose response does not match selection");
      setView(previous => previous ? { ...previous, state: event.state, history: [event, ...previous.history.filter(row => row.revision !== event.revision)] } : null);
      setReason(""); setRejectIDs([]); retry.current = null;
      setNotice("操作已记录。当前阅读的统计保持原值；显式新建阅读后才采用新的用途结果。");
    } catch (failure) { if (version === sequence.current && !controller.signal.aborted) setError(failureText(failure)); }
    finally { if (version === sequence.current) setBusy(false); }
  }

  const canReview = current && view?.can_review && session.session.permissions.includes("workbench.purpose.review");
  return <details className="purpose-panel" open={open} onToggle={event => {
    const expanded = event.currentTarget.open; setOpen(expanded); if (expanded && !view && !busy) void load();
  }}>
    <summary>请求用途 · 声明与修订</summary>
    {open ? <div aria-busy={busy}>
      <p>人工声明说明 XI 判断者的意见，不证明请求的实际操作者，也不代表工作已经完成。</p>
      <div className="purpose-toolbar">
        {readingID ? <button disabled={busy} aria-pressed={!current} onClick={() => void load(false)}>本阅读的用途</button> : null}
        <button disabled={busy} aria-pressed={current} onClick={() => void load(true)}>读取当前判断</button>
      </div>
      {view ? <>
        <p className="purpose-result"><strong>{purposeLabels[view.state.category || view.state.status]}</strong><span>{current ? "当前判断" : "本阅读的解释"} · 修订 {view.state.revision}</span></p>
        {view.state.status === "conflict" ? <p>存在未裁决的不同用途声明；本次不计入已知用途。仍有效的采用记录保留在下方。</p> : null}
        {view.state.claims.length === 0 ? <p>尚无用途声明，不能从应用名称或技术方式补出单次用途。</p> : null}
        {canReview ? <fieldset disabled={busy} className="purpose-review-form"><legend>人工声明与复核</legend>
          <label>工作用途<select aria-label="工作用途" value={category} onChange={event => setCategory(event.target.value as PurposeCategory)}>{["information_query", "content_transformation", "software_check"].map(value => <option key={value} value={value}>{purposeLabels[value]}</option>)}</select></label>
          <small>当前为有界试用词表。客服等业务域不等于本次工作用途；不能归类时保留未知。</small>
          <label>声明或复核理由<textarea aria-label="声明或复核理由" maxLength={4000} value={reason} onChange={event => setReason(event.target.value)} /></label>
          <p>新声明/修改引用当前请求的已授权证据。接受或修改时，仅拒绝下方明确勾选的其他待审声明。</p>
          <button disabled={!reason.trim()} onClick={() => void act("declare")}>提交人工声明（待复核）</button>
        </fieldset> : <p>{current ? "当前权限仅可阅读用途依据。" : "历史阅读保持原解释。需要操作时先读取当前判断。"}</p>}
        <div className="purpose-claims">{view.state.claims.map(claim => <section key={claim.claim_id} aria-label={`${purposeLabels[claim.category]} ${reviewLabels[claim.review_state]}`}>
          <header><strong>{purposeLabels[claim.category]}</strong><span>{reviewLabels[claim.review_state]}{view.state.adopted_claim_id === claim.claim_id ? " · 当前采用引用" : ""}</span></header>
          <p>{claim.reason}</p><small>XI 人工声明 · {claim.declared_by} · {new Date(claim.declared_at).toLocaleString()}</small>
          {canReview && claim.review_state === "pending" ? <label><input type="checkbox" checked={rejectIDs.includes(claim.claim_id)} onChange={event => setRejectIDs(previous => event.target.checked ? [...previous, claim.claim_id] : previous.filter(id => id !== claim.claim_id))} />本次接受/修改时明确拒绝此项</label> : null}
          <details><summary>声明与证据引用</summary><code>{claim.claim_id}</code>{claim.basis_refs.map(ref => <p key={ref.evidence_event_id}><code>{ref.evidence_event_id}</code><code>{ref.event_hash}</code></p>)}{claim.revises ? <p>修订自 <code>{claim.revises}</code></p> : null}</details>
          {canReview && ["pending", "accepted"].includes(claim.review_state) ? <div className="purpose-actions">
            <button disabled={busy || !reason.trim()} onClick={() => void act("accept", claim.claim_id)}>接受并采用</button>
            <button disabled={busy || !reason.trim()} onClick={() => void act("modify", claim.claim_id)}>修改并采用所选用途</button>
            {claim.review_state === "pending" ? <button disabled={busy || !reason.trim()} onClick={() => void act("reject", claim.claim_id)}>拒绝声明</button> : null}
            <button disabled={busy || !reason.trim()} onClick={() => void act("withdraw_declaration", claim.claim_id)}>撤回声明</button>
            {view.state.adopted_claim_id === claim.claim_id ? <button disabled={busy || !reason.trim()} onClick={() => void act("withdraw_adoption", claim.claim_id)}>撤回采用</button> : null}
          </div> : null}
        </section>)}</div>
        <details><summary>操作历史（{view.history.length} 条已载入）</summary><ol className="purpose-history">{view.history.map(event => <li key={event.revision}><strong>修订 {event.revision} · {actionLabels[event.command.action]}</strong><p>{event.command.reason}</p><small>{event.actor} · {new Date(event.recorded_at).toLocaleString()}</small></li>)}</ol>{view.next_before_revision ? <button disabled={busy} onClick={() => void load(current, view.next_before_revision)}>更早的用途历史</button> : null}</details>
      </> : null}
      {notice ? <p role="status">{notice}</p> : null}
      {error ? <div role="alert"><p>{error}</p><button disabled={busy} onClick={() => void load()}>重试用途读取</button></div> : null}
      {busy ? <p role="status">正在处理用途请求…</p> : null}
    </div> : null}
  </details>;
}
