import { navigationOwner, type RuntimeNavigation } from "./workbenchNavigation";
import { ContentPanel } from "./ContentPanel";
import { ActionWitnessPanel } from "./ActionWitnessPanel";
import { Identifier, compactIdentifier } from "./Identifier";
import type { ExecutionContext } from "./assessmentTypes";
import { ReviewStart } from "./ReviewStart";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { accessAPI, ApiRequestError, isAbortError, runtimeAPI, scopeSessionKey, type AccessSessionResponse } from "./api";
import type * as Runtime from "./runtime";
import "./runtime.css";
import { ViewTabs } from "./DetailViewTabs";
import type { ExecutionView } from "./executionReading";
import { SourceFactCard } from "./SourceFactsPanel";

const dimensionNames: Record<Runtime.Dimension, string> = {
  operation: "操作", selected_model: "选中模型", selected_provider: "选中提供方", required_capability: "所需能力",
  enabled_capability: "已启用能力依据", destination_provider: "目的地提供方", destination_origin: "目的地地址",
};
const outcomeNames: Record<string, string> = {
  local_delivery_completed: "本地交付完成", technical_failure: "技术失败", admission_rejected: "准入拒绝", output_held: "输出留置",
  interrupted: "交付中断", context_done_unresolved: "上下文结束，结果未决", unresolved: "结果未决", conflicting: "证据冲突", missing_predecessor: "前驱缺失",
};
const stateNames: Record<string, string> = {
  not_available: "不可得", not_observed: "未观察到", unknown: "未知", not_started: "未开始", registered: "已登记", available: "有依据", partial: "部分依据",
  authenticated: "已认证", admitted: "已准入", rejected: "已拒绝", completed: "已完成", write_failed: "写入失败", interrupted: "已中断",
  observed: "已观察到", reserved: "已预留", settled: "已结算", not_created: "未创建", verified: "已核验", enabled: "已启用", not_applicable: "不适用",
  lease_valid_execution: "执行租约有效", leased_pending: "等待租约有效", last_known: "最后已知状态", not_current: "当前状态未确认",
};
const stateName = (s?: string) => s ? stateNames[s] ?? s : "未提供";
const operationNames: Record<string,string> = { "openai.chat_completions.v1":"对话生成", "openai.responses.v1":"模型响应", "openai.embeddings.v1":"文本向量", chat:"对话生成" };
const measurementNames: Record<string,string> = { request_response_complete:"请求响应完成耗时", stream_first_valid_output:"首次有效流输出耗时", stream_complete:"流式完成耗时", attempt_http_round_trip:"单次尝试 HTTP 往返耗时" };
const observationNames: Record<string,string> = {
  request_started:"请求开始", admission_rejected:"准入拒绝", request_terminal:"请求终态记录",
  delivery_started:"本地交付开始", local_delivery:"本地交付观察", output_held:"输出留置", request_context_done:"请求上下文结束",
  response_measurement:"响应耗时观察", first_output_measurement:"首次输出耗时观察", stream_measurement:"流式耗时观察",
  media_running:"媒体任务运行观察", media_terminal:"媒体任务终态记录", state_reconciled:"状态核对记录",
  attempt_registered:"尝试登记", provider_response_received:"收到提供方响应", provider_rejected:"提供方拒绝",
  provider_outcome_unknown:"提供方结果未知", provider_response_unusable:"提供方响应不可用", financial_result:"财务状态观察",
  round_trip_measurement:"尝试往返耗时观察", media_attempt_registered:"媒体尝试登记", media_provider_succeeded:"媒体提供方成功观察",
  media_retry_scheduled:"媒体重试已安排", media_result:"媒体尝试结果观察", selection:"能力选择观察",
  input_result:"输入政策结果观察", output_result:"输出政策结果观察", stream_result:"流式政策结果观察",
  dispatch_registered:"目的地发送登记", dispatch_result:"目的地发送结果观察",
  local_delivery_completed:"本地交付完成",
};
const readableStates: Record<string,string> = { ...stateNames,
  streaming:"流式", non_streaming:"非流式", response_received:"已收到响应", accepted:"已接受", write_started:"已开始写入", prepared:"已准备", pending:"待定", cancelled:"已取消",
  request_response_complete:"请求响应完成", protocol_response_written:"协议响应已写出", protocol_terminal_written_and_flushed:"协议终止标记已写出并刷新", first_meaningful_frame_written_and_flushed:"首个有效帧已写出并刷新",
  local_completed:"本地已完成", legacy_unknown:"历史状态未知", invalid:"无效", configured:"由配置声明", do_observed:"已观察到发送调用", response_observed:"已观察到响应", known:"已知",
  input:"输入阶段", output:"输出阶段", stream:"流式阶段", shadow:"观察模式", enforce:"执行模式", allow:"允许", audit:"审计", block:"阻止", replace:"替换", mask:"遮蔽", match:"命中", error:"错误", not_covered:"未覆盖",
};
const readableState = (value?:string) => value ? readableStates[value] ?? "未识别的状态，见原始字段" : "未提供";
const operationName = (value:string) => operationNames[value] ?? "未识别的操作类型，见原始字段";
const measurementName = (value:string) => measurementNames[value] ?? "未识别的测量类型，见原始字段";

function time(value: string | undefined, timezone: string) {
  if (!value) return "未知";
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return "时间不可用";
  return new Intl.DateTimeFormat("zh-CN", { dateStyle: "short", timeStyle: "medium", timeZone: timezone, hour12: false }).format(date);
}
function invalidReading(error: unknown) {
  return error instanceof ApiRequestError && [401, 403, 409, 410].includes(error.status);
}
function problem(error: unknown) {
  if (error instanceof ApiRequestError) {
    if (error.status === 403) return "当前没有这部分资料的读取权限。";
    if (error.status === 409 || error.status === 410) return "阅读已失效，请重新读取。";
    if (error.status === 401) return "请重新登录。";
    if (error.status === 400) return "筛选条件无效，请检查后重试。";
  }
  return "暂时无法读取，请重试。";
}

// Each panel progresses independently. Reading/profile changes cancel old
// work and erase old values; a transient page failure can retry that cursor.
function usePage<P extends { items: unknown[]; next_cursor?: string }>(loader: (cursor: string | undefined, signal: AbortSignal) => Promise<P>, onInvalidated: (error: unknown) => void) {
  const [items, setItems] = useState<P["items"][number][]>([]);
  const [page, setPage] = useState<P | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const generation = useRef(0);
  const controller = useRef<AbortController | null>(null);
  const failedCursor = useRef<string | undefined>(undefined);
  const load = useCallback((cursor?: string) => {
    const seq = ++generation.current;
    controller.current?.abort();
    const abort = new AbortController(); controller.current = abort;
    setBusy(true); setError(null); failedCursor.current = cursor;
    if (!cursor) { setItems([]); setPage(null); }
    void loader(cursor, abort.signal).then(next => {
      if (seq !== generation.current) return;
      setItems(previous => cursor ? [...previous, ...next.items] : next.items);
      setPage(next);
    }).catch(e => {
      if (seq !== generation.current || isAbortError(e)) return;
      setError(e);
      if (invalidReading(e)) { setItems([]); setPage(null); onInvalidated(e); }
    }).finally(() => { if (seq === generation.current) setBusy(false); });
  }, [loader, onInvalidated]);
  useEffect(() => { load(); return () => { generation.current++; controller.current?.abort(); }; }, [load]);
  return { items, page, error, busy, refresh: () => load(), more: () => load(page?.next_cursor), retry: () => load(failedCursor.current) };
}
function PageActions({ state, noun }: { state: { busy: boolean; error: unknown; page: { next_cursor?: string } | null; more: () => void; retry: () => void }; noun: string }) {
  return <div className="runtime-page-actions">
    {state.busy ? <span role="status">正在读取{noun}…</span> : null}
    {state.error ? <><p role="alert">{problem(state.error)}</p><button disabled={state.busy} onClick={state.retry}>重试{noun}</button></> : null}
    {!state.error && state.page?.next_cursor ? <button disabled={state.busy} onClick={state.more}>加载更多{noun}</button> : null}
  </div>;
}
function Fields({ rows }: { rows: [string, ReactNode][] }) {
  return <dl className="runtime-fields">{rows.map(([name, value]) => <div key={name}><dt>{name}</dt><dd>{value ?? "未提供"}</dd></div>)}</dl>;
}

interface RuntimeView {
  entry: Runtime.EntryTime;
  dimensions: { runtime: Runtime.Dimension; capabilities: Runtime.Dimension };
  selected: Runtime.Request | null;
  detailView?: ExecutionView;
}
const initialView = (): RuntimeView => ({ entry: "known", dimensions: { runtime: "operation", capabilities: "selected_model" }, selected: null });
// Kept only in this authenticated App instance. Returning rechecks the same
// server reading before restoring any request or source data into the DOM.
export interface RuntimeSnapshot {
  readingID?: string; // Retains the identity when invalidation clears its data.
  identity: string; hours: number; inheritedPeriod?: Runtime.ReadingRequest["period"]; source: string; project: string; costProject: string;
  reading: Runtime.Reading | null; applied: Runtime.ReadingRequest | null;
  deadline: number; view: RuntimeView; error: unknown;
}
export function RuntimePage({ session, capabilities = false, navigate, canInvestigate = false, snapshot, onSnapshot, initialRequest, navigation, navigationInvalid = false }: { navigation?: RuntimeNavigation; navigationInvalid?: boolean; initialRequest?: Runtime.ReadingRequest; session: AccessSessionResponse; capabilities?: boolean; navigate?: (path: string) => void; canInvestigate?: boolean; snapshot?: RuntimeSnapshot | null; onSnapshot?: (value: RuntimeSnapshot) => void }) {
  // Within the same App lifetime, retain the original monotonic deadline,
  // failure and controls. History references restore only after that snapshot
  // is unavailable (for example, a refresh), and never override its failure.
  const saved = useRef(!navigationInvalid && snapshot && (!navigation ||
    navigation.owner === navigationOwner(session) && (snapshot.reading?.reading_id ?? snapshot.readingID) === navigation.readingID) ? snapshot : null);
  const initial = useRef(navigation?.request ?? initialRequest);
  const retainedNavigation = useRef(saved.current ? undefined : navigation);
  const snapshotReadingID = useRef(saved.current?.reading?.reading_id ?? saved.current?.readingID ?? navigation?.readingID);
  const timezone = saved.current?.applied?.period.display_timezone ?? navigation?.request.period.display_timezone ?? initial.current?.period.display_timezone ?? (Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC");
  const [owner, setOwner] = useState(saved.current?.identity ?? scopeSessionKey(session));
  const [view, setView] = useState<RuntimeView>(() => saved.current?.view ?? initialView());
  const [hours, setHours] = useState(saved.current?.hours ?? initial.current?.period.relative_hours ?? 24);
  const [inheritedPeriod, setInheritedPeriod] = useState<Runtime.ReadingRequest["period"] | undefined>(saved.current ? saved.current.inheritedPeriod : initial.current?.period.start_inclusive ? initial.current.period : undefined);
  const [source, setSource] = useState(saved.current?.source ?? initial.current?.filters.source_ids.join(", ") ?? "");
  const [project, setProject] = useState(saved.current?.project ?? initial.current?.filters.source_projects.join(", ") ?? "");
  const [costProject, setCostProject] = useState(saved.current?.costProject ?? initial.current?.filters.cost_projects.join(", ") ?? "");
  const [reading, setReading] = useState<Runtime.Reading | null>(null);
  const [applied, setApplied] = useState<Runtime.ReadingRequest | null>(null);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [currentFilters, setCurrentFilters] = useState<Runtime.ReadingRequest["filters"] | null>(null);
  const controller = useRef<AbortController | null>(null);
  const generation = useRef(0);
  const readingDeadline = useRef(saved.current?.deadline ?? 0);
  const identity = scopeSessionKey(session);
  const csv = (value: string) => [...new Set(value.split(",").map(v => v.trim()).filter(Boolean))];
  const filters = () => ({ source_ids: csv(source), source_projects: csv(project), cost_projects: csv(costProject) });
  const invalidate = useCallback((e: unknown) => { setReading(null); setApplied(null); setView(initialView()); setError(e); }, []);
  const create = useCallback((input: Runtime.ReadingRequest) => {
    const seq = ++generation.current;
    controller.current?.abort(); controller.current = new AbortController();
    const sentAt = performance.now();
    setReading(null); setApplied(null); setView(initialView()); setError(null); setBusy(true);
    void runtimeAPI.createReading(input, session.csrf_token, controller.current.signal).then(next => {
      if (seq === generation.current) {
        // Use the server's lifetime and conservatively include request transit;
        // a workstation clock change must not extend reading eligibility.
        readingDeadline.current = sentAt + Date.parse(next.expires_at) - Date.parse(next.cutoff);
        if (!(readingDeadline.current > performance.now())) throw new ApiRequestError(410);
        retainedNavigation.current=undefined;
        snapshotReadingID.current=next.reading_id;
        setReading(next); setApplied(input);
      }
    }).catch(e => { if (seq === generation.current && !isAbortError(e)) setError(e); })
      .finally(() => { if (seq === generation.current) setBusy(false); });
  }, [session.csrf_token]);
  useEffect(() => {
    setCurrentFilters(null);
    const previous = saved.current;
    setOwner(identity);
    const retained=retainedNavigation.current;
    if(navigationInvalid){
      invalidate(new ApiRequestError(409));setBusy(false);
    } else if(retained){
      const seq=++generation.current;
      controller.current?.abort();const control=new AbortController();controller.current=control;
      setReading(null);setApplied(null);setBusy(true);setError(null);
      void (async()=>{
        if(retained.owner!==navigationOwner(session))throw new ApiRequestError(403);
        const [current,next]=await Promise.all([accessAPI.session(control.signal),runtimeAPI.reading(retained.readingID,control.signal)]);
        if(seq!==generation.current||control.signal.aborted)return;
        if(scopeSessionKey(current)!==identity||next.reading_id!==retained.readingID||next.contract_version!=="xinsight.runtime-reading.v1"||next.metric_version!=="xinsight.runtime.request-observations.v1"||
          next.period.start_inclusive!==retained.request.period.start_inclusive||next.period.end_exclusive!==retained.request.period.end_exclusive||next.period.timezone!==retained.request.period.display_timezone)throw new ApiRequestError(409);
        const deadline=performance.now()+Math.min(Date.parse(next.expires_at)-Date.now(),Date.parse(next.expires_at)-Date.parse(next.cutoff));
        if(!(deadline>performance.now()))throw new ApiRequestError(410);
        let selected:Runtime.Request|null=null;
        if(retained.selectedRef){
          let cursor:string|undefined;const seen=new Set<string>();
          do{
            const page=await runtimeAPI.requests(next.reading_id,retained.entry,cursor,control.signal);
            if(seq!==generation.current||control.signal.aborted)return;
            if(page.reading_id!==next.reading_id)throw new ApiRequestError(409);
            selected=page.items.find(item=>item.request_ref===retained.selectedRef)??null;
            if(selected)break;
            cursor=page.next_cursor;
            if(cursor&&seen.has(cursor))throw new ApiRequestError(409);
            if(cursor)seen.add(cursor);
          }while(cursor);
          if(!selected)throw new ApiRequestError(409);
        }
        if(!(deadline>performance.now()))throw new ApiRequestError(410);
        readingDeadline.current=deadline;setReading(next);setApplied(retained.request);
        setView({...initialView(),entry:retained.entry,selected,detailView:retained.detailView});
      })().catch(e=>{if(seq===generation.current&&!isAbortError(e))invalidate(e)})
        .finally(()=>{if(seq===generation.current)setBusy(false)});
    } else if (!previous) {
      create(initial.current ?? { contract_version: "xinsight.runtime-reading.v1", metric_version: "xinsight.runtime.request-observations.v1", period: { relative_hours: 24, display_timezone: timezone }, filters: { source_ids: [], source_projects: [], cost_projects: [] } });
    } else {
      const seq = ++generation.current;
      controller.current?.abort(); controller.current = new AbortController();
      setReading(null); setApplied(null); setBusy(true); setError(null);
      void (async () => {
        if (previous.identity !== identity) throw new ApiRequestError(403);
        if (!previous.reading || !previous.applied || !(previous.deadline > performance.now())) throw previous.error ?? new ApiRequestError(410);
        const next = await runtimeAPI.reading(previous.reading.reading_id, controller.current!.signal);
        if (seq !== generation.current) return;
        const stable = ["reading_id", "membership_ref", "authorization_revision", "interpretation_revision", "contract_version", "metric_version", "cutoff", "expires_at", "period"] as const;
        if (stable.some(key => JSON.stringify(next[key]) !== JSON.stringify(previous.reading![key]))) throw new ApiRequestError(409);
        if (!(previous.deadline > performance.now())) throw new ApiRequestError(410);
        readingDeadline.current = previous.deadline;
        setReading(next); setApplied(previous.applied); setView(previous.view);
      })().catch(e => { if (seq === generation.current && !isAbortError(e)) invalidate(e); })
        .finally(() => { if (seq === generation.current) setBusy(false); });
    }
    return () => { generation.current++; controller.current?.abort(); };
  }, [identity]); // CSRF refresh preserves the same reading; changed grants invalidate it.
  useEffect(() => {
    if (busy || owner !== identity) return;
    const value = { identity, hours, inheritedPeriod, source, project, costProject, reading, readingID: snapshotReadingID.current, applied, deadline: readingDeadline.current, view, error };
    saved.current = value;
    onSnapshot?.(value);
  }, [busy, owner, identity, hours, inheritedPeriod, source, project, costProject, reading, applied, view, error, onSnapshot]);
  useEffect(() => {
    if (!reading) return;
    const delay = Math.max(0, readingDeadline.current - performance.now());
    const timer = window.setTimeout(() => invalidate(new ApiRequestError(410)), Math.min(delay, 2_147_483_647));
    return () => window.clearTimeout(timer);
  }, [reading, invalidate]);

  return <div className="runtime-workbench">
    <header className="runtime-heading"><div><h1>{capabilities ? "模型与能力" : "运行概览"}</h1><p>{capabilities ? "查看每次选择所依据的模型、能力和资格记录。" : "查看已知请求的结果、测量和逐次尝试。"}</p></div></header>
    <form className="runtime-filters" onSubmit={event => { event.preventDefault(); create({ contract_version: "xinsight.runtime-reading.v1", metric_version: "xinsight.runtime.request-observations.v1", period: inheritedPeriod ?? { relative_hours: hours, display_timezone: timezone }, filters: filters() }); }}>
      <label>时间范围<select value={inheritedPeriod?"inherited":hours} onChange={e => { setInheritedPeriod(undefined); setHours(Number(e.target.value)); }}>{inheritedPeriod?<option value="inherited">来源期间（固定起止）</option>:null}<option value={24}>最近 24 小时</option><option value={168}>最近 7 天</option><option value={720}>最近 30 天</option></select></label>
      <label>来源标识<input value={source} onChange={e => setSource(e.target.value)} placeholder="全部授权来源" /></label>
      <label>来源项目<input value={project} onChange={e => setProject(e.target.value)} placeholder="全部授权项目" /></label>
      <label>费用项目<input value={costProject} onChange={e => setCostProject(e.target.value)} placeholder="全部授权费用项目" /></label>
      <button className="primary-button" disabled={busy}>读取历史</button>
      <p>{inheritedPeriod?"沿用来源期间；每次读取建立新截止。":""}时区：{timezone}。多个筛选值用逗号分隔；费用项目筛选需要实际经济事实关联。</p>
    </form>
    {busy ? <p role="status">{retainedNavigation.current ? "正在按当前授权恢复原阅读…" : "正在建立本次阅读…"}</p> : null}
    {error ? <p role="alert">{problem(error)}{retainedNavigation.current || navigationInvalid ? " 原阅读未恢复；可通过“读取历史”显式建立新阅读。" : ""}</p> : null}
    {reading && applied ? <RuntimeHistory session={session} navigate={navigate} canInvestigate={canInvestigate} key={reading.reading_id} view={view} setView={setView} reading={reading} filters={applied.filters} capabilities={capabilities} onInvalidated={invalidate} /> : null}
    {!capabilities ? <section className="runtime-panel" aria-labelledby="current-title"><h2 id="current-title">租约观察</h2><p>独立于历史请求。只覆盖有租约的执行或等待，不证明持续连接，也不包含所有同步请求。</p><button onClick={() => setCurrentFilters(filters())}>查看当前筛选的租约观察</button>{currentFilters ? <CurrentPanel key={JSON.stringify(currentFilters)} filters={currentFilters} timezone={timezone} /> : null}</section> : null}
  </div>;
}

function RuntimeHistory({ session, reading, filters, capabilities, onInvalidated, navigate, canInvestigate, view, setView }: { session:AccessSessionResponse; view: RuntimeView; setView: (value: RuntimeView) => void; navigate?: (path: string) => void; canInvestigate: boolean; reading: Runtime.Reading; filters: Runtime.ReadingRequest["filters"]; capabilities: boolean; onInvalidated: (e: unknown) => void }) {
  const { entry, selected } = view;
  const topic = capabilities ? "capabilities" : "runtime";
  const dimension = view.dimensions[topic];
  const setDimension = (value: Runtime.Dimension) => setView({ ...view, dimensions: { ...view.dimensions, [topic]: value } });
  const setSelected = (value: Runtime.Request | null) => setView({ ...view, selected: value });
  const zone = reading.period.timezone;
  return <>
    <section className="runtime-reading" aria-label="本次阅读范围"><div className="runtime-reading-summary"><p><strong>{time(reading.period.start_inclusive, zone)} — {time(reading.period.end_exclusive, zone)}</strong> · {zone}</p><p>接收截止 {time(reading.cutoff, zone)}；期间完整性未知。已知进入时间 {reading.requests_with_entry_time} 个请求，进入时间未知 {reading.requests_with_unknown_entry_time} 个请求。</p>
      {reading.coverage.missing_predecessors > 0 ? <p>{reading.coverage.missing_predecessors} 条观察的前驱尚未收齐。</p> : null}
      {reading.coverage.streams.some(s => s.requires_attention || s.state !== "active") ? <p>部分来源暂停或需要处理；已有授权历史仍可阅读。</p> : null}
      </div><div className="runtime-entry"><label>请求时间口径<select value={entry} onChange={e => { setView({ ...view, entry: e.target.value as Runtime.EntryTime, selected: null }); }}><option value="known">已知进入时间</option><option value="unknown">进入时间未知</option><option value="all">全部已知请求（混合时间口径）</option></select></label>
      <p>{entry === "known" ? "按可信的请求开始时间进入所选期间。" : entry === "unknown" ? "没有可信开始时间，仅观察时间落在期间；单独呈现。" : "包含以上两类请求，不能作为单一开始时间口径的指标。"}</p>
      </div><details><summary>筛选、阅读凭据与来源范围</summary><p>本次筛选：来源 {filters.source_ids.join("、") || "全部授权范围"}；来源项目 {filters.source_projects.join("、") || "全部授权范围"}；费用项目 {filters.cost_projects.join("、") || "全部授权范围"}。</p><Fields rows={[["阅读", <Identifier value={reading.reading_id} label="阅读标识" />], ["成员摘要", reading.membership_ref], ["解释版本", reading.interpretation_revision], ["有效至", time(reading.expires_at, zone)]]} />{reading.coverage.streams.map(s => <p key={[s.source_system_id, s.org_id, s.project_id, s.profile].join(":")}>{s.source_instance} · 组织 {s.org_id} / 项目 {s.project_id} · {s.profile} · {stateName(s.state)}</p>)}</details>
    </section>
    <div className="runtime-primary-comparison">
      <SummaryPanel key={`summary:${entry}`} reading={reading} entry={entry} onInvalidated={onInvalidated} />
      {!capabilities ? <MeasurementsPanel key={`measure:${entry}`} reading={reading} entry={entry} onInvalidated={onInvalidated} /> : null}
    </div>
    <section className="runtime-panel" aria-labelledby="dimensions-title"><h2 id="dimensions-title">{capabilities ? "能力与选中模型" : "运行维度"}</h2><label>分组维度<select value={dimension} onChange={e => setDimension(e.target.value as Runtime.Dimension)}>{Object.entries(dimensionNames).map(([key, name]) => <option key={key} value={key}>{name}</option>)}</select></label>
      <DimensionsPanel key={`${dimension}:${entry}`} reading={reading} entry={entry} dimension={dimension} onInvalidated={onInvalidated} />
    </section>
    <RequestsPanel key={`requests:${entry}`} reading={reading} entry={entry} onSelect={setSelected} onInvalidated={onInvalidated} />
    {selected ? <section className="runtime-panel runtime-detail" aria-labelledby="runtime-detail-title"><div className="runtime-section-heading"><h2 id="runtime-detail-title">请求观察详情</h2><button onClick={() => setSelected(null)}>关闭详情</button></div>
      <RuntimeRequestDetail session={session} reading={reading} request={selected} view={view.detailView ?? "story"} onViewChange={detailView => setView({ ...view, detailView })} onInvalidated={onInvalidated} navigate={navigate} canInvestigate={canInvestigate} />
    </section> : null}
  </>;
}

function SummaryPanel({ reading, entry, onInvalidated }: { reading: Runtime.Reading; entry: Runtime.EntryTime; onInvalidated: (e: unknown) => void }) {
  const [summary, setSummary] = useState<Runtime.Summary | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController(); let live = true;
    setSummary(null); setError(null);
    void runtimeAPI.summary(reading.reading_id, entry, controller.signal).then(s => { if (live) setSummary(s); }).catch(e => { if (!live || isAbortError(e)) return; setError(e); if (invalidReading(e)) onInvalidated(e); });
    return () => { live = false; controller.abort(); };
  }, [reading.reading_id, entry, retry, onInvalidated]);
  const failure = summary?.diagnostic_failure;
  return <section className="runtime-panel" aria-labelledby="summary-title"><h2 id="summary-title">请求结果</h2>{error ? <><p role="alert">{problem(error)}</p><button onClick={() => setRetry(n => n + 1)}>重试请求结果</button></> : !summary ? <p role="status">正在读取请求结果…</p> : <>
    <div className="runtime-metrics"><div><span>本口径独立请求</span><strong>{summary.requests}</strong></div><div><span>技术失败 / 可判断请求</span><strong>{failure!.numerator} / {failure!.denominator}</strong><small>{failure!.state === "computed" && failure!.denominator > 0 ? `${(100 * failure!.numerator / failure!.denominator).toFixed(1)}%` : "不可计算"} · 排除 {failure!.excluded_requests} 个请求</small></div></div>
    <p>分母仅含本地交付完成和明确技术失败；准入拒绝、结果未决等不计入。这是诊断比例，不能代表全局失败率或服务承诺。</p>
    <ul className="runtime-outcomes">{summary.outcomes.map(o => <li key={o.outcome}><span>{outcomeNames[o.outcome] ?? o.outcome}</span><strong>{o.requests}</strong></li>)}</ul>
    {summary.requests === 0 ? <p>当前口径没有可读取的请求；不代表来源没有活动。</p> : null}
  </>}</section>;
}

function DimensionsPanel({ reading, entry, dimension, onInvalidated }: { reading: Runtime.Reading; entry: Runtime.EntryTime; dimension: Runtime.Dimension; onInvalidated: (e: unknown) => void }) {
  const loader = useCallback((cursor: string | undefined, signal: AbortSignal) => runtimeAPI.dimensions(reading.reading_id, dimension, entry, cursor, signal), [reading.reading_id, dimension, entry]);
  const state = usePage(loader, onInvalidated);
  return <>{state.page ? <p>独立请求 {state.page.requests}；有可用维度值 {state.page.requests_with_values}，其余 {state.page.requests_without_values}。分组可重叠，不能相加为请求总数。{state.page.missing_predecessors > 0 ? ` 排除了 ${state.page.missing_predecessors} 条前驱缺失观察。` : ""}</p> : null}
    <p>{dimension.startsWith("selected_") ? "这是当次选择留下的依据，模型引用不一定是显示名称；不表示当前模型目录或调用成功。" : dimension.startsWith("destination_") ? "登记的目的地不证明已经发送或对方已经接收。" : dimension === "enabled_capability" ? "资格状态来自所选来源依据；不把需要某项能力视为已获资格。" : "仅使用本次阅读中完整、已授权的来源字段。"}</p>
    {state.items.length ? <div className="runtime-table-scroll"><table><thead><tr><th scope="col">{dimensionNames[dimension]}</th><th scope="col">来源依据状态</th><th scope="col">独立请求</th><th scope="col">关联主体</th><th scope="col">观察记录</th></tr></thead><tbody>{state.items.map(d => <tr key={JSON.stringify([d.value, d.evidence_state])}><td><code>{d.value}</code></td><td>{stateName(d.evidence_state)}</td><td>{d.requests}</td><td>{d.subjects}</td><td>{d.observation_count}</td></tr>)}</tbody></table></div> : state.page ? <p>当前权限与时间口径下没有可展示的维度值。</p> : null}
    <PageActions state={state} noun="维度" /></>;
}
function MeasurementsPanel({ reading, entry, onInvalidated }: { reading: Runtime.Reading; entry: Runtime.EntryTime; onInvalidated: (e: unknown) => void }) {
  const loader = useCallback((cursor: string | undefined, signal: AbortSignal) => runtimeAPI.measurements(reading.reading_id, entry, cursor, signal), [reading.reading_id, entry]);
  const state = usePage(loader, onInvalidated);
  return <section className="runtime-panel" aria-labelledby="measure-title"><h2 id="measure-title">可比较测量</h2><p>按操作、流模式、测量版本及完成依据分别计算；分组分位数不能取平均。已观察的 0 保留为 0，未知值不补零。</p>
    {state.items.map(g => <article className="runtime-measurement" key={JSON.stringify([g.operation, g.stream_mode, g.measurement_kind, g.measurement_version, g.completion_evidence, g.entry_time_state])}><h3>{operationName(g.operation)} · {measurementName(g.measurement_kind)}</h3><p>{readableState(g.stream_mode)} · {readableState(g.completion_evidence)}</p><Fields rows={[["合格样本", g.percentiles.sample_count], ["P50（毫秒）", g.percentiles.p50 ?? "不可计算"], ["P95（毫秒）", g.percentiles.p95 ?? "不可计算"]]} /><details><summary>排除项与测量口径</summary><Fields rows={[["操作代码",g.operation],["流模式代码",g.stream_mode],["测量代码 / 版本",`${g.measurement_kind} / ${g.measurement_version}`],["完成依据代码",g.completion_evidence],["入场时间口径",g.entry_time_state],["分位数口径 / 版本",`${g.percentiles.state} / ${g.percentiles.version}`],["分位数单位",g.percentiles.unit]]}/><Fields rows={Object.entries(g.excluded).map(([key, count]) => [({ not_applicable: "不适用", not_observed: "未观察", legacy_unknown: "历史未知", invalid: "无效测量", missing_predecessor: "前驱缺失", missing_context: "上下文缺失", conflicting_context: "上下文冲突", incompatible_context: "上下文不可比较" } as Record<string, string>)[key], count])} /></details></article>)}
    {state.page && !state.items.length ? <p>当前口径没有测量记录。</p> : null}<PageActions state={state} noun="测量" /></section>;
}
function RequestsPanel({ reading, entry, onSelect, onInvalidated }: { reading: Runtime.Reading; entry: Runtime.EntryTime; onSelect: (r: Runtime.Request) => void; onInvalidated: (e: unknown) => void }) {
  const loader = useCallback((cursor: string | undefined, signal: AbortSignal) => runtimeAPI.requests(reading.reading_id, entry, cursor, signal), [reading.reading_id, entry]);
  const state = usePage(loader, onInvalidated);
  return <section className="runtime-panel" aria-labelledby="requests-title"><h2 id="requests-title">请求记录</h2>{state.items.length ? <div className="runtime-table-scroll"><table><thead><tr><th scope="col">请求</th><th scope="col">来源 / 组织 / 项目</th><th scope="col">进入时间</th><th scope="col">观察记录</th></tr></thead><tbody>{state.items.map(r => <tr key={r.request_ref}><td><button className="runtime-text-button" onClick={() => onSelect(r)} title={r.request_id}>{compactIdentifier(r.request_id)}</button>{r.request_id.length > 32 ? <Identifier value={r.request_id} label="请求标识" /> : null}</td><td>{r.source_instance} / {r.org_id} / {r.project_id}</td><td>{r.observation_count===0&&r.content_eligible_at?`内容资格 ${time(r.content_eligible_at,reading.period.timezone)}`:r.entry_time_state === "observed" ? time(r.entry_at, reading.period.timezone) : "未知"}</td><td>{r.observation_count===0&&r.content_eligible_at?"内容清单资格；无执行观察":r.observation_count}</td></tr>)}</tbody></table></div> : state.page ? <p>当前口径没有可读取的请求。</p> : null}<PageActions state={state} noun="请求" /></section>;
}

export function RuntimeRequestDetail({ executionContext, session, reading, request, view, onViewChange, onInvalidated, navigate, canInvestigate = false }: { executionContext?: ExecutionContext; session?: AccessSessionResponse; reading: Runtime.Reading; request: Runtime.Request; view: ExecutionView; onViewChange?: (view: ExecutionView) => void; onInvalidated: (e: unknown) => void; navigate?: (path: string) => void; canInvestigate?: boolean }) {
  const contentOnly=request.observation_count===0;
  const active=contentOnly?"contents":view;
  return <><Identifier value={request.request_id} label="请求标识" /><p>{request.source_instance} · 组织 {request.org_id} / 项目 {request.project_id}</p>
    <p role="status">请求结果：{contentOnly ? "无请求级运行观察，结果未核定" : request.outcome ? outcomeNames[request.outcome] ?? "结果未核定" : "结果未核定"}。{request.outcome === "unresolved" ? "现有观察不足以判定最终结果。" : null}</p>
    <p>{contentOnly?"此请求来自已授权内容清单；本次阅读没有执行观察，不据此推定执行状态或结果。":"以下为本次阅读中的授权观察；时间先后不代表因果关系，逐次尝试不增加父级费用。"}</p>
    {onViewChange ? <ViewTabs active={active} onChange={onViewChange} contents={Boolean(session)} contentOnly={contentOnly} /> : null}
    {active==="contents" ? session ? <ContentPanel executionContext={executionContext} session={session} reading={reading} request={request} onInvalidated={onInvalidated} navigate={navigate}/> : <p>请先确认内容读取权限。</p> : <ObservationsPanel key={`${active}:${request.request_ref}`} reading={reading} request={request} mode={active} onInvalidated={onInvalidated} />}
    {session && (active === "story" || active === "systems") ? <ActionWitnessPanel reading={reading} request={request} session={session} onInvalidated={onInvalidated} /> : null}
    {(active === "story" || active === "systems") ? <ResourcesPanel executionContext={executionContext} session={session} navigate={navigate} key={JSON.stringify(["resources", reading.reading_id, reading.membership_ref, request.request_ref])} reading={reading} request={request} onInvalidated={onInvalidated} /> : null}
    {active === "story" ? <AssociationsPanel key={`associations:${request.request_ref}`} reading={reading} request={request} onInvalidated={onInvalidated} navigate={navigate} canInvestigate={canInvestigate} /> : null}
  </>;
}
function ObservationsPanel({ reading, request, onInvalidated, mode = "story" }: { reading: Runtime.Reading; request: Runtime.Request; onInvalidated: (e: unknown) => void; mode?: ExecutionView }) {
  const loader = useCallback(async (cursor: string | undefined, signal: AbortSignal) => {
    const page = await runtimeAPI.observations(reading.reading_id, "observations", request.request_ref, cursor, signal);
    if (page.reading_id !== reading.reading_id || page.items.some(o => o.request_ref !== request.request_ref || o.source_system_id !== request.source_system_id ||
      o.document.source_instance !== request.source_instance || o.document.org_id !== request.org_id || o.document.project_id !== request.project_id || o.document.request_id !== request.request_id)) throw new ApiRequestError(409);
    return page;
  }, [reading.reading_id, request]);
  const state = usePage(loader, onInvalidated);
  const ordered = [...state.items].sort((a, b) => a.document.occurred_at.localeCompare(b.document.occurred_at) || a.document.event_id.localeCompare(b.document.event_id));
  const requestStages = ordered.filter(o => o.chain_state === "complete" && o.document.profile === "unirouter.runtime-observation.v1" && o.document.subject_kind === "request" && o.document.subject_id === request.request_id && o.document.runtime);
  const started = requestStages.some(o => o.document.observation_kind === "request_started");
  const ended = requestStages.some(o => ["request_terminal", "admission_rejected", "local_delivery", "output_held", "request_context_done"].includes(o.document.observation_kind));
  const latestStage = requestStages.at(-1)?.document;
  const allObservationsLoaded = Boolean(state.page && !state.page.next_cursor && !state.error);
  const groups = new Map<string, Runtime.Observation[]>();
  for (const o of ordered) { const key = `${o.document.subject_kind}:${o.document.subject_id}`; groups.set(key, [...(groups.get(key) ?? []), o]); }
  return <>
    {mode === "story" ? <section aria-label="请求阶段说明"><h3>请求阶段</h3>
      <p>本次冻结阅读的请求级结果：{request.outcome ? outcomeNames[request.outcome] ?? "结果未核定" : "结果未核定"}。只按已授权、完整的请求级观察判断；客户端错误正文和逐次尝试不替代请求终态。</p>
      {started && allObservationsLoaded && !ended ? <p>已记录请求开始，但缺少结束证据；本次结果仍未决，不能据此认定提供方失败。</p> : null}
      {started && !allObservationsLoaded ? <p>已记录请求开始；观察尚未完整加载，暂不能判断是否有结束证据。</p> : null}
      {!started && allObservationsLoaded ? <p>本次阅读没有完整的请求开始观察；不据此推定请求未发生。</p> : null}
      {latestStage?.runtime ? <p>最近一条已加载请求级观察为“{observationNames[latestStage.observation_kind] ?? latestStage.observation_kind}”；该时点提供方状态为“{readableState(latestStage.runtime.provider)}”。此状态只对应这一观察时点，不代表后续没有调用。</p> : null}
      <p>交互内容需按原权限主动查看；保存的客户端请求或错误响应不证明模型调用、工具动作或财务结果。</p>
    </section> : null}
    {mode === "graph" ? <section aria-label="请求的全部可见分支"><h3>全部已加载的来源身份分组</h3><p>分组仅表示同一请求与尝试身份；B 观察没有声明因果边时不连线，也不从时间推断并行或串行。折叠不改变成员或费用。</p>
      {[...groups].map(([key, items]) => <details key={key}><summary>{key} · {items.length} 条观察</summary>{items.map(o => <ObservationCard key={o.document.event_id} observation={o} timezone={reading.period.timezone} />)}</details>)}
    </section> : mode === "proof" ? <section aria-label="运行结构化依据"><h3>结构化授权视图</h3><p>摘要标识原始来源记录；当前投影视图已按权限收缩，不能用它重算原始字节摘要。</p>{ordered.map(o => <details key={o.document.event_id}><summary>{o.document.event_id}</summary><pre>{JSON.stringify(o, null, 2)}</pre></details>)}</section> :
      <>{mode === "systems" ? <><h3>逐次尝试目的地</h3><p>目的地登记、输入引用、发送观察与远端接收分别解释，缺少观察不能推定未发送。</p></> : null}
        {ordered.filter(o => mode !== "systems" || o.document.profile === "unirouter.destination-observation.v1").map(o => <ObservationCard key={`${o.source_system_id}:${o.document.event_id}`} observation={o} timezone={reading.period.timezone} />)}</>}
    {state.page?.next_cursor ? <p>仍有观察未加载；当前列表不代表完整过程。</p> : null}<PageActions state={state} noun="观察" />
  </>;
}

export function ObservationCard({ observation: o, timezone }: { observation: Runtime.Observation; timezone: string }) {
  const d = o.document;
  const kind = d.capability ? "能力选择" : d.attempt ? "逐次尝试" : d.policy ? "政策观察" : d.destination ? "目的地观察" : "运行观察";
  return <article className="runtime-observation"><header><h3 title={d.subject_id}>{kind} · {compactIdentifier(d.subject_id)}</h3><span>{time(d.occurred_at, timezone)}</span></header>
    <p>{observationNames[d.observation_kind] ?? "未识别的观察类型，见原始字段"} · 修订 {d.revision}</p>
    {o.chain_state !== "complete" ? <p>前驱尚未收齐，字段不作为完整事实展示或汇总依据。</p> : <>
      {d.runtime ? <Fields rows={[["操作",operationName(d.runtime.operation)],["流模式",readableState(d.runtime.stream_mode)],["准入",readableState(d.runtime.admission)],["提供方响应",readableState(d.runtime.provider)],["本地交付",readableState(d.runtime.local_delivery)],["财务状态",readableState(d.runtime.financial)],["上下文结束原因",readableState(d.runtime.context_done_reason)]]}/> : null}
      {d.attempt ? <><Fields rows={[["操作",operationName(d.attempt.operation)],["路由序号 / 总数",`${d.attempt.route_sequence ?? "未知"} / ${d.attempt.route_total ?? "未知"}`],["提供方响应",readableState(d.attempt.provider)],["HTTP 状态",d.attempt.http_status],["允许重试",d.attempt.retry_authorized?"是":"否"],["本地交付",readableState(d.attempt.local_delivery)],["财务状态",readableState(d.attempt.financial)]]}/><p>一次尝试失败不等于请求失败；财务状态不提供可加总金额。</p></> : null}
      {d.capability ? <Fields rows={[["选中模型",d.capability.model_ref],["选中提供方",d.capability.provider],["操作",operationName(d.capability.operation)],["来源依据",readableState(d.capability.evidence_state)],["所需能力",d.capability.required_capabilities.length?`已声明 ${d.capability.required_capabilities.length} 项，详见原始字段` : "未声明"]]}/> : null}
      {d.policy ? <Fields rows={[["阶段",readableState(d.policy.stage)],["模式",readableState(d.policy.mode)],["配置动作",readableState(d.policy.configured_action)],["结果",readableState(d.policy.outcome)],["效果",d.policy.effects.length?`已报告 ${d.policy.effects.length} 项，详见原始字段`:"未观察"],["降级",d.policy.degraded?"是":"否"],["强制要求",d.policy.mandatory?"是":"否"],["来源依据",readableState(d.policy.evidence_state)]]}/> : null}
      {d.destination ? <><Fields rows={[["提供方",d.destination.provider],["目的地",d.destination.origin],["区域",d.destination.region],["区域来源",readableState(d.destination.region_source)],["发送阶段",readableState(d.destination.dispatch)],["政策依据",readableState(d.destination.policy_evidence)],["输入绑定",readableState(d.destination.input_binding_state)]]}/><p>目的地登记不等于已发送或已接收；不从地址猜测区域。</p></> : null}
      {d.measurement ? <Fields rows={[["测量",measurementName(d.measurement.kind)],["测量状态",readableState(d.measurement.state)],["原始值",d.measurement.value ?? "未知"],["单位",d.measurement.unit==="nanoseconds"?"纳秒":"单位待确认"]]}/> : null}
      <details><summary>原始观察字段与口径</summary><Fields rows={[["观察类型代码",d.observation_kind],["采集配置",d.profile]]}/>
      {d.runtime ? <Fields rows={[["操作", d.runtime.operation], ["流模式", d.runtime.stream_mode], ["准入", d.runtime.admission], ["提供方响应", d.runtime.provider], ["本地交付", d.runtime.local_delivery], ["财务状态", d.runtime.financial], ["上下文结束原因", d.runtime.context_done_reason]]} /> : null}
      {d.attempt ? <><Fields rows={[["尝试 ID", d.attempt.attempt_id], ["操作", d.attempt.operation], ["路由序号 / 总数", `${d.attempt.route_sequence ?? "未知"} / ${d.attempt.route_total ?? "未知"}`], ["提供方响应", d.attempt.provider], ["HTTP 状态", d.attempt.http_status], ["允许重试", d.attempt.retry_authorized ? "是" : "否"], ["本地交付", d.attempt.local_delivery], ["财务状态", d.attempt.financial]]} /></> : null}
      {d.capability ? <><Fields rows={[["选中模型引用", d.capability.model_ref], ["选中提供方", d.capability.provider], ["操作", d.capability.operation], ["来源依据", d.capability.evidence_state], ["协议", `${d.capability.protocol.id} / ${d.capability.protocol.version}`], ["执行配置", `${d.capability.execution_profile.id} / ${d.capability.execution_profile.version}`], ["所需能力", d.capability.required_capabilities.join("、") || "未声明"], ["发布引用", d.capability.release_ref], ["绑定引用", d.capability.binding_ref], ["验证引用", d.capability.probe_ref], ["批准引用", d.capability.approval_ref]]} />{d.capability.capabilities.map((c, index) => <div className="runtime-capability" key={`${c.capability_id}:${index}`}><strong>{c.capability_id}</strong> · {c.support_state} · {c.evidence_state}<p>核验于 {time(c.verified_at, timezone)} · 修订 {c.revision}</p><details><summary>依据引用摘要</summary><code>{c.evidence_ref_sha256}</code><p>这是引用文本的摘要，不是被引用制品的校验值。</p></details></div>)}</> : null}
      {d.policy ? <Fields rows={[["政策 / 版本", `${d.policy.policy_ref} / ${d.policy.policy_version}`], ["检测器 / 版本", `${d.policy.detector_ref} / ${d.policy.detector_version}`], ["阶段", d.policy.stage], ["模式", d.policy.mode], ["配置动作", d.policy.configured_action], ["结果", d.policy.outcome], ["效果", d.policy.effects.join("、") || "未观察"], ["降级", d.policy.degraded ? "是" : "否"], ["强制要求", d.policy.mandatory ? "是" : "否"], ["来源依据", d.policy.evidence_state]]} /> : null}
      {d.destination ? <><Fields rows={[["提供方", d.destination.provider], ["目的地", d.destination.origin], ["区域", d.destination.region], ["区域来源", d.destination.region_source], ["发送阶段", d.destination.dispatch], ["政策依据", d.destination.policy_evidence], ["输入绑定", d.destination.input_binding_state]]} /></> : null}
      {d.measurement ? <Fields rows={[["测量", d.measurement.kind], ["测量状态", d.measurement.state], ["原始值", d.measurement.value ?? "未知"], ["单位", d.measurement.unit], ["起始边界", d.measurement.start_boundary], ["结束边界", d.measurement.end_boundary]]} /> : null}
      </details>
    </>}
    <details><summary>来源与修订证据</summary><Identifier value={d.subject_id} label="主体标识" /><Fields rows={[["来源实例", d.source_instance], ["来源 ID", o.source_system_id], ["组织 / 项目", `${d.org_id} / ${d.project_id}`], ["来源事实", d.source_fact_id], ["事件", d.event_id], ["来源摘要", o.source_hash], ["信封摘要", o.envelope_hash], ["记录时间", time(d.recorded_at, timezone)], ["修订原因", d.correction_reason]]} /></details>
  </article>;
}

function CurrentPanel({ filters, timezone }: { filters: Runtime.ReadingRequest["filters"]; timezone: string }) {
  const loader = useCallback(async (cursor: string | undefined, signal: AbortSignal) => {
    const sentAt = performance.now();
    const page = await runtimeAPI.current(filters, cursor, signal);
    // Each page has its own server evaluation and request start. Charging the
    // whole round trip is conservative and does not depend on the host clock.
    return { ...page, items: page.items.map(item => ({ ...item, deadline: sentAt + Date.parse(item.entry.valid_until ?? "") - Date.parse(page.evaluated_at) })) };
  }, [filters]);
  const noHistoryInvalidation = useCallback(() => {}, []);
  const state = usePage(loader, noHistoryInvalidation);
  const [, updateClock] = useState(0);
  const now = performance.now();
  useEffect(() => {
    const next = Math.min(...state.items.map(i => i.deadline).filter(t => t > now));
    if (!Number.isFinite(next)) return;
    const timer = window.setTimeout(() => updateClock(n => n + 1), Math.min(Math.max(0, next - now + 1), 2_147_483_647));
    return () => window.clearTimeout(timer);
  }, [state.items, state.page, now]);
  return <div className="runtime-current"><p>本次租约筛选：来源 {filters.source_ids.join("、") || "全部授权范围"}；来源项目 {filters.source_projects.join("、") || "全部授权范围"}；费用项目 {filters.cost_projects.join("、") || "全部授权范围"}。</p><p>各页分别观察，不能把已加载条数当成同一时刻的全局总量。</p>{state.page ? <><p>最近判定时间 {time(state.page.evaluated_at, timezone)}。租约到期会降为未确认；需要刷新才能得到新的来源观察。</p><button disabled={state.busy} onClick={state.refresh}>刷新租约观察</button></> : null}
    {state.error && state.items.length > 0 ? <p>刷新未完成，保留的记录仅供查看，不能确认当前执行状态。</p> : null}
    {state.items.map(i => {
      const active = ["lease_valid_execution", "leased_pending"].includes(i.current_state);
      const effective = active && (state.busy || state.error || !Number.isFinite(i.deadline) || !(i.deadline > now)) ? "not_current" : i.current_state;
      return <article className="runtime-current-item" key={i.ref}><h3>{i.entry.request_id} · {stateName(effective)}</h3><p>{i.source_instance} · 组织 {i.org_id} / 项目 {i.project_id} · {i.entry.subject_id}</p><Fields rows={[["操作", i.entry.operation], ["来源原状态", stateName(i.entry.original_state)], ["来源观察时间", time(i.snapshot_observed_at, timezone)], ["最后活动观察", time(i.entry.last_observed_at, timezone)], ["租约有效至", time(i.entry.valid_until, timezone)]]} /></article>;
    })}
    {state.page && !state.items.length ? <p>当前没有可展示的租约观察；可能尚未采集或不在授权范围，不能据此断言没有运行中的请求。</p> : null}<PageActions state={state} noun="租约观察" />
  </div>;
}

function AssociationsPanel({ reading, request, onInvalidated, navigate, canInvestigate }: { reading: Runtime.Reading; request: Runtime.Request; onInvalidated: (e: unknown) => void; navigate?: (path: string) => void; canInvestigate: boolean }) {
 const loader = useCallback(async (cursor: string | undefined, signal: AbortSignal) => {
   const page = await runtimeAPI.associations(reading.reading_id, request.request_ref, cursor, signal);
   if (page.reading_id !== reading.reading_id || page.request_ref !== request.request_ref || page.membership_ref !== reading.membership_ref || page.associations_version !== "xinsight.runtime.associations.v1" || page.basis !== "same_source_request_identity") throw new ApiRequestError(409);
   return page;
 }, [reading.reading_id, reading.membership_ref, request.request_ref]);
 const state = usePage(loader, onInvalidated);
 return <section aria-label="请求来源关联"><h3>同一请求的来源与经济记录</h3><p>按来源、实例、组织、项目和请求身份关联，采用本次阅读冻结的授权资料。关联本身不证明输入内容、路由或因果关系，也不另计费用。</p>
 {state.items.map(item => item.kind === "source" ? <SourceFactCard key={`${item.source.source_system_id}:${item.source.source_fact_id}`} fact={item.source} /> : <section className="source-fact-card" key={item.economic.economic_fact_id}><h4>已有经济事实</h4><Fields rows={[["经济事实", item.economic.economic_fact_id], ["执行", item.economic.execution_id], ["来源摘要", item.economic.source_event_hash]]} />
 {canInvestigate && navigate ? <button onClick={() => navigate(`/executions/${encodeURIComponent(item.economic.execution_id)}?as_of=${encodeURIComponent(reading.cutoff)}`)}>查看执行与来源轨迹</button> : null}</section>)}
 {state.page && state.items.length === 0 ? <p>本次阅读没有可展示的关联资料；不代表来源不存在或费用为零。</p> : null}<PageActions state={state} noun="关联资料" /></section>;
}


const resourceInputNames: Record<Runtime.ResourceItem["input_state"], string> = {
  not_confirmed: "未确认进入模型输入", reference_matched: "输入引用相符", reference_mismatch: "输入引用不匹配",
  conflicting_references: "输入引用存在冲突", withdrawn: "来源已撤回", reassessment_required: "有后续修订，需要重新评估", missing_predecessor: "前驱缺失，关联未确认",
};
const resourceNames: Record<string, string> = { source_audit: "来源审计声明", client_assertion: "客户端声明", enterprise_service: "企业服务", pc: "个人电脑",
  known: "已提供", unknown: "未知", managed: "已纳管", unmanaged: "未纳管", read: "读取", search: "检索", download: "下载", denied: "被拒绝", failed: "失败",
  allow: "允许", deny: "拒绝", provider_attempt: "提供方尝试", media_attempt: "媒体尝试", extract: "提取", summarize: "摘要", redact: "脱敏", translate: "翻译", truncate: "截断" };
const resourceName = (value: string) => resourceNames[value] ?? value;
function resourceSelectionKey(item: Runtime.ResourceItem) {
  const { reporter, record_ref } = item.record;
  return JSON.stringify([reporter.source_system_id, reporter.source_instance, reporter.org_id, reporter.project_id, record_ref]);
}
function ResourcesPanel({ executionContext,session,navigate,reading, request, onInvalidated }: { executionContext?:ExecutionContext; session?:AccessSessionResponse; navigate?:(path:string)=>void; reading: Runtime.Reading; request: Runtime.Request; onInvalidated: (e: unknown) => void }) {
  const loader = useCallback(async (cursor: string | undefined, signal: AbortSignal) => {
    const page = await runtimeAPI.resources(reading.reading_id, request.request_ref, cursor, signal);
    if (page.reading_id !== reading.reading_id || page.request_ref !== request.request_ref || page.membership_ref !== reading.membership_ref ||
        page.resources_version !== "xinsight.runtime.resources.v1" || page.coverage !== "synthetic_internal_records_only") throw new ApiRequestError(409);
    for (const item of page.items) {
      const record = item.record;
      if (record.schema_version !== "xinsight.resource-access.internal.v1" || record.synthetic !== true || item.content_verification !== "not_performed" ||
          item.input_evidence_basis !== "frozen_reference_witnesses" || record.request_id !== request.request_id ||
          ["source_system_id", "source_instance", "org_id", "project_id"].some(key => record.target[key as keyof Runtime.ResourceScope] !== request[key as keyof Runtime.ResourceScope])) throw new ApiRequestError(409);
    }
    return page;
  }, [reading.reading_id, reading.membership_ref, request]);
  const state = usePage(loader, onInvalidated);
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const selected = state.items.find(item => resourceSelectionKey(item) === selectedKey);
  useEffect(() => { if (selectedKey && !selected) setSelectedKey(null); }, [selectedKey, selected]);
  return <section aria-label="系统与数据"><h3>系统与数据</h3>
    <p>当前仅包含明确标注的合成验证记录。资源位置、所有权、来源读取权限和对外发送许可分别判断；来源声明与引用相符都不能证明实际内容或脱敏效果。</p>
    <div className="runtime-resource-workspace">
      <div className="runtime-resource-list">
        {state.items.length ? <div className="runtime-table-scroll" role="region" aria-label="资源清单，可横向滚动" tabIndex={0}><table>
          <caption>本次阅读的资源清单 · 合成验证资料</caption>
          <thead><tr><th scope="col">资源 / 版本</th><th scope="col">位置</th><th scope="col">操作 / 来源结果</th><th scope="col">来源读取许可</th><th scope="col">敏感性分类</th><th scope="col">输入关联</th><th scope="col">修订提醒</th></tr></thead>
          <tbody>{state.items.map(item => {
            const record = item.record, key = resourceSelectionKey(item);
            return <tr key={key} aria-selected={selectedKey === key}>
              <td><button type="button" className="runtime-text-button" aria-pressed={selectedKey === key} onClick={() => setSelectedKey(key)}>{record.resource.name}</button><br /><small>{record.resource.version} · 记录修订 {record.revision}</small></td>
              <td>{resourceName(record.resource.location)}</td>
              <td>{resourceName(record.action)} / {resourceName(record.access_outcome)}</td>
              <td>{resourceName(record.access_decision)}</td>
              <td>{record.resource.classification === "known" ? record.resource.classification_ref || "分类依据未提供" : "未知"}</td>
              <td>{resourceInputNames[item.input_state]}</td>
              <td>{item.changed_since_reading ? "存在后续修订，保留本次阅读" : "未提示后续修订"}{record.withdrawn ? "；本次记录已撤回" : ""}{item.chain_state !== "complete" ? "；前驱缺失" : ""}</td>
            </tr>;
          })}</tbody>
        </table></div> : null}
        {state.page && !state.items.length ? <p>本次阅读没有可展示的资源记录；不代表没有读取或发送数据。</p> : null}
        <PageActions state={state} noun="资源记录" />
      </div>
      <aside className="runtime-resource-detail" aria-label="所选资源属性">
        {!selected ? <p>选择一条资源查看属性、依据与复核入口。</p> : [selected].map(item => {
      const record = item.record, object = record.resource;
      return <article className="source-fact-card" key={JSON.stringify([resourceSelectionKey(item), record.revision, item.record_hash])}>
        <h4>{object.name} · 合成验证</h4><p><strong>{resourceInputNames[item.input_state]}</strong></p>
        {item.changed_since_reading ? <p>保留本次阅读的原始记录。后续修订未加入本次阅读；请重新读取后评估当前关联。</p> : null}
        <Fields rows={[["来源声明", resourceName(record.report_kind)], ["应用 / 操作者", `${record.application_ref} / ${record.actor_ref}`],
          ["资源位置", resourceName(object.location)], ["资源 / 版本", `${object.ref} / ${object.version}`],
          ["所有权", object.ownership === "known" ? object.owner_ref : "未知"], ["敏感性分类", object.classification === "known" ? object.classification_ref : "未知"],
          ["设备纳管", resourceName(object.device_management)], ["操作 / 来源结果", `${resourceName(record.action)} / ${resourceName(record.access_outcome)}`],
          ["来源读取许可", resourceName(record.access_decision)], ["许可依据 / 策略版本", record.decision_ref ? `${record.decision_ref} / ${record.policy_version}` : "未知"],
          ["外发许可", "未由本记录证明"], ["内容与转换效果核验", "未执行独立核验"], ["发生时间", time(record.occurred_at, reading.period.timezone)]]} />
        {record.access_outcome === "denied" ? <p>本次读取被拒绝，不能据此排除历史副本或其他路径的使用。</p> : null}
        {record.input ? <><Fields rows={[["声明片段 / 版本", `${record.input.fragment_ref} / ${record.input.fragment_version}`],
          ["声明输入引用", record.input.reported_input_ref], ["目标尝试", `${resourceName(record.input.attempt_kind)} / ${record.input.attempt_id}`]]} />
          <p>来源报告的转换过程；尚未核验内容：</p>{record.input.transforms.length ? <ol>{record.input.transforms.map((step, i) => <li key={i}>{resourceName(step.kind)}：{step.input_version} → {step.output_version}；依据 {step.receipt_ref}</li>)}</ol> : <p>未报告转换步骤。</p>}
        </> : null}
        {item.captured_input_evidence_refs.length ? <p>冻结的输入引用比对依据：{item.captured_input_evidence_refs.join("、")}</p> : null}
        <details><summary>记录身份与修订依据</summary><Fields rows={[["来源", record.reporter.source_system_id], ["来源范围", `${record.reporter.source_instance} / ${record.reporter.org_id} / ${record.reporter.project_id}`],
          ["记录 / 修订", `${record.record_ref} / ${record.revision}`], ["摘要", item.record_hash], ["前驱摘要", record.previous_hash || "首版"],
          ["修订链", item.chain_state === "complete" ? "完整" : "前驱缺失"], ["本次记录已撤回", record.withdrawn ? "是" : "否"]]} /></details>
        {session&&navigate?<ReviewStart key={executionContext ? JSON.stringify(executionContext) : "resource-only"} executionContext={executionContext} session={session} navigate={navigate} available={{evidence:[],gaps:[],resources:[{scope:"finding",reporter:record.reporter,target:record.target,request_id:record.request_id,record_ref:record.record_ref,revision:record.revision,record_hash:item.record_hash,relation:"context"}]}}/>:null}
      </article>;
    })}</aside>
    </div>
  </section>;
}
