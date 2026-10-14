import { localizedLabels, t, tx, useLocale, useMessageState } from "./i18n";
import { ResourceUpdates } from "./ReviewBasisView";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiRequestError, investigationQueueAPI, isAbortError, queueFilterQuery, type AccessSessionResponse, type InvestigationQueueFilter, type InvestigationQueueFilters, type InvestigationQueueItem, type InvestigationQueuePage as QueuePage, type QueueFilterCommand } from "./api";
import { ErrorState, LoadingBlock } from "./Feedback";
import { formatDateTime } from "./format";
import { clearPendingCommand, keepPendingCommand, readPendingCommand } from "./pendingReview";

const defaults: InvestigationQueueFilters = { mode: "current", period: "24h", sort: "created_desc" };
const labels: Record<string,string> = localizedLabels({}, {"24h": "m0817", "7d": "m0818", custom: "m0819", created_desc: "m0820", due_asc: "m0821", me: "m0822", current: "m0823", created: "m0824", reassessment: "m0825", unrecorded: "m0710", not_started: "m0826", in_progress: "m0827", ended: "m0828", pending_review: "m0829", supported: "m0830", not_supported: "m0831", insufficient_evidence: "m0832", unchanged: "m0833", changed: "m0834", unavailable: "m0835", active: "m0836", disabled: "m0837", not_found: "m0838", unassigned: "m0759", open: "m0839", sealed: "m0840"});
const label=(value:string)=>labels[value]??value;
export function queueFiltersFromLocation(): InvestigationQueueFilters {
 const result={...defaults};const params=new URLSearchParams(window.location.search);
 for(const key of ["mode","period","sort","from","to","assignee","progress","conclusion","basis","case_status","search"] as const){if(params.has(key))result[key]=params.get(key)??""}
 return result;
}
type Pending={kind:"create"|"update"|"delete";id:string;command:QueueFilterCommand};
export function isPendingQueueFilter(value:unknown):value is Pending {
 if(!value||typeof value!=="object")return false;const p=value as Pending;
 return ["create","update","delete"].includes(p.kind)&&typeof p.id==="string"&&(p.kind==="create"?p.id==="":/^xqf_[A-Za-z0-9_-]{22}$/.test(p.id))&&!!p.command&&
 typeof p.command.operation_id==="string"&&/^[a-f0-9-]{36}$/.test(p.command.operation_id)&&Number.isInteger(p.command.expected_revision)&&p.command.expected_revision>=0&&
 p.command.schema_version==="xinsight.investigation-queue-filter.v1"&&typeof p.command.name==="string"&&!!p.command.filters&&
 typeof p.command.filters.mode==="string"&&typeof p.command.filters.period==="string"&&typeof p.command.filters.sort==="string";
}

export function InvestigationQueuePage({session,navigate}:{session:AccessSessionResponse;navigate:(path:string)=>void}) {
  useLocale();
 const [active,setActive]=useState(queueFiltersFromLocation);const [draft,setDraft]=useState(active);
 const [page,setPage]=useState<QueuePage>();const [items,setItems]=useState<InvestigationQueueItem[]>([]);const [loading,setLoading]=useState(false);
 const [saved,setSaved]=useState<InvestigationQueueFilter[]>([]);const [selected,setSelected]=useState<InvestigationQueueFilter>();const [name,setName]=useState("");
 const [remote,setRemote]=useState<InvestigationQueueFilter>();const [needsReload,setNeedsReload]=useState(false);
 const [pending,setPending]=useState<Pending>();const [busy,setBusy]=useState(false);const [error,setError]=useState<unknown>();const [notice,setNotice]=useMessageState("");const [recoveryInvalid,setRecoveryInvalid]=useState(false);
 const life=useRef(new AbortController());const generation=useRef(0);const querySequence=useRef(0);const queryLife=useRef(new AbortController());
 const savedSequence=useRef(0);const editorEpoch=useRef(0);
 const subject=session.session.subject_id;const key="xinsight.pending-queue-filter.v1:"+subject;
 const canSave=session.session.permissions.includes("investigation.preferences.write");
 async function load(filters:InvestigationQueueFilters,cursor?:string) {
  const token=generation.current;const sequence=++querySequence.current;queryLife.current.abort();const control=new AbortController();queryLife.current=control;setLoading(true);setError(undefined);
  try{const value=await investigationQueueAPI.list(filters,cursor,control.signal);if(token!==generation.current||sequence!==querySequence.current)return;setPage(value);setItems(old=>cursor?[...old,...value.items]:value.items)}
  catch(e){if(token!==generation.current||sequence!==querySequence.current||isAbortError(e))return;setError(e);setPage(undefined);setItems([])}
  finally{if(token===generation.current&&sequence===querySequence.current)setLoading(false)}
 }
 async function loadSaved(editor?:{id:string;revision?:number;epoch:number;confirmed?:boolean}) {
  const token=generation.current;const sequence=++savedSequence.current;
  try {
   const value=await investigationQueueAPI.filters(life.current.signal);
   if(token!==generation.current||sequence!==savedSequence.current)return;
   setSaved(value.items);
   if(!editor){setNeedsReload(false);return}
   if(editor.epoch!==editorEpoch.current)return;
   const current=value.items.find(item=>item.id===editor.id);
   setNeedsReload(false);
   if(editor.confirmed){choose(current);return}
   if(!current){editorEpoch.current++;setSelected(undefined);setRemote(undefined);setNotice(() => t("m0841"));return}
   if(current.revision!==editor.revision){setRemote(current);setNotice(() => t("m0842"));return}
   setRemote(undefined);setNotice(() => t("m0843"));
  } catch(e) {if(token===generation.current&&sequence===savedSequence.current&&!isAbortError(e))throw e}
 }
 useEffect(()=>{
  generation.current++;editorEpoch.current++;life.current=new AbortController();setPage(undefined);setItems([]);setSaved([]);setSelected(undefined);setName("");setRemote(undefined);setNeedsReload(false);setPending(undefined);setRecoveryInvalid(false);setNotice(() => "");
  try{setPending(readPendingCommand(key,isPendingQueueFilter))}catch(e){setRecoveryInvalid(true);setError(e)}
  if(canSave)void loadSaved().catch(e=>{if(!isAbortError(e))setError(e)});
  return()=>{generation.current++;life.current.abort();queryLife.current.abort()};
  // Identity and current permissions bound every pending operation and response.
  // eslint-disable-next-line react-hooks/exhaustive-deps
 },[subject,session.session.permissions.join(",")]);
 useEffect(()=>{void load(active);return()=>queryLife.current.abort();
  // eslint-disable-next-line react-hooks/exhaustive-deps
 },[active,subject,session.session.permissions.join(",")]);
 function editDraft(filters:InvestigationQueueFilters){editorEpoch.current++;setDraft(filters)}
 function apply(filters:InvestigationQueueFilters){setActive({...filters});editDraft({...filters});window.history.replaceState(null,"","/investigation/queue?"+queueFilterQuery(filters))}
 function search(event:FormEvent){event.preventDefault();apply(draft)}
 function choose(item?:InvestigationQueueFilter){editorEpoch.current++;setSelected(item);setRemote(undefined);setNeedsReload(false);setName(item?.name??"");if(item)apply(item.filters)}
 function reconcile(keepDraft:boolean){if(!remote)return;editorEpoch.current++;if(keepDraft){setSelected(remote);setRemote(undefined);setNeedsReload(false)}else choose(remote);setError(undefined);setNotice(() => keepDraft?t("m0844"):t("m0845"))}
 function reread(){setError(undefined);void loadSaved(selected?{id:selected.id,revision:selected.revision,epoch:editorEpoch.current}:undefined).catch(setError)}
 async function submit(value:Pending){
  const token=generation.current;const epoch=editorEpoch.current;setBusy(true);setError(undefined);setNotice(() => "");
  try{
   keepPendingCommand(key,value);setPending(value);
   const receipt=await investigationQueueAPI.save(value.kind,value.id,value.command,session.csrf_token,life.current.signal);
   if(token!==generation.current)return;clearPendingCommand(key,value);setPending(undefined);setNotice(() => t("m0846"));
   await loadSaved({id:receipt.id,epoch,confirmed:true}).catch(e=>{if(token===generation.current&&!isAbortError(e)){setNeedsReload(true);setError(e);setNotice(() => t("m0847"))}});
  }catch(e){
   if(token!==generation.current||isAbortError(e))return;
   if(e instanceof ApiRequestError&&[400,401,403,404,409].includes(e.status)){clearPendingCommand(key,value);setPending(undefined);if([401,403].includes(e.status)){setSaved([]);setSelected(undefined);setRemote(undefined)};if([404,409].includes(e.status)){setNeedsReload(value.kind!=="create");setNotice(() => t("m0848"))}}else{setNotice(() => t("m0849"));}
   setError(e);
  }finally{if(token===generation.current)setBusy(false)}
 }
 function mutate(kind:Pending["kind"]){const command:QueueFilterCommand={operation_id:crypto.randomUUID(),expected_revision:kind==="create"?0:selected?.revision??0,schema_version:"xinsight.investigation-queue-filter.v1",name:kind==="delete"?selected?.name??"":name,filters:kind==="delete"?selected?.filters??draft:draft};void submit({kind,id:kind==="create"?"":selected?.id??"",command})}
 const blocked=busy||!!pending||recoveryInvalid;
 return <div className="page-stack investigation-queue">
  <header className="page-header"><div><p className="eyebrow">Investigation</p><h1>{tx("m0134")}</h1><p>{tx("m0850")}</p></div><button className="button" onClick={()=>void load(active)} disabled={loading}>{tx("m0851")}</button></header>
  <p className="queue-coverage">{tx("m0852")}</p>
  <div className="queue-modes" aria-label={t("m0853")}>{["current","created","reassessment"].map(mode=><button key={mode} className={active.mode===mode?"button button-primary":"button"} aria-pressed={active.mode===mode} onClick={()=>apply({...active,mode})}>{label(mode)} <strong>{page?.statistics[mode as "current"|"created"|"reassessment"]??"—"}</strong></button>)}</div>
  <details className="queue-options"><summary>{tx("m0854")}</summary>
  <form className="panel queue-filters" onSubmit={search} aria-label={t("m0855")}>
   <label>{t("m0856")}<select value={draft.period} onChange={e=>editDraft({...draft,period:e.target.value,from:undefined,to:undefined})}><option value="24h">{tx("m0817")}</option><option value="7d">{tx("m0818")}</option><option value="custom">{tx("m0819")}</option></select></label>
   {draft.period==="custom"?<><label>{t("m0857")}<input value={draft.from??""} placeholder="2026-09-01T00:00:00+08:00" onChange={e=>editDraft({...draft,from:e.target.value})}/></label><label>{t("m0858")}<input value={draft.to??""} placeholder="2026-09-10T00:00:00+08:00" onChange={e=>editDraft({...draft,to:e.target.value})}/></label></>:null}
   <label>{t("m0792")}<select value={draft.assignee??""} onChange={e=>editDraft({...draft,assignee:e.target.value})}><option value="">{tx("m0859")}</option><option value="me">{tx("m0822")}</option><option value="unassigned">{tx("m0759")}</option><option value="inactive">{tx("m0860")}</option>{draft.assignee&&!['me','unassigned','inactive'].includes(draft.assignee)?<option value={draft.assignee}>{draft.assignee}</option>:null}</select></label>
   <label>{t("m0861")}<select value={draft.progress??""} onChange={e=>editDraft({...draft,progress:e.target.value})}><option value="">{tx("m0862")}</option>{["unrecorded","not_started","in_progress","ended"].map(v=><option key={v} value={v}>{label(v)}</option>)}</select></label>
   <label>{t("m0756")}<select value={draft.conclusion??""} onChange={e=>editDraft({...draft,conclusion:e.target.value})}><option value="">{tx("m0863")}</option>{["unrecorded","pending_review","supported","not_supported","insufficient_evidence"].map(v=><option key={v} value={v}>{label(v)}</option>)}</select></label>
   <label>{t("m0864")}<select value={draft.basis??""} onChange={e=>editDraft({...draft,basis:e.target.value})}><option value="">{tx("m0865")}</option>{["unrecorded","unchanged","changed","unavailable"].map(v=><option key={v} value={v}>{label(v)}</option>)}</select></label>
   <label>{t("m0866")}<select value={draft.case_status??""} onChange={e=>editDraft({...draft,case_status:e.target.value})}><option value="">{tx("m0867")}</option><option value="open">{tx("m0839")}</option><option value="sealed">{tx("m0868")}</option></select></label>
   <label>{t("m0869")}<select value={draft.sort} onChange={e=>editDraft({...draft,sort:e.target.value})}><option value="created_desc">{tx("m0820")}</option><option value="due_asc">{tx("m0821")}</option></select></label>
   <label>{t("m0870")}<input value={draft.search??""} maxLength={200} onChange={e=>editDraft({...draft,search:e.target.value})} placeholder={t("m0871")}/></label><button className="button button-primary" disabled={loading}>{tx("m0872")}</button>
  </form>
  {canSave?<section className="panel queue-saved" aria-label={t("m0873")}><label>{t("m0874")}<select value={selected?.id??""} disabled={blocked} onChange={e=>choose(saved.find(v=>v.id===e.target.value))}><option value="">{tx("m0875")}</option>{saved.map(item=><option key={item.id} value={item.id}>{item.name}</option>)}</select></label><label>{t("m0876")}<input maxLength={120} value={name} disabled={blocked} onChange={e=>{editorEpoch.current++;setName(e.target.value)}}/></label><button className="button" disabled={blocked||needsReload||!!remote||!name.trim()} onClick={()=>mutate(selected?"update":"create")}>{selected?t("m0877"):t("m0878")}</button>{selected?<button className="button" disabled={blocked||needsReload||!!remote} onClick={()=>mutate("delete")}>{tx("m0879")}</button>:null}<button className="button" disabled={blocked} onClick={reread}>{tx("m0880")}</button></section>:null}
  </details>
  {remote?<section className="panel" aria-label={t("m0881")}><h2>{tx("m0882")}</h2><p>{tx("m0883", { value0: remote.revision, value1: remote.name })}</p><dl>{Object.entries(remote.filters).filter(([,value])=>value!==undefined&&value!=="").map(([field,value])=><div key={field}><dt>{({mode:t("m0853"),period:t("m0856"),sort:t("m0869"),from:t("m0884"),to:t("m0885"),assignee:t("m0792"),progress:t("m0861"),conclusion:t("m0756"),basis:t("m0864"),case_status:t("m0866"),search:t("m0870")} as Record<string,string>)[field]??field}</dt><dd>{label(String(value))}</dd></div>)}</dl><p>{tx("m0886")}</p><button className="button" disabled={blocked} onClick={()=>reconcile(false)}>{tx("m0887")}</button><button className="button" disabled={blocked} onClick={()=>reconcile(true)}>{tx("m0888")}</button></section>:null}
  {pending?<div className="panel"><p>{tx("m0889")}</p><button className="button" disabled={busy||!canSave} onClick={()=>void submit(pending)}>{tx("m0890")}</button></div>:null}
  {recoveryInvalid?<p role="alert">{tx("m0891")}</p>:null}
  {notice?<p role="status">{notice}</p>:null}{error?<ErrorState error={error}/>:null}{loading?<LoadingBlock label={t("m0892")}/>:null}
  {page?<><div className="queue-statistics"><span>{t("m0893")}<strong>{page.statistics.unassigned}</strong></span><span>{t("m0894")}<strong>{page.statistics.overdue}</strong></span><span>{t("m0895")}<strong>{page.statistics.basis_unrecorded}</strong></span><span>{t("m0896")}<strong>{page.statistics.basis_changed}</strong></span><span>{t("m0897")}<strong>{page.statistics.basis_unavailable}</strong></span></div><p>{tx("m0898", { value0: formatDateTime(page.evaluated_at) })}</p>
  <section className="panel"><h2>{tx("m0899", { value0: label(active.mode), value1: page.total })}</h2><div className="queue-table-scroll"><table className="queue-table"><thead><tr><th>{tx("m0900")}</th><th>{tx("m0901")}</th><th>{tx("m0902")}</th><th>{tx("m0903")}</th><th>{tx("m0904")}</th><th>{tx("m0079")}</th></tr></thead><tbody>{items.map(row=><tr key={row.finding.id}><td><strong>{row.finding.title}</strong><small>{row.case_title} · {label(row.case_status)}</small><small>{tx("m0905", { value0: row.finding.classification, value1: row.finding.status })}</small></td><td>{label(row.conclusion)}<small>{label(row.progress)}</small></td><td>{label(row.basis)}<ResourceUpdates updates={row.resource_updates} /><small>{tx("m0906", { value0: label(row.review_basis), value1: label(row.progress_basis) })}</small></td><td>{row.followup.assignee_subject_id?<button className="queue-person" onClick={()=>apply({...active,assignee:row.followup.assignee_subject_id??""})}>{row.followup.assignee_name}</button>:t("m0759")}<small>{label(row.assignee_state)}</small><small>{row.followup.due_at?formatDateTime(row.followup.due_at):t("m0760")} {row.overdue?t("m0907"):""}</small>{row.followup.updated_at?<small>{tx("m0908", { value0: formatDateTime(row.followup.updated_at) })}</small>:null}</td><td>{formatDateTime(row.finding.created_at)}</td><td><button className="button" onClick={()=>navigate(`/cases/${encodeURIComponent(row.finding.case_id)}?queue_return=${encodeURIComponent("/investigation/queue?"+queueFilterQuery(active))}&finding=${encodeURIComponent(row.finding.id)}`)}>{tx("m0909")}</button></td></tr>)}</tbody></table></div>{items.length===0?<p>{tx("m0910")}</p>:null}<p>{tx("m0911", { value0: items.length, value1: page.total })}</p>{page.next_cursor?<button className="button" disabled={loading} onClick={()=>void load(active,page.next_cursor)}>{tx("m0912")}</button>:null}</section></>:null}
 </div>;
}
