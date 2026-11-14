import { useEffect, useRef, useState } from "react";
import { ApiRequestError, connectorSetupAPI, connectorSyncAPI, isAbortError, type ConnectorSyncStatus } from "./api";
import { t, tx, type MessageKey, useLocale } from "./i18n";
import { formatDateTime } from "./format";
import { ErrorState } from "./Feedback";

const effectiveKeys: Record<string, MessageKey> = {
 quiet: "syncQuiet", stopping: "syncStopping", unknown: "syncUnknown",
 auto_running: "syncAuto", manual_running: "syncManual", blocked: "syncBlocked",
};
const stageKeys:Record<string,MessageKey> = {
 inspect:"syncStageInspect", original_activation:"syncStageActivation", activation_epoch:"syncStageActivation", activation_claim:"syncStageActivation", activation_request:"syncStageActivation", activation_commit:"syncStageActivation",
 receiver_proof:"syncStageReceiver", receiver_binding:"syncStageReceiver", receiver_state:"syncStageReceiver", feed_stage:"syncStageReceiver", feed_credential:"syncStageReceiver", feed_register:"syncStageReceiver", feed_promote:"syncStageReceiver", feed_activate:"syncStageReceiver",
 feed_activation:"syncStageFeedActivation", feed_poll:"syncStageFeedPoll",
 source_descriptor:"syncStageDescriptor", descriptor_commit:"syncStageDescriptor",
 machine_prepare:"syncStageMachine", machine_request:"syncStageMachine", machine_enqueue:"syncStageMachine", machine_config:"syncStageMachine", machine_runtime:"syncStageMachine",
 policy_scopes:"syncStagePolicy", enrollment_client:"syncStageEnrollment", enrollment_list:"syncStageEnrollment",
 project_read:"syncStageProject", project_revoke:"syncStageProject", project_qualify:"syncStageProject", project_configure:"syncStageProject", enrollment_save:"syncStageProject", project_publish:"syncStageProject", configuration_encode:"syncStageProject", unknown:"syncStageUnknown",
};
const failureKeys:Record<string,MessageKey> = {
 authorization:"syncFailureAuthorization", unavailable:"syncFailureUnavailable", contract:"syncFailureContract", conflict:"syncFailureConflict", not_found:"syncFailureNotFound",
 unconfirmed:"syncFailureUnconfirmed", canceled:"syncFailureCanceled", deadline:"syncFailureDeadline", local_permission:"syncFailurePermission", local_store:"syncFailureStore", host_busy:"syncFailureBusy", unknown:"syncFailureUnknown",
};
const runKeys:Record<string,MessageKey>={running:"syncRunRunning",completed:"syncRunCompleted",empty:"syncRunEmpty",partial:"syncRunPartial",error:"syncRunError",expired:"syncRunExpired",interrupted:"syncRunInterrupted",cancelled:"syncRunCancelled"};
function SyncFailure({code}:{code:string}) {
 const parts=code.split(":");const [stage,category]=parts;
 const recognized=parts.length===2&&Object.hasOwn(stageKeys,stage)&&Object.hasOwn(failureKeys,category);
 const known=recognized||["sync_failed","owner_exited","multiple:failures"].includes(code);
 const text=code==="multiple:failures"?t("syncFailureMultiple"):code==="owner_exited"?t("syncFailureOwnerExited"):recognized?t("syncFailureAt",{value0:t(stageKeys[stage]),value1:t(failureKeys[category])}):t("syncFailureUnknown");
 return <div><p>{text}</p>{known&&<details><summary>{tx("syncDiagnosticDetails")}</summary><code>{code}</code></details>}</div>;
}
type Pending={action:"once"|"set";body:{expected_revision:number;request_id:string;auto_sync:boolean;cadence_seconds:number}};
const storageKey=(id:string)=>`xinsight.sync.pending.${id}`;
const settling=(s:ConnectorSyncStatus)=>s.effective==="stopping"||s.run?.state==="running";
export function ConnectorSyncPanel({installationID,csrfToken,canManage}:{installationID:string;csrfToken:string;canManage:boolean}) {
 useLocale();
 const [status,setStatus]=useState<ConnectorSyncStatus>();
 const [connection,setConnection]=useState<{id:string;issues:string[]}>();
 const [cadence,setCadence]=useState("60");
 const [error,setError]=useState<unknown>();
 const [busy,setBusy]=useState(false);
 const [polling,setPolling]=useState(false);
 const [pending,setPending]=useState<Pending>();
 const pendingRef=useRef<Pending>(undefined);
 const generation=useRef(0);
 const currentID=useRef(installationID);currentID.current=installationID;
 function forget(id:string){try{sessionStorage.removeItem(storageKey(id));}catch{/* A confirmed server receipt remains authoritative. */}
  if(currentID.current===id){pendingRef.current=undefined;setPending(undefined);}}
 function accept(id:string,g:number,next:ConnectorSyncStatus){
  if(currentID.current!==id||generation.current!==g||next.installation_id!==id)return;
  setStatus(next);setPolling(settling(next));
  const prior=pendingRef.current;
  if(prior&&(next.last_request_id===prior.body.request_id||next.run?.request_id===prior.body.request_id))forget(id);
 }
 async function readConnection(id:string,g:number,signal?:AbortSignal){
  try{const next=await connectorSetupAPI.status(id,signal);
   if(!signal?.aborted&&currentID.current===id&&generation.current===g&&next.installation_id===id)setConnection({id,issues:next.issues.map(i=>i.code)});
  }catch{if(!signal?.aborted&&currentID.current===id&&generation.current===g)setConnection(undefined);}
 }
 useEffect(()=>{
  const g=++generation.current;const id=installationID;
  setConnection(undefined);setStatus(undefined);setError(undefined);setBusy(false);setPolling(false);setCadence("60");pendingRef.current=undefined;setPending(undefined);
  try{const raw=sessionStorage.getItem(storageKey(id));if(raw){const saved=JSON.parse(raw) as Pending;
   if((saved.action==="once"||saved.action==="set")&&typeof saved.body?.request_id==="string"&&Number.isInteger(saved.body.expected_revision)){pendingRef.current=saved;setPending(saved);}}}catch{setError(new Error(t("syncPendingStorage")));}
  if(!canManage)return;
  const controller=new AbortController();
  void readConnection(id,g,controller.signal);
  connectorSyncAPI.status(id,controller.signal).then(next=>{if(controller.signal.aborted)return;accept(id,g,next);if(currentID.current===id&&generation.current===g)setCadence(String(next.cadence_seconds??60));})
   .catch(e=>{if(!controller.signal.aborted&&currentID.current===id&&generation.current===g&&!isAbortError(e))setError(e);});
  return ()=>{controller.abort();generation.current++;};
 },[installationID,canManage]);
 useEffect(()=>{
  if(!polling)return;
  const id=installationID,g=generation.current,controller=new AbortController();
  const timer=window.setTimeout(()=>{connectorSyncAPI.status(id,controller.signal).then(next=>{if(!controller.signal.aborted)accept(id,g,next);})
   .catch(e=>{if(!controller.signal.aborted&&currentID.current===id&&generation.current===g&&!isAbortError(e)){setError(e);setPolling(false);}});},1500);
  return ()=>{window.clearTimeout(timer);controller.abort();};
 },[installationID,polling,status]);
 async function refresh(){const id=installationID,g=generation.current;void readConnection(id,g);try{accept(id,g,await connectorSyncAPI.status(id));}catch(e){if(currentID.current===id&&generation.current===g)setError(e);}}
 async function act(action:"once"|"set",auto=false){
  if(!status||status.installation_id!==installationID||busy)return;
  const id=installationID,g=generation.current;setBusy(true);setError(undefined);
  try{
   let mutation=pendingRef.current;
   if(!mutation){const seconds=Number(cadence);if(!Number.isInteger(seconds)||seconds<5||seconds>3600)throw new Error(t("syncCadenceInvalid"));
    mutation={action,body:{expected_revision:status.revision,request_id:crypto.randomUUID(),auto_sync:auto,cadence_seconds:seconds}};
    // Persist before sending. A lost response/reload must not create another UUID.
    try{sessionStorage.setItem(storageKey(id),JSON.stringify(mutation));}catch{throw new Error(t("syncPendingStorage"));}
    pendingRef.current=mutation;setPending(mutation);
   }
   const next=mutation.action==="once"?await connectorSyncAPI.once(id,mutation.body,csrfToken):await connectorSyncAPI.set(id,mutation.body,csrfToken);
   if(currentID.current===id&&generation.current===g){forget(id);accept(id,g,next);}
  }catch(e){if(currentID.current===id&&generation.current===g){setError(e);
   if(e instanceof ApiRequestError&&e.status===400){forget(id)}
   if(e instanceof ApiRequestError&&e.status===409){
    try{const next=await connectorSyncAPI.status(id);if(currentID.current===id&&generation.current===g){const old=pendingRef.current;
     // CAS makes this original body impossible to apply again. Refresh only;
     // never turn a conflict into a new sync in the same click.
     if(old&&next.revision>old.body.expected_revision)forget(id);accept(id,g,next);}}catch{/* Keep the original request until its outcome can be read. */}
   }
  }}finally{if(currentID.current===id&&generation.current===g)setBusy(false);}
 }
  if (!canManage) return null;
  const currentFailures=status?.workers.filter(w=>w.revision===status.revision&&w.last_error)??[];
  return <section aria-label={t("syncTitle")} className="connector-sync-panel">
    <h3>{tx("syncTitle")}</h3>
    {error ? <ErrorState error={error} /> : null}
    {pending ? <p>{tx("syncPending")} <button className="button" disabled={busy} onClick={() => void act(pending.action)}>{tx("syncConfirmPending")}</button></p> : null}
    {status?.installation_id === installationID ? <>
      <label><input type="checkbox" checked={status.auto_sync} disabled={busy || !!pending || status.run?.state === "running" && !status.auto_sync}
        onChange={e => void act("set", e.target.checked)} />{tx("syncToggle")}</label>
      <label>{tx("syncCadence")} <input type="number" min={5} max={3600} step={1} value={cadence}
        onChange={e => setCadence(e.target.value)} disabled={busy} /></label>
      <button className="button" disabled={busy || !!pending} onClick={() => void act("set", status.auto_sync)}>{tx("syncSaveCadence")}</button>
      <p>{tx("syncCadenceHelp")}</p>
      {status.cadence_seconds === null ? <p>{tx("syncExistingCadence")}</p> : null}
      <p role="status">{tx("syncEffective", { value0: t(effectiveKeys[status.effective] ?? "syncUnknown") })}</p>
      <div role="note">
        <p>{tx("syncCredentialBoundary")}</p>
        {connection?.id===installationID&&connection.issues.some(i=>["control_identity_expired","control_credential_expired"].includes(i))?<p role="status">{tx("syncCredentialExpired")}</p>:connection?.id!==installationID?<p>{tx("syncCredentialUnchecked")}</p>:null}
        <a href={`/connectors/unirouter/setup?installation=${encodeURIComponent(installationID)}`}>{tx("syncConnectionStatus")}</a>
      </div>
      <button className="button" disabled={busy || !!pending || status.auto_sync || status.run?.state === "running" || status.workers.some(w => w.busy)} onClick={() => void act("once")}>{tx("syncOnce")}</button>
      <button className="button" disabled={busy || !!pending} onClick={() => void act("set", false)}>{tx("syncStop")}</button>
      <button className="button" disabled={busy} onClick={() => void refresh()}>{tx("syncRefresh")}</button>
      <p>{tx("syncBounds")}</p>
      <p>{tx("syncLastCheck", { value0: status.last_checked_at ? formatDateTime(status.last_checked_at) : t("syncNever") })}</p>
      <p>{tx("syncLastData", { value0: status.last_data_at ? formatDateTime(status.last_data_at) : t("syncNever") })}</p>
      {status.last_error && !currentFailures.some(w=>w.last_error===status.last_error) ? <div><p>{tx("syncUnboundLastError")}</p><SyncFailure code={status.last_error}/></div> : null}
      {currentFailures.map(w=><section key={w.worker}><h4>{t("syncWorkerFailure",{value0:t(({activation:"syncWorkerActivation",projects:"syncWorkerProjects",source:"syncWorkerSource",feed:"syncWorkerFeed",policy:"syncWorkerPolicy",credentials:"syncWorkerCredentials"} as Record<string,MessageKey>)[w.worker]??"syncWorkerUnknown")})}</h4><SyncFailure code={w.last_error}/></section>)}
      {status.run ? <p>{tx("syncResult", { value0: t(runKeys[status.run.state]??"syncUnknown"), value1: status.run.pages, value2: status.run.attempts })}</p> : null}
      <p>{tx("syncPeer")}</p><p>{tx("syncGap")}</p>
    </> : <p>{tx("syncUnknown")}</p>}
  </section>;
}
