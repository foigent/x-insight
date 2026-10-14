import { useEffect, useState, type FormEvent } from "react";
import { isAbortError, reviewPolicyAPI, type AccessSessionResponse,
  type ReviewCaptureDefaultView, type ReviewCaptureSettings, type UROperation, type URPolicyConfig, type URPolicyScopes, type URPolicyUsers, type URPolicyView } from "./api";
import { currentURPolicyRelation, makeURScopedPut, newUROperationRef, type PendingURWrite } from "./reviewURPolicy";

interface Props { session: AccessSessionResponse; xi: ReviewCaptureDefaultView | undefined;
  xiDraft?: ReviewCaptureSettings; onXIDraft?: (settings: ReviewCaptureSettings) => void;
  onXIReadback?: () => Promise<unknown>; onPaired?: () => void; enterpriseRequestKey?: number }
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

export function ReviewURPolicyPanel({ session, xi, xiDraft, onXIDraft, onXIReadback, onPaired, enterpriseRequestKey = 0 }: Props) {
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
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const key = pendingKey(session);
  useEffect(() => {
    const controller = new AbortController();
    setScopes(undefined); setProjects([]); setView(undefined); setOperation(undefined); setLastOperation(undefined); setReadbackStatus("");
    const saved = storageRead(key);
    setPending(saved);
    if (saved) { setScopeKind(saved.scopeKind ?? "project"); setProjectID(saved.projectID);
	  setUserID(saved.userID ?? ""); setGrantID(saved.delegationID); }
    void reviewPolicyAPI.urScopes("", controller.signal).then(response => {
      if (response.ur.scopes) { setScopes(response.ur.scopes); setProjects(response.ur.scopes.projects); onPaired?.(); }
    }).catch(reason => { if (!isAbortError(reason)) setError("UniRouter 授权范围暂不可读；请核对成对安装及策略管理凭据。"); });
    if (saved) void reviewPolicyAPI.urOperation(saved.operationRef, controller.signal)
      .then(response => { setOperation(response.ur.operation); setReadbackStatus(response.ur.readback_status ?? ""); })
      .catch(reason => { if (!isAbortError(reason)) setNotice("上次操作尚无可读结果；保留原操作引用，先读回再决定是否重试。"); });
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
	    .catch(reason => { if (!isAbortError(reason)) setError("UR 项目成员授权范围暂不可读。"); });
	  return () => controller.abort();
	}, [scopeKind, projectID]);
  const grants = scopes?.grants.filter(grant => eligible(grant, scopeKind, projectID, userID)) ?? [];
  const chosenGrant = grants.find(grant => grant.grant_id === grantID);
  const xiChanged = Boolean(scopeKind === "enterprise" && xiDraft && xi && (xi.state === "unconfigured" ||
    JSON.stringify(xiDraft) !== JSON.stringify(xi.policy.desired)));
  async function readScope() {
    if (!grantID || scopeKind !== "enterprise" && !projectID || scopeKind === "user" && !userID || !scopes) return;
    setBusy(true); setError(""); setNotice("");
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
        try { await onXIReadback(); } catch { setNotice("UR 当前策略已读回，但 XI 当前版本暂不可读；请稍后重新读取再比较。"); }
      }
    } catch { setView(undefined); setError("当前 UniRouter 策略不可读；没有提交写入。"); }
    finally { setBusy(false); }
  }
  async function readOperation() {
    if (!pending) return;
    setBusy(true); setError("");
    setView(undefined);
    try {
      const response = await reviewPolicyAPI.urOperation(pending.operationRef);
      setOperation(response.ur.operation);
      setReadbackStatus(response.ur.readback_status ?? "");
      if (onXIReadback) {
        try { await onXIReadback(); } catch { setNotice("UR 操作已读回，但 XI 当前版本暂不可读；请重新读取 XI 策略。"); }
      }
      setNotice(response.ur.operation?.state === "effective" ? "该操作曾在 UniRouter 生效；请重新读取当前项目策略核对是否仍一致。" :
        "已读回原操作；请继续核对当前策略与 XI 默认策略。");
    } catch { setReadbackStatus(""); setNotice("原操作暂不可读；保留引用与请求内容，不创建新操作。"); }
    finally { setBusy(false); }
  }
  async function apply(event: FormEvent) {
    event.preventDefault();
    if (!view || !draft || !chosenGrant?.actions.includes("write") ||
	  scopeKind !== "enterprise" && !projectID || scopeKind === "user" && !userID || !scopes ||
      !xi || xi.state !== "unconfigured" && (xi.drift || !xi.policy.effective) ||
      xi.state === "unconfigured" && !xiChanged) return;
    if (pending && operation?.state !== "failed" && operation?.state !== "drift") {
      setError("先处理原操作引用，不能从当前草稿创建第二次写入。"); return;
    }
    let write: PendingURWrite;
    try {
      const config = draft.mode === "full_content" ? { ...draft, quota_bytes: scopeKind === "user" ? undefined : draft.quota_bytes,
        allowed_purposes: ["human_review"], on_limit: "record_capture_gap" } :
        { ...draft, allowed_purposes: [], retention_seconds: undefined, quota_bytes: undefined,
          max_saved_bytes: undefined, on_limit: undefined };
      if (scopeKind === "enterprise" && xiChanged && xiDraft && fullContentMode(config.mode) !== fullContentMode(xiDraft.mode)) {
        setError("联合保存的 XI 与 UR 正文开关不同。请先将本次 XI 草稿开关填入 UR 草稿并审阅来源数值。"); return;
      }
      if (!config.supported_protocols.length || !config.observation_points.length) {
        setError("请选择至少一个来源支持的协议和观察点。"); return;
      }
      if (config.mode === "full_content" && (!Number.isSafeInteger(config.retention_seconds) ||
        scopeKind !== "user" && !Number.isSafeInteger(config.quota_bytes) || !Number.isSafeInteger(config.max_saved_bytes) ||
        !config.retention_seconds || scopeKind !== "user" && !config.quota_bytes || !config.max_saved_bytes ||
        scopeKind !== "user" && config.max_saved_bytes > config.quota_bytes! ||
        scopeKind === "user" && (!view.effective?.project_quota_bytes || config.max_saved_bytes > view.effective.project_quota_bytes))) {
        setError("完整正文需要有效的留存、容量和单条上限。"); return;
      }
      if (scopeKind === "enterprise" && config.mode !== "full_content" && (!Number.isSafeInteger(quota) || !quota)) {
        setError("UR 企业策略需要明确每项目正文配额。"); return;
      }
      write = { operationRef: newUROperationRef(), scopeKind, orgID: scopes.org_id, projectID, userID,
	    delegationID: chosenGrant.grant_id, delegationRevision: chosenGrant.revision,
	    policy: makeURScopedPut(view, scopeKind, config, executionMode,
        config.mode === "full_content" && scopeKind !== "user" ? config.quota_bytes : quota),
        jointXI: xiChanged && xiDraft ? { settings: xiDraft, expectedRevision: xi.state === "unconfigured" ? 0 : xi.policy.desired_revision } : undefined };
    } catch { setError("策略引用不可用；先重新读取当前项目策略。"); return; }
    if (!storageWrite(key, write)) { setError("浏览器无法保存操作引用；为避免丢响应后重复写入，本次未提交。"); return; }
    setPending(write); setOperation(undefined); setReadbackStatus(""); setBusy(true); setError(""); setNotice("");
    try {
      const response = await sendURWrite(write);
      setOperation(response.ur.operation);
      setReadbackStatus(response.ur.readback_status ?? "");
      let xiReadable = true;
      if (onXIReadback) {
        try { await onXIReadback(); } catch { xiReadable = false; }
      }
      setNotice(!xiReadable ? "UR 操作已读回，但 XI 当前版本暂不可读；请重新读取后再确认状态。" :
        write.jointXI ? "XI 期望与 UR 操作已同事务保存。XI 尚待确认生效；先核对 UR 操作与当前来源策略。" :
          "已读回本次操作；请再次读取当前 UR 策略确认状态。");
    } catch { setNotice("写入结果未确认；操作引用已保留。请先用原引用读回，勿创建新写入。"); }
    finally { setBusy(false); }
  }
  async function retryExact() {
    if (!pending || operation?.state !== "unconfirmed" || readbackStatus !== "not_found" ||
      !window.confirm("仅用原操作引用和原请求内容重试。服务器会再次核对 UR 操作确实为 404，且当前版本未变化。")) return;
    setBusy(true); setError("");
    try {
      const response = await sendURWrite(pending, true);
      setOperation(response.ur.operation); setReadbackStatus(response.ur.readback_status ?? "");
      setNotice("原引用已读回；请重新读取当前项目策略。");
    } catch { setReadbackStatus(""); setNotice("原引用仍未确认，保持原请求，不生成新操作。"); }
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
  return <section id="review-ur-policy" tabIndex={-1} aria-label="UniRouter 来源策略"><h2>UniRouter 来源正文策略</h2>
    <p>这是来源侧采集权限；XI 上方的留存策略独立生效。只有两侧当前允许且授权范围匹配时才可能复制正文。</p>
    {error ? <p role="alert">{error}</p> : null}{notice ? <p role="status">{notice}</p> : null}
    {!scopes ? <p>正在读取 UniRouter 成对安装和授权范围…</p> : <>
      <p>来源实例 {scopes.source_instance} · 组织 {scopes.org_id} · 安装 {scopes.installation_id}</p>
      <label>策略范围 <select value={scopeKind} disabled={busy} onChange={event => {
	  setScopeKind(event.target.value as ScopeKind); setProjectID(""); setUserID(""); setGrantID(""); setView(undefined); setLastOperation(undefined);
	}}><option value="enterprise">企业默认与未来项目继承</option><option value="project">项目策略</option>
	  <option value="user">UR 项目成员例外</option></select></label>
      {scopeKind !== "enterprise" ? <label>来源项目 <select value={projectID} disabled={busy} onChange={event => {
        setProjectID(event.target.value); setUserID(""); setGrantID(""); setView(undefined); setLastOperation(undefined); }}><option value="">请选择</option>
        {projects.map(project => <option key={project.project_id} value={project.project_id}>{project.display_name} · {project.project_id}</option>)}</select></label>
      : null}
      {scopeKind !== "enterprise" && scopes.next_cursor ? <button type="button" disabled={busy} onClick={() => void reviewPolicyAPI.urScopes(scopes.next_cursor)
        .then(response => { if (response.ur.scopes) { setProjects(previous => [...previous, ...response.ur.scopes!.projects]);
          setScopes(previous => previous && response.ur.scopes ? { ...previous, next_cursor: response.ur.scopes.next_cursor } : previous); } })
        .catch(() => setError("后续项目暂不可读。"))}>加载更多项目</button> : null}
	  {scopeKind === "user" && projectID ? <><label>UR 项目成员 <select value={userID} disabled={busy} onChange={event => {
	    setUserID(event.target.value); setGrantID(""); setView(undefined); setLastOperation(undefined); }}><option value="">请选择</option>
	    {users?.users.map(user => <option key={user.user_id} value={user.user_id}>{user.display_name} · {user.user_id}</option>)}</select></label>
	    {users?.next_cursor ? <button type="button" disabled={busy} onClick={() => void reviewPolicyAPI.urUsers(projectID, users.next_cursor)
	      .then(response => { if (response.ur.users) setUsers(previous => previous ? { ...previous,
	        users: [...previous.users, ...response.ur.users!.users], next_cursor: response.ur.users!.next_cursor } : response.ur.users); })
	      .catch(() => setError("后续成员暂不可读。"))}>加载更多成员</button> : null}
	    <p>UR 项目成员只用于来源侧用户策略；XI 账号例外另按已认证密钥所有者证据选择。</p></> : null}
      {(scopeKind === "enterprise" || projectID && (scopeKind !== "user" || userID)) ? <><label>授权委托 <select value={grantID} disabled={busy} onChange={event => { setGrantID(event.target.value); setView(undefined); setLastOperation(undefined); }}>
        <option value="">请选择</option>{grants.map(grant => <option key={grant.grant_id} value={grant.grant_id}>
          {grant.grant_id} · {grant.actions.includes("write") ? "可管理" : "只读"} · 版本 {grant.revision}</option>)}</select></label>
        <button type="button" disabled={busy || !grantID} onClick={() => void readScope()}>读取当前来源策略</button></> : null}
    </>}
    {view ? <><p>来源策略引用：{viewedLayer?.policy_ref ?? "无显式层"}；当前状态：{viewedLayer?.application_status ?? "继承"}；
      当前生效：{effectiveConfig?.mode ?? "未知"}；来源存储：{view.storage_status}。</p>
      <details><summary>来源版本与继承</summary><p>本层期望版本 {viewedLayer?.desired?.policy_version ?? "继承"}；
        本层生效版本 {viewedLayer?.effective?.policy_version ?? "继承"}；企业期望版本 {view.enterprise.desired?.policy_version ?? "无"}。</p></details>
      {scopeKind === "enterprise" && !view.enterprise.desired && !view.effective ?
        <p role="status">UR 企业策略尚未配置。下方为来源实际支持能力内的未保存推荐草稿，首次提交使用期望版本 0；提交后仍需读回确认。</p> : null}
      {scopeKind === "enterprise" && (!view.full_content_enabled || view.storage_status !== "ready" || view.deployment_mode === "disabled") ?
        <p role="status">来源当前不满足完整正文前置条件：存储 {view.storage_status}、部署模式 {view.deployment_mode}、完整正文能力 {view.full_content_enabled ? "允许" : "未允许"}。</p> : null}
      {enterpriseModeDifferent ? <p role="alert">UR 当前正文开关与 XI 当前生效开关不同。两端留存、容量与单条上限独立管理，请分别审阅。</p> : null}
      {lastOperation ? <p role="status">此范围上次 XI 管理操作 {lastOperation.operation_ref}：{relation === "current" ? "当前版本仍一致" :
        relation === "changed" ? "当前版本已变化，请选择接受来源开关或恢复 XI 开关" :
        relation === "pending" ? "来源期望尚未生效" : "尚未核实与当前版本一致"}。</p> :
        <p>此范围没有 XI 管理操作记录；当前来源值须单独审阅。</p>}
      {scopeKind !== "enterprise" ? <p>此范围只改 UR 中选定的项目或成员策略；不会修改 XI 全局默认。XI 的项目/账号复制例外请在上方“明确例外”单独审阅。</p> : null}
      {scopeKind === "enterprise" && onXIDraft && xiDraft && effectiveConfig ?
        <button type="button" disabled={busy} onClick={() => {
          onXIDraft({ ...xiDraft, mode: effectiveConfig.mode === "full_content" ? "full_content" : "disabled" });
          setNotice("仅把 UR 当前正文开关填入 XI 草稿；XI 留存、容量和单条上限仍按 XI 原值，由你在上方分别审阅。");
        }}>按 UR 当前开关调整 XI 草稿</button> : null}
      {xi && xi.state !== "unconfigured" && xi.policy.effective && draft ?
        <button type="button" disabled={busy} onClick={() => {
          const settings = xi.policy.effective!;
          setDraft({ ...draft, mode: settings.mode, allowed_purposes: settings.mode === "full_content" ? ["human_review"] : [],
            on_limit: settings.mode === "full_content" ? "record_capture_gap" : undefined });
          setNotice("仅把 XI 当前正文开关填入 UR 草稿；UR 留存、容量和单条上限仍按来源值，需要分别审阅。");
        }}>按 XI 生效开关调整 UR 草稿</button> : null}
      {scopeKind === "enterprise" && xiDraft && draft && fullContentMode(draft.mode) !== fullContentMode(xiDraft.mode) ?
        <><p role="alert">UR 待审正文开关与本次 XI 草稿不同，不能联合保存。</p>
          <button type="button" disabled={busy} onClick={() => setDraft(previous => previous && alignWithXIDraft(previous, xiDraft.mode))}>
            按本次 XI 草稿开关调整 UR 草稿</button></> : null}
      {draft ? <form onSubmit={event => void apply(event)}><h3>当前范围期望设置</h3>
        {scopeKind === "enterprise" && !view.enterprise.desired && !view.effective && view.policy_capabilities ? <>
          <fieldset><legend>来源支持的协议（默认全选，可缩小）</legend>{view.policy_capabilities.supported_protocols.map(value =>
            <label key={value}><input type="checkbox" checked={draft.supported_protocols.includes(value)} disabled={busy}
              onChange={event => setDraft(previous => previous && { ...previous, supported_protocols: event.target.checked ?
                [...previous.supported_protocols, value] : previous.supported_protocols.filter(item => item !== value) })} />{value}</label>)}</fieldset>
          <fieldset><legend>来源支持的观察点（默认全选，可缩小）</legend>{view.policy_capabilities.observation_points.map(value =>
            <label key={value}><input type="checkbox" checked={draft.observation_points.includes(value)} disabled={busy}
              onChange={event => setDraft(previous => previous && { ...previous, observation_points: event.target.checked ?
                [...previous.observation_points, value] : previous.observation_points.filter(item => item !== value) })} />{value}</label>)}</fieldset>
        </> : <p>协议 {draft.supported_protocols.join("、")}；观察点 {draft.observation_points.join("、")}。保留当前已验证能力范围。</p>}
        <label>来源正文模式 <select value={draft.mode} disabled={busy} onChange={event => setDraft(previous => previous &&
          { ...previous, mode: event.target.value as URPolicyConfig["mode"] })}><option value="disabled">禁用</option><option value="metadata_only">仅元数据</option>
          <option value="full_content">允许完整正文</option></select></label>
        <label>执行存储 <select value={executionMode} disabled={busy} onChange={event => setExecutionMode(event.target.value)}>
          <option value="temporary_allowed">允许临时存储</option><option value="strict_no_persist">严格不落盘</option></select></label>
        {draft.mode === "full_content" ? <><label>来源留存（小时） <input type="number" min="1" max="8760" required
          value={draft.retention_seconds ? draft.retention_seconds / 3600 : ""} disabled={busy}
          onChange={event => setDraft(previous => previous && { ...previous, retention_seconds: Math.round(Number(event.target.value) * 3600) })} /></label>
          {scopeKind !== "user" ? <label>UR 每项目正文配额（GiB） <input type="number" min="0.000001" max="8192" step="any" required
            value={draft.quota_bytes ? draft.quota_bytes / gib : ""} disabled={busy}
            onChange={event => setDraft(previous => previous && { ...previous, quota_bytes: Math.round(Number(event.target.value) * gib) })} /></label> :
            <p>用户例外继承项目容量 {view.effective?.project_quota_bytes ? `${view.effective.project_quota_bytes / gib} GiB` : "未知"}。</p>}
          <label>来源单条上限（MiB） <input type="number" min="0.000001" max="32" step="any" required
            value={draft.max_saved_bytes ? draft.max_saved_bytes / mib : ""} disabled={busy}
            onChange={event => setDraft(previous => previous && { ...previous, max_saved_bytes: Math.round(Number(event.target.value) * mib) })} /></label></> : null}
        {scopeKind !== "user" && draft.mode !== "full_content" ? <label>{scopeKind === "enterprise" ? "UR 默认每项目正文配额" : "UR 当前项目正文配额"}（GiB，{scopeKind === "enterprise" ? "必填" : "留空继承"}） <input type="number" min="0.000001" max="8192" step="any" required={scopeKind === "enterprise"}
          value={quota === undefined ? "" : quota / gib} disabled={busy}
          onChange={event => setQuota(event.target.value === "" ? undefined : Math.round(Number(event.target.value) * gib))} /></label> : null}
        {executionMode === "strict_no_persist" && draft.mode === "full_content" ?
          <p role="alert">严格不落盘与完整正文冲突。请选择允许临时存储或更改正文模式。</p> : null}
        <button type="submit" disabled={busy || !chosenGrant?.actions.includes("write") || !xi ||
          xi.state !== "unconfigured" && (Boolean(xi.drift) || !xi.policy.effective) ||
          xi.state === "unconfigured" && !xiChanged || executionMode === "strict_no_persist" && draft.mode === "full_content" ||
          Boolean(pending && operation?.state !== "failed" && operation?.state !== "drift")}>
          {xiChanged ? "联合保存 XI 期望与 UR 策略" : "按当前版本提交来源策略"}</button>
      </form> : <p>来源策略缺少可编辑的协议或观察点；本页不会推测配置。</p>}</> : null}
    {pending ? <section aria-label="来源操作读回"><h3>上次操作</h3><p>引用 {pending.operationRef}；
      历史状态 {operation?.state ?? "未确认"}。</p>
      <button type="button" disabled={busy} onClick={() => void readOperation()}>用原引用读回</button>
      {operation?.state === "unconfirmed" && readbackStatus === "not_found" ?
        <button type="button" disabled={busy} onClick={() => void retryExact()}>使用原引用重试原请求</button> : null}
      {readbackStatus === "unavailable" ? <p>UR 操作读回暂不可用；此状态不允许重试写入。</p> : null}
      {operation && (operation.state === "effective" || operation.state === "failed" || operation.state === "drift" ||
        operation.state === "pending" && relation === "current") && view ?
        <button type="button" disabled={busy} onClick={() => { sessionStorage.removeItem(key); setPending(undefined); setOperation(undefined);
          setNotice("历史操作已从本浏览器待处理列表移出；提交新策略前请核对上方当前来源版本。"); }}>完成本次操作并准备新提交</button> : null}
      <p>{relation === "current" ? "当前 UR 策略与该操作以及 XI 生效版本一致。" :
        relation === "changed" ? "当前策略已变化；历史操作不能证明现状。" :
        relation === "pending" ? "来源当前期望尚未生效。" :
        relation === "unconfirmed" ? "操作结果尚未确认或未生效。" : "尚未同时读回当前 UR 策略和 XI 生效版本。"}</p></section> : null}
  </section>;
}
