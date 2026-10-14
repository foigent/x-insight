import { localizedLabels, getLocale, t, tx, useLocale, useMessageState, type MessageResolver } from "./i18n";
import { authorizedNavigationReading, fixedReadingRequest, navigationOwner, restoredDimension, usageDimension, type WorkbenchNavigation } from "./workbenchNavigation";
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
  sourcesNotice: string | MessageResolver;
  preferencesNotice: string | MessageResolver;
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
  useLocale();
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
  const [sourcesNotice,setSourcesNotice,resolveSourcesNotice]=useMessageState("");
  const [preferencesNotice,setPreferencesNotice,resolvePreferencesNotice]=useMessageState("");
  const [saveName,setSaveName]=useState(t("m2682"));
  const [busy,setBusy]=useState(false);const [error,setError]=useMessageState("");
  const requestSequence=useRef(0);
  const viewSequence=useRef(0);

  const [resolution,setResolution]=useState<"overview"|"hourly">("overview");
  const [activeBucket,setActiveBucket]=useState("");
  const [seriesWidth,setSeriesWidth]=useState(0);
  const seriesBarsRef=useRef<HTMLDivElement>(null);
  const [canRetry,setCanRetry]=useState(false);
  const retryAction=useRef<(()=>void)|null>(null);
  const initialSnapshot=useRef(snapshot);
  const snapshotSink=useRef(onSnapshot);
  snapshotSink.current=onSnapshot;
  const previewRef=useRef<HTMLElement>(null);

  function clearReading(){snapshotSink.current?.(null);setReading(null);setSummary(null);setContributions(null);setSeries(null);setSources(null);setDetails(null);setSelected(null);setActiveBucket("")}
  const loadReading=useCallback(async(request:WorkbenchReadingRequest)=>{
    const sequence=++requestSequence.current;++viewSequence.current;
    setBusy(true);setError(() => "");setCanRetry(false);clearReading();
    contributionPages.current=1;detailPages.current=1;
    setSourcesUnavailable(false);setPreferencesUnavailable(false);setSourcesNotice(() => "");setPreferencesNotice(() => "");
    try{
      const next=await workbenchAPI.createReading(request,session.csrf_token);
      const sum=await workbenchAPI.summary(next.reading_id);
      if(sequence!==requestSequence.current)return;
      const initialDimension=usage?usageDimension(next,sum.actor_known_requests):"source_project";
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
      else{setSourcesUnavailable(true);setSourcesNotice(() => optionalFailureText(sourceResult.reason,t("m2683")))}
      if(savedResult.status==="fulfilled")setSaved(savedResult.value.items);
      else{setSaved([]);setPreferencesUnavailable(true);setPreferencesNotice(() => optionalFailureText(savedResult.reason,t("m0873")))}
    }catch(failure){if(sequence===requestSequence.current)setError(() => problemText(failure))}
    finally{if(sequence===requestSequence.current)setBusy(false)}
  },[session.csrf_token,identity,usage]);
  useEffect(()=>{
    const savedView=initialSnapshot.current;
    const retained=initialNavigation.current;
    const controller=new AbortController();
    if(navigationInvalid){clearReading();setError(() => t("m2684"));}
    else if(retained){
      const sequence=++requestSequence.current;
      setBusy(true);setError(() => "");clearReading();
      void (async()=>{
        const next=await authorizedNavigationReading(retained,session,controller.signal);
        const request=fixedReadingRequest(next,retained.request);
        const earlySummary=next.dimensions.includes(retained.dimension)?undefined:await workbenchAPI.summary(next.reading_id,controller.signal);
        const restored=restoredDimension(next,retained,usage,earlySummary?.actor_known_requests??0);
        let groups=await workbenchAPI.contributions(next.reading_id,restored,retained.mode==="all"?(usage?{limit:5,orderBy:"requests"}:{limit:5}):(usage?{topN:retained.mode==="top5"?5:10,orderBy:"requests"}:{topN:retained.mode==="top5"?5:10}),controller.signal);
        for(let page=1;retained.mode==="all"&&page<retained.contributionPages;page++){
          if(!groups.next_cursor)throw new Error("navigation_access_changed");
          const more=await workbenchAPI.contributions(next.reading_id,restored,usage?{limit:5,cursor:groups.next_cursor,orderBy:"requests"}:{limit:5,cursor:groups.next_cursor},controller.signal);
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
        const [sum,line]=await Promise.all([earlySummary?Promise.resolve(earlySummary):workbenchAPI.summary(next.reading_id,controller.signal),usage?workbenchAPI.series(next.reading_id,controller.signal,retained.resolution==="hourly"?3600:overviewSeconds(next),"requests"):workbenchAPI.series(next.reading_id,controller.signal,retained.resolution==="hourly"?3600:overviewSeconds(next))]);
        const [sourceResult,savedResult]=await Promise.allSettled([workbenchAPI.sources(next.reading_id,controller.signal),workbenchAPI.savedFilters(controller.signal)]);
        if(sourceResult.status==="rejected"&&invalidatesReading(sourceResult.reason)&&!(sourceResult.reason instanceof ApiRequestError&&sourceResult.reason.status===403&&sourceResult.reason.problem?.code!=="workbench_scope_denied"))throw sourceResult.reason;
        if(savedResult.status==="rejected"&&savedResult.reason instanceof ApiRequestError&&savedResult.reason.status===401)throw savedResult.reason;
        if(controller.signal.aborted||sequence!==requestSequence.current)return;
        if(!(Date.parse(next.expires_at)>Date.now()))throw new Error("navigation_access_changed");
        contributionPages.current=retained.contributionPages;detailPages.current=retained.detailPages;
        setConfig(request);setAppliedConfig(request);setBasis(next.money_basis);setReading(next);setSummary(sum);setContributions(groups);setSeries(line);
        setSelected(selected??null);setDetails(detail);setDimension(restored);setContributionMode(retained.mode);setResolution(retained.resolution);setActiveBucket(retained.activeBucket);
        setSources(sourceResult.status==="fulfilled"?sourceResult.value:null);setSaved(savedResult.status==="fulfilled"?savedResult.value.items:[]);
        setSourcesUnavailable(sourceResult.status==="rejected");setPreferencesUnavailable(savedResult.status==="rejected");
        setSourcesNotice(() => sourceResult.status==="rejected"?optionalFailureText(sourceResult.reason,t("m2683")):"");
        setPreferencesNotice(() => savedResult.status==="rejected"?optionalFailureText(savedResult.reason,t("m0873")):"");
      })().catch(failure=>{if(sequence!==requestSequence.current||controller.signal.aborted)return;clearReading();setError(() => failure instanceof Error&&failure.message==="navigation_access_changed"?t("m2685"):problemText(failure));}).finally(()=>{if(sequence===requestSequence.current&&!controller.signal.aborted)setBusy(false)});
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
        setError(() => failure instanceof Error&&failure.message==="navigation_access_changed"?t("m2686"):problemText(failure));
      }).finally(()=>{if(sequence===requestSequence.current)setBusy(false)});
    }
    return()=>{controller.abort();++requestSequence.current;++viewSequence.current};
  },[loadReading]);
  useEffect(()=>{
    const invalidate=()=>{++requestSequence.current;++viewSequence.current;clearReading();setBusy(false);setError(() => t("m2687"));};
    window.addEventListener("xinsight:scope-changed",invalidate);window.addEventListener("xinsight:unauthorized",invalidate);
    return()=>{window.removeEventListener("xinsight:scope-changed",invalidate);window.removeEventListener("xinsight:unauthorized",invalidate);};
  },[]);
  useEffect(()=>{
    if(busy||!reading||!summary||!contributions||!series)return;
    navigationSink.current?.({version:1,owner:navigationOwner(session),readingID:reading.reading_id,request:fixedReadingRequest(reading,appliedConfig),dimension,mode:contributionMode,resolution,activeBucket,selectedRef:selected?.ref,contributionPages:contributionPages.current,detailPages:detailPages.current});
    snapshotSink.current?.({subject:session.session.subject_id,permissions:[...session.session.permissions].sort().join(","),config,appliedConfig,reading,summary,contributions,series,sources,details,selected,dimension,contributionMode,resolution,activeBucket,saved,saveName,sourcesUnavailable,preferencesUnavailable,sourcesNotice:resolveSourcesNotice,preferencesNotice:resolvePreferencesNotice});
  },[busy,reading,summary,contributions,series,sources,details,selected,dimension,contributionMode,resolution,activeBucket,saved,saveName,config,appliedConfig,sourcesUnavailable,preferencesUnavailable,resolveSourcesNotice,resolvePreferencesNotice,session]);
  useEffect(()=>{if(selected){previewRef.current?.focus({preventScroll:true});if(window.innerWidth<1200)previewRef.current?.scrollIntoView?.({block:"start"})}},[selected]);

  async function runView(action:()=>Promise<void>,readingDependent=true){
    const sequence=++viewSequence.current,generation=requestSequence.current;
    const current=()=>sequence===viewSequence.current&&generation===requestSequence.current;
    setBusy(true);setError(() => "");setCanRetry(false);
    try{await action()}
    catch(failure){if(current()){
      if((readingDependent&&invalidatesReading(failure))||(failure instanceof ApiRequestError&&failure.status===401)){
        clearReading();++requestSequence.current;++viewSequence.current;setBusy(false);
      }else{retryAction.current=()=>void runView(action,readingDependent);setCanRetry(true)}
      setError(() => problemText(failure));
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
  const seriesPoints=series?.points.map((point,index)=>{
    const start=Date.parse(point.start_inclusive), end=Date.parse(point.end_exclusive);
    const validTime=Number.isFinite(start)&&Number.isFinite(end)&&end>start;
    const valid=validTime&&/^-?\d+$/.test(point.known_value)&&(!usage||!point.known_value.startsWith("-"));
    return {...point,index,start,end,validTime,value:valid?BigInt(point.known_value):undefined};
  })??[];
  const knownPoints=seriesPoints.filter(point=>point.value!==undefined);
  const invalidSeriesPoints=seriesPoints.length-knownPoints.length;
  const seriesHasKnownValue=knownPoints.length>0;
  const signedSeries=!usage&&knownPoints.some(point=>point.value!<0n);
  const maximum=knownPoints.reduce((max,point)=>point.value!>max?point.value!:max,0n);
  const maxMagnitude=knownPoints.reduce((max,point)=>{const magnitude=point.value!<0n?-point.value!:point.value!;return magnitude>max?magnitude:max},0n);
  const plotY=(value:bigint)=>signedSeries?50-Number(value*500000n/(maxMagnitude||1n))/10000:100-Number(value*1000000n/(maximum||1n))/10000;
  const seriesSegments=seriesPoints.slice(1).flatMap((point,offset)=>{
    const previous=seriesPoints[offset];
    return previous.value!==undefined&&point.value!==undefined&&previous.end===point.start?[{from:previous,to:point}]:[];
  });
  useEffect(()=>{
    const node=seriesBarsRef.current;
    if(!node)return;
    const update=()=>setSeriesWidth(node.getBoundingClientRect().width);
    update();
    if(typeof ResizeObserver==="undefined")return;
    const observer=new ResizeObserver(update);observer.observe(node);return()=>observer.disconnect();
  },[series?.points.length,resolution]);
  const ticks=seriesTickIndexes(seriesPoints.length,seriesWidth);
  const usageDimensionName:Record<WorkbenchDimension,string>={purpose:t("m2688"),application:t("m1241"),account:t("m2689"),source_project:t("m1180"),cost_project:t("m1182"),model:t("m2690")};

  const errorNotice=error?<div className="workbench-error"><p role="alert">{error}</p>{reading?<><small>{tx("m2691")}</small>{canRetry?<button disabled={busy} onClick={()=>retryAction.current?.()}>{tx("m2692")}</button>:null}</>:<button disabled={busy} onClick={()=>void loadReading(appliedConfig)}>{tx("m2693")}</button>}</div>:null;
  const timezone=reading?.period.timezone||"UTC";
  const sourceSelection=config.filters.source_ids.length>1?"__selected_sources__":config.filters.source_ids[0]||"";
  const scrollSeries=resolution==="hourly"&&(series?.points.length||0)>30;
  const preview=details&&selected?<aside className="workbench-panel contribution-preview" ref={previewRef} tabIndex={-1} aria-label={t("m2694")}><header><div><span className="eyebrow">{tx("m2695")}</span><h2>{dimension==="purpose" ? purposeLabels[selected.label] || selected.label : selected.label}</h2><strong>{usage?t("m2696", { value0: selected.request_count }):formatMicros(selected.selected_amount_micros)}</strong>{usage?<small>{tx("m2697", { value0: formatMicros(selected.selected_amount_micros) })}</small>:null}</div><button disabled={busy} className="text-button" onClick={()=>{setDetails(null);setSelected(null)}}>{tx("m0455", { value0: usage?t("m2698"):t("m2699") })}</button></header><p className="boundary-note">{tx("m2700")}</p><div className="workbench-detail-list">{details.rows.map(row=><article key={row.evidence_event_id}><div><Identifier value={row.request_id} label={t("m2171")} /><span>{row.model||t("m1282")} · {row.terminal_status}</span></div><div><b>{formatMicros(basis==="gateway_charged"?row.gateway_charged_micros:row.calculated_reference_micros)}</b>{(session.session.permissions.includes("evidence.read")||(session.session.permissions.includes("investigation.scoped.read")&&row.investigation_available===true))?<button className="text-button" onClick={()=>navigate(`/executions/${encodeURIComponent(row.execution_id)}?as_of=${encodeURIComponent(reading!.cutoff)}&evidence=${encodeURIComponent(row.evidence_event_id)}`)}>{tx("m2701")}</button>:<small>{tx("m2702")}</small>}</div><div className="evidence-identity"><Identifier value={row.event_hash} label={t("m2703")} /></div><small>{tx("m2705", { value0: row.account_state==="known_account_owner"?t("m2704"):t("m0193"), value1: row.application_state==="known"?t("m2704"):t("m0193") })}</small>{row.source_facts?.map(fact=><SourceFactCard key={fact.source_fact_id} fact={fact} />)}<PurposePanel executionID={row.execution_id} readingID={reading?.reading_id} basis={{evidence_event_id:row.evidence_event_id,event_hash:row.event_hash}} session={session} onInvalidated={failure=>{clearReading();setError(() => problemText(failure))}}/></article>)}</div>{details.next_cursor?<button disabled={busy} className="text-button" onClick={()=>void loadMoreDetails()}>{tx("m2706")}</button>:null}</aside>:null;

  const sourceCoverage=reading?.source_coverage;
  const sourceIssues=sourceCoverage?[
    sourcesUnavailable?t("m2707"):"",
    sourceCoverage.economic_not_received>0?t("m2708", { value0: sourceCoverage.economic_not_received }):"",
    sourceCoverage.pending_predecessor>0?t("m2709", { value0: sourceCoverage.pending_predecessor }):"",
    sourceCoverage.economic_hash_conflicts>0?t("m2710", { value0: sourceCoverage.economic_hash_conflicts }):"",
    sourceCoverage.media_pending>0?t("m2711", { value0: sourceCoverage.media_pending }):"",
    sourceCoverage.not_captured>0?t("m2712", { value0: sourceCoverage.not_captured }):"",
    sourceCoverage.requires_attention?t("m2713"):"",
    sourceCoverage.collection_states?.some(state=>state!=="active")?t("m2714"):"",
    sourceCoverage.gaps.length>0?t("m2715", { value0: sourceCoverage.gaps.length }):"",
  ].filter(Boolean):[];
  const sourceFacts=sourceCoverage&&reading?<SourceFactsPanel key={reading.reading_id} reading={reading} onInvalidated={failure=>{clearReading();++requestSequence.current;++viewSequence.current;setBusy(false);setError(() => problemText(failure))}}/>:null;
  return <section className="workbench-page" aria-busy={busy}>
    <div className="workbench-hero"><div><span className="eyebrow">{tx("m0135")}</span>{!usage?<a className="budget-entry" href="/workbench/cost?view=budgets" onClick={event=>{if(event.button===0&&!event.metaKey&&!event.ctrlKey&&!event.shiftKey&&!event.altKey){event.preventDefault();navigate("/workbench/cost?view=budgets")}}}>{tx("m2716")}</a>:null}<h1>{usage?t("m0116"):t("m0117")}</h1><p>{usage?t("m2717"):t("m2718")}</p></div>{!usage?<div className="basis-switch" role="group" aria-label={t("m2719")}><button disabled={busy} className={basis==="gateway_charged"?"active":""} onClick={()=>void loadReading({...config,money_basis:"gateway_charged"})}>{tx("m2720")}</button><button disabled={busy} className={basis==="calculated_reference"?"active":""} onClick={()=>void loadReading({...config,money_basis:"calculated_reference"})}>{tx("m2721")}</button></div>:null}</div>
    <div className="workbench-controls"><label>{t("m1161")}<select aria-label={t("m1161")} disabled={busy} value={config.period.relative_hours??"inherited"} onChange={event=>setConfig({...config,period:{relative_hours:Number(event.target.value),display_timezone:config.period.display_timezone}})}>{!config.period.relative_hours?<option value="inherited">{tx("m2085")}</option>:null}<option value={24}>{tx("m0590")}</option><option value={72}>{tx("m2722")}</option><option value={168}>{tx("m0591")}</option><option value={720}>{tx("m0592")}</option></select></label><label>{t("m0472")}<select value={sourceSelection} disabled={busy||!sources?.items.length} onChange={event=>{if(event.target.value!=="__selected_sources__")setConfig({...config,filters:{...config.filters,source_ids:event.target.value?[event.target.value]:[]}})}}><option value="">{tx("m1165")}</option>{config.filters.source_ids.length>1?<option value="__selected_sources__">{tx("m2723", { value0: config.filters.source_ids.length })}</option>:sourceSelection&&!sources?.items.some(source=>source.source_system_id===sourceSelection)?<option value={sourceSelection}>{tx("m2724", { value0: compactIdentifier(sourceSelection) })}</option>:null}{sources?.items.map(source=><option key={source.source_system_id} value={source.source_system_id}>{source.display_name}</option>)}</select></label><label>{t("m1180")}{config.filters.source_projects.length>1?<small>{tx("m2725", { value0: config.filters.source_projects.length })}</small>:null}<input disabled={busy} value={config.filters.source_projects[0]||""} placeholder={t("m2726")} onChange={event=>setConfig({...config,filters:{...config.filters,source_projects:event.target.value?[event.target.value]:[]}})}/></label><button disabled={busy} onClick={()=>void loadReading(config)}>{tx("m2509")}</button></div>
    {reading?<details className="workbench-active-scope"><summary>{tx("m2727")}</summary><p>{tx("m2728", { value0: appliedConfig.filters.source_ids.join("、")||t("m2110"), value1: appliedConfig.filters.source_projects.join("、")||t("m2110"), value2: appliedConfig.filters.cost_projects.join("、")||t("m2110") })}</p></details>:null}
    {errorNotice}
    {usage?<p className="boundary-note usage-boundary">{tx("m2729")}</p>:null}
    <div className="workbench-grid">
      {usage?<>
        <article className="metric-card primary"><span>{tx("m2730")}</span><strong>{summary?.request_count??"—"}</strong><small>{summary?.request_total_complete?t("m2731"):t("m2732")}</small></article>
        <article className="metric-card"><span>{tx("m2733")}</span><strong>{reading?.dimensions.includes("purpose")&&summary?`${summary.purpose_known_requests} / ${summary.request_count}`:"—"}</strong><small>{reading?.dimensions.includes("purpose")?t("m2734"):t("m2735")}</small></article>
      </>:<>
        <article className="metric-card primary"><span>{reading?(reading.money_basis==="gateway_charged"?t("m2720"):t("m2721")):t("m2736")}</span><strong>{summary?formatMicros(summary.selected_amount_micros):"—"}</strong><small>{summary?.amount_complete?t("m2737"):t("m2738")}</small></article>
        <article className="metric-card"><span>{tx("m2739")}</span><strong>{summary?.request_count??"—"}</strong><small>{summary?.request_total_complete?t("m2740"):t("m2741")}</small></article>
      </>}
      <article className="metric-card reading-period"><span>{tx("m2742")}</span><p>{reading?`${formatTime(reading.period.start_inclusive,timezone)} → ${formatTime(reading.period.end_exclusive,timezone)}`:busy?t("m1150"):error?t("m2743"):t("m2744")}</p><small>{tx("m2748", { value0: appliedConfig.period.start_inclusive?(reading?t("m2745"):busy?t("m2746"):t("m2747")):"", value1: reading?formatTime(reading.cutoff,timezone):"—" })}</small><details><summary>{tx("m2749")}</summary><code>{reading?.reading_id}</code><dl><dt>{tx("m2750")}</dt><dd>{reading?.membership_ref}</dd><dt>{tx("m2115")}</dt><dd>{reading?.interpretation_revision}</dd></dl></details></article>
    </div>
    {usage?<p className="usage-money">{t("m2751")}{reading?(reading.money_basis==="gateway_charged"?t("m2720"):t("m2721")):t("m2752")}：<strong>{summary?formatMicros(summary.selected_amount_micros):"—"}</strong>。{summary?.amount_complete?t("m2753"):t("m2754")}</p>:null}
    <div className={`workbench-comparison${preview?" with-preview":""}`}><div className="workbench-two-column"><article className="workbench-panel"><header><div><span className="eyebrow">{tx("m1212")}</span><h2>{tx("m2755", { value0: usage?t("m1211"):t("m2736") })}</h2></div><span className={`coverage-pill coverage-${summary?.coverage.status||"unknown"}`}>{coverageLabel(summary?.coverage.status)}</span></header><label className="series-resolution">{t("m2756")}<select aria-label={t("m2756")} value={resolution} disabled={busy||!reading} onChange={event=>void changeResolution(event.target.value as "overview"|"hourly")}><option value="overview">{tx("m2757")}</option><option value="hourly">{tx("m2758")}</option></select><span>{series?t("m2759", { value0: series.bucket_seconds/3600, value1: timezone }):"—"}</span></label>
      {series&&!seriesHasKnownValue?<p className="series-empty">{tx("m2762", { value0: usage?t("m1211"):t("m2736"), value1: summary?.coverage.period_completeness==="unknown"?t("m2760"):t("m2761") })}</p>:<div className="series-scroll" role="region" aria-label={t("m2763")} tabIndex={scrollSeries?0:undefined}>
        <div ref={seriesBarsRef} className={`series-bars${scrollSeries?" series-hourly":""}${signedSeries?" series-bars--signed":""}`} data-reading-id={reading?.reading_id}>
          {signedSeries?<span className="series-zero-line" aria-hidden="true"/>:null}
          <svg className="series-trend-lines" viewBox="0 0 1000 100" preserveAspectRatio="none" aria-hidden="true">{seriesSegments.map(({from,to})=><line key={`${from.index}:${to.index}`} className="series-trend-segment" data-from={from.index} data-to={to.index} x1={(from.index+.5)*1000/seriesPoints.length} x2={(to.index+.5)*1000/seriesPoints.length} y1={plotY(from.value!)} y2={plotY(to.value!)} />)}</svg>
          {seriesPoints.map((point,index)=>{const description=point.value===undefined?t("m2764", { value0: point.start_inclusive, value1: point.end_exclusive }): `${formatTime(point.start_inclusive,timezone)} → ${formatTime(point.end_exclusive,timezone)}：${usage?t("m2696", { value0: point.known_value }):formatMicros(point.known_value)}${point.complete?"":t("m2765")}`;const direction=point.value===undefined?"invalid":point.value<0n?"negative":point.value>0n?"positive":"zero";return <div key={`${point.start_inclusive}:${index}`} tabIndex={0} aria-label={description} title={description} data-value={point.known_value} data-direction={direction} data-start={point.start_inclusive} data-end={point.end_exclusive} onFocus={()=>setActiveBucket(description)} onMouseEnter={()=>setActiveBucket(description)}>{point.value!==undefined?<span className={`series-trend-dot${point.complete?"":" series-trend-dot--partial"}`} aria-hidden="true" style={{top:`${plotY(point.value)}%`}}/>:null}{ticks.has(index)&&point.validTime?<span className="series-tick" aria-hidden="true">{formatTime(point.start_inclusive,timezone,true)}</span>:null}</div>})}
        </div>
      </div>}
      {signedSeries?<p className="series-sign-summary">{tx("m2766", { value0: knownPoints.filter(point=>point.value!<0n).length, value1: knownPoints.filter(point=>point.value!>0n).length })}</p>:null}
      {scrollSeries?<p className="boundary-note">{tx("m2767")}</p>:null}
      {seriesHasKnownValue?<p className="series-value" aria-live="polite">{activeBucket||t("m2769", { value0: usage?t("m2768"):t("m2736") })}</p>:null}
      {series&&!series.points.every(point=>point.complete)?<p className="boundary-note">{tx("m2770")}</p>:null}
      {invalidSeriesPoints>0?<p className="boundary-note" role="status">{tx("m2771", { value0: invalidSeriesPoints })}</p>:null}
      {summary?<details className="coverage-disclosure"><summary>{tx("m2772")}</summary><CoverageExplanation coverage={summary.coverage} timezone={timezone} sourceCount={sources?.items.length}/></details>:null}</article>
      </div>
    <article className="workbench-panel contributions-panel"><header><div><span className="eyebrow">{usage?t("m2698"):t("m2773")}</span><h2>{usage?t("m2774", { value0: usageDimensionName[dimension] }):t("m2775")}</h2></div><div className="contribution-controls"><select disabled={busy||!reading} aria-label={t("m2776")} value={dimension} onChange={event=>void changeDimension(event.target.value)}><option value="source_project">{tx("m1180")}</option><option value="model">{tx("m2690")}</option><option value="cost_project">{tx("m1182")}</option>{reading?.dimensions.includes("application")?<option value="application">{tx("m1241")}</option>:null}{reading?.dimensions.includes("account")?<option value="account">{tx("m2689")}</option>:null}{reading?.dimensions.includes("purpose")?<option value="purpose">{tx("m2777")}</option>:null}</select><select disabled={busy||!reading} aria-label={t("m2778")} value={contributionMode} onChange={event=>void changeContributionMode(event.target.value as "top5"|"top10"|"all")}><option value="top5">Top 5</option><option value="top10">Top 10</option><option value="all">{tx("m2779")}</option></select></div></header>
      {reading ? <p>{reading.purpose?.status==="available"?t("m2780"):t("m2781")}{usage?t("m2784", { value0: reading.dimensions.includes("application")?t("m2782"):t("m2783"), value1: reading.dimensions.includes("account")?t("m2782"):t("m2783") }):""}</p> : null}
      <div className="contribution-list">{contributions?.rows.map(row=><button key={row.ref} disabled={busy||!row.drilldown_allowed} aria-pressed={selected?.ref===row.ref} onClick={()=>void openDetails(row)}><span><strong title={dimension==="purpose" ? purposeLabels[row.label] || row.label : row.label}>{dimension==="purpose" ? purposeLabels[row.label] || row.label : row.label}</strong><small>{usage?t("m2785", { value0: summary?formatShare(String(row.request_count),String(summary.request_count)):"—" }):t("m2786", { value0: row.request_count })}{row.drilldown_allowed?t("m2787"):t("m2788")}</small></span><span><b>{usage?t("m2786", { value0: row.request_count }):formatMicros(row.selected_amount_micros)}</b><small>{usage?t("m2789", { value0: formatMicros(row.selected_amount_micros) }):formatShare(row.share.numerator,row.share.denominator)}</small></span></button>)}{contributions?.rows.length===0?<p>{tx("m2790")}</p>:null}</div>{contributions?.next_cursor?<button disabled={busy} className="text-button" onClick={()=>void loadMoreContributions()}>{tx("m2791")}</button>:null}
      <footer>{usage?<><span>{tx("m2792", { value0: contributions?.other_known_requests??"—" })}</span><span>{tx("m2793", { value0: contributions?.unassigned_requests??"—" })}</span><strong>{tx("m2794", { value0: contributions?.denominator_requests??"—" })}</strong></>:<><span>{tx("m2795", { value0: contributions?formatMicros(contributions.other_known_micros):"—" })}</span><span>{tx("m2796", { value0: contributions?formatMicros(contributions.unassigned_micros):"—" })}</span><strong>{tx("m2797", { value0: contributions?formatMicros(contributions.denominator_micros):"—" })}</strong></>}</footer></article>
    {preview}</div>
    <details className="workbench-disclosure"><summary>{usage&&sourceCoverage?t("m2799", { value0: sourceCoverage.known_documents, value1: sourceIssues.length?sourceIssues.join("；"):t("m2798") }):t("m2800")}</summary>      <article className="workbench-panel"><header><div><span className="eyebrow">{tx("m2801")}</span><h2>{tx("m2802")}</h2></div></header><div className="source-list">{sources?.items.map(source=><div key={source.source_system_id}><strong>{source.display_name}</strong><small>{tx("m2803")}</small><span>{sourceStatusLabel(source.status)}</span><small>{tx("m2804", { value0: source.last_received_at?formatTime(source.last_received_at,timezone):t("m0193") })}</small><small>{tx("m2805", { value0: source.projection_watermark?formatTime(source.projection_watermark,timezone):t("m0193") })}</small><SourceCollectionStatus items={source.source_facts} formatTime={value=>formatTime(value,timezone)}/></div>)}{sources?.items.length===0&&!sourcesUnavailable?<p>{tx("m2806")}</p>:null}{sourcesUnavailable?<p>{sourcesNotice}</p>:null}</div></article>
      {sourceFacts}
    </details>
    <details className="workbench-disclosure"><summary>{tx("m2807")}</summary>
    {!preferencesUnavailable?<article className="workbench-panel saved-filter-panel"><header><div><span className="eyebrow">{tx("m2808")}</span><h2>{tx("m2809")}</h2></div></header><div><input value={saveName} maxLength={120} onChange={event=>setSaveName(event.target.value)}/><button disabled={busy||!reading||!saveName.trim()} onClick={()=>void saveCurrent()}>{tx("m2810")}</button></div><ul>{saved.map(item=><li key={item.id}><span><strong>{item.name}</strong><small>{tx("m2811", { value0: item.revision })}</small></span><span className="saved-actions"><button disabled={busy} onClick={()=>void loadReading(item.config)}>{tx("m2812")}</button><button disabled={busy||!reading} onClick={()=>void updateSaved(item)}>{tx("m2813")}</button></span></li>)}</ul></article>:<article className="workbench-panel saved-filter-panel"><p>{preferencesNotice}</p><small>{tx("m2814")}</small></article>}
    </details>
  </section>
}

function formatMicros(value:string){const negative=value.startsWith("-");const digits=negative?value.slice(1):value;const padded=digits.padStart(7,"0");return `${negative?"-":""}¥${padded.slice(0,-6)}.${padded.slice(-6)}`}
export function seriesTickIndexes(count:number,width:number){const indexes=new Set<number>();if(count===0)return indexes;indexes.add(0);indexes.add(count-1);const slots=Math.max(2,Math.floor(width/125));if(count<=slots){for(let index=1;index<count-1;index++)indexes.add(index)}else for(let index=1;index<slots-1;index++)indexes.add(Math.round(index*(count-1)/(slots-1)));return indexes}
function formatShare(value:string,total:string){try{const denominator=BigInt(total);if(denominator===0n)return t("m1139");return `${Number(BigInt(value)*1000n/denominator)/10}%`}catch{return "—"}}
function coverageLabel(value?:string){return value==="scope_limited_unknown"?t("m2815"):value==="not_connected"?t("m2816"):value==="disconnected"?t("m1602"):value==="observed_with_gaps"?t("m2817"):value==="observed"?t("m2818"):t("m1138")}
function sourceStatusLabel(value:string){return value==="scope_limited_unknown"?t("m2815"):value==="not_connected"?t("m2816"):value==="disconnected"?t("m1602"):value==="observed_with_gaps"?t("m2817"):t("m2818")}
function problemText(failure:unknown){if(failure instanceof ApiRequestError){if(failure.problem?.code==="workbench_access_changed")return t("m2819");if(failure.problem?.code==="workbench_reading_expired")return t("m2820");return t("m2821", { value0: failure.status })}return t("m2822")}
function optionalFailureText(failure:unknown,feature:string){if(failure instanceof ApiRequestError&&failure.status===403)return t("m2823", { value0: feature });return t("m2824", { value0: feature })}

function invalidatesReading(failure:unknown){return failure instanceof ApiRequestError&&([401,403,404,410].includes(failure.status)||failure.problem?.code==="workbench_access_changed")}
function overviewSeconds(reading:WorkbenchReading){const hours=(Date.parse(reading.period.end_exclusive)-Date.parse(reading.period.start_inclusive))/3600000;return hours<=24?3600:hours<=72?10800:hours<=168?21600:86400}
function formatTime(value:string,timezone:string,compact=false){return new Intl.DateTimeFormat(getLocale(),{timeZone:timezone,month:"2-digit",day:"2-digit",hour:"2-digit",minute:compact?undefined:"2-digit",hourCycle:"h23",...(compact?{}:{year:"numeric" as const,timeZoneName:"short" as const})}).format(new Date(value))}
const gapLabels:Record<string,string>=localizedLabels({}, {source_period_completeness_not_asserted: "m2825", period_precedes_capture_start: "m2826", source_profile_capture_gaps_present: "m2827", source_not_currently_connected: "m2828", scope_limited_source_state_unknown: "m2829"});
function CoverageExplanation({coverage,timezone,sourceCount}:{coverage:WorkbenchCoverage;timezone:string;sourceCount?:number}){
  useLocale();
  const restricted=coverage.status==="scope_limited_unknown";
  const gaps=restricted?["source_period_completeness_not_asserted","scope_limited_source_state_unknown"]:coverage.gaps;
  return <section className="coverage-explanation" aria-label={t("m2830")}><strong>{tx("m2831")}</strong>
    <p>{coverage.projection_complete_for_members?t("m2832"):t("m2833")}</p>
    {!restricted?<dl><dt>{sourceCount===1?t("m2834"):t("m2835")}</dt><dd>{coverage.capture_started_at?formatTime(coverage.capture_started_at,timezone):t("m0193")}</dd><dt>{tx("m2836")}</dt><dd>{coverage.last_received_at?formatTime(coverage.last_received_at,timezone):t("m0193")}</dd><dt>{tx("m2837")}</dt><dd>{coverage.received_watermark?t("m2838"):t("m0193")}</dd></dl>:null}
    <p>{tx("m2839", { value0: coverage.projection_watermark?formatTime(coverage.projection_watermark,timezone):t("m0193") })}</p>
    <ul>{[...new Set(gaps)].map(gap=><li key={gap}>{gapLabels[gap]||t("m2840")}</li>)}</ul>
  </section>
}
