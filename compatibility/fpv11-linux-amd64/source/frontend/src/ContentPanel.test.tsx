import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ApiRequestError, acceptScopeSession, runtimeAPI, type AccessSessionResponse } from "./api";
import { ContentPanel } from "./ContentPanel";
import { RuntimeRequestDetail } from "./RuntimePage";
import { ReferencePicker, SelectionSummary } from "./ReviewBasisView";
import type { BasisSelection } from "./assessmentTypes";
import * as content from "./content";
import type { Reading, Request } from "./runtime";
const session={session:{subject_id:"reader",session_ref:"one",role:"workbench_reader",permissions:["workbench.details.read"]},scope_revision:"scope-one"} as AccessSessionResponse;
const reading={reading_id:"reading",membership_ref:"membership"} as Reading;
const request={request_ref:"request",source_system_id:"source",source_instance:"instance",org_id:"org",project_id:"project",request_id:"request-one",observation_count:0,content_eligible_at:"2026-09-15T00:00:00Z"} as Request;
const ref:content.FrozenContentReference={source_system_id:"source",source_instance:"instance",org_id:"org",project_id:"project",request_id:"request-one",source_fact_id:"fact-one",revision:"1",source_sha256:"sha256:source",capture_status:"partial",content_ref:"body",representation:{media_type:"text/html",encoding:"identity",byte_domain:"stored_representation_before_http_content_encoding",saved_bytes:5,digest:"sha256:body"},expires_at:"2099-09-15T00:00:00.000000Z"};
const page:content.ContentPage={version:"xinsight.runtime-content.v1",reading_id:"reading",request_ref:"request",membership_ref:"membership",content_interpretation_revision:"xinsight.content-membership.v1",member_basis:"content_manifest",items:[{reference:ref,availability:{state:"available",evaluated_at:"",retryable:false},newer_revision:false}]};
beforeEach(()=>{acceptScopeSession(session);vi.spyOn(content,"fetchContentPage").mockResolvedValue(page);});
afterEach(()=>{cleanup();acceptScopeSession(null);vi.restoreAllMocks();});
it("shows verified HTML as inert text and clears sensitive state on scope invalidation",async()=>{
 const hostile='<script>alert(1)</script><img src="https://outside.invalid/track">';
 vi.spyOn(content,"fetchVerifiedContent").mockResolvedValue({reference:ref,bytes:new Uint8Array(5),text:hostile,previewOnly:false});
 const invalidated=vi.fn();const {container}=render(<ContentPanel session={session} reading={reading} request={request} onInvalidated={invalidated}/>);
 fireEvent.click(await screen.findByRole("button",{name:"读取并核验正文"}));expect(await screen.findByText(hostile)).toBeTruthy();
 expect(container.querySelector("script,img,a")).toBeNull();expect(screen.getByText(/来源只保存了部分内容/)).toBeTruthy();
 act(()=>window.dispatchEvent(new Event("xinsight:scope-changed")));
 expect(screen.queryByText(hostile)).toBeNull();expect(screen.queryByText("fact-one")).toBeNull();expect(invalidated).toHaveBeenCalledOnce();
});
it("does not expose delayed content after unmount and cancels the request",async()=>{
 let complete!:(value:content.VerifiedContent)=>void;let signal!:AbortSignal;
 vi.spyOn(content,"fetchVerifiedContent").mockImplementation((_r,_q,_f,_s,s)=>{signal=s;return new Promise(resolve=>{complete=resolve;});});
 const invalidated=vi.fn();const view=render(<ContentPanel session={session} reading={reading} request={request} onInvalidated={invalidated}/>);
 fireEvent.click(await screen.findByRole("button",{name:"读取并核验正文"}));view.unmount();expect(signal.aborted).toBe(true);
 await act(async()=>complete({reference:ref,bytes:new Uint8Array(5),text:"late-secret",previewOnly:false}));expect(screen.queryByText("late-secret")).toBeNull();
});
it("keeps content expiry local but invalidates an expired reading",async()=>{
 const invalidated=vi.fn();vi.spyOn(content,"fetchVerifiedContent").mockRejectedValueOnce(new ApiRequestError(410,{type:"urn:test",title:"expired",status:410,code:"content_expired",correlation_id:"",retryable:false})).mockRejectedValueOnce(new ApiRequestError(410));
 render(<ContentPanel session={session} reading={reading} request={request} onInvalidated={invalidated}/>);
 const button=await screen.findByRole("button",{name:"读取并核验正文"});fireEvent.click(button);await screen.findByRole("alert");expect(invalidated).not.toHaveBeenCalled();
 await waitFor(()=>expect((button as HTMLButtonElement).disabled).toBe(false));fireEvent.click(button);await waitFor(()=>expect(invalidated).toHaveBeenCalledOnce());
});
it("opens content-only requests without fetching execution observations or resources",async()=>{
 const observations=vi.spyOn(runtimeAPI,"observations"),resources=vi.spyOn(runtimeAPI,"resources");
 render(<RuntimeRequestDetail session={session} reading={reading} request={request} view="story" onViewChange={vi.fn()} onInvalidated={vi.fn()}/>);
 await screen.findByRole("button",{name:"读取并核验正文"});expect(screen.getByText(/本次阅读没有执行观察/)).toBeTruthy();expect(screen.queryByRole("button",{name:/因果图/})).toBeNull();expect(observations).not.toHaveBeenCalled();expect(resources).not.toHaveBeenCalled();
});
it("adopts a stable byte location with an explicit supporting relation",()=>{
 const c:content.ContentChoice={scope:"finding",reference:ref,location:{kind:"byte_range",offset:1,length:3},relation:"context"};
 function Picker(){const [selected,setSelected]=useState<BasisSelection>({evidence:[],gaps:[]});return <><ReferencePicker available={{evidence:[],gaps:[],contents:[c]}} selected={selected} onChange={setSelected} editableRelations/><SelectionSummary selection={selected}/></>;}
 render(<Picker/>);fireEvent.click(screen.getByRole("checkbox"));fireEvent.change(screen.getByLabelText("内容引用关系"),{target:{value:"supports"}});expect(screen.getByRole("checkbox",{name:/内容 fact-one.*支持/})).toHaveProperty("checked",true);expect(screen.getAllByText("supports")).toHaveLength(2);expect(screen.getByText("内容引用 · 支持")).toBeTruthy();expect(screen.getByText(/保存字节 1 起，长度 3/)).toBeTruthy();
});

const observed:content.ContentObservation={protocol:"openai.chat_completions.v1",observation_point:"client_request",content_item_id:"body-0",occurred_at:"2026-09-21T00:00:00.000000Z",capture_reason:"none"};
function structuredFixture(raw:string,observation:content.ContentObservation=observed,media="application/json") {
 const bytes=new TextEncoder().encode(raw),reference={...ref,capture_status:"complete" as const,representation:{...ref.representation!,media_type:media,saved_bytes:bytes.length}};
 vi.mocked(content.fetchContentPage).mockResolvedValue({...page,items:[{reference,observation,availability:{state:"available",evaluated_at:"",retryable:false},newer_revision:false}]});
 vi.spyOn(content,"fetchVerifiedContent").mockResolvedValue({reference,bytes,text:raw,previewOnly:false});return reference;
}
it("adopts the original JSON node with the same frozen reference, then clears it on scope loss",async()=>{
 const raw='{"messages":[{"role":"user","content":"structured-only-secret 中😀"}]}',reference=structuredFixture(raw);
 const review=await import("./ReviewStart");const spy=vi.spyOn(review,"ReviewStart").mockImplementation(props=><pre data-testid="chosen-reference">{JSON.stringify(props.available.contents)}</pre>);
 render(<ContentPanel session={session} reading={reading} request={request} onInvalidated={vi.fn()} navigate={vi.fn()}/>);
 fireEvent.click(await screen.findByRole("button",{name:"读取并核验正文"}));fireEvent.click(await screen.findByRole("button",{name:"采用第 1 项原文"}));
 const choices=JSON.parse(screen.getByTestId("chosen-reference").textContent!);expect(choices).toEqual([{scope:"finding",reference,location:{kind:"json_pointer",pointer:"/messages/0"},relation:"context"}]);
 expect(spy).toHaveBeenCalled();expect(screen.getByText("客户输入（改写前）")).toBeTruthy();
 act(()=>window.dispatchEvent(new Event("xinsight:scope-changed")));
 expect(screen.queryByRole("region",{name:"结构化内容"})).toBeNull();expect(screen.queryByTestId("chosen-reference")).toBeNull();
});
it("adopts the complete original SSE event range and exposes provider attempt independently",async()=>{
 const raw='data: {"object":"chat.completion.chunk","choices":[{"delta":{"content":"中😀"}}]}\r\n\r\n';
 const reference=structuredFixture(raw,{...observed,observation_point:"provider_response",attempt_id:"attempt-two"},"text/event-stream");
 const review=await import("./ReviewStart");vi.spyOn(review,"ReviewStart").mockImplementation(props=><pre data-testid="chosen-reference">{JSON.stringify(props.available.contents)}</pre>);
 render(<ContentPanel session={session} reading={reading} request={request} onInvalidated={vi.fn()} navigate={vi.fn()}/>);
 fireEvent.click(await screen.findByRole("button",{name:"读取并核验正文"}));fireEvent.click(await screen.findByRole("button",{name:"采用第 1 项原文"}));
 expect(JSON.parse(screen.getByTestId("chosen-reference").textContent!)[0]).toEqual({scope:"finding",reference,location:{kind:"byte_range",offset:0,length:new TextEncoder().encode(raw).length},relation:"context"});
 expect(screen.getByText(/来源声明的尝试身份 attempt-two/)).toBeTruthy();
});
it("clears structured content and selected nodes when replacing a request",async()=>{
 const raw='{"messages":[{"role":"user","content":"old-structured-secret"}]}';structuredFixture(raw);
 const invalidated=vi.fn(),navigate=vi.fn();const view=render(<ContentPanel session={session} reading={reading} request={request} onInvalidated={invalidated} navigate={navigate}/>);
 fireEvent.click(await screen.findByRole("button",{name:"读取并核验正文"}));fireEvent.click(await screen.findByRole("button",{name:"采用第 1 项原文"}));
 vi.mocked(content.fetchContentPage).mockReturnValue(new Promise(()=>{}));
 view.rerender(<ContentPanel session={session} reading={reading} request={{...request,request_ref:"new"}} onInvalidated={invalidated} navigate={navigate}/>);
 expect(screen.queryByText('old-structured-secret')).toBeNull();expect(screen.queryByRole("region",{name:"结构化内容"})).toBeNull();expect(screen.queryByText(/已选择的原文引用/)).toBeNull();
});
it("expiry removes structured nodes and adoption controls with the loaded body",async()=>{
 const raw='{"messages":[{"role":"user","content":"expiring-node"}]}',reference=structuredFixture(raw);
 const expiry=new Date(Date.now()+500).toISOString();reference.expires_at=expiry;
 render(<ContentPanel session={session} reading={reading} request={request} onInvalidated={vi.fn()} navigate={vi.fn()}/>);
 fireEvent.click(await screen.findByRole("button",{name:"读取并核验正文"}));expect(await screen.findByRole("button",{name:"采用第 1 项原文"})).toBeTruthy();
 await waitFor(()=>expect(screen.queryByRole("region",{name:"结构化内容"})).toBeNull());expect(screen.queryByRole("button",{name:"采用第 1 项原文"})).toBeNull();
});
it("legacy observation omission shows raw bytes without inferring the obvious JSON shape",async()=>{
 const raw='{"messages":[{"role":"user","content":"legacy-node"}]}';structuredFixture(raw);
 vi.mocked(content.fetchContentPage).mockResolvedValue({...page,items:page.items});
 render(<ContentPanel session={session} reading={reading} request={request} onInvalidated={vi.fn()} navigate={vi.fn()}/>);
 fireEvent.click(await screen.findByRole("button",{name:"读取并核验正文"}));expect(await screen.findByText(raw)).toBeTruthy();expect(screen.queryByRole("button",{name:"采用第 1 项原文"})).toBeNull();expect(screen.getByText(/未提供正式协议及观察身份/)).toBeTruthy();
});
it("renders tool arguments and results containing HTML as inert text without requests",async()=>{
 const hostile='<img src="https://outside.invalid/pixel"><script>run()</script>';
 structuredFixture(JSON.stringify({messages:[{role:"assistant",tool_calls:[{type:"function",id:"t",function:{name:"read",arguments:JSON.stringify({html:hostile})}}]},{role:"tool",tool_call_id:"t",content:hostile}]}));
 const {container}=render(<ContentPanel session={session} reading={reading} request={request} onInvalidated={vi.fn()}/>);
 fireEvent.click(await screen.findByRole("button",{name:"读取并核验正文"}));expect(await screen.findByText(hostile)).toBeTruthy();expect(container.querySelector("script,img,iframe,a")).toBeNull();expect(screen.getByText(/结果由发送方报告/)).toBeTruthy();
});

it("keeps machine identifiers in reference details and Chinese phases roles and abnormal reasons in the main view",async()=>{
 const raw='{"messages":[{"role":"user","content":"hello"}]}';structuredFixture(raw,{...observed,attempt_id:"explicit-client-attempt"});
 const view=render(<ContentPanel session={session} reading={reading} request={request} onInvalidated={vi.fn()}/>);
 fireEvent.click(await screen.findByRole("button",{name:"读取并核验正文"}));expect(await screen.findByText('1. 消息 · 用户')).toBeTruthy();
 expect(screen.getByText('客户输入（改写前）')).toBeTruthy();
 for(const node of [screen.getByText(/协议 openai.chat_completions.v1/),screen.getByText(/内容项 body-0/),screen.getByText(/来源声明的尝试身份 explicit-client-attempt/),screen.getByText(/采集原因码 none/)]) expect(node.closest('details')).not.toBeNull();
 expect(view.container.textContent).not.toContain('消息 · user');
});


it.each(["xinsight:scope-changed", "xinsight:unauthorized"])("keeps mounted content empty when a body arrives after %s",async(event)=>{
 const raw='{"messages":[{"role":"user","content":"late-revoked-secret"}]}';
 const reference=structuredFixture(raw);
 let complete!:(value:content.VerifiedContent)=>void;let signal!:AbortSignal;
 vi.mocked(content.fetchVerifiedContent).mockImplementation((_r,_q,_f,_s,s)=>{signal=s;return new Promise(resolve=>{complete=resolve;});});
 const invalidated=vi.fn();
 render(<ContentPanel session={session} reading={reading} request={request} onInvalidated={invalidated} navigate={vi.fn()}/>);
 fireEvent.click(await screen.findByRole("button",{name:"读取并核验正文"}));
 act(()=>window.dispatchEvent(new Event(event)));
 expect(signal.aborted).toBe(true);expect(invalidated).toHaveBeenCalledOnce();
 await act(async()=>complete({reference,bytes:new TextEncoder().encode(raw),text:raw,previewOnly:false}));
 expect(screen.queryByText(/late-revoked-secret/)).toBeNull();
 expect(screen.queryByRole("region",{name:"结构化内容"})).toBeNull();
 expect(screen.queryByRole("button",{name:"采用第 1 项原文"})).toBeNull();
 expect(screen.queryByRole("button",{name:"读取并核验正文"})).toBeNull();
 expect(content.fetchContentPage).toHaveBeenCalledTimes(1);
});

it("passes verified content and the original execution context through the runtime detail",async()=>{
 structuredFixture('{"messages":[{"role":"user","content":"request"}]}');
 const context={workbench_reading_id:"cost-reading",execution_id:"execution",execution_as_of:"2026-09-20T00:00:00Z",runtime_reading_id:reading.reading_id,runtime_request_ref:request.request_ref};
 const review=await import("./ReviewStart");const spy=vi.spyOn(review,"ReviewStart").mockImplementation(()=> <p>Review editor</p>);
 render(<RuntimeRequestDetail executionContext={context} session={session} reading={reading} request={request} view="contents" onViewChange={vi.fn()} onInvalidated={vi.fn()} navigate={vi.fn()}/>);
 expect(await screen.findByText("清单记录：完整保存 · 可尝试读取")).toBeTruthy();expect(screen.getByText("是否可读以本次按需读取和核验结果为准。")).toBeTruthy();expect(spy).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole("button",{name:"读取并核验正文"}));await screen.findByText("Review editor");expect(spy.mock.calls.at(-1)?.[0].executionContext).toEqual(context);
});
