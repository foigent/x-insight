import { useEffect, useState, type FormEvent } from "react";
import { isAbortError, reviewPolicyAPI, type AccessSessionResponse, type ReviewCaptureDefaultView,
  type ReviewCaptureException, type ReviewCaptureExceptionCatalog, type ReviewCaptureExceptionState,
  type ReviewCaptureProjectCatalog } from "./api";

interface Props {
  session: AccessSessionResponse;
  current?: ReviewCaptureDefaultView;
  projects?: ReviewCaptureProjectCatalog;
}

const categories = ["user_input", "user_output", "model_input", "model_output", "tool_input", "tool_output",
  "client_request", "client_response", "provider_request", "provider_response"];
const categoryLabels: Record<string, string> = { user_input: "用户输入", user_output: "用户输出",
  model_input: "模型输入", model_output: "模型输出", tool_input: "工具输入", tool_output: "工具输出",
  client_request: "客户端请求", client_response: "客户端响应", provider_request: "提供方请求", provider_response: "提供方响应" };
const scopeLabels: Record<ReviewCaptureException["selector_kind"], string> = {
  tenant_category: "全部来源的正文类别", source_project: "来源项目",
  source_project_category: "来源项目的正文类别", account: "已验证的 UR 密钥所有者",
  account_category: "已验证的 UR 密钥所有者与正文类别" };
const scopeKinds: Array<{ value: ReviewCaptureException["selector_kind"]; label: string }> = [
  { value: "tenant_category", label: "全部来源的正文类别" },
  { value: "source_project", label: "来源项目" },
  { value: "source_project_category", label: "来源项目的正文类别" },
  { value: "account", label: "已验证的 UR 密钥所有者" },
  { value: "account_category", label: "已验证的 UR 密钥所有者与正文类别" },
];
const projectKey = (item: { source_system_id?: string; source_instance?: string; org_id?: string; project_id?: string }) =>
  JSON.stringify([item.source_system_id ?? "", item.source_instance ?? "", item.org_id ?? "", item.project_id ?? ""]);
const mib = 1024 * 1024;
const day = 86400;

export function ReviewExceptionPanel({ session, current, projects }: Props) {
  const [catalog, setCatalog] = useState<ReviewCaptureExceptionCatalog>();
  const [editing, setEditing] = useState<ReviewCaptureExceptionState>();
  const [kind, setKind] = useState<ReviewCaptureException["selector_kind"]>("source_project");
  const [project, setProject] = useState("");
  const [account, setAccount] = useState("");
  const [category, setCategory] = useState("user_input");
  const [mode, setMode] = useState<"disabled" | "full_content">("disabled");
  const [retentionDays, setRetentionDays] = useState(30);
  const [maxMiB, setMaxMiB] = useState(32);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    void reviewPolicyAPI.exceptions(controller.signal).then(setCatalog).catch(reason => {
      if (!isAbortError(reason)) setError("项目例外暂不可读。");
    });
    return () => controller.abort();
  }, [session.session.session_ref]);
  const selected = projects?.items.find(item => projectKey(item) === project);
  const verified = catalog?.verified_accounts.filter(item => projectKey(item) === project) ?? [];
  const needsProject = kind !== "tenant_category";
  const needsAccount = kind === "account" || kind === "account_category";
  const needsCategory = kind === "tenant_category" || kind === "source_project_category" || kind === "account_category";
  const accountSelectable = verified.some(item => item.account_id === account);

  function edit(item: ReviewCaptureExceptionState) {
    setEditing(item); setKind(item.selector_kind); setProject(projectKey(item)); setAccount(item.account_id ?? "");
    setCategory(item.content_kind || "user_input"); setMode(item.mode);
    setRetentionDays(item.retention_seconds / day); setMaxMiB(item.max_item_bytes / mib);
    setError(""); setMessage("");
  }
  async function refresh() {
    setBusy(true); setError("");
    try { setCatalog(await reviewPolicyAPI.exceptions()); }
    catch { setError("项目例外暂不可读。"); }
    finally { setBusy(false); }
  }
  async function save(event: FormEvent) {
    event.preventDefault();
    if (current?.state === "unconfigured" || !current?.policy.effective || current.drift ||
      needsProject && !selected || needsAccount && !accountSelectable) return;
    const retention = Math.round(retentionDays * day);
    const maxItem = Math.round(maxMiB * mib);
    if (!Number.isSafeInteger(retention) || retention < 1 || retention > 31536000 ||
      !Number.isSafeInteger(maxItem) || maxItem < 0 || maxItem > 32 * mib) {
      setError("留存时间或单条上限超出允许范围。"); return;
    }
    const exception: ReviewCaptureException = {
      policy_ref: editing?.policy_ref, selector_kind: kind,
      source_system_id: needsProject ? selected!.source_system_id : undefined,
      source_instance: needsProject ? selected!.source_instance : undefined,
      org_id: needsProject ? selected!.org_id : undefined,
      project_id: needsProject ? selected!.project_id : undefined,
      account_authority: needsAccount ? "ur_key_owner" : undefined,
      account_id: needsAccount ? account : undefined,
      content_kind: needsCategory ? category : undefined,
      mode, retention_seconds: retention, max_item_bytes: maxItem,
    };
    setBusy(true); setError(""); setMessage("");
    try {
      const saved = await reviewPolicyAPI.applyException(exception, editing?.desired_revision ?? 0, session.csrf_token);
      setCatalog(await reviewPolicyAPI.exceptions()); setEditing(saved);
      setMessage(saved.desired_revision === saved.effective_revision ? "例外已生效；只影响后续正文。" :
        "例外期望值已保存，尚未生效。请读回后确认。");
    } catch { setError("例外结果未确认。请先重新读取列表，核对来源范围和版本。"); }
    finally { setBusy(false); }
  }
  async function confirm(item: ReviewCaptureExceptionState) {
    setBusy(true); setError(""); setMessage("");
    try {
      const latest = await reviewPolicyAPI.exceptions();
      setCatalog(latest);
      const matching = latest.items.find(value => value.policy_ref === item.policy_ref);
      if (!matching || matching.desired_revision !== item.desired_revision || matching.effective_revision >= matching.desired_revision) {
        setMessage("例外版本已变化，请重新核对。"); return;
      }
      await reviewPolicyAPI.confirmException(item.policy_ref, item.desired_revision, session.csrf_token);
      setCatalog(await reviewPolicyAPI.exceptions()); setMessage("例外期望版本已确认生效。");
    } catch { setError("确认结果未明。请重新读取例外版本。"); }
    finally { setBusy(false); }
  }
  return <section aria-label="正文复制例外"><h2>明确例外</h2>
    <p>例外只对新复制生效。优先级：已验证账号与类别、账号、来源项目与类别、来源项目、全部来源类别、默认策略。</p>
    {error ? <p role="alert">{error}</p> : null}{message ? <p role="status">{message}</p> : null}
    <button type="button" disabled={busy} onClick={() => void refresh()}>重新读取例外</button>
    {catalog?.items_truncated || catalog?.accounts_truncated ?
      <p role="status">例外或已验证账号列表超过本页上限。当前列表仅显示前 256 项；请勿把未列出视为不存在。</p> : null}
    {catalog ? <ul>{catalog.items.map(item => <li key={item.policy_ref}>
      {scopeLabels[item.selector_kind]} · {item.source_instance || "全部来源"} {item.project_id ? `/ ${projects?.items.find(project => projectKey(project) === projectKey(item))?.xi_project_name || item.project_id}` : ""}
      {item.account_id ? ` · UR 密钥所有者 ${item.account_id}` : ""}{item.content_kind ? ` · ${categoryLabels[item.content_kind] ?? item.content_kind}` : ""}：
      期望 {item.mode === "full_content" ? "允许" : "禁用"} v{item.desired_revision}；
      生效 {item.effective_mode === "full_content" ? "允许" : item.effective_mode === "disabled" ? "禁用" : "未生效"}
      v{item.effective_revision}。
      <button type="button" disabled={busy} onClick={() => edit(item)}>编辑</button>
      {item.desired_revision > item.effective_revision ? <button type="button" disabled={busy}
        onClick={() => void confirm(item)}>确认此版本</button> : null}
    </li>)}</ul> : <p>正在读取例外…</p>}
    <form onSubmit={event => void save(event)}><h3>{editing ? "编辑现有例外" : "新建例外"}</h3>
      {editing ? <button type="button" disabled={busy} onClick={() => { setEditing(undefined); setMessage(""); }}>改为新建</button> : null}
      <label>作用范围 <select value={kind} disabled={busy || Boolean(editing)} onChange={event => {
        setKind(event.target.value as ReviewCaptureException["selector_kind"]); setAccount("");
      }}>{scopeKinds.map(choice => <option key={choice.value} value={choice.value}>{choice.label}</option>)}</select></label>
      {needsProject ? <label>已接入来源项目 <select value={project} disabled={busy || Boolean(editing)} required
        onChange={event => { setProject(event.target.value); setAccount(""); }}><option value="">请选择</option>
        {projects?.items.map(item => <option key={projectKey(item)} value={projectKey(item)}>
          {item.xi_project_name || item.source_name} · {item.source_instance} · {item.project_id}</option>)}</select></label> : null}
      {needsAccount ? <label>已验证 UR 密钥所有者 <select value={account} disabled={busy || Boolean(editing)} required
        onChange={event => setAccount(event.target.value)}><option value="">请选择</option>
        {verified.map(item => <option key={item.account_id} value={item.account_id}>{item.account_id}</option>)}</select></label> : null}
      {needsCategory ? <label>正文类别 <select value={category} disabled={busy || Boolean(editing)}
        onChange={event => setCategory(event.target.value)}>{categories.map(value => <option key={value} value={value}>{categoryLabels[value]}</option>)}</select></label> : null}
      <label>复制模式 <select value={mode} disabled={busy} onChange={event => setMode(event.target.value as "disabled" | "full_content")}>
        <option value="disabled">禁用</option><option value="full_content">允许完整正文</option></select></label>
      <label>留存（天） <input type="number" min={1 / day} max="365" step="any" value={retentionDays} disabled={busy}
        onChange={event => setRetentionDays(Number(event.target.value))} required /></label>
      <label>单条上限（MiB） <input type="number" min="0" max="32" step="any" value={maxMiB} disabled={busy}
        onChange={event => setMaxMiB(Number(event.target.value))} required /></label>
      <button type="submit" disabled={busy || current?.state === "unconfigured" || !current?.policy.effective || current.drift ||
        needsProject && !selected || needsAccount && !accountSelectable}>保存例外</button>
    </form>
  </section>;
}
