import { beforeEach as beforeLegacyLocale } from "vitest";
import { setLocale as setLegacyLocale } from "./i18n";
// These existing semantic assertions explicitly exercise the Chinese translation.
beforeLegacyLocale(() => setLegacyLocale("zh-CN"));
import { act, cleanup, fireEvent, render, screen, within, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { App } from "./App";
import { WorkbenchPage } from "./WorkbenchPage";
import { acceptScopeSession, accessAPI, ApiRequestError, workbenchAPI, type AccessSessionResponse, type WorkbenchReading, type WorkbenchReadingRequest } from "./api";
import { fixedReadingRequest, navigationOwner, readWorkbenchNavigation, type WorkbenchNavigation } from "./workbenchNavigation";

const requestContributions = workbenchAPI.contributions;

vi.mock("./ExecutionDetailPage",()=>({ExecutionDetailPage:({workbenchNavigation,navigate}:any)=><div><h2>Execution destination</h2><p>{workbenchNavigation?.readingID}</p><button onClick={()=>navigate("/cases/xc_test")}>Open review</button></div>}));
vi.mock("./ProductPages",async original=>({...await original<any>(),CaseDetailPage:()=> <h2>Persisted review destination</h2>}));
const session:AccessSessionResponse={session:{subject_id:"reviewer",session_ref:"session-1",role:"workbench_investigation_reviewer",permissions:["workbench.summary.read","workbench.details.read","investigation.scoped.read","investigation.scoped.write"],created_at:"",last_seen_at:"",idle_expires_at:"",absolute_expires_at:""},scope_revision:"scope-1",csrf_token:"csrf"};
const coverage={status:"period_completeness_unknown" as const,period_completeness:"unknown" as const,projection_complete_for_members:true,known_items:2,gaps:[]};
const reading:WorkbenchReading={reading_id:"xwr_navigation",contract_version:"xinsight.workbench.v1",metric_version:"xinsight.workbench.cost.v1",period:{start_inclusive:"2026-09-01T00:00:00Z",end_exclusive:"2026-09-08T00:00:00Z",timezone:"UTC"},cutoff:"2026-09-20T00:00:00Z",expires_at:"2099-09-20T00:30:00Z",money_basis:"gateway_charged",money_title:"网关扣费",currency:"CNY",membership_ref:"members",interpretation_revision:"interpretation",authorization_revision:"authorization",dimensions:["source_project","model"],coverage};
const request:WorkbenchReadingRequest={contract_version:"xinsight.workbench.v1",page:"cost",metric_version:"xinsight.workbench.cost.v1",period:{relative_hours:168,display_timezone:"UTC"},money_basis:"gateway_charged",currency:"CNY",filters:{source_ids:["source-1"],source_projects:["project-1"],cost_projects:["cost-1"]}};
const row={ref:"contribution-1",label:"Contribution one",request_count:1,selected_amount_micros:"17",gateway_charged_micros:"17",calculated_reference_micros:"23",share:{numerator:"17",denominator:"17",unit:"micros" as const},drilldown_allowed:true};
function navigation():WorkbenchNavigation{return {version:1,owner:navigationOwner(session),readingID:reading.reading_id,request:fixedReadingRequest(reading,request),dimension:"source_project",mode:"top10",resolution:"overview",activeBucket:"",selectedRef:row.ref,contributionPages:1,detailPages:1};}
function groups(){return {reading_id:reading.reading_id,dimension:"source_project" as const,mode:"top_n" as const,rows:[row],other_known_micros:"0",unassigned_micros:"0",unassigned_scope:"reading_constant" as const,denominator_micros:"17",is_last_page:true,authorized_group_count:1};}
function detailRow(id="execution-one"){return {request_id:id,execution_id:id,evidence_event_id:id,event_hash:"sha256:event",source_system_id:"source-1",source_version:"v1",terminal_status:"settled" as const,occurred_at:reading.period.start_inclusive,gateway_charged_micros:"17",calculated_reference_micros:"23",account_state:"unknown" as const,application_state:"unknown" as const,purpose_state:"unavailable" as const,investigation_available:true};}
beforeEach(()=>{
  window.history.replaceState(null,"","/workbench/cost");acceptScopeSession(null);
  vi.spyOn(window,"scrollTo").mockImplementation(()=>{});
  vi.spyOn(accessAPI,"session").mockResolvedValue(session);
  vi.spyOn(workbenchAPI,"createReading").mockResolvedValue(reading);
  vi.spyOn(workbenchAPI,"reading").mockResolvedValue(reading);
  vi.spyOn(workbenchAPI,"summary").mockResolvedValue({reading_id:reading.reading_id,money_basis:"gateway_charged",currency:"CNY",request_count:1,request_total_complete:false,selected_amount_micros:"17",gateway_charged_micros:"17",calculated_reference_micros:"23",amount_complete:false,purpose_known_requests:0,actor_known_requests:0,coverage});
  vi.spyOn(workbenchAPI,"contributions").mockResolvedValue(groups());
  vi.spyOn(workbenchAPI,"series").mockResolvedValue({reading_id:reading.reading_id,metric:"amount",unit:"micros",bucket_seconds:3600,points:[],coverage});
  vi.spyOn(workbenchAPI,"sources").mockResolvedValue({reading_id:reading.reading_id,items:[]});
  vi.spyOn(workbenchAPI,"savedFilters").mockResolvedValue({items:[]});
  vi.spyOn(workbenchAPI,"details").mockResolvedValue({reading_id:reading.reading_id,contribution_ref:row.ref,rows:[detailRow()]});
});
afterEach(()=>{cleanup();acceptScopeSession(null);vi.restoreAllMocks();});

it("returns through execution and review, then restores after an App remount using references only",async()=>{
  let app=render(<App/>);
  fireEvent.click(await screen.findByRole("button",{name:/Contribution one/}));
  fireEvent.click(await screen.findByRole("button",{name:"查看调查证据"}));
  await screen.findByRole("heading",{name:"Execution destination"});
  expect(window.history.state.workbenchReading.readingID).toBe(reading.reading_id);
  expect(new URLSearchParams(window.location.search).get("as_of")).toBe(reading.cutoff);
  expect(new URLSearchParams(window.location.search).get("evidence")).toBe("execution-one");
  fireEvent.click(screen.getByRole("button",{name:"Open review"}));
  await screen.findByRole("heading",{name:"Persisted review destination"});
  const saved=window.history.state.workbenchReading;
  expect(Object.keys(saved).sort()).toEqual(["activeBucket","contributionPages","detailPages","dimension","mode","owner","readingID","request","resolution","selectedRef","version"].sort());
  expect(JSON.stringify(saved)).not.toMatch(/Contribution one|selected_amount|event_hash|permissions|authorization_revision/);
  app.unmount();app=render(<App/>);
  await screen.findByRole("heading",{name:"Persisted review destination"});
  fireEvent.click(screen.getByRole("link",{name:/返回.*所选贡献/}));
  await screen.findByRole("complementary",{name:"所选贡献依据"});
  expect(workbenchAPI.createReading).toHaveBeenCalledTimes(1);
  expect(workbenchAPI.reading).toHaveBeenCalledWith(reading.reading_id,expect.any(AbortSignal));
  expect(window.history.state.workbenchReading.readingID).toBe(reading.reading_id);
  expect(screen.getByLabelText("期间")).toHaveProperty("value","inherited");
  expect(workbenchAPI.details).toHaveBeenLastCalledWith(reading.reading_id,row.ref,undefined,expect.any(AbortSignal));
});

it("restores loaded contribution and detail pages, selected ref, mode and resolution",async()=>{
  const value={...navigation(),mode:"all" as const,contributionPages:2,detailPages:2,resolution:"hourly" as const};
  vi.mocked(workbenchAPI.contributions).mockImplementation(async (_id,_dimension,options)=>options?.cursor?{...groups(),mode:"full",rows:[row]}:{...groups(),mode:"full",rows:[{...row,ref:"other",label:"Other"}],next_cursor:"groups-page-2",is_last_page:false});
  vi.mocked(workbenchAPI.details).mockImplementation(async(_id,_ref,cursor)=>({reading_id:reading.reading_id,contribution_ref:row.ref,rows:[detailRow(cursor?"page-two":"page-one")],next_cursor:cursor?undefined:"details-page-2"}));
  render(<WorkbenchPage session={session} navigate={vi.fn()} navigation={value}/>);
  await screen.findByText("page-two");expect(screen.getByText("page-one")).toBeTruthy();
  expect(screen.getByLabelText("贡献模式")).toHaveProperty("value","all");
  expect(workbenchAPI.contributions).toHaveBeenLastCalledWith(reading.reading_id,"source_project",{limit:5,cursor:"groups-page-2"},expect.any(AbortSignal));
  expect(workbenchAPI.series).toHaveBeenCalledWith(reading.reading_id,expect.any(AbortSignal),3600);
  expect(workbenchAPI.createReading).not.toHaveBeenCalled();
});

it.each([403,409,410])("does not silently replace a rejected reading (%s), allows explicit new reading",async status=>{
  vi.mocked(workbenchAPI.reading).mockRejectedValue(new ApiRequestError(status));
  render(<WorkbenchPage session={session} navigate={vi.fn()} navigation={navigation()}/>);
  const retry=await screen.findByRole("button",{name:"按当前权限重新建立阅读"});
  expect(workbenchAPI.createReading).not.toHaveBeenCalled();expect(screen.queryByText("Contribution one")).toBeNull();
  expect(screen.getByText("阅读已失效或未能完成，待重新读取")).toBeTruthy();
  expect(screen.queryByText("正在读取…")).toBeNull();
  fireEvent.click(retry);await screen.findByRole("button",{name:/Contribution one/});
  expect(workbenchAPI.createReading).toHaveBeenCalledTimes(1);
  expect(workbenchAPI.createReading).toHaveBeenCalledWith(navigation().request,session.csrf_token);
});

it.each(["session","scope"])("rejects %s identity changes before loading protected data",async kind=>{
  const changed={...session,session:{...session.session,session_ref:kind==="session"?"session-2":session.session.session_ref},scope_revision:kind==="scope"?"scope-2":session.scope_revision};
  vi.mocked(accessAPI.session).mockResolvedValue(changed);
  render(<WorkbenchPage session={session} navigate={vi.fn()} navigation={navigation()}/>);
  await screen.findByRole("button",{name:"按当前权限重新建立阅读"});
  expect(workbenchAPI.contributions).not.toHaveBeenCalled();expect(workbenchAPI.createReading).not.toHaveBeenCalled();
});

it("revocation while response is pending cannot revive old values",async()=>{
  let finish!:(v:ReturnType<typeof groups>)=>void;
  vi.mocked(workbenchAPI.contributions).mockImplementation(()=>new Promise(resolve=>{finish=resolve}));
  render(<WorkbenchPage session={session} navigate={vi.fn()} navigation={navigation()}/>);
  await waitFor(()=>expect(finish).toBeTypeOf("function"));
  act(()=>window.dispatchEvent(new Event("xinsight:scope-changed")));
  await act(async()=>{finish(groups())});
  expect(screen.queryByText("Contribution one")).toBeNull();expect(screen.getByRole("button",{name:"按当前权限重新建立阅读"})).toBeTruthy();
  expect(workbenchAPI.createReading).not.toHaveBeenCalled();
});

it("removes arbitrary cached bodies from persisted navigation and rejects malformed references",()=>{
  const parsed=readWorkbenchNavigation({...navigation(),summary:{secret:"not navigation"},request:{...navigation().request,body:"not navigation"}});
  expect(JSON.stringify(parsed)).not.toContain("not navigation");
  expect(()=>readWorkbenchNavigation({...navigation(),readingID:"../../private"})).toThrow();
});

it("rejects a reading that expires during restoration without displaying its delayed data",async()=>{
  let time=1000;vi.spyOn(Date,"now").mockImplementation(()=>time);
  vi.mocked(workbenchAPI.reading).mockResolvedValue({...reading,expires_at:new Date(2000).toISOString()});
  let finish!:(v:ReturnType<typeof groups>)=>void;
  vi.mocked(workbenchAPI.contributions).mockImplementation(()=>new Promise(resolve=>{finish=resolve}));
  render(<WorkbenchPage session={session} navigate={vi.fn()} navigation={navigation()}/>);
  await waitFor(()=>expect(finish).toBeTypeOf("function"));time=3000;await act(async()=>finish(groups()));
  await screen.findByRole("button",{name:"按当前权限重新建立阅读"});
  expect(screen.queryByText("Contribution one")).toBeNull();expect(workbenchAPI.createReading).not.toHaveBeenCalled();
});
it("does not silently create a reading for malformed persisted navigation",async()=>{
  window.history.replaceState({workbenchReading:{readingID:"bad"}},"","/workbench/cost");render(<App/>);
  await screen.findByRole("button",{name:"按当前权限重新建立阅读"});expect(workbenchAPI.createReading).not.toHaveBeenCalled();
});

it("restores an authorized usage reading without requesting its unavailable purpose dimension",async()=>{
  const changed=vi.fn();window.addEventListener("xinsight:scope-changed",changed);
  const requested:string[]=[];
  vi.mocked(workbenchAPI.contributions).mockImplementation(requestContributions);
  vi.stubGlobal("fetch",vi.fn(async(input:RequestInfo|URL)=>{
    const path=String(input);requested.push(path);
    if(path.includes("dimension=purpose")) return new Response(JSON.stringify({type:"urn:test",title:"Denied",status:403,code:"workbench_scope_denied",correlation_id:"test",retryable:false}),{status:403,headers:{"Content-Type":"application/problem+json","X-XInsight-Scope-Revision":"scope-1"}});
    return new Response(JSON.stringify({...groups(),order_by:"requests"}),{status:200,headers:{"Content-Type":"application/json","X-XInsight-Scope-Revision":"scope-1"}});
  }));
  try {
    render(<WorkbenchPage session={session} navigate={vi.fn()} navigation={{...navigation(),dimension:"purpose",selectedRef:undefined}} usage/>);
    await screen.findByText("Contribution one");
    expect(requested.some(path=>path.includes("dimension=source_project"))).toBe(true);
    expect(requested.some(path=>path.includes("dimension=purpose"))).toBe(false);
    expect(changed).not.toHaveBeenCalled();
    expect(workbenchAPI.createReading).not.toHaveBeenCalled();
  }finally{window.removeEventListener("xinsight:scope-changed",changed);vi.unstubAllGlobals();}
});

it("links from overview to usage with a dimension present in its reading",async()=>{
  window.history.replaceState(null,"","/inbox");
  vi.mocked(accessAPI.session).mockResolvedValue({...session,session:{...session.session,permissions:["workbench.summary.read"]}});
  render(<App/>);
  const link=await screen.findByRole("link",{name:/使用与目的/});
  await waitFor(()=>expect(screen.getAllByText("当前读取不支持此维度").length).toBeGreaterThan(0));
  fireEvent.click(link);
  expect(window.history.state.workbenchReading).toMatchObject({readingID:reading.reading_id,dimension:"source_project"});
});

it("does not replace a selected purpose contribution when that dimension is absent",async()=>{
  render(<WorkbenchPage session={session} navigate={vi.fn()} navigation={{...navigation(),dimension:"purpose"}} usage/>);
  await screen.findByRole("button",{name:"按当前权限重新建立阅读"});
  expect(workbenchAPI.contributions).not.toHaveBeenCalled();
  expect(workbenchAPI.createReading).not.toHaveBeenCalled();
  expect(screen.queryByText("Contribution one")).toBeNull();
});


it.each([
 [undefined,"安全与复核"],
 ["/workbench/cost","费用与使用"],
 ["/workbench/cost?view=usage","使用与目的"],
 ["/workbench/runtime","运行与执行"],
 ["/executions","运行与执行"],
 ["/investigation/queue","安全与复核"],
 ["/inbox","总览"],
 ["/connectors","安全与复核"], // Source origin is not authorized for this reviewer.
 ["https://untrusted.invalid/workbench/cost","安全与复核"],
] as const)("case details retain exactly one authorized topic for origin %s",async(origin,topic)=>{
 window.history.replaceState(origin?{workbenchOrigin:origin}:null,"","/cases/xc_test");
 render(<App/>);await screen.findByRole("heading",{name:"Persisted review destination"});
 const active=within(screen.getByRole("navigation",{name:"产品功能"})).getAllByRole("link",{current:"page"});
 expect(active).toHaveLength(1);expect(active[0].textContent).toContain(topic);
 const breadcrumb=screen.getByRole("navigation",{name:"当前位置"});
 expect(breadcrumb.textContent).toBe(`工作台/${topic}/案件详情`);
 const inherited=within(breadcrumb).queryByRole("link");
 if(origin&&!["/connectors","https://untrusted.invalid/workbench/cost"].includes(origin))expect(inherited?.getAttribute("href")).toBe(origin);
 else expect(inherited).toBeNull();
});
