import { t, tx, useLocale, useMessageState } from "./i18n";
import { type FormEvent, useEffect, useRef, useState } from "react";
import { ApiRequestError, isAbortError, workspaceAPI, type AccessSessionResponse, type FindingFollowup, type FindingFollowupCommand, type FindingReview, type LocalAssignee } from "./api";
import { ErrorState } from "./Feedback";
import { formatDateTime } from "./format";
import { deadlineCandidates, localDeadline, preciseDeadlineValue } from "./responsibilityTime";

interface Props { caseID: string; findingID: string; caseStatus: string; session: AccessSessionResponse; onChanged: () => void; showHistory?: boolean; progressState?: string | null }

function responsibilityText(value: FindingFollowup) {
  if (!value.recorded) return t("m0773");
  return value.assignee_subject_id ? `${value.assignee_name} · ${value.assignee_subject_id}` : t("m0759");
}

function deadlineText(value: FindingFollowup) {
  if (!value.due_at || !value.due_timezone) return t("m0760");
  return `${localDeadline(value.due_at, value.due_timezone).replace("T", " ")} · ${value.due_timezone}`;
}

export function FindingResponsibility({ caseID, findingID, caseStatus, session, onChanged, showHistory = true, progressState }: Props) {
  useLocale();
  const [open, setOpen] = useState(false);
  const [review, setReview] = useState<FindingReview>();
  const [candidates, setCandidates] = useState<LocalAssignee[]>([]);
  const [editing, setEditing] = useState(false);
  const [base, setBase] = useState<FindingReview>();
  const [owner, setOwner] = useState("");
  const [local, setLocal] = useState("");
  const [zone, setZone] = useState("Asia/Shanghai");
  const [offsetChoice, setOffsetChoice] = useState("");
  const [deadlineEdited, setDeadlineEdited] = useState(false);
  const [reason, setReason] = useState("");
  const [pending, setPending] = useState<FindingFollowupCommand>();
  const [saveFailure, setSaveFailure] = useState<"conflict" | "sealed" | "rejected" | "unknown">();
  const [savedVersion, setSavedVersion] = useState<number>();
  const [error, setError] = useState<unknown>();
  const [notice, setNotice] = useMessageState("");
  const [busy, setBusy] = useState(false);
  const lifetime = useRef<AbortController>(new AbortController());
  const generation = useRef(0);
  const loadSequence = useRef(0);

  function failed(caught: unknown, token: number) {
    if (token !== generation.current || isAbortError(caught)) return;
    if (caught instanceof ApiRequestError && [401, 403, 404].includes(caught.status)) {
      setReview(undefined); setCandidates([]); setEditing(false); setBase(undefined); setPending(undefined); setSavedVersion(undefined); setSaveFailure(undefined); setNotice(() => "");
    }
    setError(caught);
  }

  async function load(): Promise<FindingReview | undefined> {
    const token = generation.current;
    const sequence = ++loadSequence.current;
    const value = await workspaceAPI.getFindingReview(caseID, findingID, lifetime.current.signal);
    if (token !== generation.current || sequence !== loadSequence.current) return;
    setReview(value);
    setError(undefined);
    return value;
  }

  useEffect(() => {
    const token = ++generation.current;
    const controller = new AbortController(); lifetime.current = controller;
    setReview(undefined); setBase(undefined); setEditing(false); setPending(undefined); setSavedVersion(undefined); setSaveFailure(undefined); setError(undefined); setCandidates([]); setBusy(false); setNotice(() => "");
    if (open) void load().catch((caught) => failed(caught, token));
    return () => { generation.current++; controller.abort(); };
    // The resource identity and Case seal state define this panel's lifetime.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, caseID, findingID, caseStatus]);

  const needsReread = saveFailure === "conflict" || saveFailure === "sealed";

  async function edit() {
    const token = generation.current;
    setBusy(true); setNotice(() => "");
    try {
      const value = await load();
      if (!value || token !== generation.current) return;
      if (!value.allowed_actions.includes("update_followup")) {
        setEditing(false); setBase(undefined); setPending(undefined); setSavedVersion(undefined); setSaveFailure(undefined); onChanged();
        return;
      }
      const directory = await workspaceAPI.assigneeCandidates(caseID, findingID, lifetime.current.signal);
      if (token !== generation.current) return;
      setCandidates(directory.items); setBase(value); setOwner(value.followup.assignee_subject_id ?? "");
      const selectedZone = value.followup.due_timezone ?? Intl.DateTimeFormat().resolvedOptions().timeZone;
      setZone(selectedZone);
      const stamp = value.followup.due_at ? localDeadline(value.followup.due_at, selectedZone) : "";
      setLocal(stamp);
      setOffsetChoice(stamp ? deadlineCandidates(stamp, selectedZone).find((item) => Date.parse(item.value) === Date.parse(value.followup.due_at!))?.value ?? "" : "");
      setReason(""); setPending(undefined); setSavedVersion(undefined); setSaveFailure(undefined); setDeadlineEdited(false); setEditing(true);
      if (needsReread) setNotice(() => t("m0774"));
    } catch (caught) { failed(caught, token); }
    finally { if (token === generation.current) setBusy(false); }
  }

  let timeOptions: ReturnType<typeof deadlineCandidates> = [];
  let timeError = "";
  if (local) {
    try { timeOptions = deadlineCandidates(local, zone); if (!timeOptions.length) timeError = t("m0775"); }
    catch { timeError = t("m0776"); }
  }
  const selectedInstant = !deadlineEdited && base?.followup.due_at && base.followup.due_timezone
    ? preciseDeadlineValue(base.followup.due_at, base.followup.due_timezone)
    : timeOptions.length === 1 ? timeOptions[0].value : timeOptions.find((item) => item.value === offsetChoice)?.value;

  async function save(event: FormEvent) {
    event.preventDefault();
    if (!base || busy || needsReread || savedVersion !== undefined || !reason.trim() || new TextEncoder().encode(reason.trim()).length > 4000 || (local && !selectedInstant)) return;
    const token = generation.current;
    const payload = { assignee_subject_id: owner || null, due_at: local ? selectedInstant! : null, due_timezone: local ? zone : null, reason: reason.trim() };
    const command: FindingFollowupCommand = pending && JSON.stringify(pending.payload) === JSON.stringify(payload) ? pending : {
      operation: "update_followup", operation_id: crypto.randomUUID(), expected_version: base.followup.version, expected_case_version: base.case_version, payload,
    };
    if (pending?.operation_id !== command.operation_id) setSavedVersion(undefined);
    setPending(command); setBusy(true); setError(undefined); setSaveFailure(undefined); setNotice(() => "");
    let confirmed = false;
    try {
      const receipt = await workspaceAPI.updateFindingFollowup(caseID, findingID, command, session.csrf_token, lifetime.current.signal);
      if (token !== generation.current) return;
      confirmed = true;
      setSavedVersion(receipt.current.version); setPending(undefined);
      await finishSave(receipt.current.version);
    } catch (caught) {
      if (token !== generation.current || isAbortError(caught)) return;
      if (!confirmed && !(caught instanceof ApiRequestError && [401, 403, 404].includes(caught.status))) {
        const failure = caught instanceof ApiRequestError && caught.status === 409 ? caught.problem?.code === "case_sealed" ? "sealed" : "conflict"
          : caught instanceof ApiRequestError && caught.status >= 400 && caught.status < 500 ? "rejected" : "unknown";
        setSaveFailure(failure);
        if (failure !== "unknown") setPending(undefined);
      }
      failed(caught, token);
    }
    finally { if (token === generation.current) setBusy(false); }
  }

  async function finishSave(version: number) {
    const current = await load();
    if (!current) return;
    setNotice(() => t("m0777", { value0: version, value1: current.followup.version }));
    setEditing(false); setPending(undefined); setReason(""); setSavedVersion(undefined); setSaveFailure(undefined); onChanged();
  }

  async function refreshSaved() {
    if (savedVersion === undefined || busy) return;
    const token = generation.current; setBusy(true);
    try { await finishSave(savedVersion); }
    catch (caught) { failed(caught, token); }
    finally { if (token === generation.current) setBusy(false); }
  }

  const draftLocked = busy || saveFailure === "unknown" || needsReread || savedVersion !== undefined;

  async function moreHistory() {
    if (!review?.history.next_before_version || busy) return;
    const token = generation.current; const sequence = loadSequence.current;
    const snapshot = review.history; setBusy(true);
    try {
      const page = await workspaceAPI.getFindingHistory(caseID, findingID, snapshot.through_version, snapshot.next_before_version!, lifetime.current.signal);
      if (token !== generation.current || sequence !== loadSequence.current) return;
      setReview((current) => current ? { ...current, history: { ...page, items: [...snapshot.items, ...page.items] } } : current);
    } catch (caught) { failed(caught, token); }
    finally { if (token === generation.current) setBusy(false); }
  }

  return <details className="responsibility-panel" onToggle={(event) => setOpen(event.currentTarget.open)}>
    <summary>{tx("m0755")}</summary>
    {error ? <ErrorState error={error} /> : null}
    {saveFailure === "conflict" ? <p role="alert">{tx("m0778")}</p> : null}
    {saveFailure === "sealed" ? <p role="alert">{tx("m0779")}</p> : null}
    {saveFailure === "unknown" ? <p role="alert">{tx("m0780")}</p> : null}
    {saveFailure === "rejected" ? <p role="alert">{tx("m0781")}</p> : null}
    {error && savedVersion !== undefined ? <p role="alert">{tx("m0782", { value0: savedVersion })}</p> : null}
    {notice ? <p role="status">{notice}</p> : null}
    {open && !review && !error ? <p>{tx("m0783")}</p> : null}
    {review ? <div className="stack-form">
      <p><strong>{responsibilityText(review.followup)}</strong></p>
      {["disabled", "not_found", "unavailable"].includes(review.assignee_state) && caseStatus === "open" ? <p role="note">{tx("m0784")}</p> : null}
      <p>{deadlineText(review.followup)}</p>
      {review.followup.due_at && caseStatus === "open" && progressState !== "ended" && Date.parse(review.followup.due_at) <= Date.parse(review.evaluated_at) ? <p>{progressState ? t("m0785") : t("m0786")}</p> : null}
      {caseStatus === "open" ? <p className="quiet-copy">{tx("m0787", { value0: formatDateTime(review.evaluated_at) })}</p> : null}
      <p className="quiet-copy">{tx("m0790", { value0: review.followup.version, value1: caseStatus === "open" ? t("m0788") : t("m0789") })}</p>
      {!editing && review.allowed_actions.includes("update_followup") ? <button className="button button-secondary" type="button" disabled={busy} onClick={() => void edit()}>{tx("m0791")}</button> : null}
      {editing ? <form className="stack-form" onSubmit={(event) => void save(event)}>
        <label>{t("m0792")}<select value={owner} disabled={draftLocked} onChange={(event) => setOwner(event.target.value)}>
          <option value="">{tx("m0759")}</option>
          {owner && !candidates.some((item) => item.subject_id === owner) ? <option value={owner}>{tx("m0793", { value0: base?.followup.assignee_name })}</option> : null}
          {candidates.map((item) => <option key={item.subject_id} value={item.subject_id} disabled={item.state !== "active" && item.subject_id !== base?.followup.assignee_subject_id}>{item.display_name}{item.state === "disabled" ? t("m0794") : ""}</option>)}
        </select></label>
        <label>{t("m0795")}<input type="datetime-local" value={local} disabled={draftLocked} onChange={(event) => { setLocal(event.target.value); setOffsetChoice(""); setDeadlineEdited(true); }} /></label>
        <label>{t("m0796")}<input value={zone} disabled={draftLocked} placeholder="Asia/Shanghai" onChange={(event) => { setZone(event.target.value); setOffsetChoice(""); setDeadlineEdited(true); }} /></label>
        {timeOptions.length > 1 ? <label>{t("m0797")}<select value={offsetChoice} disabled={draftLocked} onChange={(event) => { setOffsetChoice(event.target.value); setDeadlineEdited(true); }}><option value="">{deadlineEdited ? t("m0798") : t("m0799")}</option>{timeOptions.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</select></label> : null}
        {timeError ? <p role="alert">{timeError}</p> : null}
        <label>{t("m0800")}<textarea value={reason} rows={3} disabled={draftLocked} onChange={(event) => setReason(event.target.value)} /></label>
        <p className="quiet-copy">{tx("m0801")}</p>
        <div className="button-cluster">
          {savedVersion !== undefined ? <button className="button button-primary" type="button" disabled={busy} onClick={() => void refreshSaved()}>{tx("m0726")}</button>
            : needsReread ? <button className="button button-primary" type="button" disabled={busy} onClick={() => void edit()}>{tx("m0733")}</button>
            : <><button className="button button-primary" disabled={busy || !reason.trim() || new TextEncoder().encode(reason.trim()).length > 4000 || Boolean(local && !selectedInstant)}>{busy ? t("m0639") : pending ? t("m0802") : t("m0803")}</button><button className="button button-secondary" type="button" disabled={busy} onClick={() => void edit()}>{tx("m0804")}</button></>}
          <button className="button button-secondary" type="button" disabled={busy} onClick={() => { setEditing(false); setPending(undefined); setSavedVersion(undefined); setSaveFailure(undefined); setError(undefined); }}>{tx("m0752")}</button>
        </div>
      </form> : null}
      {showHistory ? <><h4>{tx("m0805", { value0: review.history.through_version })}</h4>
      {review.history.items.map((action) => <article className="responsibility-history" key={`${action.actor_subject_id}:${action.operation_id}`}>
        <p>{tx("m0806", { value0: action.expected_version, value1: action.current.version, value2: formatDateTime(action.at) })}</p>
        <p>{action.reason}</p><p>{responsibilityText(action.previous)} → {responsibilityText(action.current)}</p>
        <p>{deadlineText(action.previous)} → {deadlineText(action.current)}</p>
        <small>{t("m0807")}<code>{action.actor_subject_id}</code> {t("m0808")}{action.case_version}</small>
      </article>)}
      {!review.history.items.length ? <p>{tx("m0809")}</p> : null}
      {review.history.next_before_version ? <button className="button button-secondary" type="button" disabled={busy} onClick={() => void moreHistory()}>{tx("m0810")}</button> : <p className="quiet-copy">{tx("m0811")}</p>}</> : <p className="quiet-copy">{tx("m0812")}</p>}
    </div> : null}
  </details>;
}
