import { useEffect, useRef, useState } from "react";
import { ApiRequestError, connectorSyncAPI, isAbortError, type ConnectorSyncStatus } from "./api";
import { t, tx, type MessageKey, useLocale } from "./i18n";
import { formatDateTime } from "./format";
import { ErrorState } from "./Feedback";

const effectiveKeys: Record<string, MessageKey> = {
 quiet: "syncQuiet", stopping: "syncStopping", unknown: "syncUnknown",
 auto_running: "syncAuto", manual_running: "syncManual", blocked: "syncBlocked",
};
type Pending={action:"once"|"set";body:{expected_revision:number;request_id:string;auto_sync:boolean;cadence_seconds:number}};
const storageKey=(id:string)=>`xinsight.sync.pending.${id}`;
const settling=(s:ConnectorSyncStatus)=>s.effective==="stopping"||s.run?.state==="running";
export function ConnectorSyncPanel({installationID,csrfToken,canManage}:{installationID:string;csrfToken:string;canManage:boolean}) {
 useLocale();
 const [status,setStatus]=useState<ConnectorSyncStatus>();
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
 useEffect(()=>{
  const g=++generation.current;const id=installationID;
  setStatus(undefined);setError(undefined);setBusy(false);setPolling(false);setCadence("60");pendingRef.current=undefined;setPending(undefined);
  try{const raw=sessionStorage.getItem(storageKey(id));if(raw){const saved=JSON.parse(raw) as Pending;
   if((saved.action==="once"||saved.action==="set")&&typeof saved.body?.request_id==="string"&&Number.isInteger(saved.body.expected_revision)){pendingRef.current=saved;setPending(saved);}}}catch{setError(new Error(t("syncPendingStorage")));}
  if(!canManage)return;
  const controller=new AbortController();
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
 async function refresh(){const id=installationID,g=generation.current;try{accept(id,g,await connectorSyncAPI.status(id));}catch(e){if(currentID.current===id&&generation.current===g)setError(e);}}
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
      <button className="button" disabled={busy || !!pending || status.auto_sync || status.run?.state === "running" || status.workers.some(w => w.busy)} onClick={() => void act("once")}>{tx("syncOnce")}</button>
      <button className="button" disabled={busy || !!pending} onClick={() => void act("set", false)}>{tx("syncStop")}</button>
      <button className="button" disabled={busy} onClick={() => void refresh()}>{tx("syncRefresh")}</button>
      <p>{tx("syncBounds")}</p>
      <p>{tx("syncLastCheck", { value0: status.last_checked_at ? formatDateTime(status.last_checked_at) : t("syncNever") })}</p>
      <p>{tx("syncLastData", { value0: status.last_data_at ? formatDateTime(status.last_data_at) : t("syncNever") })}</p>
      {status.last_error ? <p>{tx("syncError", { value0: status.last_error })}</p> : null}
      {status.run ? <p>{tx("syncResult", { value0: status.run.state, value1: status.run.pages, value2: status.run.attempts })}</p> : null}
      <p>{tx("syncPeer")}</p><p>{tx("syncGap")}</p>
    </> : <p>{tx("syncUnknown")}</p>}
  </section>;
}
