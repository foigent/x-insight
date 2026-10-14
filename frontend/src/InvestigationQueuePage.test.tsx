import { beforeEach as beforeLegacyLocale } from "vitest";
import { setLocale as setLegacyLocale } from "./i18n";
// These existing semantic assertions explicitly exercise the Chinese translation.
beforeLegacyLocale(() => setLegacyLocale("zh-CN"));
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { ApiRequestError, investigationQueueAPI, type AccessSessionResponse, type InvestigationQueueFilter, type InvestigationQueuePage as QueuePage } from "./api";
import { InvestigationQueuePage, isPendingQueueFilter, queueFiltersFromLocation } from "./InvestigationQueuePage";
import { clearPendingCommand, keepPendingCommand, readPendingCommand } from "./pendingReview";
const subject="xs_1111111111111111";
const session={session:{subject_id:subject,permissions:["evidence.read","investigation.preferences.write"]},csrf_token:"fixture-csrf"} as AccessSessionResponse;
const filters={mode:"current",period:"24h",sort:"created_desc"};
const now="2026-09-10T00:00:00Z";
const saved:InvestigationQueueFilter={id:"xqf_abcdefghijklmnopqrstuv",owner_subject:subject,schema_version:"xinsight.investigation-queue-filter.v1",name:"我的筛选",filters,revision:1,deleted:false,created_at:now,updated_at:now};
const page=(name="Test Finding",cursor?:string):QueuePage=>({items:[{finding:{id:name,case_id:"case-a",title:name,statement:"A bounded claim",classification:"hypothesis",status:"open",created_by_subject:subject,created_at:now,updated_at:now,version:1},case_title:"Synthetic Case",case_status:"sealed",assessment_version:0,followup:{case_id:"case-a",finding_id:name,recorded:false,version:0,assignee_subject_id:null,assignee_name:"",due_at:null,due_timezone:null,updated_at:null,updated_by_subject:""},progress:"unrecorded",conclusion:"unrecorded",basis:"unrecorded",review_basis:"unrecorded",progress_basis:"unrecorded",basis_revision:"a".repeat(64),overdue:false,assignee_state:"unassigned"}],total:201,evaluated_at:now,version:"a".repeat(64),filters,next_cursor:cursor,coverage:"unknown",statistics:{current:201,created:100,reassessment:0,unassigned:201,overdue:0,basis_unrecorded:201,basis_changed:0,basis_unavailable:0}});
function mocks(){vi.spyOn(investigationQueueAPI,"list").mockResolvedValue(page());vi.spyOn(investigationQueueAPI,"filters").mockResolvedValue({items:[]})}
afterEach(()=>{cleanup();sessionStorage.clear();window.history.replaceState(null,"","/");vi.restoreAllMocks()});
it("uses full-set counts, appends stable pages and returns to the exact queue",async()=>{
 mocks();vi.mocked(investigationQueueAPI.list).mockResolvedValueOnce(page("First","cursor")).mockResolvedValueOnce(page("Second"));const navigate=vi.fn();render(<InvestigationQueuePage session={session} navigate={navigate}/>);
 await screen.findByText("First");expect(screen.getByText(/已显示 1 \/ 201/)).toBeTruthy();await userEvent.click(screen.getByRole("button",{name:"继续读取下一页"}));await screen.findByText("Second");expect(screen.getByText(/已显示 2 \/ 201/)).toBeTruthy();expect(investigationQueueAPI.list).toHaveBeenLastCalledWith(filters,"cursor",expect.any(AbortSignal));await userEvent.click(screen.getAllByRole("button",{name:"查看与跟进"})[0]);expect(navigate.mock.calls[0][0]).toContain("queue_return=%2Finvestigation%2Fqueue%3Fmode%3Dcurrent");expect(navigate.mock.calls[0][0]).toContain("finding=First");
});
it("keeps an unknown operation across refresh and retries its exact identity",async()=>{
 mocks();const save=vi.spyOn(investigationQueueAPI,"save").mockRejectedValueOnce(new TypeError("unknown result")).mockResolvedValue(saved);const ui=render(<InvestigationQueuePage session={session} navigate={vi.fn()}/>);await screen.findByText("Test Finding");await userEvent.type(screen.getByLabelText("筛选名称"),saved.name);await userEvent.click(screen.getByRole("button",{name:"保存个人筛选"}));await screen.findByRole("button",{name:"原样重试待确认操作"});const original=save.mock.calls[0][2];ui.unmount();vi.mocked(investigationQueueAPI.filters).mockResolvedValue({items:[{...saved,revision:3,name:"Later revision"}]});render(<InvestigationQueuePage session={session} navigate={vi.fn()}/>);await userEvent.click(await screen.findByRole("button",{name:"原样重试待确认操作"}));await screen.findByText("个人筛选操作已确认。");expect(save.mock.calls[1][2]).toEqual(original);await waitFor(()=>expect((screen.getByLabelText("筛选名称") as HTMLInputElement).value).toBe("Later revision"));expect(sessionStorage.length).toBe(0);
});
it("updates and deletes own selected filters with revision checks",async()=>{
 mocks();vi.mocked(investigationQueueAPI.filters).mockResolvedValue({items:[saved]});const save=vi.spyOn(investigationQueueAPI,"save").mockResolvedValue({...saved,revision:2});render(<InvestigationQueuePage session={session} navigate={vi.fn()}/>);await screen.findByRole("option",{name:saved.name});fireEvent.change(screen.getByLabelText("已保存筛选"),{target:{value:saved.id}});await userEvent.click(screen.getByRole("button",{name:"更新个人筛选"}));await waitFor(()=>expect(save).toHaveBeenCalledOnce());expect(save.mock.calls[0][0]).toBe("update");expect(save.mock.calls[0][2].expected_revision).toBe(1);await waitFor(()=>expect(screen.getByRole("button",{name:"删除个人筛选"})).not.toHaveProperty("disabled",true));vi.mocked(investigationQueueAPI.filters).mockResolvedValue({items:[]});save.mockResolvedValue({...saved,revision:2,deleted:true});await userEvent.click(screen.getByRole("button",{name:"删除个人筛选"}));await waitFor(()=>expect(save).toHaveBeenCalledTimes(2));expect(save.mock.calls[1][0]).toBe("delete");await waitFor(()=>expect(screen.queryByRole("button",{name:"删除个人筛选"})).toBeNull());
});
it("clears a rejected revision without classifying it as an unknown result",async()=>{
 mocks();vi.spyOn(investigationQueueAPI,"save").mockRejectedValue(new ApiRequestError(409,{type:"conflict",title:"Conflict",status:409,code:"workspace_conflict",correlation_id:"fixture",retryable:false}));render(<InvestigationQueuePage session={session} navigate={vi.fn()}/>);await screen.findByText("Test Finding");await userEvent.type(screen.getByLabelText("筛选名称"),"Conflict");await userEvent.click(screen.getByRole("button",{name:"保存个人筛选"}));await screen.findByText(/筛选已有变化/);expect(sessionStorage.length).toBe(0);expect(screen.queryByText(/操作结果待确认。请原样重试/)).toBeNull();
});
it("keeps malformed pending data visible and blocks overwrite",async()=>{
 mocks();const key="xinsight.pending-queue-filter.v1:"+subject;sessionStorage.setItem(key,"broken");render(<InvestigationQueuePage session={session} navigate={vi.fn()}/>);await screen.findByText("Test Finding");expect(screen.getByRole("alert").textContent).toContain("记录损坏");expect(screen.getByRole("button",{name:"保存个人筛选"})).toHaveProperty("disabled",true);expect(sessionStorage.getItem(key)).toBe("broken");
});
it("discards stale reads on subject changes and removes preference controls",async()=>{
 mocks();let finish!:(v:QueuePage)=>void;const late=new Promise<QueuePage>(resolve=>{finish=resolve});vi.mocked(investigationQueueAPI.list).mockReturnValueOnce(late).mockResolvedValue(page("New subject"));const ui=render(<InvestigationQueuePage session={session} navigate={vi.fn()}/>);ui.rerender(<InvestigationQueuePage session={{...session,session:{...session.session,subject_id:"xs_2222222222222222",permissions:["evidence.read"]}}} navigate={vi.fn()}/>);await screen.findByText("New subject");await act(async()=>{finish(page("Old subject"));await late});expect(screen.queryByText("Old subject")).toBeNull();expect(screen.queryByLabelText("个人筛选")).toBeNull();
});
it("refreshes after cursor conflicts and preserves explicit custom ranges in navigation",async()=>{
 mocks();vi.mocked(investigationQueueAPI.list).mockResolvedValueOnce(page("First","old")).mockRejectedValueOnce(new ApiRequestError(409,{type:"conflict",title:"Changed",status:409,code:"workspace_conflict",correlation_id:"fixture",retryable:false})).mockResolvedValue(page("Fresh"));render(<InvestigationQueuePage session={session} navigate={vi.fn()}/>);await screen.findByText("First");await userEvent.click(screen.getByRole("button",{name:"继续读取下一页"}));await waitFor(()=>expect(screen.queryByText("First")).toBeNull());await userEvent.click(screen.getByRole("button",{name:"刷新当前数据"}));await screen.findByText("Fresh");fireEvent.change(screen.getByLabelText("新建期间"),{target:{value:"custom"}});fireEvent.change(screen.getByLabelText("起始时间（含时区）"),{target:{value:"2026-09-01T00:00:00+08:00"}});fireEvent.change(screen.getByLabelText("结束时间（不含）"),{target:{value:"2026-09-02T00:00:00+08:00"}});await userEvent.click(screen.getByRole("button",{name:"应用筛选"}));expect(queueFiltersFromLocation().from).toBe("2026-09-01T00:00:00+08:00");
});
it("bounds generic recovery storage and never removes a different command",()=>{
 expect(isPendingQueueFilter({kind:"wrong"})).toBe(false);expect(isPendingQueueFilter(null)).toBe(false);keepPendingCommand("test",{v:1});expect(()=>keepPendingCommand("test",{v:2})).toThrow();clearPendingCommand("test",{v:2});expect(readPendingCommand("test",(v):v is {v:number}=>!!v)).toEqual({v:1});clearPendingCommand("test",{v:1});expect(()=>keepPendingCommand("big","x".repeat(70000))).toThrow();sessionStorage.setItem("big","x".repeat(70000));expect(()=>readPendingCommand("big",isPendingQueueFilter)).toThrow();
});

const conflict=()=>new ApiRequestError(409,{type:"conflict",title:"Conflict",status:409,code:"workspace_conflict",correlation_id:"fixture",retryable:false});
function deferred<T>(){let resolve!:(value:T)=>void;const promise=new Promise<T>(done=>{resolve=done});return {promise,resolve}}
it("keeps a conflicted draft and explicitly rebases update onto the reread version",async()=>{
 mocks();const v3={...saved,revision:3},v4={...saved,revision:4,name:"并发保存",filters:{...filters,search:"remote search"}};
 vi.mocked(investigationQueueAPI.filters).mockResolvedValueOnce({items:[v3]}).mockResolvedValue({items:[v4]});
 const save=vi.spyOn(investigationQueueAPI,"save").mockRejectedValueOnce(conflict()).mockResolvedValue({...v4,revision:5});
 render(<InvestigationQueuePage session={session} navigate={vi.fn()}/>);await screen.findByRole("option",{name:saved.name});fireEvent.change(screen.getByLabelText("已保存筛选"),{target:{value:saved.id}});
 fireEvent.change(screen.getByLabelText("筛选名称"),{target:{value:"我的草稿"}});fireEvent.change(screen.getByLabelText("搜索事项"),{target:{value:"draft search"}});
 await userEvent.click(screen.getByRole("button",{name:"更新个人筛选"}));await screen.findByText(/筛选已有变化/);
 expect(screen.getByRole("button",{name:"更新个人筛选"})).toHaveProperty("disabled",true);
 await userEvent.click(screen.getByRole("button",{name:"重读保存列表"}));await screen.findByRole("region",{name:"保存版本冲突"});
 expect(screen.getByLabelText("筛选名称")).toHaveProperty("value","我的草稿");expect(screen.getByLabelText("搜索事项")).toHaveProperty("value","draft search");expect(screen.getByText("remote search")).toBeTruthy();
 expect(screen.getByRole("button",{name:"删除个人筛选"})).toHaveProperty("disabled",true);
 await userEvent.click(screen.getByRole("button",{name:"保留草稿继续修改"}));await userEvent.click(screen.getByRole("button",{name:"更新个人筛选"}));await waitFor(()=>expect(save).toHaveBeenCalledTimes(2));
 expect(save.mock.calls[1][2]).toMatchObject({expected_revision:4,name:"我的草稿",filters:{search:"draft search"}});
});
it("loads the reread version explicitly before retrying a conflicted delete",async()=>{
 mocks();const latest={...saved,revision:4,name:"新保存名称",filters:{...filters,search:"new filter"}};vi.mocked(investigationQueueAPI.filters).mockResolvedValueOnce({items:[{...saved,revision:3}]}).mockResolvedValueOnce({items:[latest]}).mockResolvedValue({items:[]});
 const save=vi.spyOn(investigationQueueAPI,"save").mockRejectedValueOnce(conflict()).mockResolvedValue({...latest,revision:5,deleted:true});render(<InvestigationQueuePage session={session} navigate={vi.fn()}/>);
 await screen.findByRole("option",{name:saved.name});fireEvent.change(screen.getByLabelText("已保存筛选"),{target:{value:saved.id}});fireEvent.change(screen.getByLabelText("筛选名称"),{target:{value:"未保存草稿"}});
 await userEvent.click(screen.getByRole("button",{name:"删除个人筛选"}));await screen.findByText(/筛选已有变化/);await userEvent.click(screen.getByRole("button",{name:"重读保存列表"}));
 await userEvent.click(await screen.findByRole("button",{name:"载入已保存版本"}));expect(screen.getByLabelText("筛选名称")).toHaveProperty("value",latest.name);expect(screen.getByLabelText("搜索事项")).toHaveProperty("value","new filter");
 await userEvent.click(screen.getByRole("button",{name:"删除个人筛选"}));await waitFor(()=>expect(save).toHaveBeenCalledTimes(2));expect(save.mock.calls[1][2]).toMatchObject({expected_revision:4,name:latest.name,filters:latest.filters});
});
it("preserves a draft when the selected saved filter was deleted elsewhere",async()=>{
 mocks();vi.mocked(investigationQueueAPI.filters).mockResolvedValueOnce({items:[saved]}).mockResolvedValue({items:[]});const save=vi.spyOn(investigationQueueAPI,"save");render(<InvestigationQueuePage session={session} navigate={vi.fn()}/>);
 await screen.findByRole("option",{name:saved.name});fireEvent.change(screen.getByLabelText("已保存筛选"),{target:{value:saved.id}});fireEvent.change(screen.getByLabelText("筛选名称"),{target:{value:"尚未保存的名称"}});fireEvent.change(screen.getByLabelText("搜索事项"),{target:{value:"draft"}});
 await userEvent.click(screen.getByRole("button",{name:"重读保存列表"}));await screen.findByText(/该个人筛选已被删除/);expect(screen.getByLabelText("筛选名称")).toHaveProperty("value","尚未保存的名称");expect(screen.getByLabelText("搜索事项")).toHaveProperty("value","draft");expect(screen.queryByRole("button",{name:"删除个人筛选"})).toBeNull();expect(save).not.toHaveBeenCalled();
});
it("ignores late saved lists and never replaces a changed selection or a newer draft",async()=>{
 mocks();const other={...saved,id:"xqf_abcdefghijklmnopqrstuw",name:"另一个筛选"};const late=deferred<{items:InvestigationQueueFilter[]}>(),newer=deferred<{items:InvestigationQueueFilter[]}>();
 vi.mocked(investigationQueueAPI.filters).mockResolvedValueOnce({items:[saved,other]}).mockReturnValueOnce(late.promise).mockReturnValueOnce(newer.promise);
 render(<InvestigationQueuePage session={session} navigate={vi.fn()}/>);await screen.findByRole("option",{name:saved.name});fireEvent.change(screen.getByLabelText("已保存筛选"),{target:{value:saved.id}});
 await userEvent.click(screen.getByRole("button",{name:"重读保存列表"}));fireEvent.change(screen.getByLabelText("已保存筛选"),{target:{value:other.id}});await userEvent.click(screen.getByRole("button",{name:"重读保存列表"}));fireEvent.change(screen.getByLabelText("筛选名称"),{target:{value:"读取中的新草稿"}});
 await act(async()=>{newer.resolve({items:[saved,{...other,revision:4}]});await newer.promise});await act(async()=>{late.resolve({items:[{...saved,revision:2},other]});await late.promise});
 expect(screen.getByLabelText("已保存筛选")).toHaveProperty("value",other.id);expect(screen.getByLabelText("筛选名称")).toHaveProperty("value","读取中的新草稿");expect(screen.queryByRole("region",{name:"保存版本冲突"})).toBeNull();
 vi.mocked(investigationQueueAPI.filters).mockResolvedValue({items:[saved,{...other,revision:4}]});await userEvent.click(screen.getByRole("button",{name:"重读保存列表"}));await screen.findByText(/当前保存版本 4/);
});
it("does not let an older reread replace a newer reconciliation choice",async()=>{
 mocks();const first=deferred<{items:InvestigationQueueFilter[]}>(),second=deferred<{items:InvestigationQueueFilter[]}>();vi.mocked(investigationQueueAPI.filters).mockResolvedValueOnce({items:[saved]}).mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);render(<InvestigationQueuePage session={session} navigate={vi.fn()}/>);
 await screen.findByRole("option",{name:saved.name});fireEvent.change(screen.getByLabelText("已保存筛选"),{target:{value:saved.id}});await userEvent.click(screen.getByRole("button",{name:"重读保存列表"}));await userEvent.click(screen.getByRole("button",{name:"重读保存列表"}));
 await act(async()=>{second.resolve({items:[{...saved,revision:4}]});await second.promise});await screen.findByText(/当前保存版本 4/);await userEvent.click(screen.getByRole("button",{name:"载入已保存版本"}));
 await act(async()=>{first.resolve({items:[{...saved,revision:2}]});await first.promise});expect(screen.queryByRole("region",{name:"保存版本冲突"})).toBeNull();const save=vi.spyOn(investigationQueueAPI,"save").mockResolvedValue({...saved,revision:5});vi.mocked(investigationQueueAPI.filters).mockResolvedValue({items:[{...saved,revision:5}]});await userEvent.click(screen.getByRole("button",{name:"删除个人筛选"}));await waitFor(()=>expect(save).toHaveBeenCalledOnce());expect(save.mock.calls[0][2].expected_revision).toBe(4);
});

it("shows the same withdrawal signal on the queue without adopting it",async()=>{
 mocks();const p=page();const scope={source_system_id:"11111111-1111-4111-8111-111111111111",source_instance:"app",org_id:"1",project_id:"2"};
 p.items[0].basis="changed";p.items[0].review_basis="changed";p.items[0].resource_updates=[{reference:{scope:"finding",reporter:scope,target:scope,request_id:"request-1",record_ref:"report-1",revision:1,record_hash:"sha256:"+"c".repeat(64),relation:"supports"},state:"withdrawn",latest_revision:2}];
 vi.mocked(investigationQueueAPI.list).mockResolvedValue(p);
 render(<InvestigationQueuePage session={session} navigate={vi.fn()}/>);
 expect(await screen.findByText(/后续修订 2 已声明撤回/)).toBeTruthy();expect(screen.getByText(/已采用修订 1/)).toBeTruthy();
});
