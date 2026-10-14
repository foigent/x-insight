import { ResourceUpdates } from "./ReviewBasisView";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiRequestError, investigationQueueAPI, isAbortError, queueFilterQuery, type AccessSessionResponse, type InvestigationQueueFilter, type InvestigationQueueFilters, type InvestigationQueueItem, type InvestigationQueuePage as QueuePage, type QueueFilterCommand } from "./api";
import { ErrorState, LoadingBlock } from "./Feedback";
import { formatDateTime } from "./format";
import { clearPendingCommand, keepPendingCommand, readPendingCommand } from "./pendingReview";

const defaults: InvestigationQueueFilters = { mode: "current", period: "24h", sort: "created_desc" };
const labels: Record<string,string> = { "24h":"近24小时", "7d":"近7天", custom:"明确时间范围", created_desc:"新建时间从近到远", due_asc:"期限从近到远", me:"本人", current:"当前未结束", created:"本期新增", reassessment:"依据变化待复核", unrecorded:"未记录", not_started:"未开始", in_progress:"跟进中", ended:"已结束跟进", pending_review:"待复核", supported:"证据支持", not_supported:"证据不支持", insufficient_evidence:"证据不足", unchanged:"依据未变", changed:"依据已变化", unavailable:"依据不可用", active:"账号有效", disabled:"账号已失效", not_found:"账号不可用", unassigned:"未分配", open:"开放", sealed:"已封存" };
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
 const [active,setActive]=useState(queueFiltersFromLocation);const [draft,setDraft]=useState(active);
 const [page,setPage]=useState<QueuePage>();const [items,setItems]=useState<InvestigationQueueItem[]>([]);const [loading,setLoading]=useState(false);
 const [saved,setSaved]=useState<InvestigationQueueFilter[]>([]);const [selected,setSelected]=useState<InvestigationQueueFilter>();const [name,setName]=useState("");
 const [remote,setRemote]=useState<InvestigationQueueFilter>();const [needsReload,setNeedsReload]=useState(false);
 const [pending,setPending]=useState<Pending>();const [busy,setBusy]=useState(false);const [error,setError]=useState<unknown>();const [notice,setNotice]=useState("");const [recoveryInvalid,setRecoveryInvalid]=useState(false);
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
   if(!current){editorEpoch.current++;setSelected(undefined);setRemote(undefined);setNotice("该个人筛选已被删除。当前草稿已保留，可另存为新的个人筛选。");return}
   if(current.revision!==editor.revision){setRemote(current);setNotice("保存版本已有变化。草稿已保留，请选择如何继续。");return}
   setRemote(undefined);setNotice("已重读保存列表，当前草稿保持不变。");
  } catch(e) {if(token===generation.current&&sequence===savedSequence.current&&!isAbortError(e))throw e}
 }
 useEffect(()=>{
  generation.current++;editorEpoch.current++;life.current=new AbortController();setPage(undefined);setItems([]);setSaved([]);setSelected(undefined);setName("");setRemote(undefined);setNeedsReload(false);setPending(undefined);setRecoveryInvalid(false);setNotice("");
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
 function reconcile(keepDraft:boolean){if(!remote)return;editorEpoch.current++;if(keepDraft){setSelected(remote);setRemote(undefined);setNeedsReload(false)}else choose(remote);setError(undefined);setNotice(keepDraft?"已采用新的保存版本，草稿保持不变。请核对后再保存。":"已载入新的保存版本，可继续修改或删除。")}
 function reread(){setError(undefined);void loadSaved(selected?{id:selected.id,revision:selected.revision,epoch:editorEpoch.current}:undefined).catch(setError)}
 async function submit(value:Pending){
  const token=generation.current;const epoch=editorEpoch.current;setBusy(true);setError(undefined);setNotice("");
  try{
   keepPendingCommand(key,value);setPending(value);
   const receipt=await investigationQueueAPI.save(value.kind,value.id,value.command,session.csrf_token,life.current.signal);
   if(token!==generation.current)return;clearPendingCommand(key,value);setPending(undefined);setNotice("个人筛选操作已确认。");
   await loadSaved({id:receipt.id,epoch,confirmed:true}).catch(e=>{if(token===generation.current&&!isAbortError(e)){setNeedsReload(true);setError(e);setNotice("个人筛选操作已确认；列表读取失败，可重读保存列表。")}});
  }catch(e){
   if(token!==generation.current||isAbortError(e))return;
   if(e instanceof ApiRequestError&&[400,401,403,404,409].includes(e.status)){clearPendingCommand(key,value);setPending(undefined);if([401,403].includes(e.status)){setSaved([]);setSelected(undefined);setRemote(undefined)};if([404,409].includes(e.status)){setNeedsReload(value.kind!=="create");setNotice("筛选已有变化，请重读保存列表；当前草稿已保留。")}}else{setNotice("操作结果待确认。请原样重试，避免重复保存。");}
   setError(e);
  }finally{if(token===generation.current)setBusy(false)}
 }
 function mutate(kind:Pending["kind"]){const command:QueueFilterCommand={operation_id:crypto.randomUUID(),expected_revision:kind==="create"?0:selected?.revision??0,schema_version:"xinsight.investigation-queue-filter.v1",name:kind==="delete"?selected?.name??"":name,filters:kind==="delete"?selected?.filters??draft:draft};void submit({kind,id:kind==="create"?"":selected?.id??"",command})}
 const blocked=busy||!!pending||recoveryInvalid;
 return <div className="page-stack investigation-queue">
  <header className="page-header"><div><p className="eyebrow">Investigation</p><h1>调查事项</h1><p>区分当前待办、本期新建与后来变化的依据。结案和封存不代表风险已经消除。</p></div><button className="button" onClick={()=>void load(active)} disabled={loading}>刷新当前数据</button></header>
  <p className="queue-coverage">范围：当前可见的调查记录。安全来源覆盖尚未确认；这里的数量不代表全部安全事件。</p>
  <div className="queue-modes" aria-label="队列范围">{["current","created","reassessment"].map(mode=><button key={mode} className={active.mode===mode?"button button-primary":"button"} aria-pressed={active.mode===mode} onClick={()=>apply({...active,mode})}>{label(mode)} <strong>{page?.statistics[mode as "current"|"created"|"reassessment"]??"—"}</strong></button>)}</div>
  <details className="queue-options"><summary>筛选与个人视图</summary>
  <form className="panel queue-filters" onSubmit={search} aria-label="调查事项筛选">
   <label>新建期间<select value={draft.period} onChange={e=>editDraft({...draft,period:e.target.value,from:undefined,to:undefined})}><option value="24h">近24小时</option><option value="7d">近7天</option><option value="custom">明确时间范围</option></select></label>
   {draft.period==="custom"?<><label>起始时间（含时区）<input value={draft.from??""} placeholder="2026-09-01T00:00:00+08:00" onChange={e=>editDraft({...draft,from:e.target.value})}/></label><label>结束时间（不含）<input value={draft.to??""} placeholder="2026-09-10T00:00:00+08:00" onChange={e=>editDraft({...draft,to:e.target.value})}/></label></>:null}
   <label>负责人<select value={draft.assignee??""} onChange={e=>editDraft({...draft,assignee:e.target.value})}><option value="">全部负责人</option><option value="me">本人</option><option value="unassigned">未分配</option><option value="inactive">历史失效账号</option>{draft.assignee&&!['me','unassigned','inactive'].includes(draft.assignee)?<option value={draft.assignee}>{draft.assignee}</option>:null}</select></label>
   <label>跟进状态<select value={draft.progress??""} onChange={e=>editDraft({...draft,progress:e.target.value})}><option value="">全部跟进状态</option>{["unrecorded","not_started","in_progress","ended"].map(v=><option key={v} value={v}>{label(v)}</option>)}</select></label>
   <label>复核结论<select value={draft.conclusion??""} onChange={e=>editDraft({...draft,conclusion:e.target.value})}><option value="">全部复核结论</option>{["unrecorded","pending_review","supported","not_supported","insufficient_evidence"].map(v=><option key={v} value={v}>{label(v)}</option>)}</select></label>
   <label>依据状态<select value={draft.basis??""} onChange={e=>editDraft({...draft,basis:e.target.value})}><option value="">全部依据状态</option>{["unrecorded","unchanged","changed","unavailable"].map(v=><option key={v} value={v}>{label(v)}</option>)}</select></label>
   <label>Case状态<select value={draft.case_status??""} onChange={e=>editDraft({...draft,case_status:e.target.value})}><option value="">全部Cases</option><option value="open">开放</option><option value="sealed">封存</option></select></label>
   <label>排序<select value={draft.sort} onChange={e=>editDraft({...draft,sort:e.target.value})}><option value="created_desc">新建时间从近到远</option><option value="due_asc">期限从近到远</option></select></label>
   <label>搜索事项<input value={draft.search??""} maxLength={200} onChange={e=>editDraft({...draft,search:e.target.value})} placeholder="标题或判断内容"/></label><button className="button button-primary" disabled={loading}>应用筛选</button>
  </form>
  {canSave?<section className="panel queue-saved" aria-label="个人筛选"><label>已保存筛选<select value={selected?.id??""} disabled={blocked} onChange={e=>choose(saved.find(v=>v.id===e.target.value))}><option value="">新的个人筛选</option>{saved.map(item=><option key={item.id} value={item.id}>{item.name}</option>)}</select></label><label>筛选名称<input maxLength={120} value={name} disabled={blocked} onChange={e=>{editorEpoch.current++;setName(e.target.value)}}/></label><button className="button" disabled={blocked||needsReload||!!remote||!name.trim()} onClick={()=>mutate(selected?"update":"create")}>{selected?"更新个人筛选":"保存个人筛选"}</button>{selected?<button className="button" disabled={blocked||needsReload||!!remote} onClick={()=>mutate("delete")}>删除个人筛选</button>:null}<button className="button" disabled={blocked} onClick={reread}>重读保存列表</button></section>:null}
  </details>
  {remote?<section className="panel" aria-label="保存版本冲突"><h2>保存版本已有变化</h2><p>当前保存版本 {remote.revision}：{remote.name}。你的名称和筛选草稿仍保留。</p><dl>{Object.entries(remote.filters).filter(([,value])=>value!==undefined&&value!=="").map(([field,value])=><div key={field}><dt>{({mode:"队列范围",period:"新建期间",sort:"排序",from:"起始时间",to:"结束时间",assignee:"负责人",progress:"跟进状态",conclusion:"复核结论",basis:"依据状态",case_status:"Case状态",search:"搜索事项"} as Record<string,string>)[field]??field}</dt><dd>{label(String(value))}</dd></div>)}</dl><p>载入已保存版本会替换草稿；保留草稿则采用新的保存版本，下一次保存将写入当前草稿。</p><button className="button" disabled={blocked} onClick={()=>reconcile(false)}>载入已保存版本</button><button className="button" disabled={blocked} onClick={()=>reconcile(true)}>保留草稿继续修改</button></section>:null}
  {pending?<div className="panel"><p>有一项筛选操作结果待确认，原请求已保留。</p><button className="button" disabled={busy||!canSave} onClick={()=>void submit(pending)}>原样重试待确认操作</button></div>:null}
  {recoveryInvalid?<p role="alert">待确认操作记录损坏，已保留原内容。修复前不能覆盖或提交新的筛选操作。</p>:null}
  {notice?<p role="status">{notice}</p>:null}{error?<ErrorState error={error}/>:null}{loading?<LoadingBlock label="正在读取完整范围与分页"/>:null}
  {page?<><div className="queue-statistics"><span>未分配 <strong>{page.statistics.unassigned}</strong></span><span>逾期 <strong>{page.statistics.overdue}</strong></span><span>依据未记录 <strong>{page.statistics.basis_unrecorded}</strong></span><span>依据变化 <strong>{page.statistics.basis_changed}</strong></span><span>依据不可用 <strong>{page.statistics.basis_unavailable}</strong></span></div><p>统计单位：事项；这些指标可能重叠。计算于 {formatDateTime(page.evaluated_at)}。当前未结束集合不受新建期间限制；费用阅读期间不应用于当前待办。</p>
  <section className="panel"><h2>{label(active.mode)} · {page.total} 项</h2><div className="queue-table-scroll"><table className="queue-table"><thead><tr><th>事项与 Case</th><th>复核 / 跟进</th><th>依据</th><th>负责人 / 期限</th><th>新建时间</th><th>操作</th></tr></thead><tbody>{items.map(row=><tr key={row.finding.id}><td><strong>{row.finding.title}</strong><small>{row.case_title} · {label(row.case_status)}</small><small>原分类：{row.finding.classification} · 原状态：{row.finding.status}</small></td><td>{label(row.conclusion)}<small>{label(row.progress)}</small></td><td>{label(row.basis)}<ResourceUpdates updates={row.resource_updates} /><small>复核：{label(row.review_basis)} · 跟进：{label(row.progress_basis)}</small></td><td>{row.followup.assignee_subject_id?<button className="queue-person" onClick={()=>apply({...active,assignee:row.followup.assignee_subject_id??""})}>{row.followup.assignee_name}</button>:"未分配"}<small>{label(row.assignee_state)}</small><small>{row.followup.due_at?formatDateTime(row.followup.due_at):"未设期限"} {row.overdue?"· 已逾期":""}</small>{row.followup.updated_at?<small>责任更新 {formatDateTime(row.followup.updated_at)}</small>:null}</td><td>{formatDateTime(row.finding.created_at)}</td><td><button className="button" onClick={()=>navigate(`/cases/${encodeURIComponent(row.finding.case_id)}?queue_return=${encodeURIComponent("/investigation/queue?"+queueFilterQuery(active))}&finding=${encodeURIComponent(row.finding.id)}`)}>查看与跟进</button></td></tr>)}</tbody></table></div>{items.length===0?<p>这个集合暂无事项；这不表示来源覆盖完整或不存在风险。</p>:null}<p>已显示 {items.length} / {page.total} 项</p>{page.next_cursor?<button className="button" disabled={loading} onClick={()=>void load(active,page.next_cursor)}>继续读取下一页</button>:null}</section></>:null}
 </div>;
}
