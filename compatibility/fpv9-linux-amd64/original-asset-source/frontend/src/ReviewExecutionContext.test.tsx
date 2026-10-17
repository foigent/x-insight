import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ApiRequestError, assessmentAPI, workspaceAPI, type AccessSessionResponse } from "./api";
import type { ExecutionContext, ResourceChoice, ReviewBasis, ReviewCreationCommand } from "./assessmentTypes";
import { ReviewStart } from "./ReviewStart";
import { ReviewBasisView } from "./ReviewBasisView";
import { formatDateTime } from "./format";
const session = { session: { subject_id: "xs_1111111111111111", permissions: ["investigation.write"] }, csrf_token: "csrf" } as AccessSessionResponse;
const context: ExecutionContext = {workbench_reading_id:"xwr_"+"a".repeat(22),execution_id:"xe1_"+"b".repeat(64),execution_as_of:"2026-09-20T01:00:00.123456Z",runtime_reading_id:"xrt_"+"c".repeat(22),runtime_request_ref:"xrr_"+"d".repeat(64)};
const scope={source_system_id:"11111111-1111-4111-8111-111111111111",source_instance:"instance",org_id:"17",project_id:"101"};
const resource: ResourceChoice={scope:"finding",reporter:scope,target:scope,request_id:"request",record_ref:"resource-one",revision:1,record_hash:"sha256:"+"e".repeat(64),relation:"context"};
const available={evidence:[],gaps:[],resources:[resource]};
function basis(c:Pick<ReviewCreationCommand,"execution_context"|"selection"> & {finding:{statement:string}}):ReviewBasis{return {schema:"xinsight.review-basis.v2",statement:c.finding.statement,finding_version:1,known:c.selection,selected:c.selection,adopted_gaps:[],digest:"sha256:"+"f".repeat(64),...(c.execution_context?{resolved_execution_context:{context:c.execution_context,runtime_cutoff:"2026-09-20T02:00:00.654321Z",workbench_membership_ref:"sha256:"+"a".repeat(64),runtime_membership_ref:"sha256:"+"b".repeat(64),target:scope,request_id:"request",resources:c.selection.resources??[],contents:c.selection.contents}}:{})};}
beforeEach(()=>{vi.spyOn(workspaceAPI,"listCases").mockResolvedValue({items:[]});vi.spyOn(assessmentAPI,"previewCreation").mockImplementation(async c=>basis(c));vi.spyOn(assessmentAPI,"create").mockRejectedValue(new TypeError("unknown outcome"));});
afterEach(()=>{cleanup();sessionStorage.clear();vi.restoreAllMocks();});
async function fill(select=true){
 fireEvent.click(screen.getByText("从所选依据开始复核"));
 for(const [label,value] of [["新案件标题","Case"],["调查发现标题","Finding"],["明确的判断陈述","Claim"],["开始复核的原因","Reason"],["复核理由","Pending"]])fireEvent.change(await screen.findByLabelText(label),{target:{value}});
 if(select)fireEvent.click(screen.getByRole("checkbox",{name:/资源 resource-one/}));
}
async function preview(){fireEvent.click(screen.getByRole("button",{name:"预览采用依据"}));await screen.findByRole("button",{name:"确认开始复核"});}
it("binds selected resources to server-resolved context and displays two independent cutoffs",async()=>{
 render(<ReviewStart executionContext={context} session={session} available={available} navigate={vi.fn()}/>);await fill();await preview();
 const command=vi.mocked(assessmentAPI.previewCreation).mock.calls[0][0];expect(command.execution_context).toEqual(context);expect(command.selection.evidence).toEqual([]);expect(command.selection.gaps).toEqual([]);
 fireEvent.click(screen.getByText(/服务端读取的采用依据/));
 expect(screen.getByText(formatDateTime(context.execution_as_of)).getAttribute("title")).toBe(context.execution_as_of);
 expect(screen.getByText(formatDateTime("2026-09-20T02:00:00.654321Z"))).toBeTruthy();
 expect(screen.getByText(/关联执行 · xe1_/).textContent).not.toContain(context.execution_id);
 fireEvent.click(screen.getByRole("button",{name:"确认开始复核"}));await screen.findByRole("button",{name:"原样重试创建"});
 expect(vi.mocked(assessmentAPI.create).mock.calls[0][0]).toEqual({...command,expected_basis_digest:"sha256:"+"f".repeat(64)});
});
it("does not add an execution association when no resource was selected",async()=>{
 render(<ReviewStart executionContext={context} session={session} available={available} navigate={vi.fn()}/>);await fill(false);await preview();expect(vi.mocked(assessmentAPI.previewCreation).mock.calls[0][0].execution_context).toBeUndefined();
});
it("explicit resource-only choice invalidates preview and requires a new preview",async()=>{
 render(<ReviewStart executionContext={context} session={session} available={available} navigate={vi.fn()}/>);await fill();await preview();fireEvent.click(screen.getByRole("checkbox",{name:"同时保留本次执行阅读关联"}));expect(screen.queryByRole("button",{name:"确认开始复核"})).toBeNull();await preview();expect(vi.mocked(assessmentAPI.previewCreation).mock.calls[1][0].execution_context).toBeUndefined();
});
it.each(["expired","denied","missing","different"])("does not downgrade a %s context preview",async kind=>{
 if(kind==="expired" || kind==="denied")vi.mocked(assessmentAPI.previewCreation).mockRejectedValue(new ApiRequestError(kind==="denied"?404:409));
 else vi.mocked(assessmentAPI.previewCreation).mockImplementation(async c=>{const b=basis(c);if(kind==="missing")delete b.resolved_execution_context;else b.resolved_execution_context!.context={...context,execution_id:"other"};return b;});
 render(<ReviewStart executionContext={context} session={session} available={available} navigate={vi.fn()}/>);await fill();fireEvent.click(screen.getByRole("button",{name:"预览采用依据"}));await screen.findAllByRole("alert");expect(screen.queryByRole("button",{name:"确认开始复核"})).toBeNull();expect(assessmentAPI.create).not.toHaveBeenCalled();expect(assessmentAPI.previewCreation).toHaveBeenCalledOnce();
 expect(vi.mocked(assessmentAPI.previewCreation).mock.calls[0][0].execution_context).toEqual(context);
});
it("retries the exact unresolved command after remount despite a different navigation context",async()=>{
 const first=render(<ReviewStart executionContext={context} session={session} available={available} navigate={vi.fn()}/>);await fill();await preview();fireEvent.click(screen.getByRole("button",{name:"确认开始复核"}));await screen.findByRole("button",{name:"原样重试创建"});const original=vi.mocked(assessmentAPI.create).mock.calls[0][0];first.unmount();
 render(<ReviewStart executionContext={{...context,runtime_reading_id:"xrt_new"}} session={session} available={available} navigate={vi.fn()}/>);fireEvent.click(await screen.findByRole("button",{name:"原样重试创建"}));await waitFor(()=>expect(assessmentAPI.create).toHaveBeenCalledTimes(2));expect(vi.mocked(assessmentAPI.create).mock.calls[1][0]).toEqual(original);
});
it("shows durable context on a later basis without consulting an ephemeral reading",()=>{
 const c={finding:{statement:"Claim"},selection:available,execution_context:context};render(<ReviewBasisView basis={basis(c)}/>);fireEvent.click(screen.getByText(/当时采用的依据/));expect(screen.getByLabelText("执行阅读关联").textContent).toContain("已保存的执行关系");expect(assessmentAPI.previewCreation).not.toHaveBeenCalled();
});

const contentChoice: import("./content").ContentChoice={scope:"finding",reference:{...scope,request_id:"request",source_fact_id:"content-one",revision:"1",source_sha256:"sha256:manifest",capture_status:"complete",content_ref:"body",representation:{media_type:"application/json",encoding:"identity",byte_domain:"stored_representation_before_http_content_encoding",saved_bytes:13,digest:"sha256:body"},expires_at:"2099-01-01T00:00:00.000000Z"},location:{kind:"json_pointer",pointer:"/messages/0"},relation:"context"};
it("binds a content-only selection to the original execution and selected relation in one command",async()=>{
 render(<ReviewStart executionContext={context} session={session} available={{evidence:[],gaps:[],contents:[contentChoice]}} navigate={vi.fn()}/>);
 await fill(false);fireEvent.click(screen.getByRole("checkbox",{name:/内容 content-one/}));
 fireEvent.change(screen.getByLabelText("内容引用关系"),{target:{value:"supports"}});
 expect(screen.getByRole("checkbox",{name:/内容 content-one.*支持/})).toHaveProperty("checked",true);
 await preview();const command=vi.mocked(assessmentAPI.previewCreation).mock.calls[0][0];expect(command.execution_context).toEqual(context);expect(command.selection.contents).toEqual([{...contentChoice,relation:"supports"}]);
 fireEvent.click(screen.getByRole("button",{name:"确认开始复核"}));await screen.findByRole("button",{name:"原样重试创建"});expect(vi.mocked(assessmentAPI.create).mock.calls[0][0]).toEqual({...command,expected_basis_digest:"sha256:"+"f".repeat(64)});
});
it("keeps pure content review legal without an execution origin",async()=>{
 render(<ReviewStart session={session} available={{evidence:[],gaps:[],contents:[contentChoice]}} navigate={vi.fn()}/>);await fill(false);fireEvent.click(screen.getByRole("checkbox",{name:/内容 content-one/}));await preview();expect(vi.mocked(assessmentAPI.previewCreation).mock.calls[0][0].execution_context).toBeUndefined();
});
it.each(["missing","request","relation","location"])("rejects %s content resolution instead of dropping execution binding",async kind=>{
 vi.mocked(assessmentAPI.previewCreation).mockImplementation(async c=>{const b=basis(c);const choice=structuredClone(contentChoice);if(kind==="missing")delete b.resolved_execution_context!.contents;else{if(kind==="request")choice.reference.request_id="other";if(kind==="relation")choice.relation="supports";if(kind==="location")choice.location={kind:"json_pointer",pointer:"/other"};b.resolved_execution_context!.contents=[choice];}return b;});
 render(<ReviewStart executionContext={context} session={session} available={{evidence:[],gaps:[],contents:[contentChoice]}} navigate={vi.fn()}/>);await fill(false);fireEvent.click(screen.getByRole("checkbox",{name:/内容 content-one/}));fireEvent.click(screen.getByRole("button",{name:"预览采用依据"}));await screen.findByRole("alert");expect(screen.queryByRole("button",{name:"确认开始复核"})).toBeNull();expect(assessmentAPI.create).not.toHaveBeenCalled();
});
