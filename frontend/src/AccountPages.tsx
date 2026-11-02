import { useEffect, useMemo, useState, type FormEvent } from "react";
import { managedAccountsAPI, type ManagedAccount, type AccessSessionResponse, type ProjectReadCommand, type ProjectReadOptions } from "./api";
import { t, tx, useLocale, useMessageState, type MessageKey } from "./i18n";

const fields = ["summary", "details", "coverage", "source_context", "source_identity", "source_cost", "source_runtime", "source_attempt", "source_capability", "source_policy", "source_destination"] as const;
const fieldLabel = (field: string) => t(`accounts.field.${field}` as MessageKey);

export function AccountPasswordPage() {
  useLocale();
  const [token, setToken] = useState(() => window.location.hash.slice(1));
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [busy, setBusy] = useState(false);
  const [complete, setComplete] = useState(false);
  const [message, setMessage] = useMessageState("");
  useEffect(() => { window.history.replaceState(null, "", "/access/activate"); }, []);
  async function submit(event: FormEvent) {
    event.preventDefault(); setBusy(true); setMessage("");
    try {
      await managedAccountsAPI.setPassword(token, password, confirmation);
      setToken(""); setComplete(true);
    } catch { setMessage(() => t("accounts.passwordFailed")); }
    finally { setPassword(""); setConfirmation(""); setBusy(false); }
  }
  return <main className="login-shell" id="main-content"><section className="login-card">
    <div className="login-brand" aria-hidden="true">XI</div>
    <h1>{tx("accounts.setPassword")}</h1><p>{tx("accounts.holderSetsPassword")}</p>
    {complete ? <><p role="status">{tx("accounts.passwordSet")}</p><a href="/login">{tx("accounts.signIn")}</a></> : <form onSubmit={submit}>
      {!token && <p role="alert">{tx("accounts.missingLink")}</p>}
      {window.location.protocol !== "https:" && <p role="alert">{tx("m0000")}</p>}
      <label>{tx("accounts.newPassword")}<input type="password" autoComplete="new-password" minLength={12} maxLength={1024} required disabled={busy || !token || location.protocol !== "https:"} value={password} onChange={e => setPassword(e.target.value)} /></label>
      <label>{tx("accounts.confirmPassword")}<input type="password" autoComplete="new-password" minLength={12} maxLength={1024} required disabled={busy || !token || location.protocol !== "https:"} value={confirmation} onChange={e => setConfirmation(e.target.value)} /></label>
      <button disabled={busy || !token || location.protocol !== "https:"}>{tx("accounts.savePassword")}</button>
    </form>}
    {message && <p className="feedback feedback-error" role="alert">{message}</p>}
  </section></main>;
}

export function AccountsPage({ session }: { session: AccessSessionResponse }) {
  useLocale();
  const [items, setItems] = useState<ManagedAccount[]>([]);
  const [options, setOptions] = useState<ProjectReadOptions>({ sources: [], projects: [], grants: [] });
  const [username, setUsername] = useState("");
  const [role, setRole] = useState("workbench_investigation_reader");
  const [password, setPassword] = useState("");
  const [link, setLink] = useState("");
  const [expires, setExpires] = useState("");
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useMessageState("");
  const [subject, setSubject] = useState(session.session.subject_id);
  const [source, setSource] = useState("");
  const [project, setProject] = useState("");
  const [capabilities, setCapabilities] = useState<string[]>(["details", "coverage"]);
  const [preview, setPreview] = useState<ProjectReadCommand>();
  const sourceLabel = (id:string) => {const item=options.sources.find(s=>s.id===id);return item ? options.sources.filter(s=>s.name===item.name).length>1 ? `${item.name} · ${item.id}` : item.name : id;};
  const existing = options.grants.find(g => g.subject_id === subject && g.source_system_id === source && g.source_project_ref === project && g.cost_project_ref === null);
  const grantID = useMemo(() => existing?.id ?? crypto.randomUUID(), [existing?.id, subject, source, project]);
  async function reload(signal?: AbortSignal) {
    const [accounts, scopes] = await Promise.all([managedAccountsAPI.list(signal), managedAccountsAPI.grantOptions(signal)]);
    setItems(accounts.items); setOptions(scopes);
  }
  useEffect(() => { const controller = new AbortController(); void reload(controller.signal).catch(e => { if (e?.name !== "AbortError") setMessage(() => t("accounts.unavailable")); }); return () => controller.abort(); }, []);
  useEffect(() => { setPreview(undefined); setCapabilities(existing ? fields.filter(field => existing[`allow_${field}`] === true) : ["details", "coverage"]); }, [subject, source, project, existing?.revision]);
  async function change(action: string, account?: ManagedAccount) {
    setBusy(true); setMessage(""); setLink("");
    try {
      const result = await managedAccountsAPI.change({ action, subject_id: account?.subject_id ?? "", expected_revision: account?.revision ?? 0, ...(action === "invite" ? { username, role } : {}), password }, session.csrf_token);
      setLink(result.activation_link ?? ""); setExpires(result.expires_at ?? ""); setUsername("");
      await reload(); setMessage(() => t("accounts.changed"));
    } catch { setMessage(() => t("accounts.changeFailed")); }
    finally { setPassword(""); setBusy(false); }
  }
  function command(revoke: boolean): ProjectReadCommand { return { grant_id: grantID, subject_id: subject, source_system_id: source, source_project_ref: project, expected_revision: existing?.revision ?? 0, capabilities, revoke, check_only: true }; }
  async function checkGrant(revoke: boolean) {
    setBusy(true); setMessage(""); setPreview(undefined);
    const proposed = command(revoke);
    try { await managedAccountsAPI.grant(proposed, "", session.csrf_token); setPreview(proposed); }
    catch { setMessage(() => t("accounts.previewFailed")); }
    finally { setBusy(false); }
  }
  async function applyGrant() {
    if (!preview) return;
    setBusy(true); setMessage("");
    try { await managedAccountsAPI.grant({ ...preview, check_only: false }, password, session.csrf_token); setPreview(undefined); await reload(); setMessage(() => t("accounts.grantSaved")); }
    catch { setMessage(() => t("accounts.changeFailed")); }
    finally { setPassword(""); setBusy(false); }
  }
  return <>
    <header className="topbar"><div><p className="eyebrow">{tx("accounts.management")}</p><h1>{tx("accounts.title")}</h1><p className="page-intro">{tx("accounts.intro")}</p></div></header>
    {message && <p className="feedback" role="status">{message}</p>}
    <section className="access-session-panel"><h2>{tx("accounts.confirmIdentity")}</h2>
      <label>{tx("accounts.ownerPassword")}<input type="password" autoComplete="current-password" maxLength={1024} value={password} disabled={busy} onChange={e => setPassword(e.target.value)} /></label>
      <p>{tx("accounts.targetSessionsOnly")}</p>
    </section>
    <section className="access-session-panel"><h2>{tx("accounts.invite")}</h2><form onSubmit={e => { e.preventDefault(); void change("invite"); }}>
      <label>{tx("m0014")}<input required minLength={3} maxLength={64} pattern={"[a-z0-9][a-z0-9._\\-]{2,63}"} value={username} disabled={busy} onChange={e => setUsername(e.target.value)} /></label>
      <label>{tx("accounts.role")}<select value={role} disabled={busy} onChange={e => setRole(e.target.value)}><option value="workbench_investigation_reader">{t("accounts.reader")}</option><option value="workbench_investigation_reviewer">{t("accounts.reviewer")}</option></select></label>
      <button disabled={busy || !password}>{tx("accounts.createLink")}</button>
    </form></section>
    {link && <section className="access-session-panel" aria-label={t("accounts.activationLink")}><h2>{tx("accounts.activationLink")}</h2><p>{tx("accounts.shareSecurely")}</p><label>{tx("accounts.link")}<input readOnly value={link} onFocus={e => e.target.select()} /></label><p>{tx("accounts.expires", { value0: new Date(expires).toLocaleString() })}</p></section>}
    <section className="access-session-panel"><h2>{tx("accounts.existing")}</h2><ul className="access-session-list">{items.map(account => <li key={account.subject_id}><div><strong>{account.username}</strong><span>{account.role === "platform_admin" ? t("accounts.owner") : account.role === "workbench_investigation_reviewer" ? t("accounts.reviewer") : t("accounts.reader")}</span><span>{account.disabled ? t("accounts.disabled") : account.needs_activation ? t("accounts.activationRequired") : t("accounts.active")}</span></div>
      {["workbench_investigation_reader", "workbench_investigation_reviewer"].includes(account.role) && <div>{!account.disabled && <button disabled={busy || !password} onClick={() => void change("recover", account)}>{tx("accounts.recover")}</button>}<button disabled={busy || !password} onClick={() => void change(account.disabled ? "enable" : "disable", account)}>{account.disabled ? tx("accounts.enable") : tx("accounts.disable")}</button></div>}</li>)}</ul></section>
    <section className="access-session-panel"><h2>{tx("accounts.projectReading")}</h2><p>{tx("accounts.explicitScope")}</p>
      <fieldset className="account-grant-scope" disabled={busy}><label>{tx("accounts.account")}<select value={subject} onChange={e => setSubject(e.target.value)}>{items.filter(a => !a.disabled).map(a => <option key={a.subject_id} value={a.subject_id}>{a.username}</option>)}</select></label>
      <label>{tx("accounts.source")}<select value={source} onChange={e => { setSource(e.target.value); setProject(""); }}><option value="">{t("accounts.chooseSource")}</option>{options.sources.map(s => <option key={s.id} value={s.id}>{sourceLabel(s.id)}</option>)}</select></label>
      <label>{tx("accounts.project")}<select value={project} onChange={e => setProject(e.target.value)}><option value="">{t("accounts.chooseProject")}</option>{options.projects.filter(p => p.source_system_id === source).map(p => <option key={p.source_project_ref} value={p.source_project_ref}>{p.source_project_ref}</option>)}</select></label>
      <div className="account-grant-fields">{fields.map(field => <label key={field}><input type="checkbox" checked={capabilities.includes(field)} disabled={field === "details" || field === "coverage"} onChange={e => { setPreview(undefined); setCapabilities(v => e.target.checked ? [...v, field] : v.filter(f => f !== field)); }} />{fieldLabel(field)}</label>)}</div>
      <p>{tx("accounts.bodyDenied")}</p><button disabled={!source || !project} onClick={() => void checkGrant(false)}>{tx("accounts.preview")}</button>{existing && <button onClick={() => void checkGrant(true)}>{tx("accounts.previewRevoke")}</button>}
      </fieldset>
      {preview && <section aria-label={t("accounts.preview")}><h3>{preview.revoke ? tx("accounts.revokePreview") : tx("accounts.readPreview")}</h3><p>{items.find(a => a.subject_id === preview.subject_id)?.username} · {sourceLabel(preview.source_system_id)} · {preview.source_project_ref}</p><ul>{preview.capabilities.map(f => <li key={f}>{fieldLabel(f)}</li>)}</ul><p>{tx("accounts.bodyDenied")}</p><button disabled={busy || !password} onClick={() => void applyGrant()}>{tx("accounts.confirmScope")}</button></section>}
    </section>
  </>;
}
