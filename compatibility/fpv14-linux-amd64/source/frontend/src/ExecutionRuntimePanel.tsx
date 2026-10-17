import { authorizedNavigationReading, fixedReadingRequest, type WorkbenchNavigation } from "./workbenchNavigation";
import { useCallback, useEffect, useRef, useState } from "react";
import { ApiRequestError, isAbortError, runtimeAPI, scopeSessionKey, type AccessSessionResponse, type ExecutionSummary } from "./api";
import { RuntimeRequestDetail } from "./RuntimePage";
import type * as Runtime from "./runtime";

export interface RuntimeDetailContext { reading: Runtime.Reading; deadline: number; identity: string }

export function ExecutionRuntimePanel({ executionAsOf, navigate, summary, session, context, setContext, inherited, navigationInvalid = false, view = "systems" }: { view?: "systems" | "contents"; executionAsOf?: string; inherited?: WorkbenchNavigation; navigationInvalid?: boolean; navigate?:(path:string)=>void; summary: ExecutionSummary; session: AccessSessionResponse; context: RuntimeDetailContext | null; setContext: (value: RuntimeDetailContext | null) => void }) {
  const [request, setRequest] = useState<Runtime.Request | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const [hours, setHours] = useState<number | "inherited">(() => {
    if(inherited)return "inherited";
    const value = context ? (Date.parse(context.reading.period.end_exclusive) - Date.parse(context.reading.period.start_inclusive)) / 3_600_000 : 24;
    return [24, 168, 720].includes(value) ? value : 24;
  });
  const identity = scopeSessionKey(session);
  const creation = useRef<AbortController | null>(null);
  const lookup = useRef<AbortController | null>(null);
  const invalidate = useCallback((e: unknown) => { creation.current?.abort(); lookup.current?.abort(); setRequest(null); setContext(null); setError(e); setBusy(false); }, [setContext]);
  useEffect(() => {
    setRequest(null);
    if (!context) return;
    if (context.identity !== identity || !(context.deadline > performance.now())) { invalidate(new ApiRequestError(409)); return; }
    const controller = new AbortController(); lookup.current=controller;
    setBusy(true); setError(null);
    const timer = window.setTimeout(() => { controller.abort(); invalidate(new ApiRequestError(410)); }, Math.min(context.deadline - performance.now(), 2_147_483_647));
    void runtimeAPI.executionRequest(context.reading.reading_id, summary.execution_id, controller.signal).then(link => {
      if (controller.signal.aborted) return;
      if (link.version !== "xinsight.runtime.execution-request.v1" || link.reading_id !== context.reading.reading_id || link.membership_ref !== context.reading.membership_ref ||
          link.execution_id !== summary.execution_id || link.request.source_system_id !== summary.source_system_id || link.request.request_id !== summary.request_id || !(context.deadline > performance.now())) throw new ApiRequestError(409);
      setRequest(link.request);
    }).catch(e => { if (!isAbortError(e) && !controller.signal.aborted) invalidate(e); })
      .finally(() => { if (!controller.signal.aborted) setBusy(false); });
    return () => { controller.abort(); window.clearTimeout(timer); };
  }, [context, identity, summary.execution_id, summary.source_system_id, summary.request_id, invalidate]);
  useEffect(() => {
    const invalidated=()=>invalidate(new ApiRequestError(409));
    window.addEventListener("xinsight:scope-changed",invalidated);window.addEventListener("xinsight:unauthorized",invalidated);
    return()=>{creation.current?.abort();window.removeEventListener("xinsight:scope-changed",invalidated);window.removeEventListener("xinsight:unauthorized",invalidated);};
  }, [identity, summary.execution_id, invalidate]);
  // Restore a related reading only on entry, never as a fallback after denial.
  const retained=useRef(inherited);
  useEffect(()=>{
    const origin=retained.current;
    if(context||!origin?.related||origin.related.executionID!==summary.execution_id)return;
    const controller=new AbortController();creation.current=controller;setBusy(true);
    void (async()=>{
      await authorizedNavigationReading(origin,session,controller.signal);
      const reading=await runtimeAPI.reading(origin.related!.readingID,controller.signal);
      if(controller.signal.aborted)return;
      if(reading.reading_id!==origin.related!.readingID||reading.contract_version!=="xinsight.runtime-reading.v1"||!(Date.parse(reading.expires_at)>Date.now()))throw new ApiRequestError(410);
      setContext({reading,identity,deadline:performance.now()+Date.parse(reading.expires_at)-Date.now()});
    })().catch(e=>{if(!controller.signal.aborted)invalidate(e)});
    return()=>controller.abort();
  },[identity,summary.execution_id]);
  function rememberRelated(readingID:string){
    const saved=window.history.state?.workbenchReading;
    if(inherited&&saved?.readingID===inherited.readingID&&saved?.owner===inherited.owner){
      window.history.replaceState({...window.history.state,workbenchReading:{...saved,related:{executionID:summary.execution_id,readingID}}},"",window.location.href);
    }
  }
  async function readRelated() {
    creation.current?.abort(); lookup.current?.abort(); const controller = new AbortController(); creation.current = controller;
    setBusy(true); setRequest(null); setContext(null); setError(null);
    try {
      if(navigationInvalid)throw new ApiRequestError(409);
      let period: Runtime.ReadingRequest["period"]={ relative_hours: typeof hours==="number"?hours:24, display_timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC" };
      let filters: Runtime.ReadingRequest["filters"]={ source_ids: [summary.source_system_id], source_projects: [], cost_projects: [] };
      if(inherited){
        const source=await authorizedNavigationReading(inherited,session,controller.signal);
        const request=fixedReadingRequest(source,inherited.request);
        if(request.filters.source_ids.length&&!request.filters.source_ids.includes(summary.source_system_id))throw new ApiRequestError(409);
        filters={...request.filters,source_ids:[summary.source_system_id]};
        if(hours==="inherited")period=request.period;
      }
      if(controller.signal.aborted)return;
      const sentAt=performance.now();
      const reading = await runtimeAPI.createReading({ contract_version: "xinsight.runtime-reading.v1", metric_version: "xinsight.runtime.request-observations.v1", period, filters }, session.csrf_token, controller.signal);
      if (controller.signal.aborted) return;
      const deadline = sentAt + Date.parse(reading.expires_at) - Date.parse(reading.cutoff);
      if (!(deadline > performance.now())) throw new ApiRequestError(410);
      rememberRelated(reading.reading_id);
      setContext({ reading, deadline, identity });
    } catch (e) { if (!isAbortError(e) && !controller.signal.aborted) { setError(e); setBusy(false); } }
  }
  const message = error instanceof ApiRequestError && error.status === 404 ? "本次阅读没有可展示的运行关联；不会按同名请求猜测。可调整期间重新读取。" :
    error instanceof ApiRequestError && error.status === 403 ? "当前没有读取这部分资料的权限。" :
    (error instanceof Error && error.message==="navigation_access_changed") || error instanceof ApiRequestError && [409, 410].includes(error.status) ? "阅读已失效，原资料已移除。" : "暂时无法读取相关资料，请重试。";
  return <section className="runtime-workbench runtime-panel" aria-label={view === "contents" ? "共享详情的交互内容" : "共享详情的系统与数据"}>
    <h2>{view === "contents" ? "交互内容" : "系统与数据"}</h2><p>{view === "contents" ? "查看同一执行的内容清单；正文仅在点击读取后按当前授权获取。" : "查看同一执行的运行与资源证据。"}相关过程使用独立的授权阅读，不计入原费用或贡献分母；来源读取权、调用权和外发许可分别判断。</p>
    {inherited?<p>沿用费用阅读的固定起止与来源/项目筛选，并限定同一执行。运行按观察时间及已有经济关联筛选，使用独立截止；费用贡献的模型、用途等维度不作为运行过滤，也不改变原费用分母。</p>:null}
    {navigationInvalid?<p role="alert">原费用阅读引用已失效，请返回费用页明确重新读取。</p>:null}
    <form className="runtime-filters" onSubmit={e => { e.preventDefault(); void readRelated(); }}>
      <label>相关过程期间<select value={hours} onChange={e => setHours(e.target.value==="inherited"?"inherited":Number(e.target.value))}>{inherited?<option value="inherited">原费用阅读期间（固定起止）</option>:null}<option value={24}>最近 24 小时</option><option value={168}>最近 7 天</option><option value={720}>最近 30 天</option></select></label>
      <button disabled={busy||navigationInvalid}>{view === "contents" ? "读取相关内容清单" : "读取相关运行与资源"}</button>
    </form>
    {busy ? <p role="status">正在核对同一执行的冻结关联…</p> : null}
    {error ? <p role="alert">{message}</p> : null}
    {request && context && context.identity === identity && context.deadline > performance.now() ? <><p>相关阅读期间：{context.reading.period.start_inclusive} — {context.reading.period.end_exclusive}；{context.reading.period.timezone}。截止 {context.reading.cutoff}。</p>
      <RuntimeRequestDetail executionContext={inherited && executionAsOf && !navigationInvalid ? {workbench_reading_id: inherited.readingID, execution_id: summary.execution_id, execution_as_of: executionAsOf, runtime_reading_id: context.reading.reading_id, runtime_request_ref: request.request_ref} : undefined} session={session} navigate={navigate} reading={context.reading} request={request} view={view} onInvalidated={invalidate} />
    </> : null}
  </section>;
}
