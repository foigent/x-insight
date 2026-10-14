import { localizedLabels, t, tx, useLocale, useMessageState } from "./i18n";
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
const categoryLabels: Record<string, string> = localizedLabels({}, {user_input: "m1749", user_output: "m1750", model_input: "m1751", model_output: "m1752", tool_input: "m1753", tool_output: "m1754", client_request: "m1755", client_response: "m1756", provider_request: "m0351", provider_response: "m0352"});
const scopeLabels: Record<ReviewCaptureException["selector_kind"], string> = localizedLabels({}, {tenant_category: "m1757", source_project: "m1180", source_project_category: "m1758", account: "m1759", account_category: "m1760"});
const scopeKinds: Array<{ value: ReviewCaptureException["selector_kind"]; label: string }> = [
  localizedLabels({value: "tenant_category"}, {label: "m1757"}),
  localizedLabels({value: "source_project"}, {label: "m1180"}),
  localizedLabels({value: "source_project_category"}, {label: "m1758"}),
  localizedLabels({value: "account"}, {label: "m1759"}),
  localizedLabels({value: "account_category"}, {label: "m1760"}),
];
const projectKey = (item: { source_system_id?: string; source_instance?: string; org_id?: string; project_id?: string }) =>
  JSON.stringify([item.source_system_id ?? "", item.source_instance ?? "", item.org_id ?? "", item.project_id ?? ""]);
const mib = 1024 * 1024;
const day = 86400;

export function ReviewExceptionPanel({ session, current, projects }: Props) {
  useLocale();
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
  const [message, setMessage] = useMessageState("");
  const [error, setError] = useMessageState("");
  useEffect(() => {
    const controller = new AbortController();
    void reviewPolicyAPI.exceptions(controller.signal).then(setCatalog).catch(reason => {
      if (!isAbortError(reason)) setError(() => t("m1761"));
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
    setError(() => ""); setMessage(() => "");
  }
  async function refresh() {
    setBusy(true); setError(() => "");
    try { setCatalog(await reviewPolicyAPI.exceptions()); }
    catch { setError(() => t("m1761")); }
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
      setError(() => t("m1762")); return;
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
    setBusy(true); setError(() => ""); setMessage(() => "");
    try {
      const saved = await reviewPolicyAPI.applyException(exception, editing?.desired_revision ?? 0, session.csrf_token);
      setCatalog(await reviewPolicyAPI.exceptions()); setEditing(saved);
      setMessage(() => saved.desired_revision === saved.effective_revision ? t("m1763") :
        t("m1764"));
    } catch { setError(() => t("m1765")); }
    finally { setBusy(false); }
  }
  async function confirm(item: ReviewCaptureExceptionState) {
    setBusy(true); setError(() => ""); setMessage(() => "");
    try {
      const latest = await reviewPolicyAPI.exceptions();
      setCatalog(latest);
      const matching = latest.items.find(value => value.policy_ref === item.policy_ref);
      if (!matching || matching.desired_revision !== item.desired_revision || matching.effective_revision >= matching.desired_revision) {
        setMessage(() => t("m1766")); return;
      }
      await reviewPolicyAPI.confirmException(item.policy_ref, item.desired_revision, session.csrf_token);
      setCatalog(await reviewPolicyAPI.exceptions()); setMessage(() => t("m1767"));
    } catch { setError(() => t("m1768")); }
    finally { setBusy(false); }
  }
  return <section aria-label={t("m1769")}><h2>{tx("m1770")}</h2>
    <p>{tx("m1771")}</p>
    {error ? <p role="alert">{error}</p> : null}{message ? <p role="status">{message}</p> : null}
    <button type="button" disabled={busy} onClick={() => void refresh()}>{tx("m1772")}</button>
    {catalog?.items_truncated || catalog?.accounts_truncated ?
      <p role="status">{tx("m1773")}</p> : null}
    {catalog ? <ul>{catalog.items.map(item => <li key={item.policy_ref}>
      {scopeLabels[item.selector_kind]} · {item.source_instance || t("m1407")} {item.project_id ? `/ ${projects?.items.find(project => projectKey(project) === projectKey(item))?.xi_project_name || item.project_id}` : ""}
      {item.account_id ? t("m1774", { value0: item.account_id }) : ""}{item.content_kind ? ` · ${categoryLabels[item.content_kind] ?? item.content_kind}` : ""}{t("m1775")}{item.mode === "full_content" ? t("m1776") : t("m1777")} v{item.desired_revision}{t("m1778")}{item.effective_mode === "full_content" ? t("m1776") : item.effective_mode === "disabled" ? t("m1777") : t("m1779")}
      v{item.effective_revision}。
      <button type="button" disabled={busy} onClick={() => edit(item)}>{tx("m1780")}</button>
      {item.desired_revision > item.effective_revision ? <button type="button" disabled={busy}
        onClick={() => void confirm(item)}>{tx("m1781")}</button> : null}
    </li>)}</ul> : <p>{tx("m1782")}</p>}
    <form onSubmit={event => void save(event)}><h3>{editing ? t("m1783") : t("m1784")}</h3>
      {editing ? <button type="button" disabled={busy} onClick={() => { setEditing(undefined); setMessage(() => ""); }}>{tx("m1785")}</button> : null}
      <label>{t("m1786")}<select value={kind} disabled={busy || Boolean(editing)} onChange={event => {
        setKind(event.target.value as ReviewCaptureException["selector_kind"]); setAccount("");
      }}>{scopeKinds.map(choice => <option key={choice.value} value={choice.value}>{choice.label}</option>)}</select></label>
      {needsProject ? <label>{t("m1787")}<select value={project} disabled={busy || Boolean(editing)} required
        onChange={event => { setProject(event.target.value); setAccount(""); }}><option value="">{tx("m1788")}</option>
        {projects?.items.map(item => <option key={projectKey(item)} value={projectKey(item)}>
          {item.xi_project_name || item.source_name} · {item.source_instance} · {item.project_id}</option>)}</select></label> : null}
      {needsAccount ? <label>{t("m1789")}<select value={account} disabled={busy || Boolean(editing)} required
        onChange={event => setAccount(event.target.value)}><option value="">{tx("m1788")}</option>
        {verified.map(item => <option key={item.account_id} value={item.account_id}>{item.account_id}</option>)}</select></label> : null}
      {needsCategory ? <label>{t("m1790")}<select value={category} disabled={busy || Boolean(editing)}
        onChange={event => setCategory(event.target.value)}>{categories.map(value => <option key={value} value={value}>{categoryLabels[value]}</option>)}</select></label> : null}
      <label>{t("m1791")}<select value={mode} disabled={busy} onChange={event => setMode(event.target.value as "disabled" | "full_content")}>
        <option value="disabled">{tx("m1777")}</option><option value="full_content">{tx("m1792")}</option></select></label>
      <label>{t("m1793")}<input type="number" min={1 / day} max="365" step="any" value={retentionDays} disabled={busy}
        onChange={event => setRetentionDays(Number(event.target.value))} required /></label>
      <label>{t("m1794")}<input type="number" min="0" max="32" step="any" value={maxMiB} disabled={busy}
        onChange={event => setMaxMiB(Number(event.target.value))} required /></label>
      <button type="submit" disabled={busy || current?.state === "unconfigured" || !current?.policy.effective || current.drift ||
        needsProject && !selected || needsAccount && !accountSelectable}>{tx("m1795")}</button>
    </form>
  </section>;
}
