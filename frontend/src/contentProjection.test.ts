import { beforeEach as beforeLegacyLocale } from "vitest";
import { setLocale as setLegacyLocale } from "./i18n";
// These existing semantic assertions explicitly exercise the Chinese translation.
beforeLegacyLocale(() => setLegacyLocale("zh-CN"));
import { describe, expect, it } from "vitest";
import { CONTENT_PARSE_DEPTH, CONTENT_PARSE_NODES, CONTENT_PREVIEW_ROWS, projectContent } from "./contentProjection";
import { MAX_TEXT_PREVIEW_BYTES, type ContentObservation, type FrozenContentReference, type VerifiedContent } from "./content";
const encode=(s:string)=>new TextEncoder().encode(s);
const observation:ContentObservation={protocol:"openai.chat_completions.v1",observation_point:"client_request",content_item_id:"body-0",occurred_at:"2026-09-21T00:00:00.000000Z",capture_reason:"none"};
const ref={source_system_id:"s",source_instance:"i",org_id:"7",project_id:"9",request_id:"request",source_fact_id:"f",revision:"2",source_sha256:"hash",capture_status:"complete"} as FrozenContentReference;
function saved(raw:string,media="application/json",reference=ref):VerifiedContent {return {reference:{...reference,representation:{media_type:media,encoding:"identity",byte_domain:"stored_representation_before_http_content_encoding",saved_bytes:encode(raw).length,digest:"digest"}},bytes:encode(raw),text:raw.slice(0,MAX_TEXT_PREVIEW_BYTES),previewOnly:encode(raw).length>MAX_TEXT_PREVIEW_BYTES};}
// Accepted C0 normal_tool_roundtrip_saved_bytes, not a fabricated session link.
const roundtrip='{"messages":[{"content":"Read the synthetic note.","role":"user"},{"role":"assistant","tool_calls":[{"function":{"arguments":"{\\"name\\":\\"synthetic-note\\"}","name":"read_note"},"id":"call-1","type":"function"}]},{"content":"synthetic note contents","role":"tool","tool_call_id":"call-1"}]}';
const call={role:"assistant",tool_calls:[{type:"function",id:"same",function:{name:"read",arguments:'{"path":"note"}'}}]};
const result={role:"tool",tool_call_id:"same",content:"reported"};
function parse(messages:unknown[]){return projectContent(saved(JSON.stringify({messages})),observation);}
it("projects the original roundtrip with original pointers and only a local declaration link",()=>{
 const bytes=saved(roundtrip),before=bytes.bytes.slice();const p=projectContent(bytes,observation);
 expect(p.rows.map(r=>r.kind)).toEqual(["message","message","tool_call","tool_result"]);
 expect(p.rows[2].location).toEqual({kind:"json_pointer",pointer:"/messages/1/tool_calls/0"});
 expect(p.rows[3].location).toEqual({kind:"json_pointer",pointer:"/messages/2"});expect(p.rows[3].relatedRow).toBe(2);
 expect(p.rows[3].notice).toContain("不证明实际执行");expect(bytes.bytes).toEqual(before);expect(JSON.stringify(p)).not.toContain("session_ref");
});
it.each(["openai.chat_completions.v1","integration.openai.chat.v1"])("uses only explicit source protocol %s",protocol=>{
 expect(projectContent(saved(roundtrip),{...observation,protocol}).rows).toHaveLength(4);
});
it("does not infer protocol from body or accept a provider name as protocol",()=>{
 expect(projectContent(saved(roundtrip)).rows).toEqual([]);
 expect(projectContent(saved(roundtrip),{...observation,protocol:"some-model"}).rows).toEqual([]);
 expect(projectContent(saved(roundtrip),{...observation,observation_point:"provider_response",attempt_id:"a"}).rows).toEqual([]);
});
it("never pairs declarations across sources requests attempts revisions or stored bodies",()=>{
 for(const change of [{source_system_id:"other"},{request_id:"other"},{revision:"3"}]) {
  projectContent(saved(JSON.stringify({messages:[call]})),observation);
  const p=projectContent(saved(JSON.stringify({messages:[result]}),'application/json',{...ref,...change}),{...observation,observation_point:"provider_request",attempt_id:"other-attempt"});
  expect(p.rows[0].relatedRow).toBeUndefined();expect(p.rows[0].notice).toContain("未找到唯一");
 }
});
it.each([[call,call,result],[call,result,result],[result,call]])("retains ambiguous or reversed declarations without guessing",(...messages)=>{
 const p=parse(messages);expect(p.rows.filter(r=>r.kind==='tool_result').every(r=>r.relatedRow===undefined)).toBe(true);
});
it("keeps a partial argument string without inventing a completed call",()=>{
 const p=parse([{...call,tool_calls:[{...call.tool_calls[0],function:{name:'read',arguments:'{"path":'}}]},result]);
 expect(p.rows[1].text).toBe('{"path":');expect(p.rows[1].notice).toContain('不是完整');
});
it("does not recursively interpret messages IDs scripts or file paths inside tool text",()=>{
 const text=JSON.stringify({messages:[call,result],id:'same',paths:Array(10).fill('/secret'),script:'read_all()',accesses:Array(7).fill('claimed')});
 const p=parse([{role:'user',content:text},call,{...result,content:text}]);
 expect(p.rows.filter(r=>r.kind==='tool_result')).toHaveLength(1);expect(p.rows.filter(r=>r.kind==='tool_call')).toHaveLength(1);
 expect(JSON.stringify(p)).not.toContain('resource_access');
});
it("preserves unicode escapes and prototype-named keys as inert JSON with an original pointer",()=>{
 const raw=' {"__proto__":{"polluted":true},"constructor":{"messages":[]},"messages":[{"role":"user","content":"中😀\\u4e2d\\n\\\\\\\""}]} ';
 const p=projectContent(saved(raw),observation);expect(p.rows).toHaveLength(1);expect(p.rows[0].text).toBe('中😀中\n\\"');
 expect(p.rows[0].location).toEqual({kind:'json_pointer',pointer:'/messages/0'});expect(({} as Record<string,unknown>).polluted).toBeUndefined();
});
it.each([
 '{"messages":[],"messages":[{"role":"user","content":"x"}]}',
 '{"messages":[],"mess\\u0061ges":[]}',
 '{"messages":[{"role":"user","role":"tool","content":"x"}]}',
 '{"messages":[{"role":"user","content":"\\ud800"}]}',
 '{"messages":[{"role":"user","content":"\\udc00"}]}',
 '{"messages":[],"n":9007199254740993}',
 '{"messages":[]} trailing','{"messages":[', '{"messages":[],}',
 '{"messages":[{"role":"constructor","content":"x"}]}',
])("falls back on ambiguous invalid or unsupported JSON: %s",raw=>{expect(projectContent(saved(raw),observation).rows).toEqual([]);});
it("rejects invalid UTF-8 without replacement-character semantics",()=>{
 const v=saved(roundtrip);v.bytes=new Uint8Array([123,34,0xff,34,58,49,125]);expect(projectContent(v,observation).rows).toEqual([]);
});
it("does not mistake capture partial for parsing failure or parsing success for capture complete",()=>{
 const p=projectContent(saved(roundtrip,'application/json',{...ref,capture_status:'partial'}),{...observation,capture_reason:'stream_interrupted'});
 expect(p.rows).toHaveLength(4);expect(p.notes.join()).toContain('部分采集');
});
it("bounds bytes depth nodes and row count independently",()=>{
 expect(projectContent(saved(' '.repeat(MAX_TEXT_PREVIEW_BYTES+1)),observation).rows).toEqual([]);
 expect(projectContent(saved('{"messages":[],"deep":'+'['.repeat(CONTENT_PARSE_DEPTH+1)+'0'+']'.repeat(CONTENT_PARSE_DEPTH+1)+'}'),observation).rows).toEqual([]);
 expect(projectContent(saved(JSON.stringify({messages:[],many:Array(CONTENT_PARSE_NODES).fill(null)})),observation).rows).toEqual([]);
 expect(parse(Array(CONTENT_PREVIEW_ROWS+1).fill({role:'user',content:'x'})).rows).toEqual([]);
 expect(parse(Array(CONTENT_PREVIEW_ROWS).fill({role:'user',content:'x'})).rows).toHaveLength(CONTENT_PREVIEW_ROWS);
});
it("supports source-qualified native Messages blocks without executing their inputs",()=>{
 const p=projectContent(saved(JSON.stringify({messages:[{role:'assistant',content:[{type:'tool_use',id:'t',name:'read',input:{path:'/note'}}]},{role:'user',content:[{type:'tool_result',tool_use_id:'t',content:'reported'}]}]})),{...observation,protocol:'anthropic.messages.v1'});
 expect(p.rows.map(r=>r.kind)).toEqual(['tool_call','tool_result']);expect(p.rows[1].relatedRow).toBe(0);expect(p.rows[1].location).toEqual(pointerAt('/messages/1/content/0'));
 const native=projectContent(saved('{"type":"message","role":"assistant","content":[{"type":"text","text":"hello"}]}'),{...observation,protocol:'anthropic.messages.v1',observation_point:'provider_response',attempt_id:'a'});
 expect(native.rows[0].location).toEqual(pointerAt('/content/0'));
});
const pointerAt=(pointer:string)=>({kind:'json_pointer',pointer});
it("shows known Responses messages but does not invent unsupported function tool semantics",()=>{
 const p=projectContent(saved('{"object":"response","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]},{"type":"function_call","call_id":"same"}]}'),{...observation,protocol:'openai.responses.v1',observation_point:'client_response'});
 expect(p.rows).toHaveLength(2);expect(p.rows[1].kind).toBe('message');expect(p.rows[1].notice).toContain('未确认');
});
describe('saved SSE, not HTTP chunk framing',()=>{
 const obs={...observation,observation_point:'provider_response' as const,attempt_id:'a'};
 it.each(['\n','\r\n','\r'])('maps whole event original UTF-8 bytes using %j',nl=>{
  const a=': comment'+nl+'data: {"object":"chat.completion.chunk",'+nl+'data: "choices":[{"delta":{"content":"中😀"}}]}'+nl+nl;
  const b='data: [DONE]'+nl+nl;
  const p=projectContent(saved(a+b,'text/event-stream'),obs);
  expect(p.rows).toHaveLength(2);expect(p.rows[0].location).toEqual({kind:'byte_range',offset:0,length:encode(a).length});
  expect(p.rows[1].location).toEqual({kind:'byte_range',offset:encode(a).length,length:encode(b).length});
  expect(p.rows[0].text).toContain('中😀');expect(p.rows[1].label).toContain('不改变捕获完整性');
 });
 it("keeps argument deltas as separate saved events and does not close partial on DONE",()=>{
  const raw='data: {"object":"chat.completion.chunk","choices":[{"delta":{"tool_calls":[{"index":0,"id":"c","function":{"arguments":"{\\"x\\":"}}]}}]}\n\ndata: [DONE]\n\n';
  const p=projectContent(saved(raw,'text/event-stream',{...ref,capture_status:'partial'}),{...obs,capture_reason:'stream_interrupted'});
  expect(p.rows.every(r=>r.kind==='event')).toBe(true);expect(p.notes.join()).toContain('部分采集');
 });
 it.each(['data: {"choices":', 'data: {"choices":[]}\n', 'data: [DONE]'])('does not adopt an unfinished event %s',raw=>{
  const p=projectContent(saved(raw,'text/event-stream'),obs);expect(p.rows).toEqual([]);expect(p.notes.join()).toContain('未完整结束');
 });
 it("retains malformed or conflicting completed events as raw event text",()=>{
  for(const raw of ['data: {"type":"x","type":"y"}\n\n','event: response.completed\ndata: {"type":"response.failed"}\n\n']) {
   const p=projectContent(saved(raw,'text/event-stream'),{...obs,protocol:'openai.responses.v1'});expect(p.rows).toHaveLength(1);expect(p.rows[0].notice).toContain('歧义');
  }
 });
 it("does not parse invalid UTF-8 or a cutoff event and preserves completed prefixes",()=>{
  const head='data: [DONE]\n\n',tail='data: '+ 'a'.repeat(MAX_TEXT_PREVIEW_BYTES);
  const p=projectContent(saved(head+tail,'text/event-stream'),obs);expect(p.rows).toHaveLength(1);expect(p.notes.join()).toContain('256 KiB');
  const v=saved('','text/event-stream');v.bytes=new Uint8Array([100,97,116,97,58,32,255,10,10]);expect(projectContent(v,obs).rows).toEqual([]);
 });
 it("bounds complete events",()=>{
  const p=projectContent(saved('data: [DONE]\n\n'.repeat(CONTENT_PREVIEW_ROWS+1),'text/event-stream'),obs);expect(p.rows).toHaveLength(CONTENT_PREVIEW_ROWS);expect(p.notes.join()).toContain('行数上限');
 });
});
it.each(['text/html','application/octet-stream','application/gzip'])('keeps %s inert even when bytes look like JSON',media=>{expect(projectContent(saved(roundtrip,media),observation).rows).toEqual([]);});

it("does not interpret unknown SSE event types as supported protocol events",()=>{
 const p=projectContent(saved('event: response.made_up\ndata: {"type":"response.made_up","id":"same"}\n\n','text/event-stream'),{...observation,protocol:'openai.responses.v1',observation_point:'client_response'});
 expect(p.rows[0].notice).toContain('结构未确认');expect(p.rows[0].relatedRow).toBeUndefined();
});
it("bounds SSE line and cumulative node work without changing byte verification",()=>{
 const obs={...observation,observation_point:'provider_response' as const,attempt_id:'a'};
 const comments=projectContent(saved(':x\n'.repeat(CONTENT_PARSE_NODES+1),'text/event-stream'),obs);expect(comments.notes.join()).toContain('行扫描上限');
 const event='data: '+JSON.stringify({object:'chat.completion.chunk',choices:[],values:Array(100).fill(0)})+'\n\n';
 const nodes=projectContent(saved(event.repeat(60),'text/event-stream'),obs);expect(nodes.notes.join()).toContain('节点上限');
});
it("parses at the exact preview boundary and falls back one byte beyond it",()=>{
 const raw='{"messages":[{"role":"user","content":"x"}]}';
 const exact=raw+' '.repeat(MAX_TEXT_PREVIEW_BYTES-encode(raw).length);
 expect(projectContent(saved(exact),observation).rows).toHaveLength(1);expect(projectContent(saved(exact+' '),observation).rows).toHaveLength(0);
});

it("separates response choice containers even when their call IDs are identical",()=>{
 const p=projectContent(saved(JSON.stringify({choices:[{index:0,message:call},{index:1,message:call}]})),{...observation,observation_point:'client_response'});
 const calls=p.rows.filter(r=>r.kind==='tool_call');expect(calls).toHaveLength(2);expect(calls.map(r=>r.container)).toEqual(['/choices/0','/choices/1']);expect(calls.every(r=>!r.notice?.includes('歧义'))).toBe(true);
});
it("does not link a result in another response choice to the first choice call",()=>{
 const p=projectContent(saved(JSON.stringify({choices:[{index:0,message:call},{index:1,message:result}]})),{...observation,observation_point:'client_response'});
 expect(p.rows).toEqual([]);expect(p.notes.join()).toContain('结构');
});

it("retains a completed empty data event as an exact raw event rather than dropping it",()=>{
 const raw='data:\n\n';const p=projectContent(saved(raw,'text/event-stream'),{...observation,observation_point:'client_response'});
 expect(p.rows).toHaveLength(1);expect(p.rows[0].text).toBe('');expect(p.rows[0].location).toEqual({kind:'byte_range',offset:0,length:encode(raw).length});
});

describe("exact numeric display from saved JSON",()=>{
 const native={...observation,protocol:"anthropic.messages.v1"};
 const body=(number:string)=>`{"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"amount-call","name":"calculate","input":{"amount":${number}}}]}]}`;
 it.each(["1.0000000000000001","0.1234567890123456789","1e-400","-0","1.0","1e3"])("falls back rather than rewriting original numeric token %s",number=>{
  const raw=body(number),v=saved(raw),before=v.bytes.slice(),reference=structuredClone(v.reference);
  const p=projectContent(v,native);
  expect(p.rows).toEqual([]);expect(p.notes.join()).toContain("仅显示原文");
  expect(v.text).toBe(raw);expect(v.bytes).toEqual(before);expect(v.reference).toEqual(reference);
 });
 it.each(["0","1","-42","9007199254740991","0.1","-0.125","1.5","0.12345678901234568","1e-7"])("preserves supported numeric token %s and the original node location",number=>{
  const raw=body(number),v=saved(raw),before=v.bytes.slice(),reference=structuredClone(v.reference);
  const p=projectContent(v,native);
  expect(p.rows).toHaveLength(1);expect(p.rows[0].text).toBe(`{"amount":${number}}`);
  expect(p.rows[0].location).toEqual({kind:"json_pointer",pointer:"/messages/0/content/0"});
  expect(v.text).toBe(raw);expect(v.bytes).toEqual(before);expect(v.reference).toEqual(reference);
 });
 it("keeps a lossy numeric SSE event as original text with its exact byte range",()=>{
  const data='{"object":"chat.completion.chunk","choices":[{"delta":{"content":"keep raw"}}],"amount":1e-400}';
  const raw=`data: ${data}\n\n`,v=saved(raw,"text/event-stream"),before=v.bytes.slice();
  const p=projectContent(v,{...observation,observation_point:"client_response"});
  expect(p.rows).toHaveLength(1);expect(p.rows[0].text).toBe(data);expect(p.rows[0].notice).toContain("仅显示事件原文");
  expect(p.rows[0].location).toEqual({kind:"byte_range",offset:0,length:before.length});expect(v.bytes).toEqual(before);
 });
});
