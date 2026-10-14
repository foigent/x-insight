import { localizedLabels, t } from "./i18n";
import { MAX_TEXT_PREVIEW_BYTES, type ContentLocation, type ContentObservation, type VerifiedContent } from "./content";

// Transient interpretation of one verified representation. No cross-body index,
// persisted projection, inferred session, resource event or execution authority.
export const CONTENT_PARSE_DEPTH = 64;
export const CONTENT_PARSE_NODES = 4096;
export const CONTENT_PREVIEW_ROWS = 200;
export interface ContentPreviewRow {
  kind: "message" | "tool_call" | "tool_result" | "event";
  location: ContentLocation;
  label: string;
  text: string;
  callID?: string;
  // Local JSON container only, never a protocol session or persisted identity.
  container?: string;
  relatedRow?: number;
  notice?: string;
}
export interface ContentPreview { rows: ContentPreviewRow[]; notes: string[] }
const obj = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === "object" && !Array.isArray(v);
const own = (v: unknown, key: string): unknown => obj(v) && Object.hasOwn(v,key) ? v[key] : undefined;
const str = (v: unknown): v is string => typeof v === "string";
const identifier = (v: unknown): v is string => str(v) && v.length > 0 && v.length <= 256 && !/[\u0000-\u001f\u007f]/.test(v);
const pointer = (path: string): ContentLocation => ({kind:"json_pointer",pointer:path});
const roles = new Set(["system","developer","user","assistant","tool"]);
const roleLabels:Record<string,string>=localizedLabels({}, {system: "m2848", developer: "m2849", user: "m2850", assistant: "m2851", tool: "m2852"});
const show = (v: unknown): string => str(v) ? v : v === undefined ? "" : JSON.stringify(v);

// JSON.parse alone silently accepts duplicate keys. This small bounded reader
// rejects them (including escaped equivalents), preserves prototype-named keys
// as inert own properties, and never interprets JSON contained inside strings.
export function readVerifiedJSON(raw: string): unknown {
  let pos=0, nodes=0;
  const bad = (): never => { throw new Error("JSON structure unavailable"); };
  const ws = () => { while (pos<raw.length && /[\x20\t\r\n]/.test(raw[pos])) pos++; };
  function quoted(): string {
    const start=pos++;
    while(pos<raw.length) {
      const c=raw[pos++];
      if(c==='\\') { pos++; continue; }
      if(c==='"') {
        const value: unknown=JSON.parse(raw.slice(start,pos));
        if(!str(value)) return bad();
        for(let i=0;i<value.length;i++) {
          const code=value.charCodeAt(i);
          if(code>=0xd800&&code<=0xdbff) { const low=value.charCodeAt(++i); if(!(low>=0xdc00&&low<=0xdfff))return bad(); }
          else if(code>=0xdc00&&code<=0xdfff) return bad();
        }
        return value;
      }
    }
    return bad();
  }
  function value(depth:number): unknown {
    ws();if(depth>CONTENT_PARSE_DEPTH||++nodes>CONTENT_PARSE_NODES)return bad();
    const c=raw[pos];
    if(c==='"') return quoted();
    if(c==='{'||c==='[') {
      pos++;ws();const object=c==='{'?Object.create(null) as Record<string,unknown>:null;
      const array:unknown[]=[];const end=c==='{'?'}':']';
      if(raw[pos]===end){pos++;return object??array;}
      while(pos<raw.length) {
        if(object) {
          ws();if(raw[pos]!=='"')return bad();const key=quoted();ws();
          if(Object.hasOwn(object,key)||raw[pos++]!==':')return bad();
          object[key]=value(depth+1);
        } else array.push(value(depth+1));
        ws();if(raw[pos]===end){pos++;return object??array;}
        if(raw[pos++]!==',')return bad();
      }
      return bad();
    }
    const token=/^(?:true|false|null|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/.exec(raw.slice(pos))?.[0];
    if(!token)return bad();pos+=token.length;
    const parsed:unknown=JSON.parse(token);
    // show() serializes nested values. Require the exact original numeric token
    // to survive that conversion, including decimals, underflow and negative
    // zero. Otherwise fall back to raw content instead of displaying rounding.
    if(typeof parsed==='number'&&(!Number.isFinite(parsed)||(Number.isInteger(parsed)&&!Number.isSafeInteger(parsed))||JSON.stringify(parsed)!==token))return bad();
    return parsed;
  }
  const result=value(0);ws();if(pos!==raw.length)return bad();return result;
}

function linkLocalDeclarations(rows:ContentPreviewRow[]) {
  const calls=new Map<string,number[]>(),results=new Map<string,number[]>();
  const key=(r:ContentPreviewRow)=>JSON.stringify([r.container,r.callID]);
  rows.forEach((r,i)=>{if(r.callID){const map=r.kind==='tool_call'?calls:r.kind==='tool_result'?results:null;if(map)map.set(key(r),[...(map.get(key(r))??[]),i]);}});
  rows.forEach((r,i)=>{
    if(r.kind!=='tool_call'&&r.kind!=='tool_result')return;
    const matches=r.callID?calls.get(key(r))??[]:[];
    const replies=r.callID?results.get(key(r))??[]:[];
    if(matches.length>1||replies.length>1){r.notice=t("m2853");return;}
    if(r.kind==='tool_result') {
      if(matches.length===1&&matches[0]<i) {r.relatedRow=matches[0];r.notice=t("m2854", { value0: matches[0]+1 });}
      else r.notice=t("m2855");
    } else r.notice=r.notice??t("m2856");
  });
}

function jsonRows(root:unknown, protocol:string, request:boolean):ContentPreviewRow[] {
  const rows:ContentPreviewRow[]=[];
  let container="";
  const add=(row:ContentPreviewRow)=>{if(rows.length>=CONTENT_PREVIEW_ROWS)throw new Error("row limit");rows.push({...row,container});};
  const message=(role:unknown,content:unknown,path:string)=>{
    if(!str(role)||!roles.has(role))throw new Error("unknown role");
    add({kind:"message",label:t("m2857", { value0: roleLabels[role] }),text:show(content),location:pointer(path)});
  };
  const chatMessage=(m:unknown,path:string)=>{
    const role=own(m,"role");if(!str(role)||!roles.has(role))throw new Error("unknown message");
    if(role==='tool') {
      const id=own(m,"tool_call_id");
      add({kind:"tool_result",label:t("m2858"),text:show(own(m,"content")),location:pointer(path),callID:identifier(id)?id:undefined});return;
    }
    message(role,own(m,"content"),path);
    const calls=own(m,"tool_calls");if(calls===undefined)return;
    if(role!=='assistant'||!Array.isArray(calls))throw new Error("invalid tool calls");
    calls.forEach((call,j)=>{
      const fn=own(call,"function"),id=own(call,"id"),name=own(fn,"name"),args=own(fn,"arguments");
      if(own(call,"type")!=='function'||!identifier(name)||!str(args))throw new Error("unsupported tool call");
      let notice=t("m2856");
      try{readVerifiedJSON(args);}catch{notice=t("m2859");}
      add({kind:"tool_call",label:t("m2860", { value0: name }),text:args,location:pointer(`${path}/tool_calls/${j}`),callID:identifier(id)?id:undefined,notice});
    });
  };
  if(protocol==='openai.chat_completions.v1'||protocol==='integration.openai.chat.v1') {
    if(request){container='/messages';const messages=own(root,'messages');if(!Array.isArray(messages))throw new Error("missing messages");messages.forEach((m,i)=>chatMessage(m,`/messages/${i}`));}
    else {const choices=own(root,'choices');if(!Array.isArray(choices))throw new Error("missing choices");choices.forEach((c,i)=>{const m=own(c,'message');if(own(m,'role')!=='assistant')throw new Error('invalid response message role');container=`/choices/${i}`;chatMessage(m,`/choices/${i}/message`);});}
  } else if(protocol==='anthropic.messages.v1') {
    const nativeMessage=(m:unknown,path:string)=>{
      const role=own(m,'role'),content=own(m,'content');
      if(role!=='assistant'&&role!=='user')throw new Error("unknown message");
      if(str(content)){message(role,content,path);return;}
      if(!Array.isArray(content))throw new Error("unknown blocks");
      content.forEach((b,j)=>{
        const at=`${path}/content/${j}`,type=own(b,'type');
        if(type==='text'&&str(own(b,'text')))message(role,own(b,'text'),at);
        else if(type==='tool_use'&&role==='assistant'&&identifier(own(b,'name'))&&obj(own(b,'input'))) {
          const id=own(b,'id');add({kind:'tool_call',label:t("m2860", { value0: own(b,'name') }),text:show(own(b,'input')),location:pointer(at),callID:identifier(id)?id:undefined});
        } else if(type==='tool_result'&&role==='user') {
          const id=own(b,'tool_use_id');add({kind:'tool_result',label:t("m2858"),text:show(own(b,'content')),location:pointer(at),callID:identifier(id)?id:undefined});
        } else add({kind:'message',label:t("m2861", { value0: roleLabels[role] }),text:show(b),location:pointer(at),notice:t("m2862")});
      });
    };
    if(request){container='/messages';const messages=own(root,'messages');if(!Array.isArray(messages))throw new Error("missing messages");if(own(root,'system')!==undefined)message('system',own(root,'system'),'/system');messages.forEach((m,i)=>nativeMessage(m,`/messages/${i}`));}
    else {if(own(root,'type')!=='message')throw new Error("unknown response");nativeMessage(root,'');}
  } else if(protocol==='openai.responses.v1') {
    const items=own(root,request?'input':'output'),base=request?'/input':'/output';
    if(request&&str(items))message('user',items,base);
    else {
      if(!Array.isArray(items)||(!request&&own(root,'object')!=='response'))throw new Error("unknown response items");
      items.forEach((m,i)=>{
        const role=own(m,'role'),type=own(m,'type');
        if((type==='message'||(request&&type===undefined))&&str(role)&&roles.has(role))message(role,own(m,'content'),`${base}/${i}`);
        else add({kind:'message',label:t("m2863"),text:show(m),location:pointer(`${base}/${i}`),notice:t("m2864")});
      });
    }
  } else throw new Error("unsupported protocol");
  linkLocalDeclarations(rows);return rows;
}

// Byte scan only frames SSE events. JSON nodes use existing JSON Pointer proof;
// no UTF-16-to-byte conversion or serialized JSON is used as a source location.
function ssePreview(bytes:Uint8Array,protocol:string):ContentPreview {
  const rows:ContentPreviewRow[]=[],notes:string[]=[];
  const end=Math.min(bytes.byteLength,MAX_TEXT_PREVIEW_BYTES);
  let pos=0,eventStart=0,lines:string[]=[],pending=false,totalNodes=0,lineCount=0;
  const decode=new TextDecoder('utf-8',{fatal:true});
  while(pos<end) {
    if(++lineCount>CONTENT_PARSE_NODES){notes.push(t("m2865"));break;}
    const start=pos;while(pos<end&&bytes[pos]!==10&&bytes[pos]!==13)pos++;
    if(pos===end){pending=true;break;}
    const lineEnd=pos;
    if(bytes[pos]===13&&pos+1===end&&end<bytes.byteLength){pending=true;break;}
    pos+=bytes[pos]===13&&bytes[pos+1]===10?2:1;
    let line:string;try{line=decode.decode(bytes.subarray(start,lineEnd));}catch{notes.push(t("m2866"));pending=true;break;}
    if(line!==''){lines.push(line);pending=true;continue;}
    pending=false;
    const dataLines=lines.filter(l=>l==='data'||l.startsWith('data:'));
    const data=dataLines.map(l=>l==='data'?'':l.slice(5).replace(/^ /,'')).join('\n');
    const names=lines.filter(l=>l==='event'||l.startsWith('event:')).map(l=>l==='event'?'':l.slice(6).replace(/^ /,''));
    const location:ContentLocation={kind:'byte_range',offset:eventStart,length:pos-eventStart};eventStart=pos;lines=[];
    if(!dataLines.length)continue;
    if(rows.length>=CONTENT_PREVIEW_ROWS){notes.push(t("m2867"));break;}
    const row:ContentPreviewRow={kind:'event',label:t("m2868", { value0: rows.length+1 }),text:data,location,notice:t("m2869")};
    if(data==='[DONE]'&&(protocol==='openai.chat_completions.v1'||protocol==='integration.openai.chat.v1'))row.label=t("m2870");
    else {
      try {
        const event=readVerifiedJSON(data);totalNodes+=countNodes(event);
        if(totalNodes>CONTENT_PARSE_NODES){notes.push(t("m2871"));break;}
        if(names.length>1)throw new Error('ambiguous SSE event');
        const type=own(event,'type');
        if(protocol==='openai.chat_completions.v1'||protocol==='integration.openai.chat.v1') {
          const choices=own(event,'choices');
          if(own(event,'object')==='chat.completion.chunk'&&Array.isArray(choices)){
            row.label=t("m2872");
            const fragments:string[]=[];
            for(const choice of choices) {
              const delta=own(choice,'delta'),index=own(choice,'index');
              if(str(own(delta,'content')))fragments.push(t("m2873", { value0: Number.isSafeInteger(index)?` [${index}]`:'', value1: own(delta,'content') }));
              const calls=own(delta,'tool_calls');
              if(Array.isArray(calls))for(const call of calls){const args=own(own(call,'function'),'arguments');if(str(args))fragments.push(t("m2874", { value0: args }));}
            }
            if(fragments.length)row.text=fragments.join('\n');
          }
          else row.notice=t("m2875");
        } else if(str(type)&&((protocol==='openai.responses.v1'&&['response.created','response.in_progress','response.output_item.added','response.output_item.done','response.content_part.added','response.content_part.done','response.output_text.delta','response.output_text.done','response.completed','response.incomplete','response.failed','error'].includes(type))||(protocol==='anthropic.messages.v1'&&['message_start','message_delta','message_stop','content_block_start','content_block_delta','content_block_stop','ping','error'].includes(type)))) {
          if(names.length&&names[0]!==type)throw new Error('event type mismatch');
          const labels:Record<string,string>={"response.created":t("m2876"),"response.in_progress":t("m2877"),"response.output_item.added":t("m2878"),"response.output_item.done":t("m2879"),"response.content_part.added":t("m2880"),"response.content_part.done":t("m2881"),"response.output_text.delta":t("m2882"),"response.output_text.done":t("m2883"),"response.completed":t("m2884"),"response.incomplete":t("m2885"),"response.failed":t("m2886"),message_start:t("m2887"),message_delta:t("m2888"),message_stop:t("m2889"),content_block_start:t("m2890"),content_block_delta:t("m2891"),content_block_stop:t("m2892"),ping:t("m2893"),error:t("m2894")};
          row.label=labels[type];
          const delta=own(event,'delta');
          if(type==='response.output_text.delta'&&str(delta))row.text=t("m2895", { value0: delta });
          if(type==='content_block_delta') {
            if(own(delta,'type')==='text_delta'&&str(own(delta,'text')))row.text=t("m2896", { value0: own(delta,'text') });
            if(own(delta,'type')==='input_json_delta'&&str(own(delta,'partial_json')))row.text=t("m2874", { value0: own(delta,'partial_json') });
          }
        } else row.notice=t("m2875");
      }catch{row.notice=t("m2897");}
    }
    rows.push(row);
  }
  if(pending||eventStart<end&&pos>=end)notes.push(t("m2898"));
  if(end<bytes.byteLength)notes.push(t("m2899"));
  return {rows,notes};
}
function countNodes(v:unknown):number {return 1+(Array.isArray(v)?v.reduce<number>((n,x)=>n+countNodes(x),0):obj(v)?Object.values(v).reduce<number>((n,x)=>n+countNodes(x),0):0);}

export function projectContent(verified:VerifiedContent,observation?:ContentObservation):ContentPreview {
  const notes:string[]=[];
  if(!observation)return {rows:[],notes:[t("m2900")]};
  if(!['openai.chat_completions.v1','integration.openai.chat.v1','anthropic.messages.v1','openai.responses.v1'].includes(observation.protocol))return {rows:[],notes:[t("m2901")]};
  if(verified.reference.capture_status==='partial')notes.push(t("m2902"));
  const media=verified.reference.representation?.media_type.toLowerCase(),request=observation.observation_point.endsWith('_request');
  if(media==='text/event-stream'&&!request){const result=ssePreview(verified.bytes,observation.protocol);return {rows:result.rows,notes:[...notes,...result.notes]};}
  if(!media||!/^application\/(?:json|[^;]+\+json)$/.test(media))return {rows:[],notes:[...notes,t("m2903")]};
  if(verified.bytes.byteLength>MAX_TEXT_PREVIEW_BYTES)return {rows:[],notes:[...notes,t("m2904")]};
  try {
    const root=readVerifiedJSON(new TextDecoder('utf-8',{fatal:true}).decode(verified.bytes));
    return {rows:jsonRows(root,observation.protocol,request),notes};
  }catch{return {rows:[],notes:[...notes,t("m2905")]};}
}
