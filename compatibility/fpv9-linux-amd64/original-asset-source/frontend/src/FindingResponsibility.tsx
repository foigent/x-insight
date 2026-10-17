import { type FormEvent, useEffect, useRef, useState } from "react";
import { ApiRequestError, isAbortError, workspaceAPI, type AccessSessionResponse, type FindingFollowup, type FindingFollowupCommand, type FindingReview, type LocalAssignee } from "./api";
import { ErrorState } from "./Feedback";
import { formatDateTime } from "./format";
import { deadlineCandidates, localDeadline, preciseDeadlineValue } from "./responsibilityTime";

interface Props { caseID: string; findingID: string; caseStatus: string; session: AccessSessionResponse; onChanged: () => void; showHistory?: boolean; progressState?: string | null }

function responsibilityText(value: FindingFollowup) {
  if (!value.recorded) return "责任信息未记录";
  return value.assignee_subject_id ? `${value.assignee_name} · ${value.assignee_subject_id}` : "未分配";
}

function deadlineText(value: FindingFollowup) {
  if (!value.due_at || !value.due_timezone) return "未设期限";
  return `${localDeadline(value.due_at, value.due_timezone).replace("T", " ")} · ${value.due_timezone}`;
}

export function FindingResponsibility({ caseID, findingID, caseStatus, session, onChanged, showHistory = true, progressState }: Props) {
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
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const lifetime = useRef<AbortController>(new AbortController());
  const generation = useRef(0);
  const loadSequence = useRef(0);

  function failed(caught: unknown, token: number) {
    if (token !== generation.current || isAbortError(caught)) return;
    if (caught instanceof ApiRequestError && [401, 403, 404].includes(caught.status)) {
      setReview(undefined); setCandidates([]); setEditing(false); setBase(undefined); setPending(undefined); setSavedVersion(undefined); setSaveFailure(undefined); setNotice("");
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
    setReview(undefined); setBase(undefined); setEditing(false); setPending(undefined); setSavedVersion(undefined); setSaveFailure(undefined); setError(undefined); setCandidates([]); setBusy(false); setNotice("");
    if (open) void load().catch((caught) => failed(caught, token));
    return () => { generation.current++; controller.abort(); };
    // The resource identity and Case seal state define this panel's lifetime.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, caseID, findingID, caseStatus]);

  const needsReread = saveFailure === "conflict" || saveFailure === "sealed";

  async function edit() {
    const token = generation.current;
    setBusy(true); setNotice("");
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
      if (needsReread) setNotice("已重新读取当前值。请核对负责人、期限并填写修改依据后保存。");
    } catch (caught) { failed(caught, token); }
    finally { if (token === generation.current) setBusy(false); }
  }

  let timeOptions: ReturnType<typeof deadlineCandidates> = [];
  let timeError = "";
  if (local) {
    try { timeOptions = deadlineCandidates(local, zone); if (!timeOptions.length) timeError = "该时区不存在这一当地时间，请检查日期与夏令时。"; }
    catch { timeError = "请输入有效时区，例如 Asia/Shanghai。"; }
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
    setPending(command); setBusy(true); setError(undefined); setSaveFailure(undefined); setNotice("");
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
    setNotice(`动作已保存为责任版本 ${version}；当前版本 ${current.followup.version}。`);
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
    <summary>责任与期限</summary>
    {error ? <ErrorState error={error} /> : null}
    {saveFailure === "conflict" ? <p role="alert">记录版本已变化，本次修改未保存。请重新读取并核对当前值，再决定是否重新提交。</p> : null}
    {saveFailure === "sealed" ? <p role="alert">Case 已封存，本次修改未保存。请重新读取并核对封存记录。</p> : null}
    {saveFailure === "unknown" ? <p role="alert">尚未确认保存结果。请原样重试同一操作以核对结果；重试不会重复记录。</p> : null}
    {saveFailure === "rejected" ? <p role="alert">请求已被拒绝，本次修改未保存。请检查错误和输入后重新提交。</p> : null}
    {error && savedVersion !== undefined ? <p role="alert">动作已确认保存为责任版本 {savedVersion}，但当前记录读取失败。请重新读取当前记录，无需再次提交。</p> : null}
    {notice ? <p role="status">{notice}</p> : null}
    {open && !review && !error ? <p>正在读取责任记录…</p> : null}
    {review ? <div className="stack-form">
      <p><strong>{responsibilityText(review.followup)}</strong></p>
      {["disabled", "not_found", "unavailable"].includes(review.assignee_state) && caseStatus === "open" ? <p role="note">原负责人账号已失效或当前无法确认；历史身份保留，请核对是否需要重新分配。</p> : null}
      <p>{deadlineText(review.followup)}</p>
      {review.followup.due_at && caseStatus === "open" && progressState !== "ended" && Date.parse(review.followup.due_at) <= Date.parse(review.evaluated_at) ? <p>{progressState ? "跟进期限已过。" : "登记期限已过；请读取复核与跟进记录确认进度。"}</p> : null}
      {caseStatus === "open" ? <p className="quiet-copy">核对时间：{formatDateTime(review.evaluated_at)}</p> : null}
      <p className="quiet-copy">责任版本 {review.followup.version} · {caseStatus === "open" ? "登记不授予访问权，也不发送通知。" : "封存时责任记录；不表示风险已经消除。"}</p>
      {!editing && review.allowed_actions.includes("update_followup") ? <button className="button button-secondary" type="button" disabled={busy} onClick={() => void edit()}>登记或修改责任</button> : null}
      {editing ? <form className="stack-form" onSubmit={(event) => void save(event)}>
        <label>负责人<select value={owner} disabled={draftLocked} onChange={(event) => setOwner(event.target.value)}>
          <option value="">未分配</option>
          {owner && !candidates.some((item) => item.subject_id === owner) ? <option value={owner}>{base?.followup.assignee_name}（当前无法确认，保留原记录）</option> : null}
          {candidates.map((item) => <option key={item.subject_id} value={item.subject_id} disabled={item.state !== "active" && item.subject_id !== base?.followup.assignee_subject_id}>{item.display_name}{item.state === "disabled" ? "（已失效）" : ""}</option>)}
        </select></label>
        <label>登记期限（可不填）<input type="datetime-local" value={local} disabled={draftLocked} onChange={(event) => { setLocal(event.target.value); setOffsetChoice(""); setDeadlineEdited(true); }} /></label>
        <label>期限时区<input value={zone} disabled={draftLocked} placeholder="Asia/Shanghai" onChange={(event) => { setZone(event.target.value); setOffsetChoice(""); setDeadlineEdited(true); }} /></label>
        {timeOptions.length > 1 ? <label>该当地时间出现两次，请选择<select value={offsetChoice} disabled={draftLocked} onChange={(event) => { setOffsetChoice(event.target.value); setDeadlineEdited(true); }}><option value="">{deadlineEdited ? "选择具体时刻" : "保留原记录时刻"}</option>{timeOptions.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</select></label> : null}
        {timeError ? <p role="alert">{timeError}</p> : null}
        <label>修改依据<textarea value={reason} rows={3} disabled={draftLocked} onChange={(event) => setReason(event.target.value)} /></label>
        <p className="quiet-copy">说明责任决定；不要粘贴受限原文或凭据。最多 4000 UTF-8 字节。</p>
        <div className="button-cluster">
          {savedVersion !== undefined ? <button className="button button-primary" type="button" disabled={busy} onClick={() => void refreshSaved()}>重新读取当前记录</button>
            : needsReread ? <button className="button button-primary" type="button" disabled={busy} onClick={() => void edit()}>重新读取并核对</button>
            : <><button className="button button-primary" disabled={busy || !reason.trim() || new TextEncoder().encode(reason.trim()).length > 4000 || Boolean(local && !selectedInstant)}>{busy ? "正在保存…" : pending ? "原样重试同一操作" : "保存责任记录"}</button><button className="button button-secondary" type="button" disabled={busy} onClick={() => void edit()}>重新读取当前值</button></>}
          <button className="button button-secondary" type="button" disabled={busy} onClick={() => { setEditing(false); setPending(undefined); setSavedVersion(undefined); setSaveFailure(undefined); setError(undefined); }}>取消编辑</button>
        </div>
      </form> : null}
      {showHistory ? <><h4>修改历史 · 截至责任版本 {review.history.through_version}</h4>
      {review.history.items.map((action) => <article className="responsibility-history" key={`${action.actor_subject_id}:${action.operation_id}`}>
        <p>版本 {action.expected_version} → {action.current.version} · {formatDateTime(action.at)}</p>
        <p>{action.reason}</p><p>{responsibilityText(action.previous)} → {responsibilityText(action.current)}</p>
        <p>{deadlineText(action.previous)} → {deadlineText(action.current)}</p>
        <small>修改者 <code>{action.actor_subject_id}</code> · Case 版本 {action.case_version}</small>
      </article>)}
      {!review.history.items.length ? <p>尚无责任修改记录。</p> : null}
      {review.history.next_before_version ? <button className="button button-secondary" type="button" disabled={busy} onClick={() => void moreHistory()}>读取更早修改</button> : <p className="quiet-copy">已显示此版本范围内的全部责任修改。</p>}</> : <p className="quiet-copy">责任动作与复核、进度动作统一显示在“复核与跟进”的历史中。</p>}
    </div> : null}
  </details>;
}
