import {afterEach,expect,it,vi} from "vitest";
import {cleanup,render,screen,waitFor} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {ConnectorSetupPage,type SetupCreated,type SetupPreview,type SetupStatus} from "./ConnectorSetupPage";
import {ApiRequestError,connectorSetupAPI,type AccessSessionResponse} from "./api";
const session={session:{subject_id:"xs_1111111111111111",session_ref:"setup-session"},csrf_token:"csrf"} as AccessSessionResponse;
const installation="xci_AAAAAAAAAAAAAAAAAAAAAA";
const settings={mode:"full_content" as const,retention_seconds:90*86400,project_quota_bytes:5*1024**3,max_item_bytes:32*1024**2};
const preview:SetupPreview={schema_version:"xinsight.connector-setup-preview.v1",deployment_id:"xid_AAAAAAAAAAAAAAAAAAAAAA",ur_origin:"https://ur.example",custody:{desired_revision:0,effective_revision:0,settings:null},can_change_custody:true};
const created:SetupCreated={invitation:{schema_version:"xinsight.connector-installation-invitation.v1",invitation_id:"xii_AAAAAAAAAAAAAAAAAAAAAA",installation_id:installation,deployment_id:preview.deployment_id,audience:"unirouter",xinsight_origin:"https://xi.example",requested_event_scope:[],expires_at:"2026-10-03T10:00:00Z",install_url:"https://ur.example/connectors/x-insight/install#invitation=synthetic-secret"},consent:{schema_version:"xinsight.connector-setup-consent-view.v1",installation_id:installation,deployment_id:preview.deployment_id,xi_origin:"https://xi.example",ur_origin:preview.ur_origin,consent_revision:1,consent_sha256:"sha256:"+"a".repeat(64),expires_at:"2026-10-03T10:00:00Z",custody:settings,expected_custody_revision:0}};
const status:SetupStatus={schema_version:"xinsight.connector-setup-status.v1",installation_id:installation,deployment_id:preview.deployment_id,consent_revision:1,state:"waiting_consent",custody:{...preview.custody,loaded:false},receiving:{last_success_at:null,profiles_received:[],content_received_at:null,content_read_verified_at:null},issues:[]};
const storageKey=`xinsight:connector-setup:${session.session.subject_id}`;
afterEach(()=>{cleanup();vi.restoreAllMocks();sessionStorage.clear();history.replaceState(null,"","/")});
function mocks(){vi.spyOn(connectorSetupAPI,"preview").mockResolvedValue(preview);vi.spyOn(connectorSetupAPI,"status").mockResolvedValue(status);return vi.spyOn(connectorSetupAPI,"create").mockResolvedValue(created)}
it("recommends full content 90 days and only creates after explicit confirmation",async()=>{
 const save=mocks();const user=userEvent.setup();render(<ConnectorSetupPage session={session}/>);
 expect(await screen.findByLabelText("正文复制模式")).toHaveProperty("value","full_content");expect(screen.getByLabelText("留存时间（天）")).toHaveProperty("value","90");expect(screen.getByLabelText("每项目容量（GiB）")).toHaveProperty("value","5");expect(save).not.toHaveBeenCalled();
 await user.click(screen.getByRole("button",{name:"确认设置并创建邀请"}));await screen.findByRole("link",{name:"在 UniRouter 中继续"});
 expect(save.mock.calls[0][0]).toMatchObject({custody:settings,expected_custody_revision:0});expect(sessionStorage.getItem(storageKey)).not.toContain("synthetic-secret");expect(location.href).not.toContain("synthetic-secret");
});
it("recovers the same explicit request after a lost create response and page refresh",async()=>{
 const save=mocks();save.mockRejectedValueOnce(new Error("lost response"));const user=userEvent.setup();const page=render(<ConnectorSetupPage session={session}/>);await user.click(await screen.findByRole("button",{name:"确认设置并创建邀请"}));await screen.findByRole("alert");const original=save.mock.calls[0][0];page.unmount();
 render(<ConnectorSetupPage session={{...session,session:{...session.session,session_ref:"new-login-session"}}}/>);
 await screen.findByRole("link",{name:"在 UniRouter 中继续"});expect(save.mock.calls[1][0]).toEqual(original);expect(JSON.parse(sessionStorage.getItem(storageKey)!).installation).toBe(installation);expect(sessionStorage.getItem(storageKey)).not.toContain("synthetic-secret");
});
it("refreshes an unredeemed invitation from the same non-secret saved request",async()=>{
 const save=mocks();const put={schema_version:"xinsight.connector-setup-consent.v1",request_id:"12345678-1234-4234-8234-123456789abc",expected_custody_revision:0,custody:settings};sessionStorage.setItem(storageKey,JSON.stringify({subject:session.session.subject_id,put,installation}));
 render(<ConnectorSetupPage session={session}/>);expect((await screen.findByRole("link",{name:"在 UniRouter 中继续"})).getAttribute("href")).toBe(created.invitation.install_url);expect(save.mock.calls[0][0]).toEqual(put);
});
it("shows a current applied disabled policy without replaying or freezing initial full consent",async()=>{
 const save=mocks();vi.spyOn(connectorSetupAPI,"status").mockResolvedValue({...status,state:"ready",custody:{desired_revision:2,effective_revision:2,loaded:true,settings:{...settings,mode:"disabled"}},receiving:{...status.receiving,last_success_at:"2026-10-03T09:00:00Z"}});
 sessionStorage.setItem(storageKey,JSON.stringify({subject:session.session.subject_id,put:{schema_version:"xinsight.connector-setup-consent.v1",request_id:"12345678-1234-4234-8234-123456789abc",expected_custody_revision:0,custody:settings},installation}));render(<ConnectorSetupPage session={session}/>);
 await screen.findByRole("heading",{name:"已开始实际同步"});expect(save).not.toHaveBeenCalled();expect(screen.queryByRole("link",{name:"在 UniRouter 中继续"})).toBeNull();
});
it("requires a new explicit confirmation after expired response with no installation ID",async()=>{
 const save=mocks();save.mockRejectedValue(new ApiRequestError(410));sessionStorage.setItem(storageKey,JSON.stringify({subject:session.session.subject_id,put:{schema_version:"xinsight.connector-setup-consent.v1",request_id:"12345678-1234-4234-8234-123456789abc",expected_custody_revision:0,custody:settings}}));
 render(<ConnectorSetupPage session={session}/>);const user=userEvent.setup();await user.click(await screen.findByRole("button",{name:"重新读取并开始新的确认"}));expect(save).toHaveBeenCalledTimes(1);expect(await screen.findByRole("button",{name:"确认设置并创建邀请"})).toHaveProperty("disabled",false);expect(sessionStorage.getItem(storageKey)).toBeNull();
});
it("does not replay a request belonging to a different XI subject",async()=>{
 const save=mocks();sessionStorage.setItem(storageKey,JSON.stringify({subject:"xs_2222222222222222",put:{schema_version:"xinsight.connector-setup-consent.v1",request_id:"12345678-1234-4234-8234-123456789abc",expected_custody_revision:0,custody:settings}}));render(<ConnectorSetupPage session={session}/>);await screen.findByRole("button",{name:"确认设置并创建邀请"});await waitFor(()=>expect(save).not.toHaveBeenCalled());
});
it("preserves an existing disabled effective policy as explicit reuse",async()=>{
 const save=mocks();vi.spyOn(connectorSetupAPI,"preview").mockResolvedValue({...preview,custody:{desired_revision:4,effective_revision:4,settings:{...settings,mode:"disabled"}}});const user=userEvent.setup();render(<ConnectorSetupPage session={session}/>);expect(await screen.findByLabelText("复用当前生效设置")).toHaveProperty("checked",true);expect(screen.getByLabelText("正文复制模式")).toHaveProperty("value","disabled");await user.click(screen.getByRole("button",{name:"确认设置并创建邀请"}));await screen.findByRole("link",{name:"在 UniRouter 中继续"});expect(save.mock.calls[0][0]).toMatchObject({custody:null,expected_custody_revision:4});
});
