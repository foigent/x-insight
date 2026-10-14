import { beforeEach as beforeLegacyLocale } from "vitest";
import { setLocale as setLegacyLocale } from "./i18n";
// These existing semantic assertions explicitly exercise the Chinese translation.
beforeLegacyLocale(() => setLegacyLocale("zh-CN"));
import { describe, expect, it } from "vitest";
import { previewVerifiedWorkReviewBody } from "./workReviewBody";

describe("review-body text preview", () => {
  it("preserves valid Chinese text when 64 KiB ends inside a character", () => {
    const value = "中".repeat(30_000);
    const preview = previewVerifiedWorkReviewBody(new TextEncoder().encode(value));
    expect(preview).not.toBeNull();
    expect(preview?.length).toBeGreaterThan(0);
    expect(preview?.endsWith("中")).toBe(true);
  });

  it("keeps invalid UTF-8 as verified binary", () => {
    expect(previewVerifiedWorkReviewBody(new Uint8Array([0xff]))).toBeNull();
  });
});

import { createHash, webcrypto } from "node:crypto";
import { afterEach, beforeEach, vi } from "vitest";
import { acceptScopeSession, type AccessSessionResponse } from "./api";
import { fetchVerifiedWorkReviewBody } from "./workReviewBody";
const session = {session:{subject_id:"reader",session_ref:"one",role:"workbench_reader",permissions:["workbench.details.read"]},scope_revision:"scope-one"} as AccessSessionResponse;
const bytes = new TextEncoder().encode("synthetic body");
const digest = "sha256:"+createHash("sha256").update(bytes).digest("hex");
const expiry = () => new Date(Date.now()+60_000).toISOString().replace("Z","000Z");
function response(deadline: string | null) {
 const headers: Record<string,string> = {"Content-Type":"application/octet-stream","Content-Length":String(bytes.length),"X-XInsight-Content-SHA256":digest,"X-XInsight-Scope-Revision":"scope-one"};
 if(deadline!==null) headers["X-XInsight-Content-Expires-At"]=deadline;
 return new Response(bytes,{headers});
}
beforeEach(()=>{acceptScopeSession(session);vi.stubGlobal("crypto",webcrypto);});
afterEach(()=>{acceptScopeSession(null);vi.unstubAllGlobals();vi.restoreAllMocks();vi.useRealTimers();});
it("returns only verified bytes with their independent XI deadline",async()=>{
 const deadline=expiry();vi.stubGlobal("fetch",vi.fn(async()=>response(deadline)));
 const body=await fetchVerifiedWorkReviewBody("body",digest,session,new AbortController().signal);
 expect(body.expiresAt).toBe(deadline);expect([...body.bytes]).toEqual([...bytes]);
});
it.each([null,"invalid","2026-02-30T12:00:00.000000Z","2000-01-01T00:00:00.000000Z"])("rejects missing, malformed or expired XI deadline %s",async deadline=>{
 vi.stubGlobal("fetch",vi.fn(async()=>response(deadline)));
 await expect(fetchVerifiedWorkReviewBody("body",digest,session,new AbortController().signal)).rejects.toThrow(/到期|过期/);
});
it.each([60_000,90*86400_000])("aborts a pending byte stream at the real XI deadline (%s ms)",async duration=>{
 vi.useFakeTimers();const deadline=new Date(Date.now()+duration).toISOString().replace("Z","000Z");let requested!:AbortSignal;
 vi.stubGlobal("fetch",vi.fn(async(_url,init:RequestInit)=>{
  requested=init.signal as AbortSignal;
  const stream=new ReadableStream<Uint8Array>({start(control){requested.addEventListener("abort",()=>control.error(new DOMException("Aborted","AbortError")),{once:true});}});
  const base=response(deadline);return new Response(stream,{headers:base.headers});
 }));
 const pending=fetchVerifiedWorkReviewBody("body",digest,session,new AbortController().signal);
 const rejected=expect(pending).rejects.toThrow();
 let elapsed=0;
 if(duration>2147483647){elapsed=2147483647;await vi.advanceTimersByTimeAsync(elapsed);expect(requested.aborted).toBe(false);}
 await vi.advanceTimersByTimeAsync(duration-elapsed+1);await rejected;expect(requested.aborted).toBe(true);
});
