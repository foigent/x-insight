import { localizedLabels, t, tx, useLocale } from "./i18n";
import { scheduleBodyExpiry } from "./bodyExpiry";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ApiRequestError, isAbortError, scopeSessionKey, type AccessSessionResponse } from "./api";
import { fetchContentPage, fetchVerifiedContent, MAX_CONTENT_BYTES, MAX_TEXT_PREVIEW_BYTES, type ContentItem, type ContentPage, type ContentLocation, type ContentObservation, type VerifiedContent } from "./content";
import type { Reading } from "./runtime";
import { projectContent } from "./contentProjection";
import type { ExecutionContext } from "./assessmentTypes";
import { ReviewStart } from "./ReviewStart";

const points:Record<string,string>=localizedLabels({}, {client_request: "m0350", provider_request: "m0351", provider_response: "m0352", client_response: "m0353"});
const reasons:Record<string,string>=localizedLabels({}, {capture_in_progress: "m0354", policy_metadata_only: "m0355", policy_disabled: "m0356", scope_excluded: "m0357", unsupported_protocol: "m0358", limit_exceeded: "m0359", queue_full: "m0360", storage_write_failed: "m0361", stream_interrupted: "m0362", request_cancelled: "m0363", output_held: "m0364", not_applicable: "m0365", capture_unavailable: "m0366"});
const states:Record<string,string>=localizedLabels({}, {available: "m0367", pending: "m0354", not_captured: "m0368", expired: "m0369", deleted: "m0370", missing: "m0371", corrupt: "m0372", source_unavailable: "m0373"});
function message(error:unknown) {
  if(error instanceof ApiRequestError) {
    if(error.problem?.code==="content_read_limit")return t("m0374");
    if(error.problem?.code==="content_read_busy")return t("m0375");
    if(error.status===401)return t("m0225");
    if(error.status===403)return t("m0376");
    if(error.status===409)return t("m0377");
    if(error.status===429)return t("m0378");
    if(error.status===410)return t("m0379");
    if(error.problem?.code==="content_integrity_failed")return t("m0380");
  }
  return t("m0381");
}

export function ContentPanel({session,reading,request,onInvalidated,navigate,executionContext,observationPoint}:{observationPoint?:ContentObservation["observation_point"];executionContext?:ExecutionContext;session:AccessSessionResponse;reading:Reading;request:Parameters<typeof fetchContentPage>[1];onInvalidated:(error:unknown)=>void;navigate?:(path:string)=>void}) {
  const locale = useLocale();
  const identity=JSON.stringify([scopeSessionKey(session),reading.reading_id,request.request_ref,observationPoint]),owner=useRef(identity);owner.current=identity;
  const [displayOwner,setDisplayOwner]=useState(identity);
  const [page,setPage]=useState<ContentPage|null>(null),[items,setItems]=useState<ContentItem[]>([]),[loaded,setLoaded]=useState<VerifiedContent|null>(null);
  const [busy,setBusy]=useState(false),[readingBody,setReadingBody]=useState(false),[error,setError]=useState<unknown>(null);
  const [selectedLocation,setSelectedLocation]=useState<ContentLocation>();
  const projection=useMemo(()=>loaded?projectContent(loaded,loaded.observation):null,[loaded,locale]);
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
  if(displayOwner!==identity)return <p role="status">{tx("m0382")}</p>;
  const validRange=loaded&&Number.isSafeInteger(offset)&&Number.isSafeInteger(length)&&offset>=0&&length>=0&&offset<=loaded.bytes.byteLength&&length<=loaded.bytes.byteLength-offset;
  const adoption=selectedLocation??{kind:"byte_range" as const,offset,length};
  const visibleItems=observationPoint?items.filter(item=>item.observation?.observation_point===observationPoint):items;
  return <section aria-label={t("m0383")}><h3>{tx("m0383")}</h3>
    <p>{tx("m0384", { value0: MAX_CONTENT_BYTES/1024/1024 })}</p>
    {error?<p role="alert">{message(error)}</p>:null}
    {busy?<p role="status">{tx("m0385")}</p>:null}
    {!busy&&!error&&page&&visibleItems.length===0?<p>{observationPoint?t("m0386", { value0: points[observationPoint] }):page.member_basis==="exact_execution_association"?t("m0387"):t("m0388")}</p>:null}
    {visibleItems.map(item=>{const r=item.reference;return <article className="source-fact-card" key={JSON.stringify([r.source_system_id,r.source_fact_id,r.revision])}>
      <strong>{tx("m0393", { value0: r.capture_status==="complete"?t("m0389"):r.capture_status==="partial"?t("m0390"):r.capture_status==="pending"?t("m0391"):t("m0392"), value1: states[item.availability.state]??item.availability.state })}</strong>
      <p>{tx("m0394")}</p>
      <p>{tx("m0396", { value0: r.revision, value1: r.representation?t("m0395", { value0: r.representation.media_type, value1: r.representation.saved_bytes }):"" })}</p>
      {item.observation?<><p>{points[item.observation.observation_point]}</p>{item.observation.capture_reason!=="none"?<p>{reasons[item.observation.capture_reason]??t("m0397")}</p>:null}</>:<p>{tx("m0398")}</p>}
      {item.newer_revision?<p>{tx("m0399")}</p>:null}
      <details><summary>{tx("m0400")}</summary>{item.observation?<><p>{tx("m0401", { value0: item.observation.protocol, value1: item.observation.observation_point })}</p><p>{item.observation.attempt_id?t("m0402", { value0: item.observation.attempt_id }):t("m0403")}</p><p>{tx("m0404", { value0: item.observation.content_item_id })}</p><p>{tx("m0405", { value0: item.observation.occurred_at, value1: item.observation.capture_reason })}</p></>:null}<p>{r.source_system_id} / {r.source_instance} / {r.org_id} / {r.project_id} / {r.request_id}</p><code>{r.source_fact_id}</code><p>{tx("m0406", { value0: r.source_sha256 })}</p>{r.representation?<p>{tx("m0407", { value0: r.representation.digest })}</p>:null}</details>
      {r.representation&&r.content_ref?<button type="button" disabled={readingBody||item.availability.state!=="available"||r.representation.saved_bytes>MAX_CONTENT_BYTES} onClick={()=>void read(item)}>{tx("m0408")}</button>:null}
    </article>})}
    {readingBody?<p role="status">{t("m0409")}<button type="button" onClick={()=>bodyControl.current?.abort()}>{tx("m0410")}</button></p>:null}
    {loaded?<article aria-label={t("m0411")}><h4>{tx("m0412", { value0: loaded.bytes.byteLength })}</h4>
      {loaded.reference.capture_status==="partial"?<p>{tx("m0413")}</p>:null}
      {loaded.previewOnly?<p>{tx("m0414", { value0: MAX_TEXT_PREVIEW_BYTES/1024 })}</p>:null}
      {projection?<section aria-label={t("m0415")}><h4>{tx("m0416")}</h4><p>{tx("m0417")}</p>
        {projection.notes.map((note,i)=><p key={i} role="note">{note}</p>)}
        {projection.rows.map((row,i)=><article key={i} aria-label={t("m0404", { value0: i+1 })}><strong>{i+1}. {row.label}</strong>
          <pre style={{whiteSpace:"pre-wrap",overflowWrap:"anywhere"}}>{row.text.slice(0,4096)}</pre>{row.text.length>4096?<p>{tx("m0418")}</p>:null}
          {row.notice?<p>{row.notice}</p>:null}<details><summary>{tx("m0419")}</summary>{row.callID?<p>{tx("m0420", { value0: row.callID })}</p>:null}<p>{row.location.kind==="json_pointer"?t("m0422", { value0: row.location.pointer||t("m0421") }):t("m0423", { value0: row.location.offset, value1: row.location.length })}</p></details>
          {navigate?<button type="button" onClick={()=>setSelectedLocation(row.location)}>{t("m0424", { value0: i+1 })}</button>:null}
        </article>)}
      </section>:null}
      {loaded.text!==null?<pre style={{whiteSpace:"pre-wrap",overflowWrap:"anywhere",maxHeight:"32rem",overflow:"auto"}}>{loaded.text||t("m0425")}</pre>:<p>{tx("m0426")}</p>}
      {navigate?<><label>{t("m0427")}<input type="number" min={0} max={loaded.bytes.byteLength} value={offset} onChange={e=>{setSelectedLocation(undefined);setOffset(Number(e.target.value));}}/></label><label>{t("m0428")}<input type="number" min={0} max={loaded.bytes.byteLength-offset} value={length} onChange={e=>{setSelectedLocation(undefined);setLength(Number(e.target.value));}}/></label>
      {selectedLocation?<details><summary>{tx("m0429")}</summary><p>{tx("m0431", { value0: selectedLocation.kind==="json_pointer"?selectedLocation.pointer||t("m0421"):t("m0430", { value0: selectedLocation.offset, value1: selectedLocation.length }) })}</p></details>:null}
      {(selectedLocation||validRange)?<ReviewStart key={JSON.stringify([loaded.reference,adoption,executionContext??null])} executionContext={executionContext} session={session} navigate={navigate} available={{evidence:[],gaps:[],contents:[{scope:"finding",reference:loaded.reference,location:adoption,relation:"context"}]}}/>:<p role="alert">{tx("m0432")}</p>}</>:null}
      <button type="button" onClick={()=>setLoaded(null)}>{tx("m0433")}</button>
    </article>:null}
    {page?.next_cursor?<button disabled={busy} onClick={()=>load(page.next_cursor)}>{tx("m0434")}</button>:null}
    {error&&!busy?<button onClick={()=>load()}>{tx("m0435")}</button>:null}
  </section>;
}
