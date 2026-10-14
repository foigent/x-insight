import { createHash, webcrypto } from "node:crypto";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { acceptScopeSession, type AccessSessionResponse } from "./api";
import { decodeContentMetadata, decodeContentObservation, fetchContentPage, fetchVerifiedContent, MAX_CONTENT_BYTES, MAX_TEXT_PREVIEW_BYTES, type FrozenContentReference } from "./content";
import type { Reading, Request } from "./runtime";
const session={session:{subject_id:"reader",session_ref:"one",role:"workbench_reader",permissions:["workbench.details.read"]},scope_revision:"scope-one"} as AccessSessionResponse;
const reading={reading_id:"reading",membership_ref:"membership"} as Reading;
const request={request_ref:"request",source_system_id:"source",source_instance:"instance",org_id:"org",project_id:"project",request_id:"request-one"} as Request;
function fixture(bytes:Uint8Array,media="text/plain") {
 const ref:FrozenContentReference={source_system_id:request.source_system_id,source_instance:request.source_instance,org_id:request.org_id,project_id:request.project_id,request_id:request.request_id,source_fact_id:"uf1_"+"a".repeat(64),revision:"1",source_sha256:"sha256:"+"b".repeat(64),capture_status:"complete",content_ref:"uc1_"+"c".repeat(64),representation:{media_type:media,encoding:"identity",byte_domain:"stored_representation_before_http_content_encoding",saved_bytes:bytes.length,digest:"sha256:"+createHash("sha256").update(bytes).digest("hex")},expires_at:"2099-09-16T00:00:00.000000Z"};
 const {source_system_id:_,capture_status:__,...metadata}=ref;
 const canonical=(value:unknown):unknown=>Array.isArray(value)?value.map(canonical):value&&typeof value==="object"?Object.fromEntries(Object.entries(value).sort().map(([k,v])=>[k,canonical(v)])):value;
 const raw=JSON.stringify(canonical({...metadata,schema_version:"unirouter.content-read-metadata.v1"}));
 const headers={"X-XInsight-Scope-Revision":session.scope_revision!,"X-UR-Content-Metadata":Buffer.from(raw).toString("base64url"),"Content-Length":String(bytes.length),"Content-Type":"application/octet-stream"};
 return {ref,headers,raw};
}
const encode=(s:string)=>new TextEncoder().encode(s);
beforeEach(()=>{acceptScopeSession(session);vi.stubGlobal("crypto",webcrypto);});
afterEach(()=>{acceptScopeSession(null);vi.unstubAllGlobals();});
it.each(["",'<script>alert("unsafe")</script><a href="https://external.invalid">link</a>'])("verifies full saved bytes without interpreting the body: %s",async body=>{
 const bytes=encode(body),{ref,headers}=fixture(bytes,"text/html");
 const fetcher=vi.fn(async()=>new Response(bytes,{headers}));vi.stubGlobal("fetch",fetcher);
 const result=await fetchVerifiedContent(reading,request,ref,session,new AbortController().signal);
 expect(result.text).toBe(body);expect(result.bytes.byteLength).toBe(bytes.length);expect(result.previewOnly).toBe(false);
 expect(fetcher).toHaveBeenCalledWith(expect.stringContaining("source_fact_id="),expect.objectContaining({cache:"no-store",redirect:"error",credentials:"same-origin"}));
});
it("rejects noncanonical, duplicate and mismatched metadata before reading",()=>{
 const {ref,headers,raw}=fixture(encode("hello"));
 expect(decodeContentMetadata(headers["X-UR-Content-Metadata"],ref).representation.saved_bytes).toBe(5);
 for(const text of [raw+" ",raw.replace('{','{"revision":"1",'),raw.replace('"revision":"1"','"revision":"2"')])expect(()=>decodeContentMetadata(Buffer.from(text).toString("base64url"),ref)).toThrow();
});

it("preserves the expired UR reference and accepts XI's independent read deadline",async()=>{
 const bytes=encode("independent-custody"),f=fixture(bytes);
 f.ref.expires_at="2020-09-16T00:00:00.000000Z";
 const {source_system_id:_,capture_status:__,...metadata}=f.ref;
 const canonical=(v:unknown):unknown=>Array.isArray(v)?v.map(canonical):v&&typeof v==="object"?Object.fromEntries(Object.entries(v).sort().map(([k,v])=>[k,canonical(v)])):v;
 const raw=JSON.stringify(canonical({...metadata,schema_version:"unirouter.content-read-metadata.v1"}));
 const headers={...f.headers,"X-UR-Content-Metadata":Buffer.from(raw).toString("base64url"),"X-XInsight-Content-Expires-At":"2099-09-17T00:00:00.000000Z"};
 vi.stubGlobal("fetch",vi.fn(async()=>new Response(bytes,{headers})));
 const result=await fetchVerifiedContent(reading,request,f.ref,session,new AbortController().signal);
 expect(result.reference).toBe(f.ref);expect(result.reference.expires_at).toBe("2020-09-16T00:00:00.000000Z");
 expect(result.readableUntil).toBe(headers["X-XInsight-Content-Expires-At"]);expect(result.text).toBe("independent-custody");
});

it.each(["", "not-a-time", "2099-02-30T00:00:00.000000Z", "2099-09-17T00:00:00Z"])("rejects present malformed XI expiry projection: %s",async expiry=>{
 const bytes=encode("body"),{ref,headers}=fixture(bytes);
 vi.stubGlobal("fetch",vi.fn(async()=>new Response(bytes,{headers:{...headers,"X-XInsight-Content-Expires-At":expiry}})));
 await expect(fetchVerifiedContent(reading,request,ref,session,new AbortController().signal)).rejects.toMatchObject({problem:{code:"content_integrity_failed"}});
});
it.each(["short","long","hash"])("rejects %s bytes without exposing partial text",async kind=>{
 const {ref,headers}=fixture(encode("hello"));
 vi.stubGlobal("fetch",vi.fn(async()=>new Response(encode(kind==="short"?"hell":kind==="long"?"helloo":"jello"),{headers})));
 await expect(fetchVerifiedContent(reading,request,ref,session,new AbortController().signal)).rejects.toMatchObject({problem:{code:"content_integrity_failed"}});
});
it("keeps a single owner through noncancellable digest and rejects a competing allocation",async()=>{
 const bytes=encode("hello"),{ref,headers}=fixture(bytes);let complete!:(b:ArrayBuffer)=>void;
 const digest=vi.fn(()=>new Promise<ArrayBuffer>(r=>{complete=r;}));vi.stubGlobal("crypto",{subtle:{digest}});
 const fetcher=vi.fn(async()=>new Response(bytes,{headers}));vi.stubGlobal("fetch",fetcher);
 const signal=new AbortController(),first=fetchVerifiedContent(reading,request,ref,session,signal.signal);
 const rejected=expect(first).rejects.toMatchObject({name:"AbortError"});await vi.waitFor(()=>expect(digest).toHaveBeenCalledOnce());signal.abort();
 await expect(fetchVerifiedContent(reading,request,ref,session,new AbortController().signal)).rejects.toMatchObject({status:429});expect(fetcher).toHaveBeenCalledOnce();
 complete(new ArrayBuffer(32));await rejected;
});
it("cancels the active reader on scope change and suppresses late bytes",async()=>{
 const {ref,headers}=fixture(encode("hello"));let source!:ReadableStreamDefaultController<Uint8Array>;const cancel=vi.fn();
 vi.stubGlobal("fetch",vi.fn(async()=>new Response(new ReadableStream({start(c){source=c;},cancel}),{headers})));
 const pending=fetchVerifiedContent(reading,request,ref,session,new AbortController().signal);const rejected=expect(pending).rejects.toMatchObject({name:"AbortError"});
 await vi.waitFor(()=>expect(source).toBeTruthy());window.dispatchEvent(new Event("xinsight:scope-changed"));await rejected;expect(cancel).toHaveBeenCalledOnce();
});
it("validates a saved 17 MiB representation while limiting text preview",async()=>{
 const bytes=new Uint8Array(17*1024*1024).fill(97),{ref,headers}=fixture(bytes);vi.stubGlobal("fetch",vi.fn(async()=>new Response(bytes,{headers})));
 const result=await fetchVerifiedContent(reading,request,ref,session,new AbortController().signal);
 expect(result.bytes.length).toBe(bytes.length);expect(result.previewOnly).toBe(true);expect(result.text?.length).toBe(MAX_TEXT_PREVIEW_BYTES);
 const tooBig={...ref,representation:{...ref.representation!,saved_bytes:MAX_CONTENT_BYTES+1}};
 await expect(fetchVerifiedContent(reading,request,tooBig,session,new AbortController().signal)).rejects.toThrow();
});
it("does not accept a content page from another membership or request",async()=>{
 const {ref}=fixture(encode(""));const page={version:"xinsight.runtime-content.v1",reading_id:reading.reading_id,request_ref:request.request_ref,membership_ref:"other",content_interpretation_revision:"xinsight.content-membership.v1",member_basis:"content_manifest",items:[{reference:ref,availability:{state:"available"}}]};
 vi.stubGlobal("fetch",vi.fn(async()=>new Response(JSON.stringify(page),{headers:{"X-XInsight-Scope-Revision":session.scope_revision!}})));
 await expect(fetchContentPage(reading,request,session,new AbortController().signal)).rejects.toMatchObject({problem:{code:"content_integrity_failed"}});
});

it("preserves valid UTF-8 when the bounded preview ends inside a codepoint",async()=>{
 const body="a".repeat(MAX_TEXT_PREVIEW_BYTES-1)+"中",bytes=encode(body),{ref,headers}=fixture(bytes);
 vi.stubGlobal("fetch",vi.fn(async()=>new Response(bytes,{headers})));
 const result=await fetchVerifiedContent(reading,request,ref,session,new AbortController().signal);
 expect(result.previewOnly).toBe(true);expect(result.text).toBe("a".repeat(MAX_TEXT_PREVIEW_BYTES-1));
});
it("reports an early capacity error without falsely invalidating the scope",async()=>{
 const {ref}=fixture(encode("hello")),changed=vi.fn();window.addEventListener("xinsight:scope-changed",changed);
 vi.stubGlobal("fetch",vi.fn(async()=>new Response(JSON.stringify({type:"urn:test",title:"limit",status:503,code:"content_read_limit",retryable:false}),{status:503})));
 try{await expect(fetchVerifiedContent(reading,request,ref,session,new AbortController().signal)).rejects.toMatchObject({status:503,problem:{code:"content_read_limit"}});expect(changed).not.toHaveBeenCalled();}finally{window.removeEventListener("xinsight:scope-changed",changed);}
});

const observation={protocol:"openai.chat_completions.v1",observation_point:"client_request",content_item_id:"body-0",occurred_at:"2026-09-21T00:00:00.000001Z",capture_reason:"none"};
it("accepts exact observation metadata and keeps source identity out of the body",()=>{
 const {ref}=fixture(encode("{}"));expect(decodeContentObservation(observation,ref)).toEqual(observation);
 expect(decodeContentObservation({...observation,attempt_id:"explicit-client-attempt"},ref).attempt_id).toBe("explicit-client-attempt");
 expect(decodeContentObservation({...observation,observation_point:"provider_response",attempt_id:"real-attempt"},ref).attempt_id).toBe("real-attempt");
 expect(decodeContentObservation({...observation,protocol:"unknown.protocol.v2"},ref).protocol).toBe("unknown.protocol.v2");
});
it.each([null,{}, {...observation,protocol:""},{...observation,protocol:"model name"}, {...observation,protocol:"x".repeat(129)}, {...observation,attempt_id:""}, {...observation,observation_point:"provider_response"}, {...observation,observation_point:"provider_response",attempt_id:""}, {...observation,observation_point:"tool_result"}, {...observation,content_item_id:""},{...observation,capture_reason:"stream_interrupted"},{...observation,occurred_at:"2026-02-30T00:00:00.000000Z"},{...observation,source_system_id:"body-controlled"},{...observation,session_ref:"guessed"}])("rejects present invalid observation %#",value=>{
 expect(()=>decodeContentObservation(value,fixture(encode("{} ")).ref)).toThrow();
});
it("validates capture reasons against frozen completeness",()=>{
 const {ref}=fixture(encode("{}"));
 for(const [capture_status,capture_reason] of [["partial","stream_interrupted"],["pending","capture_in_progress"],["not_captured","policy_metadata_only"]] as const)expect(decodeContentObservation({...observation,capture_reason},{...ref,capture_status}).capture_reason).toBe(capture_reason);
});
it("allows missing legacy metadata but rejects null or malformed metadata on a page",async()=>{
 const {ref}=fixture(encode("{}"));
 for(const extra of [{},{observation},{observation:null},{observation:{...observation,attempt_id:""}}]) {
  const page={version:"xinsight.runtime-content.v1",reading_id:reading.reading_id,request_ref:request.request_ref,membership_ref:reading.membership_ref,content_interpretation_revision:"xinsight.content-membership.v1",member_basis:"content_manifest",items:[{reference:ref,availability:{state:"available"},...extra}]};
  vi.stubGlobal("fetch",vi.fn(async()=>new Response(JSON.stringify(page),{headers:{"X-XInsight-Scope-Revision":session.scope_revision!}})));
  const promise=fetchContentPage(reading,request,session,new AbortController().signal);
  if("observation" in extra&&extra.observation!==observation)await expect(promise).rejects.toMatchObject({problem:{code:"content_integrity_failed"}});
  else expect((await promise).items).toHaveLength(1);
 }
});
