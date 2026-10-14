import { beforeEach as beforeLegacyLocale } from "vitest";
import { setLocale as setLegacyLocale } from "./i18n";
// These existing semantic assertions explicitly exercise the Chinese translation.
beforeLegacyLocale(() => setLegacyLocale("zh-CN"));
import { describe, expect, it } from "vitest";
import { boundedFileDiff, verifiedActionFileText } from "./actionDiff";
import type { VerifiedContent } from "./content";
import type { ActionContentLink } from "./runtime";

const encoder=new TextEncoder();
const digest=async(s:string)=>"sha256:"+[...new Uint8Array(await crypto.subtle.digest("SHA-256",encoder.encode(s)))].map(x=>x.toString(16).padStart(2,"0")).join("");
const link:ActionContentLink={purpose:"before",extent:"full_file",source_system_id:"source",source_instance:"ur",org_id:"1",project_id:"2",request_id:"r",source_fact_id:"fact",revision:"1",content_ref:"content",source_sha256:"sha256:"+"a".repeat(64),json_pointer:"/messages/0/content"};
function content(value:string,status:"complete"|"partial"="complete"):VerifiedContent {
  return {bytes:encoder.encode(JSON.stringify({messages:[{content:value}]})),text:null,previewOnly:false,reference:{...link,capture_status:status,representation:{media_type:"application/json",encoding:"identity",byte_domain:"stored_representation_before_http_content_encoding",saved_bytes:0,digest:"sha256:"+"b".repeat(64)}}};
}
describe("action file difference boundary",()=>{
  it("shows exact full-file text only when checker hash matches",async()=>{
    const body="完成记录：待填\n";
    expect(await verifiedActionFileText(content(body),link,await digest(body))).toEqual({state:"verified",text:body});
    expect(await verifiedActionFileText(content(body),link,await digest("other"))).toEqual({state:"mismatch"});
    expect(await verifiedActionFileText(content(body,"partial"),link,await digest(body))).toEqual({state:"fragment"});
    expect(boundedFileDiff("a\nold\nz","a\nnew\nz")).toEqual({removed:["old"],added:["new"],omitted:false});
  });
  it("does not interpret duplicate-key or unrelated bodies",async()=>{
    const bad=content("x");bad.bytes=encoder.encode('{"messages":[{"content":"x","content":"y"}]}');
    expect(await verifiedActionFileText(bad,link,await digest("y"))).toEqual({state:"unavailable"});
    expect(await verifiedActionFileText(content("x"),{...link,request_id:"other"},await digest("x"))).toEqual({state:"mismatch"});
  });
});
