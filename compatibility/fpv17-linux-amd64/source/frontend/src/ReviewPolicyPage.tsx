import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiRequestError, investigationAPI, isAbortError, reviewPolicyAPI, type AccessSessionResponse,
  type ReviewCaptureDefaultView, type ReviewCaptureSettings, type ReviewCaptureProjectCatalog } from "./api";
import { ReviewURPolicyPanel } from "./ReviewURPolicyPanel";
import { ReviewExceptionPanel } from "./ReviewExceptionPanel";
import { WorkInstallPanel } from "./WorkInstallPanel";

interface Props { session: AccessSessionResponse }
const mib = 1024 * 1024;
const gib = 1024 * mib;
const hour = 3600;
const day = 24 * hour;
type URPairing = "loading" | "paired" | "standalone" | "unknown";
const modeChangedFromEffective = (current: ReviewCaptureDefaultView, draft: ReviewCaptureSettings) =>
  current.state === "unconfigured" || !current.policy.effective || draft.mode !== current.policy.effective.mode;
const amount = (value: number) => Number(value.toFixed(10)).toString();
function showFailure(error: unknown) {
  if (error instanceof ApiRequestError && error.problem?.code === "review_policy_unconfirmed")
    return "操作结果未确认。请先重新读取当前期望值与生效值，再决定是否确认或重试。";
  if (error instanceof ApiRequestError && error.status === 403) return "当前会话无权管理复核正文策略。";
  return "策略暂不可用，请重新读取后再试。";
}

export function ReviewPolicyPage({ session }: Props) {
  const [current, setCurrent] = useState<ReviewCaptureDefaultView>();
  const [draft, setDraft] = useState<ReviewCaptureSettings>();
  const [retentionUnit, setRetentionUnit] = useState<"days" | "hours">("days");
  const [projects, setProjects] = useState<ReviewCaptureProjectCatalog>();
  const [sourceChoice, setSourceChoice] = useState("");
  const [targetProject, setTargetProject] = useState("__new__");
  const [newProjectName, setNewProjectName] = useState("");
  const [busy, setBusy] = useState(false);
  const [pairing, setPairing] = useState<URPairing>("loading");
  const pairingRef = useRef<URPairing>("loading");
  const [enterpriseRequestKey, setEnterpriseRequestKey] = useState(0);
const [enterpriseWrite,setEnterpriseWrite]=useState<"available"|"denied"|"unknown">("unknown");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const read = async (signal?: AbortSignal) => {
    const value = await reviewPolicyAPI.current(signal);
    setCurrent(value);
    setDraft(value.state === "unconfigured" ? value.suggested : value.policy.desired);
    const seconds = value.state === "unconfigured" ? value.suggested.retention_seconds : value.policy.desired.retention_seconds;
    setRetentionUnit(seconds % day === 0 ? "days" : "hours");
    setError("");
    return value;
  };
  const readPairing = async (signal?: AbortSignal): Promise<URPairing> => {
    try {
      const value = await investigationAPI.listConnectors(signal);
      const installations = value.installations ?? [];
      const observed: URPairing = installations.some(item => item.connector_type === "unirouter" &&
        Boolean(item.installation_acknowledgement_sha256)) ? "paired" :
        value.installation_control?.enabled && installations.length === 0 ? "standalone" : "unknown";
      const next = pairingRef.current === "paired" ? "paired" : observed;
      pairingRef.current = next; setPairing(next);
      return next;
    } catch (reason) {
      if (!isAbortError(reason)) {
        const next = pairingRef.current === "paired" ? "paired" : "unknown";
        pairingRef.current = next; setPairing(next);
      }
      return pairingRef.current;
    }
  };
  useEffect(() => {
    const controller = new AbortController();
    pairingRef.current = "loading"; setPairing("loading");
    void read(controller.signal).catch(reason => { if (!isAbortError(reason)) setError(showFailure(reason)); });
    void readPairing(controller.signal);
    void reviewPolicyAPI.projects(controller.signal).then(setProjects)
      .catch(reason => { if (!isAbortError(reason)) setError("项目绑定资料暂不可读，请重新读取。"); });
    return () => controller.abort();
  }, [session.session.session_ref]);
  async function refresh() {
    setBusy(true); setNotice("");
    try { await read(); setProjects(await reviewPolicyAPI.projects()); await readPairing(); } catch (reason) { setError(showFailure(reason)); }
    finally { setBusy(false); }
  }
  async function apply(event: FormEvent) {
    event.preventDefault();
    if (!draft || !current) return;
    if (modeChangedFromEffective(current, draft)) {
      const pairingNow = pairingRef.current === "standalone" ? await readPairing() : pairingRef.current;
      if (pairingNow === "paired") {
 if(enterpriseWrite!=="available"){setNotice(enterpriseWrite==="denied"?"当前连接没有企业正文联管写权限。请由 UniRouter 组织管理员明确授予企业默认策略的读取与修改权限。":"企业正文联管权限尚未读回，请先重新读取来源授权范围。");return}
        setEnterpriseRequestKey(previous => previous + 1);
        setNotice("正文开关已保留在 XI 草稿。请在下方企业范围读取 UR 当前策略，并使用联合保存。");
        return;
      }
      if (pairingNow !== "standalone") {
        setNotice("无法确认当前是否已配对 UniRouter；未单独写入 XI 正文开关。请重新读取配对状态。");
        return;
      }
    }
    setBusy(true); setError(""); setNotice("");
    try {
      const result = await reviewPolicyAPI.apply(draft,
        current.state === "unconfigured" ? 0 : current.policy.desired_revision, session.csrf_token);
      setCurrent(result);
      setNotice(result.state === "pending" ? "期望值已保存，尚未生效。请读回并确认。" : "策略已生效。已读回当前版本。");
    } catch (reason) { setError(showFailure(reason)); }
    finally { setBusy(false); }
  }
  async function confirm() {
    if (!current || current.state === "unconfigured" || !current.drift) return;
    setBusy(true); setError(""); setNotice("");
    try {
      const latest = await reviewPolicyAPI.current();
      setCurrent(latest);
      setDraft(latest.state === "unconfigured" ? latest.suggested : latest.policy.desired);
      const seconds = latest.state === "unconfigured" ? latest.suggested.retention_seconds : latest.policy.desired.retention_seconds;
      setRetentionUnit(seconds % day === 0 ? "days" : "hours");
      if (latest.state === "unconfigured" || !latest.drift || latest.policy.desired_revision !== current.policy.desired_revision) {
        setNotice("版本已变化，请先检查当前期望值。");
        return;
      }
      const result = await reviewPolicyAPI.confirm(latest.policy.desired_revision, session.csrf_token);
      setCurrent(result); setNotice("该期望版本已确认生效。");
    } catch (reason) { setError(showFailure(reason)); }
    finally { setBusy(false); }
  }
  const change = (key: keyof ReviewCaptureSettings, value: string) => setDraft(previous => previous ? {
    ...previous, [key]: key === "mode" ? value : Number(value),
  } as ReviewCaptureSettings : previous);
  const formatRetention = (seconds: number) => seconds % day === 0 ? `${seconds / day} 天` : `${amount(seconds / hour)} 小时`;
  const choiceKey = (item: { source_system_id: string; source_instance: string; org_id: string; project_id: string }) =>
    `${item.source_system_id}:${item.source_instance}:${item.org_id}:${item.project_id}`;
  const selectedChoice = projects?.items.find(item => choiceKey(item) === sourceChoice);
  const modeChanged = Boolean(current && draft && modeChangedFromEffective(current, draft));
  async function bindProject(event: FormEvent) {
    event.preventDefault();
    if (!selectedChoice || selectedChoice.xi_project_id) return;
    const existing = projects?.projects.find(item => item.id === targetProject);
    const name = existing?.display_name ?? newProjectName.trim();
    if (!name) return;
    setBusy(true); setError(""); setNotice("");
    try {
      const result = await reviewPolicyAPI.bindProject(selectedChoice, existing?.id ?? "", name, session.csrf_token);
      setProjects(result); setNotice("来源项目已绑定到 XI 项目；新正文复制将使用该项目配额。");
      setSourceChoice(""); setNewProjectName("");
    } catch (reason) { setError(showFailure(reason)); }
    finally { setBusy(false); }
  }
  return <section className="product-page review-policy-page" aria-labelledby="review-policy-title">
    <header className="page-header"><div><p className="eyebrow">管理 / 复核正文</p><h1 id="review-policy-title">正文留存策略</h1>
      <p>控制 XI 自有加密副本的默认留存、项目容量与单条上限。来源策略和项目例外仍分别约束实际复制。</p></div>
      <button type="button" onClick={() => void refresh()} disabled={busy}>重新读取</button></header>
    {error ? <p role="alert">{error}</p> : null}{notice ? <p role="status">{notice}</p> : null}
    {current && draft ? <><section aria-label="当前状态">
      <details><summary>技术版本</summary><p>期望版本：{current.state === "unconfigured" ? "未配置" : current.policy.desired_revision}；
        生效版本：{current.state === "unconfigured" || current.policy.effective_revision === 0 ? "无" : current.policy.effective_revision}。</p></details>
      {current.state !== "unconfigured" && current.drift ? <p role="status">期望值尚未生效。先核对下方设置，再确认该版本。</p> : null}
      {current.state !== "unconfigured" && current.policy.effective ? <p>当前生效：{current.policy.effective.mode === "full_content" ? "允许完整正文" : "禁用"}，
        留存 {formatRetention(current.policy.effective.retention_seconds)}，项目容量 {amount(current.policy.effective.project_quota_bytes / gib)} GiB，
        单条上限 {amount(current.policy.effective.max_item_bytes / mib)} MiB。</p> : null}
    </section><form onSubmit={event => void apply(event)}>
      <h2>期望设置</h2>
      <label>正文复制模式 <select value={draft.mode} onChange={event => change("mode", event.target.value)} disabled={busy}>
        <option value="disabled">禁用</option><option value="full_content">允许完整正文</option></select></label>
      <label>留存时间 <input type="number" min={retentionUnit === "days" ? 1 / day : 1 / hour}
        max={retentionUnit === "days" ? 365 : 8760} step="any" required
        value={amount(draft.retention_seconds / (retentionUnit === "days" ? day : hour))}
        onChange={event => change("retention_seconds", String(Math.round(Number(event.target.value) * (retentionUnit === "days" ? day : hour))))} disabled={busy} />
        <select aria-label="留存时间单位" value={retentionUnit} onChange={event => setRetentionUnit(event.target.value as "days" | "hours")} disabled={busy}>
          <option value="days">天</option><option value="hours">小时</option></select></label>
      <label>项目容量（GiB） <input type="number" min={1 / gib} max="1024" step="any" required
        value={amount(draft.project_quota_bytes / gib)} onChange={event => change("project_quota_bytes", String(Math.round(Number(event.target.value) * gib)))} disabled={busy} /></label>
      <label>单条上限（MiB） <input type="number" min="0" max="32" step="any" required
        value={amount(draft.max_item_bytes / mib)} onChange={event => change("max_item_bytes", String(Math.round(Number(event.target.value) * mib)))} disabled={busy} /></label>
      {pairing === "paired"&&enterpriseWrite!=="available"?<p role="status">{enterpriseWrite==="denied"?"当前连接没有企业正文联管写权限，请由 UniRouter 组织管理员授予企业默认策略的读取与修改权限。供数与其他已有授权独立运行。":"正在核对企业正文联管权限；总开关在权限读回前不可提交。"}</p>:null}
      {modeChanged && pairing === "paired" ? <p role="status">正文开关需要在下方企业范围与来源策略联合保存；XI 留存数值仍独立。</p> : null}
      {modeChanged && (pairing === "loading" || pairing === "unknown") ? <p role="status">配对状态尚未确认，正文开关不会单独写入 XI。</p> : null}
      <button type="submit" disabled={busy || modeChanged && (pairing === "loading" || pairing === "unknown" || pairing==="paired"&&enterpriseWrite!=="available")}>
        {modeChanged && pairing === "paired" ? "前往企业联合保存" : modeChanged ? "保存 XI 正文模式" : "仅保存 XI 留存设置"}</button>
      {(pairing === "loading" || pairing === "unknown") ? <button type="button" disabled={busy} onClick={() => void readPairing()}>重新读取配对状态</button> : null}
      {current.state !== "unconfigured" && current.drift ? <button type="button" onClick={() => void confirm()}
        disabled={busy || JSON.stringify(draft) !== JSON.stringify(current.policy.desired)}>确认期望版本 {current.policy.desired_revision}</button> : null}
    </form><section aria-label="项目绑定"><h2>来源项目与 XI 项目</h2>
      <p>每个已接入来源项目需明确绑定到一个 XI 项目，才能保存新的复查正文；同名或同编号不会自动合并配额。</p>
      {projects ? <><ul>{projects.items.map(item => <li key={choiceKey(item)}>
        {item.source_name} · {item.source_instance} · 组织 {item.org_id} · 项目 {item.project_id}：
        {item.xi_project_id ? `已绑定 ${item.xi_project_name}` : "尚未绑定，正文复制等待配置"}</li>)}</ul>
        {projects.items.some(item => !item.xi_project_id) ? <form onSubmit={event => void bindProject(event)}>
          <label>待绑定来源项目 <select value={sourceChoice} onChange={event => setSourceChoice(event.target.value)} disabled={busy} required>
            <option value="">请选择</option>{projects.items.filter(item => !item.xi_project_id).map(item =>
              <option key={choiceKey(item)} value={choiceKey(item)}>{item.source_name} · {item.source_instance} · 项目 {item.project_id}</option>)}</select></label>
          <label>XI 项目 <select value={targetProject} onChange={event => setTargetProject(event.target.value)} disabled={busy}>
            <option value="__new__">新建 XI 项目</option>{projects.projects.map(item => <option key={item.id} value={item.id}>{item.display_name}</option>)}</select></label>
          {targetProject === "__new__" ? <label>新项目名称 <input value={newProjectName} onChange={event => setNewProjectName(event.target.value)}
            maxLength={128} required disabled={busy} /></label> : null}
          <button type="submit" disabled={busy || !sourceChoice}>绑定项目</button>
        </form> : null}</> : <p>正在读取已接入项目…</p>}
    </section></> : !error ? <p>正在读取策略…</p> : null}
    <ReviewExceptionPanel session={session} current={current} projects={projects} />
    <ReviewURPolicyPanel session={session} xi={current} xiDraft={draft} onXIDraft={setDraft}
      enterpriseRequestKey={enterpriseRequestKey} onEnterpriseWrite={setEnterpriseWrite} onPaired={() => { pairingRef.current = "paired"; setPairing("paired"); }}
      onXIReadback={async () => { setCurrent(await reviewPolicyAPI.current()); }} />
    <WorkInstallPanel session={session} projects={projects} />
  </section>;
}
