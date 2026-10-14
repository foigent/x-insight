import { beforeEach as beforeLegacyLocale } from "vitest";
import { setLocale as setLegacyLocale } from "./i18n";
// These existing semantic assertions explicitly exercise the Chinese translation.
beforeLegacyLocale(() => setLegacyLocale("zh-CN"));
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { WorkInstallPanel } from "./WorkInstallPanel";
import { workInstallAPI, type AccessSessionResponse, type WorkInstallInvitation, type WorkInstallResponse, type ReviewCaptureProjectCatalog } from "./api";

afterEach(()=>{cleanup();vi.restoreAllMocks();});
const session={session:{subject_id:"xs_1111111111111111",session_ref:"installation-admin"},csrf_token:"synthetic-csrf"} as AccessSessionResponse;
const invitation:WorkInstallInvitation={invitation_id:`xiei_${"a".repeat(32)}`,display_name:"研发 OpenClaw",source_instance:"openclaw-test",state:"invited",future_agents_allowed:false,expires_at:"2099-01-01T00:00:00Z"};
const candidates:WorkInstallInvitation={...invitation,state:"candidates",projects:[{project_key:"agent:main",label:"研究助手"}]};
const catalog:ReviewCaptureProjectCatalog={projects:[],items:[{source_system_id:"11111111-1111-4111-8111-111111111111",source_instance:"ur-a",org_id:"7",project_id:"9",source_type:"unirouter",source_name:"日常 UR",xi_project_name:"产品研发"}]};
const response=(item:WorkInstallInvitation):WorkInstallResponse=>({schema_version:"xinsight.local-work-install-response.v1",action:"read",state:item.state,invitation:item});

it("refreshing reported candidates prefills readable project choices and submits the exact selected UR scope",async()=>{
  vi.spyOn(workInstallAPI,"list").mockResolvedValue({...response(invitation),items:[invitation]});
  const read=vi.spyOn(workInstallAPI,"read").mockResolvedValueOnce(response(invitation)).mockResolvedValue(response(candidates));
  const approve=vi.spyOn(workInstallAPI,"approve").mockResolvedValue(response({...candidates,state:"approved",bindings:[]}));
  const user=userEvent.setup();render(<WorkInstallPanel session={session} projects={catalog}/>);
  await user.click(await screen.findByRole("button",{name:"研发 OpenClaw"}));
  expect(await screen.findByRole("region",{name:"主机安装步骤"})).toBeTruthy();
  await user.click(screen.getByRole("button",{name:"刷新安装状态"}));
  expect(await screen.findByLabelText("新项目名称")).toHaveProperty("value","研究助手");
  expect(screen.getByLabelText("自动接入这台主机今后新增的 Agent")).toHaveProperty("checked",true);
  await user.selectOptions(screen.getByLabelText("关联的 UR 项目（可选）"),JSON.stringify([catalog.items[0].source_system_id,"ur-a","7","9"]));
  await user.click(screen.getByRole("button",{name:"确认所列项目并授权接入"}));
  await waitFor(()=>expect(approve).toHaveBeenCalledWith(invitation.invitation_id,[{project_key:"agent:main",xi_project_name:"研究助手",ur_target:{source_system_id:catalog.items[0].source_system_id,source_instance:"ur-a",org_id:"7",project_id:"9"}}],[],true,session.csrf_token));
  expect(read).toHaveBeenCalledTimes(2);
  expect(screen.getByRole("status").textContent).toContain("收到活动后");
});

it("refresh preserves the administrator's unfinished project name and an uncertain approval requires readback",async()=>{
  vi.spyOn(workInstallAPI,"list").mockResolvedValue({...response(candidates),items:[candidates]});
  vi.spyOn(workInstallAPI,"read").mockResolvedValue(response(candidates));
  const approve=vi.spyOn(workInstallAPI,"approve").mockRejectedValue(new Error("lost reply"));
  const user=userEvent.setup();render(<WorkInstallPanel session={session} projects={catalog}/>);
  await user.click(await screen.findByRole("button",{name:"研发 OpenClaw"}));
  const name=await screen.findByLabelText("新项目名称");await user.clear(name);await user.type(name,"新研究项目");
  await user.click(screen.getByRole("button",{name:"刷新安装状态"}));
  await waitFor(()=>expect(screen.getByLabelText("新项目名称")).toHaveProperty("value","新研究项目"));
  await user.click(screen.getByLabelText("自动接入这台主机今后新增的 Agent"));
  await user.click(screen.getByRole("button",{name:"确认所列项目并授权接入"}));
  await waitFor(()=>expect(approve.mock.calls[0][3]).toBe(false));
  expect(await screen.findByRole("alert")).toHaveProperty("textContent","批准结果未确认；请读回同一邀请，勿立即重复批准。");
  expect(screen.getByRole("button",{name:"确认所列项目并授权接入"})).toHaveProperty("disabled",true);
  expect(approve).toHaveBeenCalledTimes(1);
  await user.click(screen.getByRole("button",{name:"刷新安装状态"}));
  await waitFor(()=>expect(screen.getByRole("button",{name:"确认所列项目并授权接入"})).toHaveProperty("disabled",false));
});


it("initial setup defaults to all projects and sends precise exclusions without creating their mappings",async()=>{
  const both={...candidates,projects:[...candidates.projects!,{project_key:"agent:private",label:"专用助手"}]};
  vi.spyOn(workInstallAPI,"list").mockResolvedValue({...response(both),items:[both]});
  vi.spyOn(workInstallAPI,"read").mockResolvedValue(response(both));
  const approve=vi.spyOn(workInstallAPI,"approve").mockResolvedValue(response({...both,state:"approved"}));
  const user=userEvent.setup();render(<WorkInstallPanel session={session} projects={catalog}/>);
  await user.click(await screen.findByRole("button",{name:"研发 OpenClaw"}));
  expect(await screen.findByLabelText("接入 研究助手")).toHaveProperty("checked",true);
  expect(screen.getByLabelText("接入 专用助手")).toHaveProperty("checked",true);
  await user.click(screen.getByLabelText("接入 专用助手"));
  await user.click(screen.getByRole("button",{name:"确认所列项目并授权接入"}));
  await waitFor(()=>expect(approve).toHaveBeenCalledWith(invitation.invitation_id,
    [{project_key:"agent:main",xi_project_name:"研究助手"}],["agent:private"],true,session.csrf_token));
});

it("future manual decisions partition all pending agents and an HTTP success with conflict never reports approval",async()=>{
  const future={...invitation,state:"approved" as const,projects:candidates.projects,bindings:[],future_agents_allowed:false,
    future_candidates:[{project_key:"agent:writer",label:"写作助手"},{project_key:"agent:private",label:"专用助手"}]};
  vi.spyOn(workInstallAPI,"list").mockResolvedValue({...response(future),items:[future]});
  vi.spyOn(workInstallAPI,"read").mockResolvedValue(response(future));
  const decide=vi.spyOn(workInstallAPI,"decideFuture").mockResolvedValue({...response(future),state:"conflict"});
  const user=userEvent.setup();render(<WorkInstallPanel session={session} projects={catalog}/>);
  await user.click(await screen.findByRole("button",{name:"研发 OpenClaw"}));
  expect(await screen.findByRole("heading",{name:"新增 Agent 等待确认"})).toBeTruthy();
  expect(screen.queryByLabelText("自动接入这台主机今后新增的 Agent")).toBeNull();
  await user.click(screen.getByLabelText("接入 专用助手"));
  await user.click(screen.getByRole("button",{name:"保存新增 Agent 的选择"}));
  await waitFor(()=>expect(decide).toHaveBeenCalledWith(invitation.invitation_id,
    [{project_key:"agent:writer",xi_project_name:"写作助手"}],["agent:private"],session.csrf_token));
  expect(await screen.findByRole("alert")).toHaveProperty("textContent","批准结果未确认；请读回同一邀请，勿立即重复批准。");
  expect(screen.queryByRole("status")).toBeNull();
  expect(within(screen.getByRole("region",{name:"安装候选详情"})).getByRole("button",{name:"保存新增 Agent 的选择"})).toHaveProperty("disabled",true);
});


it("distinguishes authorization from host reception and retains historical gaps without claiming current failure",async()=>{
  const approved={...invitation,state:"approved" as const};
  vi.spyOn(workInstallAPI,"list").mockResolvedValue({...response(approved),items:[approved]});
  vi.spyOn(workInstallAPI,"read").mockResolvedValueOnce(response(approved)).mockResolvedValue(response({...approved,
    status_received_at:new Date(Date.now()-4*60000).toISOString(),latest_status:{metadata_pending:2,metadata_blocked:1,body_pending:3,
      gaps:[{code:"body_limit",occurrences:4}]}}));
  const user=userEvent.setup();render(<WorkInstallPanel session={session} projects={catalog}/>);
  await user.click(await screen.findByRole("button",{name:"研发 OpenClaw"}));
  expect(await screen.findByText("尚未收到主机状态；项目获批不等于采集已经运行。")).toBeTruthy();
  await user.click(screen.getByRole("button",{name:"刷新安装状态"}));
  expect(await screen.findByText(/上报已超过 3 分钟/)).toBeTruthy();
  expect(screen.getByText("待发送活动 2 条，受阻活动 1 条，待发送正文 3 份。")).toBeTruthy();
  expect(screen.getByText("正文超过单条上限：4 次")).toBeTruthy();
  expect(screen.getByText(/累计报告次数不代表丢失事件数/)).toBeTruthy();
});


it("continues the server cursor and keeps an approved installation discoverable across pages",async()=>{
  const next={...invitation,invitation_id:`xiei_${"b".repeat(32)}`,display_name:"已部署财务助手",state:"approved" as const};
  const list=vi.spyOn(workInstallAPI,"list").mockResolvedValueOnce({...response(invitation),items:[invitation],next_cursor:invitation.invitation_id})
    .mockResolvedValue({...response(next),items:[invitation,next]});
  vi.spyOn(workInstallAPI,"read").mockResolvedValue(response(next));
  const user=userEvent.setup();render(<WorkInstallPanel session={session} projects={catalog}/>);
  await user.click(await screen.findByRole("button",{name:"加载更多安装"}));
  expect(list.mock.calls[1]).toEqual([undefined,invitation.invitation_id]);
  expect(screen.getAllByRole("button",{name:"研发 OpenClaw"})).toHaveLength(1);
  await user.click(await screen.findByRole("button",{name:"已部署财务助手"}));
  expect(await screen.findByText("尚未收到主机状态；项目获批不等于采集已经运行。")).toBeTruthy();
  expect(screen.queryByRole("button",{name:"加载更多安装"})).toBeNull();
});
