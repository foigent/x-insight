import { t, tx, useLocale, useMessageState } from "./i18n";
import { useEffect, useState, type FormEvent } from "react";
import { isAbortError, reviewPolicyAPI, type AccessSessionResponse,
  type ReviewCaptureDefaultView, type ReviewCaptureSettings, type UROperation, type URPolicyConfig, type URPolicyScopes, type URPolicyUsers, type URPolicyView } from "./api";
import { currentURPolicyRelation, makeURScopedPut, newUROperationRef, type PendingURWrite } from "./reviewURPolicy";

interface Props { session: AccessSessionResponse; xi: ReviewCaptureDefaultView | undefined;
  xiDraft?: ReviewCaptureSettings; onXIDraft?: (settings: ReviewCaptureSettings) => void;
  onXIReadback?: () => Promise<unknown>; onPaired?: () => void; enterpriseRequestKey?: number; onEnterpriseWrite?:(state:"available"|"denied"|"unknown")=>void }
const mib = 1024 * 1024;
const gib = 1024 * mib;
const pendingKey = (session: AccessSessionResponse) =>
  `xinsight:ur-policy:${JSON.stringify([session.session.subject_id, session.session.session_ref])}`;
type ScopeKind = "enterprise" | "project" | "user";
const eligible = (grant: URPolicyScopes["grants"][number], kind: ScopeKind, projectID: string, userID: string) =>
  grant.actions.includes("read") && (kind === "enterprise" ? grant.target_kind === "enterprise_default" :
    kind === "project" ? grant.target_kind === "project_policy" &&
      (grant.coverage === "all_current_future" || grant.project_id === projectID) :
      grant.target_kind === "user_exception" &&
      (grant.coverage === "all_current_future" || grant.project_id === projectID && grant.user_id === userID));
const storageRead = (key: string): PendingURWrite | undefined => {
  try {
    const value = JSON.parse(sessionStorage.getItem(key) ?? "null") as PendingURWrite | null;
    const kind = value?.scopeKind ?? "project";
    return value && /^xipw_[a-f0-9]{32}$/.test(value.operationRef) &&
      (kind === "enterprise" && /^[1-9][0-9]*$/.test(value.orgID ?? "") ||
        kind === "project" && /^[1-9][0-9]*$/.test(value.projectID) ||
        kind === "user" && /^[1-9][0-9]*$/.test(value.projectID) && /^[1-9][0-9]*$/.test(value.userID ?? "")) ? value : undefined;
  } catch { return undefined; }
};
const storageWrite = (key: string, pending: PendingURWrite) => {
  try { sessionStorage.setItem(key, JSON.stringify(pending)); return true; } catch { return false; }
};
function draftFrom(view: URPolicyView, kind: ScopeKind): URPolicyConfig | undefined {
  const layer = kind === "enterprise" ? view.enterprise : kind === "user" ? view.user : view.project;
  const inherited = layer?.desired?.config ?? view.effective?.effective.config ?? view.enterprise.desired?.config;
  if (!inherited && kind === "enterprise" && !view.enterprise.desired && !view.effective) {
    const caps = view.policy_capabilities;
    if (!caps?.supported_protocols?.length || !caps.observation_points?.length) return undefined;
    const full = view.full_content_enabled && view.storage_status === "ready" &&
      view.deployment_mode !== "disabled" && Number.isSafeInteger(view.limits?.max_saved_bytes) &&
      (view.limits?.max_saved_bytes ?? 0) > 0;
    return { mode: full ? "full_content" : "metadata_only",
      supported_protocols: [...caps.supported_protocols], observation_points: [...caps.observation_points],
      allowed_purposes: full ? ["human_review"] : [],
      retention_seconds: full ? 30 * 86400 : undefined,
      quota_bytes: full ? 5 * gib : undefined,
      max_saved_bytes: full ? Math.min(32 * mib, view.limits!.max_saved_bytes) : undefined,
      on_limit: full ? "record_capture_gap" : undefined };
  }
  if (!inherited?.supported_protocols?.length || !inherited.observation_points?.length) return undefined;
  if (inherited.mode !== "full_content") return { ...inherited, allowed_purposes: [],
    retention_seconds: undefined, quota_bytes: undefined, max_saved_bytes: undefined, on_limit: undefined };
  return { ...inherited, quota_bytes: kind === "user" ? undefined : inherited.quota_bytes };
}
const fullContentMode = (mode: URPolicyConfig["mode"] | ReviewCaptureSettings["mode"]) => mode === "full_content";
function alignWithXIDraft(config: URPolicyConfig, xiMode: ReviewCaptureSettings["mode"]): URPolicyConfig {
  const mode = fullContentMode(xiMode) ? "full_content" : config.mode === "disabled" ? "disabled" : "metadata_only";
  return { ...config, mode, allowed_purposes: mode === "full_content" ? ["human_review"] : [],
    on_limit: mode === "full_content" ? "record_capture_gap" : undefined };
}

export function ReviewURPolicyPanel({ session, xi, xiDraft, onXIDraft, onXIReadback, onPaired, enterpriseRequestKey = 0, onEnterpriseWrite }: Props) {
  useLocale();
	const [scopeKind, setScopeKind] = useState<ScopeKind>("enterprise");
  const [scopes, setScopes] = useState<URPolicyScopes>();
  const [projects, setProjects] = useState<URPolicyScopes["projects"]>([]);
  const [projectID, setProjectID] = useState("");
	const [userID, setUserID] = useState("");
	const [users, setUsers] = useState<URPolicyUsers>();
  const [grantID, setGrantID] = useState("");
  const [view, setView] = useState<URPolicyView>();
  const [draft, setDraft] = useState<URPolicyConfig>();
  const [executionMode, setExecutionMode] = useState("temporary_allowed");
  const [quota, setQuota] = useState<number>();
  const [pending, setPending] = useState<PendingURWrite>();
  const [operation, setOperation] = useState<UROperation>();
  const [lastOperation, setLastOperation] = useState<UROperation>();
  const [readbackStatus, setReadbackStatus] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useMessageState("");
  const [notice, setNotice] = useMessageState("");
  const key = pendingKey(session);
  useEffect(() => {
    const controller = new AbortController();
    setScopes(undefined); setProjects([]); setView(undefined); setOperation(undefined); setLastOperation(undefined); setReadbackStatus("");
    const saved = storageRead(key);
    setPending(saved);
    if (saved) { setScopeKind(saved.scopeKind ?? "project"); setProjectID(saved.projectID);
	  setUserID(saved.userID ?? ""); setGrantID(saved.delegationID); }
    void reviewPolicyAPI.urScopes("", controller.signal).then(response => {
      if (response.ur.scopes) { setScopes(response.ur.scopes); setProjects(response.ur.scopes.projects); onPaired?.(); onEnterpriseWrite?.(response.ur.scopes.grants.some(grant=>grant.target_kind==="enterprise_default"&&grant.actions.includes("read")&&grant.actions.includes("write"))?"available":"denied"); }
    }).catch(reason => { if (!isAbortError(reason)){onEnterpriseWrite?.("unknown");setError(() => t("m1880"));} });
    if (saved) void reviewPolicyAPI.urOperation(saved.operationRef, controller.signal)
      .then(response => { setOperation(response.ur.operation); setReadbackStatus(response.ur.readback_status ?? ""); })
      .catch(reason => { if (!isAbortError(reason)) setNotice(() => t("m1881")); });
    return () => controller.abort();
  }, [key]);
	useEffect(() => {
	  if (!enterpriseRequestKey) return;
	  if (scopeKind !== "enterprise") {
	    setScopeKind("enterprise"); setProjectID(""); setUserID(""); setGrantID(""); setView(undefined); setLastOperation(undefined);
	  }
	  if (xiDraft) setDraft(previous => previous && alignWithXIDraft(previous, xiDraft.mode));
	  document.getElementById("review-ur-policy")?.focus();
	}, [enterpriseRequestKey]);
	useEffect(() => {
	  if (scopeKind !== "user" || !projectID) { setUsers(undefined); return; }
	  const controller = new AbortController();
	  void reviewPolicyAPI.urUsers(projectID, "", controller.signal).then(response => setUsers(response.ur.users))
	    .catch(reason => { if (!isAbortError(reason)) setError(() => t("m1882")); });
	  return () => controller.abort();
	}, [scopeKind, projectID]);
  const grants = scopes?.grants.filter(grant => eligible(grant, scopeKind, projectID, userID)) ?? [];
 useEffect(()=>{if(scopeKind!=="enterprise"||!enterpriseRequestKey||!scopes)return;const writable=scopes.grants.filter(g=>eligible(g,"enterprise","","")&&g.actions.includes("write"));if(writable.length===1)setGrantID(writable[0].grant_id)},[scopeKind,enterpriseRequestKey,scopes]);
  const chosenGrant = grants.find(grant => grant.grant_id === grantID);
  const xiChanged = Boolean(scopeKind === "enterprise" && xiDraft && xi && (xi.state === "unconfigured" ||
    JSON.stringify(xiDraft) !== JSON.stringify(xi.policy.desired)));
  async function readScope() {
    if (!grantID || scopeKind !== "enterprise" && !projectID || scopeKind === "user" && !userID || !scopes) return;
    setBusy(true); setError(() => ""); setNotice(() => "");
    try {
      const response = scopeKind === "enterprise" ? await reviewPolicyAPI.urEnterprise(scopes.org_id, grantID) :
	    scopeKind === "user" ? await reviewPolicyAPI.urUser(projectID, userID, grantID) :
	    await reviewPolicyAPI.urProject(projectID, grantID);
      if (!response.ur.view) throw new Error("missing policy view");
      setView(response.ur.view);
      setLastOperation(response.ur.last_operation);
	  const candidate = draftFrom(response.ur.view, scopeKind);
	  setDraft(candidate && scopeKind === "enterprise" && enterpriseRequestKey && xiDraft ?
	    alignWithXIDraft(candidate, xiDraft.mode) : candidate);
	  const layer = scopeKind === "enterprise" ? response.ur.view.enterprise : scopeKind === "user" ? response.ur.view.user : response.ur.view.project;
      setExecutionMode(layer?.desired_execution_storage_mode ??
        response.ur.view.effective?.execution_storage_mode ?? "temporary_allowed");
	  setQuota(scopeKind === "enterprise" ? layer?.desired_default_project_quota_bytes ?? layer?.effective_default_project_quota_bytes ?? candidate?.quota_bytes : layer?.project_quota_bytes);
      if (onXIReadback) {
        try { await onXIReadback(); } catch { setNotice(() => t("m1883")); }
      }
    } catch { setView(undefined); setError(() => t("m1884")); }
    finally { setBusy(false); }
  }
  async function readOperation() {
    if (!pending) return;
    setBusy(true); setError(() => "");
    setView(undefined);
    try {
      const response = await reviewPolicyAPI.urOperation(pending.operationRef);
      setOperation(response.ur.operation);
      setReadbackStatus(response.ur.readback_status ?? "");
      if (onXIReadback) {
        try { await onXIReadback(); } catch { setNotice(() => t("m1885")); }
      }
      setNotice(() => response.ur.operation?.state === "effective" ? t("m1886") :
        t("m1887"));
    } catch { setReadbackStatus(""); setNotice(() => t("m1888")); }
    finally { setBusy(false); }
  }
  async function apply(event: FormEvent) {
    event.preventDefault();
    if (!view || !draft || !chosenGrant?.actions.includes("write") ||
	  scopeKind !== "enterprise" && !projectID || scopeKind === "user" && !userID || !scopes ||
      !xi || xi.state !== "unconfigured" && (xi.drift || !xi.policy.effective) ||
      xi.state === "unconfigured" && !xiChanged) return;
    if (pending && operation?.state !== "failed" && operation?.state !== "drift") {
      setError(() => t("m1889")); return;
    }
    let write: PendingURWrite;
    try {
      const config = draft.mode === "full_content" ? { ...draft, quota_bytes: scopeKind === "user" ? undefined : draft.quota_bytes,
        allowed_purposes: ["human_review"], on_limit: "record_capture_gap" } :
        { ...draft, allowed_purposes: [], retention_seconds: undefined, quota_bytes: undefined,
          max_saved_bytes: undefined, on_limit: undefined };
      if (scopeKind === "enterprise" && xiChanged && xiDraft && fullContentMode(config.mode) !== fullContentMode(xiDraft.mode)) {
        setError(() => t("m1890")); return;
      }
      if (!config.supported_protocols.length || !config.observation_points.length) {
        setError(() => t("m1891")); return;
      }
      if (config.mode === "full_content" && (!Number.isSafeInteger(config.retention_seconds) ||
        scopeKind !== "user" && !Number.isSafeInteger(config.quota_bytes) || !Number.isSafeInteger(config.max_saved_bytes) ||
        !config.retention_seconds || scopeKind !== "user" && !config.quota_bytes || !config.max_saved_bytes ||
        scopeKind !== "user" && config.max_saved_bytes > config.quota_bytes! ||
        scopeKind === "user" && (!view.effective?.project_quota_bytes || config.max_saved_bytes > view.effective.project_quota_bytes))) {
        setError(() => t("m1892")); return;
      }
      if (scopeKind === "enterprise" && config.mode !== "full_content" && (!Number.isSafeInteger(quota) || !quota)) {
        setError(() => t("m1893")); return;
      }
      write = { operationRef: newUROperationRef(), scopeKind, orgID: scopes.org_id, projectID, userID,
	    delegationID: chosenGrant.grant_id, delegationRevision: chosenGrant.revision,
	    policy: makeURScopedPut(view, scopeKind, config, executionMode,
        config.mode === "full_content" && scopeKind !== "user" ? config.quota_bytes : quota),
        jointXI: xiChanged && xiDraft ? { settings: xiDraft, expectedRevision: xi.state === "unconfigured" ? 0 : xi.policy.desired_revision } : undefined };
    } catch { setError(() => t("m1894")); return; }
    if (!storageWrite(key, write)) { setError(() => t("m1895")); return; }
    setPending(write); setOperation(undefined); setReadbackStatus(""); setBusy(true); setError(() => ""); setNotice(() => "");
    try {
      const response = await sendURWrite(write);
      setOperation(response.ur.operation);
      setReadbackStatus(response.ur.readback_status ?? "");
      let xiReadable = true;
      if (onXIReadback) {
        try { await onXIReadback(); } catch { xiReadable = false; }
      }
      setNotice(() => !xiReadable ? t("m1896") :
        write.jointXI ? t("m1897") :
          t("m1898"));
    } catch { setNotice(() => t("m1899")); }
    finally { setBusy(false); }
  }
  async function retryExact() {
    if (!pending || operation?.state !== "unconfirmed" || readbackStatus !== "not_found" ||
      !window.confirm(t("m1900"))) return;
    setBusy(true); setError(() => "");
    try {
      const response = await sendURWrite(pending, true);
      setOperation(response.ur.operation); setReadbackStatus(response.ur.readback_status ?? "");
      setNotice(() => t("m1901"));
    } catch { setReadbackStatus(""); setNotice(() => t("m1902")); }
    finally { setBusy(false); }
  }
	function sendURWrite(write: PendingURWrite, retry = false) {
	  if (write.scopeKind === "enterprise") return reviewPolicyAPI.urApplyEnterprise(write.orgID ?? "", write.delegationID,
	    write.delegationRevision, write.operationRef, write.policy, session.csrf_token, retry, write.jointXI);
	  if (write.scopeKind === "user") return reviewPolicyAPI.urApplyUser(write.projectID, write.userID ?? "", write.delegationID,
	    write.delegationRevision, write.operationRef, write.policy, session.csrf_token, retry, write.jointXI);
	  return reviewPolicyAPI.urApply(write.projectID, write.delegationID, write.delegationRevision,
	    write.operationRef, write.policy, session.csrf_token, retry, write.jointXI);
	}
  const observedOperation = pending && (pending.scopeKind ?? "project") === scopeKind ? operation : lastOperation;
  const relation = currentURPolicyRelation(observedOperation, view, xi);
  const effectiveConfig = scopeKind === "enterprise" ? view?.enterprise.effective?.config : view?.effective?.effective.config;
  const enterpriseModeDifferent = scopeKind === "enterprise" && effectiveConfig && xi && xi.state !== "unconfigured" &&
    xi.policy.effective && (effectiveConfig.mode === "full_content") !== (xi.policy.effective.mode === "full_content");
	const viewedLayer = view ? scopeKind === "enterprise" ? view.enterprise : scopeKind === "user" ? view.user : view.project : undefined;
  async function refreshScopes() {
    setBusy(true); setError(() => ""); onEnterpriseWrite?.("unknown");
    try {
      const response = await reviewPolicyAPI.urScopes("");
      if (!response.ur.scopes) throw new Error("missing scopes");
      const next=response.ur.scopes;
      setScopes(next);setProjects(next.projects);setGrantID("");setView(undefined);setDraft(undefined);
      onEnterpriseWrite?.(next.grants.some(grant=>grant.target_kind==="enterprise_default"&&grant.actions.includes("read")&&grant.actions.includes("write"))?"available":"denied");
    } catch { setScopes(undefined);setView(undefined);setDraft(undefined);setError(() => t("m1903")); }
    finally {setBusy(false)}
  }
  return <section id="review-ur-policy" tabIndex={-1} aria-label={t("m1904")}><h2>{tx("m1905")}</h2>
    <p>{tx("m1906")}</p>
    {error ? <p role="alert">{error}</p> : null}{notice ? <p role="status">{notice}</p> : null}
    <button type="button" disabled={busy} onClick={()=>void refreshScopes()}>{tx("m1907")}</button>
    {!scopes ? <p>{tx("m1908")}</p> : <>
      <p>{tx("m1909", { value0: scopes.source_instance, value1: scopes.org_id, value2: scopes.installation_id })}</p>
      <label>{t("m1910")}<select value={scopeKind} disabled={busy} onChange={event => {
	  setScopeKind(event.target.value as ScopeKind); setProjectID(""); setUserID(""); setGrantID(""); setView(undefined); setLastOperation(undefined);
	}}><option value="enterprise">{tx("m1911")}</option><option value="project">{tx("m1912")}</option>
	  <option value="user">{tx("m1913")}</option></select></label>
      {scopeKind !== "enterprise" ? <label>{t("m1914")}<select value={projectID} disabled={busy} onChange={event => {
        setProjectID(event.target.value); setUserID(""); setGrantID(""); setView(undefined); setLastOperation(undefined); }}><option value="">{tx("m1788")}</option>
        {projects.map(project => <option key={project.project_id} value={project.project_id}>{project.display_name} · {project.project_id}</option>)}</select></label>
      : null}
      {scopeKind !== "enterprise" && scopes.next_cursor ? <button type="button" disabled={busy} onClick={() => void reviewPolicyAPI.urScopes(scopes.next_cursor)
        .then(response => { if (response.ur.scopes) { setProjects(previous => [...previous, ...response.ur.scopes!.projects]);
          setScopes(previous => previous && response.ur.scopes ? { ...previous, next_cursor: response.ur.scopes.next_cursor } : previous); } })
        .catch(() => setError(() => t("m1916")))}>{tx("m1915")}</button> : null}
	  {scopeKind === "user" && projectID ? <><label>{t("m1917")}<select value={userID} disabled={busy} onChange={event => {
	    setUserID(event.target.value); setGrantID(""); setView(undefined); setLastOperation(undefined); }}><option value="">{tx("m1788")}</option>
	    {users?.users.map(user => <option key={user.user_id} value={user.user_id}>{user.display_name} · {user.user_id}</option>)}</select></label>
	    {users?.next_cursor ? <button type="button" disabled={busy} onClick={() => void reviewPolicyAPI.urUsers(projectID, users.next_cursor)
	      .then(response => { if (response.ur.users) setUsers(previous => previous ? { ...previous,
	        users: [...previous.users, ...response.ur.users!.users], next_cursor: response.ur.users!.next_cursor } : response.ur.users); })
	      .catch(() => setError(() => t("m1919")))}>{tx("m1918")}</button> : null}
	    <p>{tx("m1920")}</p></> : null}
      {(scopeKind === "enterprise" || projectID && (scopeKind !== "user" || userID)) ? <><label>{t("m1921")}<select value={grantID} disabled={busy} onChange={event => { setGrantID(event.target.value); setView(undefined); setLastOperation(undefined); }}>
        <option value="">{tx("m1788")}</option>{grants.map(grant => <option key={grant.grant_id} value={grant.grant_id}>
          {scopeKind==="enterprise"?t("m1922"):scopeKind==="project"?t("m1912"):t("m1923")} · {grant.actions.includes("write") ? t("m1924") : t("m1925")}</option>)}</select></label>
        <button type="button" disabled={busy || !grantID} onClick={() => void readScope()}>{tx("m1926")}</button></> : null}
    </>}
 {scopes&&scopeKind==="enterprise"&&!scopes.grants.some(g=>eligible(g,"enterprise","","")&&g.actions.includes("write"))?<p role="status">{tx("m1927")}</p>:null}
    {view ? <><p>{tx("m1930", { value0: viewedLayer?.policy_ref ?? t("m1928"), value1: viewedLayer?.application_status ?? t("m1929"), value2: effectiveConfig?.mode ?? t("m0193"), value3: view.storage_status })}</p>
      <details><summary>{tx("m1931")}</summary><p>{tx("m1932", { value0: viewedLayer?.desired?.policy_version ?? t("m1929"), value1: viewedLayer?.effective?.policy_version ?? t("m1929"), value2: view.enterprise.desired?.policy_version ?? t("m0342") })}</p></details>
      {scopeKind === "enterprise" && !view.enterprise.desired && !view.effective ?
        <p role="status">{tx("m1933")}</p> : null}
      {scopeKind === "enterprise" && (!view.full_content_enabled || view.storage_status !== "ready" || view.deployment_mode === "disabled") ?
        <p role="status">{tx("m1935", { value0: view.storage_status, value1: view.deployment_mode, value2: view.full_content_enabled ? t("m1776") : t("m1934") })}</p> : null}
      {enterpriseModeDifferent ? <p role="alert">{tx("m1936")}</p> : null}
      {lastOperation ? <p role="status">{tx("m1941", { value0: lastOperation.operation_ref, value1: relation === "current" ? t("m1937") :
        relation === "changed" ? t("m1938") :
        relation === "pending" ? t("m1939") : t("m1940") })}</p> :
        <p>{tx("m1942")}</p>}
      {scopeKind !== "enterprise" ? <p>{tx("m1943")}</p> : null}
      {scopeKind === "enterprise" && onXIDraft && xiDraft && effectiveConfig ?
        <button type="button" disabled={busy} onClick={() => {
          onXIDraft({ ...xiDraft, mode: effectiveConfig.mode === "full_content" ? "full_content" : "disabled" });
          setNotice(() => t("m1945"));
        }}>{tx("m1944")}</button> : null}
      {xi && xi.state !== "unconfigured" && xi.policy.effective && draft ?
        <button type="button" disabled={busy} onClick={() => {
          const settings = xi.policy.effective!;
          setDraft({ ...draft, mode: settings.mode, allowed_purposes: settings.mode === "full_content" ? ["human_review"] : [],
            on_limit: settings.mode === "full_content" ? "record_capture_gap" : undefined });
          setNotice(() => t("m1947"));
        }}>{tx("m1946")}</button> : null}
      {scopeKind === "enterprise" && xiDraft && draft && fullContentMode(draft.mode) !== fullContentMode(xiDraft.mode) ?
        <><p role="alert">{tx("m1948")}</p>
          <button type="button" disabled={busy} onClick={() => setDraft(previous => previous && alignWithXIDraft(previous, xiDraft.mode))}>{tx("m1949")}</button></> : null}
      {draft ? <form onSubmit={event => void apply(event)}><h3>{tx("m1950")}</h3>
        {scopeKind === "enterprise" && !view.enterprise.desired && !view.effective && view.policy_capabilities ? <>
          <fieldset><legend>{tx("m1951")}</legend>{view.policy_capabilities.supported_protocols.map(value =>
            <label key={value}><input type="checkbox" checked={draft.supported_protocols.includes(value)} disabled={busy}
              onChange={event => setDraft(previous => previous && { ...previous, supported_protocols: event.target.checked ?
                [...previous.supported_protocols, value] : previous.supported_protocols.filter(item => item !== value) })} />{value}</label>)}</fieldset>
          <fieldset><legend>{tx("m1952")}</legend>{view.policy_capabilities.observation_points.map(value =>
            <label key={value}><input type="checkbox" checked={draft.observation_points.includes(value)} disabled={busy}
              onChange={event => setDraft(previous => previous && { ...previous, observation_points: event.target.checked ?
                [...previous.observation_points, value] : previous.observation_points.filter(item => item !== value) })} />{value}</label>)}</fieldset>
        </> : <p>{tx("m1953", { value0: draft.supported_protocols.join("、"), value1: draft.observation_points.join("、") })}</p>}
        <label>{t("m1954")}<select value={draft.mode} disabled={busy} onChange={event => setDraft(previous => previous &&
          { ...previous, mode: event.target.value as URPolicyConfig["mode"] })}><option value="disabled">{tx("m1777")}</option><option value="metadata_only">{tx("m0481")}</option>
          <option value="full_content">{tx("m1792")}</option></select></label>
        <label>{t("m1955")}<select value={executionMode} disabled={busy} onChange={event => setExecutionMode(event.target.value)}>
          <option value="temporary_allowed">{tx("m1956")}</option><option value="strict_no_persist">{tx("m1957")}</option></select></label>
        {draft.mode === "full_content" ? <><label>{t("m1958")}<input type="number" min="1" max="8760" required
          value={draft.retention_seconds ? draft.retention_seconds / 3600 : ""} disabled={busy}
          onChange={event => setDraft(previous => previous && { ...previous, retention_seconds: Math.round(Number(event.target.value) * 3600) })} /></label>
          {scopeKind !== "user" ? <label>{t("m1959")}<input type="number" min="0.000001" max="8192" step="any" required
            value={draft.quota_bytes ? draft.quota_bytes / gib : ""} disabled={busy}
            onChange={event => setDraft(previous => previous && { ...previous, quota_bytes: Math.round(Number(event.target.value) * gib) })} /></label> :
            <p>{tx("m1960", { value0: view.effective?.project_quota_bytes ? `${view.effective.project_quota_bytes / gib} GiB` : t("m0193") })}</p>}
          <label>{t("m1961")}<input type="number" min="0.000001" max="32" step="any" required
            value={draft.max_saved_bytes ? draft.max_saved_bytes / mib : ""} disabled={busy}
            onChange={event => setDraft(previous => previous && { ...previous, max_saved_bytes: Math.round(Number(event.target.value) * mib) })} /></label></> : null}
        {scopeKind !== "user" && draft.mode !== "full_content" ? <label>{scopeKind === "enterprise" ? t("m1962") : t("m1963")}（GiB，{scopeKind === "enterprise" ? t("m1964") : t("m1965")}） <input type="number" min="0.000001" max="8192" step="any" required={scopeKind === "enterprise"}
          value={quota === undefined ? "" : quota / gib} disabled={busy}
          onChange={event => setQuota(event.target.value === "" ? undefined : Math.round(Number(event.target.value) * gib))} /></label> : null}
        {executionMode === "strict_no_persist" && draft.mode === "full_content" ?
          <p role="alert">{tx("m1966")}</p> : null}
        <button type="submit" disabled={busy || !chosenGrant?.actions.includes("write") || !xi ||
          xi.state !== "unconfigured" && (Boolean(xi.drift) || !xi.policy.effective) ||
          xi.state === "unconfigured" && !xiChanged || executionMode === "strict_no_persist" && draft.mode === "full_content" ||
          Boolean(pending && operation?.state !== "failed" && operation?.state !== "drift")}>
          {xiChanged ? t("m1967") : t("m1968")}</button>
      </form> : <p>{tx("m1969")}</p>}</> : null}
    {pending ? <section aria-label={t("m1970")}><h3>{tx("m1971")}</h3><p>{tx("m1972", { value0: pending.operationRef, value1: operation?.state ?? t("m0098") })}</p>
      <button type="button" disabled={busy} onClick={() => void readOperation()}>{tx("m1973")}</button>
      {operation?.state === "unconfirmed" && readbackStatus === "not_found" ?
        <button type="button" disabled={busy} onClick={() => void retryExact()}>{tx("m1974")}</button> : null}
      {readbackStatus === "unavailable" ? <p>{tx("m1975")}</p> : null}
      {operation && (operation.state === "effective" || operation.state === "failed" || operation.state === "drift" ||
        operation.state === "pending" && relation === "current") && view ?
        <button type="button" disabled={busy} onClick={() => { sessionStorage.removeItem(key); setPending(undefined); setOperation(undefined);
          setNotice(() => t("m1977")); }}>{tx("m1976")}</button> : null}
      <p>{relation === "current" ? t("m1978") :
        relation === "changed" ? t("m1979") :
        relation === "pending" ? t("m1980") :
        relation === "unconfirmed" ? t("m1981") : t("m1982")}</p></section> : null}
  </section>;
}
