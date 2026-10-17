import { scheduleBodyExpiry } from "./bodyExpiry";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ApiRequestError, isAbortError, scopeSessionKey, type AccessSessionResponse } from "./api";
import { fetchContentPage, fetchVerifiedContent, MAX_CONTENT_BYTES, MAX_TEXT_PREVIEW_BYTES, type ContentItem, type ContentPage, type ContentLocation, type ContentObservation, type VerifiedContent } from "./content";
import type { Reading } from "./runtime";
import { projectContent } from "./contentProjection";
import type { ExecutionContext } from "./assessmentTypes";
import { ReviewStart } from "./ReviewStart";

const points:Record<string,string>={client_request:"客户输入（改写前）",provider_request:"提供方请求",provider_response:"提供方响应",client_response:"客户输出"};
const reasons:Record<string,string>={capture_in_progress:"正在采集",policy_metadata_only:"当前策略仅保存元数据",policy_disabled:"内容采集已关闭",scope_excluded:"不在内容采集范围内",unsupported_protocol:"来源不支持采集此协议",limit_exceeded:"达到保存容量限制",queue_full:"采集队列已满",storage_write_failed:"正文保存失败",stream_interrupted:"内容流中断",request_cancelled:"请求已取消",output_held:"输出被暂扣",not_applicable:"此处不适用内容采集",capture_unavailable:"内容采集暂不可用"};
const states:Record<string,string>={available:"可尝试读取",pending:"正在采集",not_captured:"未采集正文",expired:"正文已到期",deleted:"正文已删除",missing:"保存正文缺失",corrupt:"完整性异常",source_unavailable:"来源暂不可用"};
function message(error:unknown) {
  if(error instanceof ApiRequestError) {
    if(error.problem?.code==="content_read_limit")return "内容超过当前读取或解析上限，本次无法验证，不会自动重试。";
    if(error.problem?.code==="content_read_busy")return "另一次内容读取正在完成，请稍后手动重试。";
    if(error.status===401)return "请重新登录。";
    if(error.status===403)return "当前没有内容读取权限。";
    if(error.status===409)return "内容范围已变化，请重新读取。";
    if(error.status===429)return "另一次正文读取尚在完成或取消中，请稍后重试。";
    if(error.status===410)return "阅读或正文已到期；历史判断与引用仍保留。";
    if(error.problem?.code==="content_integrity_failed")return "内容长度、来源或摘要核验失败，本次没有显示正文。";
  }
  return "内容暂不可读，请重试。";
}

export function ContentPanel({session,reading,request,onInvalidated,navigate,executionContext,observationPoint}:{observationPoint?:ContentObservation["observation_point"];executionContext?:ExecutionContext;session:AccessSessionResponse;reading:Reading;request:Parameters<typeof fetchContentPage>[1];onInvalidated:(error:unknown)=>void;navigate?:(path:string)=>void}) {
  const identity=JSON.stringify([scopeSessionKey(session),reading.reading_id,request.request_ref,observationPoint]),owner=useRef(identity);owner.current=identity;
  const [displayOwner,setDisplayOwner]=useState(identity);
  const [page,setPage]=useState<ContentPage|null>(null),[items,setItems]=useState<ContentItem[]>([]),[loaded,setLoaded]=useState<VerifiedContent|null>(null);
  const [busy,setBusy]=useState(false),[readingBody,setReadingBody]=useState(false),[error,setError]=useState<unknown>(null);
  const [selectedLocation,setSelectedLocation]=useState<ContentLocation>();
  const projection=useMemo(()=>loaded?projectContent(loaded,loaded.observation):null,[loaded]);
  const [offset,setOffset]=useState(0),[length,setLength]=useState(0);
  const pageControl=useRef<AbortController|null>(null),bodyControl=useRef<AbortController|null>(null),generation=useRef(0);
  const fail=useCallback((e:unknown)=>{setError(e);if(e instanceof ApiRequestError&&[401,403,409].includes(e.status)){setItems([]);setPage(null);setLoaded(null);bodyControl.current?.abort();}if(e instanceof ApiRequestError&&([401,409].includes(e.status)||(e.status===410&&e.problem?.code!=="content_expired")))onInvalidated(e);},[onInvalidated]);
  const load=useCallback((cursor?:string)=>{
    const token=generation.current;pageControl.current?.abort();const control=new AbortController();pageControl.current=control;setBusy(true);setError(null);
    if(!cursor){setItems([]);setPage(null);}
    void fetchContentPage(reading,request,session,control.signal,cursor).then(next=>{
      if(control.signal.aborted||token!==generation.current||owner.current!==identity)return;
      setPage(next);setItems(previous=>cursor?[...previous,...next.items]:next.items);
    }).catch(e=>{if(!control.signal.aborted&&token===generation.current&&!isAbortError(e))fail(e);}).finally(()=>{if(!control.signal.aborted&&token===generation.current)setBusy(false);});
  },[reading,request,session,identity,fail]);
  useEffect(()=>{
    generation.current++;setDisplayOwner(identity);setLoaded(null);setReadingBody(false);load();
    const revoke=()=>{generation.current++;pageControl.current?.abort();bodyControl.current?.abort();setItems([]);setPage(null);setLoaded(null);setReadingBody(false);onInvalidated(new ApiRequestError(409));};
    window.addEventListener("xinsight:scope-changed",revoke);window.addEventListener("xinsight:unauthorized",revoke);
    return()=>{generation.current++;pageControl.current?.abort();bodyControl.current?.abort();window.removeEventListener("xinsight:scope-changed",revoke);window.removeEventListener("xinsight:unauthorized",revoke);};
  },[load,onInvalidated]);
  useEffect(()=>{
    const expiry=loaded?.readableUntil??loaded?.reference.expires_at;
    if(!expiry)return;
    return scheduleBodyExpiry(Date.parse(expiry),()=>setLoaded(null));
  },[loaded]);
  async function read(item:ContentItem) {
    bodyControl.current?.abort();const control=new AbortController();bodyControl.current=control;const token=generation.current;
    setLoaded(null);setError(null);setReadingBody(true);
    try {const value=await fetchVerifiedContent(reading,request,item.reference,session,control.signal);
      if(control.signal.aborted||token!==generation.current||owner.current!==identity)return;
      setOffset(0);setLength(value.bytes.byteLength);setSelectedLocation(undefined);setLoaded({...value,observation:item.observation});
    }catch(e){if(!control.signal.aborted&&token===generation.current&&!isAbortError(e))fail(e);}
    finally{if(bodyControl.current===control&&token===generation.current)setReadingBody(false);}
  }
  if(displayOwner!==identity)return <p role="status">正在确认新的内容读取范围…</p>;
  const validRange=loaded&&Number.isSafeInteger(offset)&&Number.isSafeInteger(length)&&offset>=0&&length>=0&&offset<=loaded.bytes.byteLength&&length<=loaded.bytes.byteLength-offset;
  const adoption=selectedLocation??{kind:"byte_range" as const,offset,length};
  const visibleItems=observationPoint?items.filter(item=>item.observation?.observation_point===observationPoint):items;
  return <section aria-label="交互内容"><h3>交互内容</h3>
    <p>本次阅读冻结的内容引用。后续修订需要重新读取；内容到期不会改写原复核判断。单次可查看最多 {MAX_CONTENT_BYTES/1024/1024} MiB。</p>
    {error?<p role="alert">{message(error)}</p>:null}
    {busy?<p role="status">正在读取内容清单…</p>:null}
    {!busy&&!error&&page&&visibleItems.length===0?<p>{observationPoint?`本页没有${points[observationPoint]}的内容引用。`:page.member_basis==="exact_execution_association"?"本请求存在准确执行关联，本次阅读尚无内容清单。":"本次阅读没有内容清单。"}</p>:null}
    {visibleItems.map(item=>{const r=item.reference;return <article className="source-fact-card" key={JSON.stringify([r.source_system_id,r.source_fact_id,r.revision])}>
      <strong>清单记录：{r.capture_status==="complete"?"完整保存":r.capture_status==="partial"?"部分保存":r.capture_status==="pending"?"采集中":"未保存正文"} · {states[item.availability.state]??item.availability.state}</strong>
      <p>是否可读以本次按需读取和核验结果为准。</p>
      <p>修订 {r.revision}{r.representation?` · ${r.representation.media_type} · ${r.representation.saved_bytes} 字节`:""}</p>
      {item.observation?<><p>{points[item.observation.observation_point]}</p>{item.observation.capture_reason!=="none"?<p>{reasons[item.observation.capture_reason]??"采集原因未识别，请查看引用详情。"}</p>:null}</>:<p>来源未提供观察身份；暂时只显示原文。</p>}
      {item.newer_revision?<p>存在更新修订；当前仍显示本次冻结版本。</p>:null}
      <details><summary>引用与摘要</summary>{item.observation?<><p>协议 {item.observation.protocol} · 观察点 {item.observation.observation_point}</p><p>{item.observation.attempt_id?`来源声明的尝试身份 ${item.observation.attempt_id}`:"此客户观察记录未提供尝试身份；不据此推定提供方尝试缺失。"}</p><p>内容项 {item.observation.content_item_id}</p><p>原始观察时间 {item.observation.occurred_at} · 采集原因码 {item.observation.capture_reason}</p></>:null}<p>{r.source_system_id} / {r.source_instance} / {r.org_id} / {r.project_id} / {r.request_id}</p><code>{r.source_fact_id}</code><p>清单摘要 {r.source_sha256}</p>{r.representation?<p>保存字节摘要 {r.representation.digest}</p>:null}</details>
      {r.representation&&r.content_ref?<button type="button" disabled={readingBody||item.availability.state!=="available"||r.representation.saved_bytes>MAX_CONTENT_BYTES} onClick={()=>void read(item)}>读取并核验正文</button>:null}
    </article>})}
    {readingBody?<p role="status">正在完整读取并核验正文… <button type="button" onClick={()=>bodyControl.current?.abort()}>取消读取</button></p>:null}
    {loaded?<article aria-label="已核验保存表示"><h4>已核验保存表示 · {loaded.bytes.byteLength} 字节</h4>
      {loaded.reference.capture_status==="partial"?<p>来源只保存了部分内容，不能据此推定完整回复或工具成功。</p>:null}
      {loaded.previewOnly?<p>完整保存字节已核验。下方仅预览前 {MAX_TEXT_PREVIEW_BYTES/1024} KiB，不是完整正文；采用范围按完整保存字节计。</p>:null}
      {projection?<section aria-label="结构化内容"><h4>正文中的消息与工具声明</h4><p>仅解释当前保存表示；结果声明不证明工具执行或实际资源访问，不跨正文或请求关联。</p>
        {projection.notes.map((note,i)=><p key={i} role="note">{note}</p>)}
        {projection.rows.map((row,i)=><article key={i} aria-label={`内容项 ${i+1}`}><strong>{i+1}. {row.label}</strong>
          <pre style={{whiteSpace:"pre-wrap",overflowWrap:"anywhere"}}>{row.text.slice(0,4096)}</pre>{row.text.length>4096?<p>此项仅预览前 4096 个字符；采用位置仍指向完整原节点或事件。</p>:null}
          {row.notice?<p>{row.notice}</p>:null}<details><summary>原文引用</summary>{row.callID?<p>声明 ID：{row.callID}</p>:null}<p>{row.location.kind==="json_pointer"?`原 JSON 位置 ${row.location.pointer||"（根节点）"}`:`原事件字节 ${row.location.offset} 起，长度 ${row.location.length}`}</p></details>
          {navigate?<button type="button" onClick={()=>setSelectedLocation(row.location)}>{`采用第 ${i+1} 项原文`}</button>:null}
        </article>)}
      </section>:null}
      {loaded.text!==null?<pre style={{whiteSpace:"pre-wrap",overflowWrap:"anywhere",maxHeight:"32rem",overflow:"auto"}}>{loaded.text||"（空正文：0 字节）"}</pre>:<p>二进制表示已完成长度与摘要核验，未将其解释或执行为文本。</p>}
      {navigate?<><label>采用起始字节<input type="number" min={0} max={loaded.bytes.byteLength} value={offset} onChange={e=>{setSelectedLocation(undefined);setOffset(Number(e.target.value));}}/></label><label>采用字节长度<input type="number" min={0} max={loaded.bytes.byteLength-offset} value={length} onChange={e=>{setSelectedLocation(undefined);setLength(Number(e.target.value));}}/></label>
      {selectedLocation?<details><summary>已选择的原文引用</summary><p>原文位置：{selectedLocation.kind==="json_pointer"?selectedLocation.pointer||"（根节点）":`字节 ${selectedLocation.offset} 起，长度 ${selectedLocation.length}`}</p></details>:null}
      {(selectedLocation||validRange)?<ReviewStart key={JSON.stringify([loaded.reference,adoption,executionContext??null])} executionContext={executionContext} session={session} navigate={navigate} available={{evidence:[],gaps:[],contents:[{scope:"finding",reference:loaded.reference,location:adoption,relation:"context"}]}}/>:<p role="alert">采用范围超出保存表示。</p>}</>:null}
      <button type="button" onClick={()=>setLoaded(null)}>清除正文显示</button>
    </article>:null}
    {page?.next_cursor?<button disabled={busy} onClick={()=>load(page.next_cursor)}>加载更多内容引用</button>:null}
    {error&&!busy?<button onClick={()=>load()}>重试内容清单</button>:null}
  </section>;
}
