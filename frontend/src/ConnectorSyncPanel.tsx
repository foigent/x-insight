import { useCallback, useEffect, useRef, useState } from "react";
import { ApiRequestError, connectorSetupAPI, connectorSyncAPI, isAbortError, type ConnectorSyncStatus } from "./api";
import { t, tx, type MessageKey, useLocale } from "./i18n";
import { formatDateTime } from "./format";
import { ErrorState, LoadingBlock } from "./Feedback";
import { canRetryRead, isAccessDenied } from "./readFailures";
import { useConnectorRead } from "./useConnectorRead";

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
  const [cadence, setCadence] = useState<string>();
  const [actionError, setActionError] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState<Pending>();
  const pendingRef = useRef<Pending>(undefined);
  const generation = useRef(0);
  const loadStatus = useCallback(async (signal: AbortSignal) => {
    const next = await connectorSyncAPI.status(installationID, signal);
    if (next.installation_id !== installationID) throw new Error("Sync status belongs to another installation");
    return next;
  }, [installationID]);
  const loadConnection = useCallback(async (signal: AbortSignal) => {
    const next = await connectorSetupAPI.status(installationID, signal);
    if (next.installation_id !== installationID) throw new Error("Connection status belongs to another installation");
    return next;
  }, [installationID]);
  const connection = useConnectorRead(canManage ? installationID : null, loadConnection);
  const denied = isAccessDenied(connection.error) ? connection.error : undefined;
  const read = useConnectorRead(canManage ? installationID : null, loadStatus, busy || denied ? undefined : 1500, settling);
  const status = denied ? undefined : read.data;
  const ready = !!status && !read.loading && !busy;
  const cadenceValue = cadence ?? String(status?.cadence_seconds ?? 60);

  const forget = useCallback((id: string) => {
    try { sessionStorage.removeItem(storageKey(id)); } catch { /* A confirmed server receipt remains authoritative. */ }
    pendingRef.current = undefined;
    setPending(undefined);
  }, []);

  useEffect(() => {
    setActionError(undefined); setBusy(false); setCadence(undefined);
    pendingRef.current = undefined; setPending(undefined);
    if (canManage) {
      try {
        const raw = sessionStorage.getItem(storageKey(installationID));
        if (raw) {
          const saved = JSON.parse(raw) as Pending;
          if ((saved.action === "once" || saved.action === "set") && typeof saved.body?.request_id === "string" && Number.isInteger(saved.body.expected_revision)) {
            pendingRef.current = saved; setPending(saved);
          }
        }
      } catch { setActionError(new Error(t("syncPendingStorage"))); }
    }
    return () => { generation.current++; };
  }, [installationID, canManage]);

  const invalidate = read.invalidate;
  useEffect(() => {
    if (!denied) return;
    generation.current++;
    invalidate(denied); setActionError(undefined); setBusy(false);
  }, [denied, invalidate]);

  useEffect(() => {
    const prior = pendingRef.current;
    if (status && prior && (status.last_request_id === prior.body.request_id || status.run?.request_id === prior.body.request_id)) {
      forget(installationID);
    }
  }, [forget, status, installationID]);

  function refresh() {
    setActionError(undefined);
    void connection.refresh();
    void read.refresh();
  }

  async function act(action: "once" | "set", auto = false) {
    if (!canManage || !ready || !status) return;
    const id = installationID, g = generation.current;
    const current = () => generation.current === g;
    setBusy(true); setActionError(undefined);
    try {
      let mutation = pendingRef.current;
      if (!mutation) {
        const seconds = Number(cadenceValue);
        if (!Number.isInteger(seconds) || seconds < 5 || seconds > 3600) throw new Error(t("syncCadenceInvalid"));
        mutation = { action, body: { expected_revision: status.revision, request_id: crypto.randomUUID(), auto_sync: auto, cadence_seconds: seconds } };
        // Persist before sending. A lost response/reload must keep the original UUID.
        try { sessionStorage.setItem(storageKey(id), JSON.stringify(mutation)); } catch { throw new Error(t("syncPendingStorage")); }
        pendingRef.current = mutation; setPending(mutation);
      }
      const next = mutation.action === "once" ? await connectorSyncAPI.once(id, mutation.body, csrfToken) : await connectorSyncAPI.set(id, mutation.body, csrfToken);
      if (!current()) return;
      if (next.installation_id !== id) throw new Error("Sync receipt belongs to another installation");
      forget(id);
      read.accept(next);
      setCadence(undefined);
    } catch (error) {
      if (!current() || isAbortError(error)) return;
      if (isAccessDenied(error) || error instanceof ApiRequestError && (error.status === 404 || error.status === 410 || error.problem?.code === "connector_sync_binding_required")) {
        read.invalidate(error);
        return;
      }
      setActionError(error);
      if (error instanceof ApiRequestError && error.status === 400) forget(id);
      if (error instanceof ApiRequestError && error.status === 409) {
        const next = await read.refresh();
        if (!current() || !next) return;
        // A newer revision makes the original CAS body inapplicable. The
        // status refresh stays read-only, even when it resolves a conflict.
        const prior = pendingRef.current;
        if (prior && next.revision > prior.body.expected_revision) forget(id);
      }
    } finally {
      if (current()) setBusy(false);
    }
  }
  if (!canManage) return null;
  const currentFailures = status?.workers.filter(w => w.revision === status.revision && w.last_error) ?? [];
  const readError = denied || read.error;
  const mayRefresh = !readError || canRetryRead(readError) ||
    readError instanceof ApiRequestError && readError.problem?.code === "connector_sync_binding_required";
  return <section aria-label={t("syncTitle")} className="connector-sync-panel">
    <h3>{tx("syncTitle")}</h3>
    {actionError && !readError ? <ErrorState error={actionError} /> : null}
    {readError ? <ErrorState error={readError} /> : null}
    {!readError && read.loading && !status ? <LoadingBlock label={t("connector.syncLoading")} /> : null}
    {pending && status ? <p>{tx("syncPending")} <button className="button" disabled={!ready} onClick={() => void act(pending.action)}>{tx("syncConfirmPending")}</button></p> : null}
    {status?.installation_id === installationID ? <>
      <label><input type="checkbox" checked={status.auto_sync} disabled={!ready || !!pending || status.run?.state === "running" && !status.auto_sync}
        onChange={e => void act("set", e.target.checked)} />{tx("syncToggle")}</label>
      <label>{tx("syncCadence")} <input type="number" min={5} max={3600} step={1} value={cadenceValue}
        onChange={e => setCadence(e.target.value)} disabled={!ready} /></label>
      <button className="button" disabled={!ready || !!pending} onClick={() => void act("set", status.auto_sync)}>{tx("syncSaveCadence")}</button>
      <p>{tx("syncCadenceHelp")}</p>
      {status.cadence_seconds === null ? <p>{tx("syncExistingCadence")}</p> : null}
      <p role="status">{tx("syncEffective", { value0: t(effectiveKeys[status.effective] ?? "syncUnknown") })}</p>
      <div role="note">
        <p>{tx("syncCredentialBoundary")}</p>
        {connection.data?.issues.some(i=>["control_identity_expired","control_credential_expired"].includes(i.code))?<p role="status">{tx("syncCredentialExpired")}</p>:!connection.data?<p>{tx("syncCredentialUnchecked")}</p>:null}
        <a href={`/connectors/unirouter/setup?installation=${encodeURIComponent(installationID)}`}>{tx("syncConnectionStatus")}</a>
      </div>
      <button className="button" disabled={!ready || !!pending || status.auto_sync || status.run?.state === "running" || status.workers.some(w => w.busy)} onClick={() => void act("once")}>{tx("syncOnce")}</button>
      <button className="button" disabled={!ready || !!pending} onClick={() => void act("set", false)}>{tx("syncStop")}</button>
      <p>{tx("syncBounds")}</p>
      <p>{tx("syncLastCheck", { value0: status.last_checked_at ? formatDateTime(status.last_checked_at) : t("syncNever") })}</p>
      <p>{tx("syncLastData", { value0: status.last_data_at ? formatDateTime(status.last_data_at) : t("syncNever") })}</p>
      {status.last_error && !currentFailures.some(w=>w.last_error===status.last_error) ? <div><p>{tx("syncUnboundLastError")}</p><SyncFailure code={status.last_error}/></div> : null}
      {currentFailures.map(w=><section key={w.worker}><h4>{t("syncWorkerFailure",{value0:t(({activation:"syncWorkerActivation",projects:"syncWorkerProjects",source:"syncWorkerSource",feed:"syncWorkerFeed",policy:"syncWorkerPolicy",credentials:"syncWorkerCredentials"} as Record<string,MessageKey>)[w.worker]??"syncWorkerUnknown")})}</h4><SyncFailure code={w.last_error}/></section>)}
      {status.run ? <p>{tx("syncResult", { value0: t(runKeys[status.run.state]??"syncUnknown"), value1: status.run.pages, value2: status.run.attempts })}</p> : null}
      <p>{tx("syncPeer")}</p><p>{tx("syncGap")}</p>
    </> : null}
    {mayRefresh ? <button className="button" disabled={busy || read.loading} onClick={refresh}>{tx("syncRefresh")}</button> : null}
  </section>;
}
