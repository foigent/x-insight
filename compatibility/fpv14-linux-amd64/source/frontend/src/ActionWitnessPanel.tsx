import { scheduleBodyExpiry } from "./bodyExpiry";
import { useEffect, useRef, useState } from "react";
import { ApiRequestError, isAbortError, runtimeAPI, type AccessSessionResponse } from "./api";
import { fetchContentPage, fetchVerifiedContent, type VerifiedContent } from "./content";
import { ContentPanel } from "./ContentPanel";
import { actionLinkedText, boundedFileDiff, verifiedActionFileText } from "./actionDiff";
import type { ActionContentLink, ActionWitnesses, Reading, Request } from "./runtime";

type Item = ActionWitnesses["items"][number];
type Difference = {removed:string[];added:string[];omitted:boolean};
const toolStates:Record<string,string>={completed:"运行时报告完成",failed:"运行时报告失败",unknown:"运行时结果未确认"};
function checkSummary(item:Item) {
  if(item.record.withdrawn)return "这条记录已撤回";
  if(item.changed_since_reading||item.chain_state!=="complete")return "记录关系待复核";
  const check=item.record.acceptance;
  if(check.state==="failed")return "文件结果检查未通过";
  if(check.state!=="confirmed")return "文件结果尚未独立检查";
  return `测试读回确认内容变化；目标检查${check.goal_state==="passed"?"通过":check.goal_state==="failed"?"未通过":"未执行"}`;
}

function readButtonLabel(item:Item, items:Item[]):string {
  const name=item.record.object.name;
  const named=items.filter(x=>x.record.object.name===name);
  if(named.length<2)return `查看读取 ${name}`;
  const native=named[0]?.record.native_evidence;
  if(native&&named.every(x=>{
    if(x.record.run_id!==item.record.run_id||x.record.object.ref!==item.record.object.ref)return false;
    const evidence=x.record.native_evidence;
    return evidence.trace_id===native.trace_id&&evidence.session_id===native.session_id&&
      evidence.manifest_sha256===native.manifest_sha256&&evidence.events_sha256===native.events_sha256&&
      Number.isSafeInteger(evidence.call_seq)&&evidence.call_seq>0;
  })&&new Set(named.map(x=>x.record.native_evidence.call_seq)).size===named.length){
    const order=[...named].sort((a,b)=>a.record.native_evidence.call_seq-b.record.native_evidence.call_seq);
    const ordinal=order.indexOf(item)+1;
    if(ordinal>0)return `查看第 ${ordinal} 次读取 ${name}`;
  }
  const occurred=Number.isFinite(Date.parse(item.record.occurred_at))?item.record.occurred_at:"时间未确认";
  return `查看读取 ${name}（${occurred}；运行 ${item.record.run_id}；记录 ${item.record.record_ref}）`;
}

export function ActionWitnessPanel({reading,request,session,onInvalidated}:{reading:Reading;request:Request;session:AccessSessionResponse;onInvalidated:(e:unknown)=>void}) {
  const [items,setItems]=useState<Item[]>([]),[busy,setBusy]=useState(true),[error,setError]=useState<unknown>();
  const [diff,setDiff]=useState<Difference>(),[diffState,setDiffState]=useState<string>(),[diffBusy,setDiffBusy]=useState(false);
  const [detailText,setDetailText]=useState<string>();
  const [providerRequest,setProviderRequest]=useState<Parameters<typeof fetchContentPage>[1]|null>(null);
  const [bodyExpiresAt,setBodyExpiresAt]=useState<number|null>(null);
  const detailControl=useRef<AbortController|null>(null);
  useEffect(()=>{
    const control=new AbortController();detailControl.current?.abort();setItems([]);setBusy(true);setError(undefined);setDiff(undefined);setDiffState(undefined);setDetailText(undefined);setProviderRequest(null);setBodyExpiresAt(null);
    void (async()=>{
      const first=await runtimeAPI.actionWitnesses(reading.reading_id,request.request_ref,undefined,control.signal);
      if(first.action_witnesses_version!=="xinsight.runtime.action-witnesses.v1"||first.reading_id!==reading.reading_id||first.request_ref!==request.request_ref||first.membership_ref!==reading.membership_ref||first.coverage!=="test_development_action_witnesses_only")throw new ApiRequestError(409);
      let page=first;
      const final=first.items[0]?.final_request_ref;
      if(final&&final!==request.request_ref) page=await runtimeAPI.actionWitnesses(reading.reading_id,final,undefined,control.signal);
      if(page.action_witnesses_version!=="xinsight.runtime.action-witnesses.v1"||page.reading_id!==reading.reading_id||page.request_ref!==(final||request.request_ref)||page.membership_ref!==reading.membership_ref||page.coverage!=="test_development_action_witnesses_only")throw new ApiRequestError(409);
      if(page.next_cursor)throw new Error("动作清单超过单页；当前摘要无法完整展示");
      if(page.items.some(x=>x.record.schema_version!=="xinsight.resource-action.v1"||x.record.final_request_id!==first.items[0]?.record.final_request_id||x.final_request_ref!==final||x.record.target.source_system_id!==request.source_system_id||x.record.target.source_instance!==request.source_instance||x.record.target.org_id!==request.org_id||x.record.target.project_id!==request.project_id))throw new ApiRequestError(409);
      if(!control.signal.aborted)setItems(page.items);
    })().catch(e=>{if(!control.signal.aborted&&!isAbortError(e)){setError(e);if(e instanceof ApiRequestError&&([401,409].includes(e.status)||(e.status===410&&e.problem?.code!=="content_expired")))onInvalidated(e);}}).finally(()=>{if(!control.signal.aborted)setBusy(false)});
    const clear=()=>{control.abort();detailControl.current?.abort();setItems([]);setDiff(undefined);setDiffState(undefined);setDetailText(undefined);setProviderRequest(null);setBodyExpiresAt(null);setDiffBusy(false)};
    window.addEventListener("xinsight:scope-changed",clear);window.addEventListener("xinsight:unauthorized",clear);
    return()=>{clear();window.removeEventListener("xinsight:scope-changed",clear);window.removeEventListener("xinsight:unauthorized",clear)};
  },[reading.reading_id,reading.membership_ref,request.request_ref,request.source_system_id,request.source_instance,request.org_id,request.project_id,onInvalidated]);

  useEffect(()=>{
    if(bodyExpiresAt===null)return;
    const clear=()=>{detailControl.current?.abort();setDetailText(undefined);setDiff(undefined);setDiffBusy(false);setBodyExpiresAt(null);setDiffState("正文已到期；动作摘要与历史复核仍保留。");};
    return scheduleBodyExpiry(bodyExpiresAt,clear);
  },[bodyExpiresAt]);
  function contentDeadline(value:VerifiedContent) {
    const bodyDeadline=Date.parse(value.readableUntil??value.reference.expires_at??"");
    const readingDeadline=reading.expires_at?Date.parse(reading.expires_at):Infinity;
    const deadline=Math.min(bodyDeadline,readingDeadline);
    if(!Number.isFinite(deadline)||deadline<=Date.now())throw new Error("正文已到期，无法继续展示");
    return deadline;
  }

  function resolveRequest(item:Item,link:ActionContentLink) {
    const requestRef=item.resolved_content_links.find(x=>x.purpose===link.purpose)?.request_ref;
    if(!requestRef)throw new Error("准确正文引用不在当前阅读中");
    return {request_ref:requestRef,source_system_id:link.source_system_id,source_instance:link.source_instance,org_id:link.org_id,project_id:link.project_id,request_id:link.request_id};
  }
  async function loadText(item:Item,link:ActionContentLink,hash:string,signal:AbortSignal){
    const req=resolveRequest(item,link);
    const page=await fetchContentPage(reading,req,session,signal);
    const member=page.items.find(x=>x.reference.source_fact_id===link.source_fact_id&&x.reference.revision===link.revision&&x.reference.content_ref===link.content_ref&&x.reference.source_sha256===link.source_sha256);
    if(!member||member.availability.state!=="available")throw new Error("原文引用缺失、过期或未授权");
    const content=await fetchVerifiedContent(reading,req,member.reference,session,signal);
    return {verified:await verifiedActionFileText(content,link,hash),deadline:contentDeadline(content)};
  }
  async function loadLinked(item:Item,link:ActionContentLink,signal:AbortSignal){
    const req=resolveRequest(item,link),page=await fetchContentPage(reading,req,session,signal);
    const member=page.items.find(x=>x.reference.source_fact_id===link.source_fact_id&&x.reference.revision===link.revision&&x.reference.content_ref===link.content_ref&&x.reference.source_sha256===link.source_sha256);
    if(!member||member.availability.state!=="available")throw new Error("准确正文当前不可读");
    return fetchVerifiedContent(reading,req,member.reference,session,signal);
  }
  async function showInput(item:Item){
    const link=item.record.content_links.find(x=>x.purpose==="result");
    if(!link){setDetailText("读取资料的准确正文引用未采集。");return}
    detailControl.current?.abort();const control=new AbortController();detailControl.current=control;setDetailText(undefined);setProviderRequest(null);setBodyExpiresAt(null);setDiff(undefined);setDiffState(undefined);setDiffBusy(true);
    try{const content=await loadLinked(item,link,control.signal);if(control.signal.aborted)return;
      const deadline=contentDeadline(content);
      const value=actionLinkedText(content,link);
      setBodyExpiresAt(deadline);
      setDetailText(value===undefined?"读取结果不是可核对的完整文本。":value.length>256*1024?"读取结果超过本页完整展示上限，请在交互内容中按准确引用查看。":`已核验 C 保存表示中的读取结果（运行时报告，未独立核对原文件）：\n${value}`);
    }catch(e){if(!control.signal.aborted&&!isAbortError(e)){setDetailText("读取资料当前不可按准确引用查看；动作摘要仍可阅读。");if(e instanceof ApiRequestError&&([401,409].includes(e.status)||(e.status===410&&e.problem?.code!=="content_expired")))onInvalidated(e)}}
    finally{if(detailControl.current===control&&!control.signal.aborted)setDiffBusy(false)}
  }
  function showProvider(item:Item, final=false){
    const requestRef=final?item.final_request_ref:item.trigger_request_ref;
    const requestID=final?item.record.final_request_id:item.record.request_id;
    detailControl.current?.abort();setDetailText(undefined);setDiff(undefined);setDiffState(undefined);setBodyExpiresAt(null);setDiffBusy(false);setProviderRequest(null);
    if(!requestRef||!requestID){setDetailText("请求关系未确认，无法定位提交内容。");return;}
    setProviderRequest({request_ref:requestRef,source_system_id:item.record.target.source_system_id,source_instance:item.record.target.source_instance,org_id:item.record.target.org_id,project_id:item.record.target.project_id,request_id:requestID});
  }
  async function inspectChange(item:Item){
    if(item.changed_since_reading||item.chain_state!=="complete"){setDiff(undefined);setDiffState("资源修订或链不完整；需重新阅读后核对差异。");return}
    detailControl.current?.abort();
    setDiff(undefined);setDiffState(undefined);setDetailText(undefined);setProviderRequest(null);setBodyExpiresAt(null);setDiffBusy(true);
    const control=new AbortController();detailControl.current=control;
    try{
      const before=item.record.content_links.find(x=>x.purpose==="before"),after=item.record.content_links.find(x=>x.purpose==="after");
      if(!before||!after||!item.record.acceptance.before_sha256||!item.record.acceptance.after_sha256){setDiffState("前后正文引用未采集，不能展示完整差异。");return}
      const a=await loadText(item,before,item.record.acceptance.before_sha256,control.signal);
      if(control.signal.aborted)return;
      const b=await loadText(item,after,item.record.acceptance.after_sha256,control.signal);
      if(control.signal.aborted)return;
      const deadline=Math.min(a.deadline,b.deadline);
      if(deadline<=Date.now())throw new Error("正文已到期，无法展示文件差异");
      setBodyExpiresAt(deadline);
      if(a.verified.state!=="verified"||b.verified.state!=="verified"||a.verified.text===undefined||b.verified.text===undefined){setDiffState(`前后内容不能作为完整文件差异：改前 ${a.verified.state}，改后 ${b.verified.state}。`);return}
      setDiff(boundedFileDiff(a.verified.text,b.verified.text));setDiffState("改前和改后正文均与测试检查器全文件摘要相符；下面是本页临时计算的差异。");
    }catch(e){if(!control.signal.aborted&&!isAbortError(e)){setDiffState("前后正文当前不可核对；需有内容读取权限、完整表示及准确引用。动作摘要仍可阅读。");if(e instanceof ApiRequestError&&([401,409].includes(e.status)||(e.status===410&&e.problem?.code!=="content_expired")))onInvalidated(e)}}
    finally{if(detailControl.current===control&&!control.signal.aborted)setDiffBusy(false)}
  }
  const reads=items.filter(x=>x.record.action_kind==="read"&&!x.record.withdrawn&&!x.changed_since_reading&&x.chain_state==="complete");
  const edits=items.filter(x=>x.record.action_kind==="edit"&&!x.record.withdrawn);
  const stableEdits=edits.filter(x=>!x.changed_since_reading&&x.chain_state==="complete");
  return <section className="runtime-panel" aria-label="这次工作"><h3>这次工作</h3>
    <p>查看读取的资料、提交给模型的内容、文件修改和检查结果。此处是测试或开发环境的运行记录。</p>
    {busy?<p role="status">正在读取本次工作动作…</p>:null}
    {error?<p role="alert">动作证据暂不可读，不能据此判断没有发生操作。</p>:null}
    {!busy&&!error&&!items.length?<p>当前阅读没有与此请求准确关联的动作见证；文件结果未知。</p>:null}
    {items.length?<><p>{items.length} 条 OpenClaw 运行时动作报告。{edits.length?edits.map(x=>`${x.record.object.name}：${checkSummary(x)}`).join("；"):"当前未见文件修改检查。"}</p>
      <div className="runtime-table-scroll"><table aria-label="读取与修改结果"><thead><tr><th scope="col">资料</th><th scope="col">操作</th><th scope="col">工具报告</th><th scope="col">文件检查</th></tr></thead><tbody>{items.map(x=><tr key={x.record_hash}><td>{x.record.object.name}</td><td>{x.record.action_kind==="edit"?"修改":"读取"}</td><td>{toolStates[x.record.tool_result.state]??"运行时结果未确认"}</td><td>{checkSummary(x)}</td></tr>)}</tbody></table></div>
      {reads.map(x=><button key={`read-${x.record_hash}`} type="button" disabled={diffBusy} onClick={()=>void showInput(x)}>{readButtonLabel(x,reads)}</button>)}
      <details><summary>查看已关联的模型请求</summary><p>逐次查看网关提交的内容；这不证明模型内部使用了每一字节。</p>
        {Array.from(new Map(items.map(x=>[x.trigger_request_ref,x])).values()).map(x=><button key={`provider-${x.trigger_request_ref}`} type="button" disabled={diffBusy} onClick={()=>showProvider(x)}>查看调用 {x.record.request_id} 的提交内容</button>)}
        {items.some(x=>x.final_request_ref&&x.record.final_request_id)?<button type="button" disabled={diffBusy} onClick={()=>showProvider(items.find(x=>x.final_request_ref&&x.record.final_request_id)!,true)}>查看最后一次调用的提交内容</button>:null}
      </details>
      {providerRequest?<section aria-label="本次调用的模型输入"><p>调用：{providerRequest.request_id}。有多次提供方尝试时，请分别选择对应内容。</p><ContentPanel key={providerRequest.request_ref} reading={reading} request={providerRequest} session={session} observationPoint="provider_request" onInvalidated={onInvalidated}/></section>:null}
      {stableEdits.map(x=><button key={`edit-${x.record_hash}`} type="button" disabled={diffBusy} onClick={()=>void inspectChange(x)}>{diffBusy?"正在核对前后原文…":`查看 ${x.record.object.name} 前后差异`}</button>)}
      {detailText?<pre style={{whiteSpace:"pre-wrap",overflowWrap:"anywhere",maxHeight:"26rem",overflow:"auto"}}>{detailText}</pre>:null}

      {diffState?<p role="status">{diffState}</p>:null}
      {diff?<div aria-label="已核对文件差异"><pre style={{whiteSpace:"pre-wrap",overflowWrap:"anywhere"}}>{diff.removed.map(x=>`− ${x}`).concat(diff.added.map(x=>`+ ${x}`)).join("\n")}</pre>{diff.omitted?<p>差异超出本页展示上限，仅显示前 100 行删除和新增。</p>:null}</div>:null}
      <details><summary>相关请求与依据</summary><p>“运行时报告”来自 OpenClaw 原生事件；“测试读回”来自受控文件检查器，均不等于独立资源系统审计。提供方请求表示只能证明网关提交内容，不能证明模型内部实际使用。</p><ol>{items.map(x=><li key={x.record_hash}>{x.record.action_kind==="edit"?"修改":"读取"} {x.record.object.name}：{toolStates[x.record.tool_result.state]??"运行时结果未确认"}；{checkSummary(x)}。{x.changed_since_reading?"已有更新修订；需重新阅读。":""}<details><summary>来源引用</summary><code>{x.record.run_id} / {x.record.tool_call_id}</code><p>触发请求 {x.trigger_request_ref}；最终请求 {x.final_request_ref||"未确认"}</p><p>关系收据 {x.record.relationship?.receipt_ref??"未提供"} · {x.record.relationship?.receipt_sha256??"未提供"}</p><p>原生事件 {x.record.native_evidence.call_sha256} · {x.record.native_evidence.result_sha256??"未记录结果"}</p><p>记录 {x.record_hash} · 修订链 {x.chain_state}</p></details></li>)}</ol></details>
    </>:null}
  </section>;
}
