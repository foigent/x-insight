import { beforeEach as beforeLegacyLocale } from "vitest";
import { setLocale as setLegacyLocale } from "./i18n";
// These existing semantic assertions explicitly exercise the Chinese translation.
beforeLegacyLocale(() => setLegacyLocale("zh-CN"));
import userEvent from "@testing-library/user-event";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useState } from "react";
import { ApiRequestError, assessmentAPI,workspaceAPI,runtimeAPI, scopeSessionKey, type AccessSessionResponse, type ExecutionSummary } from "./api";
import { ExecutionRuntimePanel, type RuntimeDetailContext } from "./ExecutionRuntimePanel";
import type * as Runtime from "./runtime";
import * as content from "./content";
const session: AccessSessionResponse = { session: { subject_id: "subject", session_ref: "session", role: "workbench_reader", permissions: ["workbench.details.read"], created_at: "", last_seen_at: "", idle_expires_at: "", absolute_expires_at: "" }, csrf_token: "csrf" };
const summary = { execution_id: "execution-one", source_system_id: "source-one", request_id: "same-name" } as ExecutionSummary;
function reading(id = "reading-one"): Runtime.Reading {
  const now = Date.now();
  return { reading_id: id, contract_version: "xinsight.runtime-reading.v1", metric_version: "xinsight.runtime.request-observations.v1", period: { start_inclusive: new Date(now-86400000).toISOString(), end_exclusive: new Date(now).toISOString(), timezone: "UTC" }, cutoff: new Date(now).toISOString(), expires_at: new Date(now+1800000).toISOString(), authorization_revision: "auth", interpretation_revision: "interpretation", membership_ref: "membership-one", requests_with_entry_time: 1, requests_with_unknown_entry_time: 0, observation_count: 1, coverage: { period_completeness: "unknown", timeline_complete: false, time_basis: "trusted_request_started", unknown_entry_time_basis: "authorized_observation_in_period", missing_predecessors: 0, gaps: [], streams: [] } };
}
// These cases exercise an observed execution and its resource references.
// ContentPanel.test.tsx separately covers zero-observation content-only reads.
const request: Runtime.Request = { request_ref: "request-one", source_system_id: "source-one", source_instance: "instance-one", org_id: "org-one", project_id: "project-one", request_id: "same-name", entry_time_state: "unknown", last_observed_at: "2026-09-13T00:00:00Z", observation_count: 1 };
function Wrapper({ initial = null, view = "systems" }: { initial?: RuntimeDetailContext | null; view?: "systems" | "contents" }) {
  const [context, setContext] = useState(initial);
  return <ExecutionRuntimePanel view={view} summary={summary} session={session} context={context} setContext={setContext} />;
}
beforeEach(() => {
  vi.spyOn(runtimeAPI, "createReading").mockResolvedValue(reading());
  vi.spyOn(runtimeAPI, "executionRequest").mockImplementation(async id => ({ version: "xinsight.runtime.execution-request.v1", reading_id: id, membership_ref: "membership-one", execution_id: "execution-one", request }));
  vi.spyOn(runtimeAPI, "resources").mockImplementation(async (id, ref) => ({ resources_version: "xinsight.runtime.resources.v1", reading_id: id, request_ref: ref, membership_ref: "membership-one", coverage: "synthetic_internal_records_only", items: [] }));
  vi.spyOn(runtimeAPI, "observations").mockImplementation(async id => ({ reading_id: id, items: [] }));
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });
it("requires an explicit related read, preserves the source boundary and can retry an unmatched period", async () => {
  vi.mocked(runtimeAPI.executionRequest).mockRejectedValueOnce(new ApiRequestError(404));
  render(<Wrapper />);
  expect(runtimeAPI.createReading).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole("button", { name: "读取相关运行与资源" }));
  await screen.findByText(/不会按同名请求猜测/);
  expect(screen.getByRole("button", { name: "读取相关运行与资源" }).hasAttribute("disabled")).toBe(false);
  fireEvent.change(screen.getByLabelText("相关过程期间"), { target: { value: "168" } });
  fireEvent.click(screen.getByRole("button", { name: "读取相关运行与资源" }));
  await screen.findByText(/本次阅读没有可展示的资源记录/);
  expect(runtimeAPI.createReading).toHaveBeenLastCalledWith(expect.objectContaining({ period: expect.objectContaining({ relative_hours: 168 }), filters: { source_ids: ["source-one"], source_projects: [], cost_projects: [] } }), "csrf", expect.any(AbortSignal));
});
it("reuses a validated runtime reading and rejects an execution lookup bound to different membership", async () => {
  vi.mocked(runtimeAPI.executionRequest).mockResolvedValueOnce({ version: "xinsight.runtime.execution-request.v1", reading_id: "reading-one", membership_ref: "wrong-membership", execution_id: "execution-one", request });
  render(<Wrapper initial={{ reading: reading(), deadline: performance.now()+1800000, identity: scopeSessionKey(session) }} />);
  await screen.findByText("阅读已失效，原资料已移除。");
  expect(runtimeAPI.createReading).not.toHaveBeenCalled();
  expect(runtimeAPI.resources).not.toHaveBeenCalled();
  await waitFor(() => expect(screen.queryByText("same-name")).toBeNull());
});

it("shows the reused reading period without creating a new reading", async () => {
  const retained = reading();
  retained.period.start_inclusive = new Date(Date.parse(retained.period.end_exclusive) - 168 * 3_600_000).toISOString();
  render(<Wrapper initial={{ reading: retained, deadline: performance.now()+1800000, identity: scopeSessionKey(session) }} />);
  await screen.findByText(/本次阅读没有可展示的资源记录/);
  expect((screen.getByLabelText("相关过程期间") as HTMLSelectElement).value).toBe("168");
  expect(runtimeAPI.createReading).not.toHaveBeenCalled();
});

// Follow the customer's actual resource card, not an unrelated Story gap.
it("adopts the exact resource revision from shared execution detail into review",async()=>{
 const reviewer:AccessSessionResponse={...session,session:{...session.session,role:"workbench_investigation_reviewer",permissions:["investigation.scoped.read","investigation.scoped.write"]}};
 const record:Runtime.ResourceRecord={schema_version:"xinsight.resource-access.internal.v1",synthetic:true,record_ref:"resource-7",revision:2,previous_hash:"sha256:"+"b".repeat(64),reporter:{source_system_id:"reporter-one",source_instance:"app-one",org_id:"17",project_id:"101"},target:{source_system_id:request.source_system_id,source_instance:request.source_instance,org_id:request.org_id,project_id:request.project_id},request_id:request.request_id,application_ref:"app",actor_ref:"actor",occurred_at:"2026-09-14T00:00:00Z",report_kind:"source_audit",resource:{ref:"doc",version:"v2",name:"Resource citation fixture",location:"enterprise_service",ownership:"unknown",classification:"unknown",device_management:"unknown"},action:"read",access_outcome:"unknown",access_decision:"unknown",withdrawn:false};
 vi.mocked(runtimeAPI.resources).mockResolvedValue({resources_version:"xinsight.runtime.resources.v1",reading_id:"reading-one",request_ref:request.request_ref,membership_ref:"membership-one",coverage:"synthetic_internal_records_only",items:[{record,record_hash:"sha256:"+"a".repeat(64),chain_state:"complete",input_state:"not_confirmed",content_verification:"not_performed",input_evidence_basis:"frozen_reference_witnesses",changed_since_reading:false,captured_input_evidence_refs:[]}]});
 vi.spyOn(workspaceAPI,"listCases").mockResolvedValue({items:[]});
 const preview=vi.spyOn(assessmentAPI,"previewCreation").mockImplementation(async c=>({schema:"xinsight.review-basis.v2",statement:c.finding.statement,finding_version:1,known:c.selection,selected:c.selection,adopted_gaps:[],adopted_resources:[{reference:c.selection.resources![0],record}],digest:"sha256:"+"c".repeat(64)}));
 const user=userEvent.setup();render(<ExecutionRuntimePanel summary={summary} session={reviewer} context={{reading:reading(),deadline:performance.now()+1800000,identity:scopeSessionKey(reviewer)}} setContext={vi.fn()} navigate={vi.fn()}/>);
 await user.click(await screen.findByRole("button",{name:"Resource citation fixture"}));
 await user.click(await screen.findByText("从所选依据开始复核"));
 await user.click(await screen.findByRole("checkbox",{name:/资源 resource-7/}));
 await user.selectOptions(screen.getByLabelText("资源引用关系"),"supports");
 for (const label of ["新案件标题","调查发现标题","明确的判断陈述","开始复核的原因","复核理由"])await user.type(screen.getByLabelText(label),"Bounded resource review");
 await user.selectOptions(screen.getByLabelText("结论"),"supported");
 await user.click(screen.getByRole("button",{name:"预览采用依据"}));
 await screen.findByRole("button",{name:"确认开始复核"});
 expect(preview).toHaveBeenCalledTimes(1);
 expect(preview.mock.calls[0][0].selection).toEqual({evidence:[],gaps:[],resources:[{scope:"finding",reporter:record.reporter,target:record.target,request_id:record.request_id,record_ref:"resource-7",revision:2,record_hash:"sha256:"+"a".repeat(64),relation:"supports"}]});
});


it.each([1, 0])("opens exact execution content with %i B observations and reads body only on demand",async(count)=>{
 const related=reading();related.observation_count=count;
 vi.mocked(runtimeAPI.createReading).mockResolvedValue(related);
 const selected={...request,observation_count:count};
 vi.mocked(runtimeAPI.executionRequest).mockResolvedValue({version:"xinsight.runtime.execution-request.v1",reading_id:related.reading_id,membership_ref:related.membership_ref,execution_id:summary.execution_id,request:selected});
 const bytes=new TextEncoder().encode("saved-body");
 const reference:content.FrozenContentReference={source_system_id:selected.source_system_id,source_instance:selected.source_instance,org_id:selected.org_id,project_id:selected.project_id,request_id:selected.request_id,source_fact_id:"exact-manifest",revision:"1",source_sha256:"sha256:manifest",content_ref:"exact-body",capture_status:"complete",representation:{media_type:"text/plain",encoding:"identity",byte_domain:"stored_representation_before_http_content_encoding",saved_bytes:bytes.length,digest:"sha256:body"},expires_at:"2099-01-01T00:00:00.000000Z"};
 const listing=vi.spyOn(content,"fetchContentPage").mockResolvedValue({version:"xinsight.runtime-content.v1",reading_id:related.reading_id,request_ref:selected.request_ref,membership_ref:related.membership_ref,content_interpretation_revision:"xinsight.content-membership.v1",member_basis:"content_manifest",items:[{reference,availability:{state:"available",evaluated_at:"",retryable:false},newer_revision:false}]});
 const body=vi.spyOn(content,"fetchVerifiedContent").mockResolvedValue({reference,bytes,text:"saved-body",previewOnly:false});
 render(<Wrapper view="contents"/>);
 expect(runtimeAPI.createReading).not.toHaveBeenCalled();expect(listing).not.toHaveBeenCalled();expect(body).not.toHaveBeenCalled();
 fireEvent.click(screen.getByRole("button",{name:"读取相关内容清单"}));
 const read=await screen.findByRole("button",{name:"读取并核验正文"});
 expect(runtimeAPI.executionRequest).toHaveBeenCalledWith(related.reading_id,summary.execution_id,expect.any(AbortSignal));
 expect(listing).toHaveBeenCalledWith(related,selected,session,expect.any(AbortSignal),undefined);
 expect(runtimeAPI.observations).not.toHaveBeenCalled();expect(runtimeAPI.resources).not.toHaveBeenCalled();expect(body).not.toHaveBeenCalled();
 fireEvent.click(read);expect(await screen.findByText("saved-body")).toBeTruthy();
 expect(body).toHaveBeenCalledWith(related,selected,reference,session,expect.any(AbortSignal));
 expect(runtimeAPI.createReading).toHaveBeenCalledTimes(1);
});
