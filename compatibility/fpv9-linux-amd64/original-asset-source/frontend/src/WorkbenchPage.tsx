import { authorizedNavigationReading, fixedReadingRequest, navigationOwner, type WorkbenchNavigation } from "./workbenchNavigation";
import { Identifier, compactIdentifier } from "./Identifier";
import { SourceCollectionStatus } from "./SourceCollectionStatus";
import { PurposePanel, purposeLabels } from "./PurposePanel";
import { SourceFactsPanel, SourceFactCard } from "./SourceFactsPanel";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import {
  ApiRequestError,
  accessAPI,
  type AccessSessionResponse,
  type WorkbenchContribution,
  type WorkbenchCoverage,
  type WorkbenchContributions,
  type WorkbenchDetails,
  type WorkbenchMoneyBasis,
  type WorkbenchReading,
  type WorkbenchReadingRequest,
  type WorkbenchSavedFilter,
  type WorkbenchSeries,
  type WorkbenchSourceStatuses,
  type WorkbenchSummary,
  scopeSessionKey,
  workbenchAPI,
} from "./api";

type WorkbenchDimension = "model" | "source_project" | "cost_project" | "application" | "account" | "purpose";
function usageDimension(reading: WorkbenchReading, summary: WorkbenchSummary): WorkbenchDimension {
  if (reading.dimensions.includes("purpose")) return "purpose";
  if (summary.actor_known_requests > 0 && reading.dimensions.includes("account")) return "account";
  return "source_project";
}

// Transient navigation state, never persisted as a source of authority.
export interface WorkbenchSnapshot {
  subject: string;
  permissions: string;
  config: WorkbenchReadingRequest;
  appliedConfig: WorkbenchReadingRequest;
  reading: WorkbenchReading;
  summary: WorkbenchSummary;
  contributions: WorkbenchContributions;
  series: WorkbenchSeries;
  sources: WorkbenchSourceStatuses | null;
  details: WorkbenchDetails | null;
  selected: WorkbenchContribution | null;
  dimension: WorkbenchDimension;
  contributionMode: "top5" | "top10" | "all";
  resolution: "overview" | "hourly";
  activeBucket: string;
  saved: WorkbenchSavedFilter[];
  saveName: string;
  sourcesUnavailable: boolean;
  preferencesUnavailable: boolean;
  sourcesNotice: string;
  preferencesNotice: string;
}

const baseRequest: WorkbenchReadingRequest = {
  contract_version: "xinsight.workbench.v1",
  page: "cost",
  metric_version: "xinsight.workbench.cost.v1",
  period: {
    relative_hours: 24,
    display_timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC",
  },
  money_basis: "gateway_charged",
  currency: "CNY",
  filters: { source_ids: [], source_projects: [], cost_projects: [] },
};

export function WorkbenchPage({ session, navigate, snapshot, onSnapshot, initialConfig, navigation, navigationInvalid = false, onNavigation, usage = false }: {
  session: AccessSessionResponse;
  initialConfig?: WorkbenchReadingRequest;
  navigation?: WorkbenchNavigation;
  navigationInvalid?: boolean;
  onNavigation?: (value: WorkbenchNavigation) => void;
  navigate: (path: string) => void;
  snapshot?: WorkbenchSnapshot | null;
  onSnapshot?: (snapshot: WorkbenchSnapshot | null) => void;
  usage?: boolean;
}) {
  const initialRequest=useRef(navigation?.request ?? initialConfig ?? baseRequest);
  const initialNavigation=useRef(navigation);
  const navigationSink=useRef(onNavigation); navigationSink.current=onNavigation;
  const contributionPages=useRef(1), detailPages=useRef(1);
  const identity=scopeSessionKey(session);
  const [basis,setBasis]=useState<WorkbenchMoneyBasis>(initialRequest.current.money_basis);
  const [config,setConfig]=useState<WorkbenchReadingRequest>(initialRequest.current);
  const [appliedConfig,setAppliedConfig]=useState<WorkbenchReadingRequest>(initialRequest.current);
  const [reading,setReading]=useState<WorkbenchReading|null>(null);
  const [summary,setSummary]=useState<WorkbenchSummary|null>(null);
  const [contributions,setContributions]=useState<WorkbenchContributions|null>(null);
  const [series,setSeries]=useState<WorkbenchSeries|null>(null);
  const [sources,setSources]=useState<WorkbenchSourceStatuses|null>(null);
  const [details,setDetails]=useState<WorkbenchDetails|null>(null);
  const [selected,setSelected]=useState<WorkbenchContribution|null>(null);
  const [dimension,setDimension]=useState<WorkbenchDimension>("source_project");
  const [contributionMode,setContributionMode]=useState<"top5"|"top10"|"all">("top10");
  const [saved,setSaved]=useState<WorkbenchSavedFilter[]>([]);
  const [sourcesUnavailable,setSourcesUnavailable]=useState(false);
  const [preferencesUnavailable,setPreferencesUnavailable]=useState(false);
  const [sourcesNotice,setSourcesNotice]=useState("");
  const [preferencesNotice,setPreferencesNotice]=useState("");
  const [saveName,setSaveName]=useState("我的费用视图");
  const [busy,setBusy]=useState(false);const [error,setError]=useState("");
  const requestSequence=useRef(0);
  const viewSequence=useRef(0);

  const [resolution,setResolution]=useState<"overview"|"hourly">("overview");
  const [activeBucket,setActiveBucket]=useState("");
  const [canRetry,setCanRetry]=useState(false);
  const retryAction=useRef<(()=>void)|null>(null);
  const initialSnapshot=useRef(snapshot);
  const snapshotSink=useRef(onSnapshot);
  snapshotSink.current=onSnapshot;
  const previewRef=useRef<HTMLElement>(null);

  function clearReading(){snapshotSink.current?.(null);setReading(null);setSummary(null);setContributions(null);setSeries(null);setSources(null);setDetails(null);setSelected(null);setActiveBucket("")}
  const loadReading=useCallback(async(request:WorkbenchReadingRequest)=>{
    const sequence=++requestSequence.current;++viewSequence.current;
    setBusy(true);setError("");setCanRetry(false);clearReading();
    contributionPages.current=1;detailPages.current=1;
    setSourcesUnavailable(false);setPreferencesUnavailable(false);setSourcesNotice("");setPreferencesNotice("");
    try{
      const next=await workbenchAPI.createReading(request,session.csrf_token);
      const sum=await workbenchAPI.summary(next.reading_id);
      if(sequence!==requestSequence.current)return;
      const initialDimension=usage?usageDimension(next,sum):"source_project";
      const [groups,line]=await Promise.all([
        workbenchAPI.contributions(next.reading_id,initialDimension,usage?{topN:10,orderBy:"requests"}:{topN:10}),
        usage?workbenchAPI.series(next.reading_id,undefined,overviewSeconds(next),"requests"):workbenchAPI.series(next.reading_id,undefined,overviewSeconds(next)),
      ]);
      const [sourceResult,savedResult]=await Promise.allSettled([workbenchAPI.sources(next.reading_id),workbenchAPI.savedFilters()]);
      if(sequence!==requestSequence.current)return;
      if(sourceResult.status==="rejected"&&invalidatesReading(sourceResult.reason)&&!(sourceResult.reason instanceof ApiRequestError&&sourceResult.reason.status===403&&sourceResult.reason.problem?.code!=="workbench_scope_denied"))throw sourceResult.reason;
      if(savedResult.status==="rejected"&&savedResult.reason instanceof ApiRequestError&&savedResult.reason.status===401)throw savedResult.reason;
      setConfig(request);setAppliedConfig(request);setBasis(request.money_basis);setDimension(initialDimension);setContributionMode("top10");setResolution("overview");
      setReading(next);setSummary(sum);setContributions(groups);setSeries(line);
      if(sourceResult.status==="fulfilled")setSources(sourceResult.value);
      else{setSourcesUnavailable(true);setSourcesNotice(optionalFailureText(sourceResult.reason,"来源级状态"))}
      if(savedResult.status==="fulfilled")setSaved(savedResult.value.items);
      else{setSaved([]);setPreferencesUnavailable(true);setPreferencesNotice(optionalFailureText(savedResult.reason,"个人筛选"))}
    }catch(failure){if(sequence===requestSequence.current)setError(problemText(failure))}
    finally{if(sequence===requestSequence.current)setBusy(false)}
  },[session.csrf_token,identity,usage]);
  useEffect(()=>{
    const savedView=initialSnapshot.current;
    const retained=initialNavigation.current;
    const controller=new AbortController();
    if(navigationInvalid){clearReading();setError("阅读导航引用已失效，请明确重新建立阅读。");}
    else if(retained){
      const sequence=++requestSequence.current;
      setBusy(true);setError("");clearReading();
      void (async()=>{
        const next=await authorizedNavigationReading(retained,session,controller.signal);
        const request=fixedReadingRequest(next,retained.request);
        let groups=await workbenchAPI.contributions(next.reading_id,retained.dimension,retained.mode==="all"?(usage?{limit:5,orderBy:"requests"}:{limit:5}):(usage?{topN:retained.mode==="top5"?5:10,orderBy:"requests"}:{topN:retained.mode==="top5"?5:10}),controller.signal);
        for(let page=1;retained.mode==="all"&&page<retained.contributionPages;page++){
          if(!groups.next_cursor)throw new Error("navigation_access_changed");
          const more=await workbenchAPI.contributions(next.reading_id,retained.dimension,usage?{limit:5,cursor:groups.next_cursor,orderBy:"requests"}:{limit:5,cursor:groups.next_cursor},controller.signal);
          if(more.denominator_micros!==groups.denominator_micros||more.unassigned_micros!==groups.unassigned_micros)throw new Error("navigation_access_changed");
          groups={...more,rows:[...groups.rows,...more.rows]};
        }
        const selected=retained.selectedRef?groups.rows.find(row=>row.ref===retained.selectedRef):null;
        if(retained.selectedRef&&!selected)throw new Error("navigation_access_changed");
        let detail=selected?await workbenchAPI.details(next.reading_id,selected.ref,undefined,controller.signal):null;
        for(let page=1;detail&&page<retained.detailPages;page++){
          if(!detail.next_cursor)throw new Error("navigation_access_changed");
          const more=await workbenchAPI.details(next.reading_id,detail.contribution_ref,detail.next_cursor,controller.signal);
          detail={...more,rows:[...detail.rows,...more.rows]};
        }
        const [sum,line]=await Promise.all([workbenchAPI.summary(next.reading_id,controller.signal),usage?workbenchAPI.series(next.reading_id,controller.signal,retained.resolution==="hourly"?3600:overviewSeconds(next),"requests"):workbenchAPI.series(next.reading_id,controller.signal,retained.resolution==="hourly"?3600:overviewSeconds(next))]);
        const [sourceResult,savedResult]=await Promise.allSettled([workbenchAPI.sources(next.reading_id,controller.signal),workbenchAPI.savedFilters(controller.signal)]);
        if(sourceResult.status==="rejected"&&invalidatesReading(sourceResult.reason)&&!(sourceResult.reason instanceof ApiRequestError&&sourceResult.reason.status===403&&sourceResult.reason.problem?.code!=="workbench_scope_denied"))throw sourceResult.reason;
        if(savedResult.status==="rejected"&&savedResult.reason instanceof ApiRequestError&&savedResult.reason.status===401)throw savedResult.reason;
        if(controller.signal.aborted||sequence!==requestSequence.current)return;
        if(!(Date.parse(next.expires_at)>Date.now()))throw new Error("navigation_access_changed");
        contributionPages.current=retained.contributionPages;detailPages.current=retained.detailPages;
        setConfig(request);setAppliedConfig(request);setBasis(next.money_basis);setReading(next);setSummary(sum);setContributions(groups);setSeries(line);
        setSelected(selected??null);setDetails(detail);setDimension(retained.dimension);setContributionMode(retained.mode);setResolution(retained.resolution);setActiveBucket(retained.activeBucket);
        setSources(sourceResult.status==="fulfilled"?sourceResult.value:null);setSaved(savedResult.status==="fulfilled"?savedResult.value.items:[]);
        setSourcesUnavailable(sourceResult.status==="rejected");setPreferencesUnavailable(savedResult.status==="rejected");
        setSourcesNotice(sourceResult.status==="rejected"?optionalFailureText(sourceResult.reason,"来源级状态"):"");
        setPreferencesNotice(savedResult.status==="rejected"?optionalFailureText(savedResult.reason,"个人筛选"):"");
      })().catch(failure=>{if(sequence!==requestSequence.current||controller.signal.aborted)return;clearReading();setError(failure instanceof Error&&failure.message==="navigation_access_changed"?"权限或阅读已失效，不能恢复先前视图。":problemText(failure));}).finally(()=>{if(sequence===requestSequence.current&&!controller.signal.aborted)setBusy(false)});
    }
    else if(!savedView){void loadReading(initialRequest.current)}
    else{
      const sequence=++requestSequence.current;
      setBusy(true);
      // Recheck both the product role and the reading's current grants before
      // putting any retained values back into the DOM.
      void Promise.all([accessAPI.session(),workbenchAPI.reading(savedView.reading.reading_id)]).then(([current,next])=>{
        if(sequence!==requestSequence.current)return;
        const sameIdentity=current.session.subject_id===savedView.subject&&[...current.session.permissions].sort().join(",")===savedView.permissions;
        const sameReading=["reading_id","membership_ref","source_manifest_ref","authorization_revision","interpretation_revision","contract_version","metric_version"].every(key=>next[key as keyof WorkbenchReading]===savedView.reading[key as keyof WorkbenchReading]);
        if(!sameIdentity||!sameReading)throw new Error("navigation_access_changed");
        setConfig(savedView.config);setAppliedConfig(savedView.appliedConfig);setBasis(next.money_basis);
        setReading(next);setSummary(savedView.summary);setContributions(savedView.contributions);setSeries(savedView.series);
        setSources(savedView.sources);setDetails(savedView.details);setSelected(savedView.selected);
        setDimension(savedView.dimension);setContributionMode(savedView.contributionMode);setResolution(savedView.resolution);setActiveBucket(savedView.activeBucket);
        setSaved(savedView.saved);setSaveName(savedView.saveName);setSourcesUnavailable(savedView.sourcesUnavailable);setPreferencesUnavailable(savedView.preferencesUnavailable);
        setSourcesNotice(savedView.sourcesNotice);setPreferencesNotice(savedView.preferencesNotice);
      }).catch(failure=>{
        if(sequence!==requestSequence.current)return;
        clearReading();setAppliedConfig(savedView.appliedConfig);setConfig(savedView.appliedConfig);
        setError(failure instanceof Error&&failure.message==="navigation_access_changed"?"权限或阅读版本已变化，不能恢复先前视图。":problemText(failure));
      }).finally(()=>{if(sequence===requestSequence.current)setBusy(false)});
    }
    return()=>{controller.abort();++requestSequence.current;++viewSequence.current};
  },[loadReading]);
  useEffect(()=>{
    const invalidate=()=>{++requestSequence.current;++viewSequence.current;clearReading();setBusy(false);setError("登录或授权范围已变更，原阅读已移除，请重新读取。");};
    window.addEventListener("xinsight:scope-changed",invalidate);window.addEventListener("xinsight:unauthorized",invalidate);
    return()=>{window.removeEventListener("xinsight:scope-changed",invalidate);window.removeEventListener("xinsight:unauthorized",invalidate);};
  },[]);
  useEffect(()=>{
    if(busy||!reading||!summary||!contributions||!series)return;
    navigationSink.current?.({version:1,owner:navigationOwner(session),readingID:reading.reading_id,request:fixedReadingRequest(reading,appliedConfig),dimension,mode:contributionMode,resolution,activeBucket,selectedRef:selected?.ref,contributionPages:contributionPages.current,detailPages:detailPages.current});
    snapshotSink.current?.({subject:session.session.subject_id,permissions:[...session.session.permissions].sort().join(","),config,appliedConfig,reading,summary,contributions,series,sources,details,selected,dimension,contributionMode,resolution,activeBucket,saved,saveName,sourcesUnavailable,preferencesUnavailable,sourcesNotice,preferencesNotice});
  },[busy,reading,summary,contributions,series,sources,details,selected,dimension,contributionMode,resolution,activeBucket,saved,saveName,config,appliedConfig,sourcesUnavailable,preferencesUnavailable,sourcesNotice,preferencesNotice,session]);
  useEffect(()=>{if(selected){previewRef.current?.focus({preventScroll:true});if(window.innerWidth<1200)previewRef.current?.scrollIntoView?.({block:"start"})}},[selected]);

  async function runView(action:()=>Promise<void>,readingDependent=true){
    const sequence=++viewSequence.current,generation=requestSequence.current;
    const current=()=>sequence===viewSequence.current&&generation===requestSequence.current;
    setBusy(true);setError("");setCanRetry(false);
    try{await action()}
    catch(failure){if(current()){
      if((readingDependent&&invalidatesReading(failure))||(failure instanceof ApiRequestError&&failure.status===401)){
        clearReading();++requestSequence.current;++viewSequence.current;setBusy(false);
      }else{retryAction.current=()=>void runView(action,readingDependent);setCanRetry(true)}
      setError(problemText(failure));
    }}finally{if(current())setBusy(false)}
  }
  // Every async success is tied to the reading generation and view request.
  function viewGuard(){const generation=requestSequence.current,sequence=viewSequence.current;return()=>generation===requestSequence.current&&sequence===viewSequence.current}
  async function changeDimension(value:string){if(!reading)return;await runView(async()=>{
    const current=viewGuard(),nextDimension=value as WorkbenchDimension;
    const next=await workbenchAPI.contributions(reading.reading_id,nextDimension,usage?{topN:10,orderBy:"requests"}:{topN:10});
    if(current()){contributionPages.current=1;detailPages.current=1;setContributions(next);setDimension(nextDimension);setContributionMode("top10");setDetails(null);setSelected(null)}
  })}
  async function changeContributionMode(mode:"top5"|"top10"|"all"){if(!reading)return;await runView(async()=>{
    const current=viewGuard();const next=await workbenchAPI.contributions(reading.reading_id,dimension,mode==="all"?(usage?{limit:5,orderBy:"requests"}:{limit:5}):(usage?{topN:mode==="top5"?5:10,orderBy:"requests"}:{topN:mode==="top5"?5:10}));
    if(current()){contributionPages.current=1;detailPages.current=1;setSelected(null);setDetails(null);setContributions(next);setContributionMode(mode)}
  })}
  async function changeResolution(value:"overview"|"hourly"){if(!reading)return;await runView(async()=>{
    const current=viewGuard();const next=usage?await workbenchAPI.series(reading.reading_id,undefined,value==="hourly"?3600:overviewSeconds(reading),"requests"):await workbenchAPI.series(reading.reading_id,undefined,value==="hourly"?3600:overviewSeconds(reading));
    if(current()){setSeries(next);setResolution(value);setActiveBucket("")}
  })}
  async function loadMoreContributions(){if(!reading||!contributions?.next_cursor)return;await runView(async()=>{
    const current=viewGuard();const next=await workbenchAPI.contributions(reading.reading_id,dimension,usage?{limit:5,cursor:contributions.next_cursor,orderBy:"requests"}:{limit:5,cursor:contributions.next_cursor});
    if(next.denominator_micros!==contributions.denominator_micros||next.unassigned_micros!==contributions.unassigned_micros||next.unassigned_scope!=="reading_constant")throw new Error("reading constants changed across pages");
    if(current()){contributionPages.current++;setContributions({...next,rows:[...contributions.rows,...next.rows]});}
  })}
  async function openDetails(row:WorkbenchContribution){if(!reading||!row.drilldown_allowed)return;await runView(async()=>{
    const current=viewGuard(),next=await workbenchAPI.details(reading.reading_id,row.ref);
    if(current()){detailPages.current=1;setSelected(row);setDetails(next)}
  })}
  async function loadMoreDetails(){if(!reading||!selected||!details?.next_cursor)return;await runView(async()=>{
    const current=viewGuard(),next=await workbenchAPI.details(reading.reading_id,selected.ref,details.next_cursor);
    if(current()){detailPages.current++;setDetails({...next,rows:[...details.rows,...next.rows]});}
  })}
  async function saveCurrent(){if(!reading)return;const idempotencyKey=crypto.randomUUID();await runView(async()=>{
    const current=viewGuard(),created=await workbenchAPI.createSavedFilter({name:saveName,config:appliedConfig,expected_revision:0,idempotency_key:idempotencyKey},session.csrf_token);
    if(current())setSaved(items=>[created,...items.filter(item=>item.id!==created.id)]);
  },false)}
  async function updateSaved(item:WorkbenchSavedFilter){const idempotencyKey=crypto.randomUUID();await runView(async()=>{
    const current=viewGuard(),updated=await workbenchAPI.updateSavedFilter(item.id,{name:item.name,config:appliedConfig,expected_revision:item.revision,idempotency_key:idempotencyKey},session.csrf_token);
    if(current())setSaved(items=>items.map(candidate=>candidate.id===updated.id?updated:candidate));
  },false)}
  const maximum=useMemo(()=>series?.points.reduce((max,point)=>greaterMicros(max,point.known_value),"0")??"0",[series]);
  const seriesHasKnownValue=series?.points.some(point=>{try{return BigInt(point.known_value)!==0n}catch{return false}})??false;
  const usageDimensionName:Record<WorkbenchDimension,string>={purpose:"用途",application:"应用",account:"账户归属",source_project:"来源项目",cost_project:"费用项目",model:"模型"};

  const errorNotice=error?<div className="workbench-error"><p role="alert">{error}</p>{reading?<><small>仍显示上次成功的视图；本次更新未生效。</small>{canRetry?<button disabled={busy} onClick={()=>retryAction.current?.()}>重试更新</button>:null}</>:<button disabled={busy} onClick={()=>void loadReading(appliedConfig)}>按当前权限重新建立阅读</button>}</div>:null;
  const timezone=reading?.period.timezone||"UTC";
  const sourceSelection=config.filters.source_ids.length>1?"__selected_sources__":config.filters.source_ids[0]||"";
  const scrollSeries=resolution==="hourly"&&(series?.points.length||0)>30;
  const preview=details&&selected?<aside className="workbench-panel contribution-preview" ref={previewRef} tabIndex={-1} aria-label="所选贡献依据"><header><div><span className="eyebrow">所选贡献</span><h2>{dimension==="purpose" ? purposeLabels[selected.label] || selected.label : selected.label}</h2><strong>{usage?`${selected.request_count} 个已知请求`:formatMicros(selected.selected_amount_micros)}</strong>{usage?<small>所选金额 {formatMicros(selected.selected_amount_micros)}</small>:null}</div><button disabled={busy} className="text-button" onClick={()=>{setDetails(null);setSelected(null)}}>← 返回{usage?"使用分布":"费用贡献"}</button></header><p className="boundary-note">明细固定于本次阅读；打开执行详情后可返回这一选择。</p><div className="workbench-detail-list">{details.rows.map(row=><article key={row.evidence_event_id}><div><Identifier value={row.request_id} label="请求标识" /><span>{row.model||"模型未知"} · {row.terminal_status}</span></div><div><b>{formatMicros(basis==="gateway_charged"?row.gateway_charged_micros:row.calculated_reference_micros)}</b>{(session.session.permissions.includes("evidence.read")||(session.session.permissions.includes("investigation.scoped.read")&&row.investigation_available===true))?<button className="text-button" onClick={()=>navigate(`/executions/${encodeURIComponent(row.execution_id)}?as_of=${encodeURIComponent(reading!.cutoff)}&evidence=${encodeURIComponent(row.evidence_event_id)}`)}>查看调查证据</button>:<small>当前角色仅可查看本阅读内依据</small>}</div><div className="evidence-identity"><Identifier value={row.event_hash} label="证据标识" /></div><small>账户归属：{row.account_state==="known_account_owner"?"已记录":"未知"} · 应用：{row.application_state==="known"?"已记录":"未知"} · 实际操作者：未知</small>{row.source_facts?.map(fact=><SourceFactCard key={fact.source_fact_id} fact={fact} />)}<PurposePanel executionID={row.execution_id} readingID={reading?.reading_id} basis={{evidence_event_id:row.evidence_event_id,event_hash:row.event_hash}} session={session} onInvalidated={failure=>{clearReading();setError(problemText(failure))}}/></article>)}</div>{details.next_cursor?<button disabled={busy} className="text-button" onClick={()=>void loadMoreDetails()}>加载更多依据</button>:null}</aside>:null;

  const sourceCoverage=reading?.source_coverage;
  const sourceIssues=sourceCoverage?[
    sourcesUnavailable?"来源级状态未能读取":"",
    sourceCoverage.economic_not_received>0?`${sourceCoverage.economic_not_received} 未关联经济事实`:"",
    sourceCoverage.pending_predecessor>0?`${sourceCoverage.pending_predecessor} 修订待前驱`:"",
    sourceCoverage.economic_hash_conflicts>0?`${sourceCoverage.economic_hash_conflicts} 经济引用不匹配`:"",
    sourceCoverage.media_pending>0?`${sourceCoverage.media_pending} 媒体经济待定`:"",
    sourceCoverage.not_captured>0?`${sourceCoverage.not_captured} 原字段未采集`:"",
    sourceCoverage.requires_attention?"采集注册需处理":"",
    sourceCoverage.collection_states?.some(state=>state!=="active")?"采集未全部启用":"",
    sourceCoverage.gaps.length>0?`${sourceCoverage.gaps.length} 项覆盖限制`:"",
  ].filter(Boolean):[];
  const sourceFacts=sourceCoverage&&reading?<SourceFactsPanel key={reading.reading_id} reading={reading} onInvalidated={failure=>{clearReading();++requestSequence.current;++viewSequence.current;setBusy(false);setError(problemText(failure))}}/>:null;
  return <section className="workbench-page" aria-busy={busy}>
    <div className="workbench-hero"><div><span className="eyebrow">共同阅读</span>{!usage?<a className="budget-entry" href="/workbench/cost?view=budgets" onClick={event=>{if(event.button===0&&!event.metaKey&&!event.ctrlKey&&!event.shiftKey&&!event.altKey){event.preventDefault();navigate("/workbench/cost?view=budgets")}}}>查看全周期预算</a>:null}<h1>{usage?"使用与目的":"费用与使用"}</h1><p>{usage?"先看已知请求的用途与归属，再沿记录核对依据。请求数只覆盖本次费用资料成员。":"总额、趋势、贡献与依据使用同一次阅读。范围变化时重新建立。"}</p></div>{!usage?<div className="basis-switch" role="group" aria-label="金额口径"><button disabled={busy} className={basis==="gateway_charged"?"active":""} onClick={()=>void loadReading({...config,money_basis:"gateway_charged"})}>网关扣费</button><button disabled={busy} className={basis==="calculated_reference"?"active":""} onClick={()=>void loadReading({...config,money_basis:"calculated_reference"})}>计算参考值</button></div>:null}</div>
    <div className="workbench-controls"><label>期间<select aria-label="期间" disabled={busy} value={config.period.relative_hours??"inherited"} onChange={event=>setConfig({...config,period:{relative_hours:Number(event.target.value),display_timezone:config.period.display_timezone}})}>{!config.period.relative_hours?<option value="inherited">来源期间（固定起止）</option>:null}<option value={24}>最近 24 小时</option><option value={72}>最近 3 天</option><option value={168}>最近 7 天</option><option value={720}>最近 30 天</option></select></label><label>来源<select value={sourceSelection} disabled={busy||!sources?.items.length} onChange={event=>{if(event.target.value!=="__selected_sources__")setConfig({...config,filters:{...config.filters,source_ids:event.target.value?[event.target.value]:[]}})}}><option value="">全部已授权来源</option>{config.filters.source_ids.length>1?<option value="__selected_sources__">已选 {config.filters.source_ids.length} 个来源</option>:sourceSelection&&!sources?.items.some(source=>source.source_system_id===sourceSelection)?<option value={sourceSelection}>已选来源 · {compactIdentifier(sourceSelection)}</option>:null}{sources?.items.map(source=><option key={source.source_system_id} value={source.source_system_id}>{source.display_name}</option>)}</select></label><label>来源项目{config.filters.source_projects.length>1?<small>已选 {config.filters.source_projects.length} 个；编辑将替换全部</small>:null}<input disabled={busy} value={config.filters.source_projects[0]||""} placeholder="全部已授权项目" onChange={event=>setConfig({...config,filters:{...config.filters,source_projects:event.target.value?[event.target.value]:[]}})}/></label><button disabled={busy} onClick={()=>void loadReading(config)}>应用范围</button></div>
    {reading?<details className="workbench-active-scope"><summary>当前生效筛选</summary><p>来源：{appliedConfig.filters.source_ids.join("、")||"全部授权范围"}；来源项目：{appliedConfig.filters.source_projects.join("、")||"全部授权范围"}；费用项目：{appliedConfig.filters.cost_projects.join("、")||"全部授权范围"}。</p></details>:null}
    {errorNotice}
    {usage?<p className="boundary-note usage-boundary">人工用途声明不证明实际操作者或工作已完成；未知与冲突仍保留在本次请求分母。</p>:null}
    <div className="workbench-grid">
      {usage?<>
        <article className="metric-card primary"><span>费用资料中的已知请求</span><strong>{summary?.request_count??"—"}</strong><small>{summary?.request_total_complete?"当前资料成员数":"不等于全部 AI 工作"}</small></article>
        <article className="metric-card"><span>已采用用途</span><strong>{reading?.dimensions.includes("purpose")&&summary?`${summary.purpose_known_requests} / ${summary.request_count}`:"—"}</strong><small>{reading?.dimensions.includes("purpose")?"人工采用的主用途；其余保持未知、待复核或冲突":"用途维度状态见下方分布"}</small></article>
      </>:<>
        <article className="metric-card primary"><span>{reading?.money_title||"金额"}</span><strong>{summary?formatMicros(summary.selected_amount_micros):"—"}</strong><small>{summary?.amount_complete?"完整到当前截止":"已知小计；覆盖尚不完整"}</small></article>
        <article className="metric-card"><span>已知请求</span><strong>{summary?.request_count??"—"}</strong><small>{summary?.request_total_complete?"完整分母":"不是全部工作分母"}</small></article>
      </>}
      <article className="metric-card reading-period"><span>本次阅读期间</span><p>{reading?`${formatTime(reading.period.start_inclusive,timezone)} → ${formatTime(reading.period.end_exclusive,timezone)}`:busy?"正在读取…":error?"阅读已失效或未能完成，待重新读取":"尚未建立阅读"}</p><small>{appliedConfig.period.start_inclusive?(reading?"沿用来源期间。":busy?"正在读取来源期间。":"待重新读取来源期间。 "):""}接收截止：{reading?formatTime(reading.cutoff,timezone):"—"}</small><details><summary>阅读标识与版本</summary><code>{reading?.reading_id}</code><dl><dt>成员版本</dt><dd>{reading?.membership_ref}</dd><dt>解释版本</dt><dd>{reading?.interpretation_revision}</dd></dl></details></article>
    </div>
    {usage?<p className="usage-money">辅助金额 · {reading?.money_title||"当前口径"}：<strong>{summary?formatMicros(summary.selected_amount_micros):"—"}</strong>。{summary?.amount_complete?"当前资料成员金额。":"已知小计；未覆盖部分不按零计。"}</p>:null}
    <div className={`workbench-comparison${preview?" with-preview":""}`}><div className="workbench-two-column"><article className="workbench-panel"><header><div><span className="eyebrow">趋势</span><h2>固定期间的已知{usage?"请求":"金额"}</h2></div><span className={`coverage-pill coverage-${summary?.coverage.status||"unknown"}`}>{coverageLabel(summary?.coverage.status)}</span></header><label className="series-resolution">趋势粒度<select aria-label="趋势粒度" value={resolution} disabled={busy||!reading} onChange={event=>void changeResolution(event.target.value as "overview"|"hourly")}><option value="overview">全期间概览</option><option value="hourly">每小时详情</option></select><span>{series?`每桶 ${series.bucket_seconds/3600} 小时 · ${timezone}`:"—"}</span></label>
      {series&&!seriesHasKnownValue?<p className="series-empty">本次阅读暂无已知{usage?"请求":"金额"}趋势；{summary?.coverage.period_completeness==="unknown"?"期间完整性未知，不能把空桶当作真实零。":"查看下方覆盖依据核对范围。"}</p>:<div className="series-scroll" role="region" aria-label="趋势时间桶" tabIndex={scrollSeries?0:undefined}>
        <div className={`series-bars${scrollSeries?" series-hourly":""}`} data-reading-id={reading?.reading_id}>
          {series?.points.map((point,index)=>{const description=`${formatTime(point.start_inclusive,timezone)} → ${formatTime(point.end_exclusive,timezone)}：${usage?`${point.known_value} 个已知请求`:formatMicros(point.known_value)}${point.complete?"":" · 期间完整性未知"}`;return <div key={point.start_inclusive} tabIndex={0} aria-label={description} title={description} data-value={point.known_value} data-start={point.start_inclusive} data-end={point.end_exclusive} onFocus={()=>setActiveBucket(description)} onMouseEnter={()=>setActiveBucket(description)}><i style={{height:`${barHeight(point.known_value,maximum)}%`}}/>{index===0||index===series.points.length-1||index%Math.max(1,Math.ceil(series.points.length/5))===0?<span>{formatTime(point.start_inclusive,timezone,true)}</span>:null}</div>})}
        </div>
      </div>}
      {scrollSeries?<p className="boundary-note">可在图表内横向滚动查看全部小时，首末桶均保留。</p>:null}
      {seriesHasKnownValue?<p className="series-value" aria-live="polite">{activeBucket||`聚焦或指向时间桶查看精确起止时间与${usage?"请求数":"金额"}。`}</p>:null}
      {series&&!series.points.every(point=>point.complete)?<p className="boundary-note">空桶表示已知小计为 0，不代表该时段已经完整采集。来源的接收进度不能证明已完整采集该期间。</p>:null}
      {summary?<details className="coverage-disclosure"><summary>查看覆盖依据与缺项</summary><CoverageExplanation coverage={summary.coverage} timezone={timezone} sourceCount={sources?.items.length}/></details>:null}</article>
      </div>
    <article className="workbench-panel contributions-panel"><header><div><span className="eyebrow">{usage?"使用分布":"贡献"}</span><h2>{usage?`当前范围的${usageDimensionName[dimension]}请求分布`:"当前范围的费用贡献"}</h2></div><div className="contribution-controls"><select disabled={busy||!reading} aria-label="贡献维度" value={dimension} onChange={event=>void changeDimension(event.target.value)}><option value="source_project">来源项目</option><option value="model">模型</option><option value="cost_project">费用项目</option>{reading?.dimensions.includes("application")?<option value="application">应用</option>:null}{reading?.dimensions.includes("account")?<option value="account">账户归属</option>:null}{reading?.dimensions.includes("purpose")?<option value="purpose">请求用途</option>:null}</select><select disabled={busy||!reading} aria-label="贡献模式" value={contributionMode} onChange={event=>void changeContributionMode(event.target.value as "top5"|"top10"|"all")}><option value="top5">Top 5</option><option value="top10">Top 10</option><option value="all">全部贡献</option></select></div></header>
      {reading ? <p>{reading.purpose?.status==="available"?"用途已纳入本阅读；每个请求只采用一个主用途，修订在新阅读后生效。":"本阅读未提供完整用途维度；不能据此断言没有用途。"}{usage?` 应用：${reading.dimensions.includes("application")?"可读":"未纳入本阅读"}；账户归属：${reading.dimensions.includes("account")?"可读":"未纳入本阅读"}。`:""}</p> : null}
      <div className="contribution-list">{contributions?.rows.map(row=><button key={row.ref} disabled={busy||!row.drilldown_allowed} aria-pressed={selected?.ref===row.ref} onClick={()=>void openDetails(row)}><span><strong title={dimension==="purpose" ? purposeLabels[row.label] || row.label : row.label}>{dimension==="purpose" ? purposeLabels[row.label] || row.label : row.label}</strong><small>{usage?`本次请求占比 ${summary?formatShare(String(row.request_count),String(summary.request_count)):"—"}`:`${row.request_count} 个请求`}{row.drilldown_allowed?" · 可查看依据":" · 仅汇总"}</small></span><span><b>{usage?`${row.request_count} 个请求`:formatMicros(row.selected_amount_micros)}</b><small>{usage?`辅助金额 ${formatMicros(row.selected_amount_micros)}`:formatShare(row.share.numerator,row.share.denominator)}</small></span></button>)}{contributions?.rows.length===0?<p>当前范围没有已归属贡献；未知与未归属仍计入本次分母。</p>:null}</div>{contributions?.next_cursor?<button disabled={busy} className="text-button" onClick={()=>void loadMoreContributions()}>加载更多贡献</button>:null}
      <footer>{usage?<><span>其他已知组 {contributions?.other_known_requests??"—"} 个请求</span><span>未归属 {contributions?.unassigned_requests??"—"} 个请求</span><strong>本次费用资料成员 {contributions?.denominator_requests??"—"} 个请求</strong></>:<><span>其他已知组 {contributions?formatMicros(contributions.other_known_micros):"—"}</span><span>未归属（本次范围固定） {contributions?formatMicros(contributions.unassigned_micros):"—"}</span><strong>当前范围已知金额 {contributions?formatMicros(contributions.denominator_micros):"—"}</strong></>}</footer></article>
    {preview}</div>
    <details className="workbench-disclosure"><summary>{usage&&sourceCoverage?`来源依据与限制 · ${sourceCoverage.known_documents} 份已授权记录 · ${sourceIssues.length?sourceIssues.join("；"):"未报告需关注项，完整性仍未知"}`:"来源观察与覆盖"}</summary>      <article className="workbench-panel"><header><div><span className="eyebrow">来源观察</span><h2>当前来源观察</h2></div></header><div className="source-list">{sources?.items.map(source=><div key={source.source_system_id}><strong>{source.display_name}</strong><small>经济事实采集</small><span>{sourceStatusLabel(source.status)}</span><small>接收：{source.last_received_at?formatTime(source.last_received_at,timezone):"未知"}</small><small>投影：{source.projection_watermark?formatTime(source.projection_watermark,timezone):"未知"}</small><SourceCollectionStatus items={source.source_facts} formatTime={value=>formatTime(value,timezone)}/></div>)}{sources?.items.length===0&&!sourcesUnavailable?<p>当前范围没有可展示来源。</p>:null}{sourcesUnavailable?<p>{sourcesNotice}</p>:null}</div></article>
      {usage?sourceFacts:null}
    </details>
    {!usage?sourceFacts:null}
    <details className="workbench-disclosure"><summary>个人筛选 · 保存与重开</summary>
    {!preferencesUnavailable?<article className="workbench-panel saved-filter-panel"><header><div><span className="eyebrow">个人偏好</span><h2>保存并按当前权限重开</h2></div></header><div><input value={saveName} maxLength={120} onChange={event=>setSaveName(event.target.value)}/><button disabled={busy||!reading||!saveName.trim()} onClick={()=>void saveCurrent()}>保存当前筛选</button></div><ul>{saved.map(item=><li key={item.id}><span><strong>{item.name}</strong><small>修订 {item.revision}</small></span><span className="saved-actions"><button disabled={busy} onClick={()=>void loadReading(item.config)}>重开</button><button disabled={busy||!reading} onClick={()=>void updateSaved(item)}>更新</button></span></li>)}</ul></article>:<article className="workbench-panel saved-filter-panel"><p>{preferencesNotice}</p><small>本次阅读不受影响。</small></article>}
    </details>
  </section>
}

function formatMicros(value:string){const negative=value.startsWith("-");const digits=negative?value.slice(1):value;const padded=digits.padStart(7,"0");return `${negative?"-":""}¥${padded.slice(0,-6)}.${padded.slice(-6)}`}
function greaterMicros(left:string,right:string){try{return BigInt(right)>BigInt(left)?right:left}catch{return left}}
function barHeight(value:string,maximum:string){try{const max=BigInt(maximum);if(max===0n||BigInt(value)===0n)return 0;return Math.max(2,Number(BigInt(value)*100n/max))}catch{return 0}}
function formatShare(value:string,total:string){try{const denominator=BigInt(total);if(denominator===0n)return "不可计算";return `${Number(BigInt(value)*1000n/denominator)/10}%`}catch{return "—"}}
function coverageLabel(value?:string){return value==="scope_limited_unknown"?"范围受限 · 状态未知":value==="not_connected"?"未接入":value==="disconnected"?"已断开":value==="observed_with_gaps"?"已观察 · 有缺项":value==="observed"?"已观察":"期间完整性未知"}
function sourceStatusLabel(value:string){return value==="scope_limited_unknown"?"范围受限 · 状态未知":value==="not_connected"?"未接入":value==="disconnected"?"已断开":value==="observed_with_gaps"?"已观察 · 有缺项":"已观察"}
function problemText(failure:unknown){if(failure instanceof ApiRequestError){if(failure.problem?.code==="workbench_access_changed")return "权限已变化，请重新读取当前范围。";if(failure.problem?.code==="workbench_reading_expired")return "本次阅读已过期，请重新读取。";return failure.problem?.title||`请求失败（${failure.status}）`}return "费用工作台暂时不可用。"}
function optionalFailureText(failure:unknown,feature:string){if(failure instanceof ApiRequestError&&failure.status===403)return `当前授权不包含${feature}；金额与请求的已知小计仍可阅读。`;return `${feature}暂时不可用；金额与请求的已知小计仍可阅读。`}

function invalidatesReading(failure:unknown){return failure instanceof ApiRequestError&&([401,403,404,410].includes(failure.status)||failure.problem?.code==="workbench_access_changed")}
function overviewSeconds(reading:WorkbenchReading){const hours=(Date.parse(reading.period.end_exclusive)-Date.parse(reading.period.start_inclusive))/3600000;return hours<=24?3600:hours<=72?10800:hours<=168?21600:86400}
function formatTime(value:string,timezone:string,compact=false){return new Intl.DateTimeFormat("zh-CN",{timeZone:timezone,month:"2-digit",day:"2-digit",hour:"2-digit",minute:compact?undefined:"2-digit",hourCycle:"h23",...(compact?{}:{year:"numeric" as const,timeZoneName:"short" as const})}).format(new Date(value))}
const gapLabels:Record<string,string>={
  source_period_completeness_not_asserted:"来源未提供所选期间完整采集的保证。",
  period_precedes_capture_start:"至少一个来源的采集起点晚于所选期间起点，该部分历史未确认覆盖。",
  source_profile_capture_gaps_present:"已观察到来源字段缺项；这与采集起点之前的历史缺口分别计算。",
  source_not_currently_connected:"建立本阅读时来源已断开，不能据此推断期间完整。",
  scope_limited_source_state_unknown:"当前授权不足以判断整源覆盖。",
};
function CoverageExplanation({coverage,timezone,sourceCount}:{coverage:WorkbenchCoverage;timezone:string;sourceCount?:number}){
  const restricted=coverage.status==="scope_limited_unknown";
  const gaps=restricted?["source_period_completeness_not_asserted","scope_limited_source_state_unknown"]:coverage.gaps;
  return <section className="coverage-explanation" aria-label="本阅读覆盖依据"><strong>本阅读固定覆盖依据 · 期间完整性未知</strong>
    <p>{coverage.projection_complete_for_members?"已纳入成员的投影已完成；不代表整个期间完整。":"已纳入成员的投影仍未完成。"}</p>
    {!restricted?<dl><dt>{sourceCount===1?"已知采集起点":"最早已知采集起点（不代表所有来源）"}</dt><dd>{coverage.capture_started_at?formatTime(coverage.capture_started_at,timezone):"未知"}</dd><dt>本阅读成员最近接收</dt><dd>{coverage.last_received_at?formatTime(coverage.last_received_at,timezone):"未知"}</dd><dt>消费位置</dt><dd>{coverage.received_watermark?"已记录（不透明，不能证明时间完整）":"未知"}</dd></dl>:null}
    <p>本阅读成员投影时间：{coverage.projection_watermark?formatTime(coverage.projection_watermark,timezone):"未知"}</p>
    <ul>{[...new Set(gaps)].map(gap=><li key={gap}>{gapLabels[gap]||"存在尚未解释的覆盖限制。"}</li>)}</ul>
  </section>
}
