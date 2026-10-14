import { ApiRequestError, currentScopeSessionKey, scopeSessionKey, type AccessSessionResponse } from "./api";
import type { Reading, Request } from "./runtime";
type ContentRequestIdentity = Pick<Request,"request_ref"|"source_system_id"|"source_instance"|"org_id"|"project_id"|"request_id">;

export const MAX_TEXT_PREVIEW_BYTES = 256 * 1024;
export const MAX_CONTENT_BYTES = 32 * 1024 * 1024;
export interface ContentRepresentation {
  media_type: string; encoding: "identity"; byte_domain: "stored_representation_before_http_content_encoding";
  saved_bytes: number; digest: string; redaction_ref?: { id: string; version: string };
}
export interface FrozenContentReference {
  source_system_id: string; source_instance: string; org_id: string; project_id: string; request_id: string;
  source_fact_id: string; revision: string; source_sha256: string; capture_status: "complete" | "partial" | "pending" | "not_captured";
  content_ref?: string; representation?: ContentRepresentation; expires_at?: string;
}
export type ContentLocation = {kind: "byte_range"; offset: number; length: number} | {kind: "json_pointer"; pointer: string};
export interface ContentChoice { scope: "finding" | "case"; reference: FrozenContentReference; location: ContentLocation; relation: "supports" | "contradicts" | "context" }
export interface ContentLink { case_id: string; finding_id?: string; reference: ContentChoice; added_by_subject: string; added_at: string }
export interface ContentObservation {
  protocol: string; observation_point: "client_request" | "provider_request" | "provider_response" | "client_response";
  attempt_id?: string; content_item_id: string; occurred_at: string; capture_reason: string;
}
export interface ContentItem { observation?: ContentObservation; reference: FrozenContentReference; availability: { state: string; evaluated_at: string; retryable: boolean }; newer_revision: boolean }
export interface ContentPage {
  version: "xinsight.runtime-content.v1"; reading_id: string; request_ref: string; membership_ref: string;
  content_interpretation_revision: "xinsight.content-membership.v1"; member_basis: "content_manifest" | "exact_execution_association";
  items: ContentItem[]; next_cursor?: string;
}
interface ReadMetadata extends Omit<FrozenContentReference, "source_system_id" | "capture_status"> { schema_version: "unirouter.content-read-metadata.v1"; representation: ContentRepresentation; content_ref: string; expires_at: string }
export interface VerifiedContent { observation?: ContentObservation; reference: FrozenContentReference; bytes: Uint8Array; text: string | null; previewOnly: boolean }

export function contentKey(c: ContentChoice) { return canonical([c.scope, c.reference, c.location, c.relation]); }
const failure = () => new ApiRequestError(503, {type:"urn:xinsight:error:content_integrity_failed",title:"Content could not be verified",status:503,code:"content_integrity_failed",correlation_id:"",retryable:false});
const canonical = (value: unknown): string => JSON.stringify(sortValue(value));
function sortValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(sortValue);
  if (value !== null && typeof value === "object") return Object.fromEntries(Object.entries(value).sort(([a],[b]) => a < b ? -1 : a > b ? 1 : 0).map(([k,v])=>[k,sortValue(v)]));
  return value;
}
const exactKeys = (v: Record<string, unknown>, required: string[], optional: string[] = []) => required.every(k=>Object.hasOwn(v,k)) && Object.keys(v).every(k=>required.includes(k)||optional.includes(k));
function object(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === "object" && !Array.isArray(value); }

export function decodeContentMetadata(header: string | null, ref: FrozenContentReference): ReadMetadata {
  if (!header || header.length > 65536 || !/^[A-Za-z0-9_-]+$/.test(header) || !ref.representation || !ref.content_ref) throw failure();
  let raw: string, value: unknown;
  try { raw = new TextDecoder("utf-8", {fatal:true}).decode(Uint8Array.from(atob(header.replace(/-/g,"+").replace(/_/g,"/")), c=>c.charCodeAt(0))); value=JSON.parse(raw); } catch { throw failure(); }
  const keys=["schema_version","source_instance","org_id","project_id","request_id","source_fact_id","revision","source_sha256","content_ref","representation","expires_at"];
  if (!object(value)||!exactKeys(value,keys)||canonical(value)!==raw||value.schema_version!=="unirouter.content-read-metadata.v1") throw failure();
  for (const k of keys.filter(k=>k!=="schema_version"&&k!=="representation")) if (value[k]!==ref[k as keyof FrozenContentReference]) throw failure();
  const r=value.representation;
  if (!object(r)||!exactKeys(r,["media_type","encoding","byte_domain","saved_bytes","digest"],["redaction_ref"]) || r.encoding!=="identity" || r.byte_domain!=="stored_representation_before_http_content_encoding" || !Number.isSafeInteger(r.saved_bytes) || (r.saved_bytes as number)<0 || (r.saved_bytes as number)>MAX_CONTENT_BYTES || typeof r.digest!=="string" || !/^sha256:[a-f0-9]{64}$/.test(r.digest) || canonical(r)!==canonical(ref.representation)) throw failure();
  if (r.redaction_ref!==undefined && (!object(r.redaction_ref)||!exactKeys(r.redaction_ref,["id","version"]))) throw failure();
  return value as unknown as ReadMetadata;
}

function assertScope(session: AccessSessionResponse, signal: AbortSignal) {
  if (signal.aborted || currentScopeSessionKey()!==scopeSessionKey(session)) throw new DOMException("Reading cancelled", "AbortError");
  if (!session.scope_revision) throw new ApiRequestError(409);
}
function checkResponseScope(response: Response, session: AccessSessionResponse, signal: AbortSignal) {
  assertScope(session,signal);
  const revision=response.headers.get("X-XInsight-Scope-Revision");
  if ((response.ok||revision!==null)&&revision!==session.scope_revision) {
    window.dispatchEvent(new Event("xinsight:scope-changed")); throw new ApiRequestError(409);
  }
  if (response.status===401) window.dispatchEvent(new Event("xinsight:unauthorized"));
}
function basePath(reading: Reading, request: ContentRequestIdentity) { return `/api/v1/runtime/readings/${encodeURIComponent(reading.reading_id)}/requests/${encodeURIComponent(request.request_ref)}/contents`; }

async function boundedBytes(response: Response, maximum: number, signal: AbortSignal, verifyCurrent: ()=>void, exact?: number): Promise<Uint8Array> {
  if (!response.body) { if (exact===0) return new Uint8Array(); throw failure(); }
  const reader=response.body.getReader();
  // One bounded allocation: no unbounded chunk list or whole-body JSON wrapper.
  const output=new Uint8Array(exact??maximum); let length=0;
  const cancel=()=>{void reader.cancel().catch(()=>undefined);};signal.addEventListener("abort",cancel,{once:true});
  try {
    while (true) {
      verifyCurrent(); const chunk=await reader.read(); verifyCurrent();
      if (chunk.done) break;
      if (length+chunk.value.byteLength>output.byteLength) throw failure();
      output.set(chunk.value,length);length+=chunk.value.byteLength;
    }
    if (exact!==undefined&&length!==exact) throw failure();
    return exact===undefined?output.slice(0,length):output;
  } catch(e) { cancel(); throw e; } finally {signal.removeEventListener("abort",cancel);reader.releaseLock();}
}

async function rejectResponse(response: Response, session: AccessSessionResponse, signal: AbortSignal): Promise<never> {
  let problem: unknown;
  try { problem=JSON.parse(new TextDecoder().decode(await boundedBytes(response,65536,signal,()=>assertScope(session,signal)))); } catch { throw new ApiRequestError(response.status); }
  if (object(problem)&&typeof problem.code==="string") {
    if (response.status===409||problem.code==="scope_revision_changed"||problem.code==="workbench_access_changed") window.dispatchEvent(new Event("xinsight:scope-changed"));
    throw new ApiRequestError(response.status,problem as never);
  }
  throw new ApiRequestError(response.status);
}

// Missing is a legacy response; present-but-invalid is never treated as legacy.
export function decodeContentObservation(value: unknown, ref: FrozenContentReference): ContentObservation {
  const text = (v: unknown): v is string => typeof v === "string" && [...v].length > 0 && [...v].length <= 128 && !/[\u0000-\u001f\u007f]/.test(v);
  if (!object(value) || !exactKeys(value,["protocol","observation_point","content_item_id","occurred_at","capture_reason"],["attempt_id"]) || !text(value.protocol) || !/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(value.protocol) || !text(value.content_item_id) || typeof value.observation_point !== "string" || !["client_request","provider_request","provider_response","client_response"].includes(value.observation_point)) throw failure();
  const provider = value.observation_point.startsWith("provider_");
  if ((provider || Object.hasOwn(value,"attempt_id")) && !text(value.attempt_id)) throw failure();
  const at = value.occurred_at;
  if (typeof at !== "string" || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{6}Z$/.test(at) || !Number.isFinite(Date.parse(at)) || new Date(at).toISOString() !== at.slice(0,23)+"Z") throw failure();
  const reasons: Record<FrozenContentReference["capture_status"], readonly string[]> = {
    complete:["none"], pending:["capture_in_progress"],
    partial:["limit_exceeded","queue_full","storage_write_failed","stream_interrupted","request_cancelled","output_held"],
    not_captured:["policy_metadata_only","policy_disabled","scope_excluded","unsupported_protocol","limit_exceeded","queue_full","storage_write_failed","not_applicable","capture_unavailable"],
  };
  if (typeof value.capture_reason !== "string" || !reasons[ref.capture_status]?.includes(value.capture_reason)) throw failure();
  return value as unknown as ContentObservation;
}

export async function fetchContentPage(reading: Reading, request: ContentRequestIdentity, session: AccessSessionResponse, signal: AbortSignal, cursor?: string): Promise<ContentPage> {
  assertScope(session,signal);const query=new URLSearchParams({limit:"50"});if(cursor)query.set("cursor",cursor);
  const response=await fetch(`${basePath(reading,request)}?${query}`,{signal,credentials:"same-origin",cache:"no-store",redirect:"error",headers:{Accept:"application/json","X-XInsight-Scope-Revision":session.scope_revision!}});
  checkResponseScope(response,session,signal);if(!response.ok)return rejectResponse(response,session,signal);
  const value=JSON.parse(new TextDecoder("utf-8",{fatal:true}).decode(await boundedBytes(response,2*1024*1024,signal,()=>assertScope(session,signal)))) as ContentPage;
  if(value.version!=="xinsight.runtime-content.v1"||value.content_interpretation_revision!=="xinsight.content-membership.v1"||value.reading_id!==reading.reading_id||value.request_ref!==request.request_ref||value.membership_ref!==reading.membership_ref||!Array.isArray(value.items)||value.items.length>50)throw failure();
  for(const item of value.items) {
    const r=item.reference;
    if(!r||typeof r.source_fact_id!=="string"||typeof r.revision!=="string"||!/^([1-9][0-9]*)$/.test(r.revision)||typeof r.source_sha256!=="string"||!/^sha256:[a-f0-9]{64}$/.test(r.source_sha256)||!["complete","partial","pending","not_captured"].includes(r.capture_status)||["source_system_id","source_instance","org_id","project_id","request_id"].some(k=>r[k as keyof FrozenContentReference]!==request[k as keyof ContentRequestIdentity])||!item.availability||typeof item.availability.state!=="string")throw failure();
    if (Object.hasOwn(item,"observation")) item.observation=decodeContentObservation(item.observation,r);
    const saved=["complete","partial"].includes(r.capture_status);
    if(saved?(!r.content_ref||!r.representation||!r.expires_at||!Number.isSafeInteger(r.representation.saved_bytes)||r.representation.saved_bytes<0):(r.content_ref!==undefined||r.representation!==undefined||r.expires_at!==undefined))throw failure();
  }
  assertScope(session,signal);return value;
}

let activeRead: AbortController | undefined;
// Keep ownership through the non-cancellable digest. A competing read must wait
// for completion; it cannot allocate another maximum-size body.
export async function fetchVerifiedContent(reading: Reading, request: ContentRequestIdentity, reference: FrozenContentReference, session: AccessSessionResponse, signal: AbortSignal): Promise<VerifiedContent> {
  if(activeRead)throw new ApiRequestError(429);
  const control=new AbortController();activeRead=control;
  const cancel=()=>control.abort();signal.addEventListener("abort",cancel,{once:true});if(signal.aborted)cancel();
  const onScopeChange=()=>control.abort();window.addEventListener("xinsight:scope-changed",onScopeChange);window.addEventListener("xinsight:unauthorized",onScopeChange);
  try {
    assertScope(session,control.signal);
    if(!reference.content_ref||!reference.representation||!["complete","partial"].includes(reference.capture_status)||reference.representation.saved_bytes>MAX_CONTENT_BYTES)throw failure();
    const query=new URLSearchParams({source_fact_id:reference.source_fact_id,revision:reference.revision});
    const response=await fetch(`${basePath(reading,request)}/${encodeURIComponent(reference.content_ref)}?${query}`,{signal:control.signal,credentials:"same-origin",cache:"no-store",redirect:"error",headers:{Accept:"application/octet-stream","X-XInsight-Scope-Revision":session.scope_revision!}});
    checkResponseScope(response,session,control.signal);if(response.status!==200)return await rejectResponse(response,session,control.signal);
    const metadata=decodeContentMetadata(response.headers.get("X-UR-Content-Metadata"),reference);
    if(response.headers.get("Content-Length")!==String(metadata.representation.saved_bytes)||![null,"identity"].includes(response.headers.get("Content-Encoding"))||response.headers.get("Content-Type")?.split(";")[0]!=="application/octet-stream")throw failure();
    const bytes=await boundedBytes(response,MAX_CONTENT_BYTES,control.signal,()=>assertScope(session,control.signal),metadata.representation.saved_bytes);
    const digest=await crypto.subtle.digest("SHA-256",bytes as Uint8Array<ArrayBuffer>);
    assertScope(session,control.signal);
    if("sha256:"+[...new Uint8Array(digest)].map(v=>v.toString(16).padStart(2,"0")).join("")!==metadata.representation.digest)throw failure();
    let text:string|null=null;
    if(/^(text\/|application\/(?:json|[^;]+\+json)$)/.test(metadata.representation.media_type)) { try {text=new TextDecoder("utf-8",{fatal:true}).decode(bytes.subarray(0,MAX_TEXT_PREVIEW_BYTES),{stream:bytes.byteLength>MAX_TEXT_PREVIEW_BYTES});}catch{/* Preserve binary/invalid UTF-8 as verified bytes. */} }
    return {reference,bytes,text,previewOnly:bytes.byteLength>MAX_TEXT_PREVIEW_BYTES};
  } finally {control.abort();signal.removeEventListener("abort",cancel);window.removeEventListener("xinsight:scope-changed",onScopeChange);window.removeEventListener("xinsight:unauthorized",onScopeChange);if(activeRead===control)activeRead=undefined;}
}
