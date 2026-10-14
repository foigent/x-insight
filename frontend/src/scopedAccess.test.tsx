import { beforeEach as beforeLegacyLocale } from "vitest";
import { setLocale as setLegacyLocale } from "./i18n";
// These existing semantic assertions explicitly exercise the Chinese translation.
beforeLegacyLocale(() => setLegacyLocale("zh-CN"));
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { App } from "./App";
import { acceptScopeSession, accessAPI, budgetAPI, runtimeAPI, workbenchAPI, investigationAPI, type AccessSessionResponse } from "./api";
vi.mock("./InvestigationQueuePage",()=>({InvestigationQueuePage:()=> {
 const [draft,setDraft]=useState("");
 return <><p>Authorized queue contents</p><label>Review draft<input value={draft} onChange={e=>setDraft(e.target.value)}/></label></>;
}}));
vi.mock("./OverviewPage",()=>({OverviewPage:()=> <p>Scoped overview page</p>}));
vi.mock("./WorkbenchPage",()=>({WorkbenchPage:()=> <p>Scoped cost page</p>}));
const session:AccessSessionResponse={scope_revision:"scope-one",csrf_token:"test-csrf",session:{session_ref:"xr_fixture",subject_id:"xs_1111111111111111",role:"workbench_investigation_reviewer",permissions:["investigation.scoped.read","investigation.scoped.cite","investigation.scoped.write","workbench.summary.read"],created_at:"2026-09-10T00:00:00Z",last_seen_at:"2026-09-10T00:00:00Z",idle_expires_at:"2026-09-10T01:00:00Z",absolute_expires_at:"2026-09-11T00:00:00Z"}};
afterEach(()=>{cleanup();acceptScopeSession(null);vi.restoreAllMocks();vi.unstubAllGlobals();window.history.replaceState(null,"","/");});
describe("scoped authorization recovery",()=>{
 it.each<[string,AccessSessionResponse]>([
  ["scope",{...session,scope_revision:"scope-two"}],
  ["subject",{...session,session:{...session.session,subject_id:"xs_2222222222222222"}}],
  ["session",{...session,session:{...session.session,session_ref:"xr_replacement"}}],
  ["role",{...session,session:{...session.session,role:"workbench_reader"}}],
  ["permissions",{...session,session:{...session.session,permissions:["investigation.scoped.read"]}}],
 ])("discards an old response when the %s changes",async(_label,current)=>{
  acceptScopeSession(session);let resolve!:(value:Response)=>void;
  const fetchMock=vi.fn((_path:RequestInfo|URL,_options?:RequestInit)=>new Promise<Response>(r=>{resolve=r}));vi.stubGlobal("fetch",fetchMock);
  const pending=investigationAPI.getExecution("xe1_fixture");
  const rejected=expect(pending).rejects.toMatchObject({name:"AbortError"});
  expect(fetchMock.mock.calls[0][1]).toMatchObject({headers:{"X-XInsight-Scope-Revision":"scope-one"}});
  acceptScopeSession(current);
  resolve(new Response(JSON.stringify({summary:{execution_id:"private-old"}}),{status:200,headers:{"Content-Type":"application/json","X-XInsight-Scope-Revision":"scope-one"}}));
  await rejected;
 });
 it("notifies the shell of grant denial without delivering the denied payload",async()=>{
  acceptScopeSession(session);const changed=vi.fn();window.addEventListener("xinsight:scope-changed",changed);
  vi.stubGlobal("fetch",vi.fn().mockResolvedValue(new Response(JSON.stringify({type:"urn:xinsight:error:scope_denied",title:"Denied",status:403,code:"scope_denied",correlation_id:"fixture",retryable:false}),{status:403,headers:{"Content-Type":"application/problem+json"}})));
  await expect(investigationAPI.getExecution("xe1_fixture")).rejects.toMatchObject({status:403});
  expect(changed).toHaveBeenCalledOnce();window.removeEventListener("xinsight:scope-changed",changed);
 });
 it.each([
  ["no_authorized_scope","no_authorized_scope","workbench_scope_denied",false],
  ["scope-one","scope-one","workbench_scope_denied",false],
  ["scope-one","scope-two","workbench_scope_denied",true],
  ["scope-one",undefined,"workbench_scope_denied",true],
  ["scope-one","scope-one","permission_denied",true],
 ] as const)("settles only a current budget denial (%s, %s, %s)",async(current,returned,code,refresh)=>{
  acceptScopeSession({...session,scope_revision:current});
  const changed=vi.fn();window.addEventListener("xinsight:scope-changed",changed);
  try {
   const headers:Record<string,string>={"Content-Type":"application/problem+json"};
   if(returned!==undefined)headers["X-XInsight-Scope-Revision"]=returned;
   vi.stubGlobal("fetch",vi.fn().mockResolvedValue(new Response(JSON.stringify({type:"urn:test:denied",title:"Denied",status:403,code,correlation_id:"fixture",retryable:false}),{status:403,headers})));
   await expect(budgetAPI.createReading({contract_version:"xinsight.budget-reading.v1",filters:{source_ids:[],source_projects:[],cost_projects:[]}},session.csrf_token)).rejects.toMatchObject({status:403});
   expect(changed).toHaveBeenCalledTimes(refresh?1:0);
  } finally {window.removeEventListener("xinsight:scope-changed",changed);}
 });
 it.each(["focus","pageshow"])("preserves the mounted draft across unchanged %s revalidation",async event=>{
  window.history.replaceState(null,"","/investigation/queue");
  const sessions=vi.spyOn(accessAPI,"session").mockResolvedValue(session);
  render(<App/>);await screen.findByText("Authorized queue contents");
  await waitFor(()=>expect(sessions).toHaveBeenCalledTimes(2));
  const input=screen.getByLabelText("Review draft");
  fireEvent.change(input,{target:{value:"Unsaved review judgment"}});
  let resolve!:(value:AccessSessionResponse)=>void;
  sessions.mockImplementationOnce(()=>new Promise(r=>{resolve=r}));
  fireEvent(window,new Event(event));
  await waitFor(()=>expect(sessions).toHaveBeenCalledTimes(3));
  expect(screen.getByLabelText("Review draft")).toBe(input);
  expect((input as HTMLInputElement).value).toBe("Unsaved review judgment");
  await act(async()=>resolve({...session,session:{...session.session,last_seen_at:"2026-09-10T00:05:00Z",permissions:[...session.session.permissions].reverse()}}));
  expect(screen.getByLabelText("Review draft")).toBe(input);
  expect((input as HTMLInputElement).value).toBe("Unsaved review judgment");
 });
 it("offers supported pages and removes old results after a verified role change",async()=>{
  window.history.replaceState(null,"","/investigation/queue");
  const sessions=vi.spyOn(accessAPI,"session").mockResolvedValue(session);
  render(<App/>);await screen.findByText("Authorized queue contents");
  // The initial scoped refresh starts after the shell mounts its listeners.
  await waitFor(()=>expect(sessions).toHaveBeenCalledTimes(2));
  expect(screen.queryByRole("link",{name:/运行态势/})).toBeNull();
  expect(document.querySelector('a[href="/sessions"]')).toBeNull();
  expect(document.querySelector('a[href="/discovery"]')).toBeNull();
  let resolve!:(value:AccessSessionResponse)=>void;
  sessions.mockImplementationOnce(()=>new Promise(r=>{resolve=r}));
  fireEvent(window,new Event("focus"));
  await waitFor(()=>expect(sessions).toHaveBeenCalledTimes(3));
  expect(screen.getByText("Authorized queue contents")).not.toBeNull();
  await act(async()=>resolve({...session,scope_revision:"scope-two",session:{...session.session,role:"workbench_reader",permissions:["workbench.summary.read"]}}));
  await screen.findByText("Scoped overview page");
  expect(screen.queryByText("Authorized queue contents")).toBeNull();
  expect(window.location.pathname).toBe("/inbox");
 });
 it.each(["scope","session"])("clears a draft when the %s changes even if the route remains allowed",async kind=>{
  window.history.replaceState(null,"","/investigation/queue");
  const sessions=vi.spyOn(accessAPI,"session").mockResolvedValue(session);
  render(<App/>);await screen.findByText("Authorized queue contents");
  await waitFor(()=>expect(sessions).toHaveBeenCalledTimes(2));
  fireEvent.change(screen.getByLabelText("Review draft"),{target:{value:"Private old draft"}});
  const current=kind==="scope"?{...session,scope_revision:"scope-two"}:{...session,session:{...session.session,session_ref:"xr_replacement"}};
  sessions.mockResolvedValue(current);
  fireEvent(window,new Event("focus"));
  await waitFor(()=>expect((screen.getByLabelText("Review draft") as HTMLInputElement).value).toBe(""));
 });
 it.each(["visible","hidden"] as const)("closes denied content while %s and ignores a late normal revalidation",async visibility=>{
  window.history.replaceState(null,"","/investigation/queue");
  const sessions=vi.spyOn(accessAPI,"session").mockResolvedValue(session);
  render(<App/>);await screen.findByText("Authorized queue contents");
  await waitFor(()=>expect(sessions).toHaveBeenCalledTimes(2));
  let resolve!:(value:AccessSessionResponse)=>void;
  sessions.mockImplementationOnce(()=>new Promise(r=>{resolve=r}));
  fireEvent(window,new Event("focus"));
  await waitFor(()=>expect(sessions).toHaveBeenCalledTimes(3));
  vi.spyOn(document,"visibilityState","get").mockReturnValue(visibility);
  sessions.mockResolvedValue({...session,scope_revision:"no_authorized_scope"});
  fireEvent(window,new Event("xinsight:scope-changed"));
  expect(screen.queryByText("Authorized queue contents")).toBeNull();
  await screen.findByText("当前没有可访问的资料范围");
  await act(async()=>resolve(session));
  expect(screen.getByText("当前没有可访问的资料范围")).not.toBeNull();
  expect(screen.queryByText("Authorized queue contents")).toBeNull();
 });
 it("keeps hidden denied content closed during pending and failed Session recovery without a retry loop",async()=>{
  window.history.replaceState(null,"","/investigation/queue");
  const sessions=vi.spyOn(accessAPI,"session").mockResolvedValue(session);
  render(<App/>);await screen.findByText("Authorized queue contents");
  await waitFor(()=>expect(sessions).toHaveBeenCalledTimes(2));
  fireEvent.change(screen.getByLabelText("Review draft"),{target:{value:"Restricted draft"}});
  vi.spyOn(document,"visibilityState","get").mockReturnValue("hidden");
  let reject!:(reason:Error)=>void;
  sessions.mockImplementationOnce(()=>new Promise((_resolve,r)=>{reject=r}));
  vi.stubGlobal("fetch",vi.fn().mockResolvedValue(new Response(JSON.stringify({status:403,code:"scope_denied"}),{status:403,headers:{"Content-Type":"application/problem+json"}})));
  await act(async()=>{await expect(investigationAPI.getExecution("xe1_fixture")).rejects.toMatchObject({status:403});});
  expect(sessions).toHaveBeenCalledTimes(3);
  expect(screen.queryByText("Authorized queue contents")).toBeNull();
  expect(screen.queryByLabelText("Review draft")).toBeNull();
  expect(screen.getByText("正在验证登录状态…")).not.toBeNull();
  // Visibility alone never reopens the mounted content while recovery is pending.
  fireEvent(window,new Event("focus"));
  expect(sessions).toHaveBeenCalledTimes(3);
  await act(async()=>reject(new Error("Session temporarily unavailable")));
  expect(screen.getByText("暂时无法验证登录状态")).not.toBeNull();
  expect(screen.queryByText("Authorized queue contents")).toBeNull();
  expect(sessions).toHaveBeenCalledTimes(3);
 });
});
it("keeps a fully revoked session usable without issuing a business reload loop",async()=>{
 window.history.replaceState(null,"","/investigation/queue");
 vi.spyOn(accessAPI,"session").mockResolvedValue({...session,scope_revision:"no_authorized_scope"});
 render(<App/>);await screen.findByText("当前没有可访问的资料范围");
 expect(screen.queryByText("Authorized queue contents")).toBeNull();
 expect(screen.queryByText("Scoped cost page")).toBeNull();
});


it.each([
 ["scope-one","workbench_scope_denied",false],
 ["scope-two","workbench_scope_denied",true],
 [undefined,"workbench_scope_denied",true],
 ["scope-one","permission_denied",true],
 ["scope-one","scope_denied",true],
] as const)("runtime qualification denial stays local only with proven current scope (%s/%s)",async(revision,code,invalidated)=>{
 acceptScopeSession(session);
 const changed=vi.fn();window.addEventListener("xinsight:scope-changed",changed);
 try {
  const headers:Record<string,string>={"Content-Type":"application/problem+json"};
  if(revision)headers["X-XInsight-Scope-Revision"]=revision;
  vi.stubGlobal("fetch",vi.fn().mockResolvedValue(new Response(JSON.stringify({type:"about:blank",title:"Denied",status:403,code,correlation_id:"fixture",retryable:false}),{status:403,headers})));
  await expect(runtimeAPI.createReading({contract_version:"xinsight.runtime-reading.v1",metric_version:"xinsight.runtime.request-observations.v1",period:{relative_hours:24,display_timezone:"UTC"},filters:{source_ids:[],source_projects:[],cost_projects:[]}},session.csrf_token)).rejects.toMatchObject({status:403});
  expect(changed).toHaveBeenCalledTimes(invalidated?1:0);
 }finally{window.removeEventListener("xinsight:scope-changed",changed);}
});


it.each([
 ["create",403,"scope-one","workbench_scope_denied",false,false],
 ["create",403,"scope-two","workbench_scope_denied",true,false],
 ["create",403,undefined,"workbench_scope_denied",true,false],
 ["create",403,"scope-one","permission_denied",true,false],
 ["create",403,"scope-one","csrf_validation_failed",true,false],
 ["create",403,"scope-one","scope_denied",true,false],
 ["create",409,"scope-one","scope_revision_changed",true,false],
 ["create",401,"scope-one","authentication_required",false,true],
 ["reading",403,"scope-one","workbench_scope_denied",true,false],
 ["summary",403,"scope-one","workbench_scope_denied",true,false],
] as const)("cost qualification denial is bounded to creation (%s/%s/%s/%s)",async(operation,status,revision,code,invalidated,unauthenticated)=>{
 acceptScopeSession(session);
 const changed=vi.fn(), unauthorized=vi.fn();
 window.addEventListener("xinsight:scope-changed",changed);window.addEventListener("xinsight:unauthorized",unauthorized);
 try {
  const headers:Record<string,string>={"Content-Type":"application/problem+json"};
  if(revision)headers["X-XInsight-Scope-Revision"]=revision;
  const fetch=vi.fn().mockResolvedValue(new Response(JSON.stringify({type:"about:blank",title:"Denied",status,code,correlation_id:"fixture",retryable:false}),{status,headers}));
  vi.stubGlobal("fetch",fetch);
  const pending=operation==="create"?workbenchAPI.createReading({contract_version:"xinsight.workbench.v1",page:"cost",metric_version:"xinsight.workbench.cost.v1",period:{relative_hours:24,display_timezone:"UTC"},money_basis:"gateway_charged",currency:"CNY",filters:{source_ids:[],source_projects:[],cost_projects:[]}},session.csrf_token):operation==="reading"?workbenchAPI.reading("reading-a"):workbenchAPI.summary("reading-a");
  await expect(pending).rejects.toMatchObject({status});
  expect(fetch.mock.calls[0][1].method).toBe(operation==="create"?"POST":"GET");
  expect(changed).toHaveBeenCalledTimes(invalidated?1:0);
  expect(unauthorized).toHaveBeenCalledTimes(unauthenticated?1:0);
 }finally{window.removeEventListener("xinsight:scope-changed",changed);window.removeEventListener("xinsight:unauthorized",unauthorized);}
});

it.each(["scope-one",undefined,"scope-two"])("keeps only same-revision preference denial local (%s)",async revision=>{
 acceptScopeSession(session);const changed=vi.fn();window.addEventListener("xinsight:scope-changed",changed);
 try{const headers:Record<string,string>={"Content-Type":"application/problem+json"};if(revision)headers["X-XInsight-Scope-Revision"]=revision;
 vi.stubGlobal("fetch",vi.fn().mockResolvedValue(new Response(JSON.stringify({type:"urn:test",title:"Denied",status:403,code:"workbench_scope_denied",correlation_id:"test",retryable:false}),{status:403,headers})));
 await expect(workbenchAPI.savedFilters()).rejects.toMatchObject({status:403});expect(changed).toHaveBeenCalledTimes(revision==="scope-one"?0:1);
 }finally{window.removeEventListener("xinsight:scope-changed",changed);}
});
it("stops same-scope denied pages from remounting even on subsequent focus checks",async()=>{
 window.history.replaceState(null,"","/investigation/queue");const sessions=vi.spyOn(accessAPI,"session").mockResolvedValue(session);render(<App/>);await screen.findByText("Authorized queue contents");await waitFor(()=>expect(sessions).toHaveBeenCalledTimes(2));
 fireEvent(window,new Event("xinsight:scope-changed"));expect(screen.queryByText("Authorized queue contents")).toBeNull();await screen.findByText("当前操作被拒绝");
 for(const event of ["focus","pageshow"]){fireEvent(window,new Event(event));await act(async()=>{});expect(screen.queryByText("Authorized queue contents")).toBeNull();expect(screen.getByText("当前操作被拒绝")).toBeTruthy();}
 fireEvent.click(screen.getByRole("button",{name:"返回总览"}));await screen.findByText("Scoped overview page");
});
