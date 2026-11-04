import { useEffect, useMemo, useRef, useState, type FormEvent } from "react";
import { accessAPI, ApiRequestError, ApiResponseError, managedAccountsAPI, type ManagedAccount, type AccessSessionResponse, type ProjectReadCommand, type ProjectReadOptions } from "./api";
import { LoginPage } from "./AccessPages";
import { roleLabel } from "./accessPresentation";
import { announceSessionChange } from "./sessionChange";
import { t, tx, useLocale, useMessageState, type MessageKey } from "./i18n";

const fields = ["summary", "details", "coverage", "source_context", "source_identity", "source_cost", "source_runtime", "source_attempt", "source_capability", "source_policy", "source_destination"] as const;
const fieldLabel = (field: string) => t(`accounts.field.${field}` as MessageKey);
function accountFailure(error: unknown) {
  if (error instanceof ApiRequestError) {
    if (error.status === 401) return t("accounts.confirmPasswordFailed");
    if (error.status === 403) return t("accounts.actionDenied");
    if (error.status === 409) return t(error.problem?.code === "account_username_exists" ? "accounts.usernameExists" : "accounts.changedElsewhere");
    if (error.status === 429) return t("accounts.rateLimited");
    if (error.status === 400) return t("accounts.invalidAction");
  }
  return t("accounts.submissionUnknown");
}
function definiteRejection(error: unknown) { return error instanceof ApiRequestError && [400,401,403,409,429].includes(error.status); }

export function AccountPasswordPage() {
  useLocale();
  const [token, setToken] = useState(() => window.location.hash.slice(1));
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [busy, setBusy] = useState(false);
  const [completedUsername, setCompletedUsername] = useState("");
  const [signingIn, setSigningIn] = useState(false);
  const [passwordUncertain,setPasswordUncertain]=useState(false);
  const [current, setCurrent] = useState<AccessSessionResponse | null>(null);
  const [currentKnown, setCurrentKnown] = useState(false);
  const currentRead=useRef(0);
  const [message, setMessage] = useMessageState("");
  async function readCurrent(signal?: AbortSignal) {
    const request=++currentRead.current;setCurrentKnown(false);
    try { const value=await accessAPI.session(signal); if (!signal?.aborted&&request===currentRead.current) { setCurrent(value); setCurrentKnown(true); } }
    catch (error) { if (signal?.aborted||request!==currentRead.current) return; if (error instanceof ApiRequestError && error.status===401) { setCurrent(null); setCurrentKnown(true); } else setMessage(() => t("accounts.sessionReadFailed")); }
  }
  useEffect(() => { window.history.replaceState(null, "", "/access/activate"); const controller=new AbortController(); void readCurrent(controller.signal); return()=>controller.abort(); }, []);
  async function submit(event: FormEvent) {
    event.preventDefault(); setMessage("");
    if (password!==confirmation) { setMessage(() => t("accounts.passwordMismatch")); return; }
    if ([...password].length<12 || new TextEncoder().encode(password).byteLength>1024) {setMessage(()=>t("accounts.passwordRules"));return;}
    setBusy(true);
    try {
      const result=await managedAccountsAPI.setPassword(token, password, confirmation);
      if (typeof result.username!=="string" || !result.username || result.status!=="password_set_sign_in_required") throw new ApiResponseError("account-password","invalid completion receipt");
      setToken(""); setCompletedUsername(result.username); announceSessionChange();
      // A committed password change stays successful even if session readback fails.
      await readCurrent();
    } catch (error) { if(!definiteRejection(error)){setPasswordUncertain(true);setToken("");}else if(error instanceof ApiRequestError&&error.status===400)setToken(""); setMessage(() => error instanceof ApiRequestError && error.status===400 ? t("accounts.linkInvalid") : definiteRejection(error) ? accountFailure(error) : t("accounts.passwordOutcomeUnknown")); }
    finally { setPassword(""); setConfirmation(""); setBusy(false); }
  }
  async function continueSignIn() {
    if (!currentKnown) return;
    if (current?.session.username===completedUsername) { window.location.assign("/"); return; }
    setBusy(true); setMessage("");
    try { if (current) { try { await accessAPI.logout(current.csrf_token); } catch(error) { if (!(error instanceof ApiRequestError && error.status===401)) throw error; } announceSessionChange(); } setCurrent(null); setSigningIn(true); }
    catch { setCurrentKnown(false); setMessage(() => t("accounts.sessionReadFailed")); }
    finally { setBusy(false); }
  }
  if (signingIn) return <LoginPage initialUsername={completedUsername} onAuthenticated={()=>{announceSessionChange();window.location.assign("/");}}/>;
  return <main className="login-shell" id="main-content"><section className="login-card">
    <div className="login-brand" aria-hidden="true">XI</div>
    <h1>{tx("accounts.setPassword")}</h1><p>{tx("accounts.holderSetsPassword")}</p>
    {currentKnown && current ? <p aria-label={t("session.current")}>{tx("accounts.currentlySignedIn",{value0:current.session.username??current.session.subject_id,value1:roleLabel(current.session.role)})}</p> : null}
    {completedUsername ? <><p role="status">{tx("accounts.passwordSet")}</p><p>{tx("accounts.passwordSetFor",{value0:completedUsername})}</p><p>{tx("accounts.setupDoesNotSwitch")}</p>
      <button className="primary-button" disabled={busy||!currentKnown} onClick={()=>void continueSignIn()}>{current ? current.session.username===completedUsername ? t("accounts.continueCurrent",{value0:completedUsername}) : t("accounts.switchAndSignIn",{value0:current.session.username??current.session.subject_id,value1:completedUsername}) : t("accounts.signInAs",{value0:completedUsername})}</button>
      {!currentKnown ? <button disabled={busy} onClick={()=>void readCurrent()}>{tx("accounts.checkCurrentSession")}</button> : null}
    </> : <form onSubmit={submit}>
      {!token && !passwordUncertain && <p role="alert">{tx("accounts.missingLink")}</p>}
      {window.location.protocol!=="https:" && <p role="alert">{tx("m0000")}</p>}
      <label>{tx("accounts.newPassword")}<input type="password" autoComplete="new-password" minLength={12} maxLength={1024} required disabled={busy||!token||passwordUncertain||location.protocol!=="https:"} value={password} onChange={e=>setPassword(e.target.value)}/></label>
      <label>{tx("accounts.confirmPassword")}<input type="password" autoComplete="new-password" minLength={12} maxLength={1024} required disabled={busy||!token||passwordUncertain||location.protocol!=="https:"} value={confirmation} onChange={e=>setConfirmation(e.target.value)}/></label>
      <button disabled={busy||!token||passwordUncertain||location.protocol!=="https:"}>{tx("accounts.savePassword")}</button>
    </form>}
    {passwordUncertain ? <><button className="primary-button" disabled={busy||!currentKnown} onClick={()=>void continueSignIn()}>{tx("accounts.normalSignIn")}</button>{!currentKnown?<button onClick={()=>void readCurrent()}>{tx("accounts.checkCurrentSession")}</button>:null}</> : null}
    {message && <p className="feedback feedback-error" role="alert">{message}</p>}
  </section></main>;
}

type AccountAction = {action:"invite"|"recover"|"enable"|"disable";account?:ManagedAccount;username:string;role:string};
type AccountResult = {action:string;account:ManagedAccount;link:string;expires:string};
export function AccountsPage({ session }: { session: AccessSessionResponse }) {
  useLocale();
  const [items,setItems]=useState<ManagedAccount[]>([]);
  const [options,setOptions]=useState<ProjectReadOptions>({sources:[],projects:[],grants:[]});
  const [username,setUsername]=useState("");
  const [role,setRole]=useState("workbench_investigation_reader");
  const [password,setPassword]=useState("");
  const [actionPassword,setActionPassword]=useState("");
  const [pending,setPending]=useState<AccountAction>();
  const [result,setResult]=useState<AccountResult>();
  const [uncertain,setUncertain]=useState(false);
  const [refreshFailed,setRefreshFailed]=useState(false);
  const [loaded,setLoaded]=useState(false);
  const [busy,setBusy]=useState(false);
  const [message,setMessage]=useMessageState("");
  const [subject,setSubject]=useState(session.session.subject_id);
  const [source,setSource]=useState("");
  const [project,setProject]=useState("");
  const [capabilities,setCapabilities]=useState<string[]>(["details","coverage"]);
  const [preview,setPreview]=useState<ProjectReadCommand>();
  const sourceLabel=(id:string)=>{const item=options.sources.find(s=>s.id===id);return item?options.sources.filter(s=>s.name===item.name).length>1?`${item.name} · ${item.id}`:item.name:id;};
  const existing=options.grants.find(g=>g.subject_id===subject&&g.source_system_id===source&&g.source_project_ref===project&&g.cost_project_ref===null);
  const grantID=useMemo(()=>existing?.id??crypto.randomUUID(),[existing?.id,subject,source,project]);
  const duplicate=items.find(account=>account.username===username);
  async function reload(signal?:AbortSignal) { const [accounts,scopes]=await Promise.all([managedAccountsAPI.list(signal),managedAccountsAPI.grantOptions(signal)]);if(signal?.aborted)return;setItems(accounts.items);setOptions(scopes);setRefreshFailed(false);setLoaded(true); }
  useEffect(()=>{const controller=new AbortController();void reload(controller.signal).catch(e=>{if(e?.name!=="AbortError"){setRefreshFailed(true);setMessage(()=>t("accounts.unavailable"));}});return()=>controller.abort();},[]);
  useEffect(()=>{setPreview(undefined);setCapabilities(existing?fields.filter(field=>existing[`allow_${field}`]===true):["details","coverage"]);},[subject,source,project,existing?.revision]);
  function review(action:AccountAction) { setPending(action);setActionPassword("");setResult(undefined);setMessage(""); }
  async function refreshOnly() { setBusy(true);try {await reload();setUncertain(false);setPending(undefined);setPreview(undefined);setMessage(()=>t("accounts.readbackRefreshed"));}catch{setRefreshFailed(true);setMessage(()=>t("accounts.readbackFailed"));}finally{setBusy(false);} }
  async function refreshAfterCommit() { try {await reload();}catch{setRefreshFailed(true);setMessage(()=>t("accounts.committedReadbackFailed"));} }
  async function change(event:FormEvent) {
    event.preventDefault();if(!pending||uncertain)return;const proposed=pending;
    setBusy(true);setMessage("");
    try {
      const response=await managedAccountsAPI.change({action:proposed.action,subject_id:proposed.account?.subject_id??"",expected_revision:proposed.account?.revision??0,...(proposed.action==="invite"?{username:proposed.username,role:proposed.role}:{}),password:actionPassword},session.csrf_token);
      if(!response.account||response.account.username!==proposed.username||(proposed.account&&response.account.subject_id!==proposed.account.subject_id))throw new ApiResponseError("accounts","unexpected account receipt");
      setResult({action:proposed.action,account:response.account,link:response.activation_link??"",expires:response.expires_at??""});setPending(undefined);setUsername("");setMessage(()=>t("accounts.changed"));announceSessionChange();
      await refreshAfterCommit();
    } catch(error) {setPending(undefined);if(!definiteRejection(error))setUncertain(true);setMessage(()=>accountFailure(error));}
    finally {setActionPassword("");setBusy(false);}
  }
  function command(revoke:boolean):ProjectReadCommand{return{grant_id:grantID,subject_id:subject,source_system_id:source,source_project_ref:project,expected_revision:existing?.revision??0,capabilities,revoke,check_only:true};}
  async function checkGrant(revoke:boolean) {setBusy(true);setMessage("");setPreview(undefined);const proposed=command(revoke);try{await managedAccountsAPI.grant(proposed,"",session.csrf_token);setPreview(proposed);}catch{setMessage(()=>t("accounts.previewFailed"));}finally{setBusy(false);}}
  async function applyGrant() {if(!preview||uncertain)return;setBusy(true);setMessage("");try{await managedAccountsAPI.grant({...preview,check_only:false},password,session.csrf_token);setPreview(undefined);setMessage(()=>t("accounts.grantSaved"));announceSessionChange();await refreshAfterCommit();}catch(error){setPreview(undefined);if(!definiteRejection(error))setUncertain(true);setMessage(()=>accountFailure(error));}finally{setPassword("");setBusy(false);}}
  const blocked=busy||uncertain||refreshFailed||!loaded;
  return <>
    <header className="topbar"><div><p className="eyebrow">{tx("accounts.management")}</p><h1>{tx("accounts.title")}</h1><p className="page-intro">{tx("accounts.intro")}</p></div></header>
    {message&&<p className="feedback" role={uncertain||refreshFailed?"alert":"status"}>{message}</p>}
    {(uncertain||refreshFailed)&&<button disabled={busy} onClick={()=>void refreshOnly()}>{tx("accounts.refreshState")}</button>}
    <section className="access-session-panel"><h2>{tx("accounts.inviteNew")}</h2><p>{tx("accounts.inviteBoundary")}</p><form onSubmit={e=>{e.preventDefault();if(!duplicate)review({action:"invite",username,role});}}>
      <label>{tx("m0014")}<input required minLength={3} maxLength={64} pattern={"[a-z0-9][a-z0-9._\\-]{2,63}"} value={username} disabled={blocked} onChange={e=>{setUsername(e.target.value);setPending(undefined);setActionPassword("");}}/></label>
      <label>{tx("accounts.role")}<select value={role} disabled={blocked} onChange={e=>{setRole(e.target.value);setPending(undefined);setActionPassword("");}}><option value="workbench_investigation_reader">{t("accounts.reader")}</option><option value="workbench_investigation_reviewer">{t("accounts.reviewer")}</option></select></label>
      {duplicate&&<p role="alert">{tx("accounts.usernameExists")}</p>}
      <button disabled={blocked||!!duplicate}>{tx("accounts.reviewInvite")}</button>
    </form></section>
    <section className="access-session-panel"><h2>{tx("accounts.existing")}</h2><p>{tx("accounts.recoveryBoundary")}</p><ul className="access-session-list">{items.map(account=><li key={account.subject_id}><div><strong>{account.username}</strong><span>{account.role==="platform_admin"?t("accounts.owner"):account.role==="workbench_investigation_reviewer"?t("accounts.reviewer"):t("accounts.reader")}</span><span>{account.disabled?t("accounts.disabled"):account.needs_activation?t("accounts.activationRequired"):t("accounts.active")}</span></div>
      {["workbench_investigation_reader","workbench_investigation_reviewer"].includes(account.role)&&<div>{!account.disabled&&<button disabled={blocked} onClick={()=>review({action:"recover",account,username:account.username,role:account.role})}>{tx("accounts.recover")}</button>}<button disabled={blocked} onClick={()=>review({action:account.disabled?"enable":"disable",account,username:account.username,role:account.role})}>{account.disabled?tx("accounts.enable"):tx("accounts.disable")}</button></div>}</li>)}</ul></section>
    {pending&&<section className="access-session-panel" aria-label={t("accounts.confirmAction")}><h2>{tx("accounts.confirmAction")}</h2><p>{tx(pending.action==="invite"?"accounts.inviteTarget":pending.action==="recover"?"accounts.recoverTarget":pending.action==="disable"?"accounts.disableTarget":"accounts.enableTarget",{value0:pending.username})}</p><p>{tx("accounts.targetSessionsOnly")}</p><form onSubmit={e=>void change(e)}>
      <label>{tx("accounts.actionPassword")}<input autoFocus type="password" autoComplete="current-password" required maxLength={1024} value={actionPassword} disabled={busy||uncertain} onChange={e=>setActionPassword(e.target.value)}/></label>
      <button disabled={blocked||!actionPassword}>{tx("accounts.confirmAction")}</button><button type="button" disabled={busy} onClick={()=>{setPending(undefined);setActionPassword("");}}>{tx("accounts.cancelAction")}</button>
    </form></section>}
    {result&&<section className="access-session-panel" aria-label={t("accounts.actionResult")}><h2>{tx("accounts.actionResult")}</h2><p>{tx("accounts.resultAccount",{value0:result.account.username})}</p><p role="status">{result.account.disabled?t("accounts.disabled"):result.account.needs_activation?t("accounts.awaitingHolder"):t("accounts.active")}</p>
      {result.link&&<><h3>{tx(result.action==="recover"?"accounts.recoveryLink":"accounts.activationLink")}</h3><p>{tx("accounts.shareSecurely")}</p><p>{tx("accounts.setupDoesNotSwitch")}</p><label>{tx("accounts.link")}<input readOnly value={result.link} onFocus={e=>e.target.select()}/></label><p>{tx("accounts.expires",{value0:new Date(result.expires).toLocaleString()})}</p><button onClick={()=>setResult({...result,link:""})}>{tx("accounts.hideLink")}</button></>}
    </section>}
    <section className="access-session-panel"><h2>{tx("accounts.confirmIdentity")}</h2><p>{tx("accounts.grantPasswordBoundary")}</p><label>{tx("accounts.ownerPassword")}<input type="password" autoComplete="current-password" maxLength={1024} value={password} disabled={blocked} onChange={e=>setPassword(e.target.value)}/></label></section>
    <section className="access-session-panel"><h2>{tx("accounts.projectReading")}</h2><p>{tx("accounts.explicitScope")}</p>
      <fieldset className="account-grant-scope" disabled={blocked}><label>{tx("accounts.account")}<select value={subject} onChange={e => setSubject(e.target.value)}>{items.filter(a => !a.disabled).map(a => <option key={a.subject_id} value={a.subject_id}>{a.username}</option>)}</select></label>
      <label>{tx("accounts.source")}<select value={source} onChange={e => { setSource(e.target.value); setProject(""); }}><option value="">{t("accounts.chooseSource")}</option>{options.sources.map(s => <option key={s.id} value={s.id}>{sourceLabel(s.id)}</option>)}</select></label>
      <label>{tx("accounts.project")}<select value={project} onChange={e => setProject(e.target.value)}><option value="">{t("accounts.chooseProject")}</option>{options.projects.filter(p => p.source_system_id === source).map(p => <option key={p.source_project_ref} value={p.source_project_ref}>{p.source_project_ref}</option>)}</select></label>
      <div className="account-grant-fields">{fields.map(field => <label key={field}><input type="checkbox" checked={capabilities.includes(field)} disabled={field === "details" || field === "coverage"} onChange={e => { setPreview(undefined); setCapabilities(v => e.target.checked ? [...v, field] : v.filter(f => f !== field)); }} />{fieldLabel(field)}</label>)}</div>
      <p>{tx("accounts.bodyDenied")}</p><button disabled={!source || !project} onClick={() => void checkGrant(false)}>{tx("accounts.preview")}</button>{existing && <button onClick={() => void checkGrant(true)}>{tx("accounts.previewRevoke")}</button>}
      </fieldset>
      {preview && <section aria-label={t("accounts.preview")}><h3>{preview.revoke ? tx("accounts.revokePreview") : tx("accounts.readPreview")}</h3><p>{items.find(a => a.subject_id === preview.subject_id)?.username} · {sourceLabel(preview.source_system_id)} · {preview.source_project_ref}</p><ul>{preview.capabilities.map(f => <li key={f}>{fieldLabel(f)}</li>)}</ul><p>{tx("accounts.bodyDenied")}</p><button disabled={blocked || !password} onClick={() => void applyGrant()}>{tx("accounts.confirmScope")}</button></section>}
    </section>
  </>;
}
