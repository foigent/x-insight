import { localizedLabels, t, tx, useLocale, useMessageState } from "./i18n";
import { PageHeader } from "./PageHeader";
import { formatDateTime } from "./format";
import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import {ApiRequestError,connectorSetupAPI,isAbortError,type AccessSessionResponse,type ConnectorInvitation} from "./api";
import type {ReviewCaptureSettings} from "./api";
import { ErrorState, LoadingBlock } from "./Feedback";
import { canRetryRead, isAccessDenied } from "./readFailures";
import { useConnectorRead } from "./useConnectorRead";
type Custody={desired_revision:number;effective_revision:number;settings:ReviewCaptureSettings|null};
export type SetupPreview={schema_version:"xinsight.connector-setup-preview.v1";deployment_id:string;ur_origin:string;custody:Custody;can_change_custody:boolean};
export type SetupStatus={schema_version:"xinsight.connector-setup-status.v1";installation_id:string;deployment_id:string;consent_revision:number;state:"waiting_consent"|"provisioning"|"needs_action"|"waiting_data"|"ready"|"failed";custody:Custody&{loaded:boolean};receiving:{last_success_at:string|null;profiles_received:string[];content_received_at:string|null;content_read_verified_at:string|null};issues:{code:string;requires_choice:boolean}[]};
export type SetupCreated={invitation:ConnectorInvitation;consent:{schema_version:"xinsight.connector-setup-consent-view.v1";installation_id:string;deployment_id:string;xi_origin:string;ur_origin:string;consent_revision:number;consent_sha256:string;expires_at:string;custody:ReviewCaptureSettings;expected_custody_revision:number}};
const states:Record<SetupStatus["state"],string>=localizedLabels({}, {waiting_consent: "m0280", provisioning: "m0281", needs_action: "m0282", waiting_data: "m0283", ready: "m0284", failed: "m0285"});
const issues:Record<string,string>=localizedLabels({}, {invitation_expired: "m0286", installation_inactive: "m0287", control_identity_expired: "m0288", control_identity_revoked: "m0289", control_identity_unavailable: "m0290", control_credential_expired: "m0291", control_credential_revoked: "m0292", control_credential_pending: "m0293", control_credential_unavailable: "m0294", xi_actor_permission_changed: "m0295", custody_revision_changed: "m0296", current_custody_pending: "m0297", custody_unavailable: "m0298", custody_loading: "m0299"});
const defaults:ReviewCaptureSettings={mode:"full_content",retention_seconds:90*86400,project_quota_bytes:5*1073741824,max_item_bytes:33554432};
const validInstallation = (id: string) => /^xci_[A-Za-z0-9_-]{22}$/.test(id);
const pendingProgress = (status: SetupStatus) => ["waiting_consent", "provisioning", "waiting_data"].includes(status.state);
const stamp=(value:string|null)=>value?formatDateTime(value):t("m0300");
function failure(error:unknown){if(error instanceof ApiRequestError){if(error.status===409)return t("m0301");if(error.status===410)return t("m0286");if(error.status===403)return t("m0302");}return t("m0303");}
type SetupPut=Parameters<typeof connectorSetupAPI.create>[0];
 type StoredSetup={subject:string;put:SetupPut;installation?:string};
 const setupStorageKey=(subject:string)=>`xinsight:connector-setup:${subject}`;
 function readStoredSetup(subject:string):StoredSetup|undefined{try{const r=JSON.parse(sessionStorage.getItem(setupStorageKey(subject))??"null") as StoredSetup|null;const p=r?.put,c=p?.custody;return r&&r.subject===subject&&p?.schema_version==="xinsight.connector-setup-consent.v1"&&/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(p.request_id)&&Number.isSafeInteger(p.expected_custody_revision)&&p.expected_custody_revision>=0&&(c===null||c&&["disabled","full_content"].includes(c.mode)&&[c.retention_seconds,c.project_quota_bytes,c.max_item_bytes].every(Number.isSafeInteger)&&c.retention_seconds>=1&&c.retention_seconds<=31536000&&c.project_quota_bytes>=1&&c.project_quota_bytes<=2**40&&c.max_item_bytes>=0&&c.max_item_bytes<=33554432)&&(!r.installation||/^xci_[A-Za-z0-9_-]{22}$/.test(r.installation))?{subject,put:{schema_version:p.schema_version,request_id:p.request_id,expected_custody_revision:p.expected_custody_revision,custody:c},installation:r.installation}:undefined}catch{return undefined}}
 function persistSetup(saved:StoredSetup){sessionStorage.setItem(setupStorageKey(saved.subject),JSON.stringify(saved))}
export function ConnectorSetupPage({session}:{session:AccessSessionResponse}){
  useLocale();
  const subject = session.session.subject_id;
  const [entry] = useState(() => {
    const explicit = new URLSearchParams(location.search).get("installation") ?? "";
    const requested = validInstallation(explicit) ? explicit : "";
    const stored = readStoredSetup(subject);
    const saved = !requested || stored?.installation === requested ? stored : undefined;
    return { saved, installation: requested || saved?.installation || "" };
  });
  // The stored request is an already confirmed, non-secret idempotency body.
  const resume = useRef(entry.saved);
  const [pendingPut, setPendingPut] = useState<SetupPut | undefined>(entry.saved?.put);
  const [draft, setDraft] = useState<{ settings: ReviewCaptureSettings; reuse: boolean }>();
  const [requestID, setRequestID] = useState(() => entry.saved?.put.request_id ?? crypto.randomUUID());
  const [installation, setInstallation] = useState(entry.installation);
  const [created, setCreated] = useState<SetupCreated>();
  const [recovery, setRecovery] = useState<{ recover_url: string; expires_at: string }>();
  const [needsNewConsent, setNeedsNewConsent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [writeDenied, setWriteDenied] = useState(false);
  const [error, setError] = useMessageState("");
  const generation = useRef(0);
  const previewRead = useConnectorRead(session.session.session_ref, connectorSetupAPI.preview);
  const loadStatus = useCallback(async (signal: AbortSignal) => {
    const next = await connectorSetupAPI.status(installation, signal);
    if (next.installation_id !== installation) throw new Error("Connection status belongs to another installation");
    return next;
  }, [installation]);
  const progress = useConnectorRead(installation || null, loadStatus, 5000, pendingProgress);
  const preview = previewRead.data;
  const status = progress.data;
  const mayManage = !!preview?.can_change_custody && !writeDenied && !isAccessDenied(progress.error);
  const settings = draft?.settings ?? pendingPut?.custody ?? preview?.custody.settings ?? defaults;
  const reuse = draft?.reuse ?? (pendingPut ? pendingPut.custody === null : !!preview?.custody.settings &&
    preview.custody.desired_revision === preview.custody.effective_revision);
  const awaitingConsent = status?.state === "waiting_consent" && !status.issues.some(issue => issue.code === "invitation_expired");
  const canRecover = mayManage && status?.issues.some(issue => ["control_identity_expired", "control_credential_expired"].includes(issue.code));

  useEffect(() => () => { generation.current++; }, [session.session.session_ref]);
  useEffect(() => {
    if (isAccessDenied(previewRead.error) || isAccessDenied(progress.error) || preview?.can_change_custody === false) {
      generation.current++;
      setCreated(undefined); setRecovery(undefined); setBusy(false);
    }
  }, [previewRead.error, progress.error, preview?.can_change_custody]);

  const submit = useCallback(async (put: SetupPut) => {
    const current = ++generation.current;
    setBusy(true); setError("");
    try {
      persistSetup({ subject, put, installation: installation || undefined });
      setPendingPut(put);
      const next = await connectorSetupAPI.create(put, session.csrf_token);
      if (current !== generation.current) return;
      const id = next.invitation.installation_id;
      if (id !== next.consent.installation_id || installation && id !== installation) throw new Error("Invitation belongs to another installation");
      persistSetup({ subject, put, installation: id });
      setCreated(next); setInstallation(id);
      history.replaceState(history.state, "", `/connectors/unirouter/setup?installation=${encodeURIComponent(id)}`);
    } catch (reason) {
      if (current !== generation.current || isAbortError(reason)) return;
      setError(() => failure(reason));
      if (reason instanceof ApiRequestError && [409, 410].includes(reason.status)) setNeedsNewConsent(true);
      if (isAccessDenied(reason)) {
        setWriteDenied(true); setCreated(undefined); setRecovery(undefined);
      }
    } finally {
      if (current === generation.current) setBusy(false);
    }
  }, [installation, session.csrf_token, setError, subject]);

  useEffect(() => {
    const prior = resume.current;
    if (!prior) return;
    if (previewRead.error || progress.error || preview?.can_change_custody === false) {
      resume.current = undefined;
      return;
    }
    if (!mayManage || previewRead.loading || prior.installation && (!status || progress.loading)) return;
    resume.current = undefined;
    if (!prior.installation || awaitingConsent) void submit(prior.put);
  }, [awaitingConsent, mayManage, preview?.can_change_custody, previewRead.error, previewRead.loading, progress.error, progress.loading, status, submit]);

  function change(next: { settings: ReviewCaptureSettings; reuse: boolean }) {
    setDraft(next); setPendingPut(undefined); setRequestID(crypto.randomUUID());
  }
  function save(event: FormEvent) {
    event.preventDefault();
    if (!mayManage || !preview || previewRead.loading || busy || needsNewConsent) return;
    void submit(pendingPut ?? {
      schema_version: "xinsight.connector-setup-consent.v1", request_id: requestID,
      expected_custody_revision: preview.custody.desired_revision, custody: reuse ? null : settings,
    });
  }
  function renew() {
    sessionStorage.removeItem(setupStorageKey(subject));
    setCreated(undefined); setRecovery(undefined); setPendingPut(undefined); setDraft(undefined);
    setNeedsNewConsent(false); setInstallation(""); setRequestID(crypto.randomUUID());
    history.replaceState(history.state, "", "/connectors/unirouter/setup"); setError("");
    void previewRead.refresh();
  }
  async function issueRecovery() {
    if (!canRecover || !installation || busy || progress.loading) return;
    const current = ++generation.current;
    setBusy(true); setError("");
    const key = `xinsight:connector-recovery-request:${subject}:${installation}`;
    try {
      if (recovery && Date.parse(recovery.expires_at) <= Date.now()) {
        sessionStorage.removeItem(key);
        setRecovery(undefined);
      }
      const existing = sessionStorage.getItem(key);
      const id = existing && /^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(existing) ? existing : crypto.randomUUID();
      sessionStorage.setItem(key, id);
      const material = await connectorSetupAPI.recover(installation, id, session.csrf_token);
      if (current !== generation.current) return;
      if (material.installation_id !== installation || !material.recover_url.startsWith(`${preview?.ur_origin}/connectors/x-insight/recover#`)) {
        throw new Error("Recovery belongs to another installation");
      }
      setRecovery(material);
    } catch (reason) {
      if (current !== generation.current || isAbortError(reason)) return;
      if (reason instanceof ApiRequestError && reason.status === 410 && reason.problem?.code === "connector_credential_recovery_expired") {
        sessionStorage.removeItem(key); setRecovery(undefined); setError(() => t("m0304"));
      } else {
        setError(() => failure(reason));
        if (isAccessDenied(reason)) { setWriteDenied(true); setCreated(undefined); setRecovery(undefined); }
      }
    } finally {
      if (current === generation.current) setBusy(false);
    }
  }
 return <section className="product-page" aria-labelledby="setup-title"><PageHeader eyebrow={t("m0305")} title={t("m0306")} titleID="setup-title" description={t("m0307")}
   action={<a href="/connectors">{tx("m0308")}</a>} />{error?<p role="alert">{error}</p>:null}
 {previewRead.error ? <ErrorState error={previewRead.error} onRetry={canRetryRead(previewRead.error) ? () => void previewRead.refresh() : undefined}/> : null}
 {previewRead.loading && !preview ? <LoadingBlock label={t("m0323")}/> : null}
 {preview?<><p>{tx("m0309", { value0: preview.ur_origin })}</p>{!preview.can_change_custody?<p role="status">{tx("m0310")}</p>:!installation&&mayManage&&!previewRead.loading?<form onSubmit={event=>void save(event)}><h2>{tx("m0311")}</h2><p>{tx("m0312")}</p>{preview.custody.settings&&preview.custody.desired_revision===preview.custody.effective_revision?<label><input type="checkbox" checked={reuse} disabled={busy} onChange={event=>{change({settings,reuse:event.target.checked})}}/>{t("m0313")}</label>:null}
 <label>{t("m0314")}<select value={settings.mode} disabled={busy||reuse} onChange={event=>change({settings:{...settings,mode:event.target.value as ReviewCaptureSettings["mode"]},reuse})}><option value="disabled">{tx("m0315")}</option><option value="full_content">{tx("m0316")}</option></select></label>
 <label>{t("m0317")}<input type="number" required min={1/86400} max="365" step="any" value={settings.retention_seconds/86400} disabled={busy||reuse} onChange={event=>change({settings:{...settings,retention_seconds:Math.round(Number(event.target.value)*86400)},reuse})}/></label>
 <label>{t("m0318")}<input type="number" required min={1/1073741824} max="1024" step="any" value={settings.project_quota_bytes/1073741824} disabled={busy||reuse} onChange={event=>change({settings:{...settings,project_quota_bytes:Math.round(Number(event.target.value)*1073741824)},reuse})}/></label>
 <label>{t("m0319")}<input type="number" required min="0" max="32" step="any" value={settings.max_item_bytes/1048576} disabled={busy||reuse} onChange={event=>change({settings:{...settings,max_item_bytes:Math.round(Number(event.target.value)*1048576)},reuse})}/></label><button type="submit" disabled={busy||needsNewConsent}>{busy?t("m0320"):pendingPut?t("m0321"):t("m0322")}</button></form>:null}</>:null}
 {needsNewConsent&&mayManage?<button onClick={renew}>{tx("m0324")}</button>:null}
 {progress.error ? <ErrorState error={progress.error} onRetry={canRetryRead(progress.error) ? () => void progress.refresh() : undefined}/> : null}
 {progress.loading && !status ? <LoadingBlock label={t("connector.progressLoading")}/> : null}
 {mayManage && awaitingConsent && !created && pendingPut && !needsNewConsent ? <button disabled={busy || progress.loading} onClick={() => void submit(pendingPut)}>{tx("m0321")}</button> : null}
 {mayManage && awaitingConsent && created && status.consent_revision === created.consent.consent_revision ?<section aria-label={t("m0325")}><h2>{tx("m0326")}</h2><p>{tx("m0327", { value0: stamp(created.consent.expires_at) })}</p><a href={created.invitation.install_url} rel="noreferrer">{tx("m0328")}</a></section>:null}
 {status?<section aria-label={t("m0329")} aria-busy={progress.loading}><h2>{states[status.state]}</h2>{status.issues.map(issue=><p role="status" key={issue.code}>{issues[issue.code]??t("m0330")}</p>)}<p>{tx("m0333", { value0: status.custody.effective_revision>0&&status.custody.effective_revision===status.custody.desired_revision&&status.custody.loaded?t("m0331"):t("m0332") })}</p><p>{tx("m0334", { value0: stamp(status.receiving.last_success_at) })}</p><p>{tx("m0337", { value0: status.receiving.profiles_received.length?t("m0335", { value0: status.receiving.profiles_received.length }):t("m0336") })}</p><p>{tx("m0338", { value0: stamp(status.receiving.content_received_at) })}</p><details><summary>{tx("m0339")}</summary><p>{tx("m0340", { value0: status.installation_id })}</p><p>{tx("m0341", { value0: stamp(status.receiving.content_read_verified_at) })}</p><p>{tx("m0343", { value0: status.receiving.profiles_received.join("、")||t("m0342") })}</p><p>{tx("m0344", { value0: status.issues.map(i=>i.code).join("、")||t("m0342") })}</p></details>{mayManage&&status.issues.some(i=>i.code==="invitation_expired")?<button disabled={busy||progress.loading} onClick={renew}>{tx("m0345")}</button>:null}{canRecover?<><button disabled={busy||progress.loading} onClick={()=>void issueRecovery()}>{tx("m0346")}</button>{recovery?<p><a href={recovery.recover_url} rel="noreferrer">{tx("m0347")}</a>{t("m0348")}{stamp(recovery.expires_at)}）</p>:null}</>:null}<button disabled={busy||progress.loading} onClick={()=>void progress.refresh()}>{tx("m0349")}</button></section>:null}</section>;
}
