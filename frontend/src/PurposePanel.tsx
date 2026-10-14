import { localizedLabels, getLocale, t, tx, useLocale, useMessageState } from "./i18n";
import { useEffect, useRef, useState } from "react";
import { ApiRequestError, type AccessSessionResponse, type PurposeAction, type PurposeBasis, type PurposeCategory, type PurposeCommand, type PurposeView, workbenchAPI } from "./api";
import "./purpose.css";

export const purposeLabels: Record<string, string> = localizedLabels({}, {information_query: "m1643", content_transformation: "m1644", software_check: "m1645", unknown: "m1646", unreviewed: "m0829", conflict: "m1647", adopted: "m1648"});
const reviewLabels: Record<string, string> = localizedLabels({}, {pending: "m0829", accepted: "m1649", modified: "m1650", rejected: "m1651", withdrawn: "m1652"});
const actionLabels: Record<PurposeAction, string> = localizedLabels({}, {declare: "m1653", accept: "m1654", modify: "m1655", reject: "m1656", withdraw_declaration: "m1657", withdraw_adoption: "m1658"});

export function PurposePanel({ executionID, readingID, basis, session, onInvalidated }: {
  executionID: string; readingID?: string; basis: PurposeBasis; session: AccessSessionResponse;
  onInvalidated?: (failure: unknown) => void;
}) {
  useLocale();
  const [open, setOpen] = useState(false);
  const [current, setCurrent] = useState(!readingID);
  const [view, setView] = useState<PurposeView | null>(null);
  const [reason, setReason] = useState("");
  const [category, setCategory] = useState<PurposeCategory>("software_check");
  const [rejectIDs, setRejectIDs] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useMessageState("");
  const [notice, setNotice] = useMessageState("");
  const sequence = useRef(0);
  const abort = useRef<AbortController | null>(null);
  const retry = useRef<{ body: string; key: string } | null>(null);
  const invalidated = useRef(onInvalidated); invalidated.current = onInvalidated;
  const identity = `${session.session.subject_id}|${session.session.role}|${session.session.permissions.join(",")}|${session.scope_revision ?? ""}`;

  function failureText(failure: unknown) {
    if (failure instanceof ApiRequestError) {
      if ([401, 403, 404, 410].includes(failure.status) || ["workbench_access_changed", "scope_revision_changed"].includes(failure.problem?.code ?? "")) {
        return t("m1659");
      }
      if (failure.status === 409) return t("m1660");
      return t("m1661");
    }
    return t("m1662");
  }

  function handleFailure(failure: unknown) {
    if (failure instanceof ApiRequestError && ([401,403,404,410].includes(failure.status) || ["workbench_access_changed", "scope_revision_changed"].includes(failure.problem?.code ?? ""))) {
      setView(null); setReason(""); setRejectIDs([]); retry.current = null;
      if (failure.problem?.code === "workbench_access_changed" || failure.problem?.code === "scope_revision_changed" || failure.status === 401) invalidated.current?.(failure);
    }
    setError(() => failureText(failure));
  }

  async function load(readCurrent = current, before?: number) {
    const version = ++sequence.current;
    abort.current?.abort(); const controller = new AbortController(); abort.current = controller;
    setBusy(true); setError(() => "");
    try {
      const next = await workbenchAPI.purpose(executionID, readCurrent ? undefined : readingID, before, controller.signal);
      if (version !== sequence.current || controller.signal.aborted) return;
      if (next.execution_id !== executionID || (!readCurrent && next.reading_id !== readingID)) throw new Error("Purpose reading does not match selection");
      setView(previous => before && previous ? { ...next, history: [...previous.history, ...next.history] } : next);
      setCurrent(readCurrent); setRejectIDs([]);
    } catch (failure) { if (version === sequence.current && !controller.signal.aborted) handleFailure(failure); }
    finally { if (version === sequence.current) setBusy(false); }
  }

  useEffect(() => {
    const clear = () => { ++sequence.current; abort.current?.abort(); setView(null); setReason(""); setRejectIDs([]); setBusy(false); retry.current = null; };
    clear(); setCurrent(!readingID); setOpen(false); setError(() => ""); setNotice(() => "");
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
    setBusy(true); setError(() => ""); setNotice(() => "");
    try {
      const event = await workbenchAPI.purposeCommand(executionID, command, session.csrf_token, controller.signal);
      if (version !== sequence.current || controller.signal.aborted) return;
      if (event.execution_id !== executionID) throw new Error("Purpose response does not match selection");
      setView(previous => previous ? { ...previous, state: event.state, history: [event, ...previous.history.filter(row => row.revision !== event.revision)] } : null);
      setReason(""); setRejectIDs([]); retry.current = null;
      setNotice(() => t("m1663"));
    } catch (failure) { if (version === sequence.current && !controller.signal.aborted) handleFailure(failure); }
    finally { if (version === sequence.current) setBusy(false); }
  }

  const canReview = current && view?.can_review && session.session.permissions.includes("workbench.purpose.review");
  return <details className="purpose-panel" open={open} onToggle={event => {
    const expanded = event.currentTarget.open; setOpen(expanded); if (expanded && !view && !busy) void load();
  }}>
    <summary>{tx("m1664")}</summary>
    {open ? <div aria-busy={busy}>
      <p>{tx("m1665")}</p>
      <div className="purpose-toolbar">
        {readingID ? <button disabled={busy} aria-pressed={!current} onClick={() => void load(false)}>{tx("m1666")}</button> : null}
        <button disabled={busy} aria-pressed={current} onClick={() => void load(true)}>{tx("m1667")}</button>
      </div>
      {view ? <>
        <p className="purpose-result"><strong>{purposeLabels[view.state.category || view.state.status]}</strong><span>{tx("m1670", { value0: current ? t("m1668") : t("m1669"), value1: view.state.revision })}</span></p>
        {view.state.status === "conflict" ? <p>{tx("m1671")}</p> : null}
        {view.state.claims.length === 0 ? <p>{tx("m1672")}</p> : null}
        {canReview ? <fieldset disabled={busy} className="purpose-review-form"><legend>{tx("m1673")}</legend>
          <label>{t("m1674")}<select aria-label={t("m1674")} value={category} onChange={event => setCategory(event.target.value as PurposeCategory)}>{["information_query", "content_transformation", "software_check"].map(value => <option key={value} value={value}>{purposeLabels[value]}</option>)}</select></label>
          <small>{tx("m1675")}</small>
          <label>{t("m1676")}<textarea aria-label={t("m1676")} maxLength={4000} value={reason} onChange={event => setReason(event.target.value)} /></label>
          <p>{tx("m1677")}</p>
          <button disabled={!reason.trim()} onClick={() => void act("declare")}>{tx("m1678")}</button>
        </fieldset> : <p>{current ? t("m1679") : t("m1680")}</p>}
        <div className="purpose-claims">{view.state.claims.map(claim => <section key={claim.claim_id} aria-label={`${purposeLabels[claim.category]} ${reviewLabels[claim.review_state]}`}>
          <header><strong>{purposeLabels[claim.category]}</strong><span>{reviewLabels[claim.review_state]}{view.state.adopted_claim_id === claim.claim_id ? t("m1681") : ""}</span></header>
          <p>{claim.reason}</p><small>{tx("m1682", { value0: claim.declared_by, value1: new Date(claim.declared_at).toLocaleString(getLocale()) })}</small>
          {canReview && claim.review_state === "pending" ? <label><input type="checkbox" checked={rejectIDs.includes(claim.claim_id)} onChange={event => setRejectIDs(previous => event.target.checked ? [...previous, claim.claim_id] : previous.filter(id => id !== claim.claim_id))} />{t("m1683")}</label> : null}
          <details><summary>{tx("m1684")}</summary><code>{claim.claim_id}</code>{claim.basis_refs.map(ref => <p key={ref.evidence_event_id}><code>{ref.evidence_event_id}</code><code>{ref.event_hash}</code></p>)}{claim.revises ? <p>{t("m1685")}<code>{claim.revises}</code></p> : null}</details>
          {canReview && ["pending", "accepted"].includes(claim.review_state) ? <div className="purpose-actions">
            <button disabled={busy || !reason.trim()} onClick={() => void act("accept", claim.claim_id)}>{tx("m1654")}</button>
            <button disabled={busy || !reason.trim()} onClick={() => void act("modify", claim.claim_id)}>{tx("m1686")}</button>
            {claim.review_state === "pending" ? <button disabled={busy || !reason.trim()} onClick={() => void act("reject", claim.claim_id)}>{tx("m1656")}</button> : null}
            <button disabled={busy || !reason.trim()} onClick={() => void act("withdraw_declaration", claim.claim_id)}>{tx("m1657")}</button>
            {view.state.adopted_claim_id === claim.claim_id ? <button disabled={busy || !reason.trim()} onClick={() => void act("withdraw_adoption", claim.claim_id)}>{tx("m1658")}</button> : null}
          </div> : null}
        </section>)}</div>
        <details><summary>{tx("m1687", { value0: view.history.length })}</summary><ol className="purpose-history">{view.history.map(event => <li key={event.revision}><strong>{tx("m1688", { value0: event.revision, value1: actionLabels[event.command.action] })}</strong><p>{event.command.reason}</p><small>{event.actor} · {new Date(event.recorded_at).toLocaleString(getLocale())}</small></li>)}</ol>{view.next_before_revision ? <button disabled={busy} onClick={() => void load(current, view.next_before_revision)}>{tx("m1689")}</button> : null}</details>
      </> : null}
      {notice ? <p role="status">{notice}</p> : null}
      {error ? <div role="alert"><p>{error}</p><button disabled={busy} onClick={() => void load()}>{tx("m1690")}</button></div> : null}
      {busy ? <p role="status">{tx("m1691")}</p> : null}
    </div> : null}
  </details>;
}
