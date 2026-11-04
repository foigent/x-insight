import {act,cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import {afterEach,beforeEach,describe,expect,it,vi} from "vitest";
import {App} from "./App";
import {accessAPI,acceptScopeSession,ApiRequestError,investigationAPI,investigationQueueAPI,managedAccountsAPI,runtimeAPI,type AccessSessionResponse} from "./api";
import type * as Runtime from "./runtime";
import {sessionChangeKey} from "./sessionChange";
import {setLocale,t} from "./i18n";

const owner:AccessSessionResponse={scope_revision:"scope-home",csrf_token:"synthetic",reading_capabilities:{summary:true,runtime:true,coverage:true,work_activity:true},session:{session_ref:"xr_home",subject_id:"xs_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",username:"owner",role:"platform_admin",permissions:["accounts.manage","evidence.read","workbench.summary.read","workbench.details.read","workbench.coverage.read"],created_at:"2026-11-04T00:00:00Z",last_seen_at:"2026-11-04T00:00:00Z",idle_expires_at:"2026-11-04T00:30:00Z",absolute_expires_at:"2026-11-04T12:00:00Z"}};
const reader:AccessSessionResponse={...owner,reading_capabilities:{summary:false,runtime:true,coverage:true,work_activity:false},session:{...owner.session,username:"reader",subject_id:"xs_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",role:"workbench_investigation_reader",permissions:["investigation.scoped.read","workbench.summary.read","workbench.details.read","workbench.coverage.read"]}};
function reading():Runtime.Reading {const now=new Date().toISOString();return{reading_id:"xrt_home",contract_version:"xinsight.runtime-reading.v1",metric_version:"xinsight.runtime.request-observations.v1",period:{start_inclusive:now,end_exclusive:now,timezone:"UTC"},cutoff:now,expires_at:new Date(Date.now()+1800000).toISOString(),authorization_revision:"sha256:auth",interpretation_revision:"sha256:rule",membership_ref:"sha256:membership",requests_with_entry_time:4,requests_with_unknown_entry_time:0,observation_count:5,coverage:{period_completeness:"unknown",timeline_complete:false,time_basis:"trusted_request_started",unknown_entry_time_basis:"authorized_observation_in_period",missing_predecessors:0,streams:[],gaps:[]}};}
function response(value:unknown,status=200){return new Response(JSON.stringify(value),{status,headers:{"Content-Type":"application/json","X-XInsight-Scope-Revision":"scope-home"}});}
function denied(){return response({type:"urn:test:scope",title:"Not permitted for this field",status:403,code:"workbench_scope_denied",correlation_id:"synthetic",retryable:false},403);}
function setup(current:AccessSessionResponse) {
 const sessions=vi.spyOn(accessAPI,"session").mockResolvedValue(current);
 vi.spyOn(accessAPI,"providerCapabilities").mockRejectedValue(new Error("No external provider lookup in this fixture"));
 let frozen=reading();const make=vi.spyOn(runtimeAPI,"createReading").mockImplementation(async input=>{const now=Date.now();frozen={...reading(),period:{start_inclusive:input.period.start_inclusive??new Date(now-(input.period.relative_hours??24)*3600000).toISOString(),end_exclusive:input.period.end_exclusive??new Date(now).toISOString(),timezone:input.period.display_timezone}};return frozen;});vi.spyOn(runtimeAPI,"reading").mockImplementation(async()=>frozen);
 vi.spyOn(runtimeAPI,"summary").mockImplementation(async(id,entry)=>({reading_id:id,summary_version:"v1",interpretation_revision:"sha256:rule",entry_time:entry,requests:4,outcomes:[{outcome:"local_delivery_completed",requests:3},{outcome:"technical_failure",requests:1}],diagnostic_failure:{state:"computed",numerator:1,denominator:4,excluded_requests:0,basis:"diagnostic"},coverage:reading().coverage}));
 vi.spyOn(runtimeAPI,"dimensions").mockImplementation(async(id,dimension,entry)=>({reading_id:id,dimensions_version:"v1",interpretation_revision:"sha256:rule",dimension,entry_time:entry,requests:4,requests_with_values:0,requests_without_values:4,missing_predecessors:0,groups_may_overlap:true,items:[]}));
 vi.spyOn(runtimeAPI,"measurements").mockImplementation(async id=>({reading_id:id,eligibility_version:"v1",interpretation_revision:"sha256:rule",items:[]}));
 vi.spyOn(runtimeAPI,"requests").mockImplementation(async id=>({reading_id:id,items:[]}));
 vi.spyOn(managedAccountsAPI,"list").mockResolvedValue({items:[{subject_id:owner.session.subject_id,username:"owner",role:"platform_admin",disabled:false,needs_activation:false,revision:1}]});
 vi.spyOn(managedAccountsAPI,"grantOptions").mockResolvedValue({sources:[],projects:[],grants:[]});
 vi.spyOn(investigationQueueAPI,"list").mockResolvedValue({items:[],total:0,evaluated_at:new Date().toISOString(),version:"v1",filters:{mode:"current",period:"24h",sort:"created_desc"},coverage:"authorized",statistics:{current:0,created:0,reassessment:0,unassigned:0,overdue:0,basis_unrecorded:0,basis_changed:0,basis_unavailable:0}});
 const fetcher=vi.fn(async(path:RequestInfo|URL)=>{const url=String(path);if(url.startsWith("/api/v1/work-activity/scopes"))return response({version:"xinsight.work-activity-scopes.v1",items:[]});if(url.startsWith("/api/v1/work-activity/summary")||url==="/api/v1/workbench/readings")return denied();throw new Error("Unexpected synthetic HTTP path: "+url);});vi.stubGlobal("fetch",fetcher);
 return{sessions,make,fetcher,frozen:()=>frozen};
}
beforeEach(()=>{setLocale("en-US");window.history.replaceState(null,"","/inbox");});
afterEach(()=>{cleanup();acceptScopeSession(null);vi.restoreAllMocks();vi.unstubAllGlobals();window.history.replaceState(null,"","/");});

// All product pages are real components. Only their data sources are isolated;
// typed local HTTP denials still run through the real API invalidation logic.
describe("normal entry and partial authorization journey",()=>{
 it("keeps the actual overview and navigation usable after local economic/activity denials",async()=>{
  const {sessions,fetcher,frozen}=setup(owner);render(<App/>);
  await screen.findByText("3 / 4");await waitFor(()=>expect(fetcher.mock.calls.some(([path])=>String(path).startsWith("/api/v1/work-activity/summary"))).toBe(true));
  expect(screen.getByRole("heading",{name:t("m0121")})).toBeTruthy();expect(screen.getByLabelText("Current account and role").textContent).toContain("owner");expect(sessions).toHaveBeenCalledTimes(2);
  fireEvent.click(document.querySelector('a[href="/workbench/runtime"]')!);expect(frozen().period).toMatchObject({start_inclusive:window.history.state.runtimeReading.request.period.start_inclusive,end_exclusive:window.history.state.runtimeReading.request.period.end_exclusive,timezone:window.history.state.runtimeReading.request.period.display_timezone});await screen.findByRole("heading",{name:t("m0155")});await screen.findByText("1 / 4");expect(window.location.pathname).toBe("/workbench/runtime");
  fireEvent.click(document.querySelector('a[href="/access/accounts"]')!);await screen.findByRole("heading",{name:"Accounts"});await screen.findByText("Existing accounts");
  // Navigation legitimately rechecks the session. Once the destination has
  // settled, no denial may start an autonomous remount/revalidation loop.
  const settled=sessions.mock.calls.length;await new Promise(resolve=>setTimeout(resolve,100));expect(sessions).toHaveBeenCalledTimes(settled);
 });
 it("opens the permitted runtime directly for a metadata-only reader without asking for summary/activity",async()=>{
  const {fetcher}=setup(reader);render(<App/>);await screen.findByRole("heading",{name:t("m0155")});await screen.findByText("1 / 4");
  expect(window.location.pathname).toBe("/workbench/runtime");expect(screen.getByLabelText("Current account and role").textContent).toContain("reader");
  expect(fetcher).not.toHaveBeenCalled();expect(document.querySelector('a[href="/work/activity"]')).toBeNull();expect(document.querySelector('a[href="/workbench/cost"]')).toBeNull();expect(document.querySelector('a[href="/access/accounts"]')).toBeNull();
  act(()=>setLocale("zh-CN"));expect(screen.getByLabelText("当前账号与角色").textContent).toContain("reader");expect(screen.getByRole("heading",{name:t("m0155")})).toBeTruthy();
 });
 it("closes real runtime immediately on cross-tab revalidation and discards late old data before the new session arrives",async()=>{
  const {sessions,fetcher}=setup(reader);render(<App/>);await screen.findByText("1 / 4");await waitFor(()=>expect(sessions).toHaveBeenCalledTimes(2));
  let resolveSession!:(value:AccessSessionResponse)=>void;sessions.mockImplementationOnce(()=>new Promise(resolve=>{resolveSession=resolve;}));
  let resolveOld!:(value:Response)=>void;fetcher.mockImplementationOnce(()=>new Promise<Response>(resolve=>{resolveOld=resolve;}));const old=investigationAPI.getExecution("xe1_old");const rejected=expect(old).rejects.toMatchObject({name:"AbortError"});
  fireEvent(window,new StorageEvent("storage",{key:sessionChangeKey,newValue:"synthetic-notification"}));expect(screen.queryByText("1 / 4")).toBeNull();const pendingHistory=JSON.stringify(window.history.state);
  resolveOld(response({summary:{execution_id:"late-old-private"}}));await rejected;expect(JSON.stringify(window.history.state)).toBe(pendingHistory);expect(JSON.stringify(window.history.state)).not.toContain("late-old-private");
  const revoked={...reader,scope_revision:"no_authorized_scope",reading_capabilities:{summary:false,runtime:false,coverage:false,work_activity:false}};sessions.mockResolvedValue(revoked);await act(async()=>resolveSession(revoked));await screen.findByText("No accessible scope");expect(screen.queryByText("1 / 4")).toBeNull();expect(window.history.state).toBeNull();
 });
 it("clears an expired real page after the authoritative session rejects it",async()=>{
  const {sessions}=setup(reader);render(<App/>);await screen.findByText("1 / 4");await waitFor(()=>expect(sessions).toHaveBeenCalledTimes(2));sessions.mockRejectedValueOnce(new ApiRequestError(401));fireEvent(window,new Event("focus"));await screen.findByRole("heading",{name:t("m0007")});expect(screen.queryByText("1 / 4")).toBeNull();expect(screen.queryByLabelText("Current account and role")).toBeNull();
 });
});
