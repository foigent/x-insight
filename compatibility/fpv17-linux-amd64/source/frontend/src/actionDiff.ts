import { readVerifiedJSON } from "./contentProjection";
import type { VerifiedContent } from "./content";
import type { ActionContentLink } from "./runtime";

function atPointer(root: unknown, pointer: string): unknown {
  if (pointer === "") return root;
  if (!pointer.startsWith("/")) throw new Error("invalid pointer");
  let value = root;
  for (const escaped of pointer.slice(1).split("/")) {
    if (/~(?![01])/.test(escaped)) throw new Error("invalid pointer");
    const key = escaped.replace(/~1/g, "/").replace(/~0/g, "~");
    if (Array.isArray(value)) {
      if (!/^(0|[1-9][0-9]*)$/.test(key)) throw new Error("invalid array position");
      value = value[Number(key)];
    } else if (value !== null && typeof value === "object" && Object.hasOwn(value, key)) {
      value = (value as Record<string, unknown>)[key];
    } else throw new Error("pointer missing");
  }
  return value;
}

export function actionLinkedText(content: VerifiedContent, link: ActionContentLink): string | undefined {
  const ref=content.reference;
  if(ref.capture_status!=="complete"||content.bytes.byteLength>256*1024||["source_system_id","source_instance","org_id","project_id","request_id","source_fact_id","revision","content_ref","source_sha256"].some(k=>ref[k as keyof typeof ref]!==link[k as keyof ActionContentLink]))return undefined;
  try{
    const root=readVerifiedJSON(new TextDecoder("utf-8",{fatal:true}).decode(content.bytes));
    const value=atPointer(root,link.json_pointer);
    return typeof value==="string"?value:undefined;
  }catch{return undefined}
}

export async function verifiedActionFileText(content: VerifiedContent, link: ActionContentLink, expectedHash: string): Promise<{state:"verified"|"fragment"|"mismatch"|"unavailable";text?:string}> {
  const ref = content.reference;
  if (["source_system_id","source_instance","org_id","project_id","request_id","source_fact_id","revision","content_ref","source_sha256"].some(k => ref[k as keyof typeof ref] !== link[k as keyof ActionContentLink])) return {state:"mismatch"};
  if (link.extent !== "full_file" || ref.capture_status !== "complete") return {state:"fragment"};
  if (content.bytes.byteLength > 256*1024) return {state:"unavailable"};
  try {
    const raw = new TextDecoder("utf-8",{fatal:true}).decode(content.bytes);
    const value = atPointer(readVerifiedJSON(raw),link.json_pointer);
    if (typeof value !== "string") return {state:"unavailable"};
    const bytes = new TextEncoder().encode(value);
    const digest = await crypto.subtle.digest("SHA-256",bytes);
    const sha="sha256:"+[...new Uint8Array(digest)].map(v=>v.toString(16).padStart(2,"0")).join("");
    return sha===expectedHash ? {state:"verified",text:value} : {state:"mismatch"};
  } catch { return {state:"unavailable"}; }
}

export function boundedFileDiff(before: string, after: string): {removed:string[];added:string[];omitted:boolean} {
  const a=before.split("\n"),b=after.split("\n");
  let first=0;while(first<a.length&&first<b.length&&a[first]===b[first])first++;
  let lastA=a.length,lastB=b.length;while(lastA>first&&lastB>first&&a[lastA-1]===b[lastB-1]){lastA--;lastB--;}
  const removed=a.slice(first,lastA),added=b.slice(first,lastB);
  return {removed:removed.slice(0,100),added:added.slice(0,100),omitted:removed.length>100||added.length>100};
}
