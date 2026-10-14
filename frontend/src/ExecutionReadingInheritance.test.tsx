import { beforeEach as beforeLegacyLocale } from "vitest";
import { setLocale as setLegacyLocale } from "./i18n";
// These existing semantic assertions explicitly exercise the Chinese translation.
beforeLegacyLocale(() => setLegacyLocale("zh-CN"));
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { assessmentAPI, workspaceAPI, accessAPI, workbenchAPI, runtimeAPI, ApiRequestError, type AccessSessionResponse, type ExecutionSummary, type WorkbenchReading } from "./api";
import { ExecutionRuntimePanel, type RuntimeDetailContext } from "./ExecutionRuntimePanel";
import { navigationOwner, type WorkbenchNavigation } from "./workbenchNavigation";
import type * as Runtime from "./runtime";
import * as content from "./content";
const session:AccessSessionResponse={session:{subject_id:"reviewer",session_ref:"session",role:"workbench_reader",permissions:["workbench.details.read","investigation.scoped.write"],created_at:"",last_seen_at:"",idle_expires_at:"",absolute_expires_at:""},csrf_token:"csrf",scope_revision:"scope"};
const summary={execution_id:"execution-one",source_system_id:"source-one",request_id:"request-one"} as ExecutionSummary;
const period={start_inclusive:"2026-09-01T00:00:00Z",end_exclusive:"2026-09-08T00:00:00Z",display_timezone:"UTC"};
const origin:WorkbenchNavigation={version:1,owner:navigationOwner(session),readingID:"xwr_cost",request:{contract_version:"xinsight.workbench.v1",page:"cost",metric_version:"xinsight.workbench.cost.v1",money_basis:"gateway_charged",currency:"CNY",period,filters:{source_ids:["source-one","source-two"],source_projects:["project-9"],cost_projects:["cost-8"]}},dimension:"purpose",mode:"top10",resolution:"overview",activeBucket:"",contributionPages:1,detailPages:1};
const cost:WorkbenchReading={reading_id:"xwr_cost",contract_version:"xinsight.workbench.v1",metric_version:"xinsight.workbench.cost.v1",money_basis:"gateway_charged",currency:"CNY",period:{...period,timezone:"UTC"},expires_at:"2099-01-01T00:00:00Z",cutoff:"2026-09-20T00:00:00Z",money_title:"网关扣费",membership_ref:"members",interpretation_revision:"interpretation",authorization_revision:"auth",dimensions:["source_project"],coverage:{status:"period_completeness_unknown",period_completeness:"unknown",projection_complete_for_members:true,known_items:1,gaps:[]}};
const request:Runtime.Request={request_ref:"request-ref",source_system_id:"source-one",source_instance:"instance",org_id:"org",project_id:"project",request_id:"request-one",entry_time_state:"unknown",last_observed_at:period.end_exclusive,observation_count:1};
function reading():Runtime.Reading{const now=Date.now();return {reading_id:"xrt_related",contract_version:"xinsight.runtime-reading.v1",metric_version:"xinsight.runtime.request-observations.v1",period:{...period,timezone:"UTC"},cutoff:new Date(now).toISOString(),expires_at:new Date(now+1800000).toISOString(),membership_ref:"members",authorization_revision:"auth",interpretation_revision:"interpretation",requests_with_entry_time:1,requests_with_unknown_entry_time:0,observation_count:1,coverage:{period_completeness:"unknown",timeline_complete:false,time_basis:"trusted_request_started",unknown_entry_time_basis:"authorized_observation_in_period",missing_predecessors:0,gaps:[],streams:[]}};}
function Wrapper({inherited=origin,view="systems"}:{inherited?:WorkbenchNavigation;view?:"systems"|"contents"}){const [context,setContext]=useState<RuntimeDetailContext|null>(null);return <ExecutionRuntimePanel view={view} executionAsOf={cost.cutoff} navigate={()=>{}} inherited={inherited} summary={summary} session={session} context={context} setContext={setContext}/>;}
beforeEach(()=>{
  window.history.replaceState({workbenchReading:origin},"","/executions/execution-one");
  vi.spyOn(accessAPI,"session").mockResolvedValue(session);vi.spyOn(workbenchAPI,"reading").mockResolvedValue(cost);
  vi.spyOn(runtimeAPI,"createReading").mockResolvedValue(reading());vi.spyOn(runtimeAPI,"reading").mockResolvedValue(reading());
  vi.spyOn(runtimeAPI,"executionRequest").mockResolvedValue({version:"xinsight.runtime.execution-request.v1",reading_id:"xrt_related",membership_ref:"members",execution_id:"execution-one",request});
  vi.spyOn(runtimeAPI,"resources").mockResolvedValue({resources_version:"xinsight.runtime.resources.v1",reading_id:"xrt_related",request_ref:"request-ref",membership_ref:"members",coverage:"synthetic_internal_records_only",items:[]});
  vi.spyOn(runtimeAPI,"observations").mockResolvedValue({reading_id:"xrt_related",items:[]});
});
afterEach(()=>{cleanup();vi.restoreAllMocks();});
it("inherits exact absolute period and both project filters, while narrowing to this execution source",async()=>{
  render(<Wrapper/>);expect(runtimeAPI.createReading).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button",{name:"读取相关运行与资源"}));await screen.findByText(/本次阅读没有可展示的资源记录/);
  expect(runtimeAPI.createReading).toHaveBeenCalledWith({contract_version:"xinsight.runtime-reading.v1",metric_version:"xinsight.runtime.request-observations.v1",period,filters:{source_ids:["source-one"],source_projects:["project-9"],cost_projects:["cost-8"]}},"csrf",expect.any(AbortSignal));
  expect(window.history.state.workbenchReading.related).toEqual({executionID:"execution-one",readingID:"xrt_related"});
  expect(screen.getByText(/费用贡献的模型、用途等维度不作为运行过滤/)).toBeTruthy();
});
it("explicitly changes period without dropping inherited project restrictions",async()=>{
  render(<Wrapper/>);fireEvent.change(screen.getByLabelText("相关过程期间"),{target:{value:"720"}});fireEvent.click(screen.getByRole("button",{name:"读取相关运行与资源"}));
  await screen.findByText(/本次阅读没有可展示的资源记录/);
  expect(runtimeAPI.createReading).toHaveBeenCalledWith(expect.objectContaining({period:expect.objectContaining({relative_hours:720}),filters:{source_ids:["source-one"],source_projects:["project-9"],cost_projects:["cost-8"]}}),"csrf",expect.any(AbortSignal));
});
it("restores the related reading on refresh instead of silently creating another",async()=>{
  render(<Wrapper inherited={{...origin,related:{executionID:"execution-one",readingID:"xrt_related"}}}/>);
  await screen.findByText(/本次阅读没有可展示的资源记录/);expect(runtimeAPI.createReading).not.toHaveBeenCalled();
  expect(workbenchAPI.reading).toHaveBeenCalledWith("xwr_cost",expect.any(AbortSignal));expect(runtimeAPI.reading).toHaveBeenCalledWith("xrt_related",expect.any(AbortSignal));
});
it.each([403,409,410])("never falls back to a wider read when origin authorization fails (%s)",async status=>{
  vi.mocked(workbenchAPI.reading).mockRejectedValue(new ApiRequestError(status));render(<Wrapper/>);
  fireEvent.click(screen.getByRole("button",{name:"读取相关运行与资源"}));await screen.findByRole("alert");
  expect(runtimeAPI.createReading).not.toHaveBeenCalled();expect(runtimeAPI.resources).not.toHaveBeenCalled();
});
it("revocation cancels a delayed create response before it can restore resource content",async()=>{
  let finish!:(v:Runtime.Reading)=>void;vi.mocked(runtimeAPI.createReading).mockImplementation(()=>new Promise(resolve=>{finish=resolve}));
  render(<Wrapper/>);fireEvent.click(screen.getByRole("button",{name:"读取相关运行与资源"}));await waitFor(()=>expect(finish).toBeTypeOf("function"));
  act(()=>window.dispatchEvent(new Event("xinsight:scope-changed")));await act(async()=>finish(reading()));
  expect(runtimeAPI.executionRequest).not.toHaveBeenCalled();expect(runtimeAPI.resources).not.toHaveBeenCalled();expect(screen.getByRole("alert").textContent).toContain("阅读已失效");
});

it("keeps expired related-reading recovery failed without replacing it",async()=>{
  vi.mocked(runtimeAPI.reading).mockRejectedValue(new ApiRequestError(410));
  render(<Wrapper inherited={{...origin,related:{executionID:"execution-one",readingID:"xrt_related"}}}/>);
  await screen.findByRole("alert");expect(runtimeAPI.createReading).not.toHaveBeenCalled();expect(runtimeAPI.resources).not.toHaveBeenCalled();
});
it("refuses an origin whose source scope does not contain the execution",async()=>{
  render(<Wrapper inherited={{...origin,request:{...origin.request,filters:{...origin.request.filters,source_ids:["other-source"]}}}}/>);
  fireEvent.click(screen.getByRole("button",{name:"读取相关运行与资源"}));await screen.findByRole("alert");expect(runtimeAPI.createReading).not.toHaveBeenCalled();
});

it("passes the original execution cutoff and both reading references through the resource review entry",async()=>{
 const target={source_system_id:request.source_system_id,source_instance:request.source_instance,org_id:request.org_id,project_id:request.project_id};
 const record:Runtime.ResourceRecord={schema_version:"xinsight.resource-access.internal.v1",synthetic:true,record_ref:"linked-resource",revision:1,reporter:target,target,request_id:request.request_id,application_ref:"app",actor_ref:"actor",occurred_at:cost.cutoff,report_kind:"source_audit",resource:{ref:"doc",version:"v1",name:"Linked resource",location:"enterprise_service",ownership:"unknown",classification:"unknown",device_management:"unknown"},action:"read",access_outcome:"unknown",access_decision:"unknown",withdrawn:false};
 vi.mocked(runtimeAPI.resources).mockResolvedValue({resources_version:"xinsight.runtime.resources.v1",reading_id:"xrt_related",request_ref:request.request_ref,membership_ref:"members",coverage:"synthetic_internal_records_only",items:[{record,record_hash:"sha256:"+"a".repeat(64),chain_state:"complete",input_state:"not_confirmed",input_evidence_basis:"frozen_reference_witnesses",content_verification:"not_performed",captured_input_evidence_refs:[],changed_since_reading:false}]});
 vi.spyOn(workspaceAPI,"listCases").mockResolvedValue({items:[]});
 const preview=vi.spyOn(assessmentAPI,"previewCreation").mockRejectedValue(new ApiRequestError(409));
 render(<Wrapper/>);fireEvent.click(screen.getByRole("button",{name:"读取相关运行与资源"}));fireEvent.click(await screen.findByRole("button",{name:"Linked resource"}));await screen.findByText("Linked resource · 合成验证");fireEvent.click(screen.getByText("从所选依据开始复核"));
 for(const label of ["新案件标题","调查发现标题","明确的判断陈述","开始复核的原因","复核理由"])fireEvent.change(await screen.findByLabelText(label),{target:{value:"Bounded review"}});
 fireEvent.click(screen.getByRole("checkbox",{name:/资源 linked-resource/}));fireEvent.click(screen.getByRole("button",{name:"预览采用依据"}));await waitFor(()=>expect(preview).toHaveBeenCalledOnce());
 expect(preview.mock.calls[0][0].execution_context).toEqual({workbench_reading_id:origin.readingID,execution_id:summary.execution_id,execution_as_of:cost.cutoff,runtime_reading_id:"xrt_related",runtime_request_ref:request.request_ref});
 expect(preview.mock.calls[0][0].selection.evidence).toEqual([]);expect(preview.mock.calls[0][0].selection.gaps).toEqual([]);
});


it("keeps inherited period, project filters and the same reading when content returns from another view or remounts",async()=>{
 const related=reading();vi.mocked(runtimeAPI.createReading).mockResolvedValue(related);vi.mocked(runtimeAPI.reading).mockResolvedValue(related);
 const listing=vi.spyOn(content,"fetchContentPage").mockResolvedValue({version:"xinsight.runtime-content.v1",reading_id:related.reading_id,request_ref:request.request_ref,membership_ref:related.membership_ref,content_interpretation_revision:"xinsight.content-membership.v1",member_basis:"content_manifest",items:[]});
 const body=vi.spyOn(content,"fetchVerifiedContent");
 const view=render(<Wrapper view="contents"/>);
 fireEvent.click(screen.getByRole("button",{name:"读取相关内容清单"}));await screen.findByText("本次阅读没有内容清单。");
 expect(runtimeAPI.createReading).toHaveBeenCalledWith({contract_version:"xinsight.runtime-reading.v1",metric_version:"xinsight.runtime.request-observations.v1",period,filters:{source_ids:["source-one"],source_projects:["project-9"],cost_projects:["cost-8"]}},"csrf",expect.any(AbortSignal));
 expect(body).not.toHaveBeenCalled();expect(runtimeAPI.resources).not.toHaveBeenCalled();expect(runtimeAPI.observations).not.toHaveBeenCalled();
 view.rerender(<Wrapper view="systems"/>);await screen.findByText(/本次阅读没有可展示的资源记录/);
 view.rerender(<Wrapper view="contents"/>);await screen.findByText("本次阅读没有内容清单。");
 expect(runtimeAPI.createReading).toHaveBeenCalledTimes(1);
 const retained=window.history.state.workbenchReading as WorkbenchNavigation;
 expect(retained.readingID).toBe(origin.readingID);expect(retained.related).toEqual({executionID:summary.execution_id,readingID:related.reading_id});
 view.unmount();render(<Wrapper view="contents" inherited={retained}/>);await screen.findByText("本次阅读没有内容清单。");
 expect(workbenchAPI.reading).toHaveBeenLastCalledWith(origin.readingID,expect.any(AbortSignal));expect(runtimeAPI.reading).toHaveBeenLastCalledWith(related.reading_id,expect.any(AbortSignal));
 expect(runtimeAPI.createReading).toHaveBeenCalledTimes(1);expect(body).not.toHaveBeenCalled();
 expect(listing.mock.calls.every(([r,q])=>r.reading_id===related.reading_id&&q.request_ref===request.request_ref)).toBe(true);
});

it.each([403,409,410])("does not broaden a content reading after inherited scope fails (%i)",async(status)=>{
 vi.mocked(workbenchAPI.reading).mockRejectedValue(new ApiRequestError(status));
 const listing=vi.spyOn(content,"fetchContentPage"),body=vi.spyOn(content,"fetchVerifiedContent");
 render(<Wrapper view="contents"/>);fireEvent.click(screen.getByRole("button",{name:"读取相关内容清单"}));await screen.findByRole("alert");
 expect(runtimeAPI.createReading).not.toHaveBeenCalled();expect(runtimeAPI.executionRequest).not.toHaveBeenCalled();expect(listing).not.toHaveBeenCalled();expect(body).not.toHaveBeenCalled();
});
