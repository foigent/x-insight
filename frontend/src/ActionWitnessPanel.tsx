import { localizedLabels, LocalizedError, t, tx, useLocale } from "./i18n";
import { scheduleBodyExpiry } from "./bodyExpiry";
import { useEffect, useRef, useState } from "react";
import { ApiRequestError, isAbortError, runtimeAPI, type AccessSessionResponse } from "./api";
import { fetchContentPage, fetchVerifiedContent, type VerifiedContent } from "./content";
import { ContentPanel } from "./ContentPanel";
import { actionLinkedText, boundedFileDiff, verifiedActionFileText } from "./actionDiff";
import type { ActionContentLink, ActionWitnesses, Reading, Request } from "./runtime";

type Item = ActionWitnesses["items"][number];
type Difference = {removed:string[];added:string[];omitted:boolean};
const toolStates:Record<string,string>=localizedLabels({}, {completed: "m0037", failed: "m0038", unknown: "m0039"});
function checkSummary(item:Item) {
  if(item.record.withdrawn)return t("m0040");
  if(item.changed_since_reading||item.chain_state!=="complete")return t("m0041");
  const check=item.record.acceptance;
  if(check.state==="failed")return t("m0042");
  if(check.state!=="confirmed")return t("m0043");
  return t("m0047", { value0: check.goal_state==="passed"?t("m0044"):check.goal_state==="failed"?t("m0045"):t("m0046") });
}

function readButtonLabel(item:Item, items:Item[]):string {
  const name=item.record.object.name;
  const named=items.filter(x=>x.record.object.name===name);
  if(named.length<2)return t("m0048", { value0: name });
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
    if(ordinal>0)return t("m0049", { value0: ordinal, value1: name });
  }
  const occurred=Number.isFinite(Date.parse(item.record.occurred_at))?item.record.occurred_at:t("m0050");
  return t("m0051", { value0: name, value1: occurred, value2: item.record.run_id, value3: item.record.record_ref });
}

export function ActionWitnessPanel({reading,request,session,onInvalidated}:{reading:Reading;request:Request;session:AccessSessionResponse;onInvalidated:(e:unknown)=>void}) {
  useLocale();
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
      if(page.next_cursor)throw new LocalizedError(() => t("m0052"));
      if(page.items.some(x=>x.record.schema_version!=="xinsight.resource-action.v1"||x.record.final_request_id!==first.items[0]?.record.final_request_id||x.final_request_ref!==final||x.record.target.source_system_id!==request.source_system_id||x.record.target.source_instance!==request.source_instance||x.record.target.org_id!==request.org_id||x.record.target.project_id!==request.project_id))throw new ApiRequestError(409);
      if(!control.signal.aborted)setItems(page.items);
    })().catch(e=>{if(!control.signal.aborted&&!isAbortError(e)){setError(e);if(e instanceof ApiRequestError&&([401,409].includes(e.status)||(e.status===410&&e.problem?.code!=="content_expired")))onInvalidated(e);}}).finally(()=>{if(!control.signal.aborted)setBusy(false)});
    const clear=()=>{control.abort();detailControl.current?.abort();setItems([]);setDiff(undefined);setDiffState(undefined);setDetailText(undefined);setProviderRequest(null);setBodyExpiresAt(null);setDiffBusy(false)};
    window.addEventListener("xinsight:scope-changed",clear);window.addEventListener("xinsight:unauthorized",clear);
    return()=>{clear();window.removeEventListener("xinsight:scope-changed",clear);window.removeEventListener("xinsight:unauthorized",clear)};
  },[reading.reading_id,reading.membership_ref,request.request_ref,request.source_system_id,request.source_instance,request.org_id,request.project_id,onInvalidated]);

  useEffect(()=>{
    if(bodyExpiresAt===null)return;
    const clear=()=>{detailControl.current?.abort();setDetailText(undefined);setDiff(undefined);setDiffBusy(false);setBodyExpiresAt(null);setDiffState(t("m0053"));};
    return scheduleBodyExpiry(bodyExpiresAt,clear);
  },[bodyExpiresAt]);
  function contentDeadline(value:VerifiedContent) {
    const bodyDeadline=Date.parse(value.readableUntil??value.reference.expires_at??"");
    const readingDeadline=reading.expires_at?Date.parse(reading.expires_at):Infinity;
    const deadline=Math.min(bodyDeadline,readingDeadline);
    if(!Number.isFinite(deadline)||deadline<=Date.now())throw new LocalizedError(() => t("m0054"));
    return deadline;
  }

  function resolveRequest(item:Item,link:ActionContentLink) {
    const requestRef=item.resolved_content_links.find(x=>x.purpose===link.purpose)?.request_ref;
    if(!requestRef)throw new LocalizedError(() => t("m0055"));
    return {request_ref:requestRef,source_system_id:link.source_system_id,source_instance:link.source_instance,org_id:link.org_id,project_id:link.project_id,request_id:link.request_id};
  }
  async function loadText(item:Item,link:ActionContentLink,hash:string,signal:AbortSignal){
    const req=resolveRequest(item,link);
    const page=await fetchContentPage(reading,req,session,signal);
    const member=page.items.find(x=>x.reference.source_fact_id===link.source_fact_id&&x.reference.revision===link.revision&&x.reference.content_ref===link.content_ref&&x.reference.source_sha256===link.source_sha256);
    if(!member||member.availability.state!=="available")throw new LocalizedError(() => t("m0056"));
    const content=await fetchVerifiedContent(reading,req,member.reference,session,signal);
    return {verified:await verifiedActionFileText(content,link,hash),deadline:contentDeadline(content)};
  }
  async function loadLinked(item:Item,link:ActionContentLink,signal:AbortSignal){
    const req=resolveRequest(item,link),page=await fetchContentPage(reading,req,session,signal);
    const member=page.items.find(x=>x.reference.source_fact_id===link.source_fact_id&&x.reference.revision===link.revision&&x.reference.content_ref===link.content_ref&&x.reference.source_sha256===link.source_sha256);
    if(!member||member.availability.state!=="available")throw new LocalizedError(() => t("m0057"));
    return fetchVerifiedContent(reading,req,member.reference,session,signal);
  }
  async function showInput(item:Item){
    const link=item.record.content_links.find(x=>x.purpose==="result");
    if(!link){setDetailText(t("m0058"));return}
    detailControl.current?.abort();const control=new AbortController();detailControl.current=control;setDetailText(undefined);setProviderRequest(null);setBodyExpiresAt(null);setDiff(undefined);setDiffState(undefined);setDiffBusy(true);
    try{const content=await loadLinked(item,link,control.signal);if(control.signal.aborted)return;
      const deadline=contentDeadline(content);
      const value=actionLinkedText(content,link);
      setBodyExpiresAt(deadline);
      setDetailText(value===undefined?t("m0059"):value.length>256*1024?t("m0060"):t("m0061", { value0: value }));
    }catch(e){if(!control.signal.aborted&&!isAbortError(e)){setDetailText(t("m0062"));if(e instanceof ApiRequestError&&([401,409].includes(e.status)||(e.status===410&&e.problem?.code!=="content_expired")))onInvalidated(e)}}
    finally{if(detailControl.current===control&&!control.signal.aborted)setDiffBusy(false)}
  }
  function showProvider(item:Item, final=false){
    const requestRef=final?item.final_request_ref:item.trigger_request_ref;
    const requestID=final?item.record.final_request_id:item.record.request_id;
    detailControl.current?.abort();setDetailText(undefined);setDiff(undefined);setDiffState(undefined);setBodyExpiresAt(null);setDiffBusy(false);setProviderRequest(null);
    if(!requestRef||!requestID){setDetailText(t("m0063"));return;}
    setProviderRequest({request_ref:requestRef,source_system_id:item.record.target.source_system_id,source_instance:item.record.target.source_instance,org_id:item.record.target.org_id,project_id:item.record.target.project_id,request_id:requestID});
  }
  async function inspectChange(item:Item){
    if(item.changed_since_reading||item.chain_state!=="complete"){setDiff(undefined);setDiffState(t("m0064"));return}
    detailControl.current?.abort();
    setDiff(undefined);setDiffState(undefined);setDetailText(undefined);setProviderRequest(null);setBodyExpiresAt(null);setDiffBusy(true);
    const control=new AbortController();detailControl.current=control;
    try{
      const before=item.record.content_links.find(x=>x.purpose==="before"),after=item.record.content_links.find(x=>x.purpose==="after");
      if(!before||!after||!item.record.acceptance.before_sha256||!item.record.acceptance.after_sha256){setDiffState(t("m0065"));return}
      const a=await loadText(item,before,item.record.acceptance.before_sha256,control.signal);
      if(control.signal.aborted)return;
      const b=await loadText(item,after,item.record.acceptance.after_sha256,control.signal);
      if(control.signal.aborted)return;
      const deadline=Math.min(a.deadline,b.deadline);
      if(deadline<=Date.now())throw new LocalizedError(() => t("m0066"));
      setBodyExpiresAt(deadline);
      if(a.verified.state!=="verified"||b.verified.state!=="verified"||a.verified.text===undefined||b.verified.text===undefined){setDiffState(t("m0067", { value0: a.verified.state, value1: b.verified.state }));return}
      setDiff(boundedFileDiff(a.verified.text,b.verified.text));setDiffState(t("m0068"));
    }catch(e){if(!control.signal.aborted&&!isAbortError(e)){setDiffState(t("m0069"));if(e instanceof ApiRequestError&&([401,409].includes(e.status)||(e.status===410&&e.problem?.code!=="content_expired")))onInvalidated(e)}}
    finally{if(detailControl.current===control&&!control.signal.aborted)setDiffBusy(false)}
  }
  const reads=items.filter(x=>x.record.action_kind==="read"&&!x.record.withdrawn&&!x.changed_since_reading&&x.chain_state==="complete");
  const edits=items.filter(x=>x.record.action_kind==="edit"&&!x.record.withdrawn);
  const stableEdits=edits.filter(x=>!x.changed_since_reading&&x.chain_state==="complete");
  return <section className="runtime-panel" aria-label={t("m0070")}><h3>{tx("m0070")}</h3>
    <p>{tx("m0071")}</p>
    {busy?<p role="status">{tx("m0072")}</p>:null}
    {error?<p role="alert">{tx("m0073")}</p>:null}
    {!busy&&!error&&!items.length?<p>{tx("m0074")}</p>:null}
    {items.length?<><p>{tx("m0076", { value0: items.length, value1: edits.length?edits.map(x=>`${x.record.object.name}：${checkSummary(x)}`).join("；"):t("m0075") })}</p>
      <div className="runtime-table-scroll"><table aria-label={t("m0077")}><thead><tr><th scope="col">{tx("m0078")}</th><th scope="col">{tx("m0079")}</th><th scope="col">{tx("m0080")}</th><th scope="col">{tx("m0081")}</th></tr></thead><tbody>{items.map(x=><tr key={x.record_hash}><td>{x.record.object.name}</td><td>{x.record.action_kind==="edit"?t("m0082"):t("m0083")}</td><td>{toolStates[x.record.tool_result.state]??t("m0039")}</td><td>{checkSummary(x)}</td></tr>)}</tbody></table></div>
      {reads.map(x=><button key={`read-${x.record_hash}`} type="button" disabled={diffBusy} onClick={()=>void showInput(x)}>{readButtonLabel(x,reads)}</button>)}
      <details><summary>{tx("m0084")}</summary><p>{tx("m0085")}</p>
        {Array.from(new Map(items.map(x=>[x.trigger_request_ref,x])).values()).map(x=><button key={`provider-${x.trigger_request_ref}`} type="button" disabled={diffBusy} onClick={()=>showProvider(x)}>{tx("m0086", { value0: x.record.request_id })}</button>)}
        {items.some(x=>x.final_request_ref&&x.record.final_request_id)?<button type="button" disabled={diffBusy} onClick={()=>showProvider(items.find(x=>x.final_request_ref&&x.record.final_request_id)!,true)}>{tx("m0087")}</button>:null}
      </details>
      {providerRequest?<section aria-label={t("m0088")}><p>{tx("m0089", { value0: providerRequest.request_id })}</p><ContentPanel key={providerRequest.request_ref} reading={reading} request={providerRequest} session={session} observationPoint="provider_request" onInvalidated={onInvalidated}/></section>:null}
      {stableEdits.map(x=><button key={`edit-${x.record_hash}`} type="button" disabled={diffBusy} onClick={()=>void inspectChange(x)}>{diffBusy?t("m0090"):t("m0091", { value0: x.record.object.name })}</button>)}
      {detailText?<pre style={{whiteSpace:"pre-wrap",overflowWrap:"anywhere",maxHeight:"26rem",overflow:"auto"}}>{detailText}</pre>:null}

      {diffState?<p role="status">{diffState}</p>:null}
      {diff?<div aria-label={t("m0092")}><pre style={{whiteSpace:"pre-wrap",overflowWrap:"anywhere"}}>{diff.removed.map(x=>`− ${x}`).concat(diff.added.map(x=>`+ ${x}`)).join("\n")}</pre>{diff.omitted?<p>{tx("m0093")}</p>:null}</div>:null}
      <details><summary>{tx("m0094")}</summary><p>{tx("m0095")}</p><ol>{items.map(x=><li key={x.record_hash}>{x.record.action_kind==="edit"?t("m0082"):t("m0083")} {x.record.object.name}：{toolStates[x.record.tool_result.state]??t("m0039")}；{checkSummary(x)}。{x.changed_since_reading?t("m0096"):""}<details><summary>{tx("m0097")}</summary><code>{x.record.run_id} / {x.record.tool_call_id}</code><p>{tx("m0099", { value0: x.trigger_request_ref, value1: x.final_request_ref||t("m0098") })}</p><p>{tx("m0101", { value0: x.record.relationship?.receipt_ref??t("m0100"), value1: x.record.relationship?.receipt_sha256??t("m0100") })}</p><p>{tx("m0103", { value0: x.record.native_evidence.call_sha256, value1: x.record.native_evidence.result_sha256??t("m0102") })}</p><p>{tx("m0104", { value0: x.record_hash, value1: x.chain_state })}</p></details></li>)}</ol></details>
    </>:null}
  </section>;
}
