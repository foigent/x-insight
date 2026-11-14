import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ConnectorSyncPanel } from "./ConnectorSyncPanel";
import { connectorSetupAPI, connectorSyncAPI, type ConnectorSyncStatus } from "./api";
import { setLocale } from "./i18n";

const id="xci_AAAAAAAAAAAAAAAAAAAAAA",other="xci_BBBBBBBBBBBBBBBBBBBBBB";
function state(installation=id,extra:Partial<ConnectorSyncStatus>={}):ConnectorSyncStatus{return {
 installation_id:installation,auto_sync:false,cadence_seconds:null,revision:1,last_request_id:"",
 effective:"quiet",peer:"unknown",last_checked_at:null,last_data_at:null,last_error:"",required_workers:[],workers:[],...extra,
};}
function connection(installation_id=id,issues:Awaited<ReturnType<typeof connectorSetupAPI.status>>["issues"]=[]):Awaited<ReturnType<typeof connectorSetupAPI.status>> {return {schema_version:"xinsight.connector-setup-status.v1",installation_id,deployment_id:"xid_AAAAAAAAAAAAAAAAAAAAAA",consent_revision:1,state:"ready",custody:{desired_revision:1,effective_revision:1,settings:null,loaded:true},receiving:{last_success_at:null,profiles_received:[],content_received_at:null,content_read_verified_at:null},issues};}
beforeEach(()=>{setLocale("en-US");sessionStorage.clear();vi.spyOn(connectorSetupAPI,"status").mockResolvedValue(connection());});
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.useRealTimers();});

it("opening and refreshing reads only XI local status; auto and once are explicit",async()=>{
 const read=vi.spyOn(connectorSyncAPI,"status").mockResolvedValue(state());
 const set=vi.spyOn(connectorSyncAPI,"set").mockResolvedValue(state(id,{auto_sync:true,revision:2,cadence_seconds:60}));
 const once=vi.spyOn(connectorSyncAPI,"once").mockResolvedValue(state());
 render(<ConnectorSyncPanel installationID={id} csrfToken="csrf" canManage />);
 await screen.findByText(/XI outbound confirmed quiet/);
 expect(set).not.toHaveBeenCalled();expect(once).not.toHaveBeenCalled();
 fireEvent.click(screen.getByText("Refresh local status"));await waitFor(()=>expect(read).toHaveBeenCalledTimes(2));
 fireEvent.click(screen.getByLabelText("Auto sync"));await waitFor(()=>expect(set).toHaveBeenCalledTimes(1));
 expect(set.mock.calls[0][1]).toMatchObject({expected_revision:1,auto_sync:true,cadence_seconds:60});
 await waitFor(()=>expect((screen.getByText("Sync once") as HTMLButtonElement).disabled).toBe(true));
});

it("a lost once response retains the UUID/body through remount and replays it exactly",async()=>{
 vi.spyOn(connectorSyncAPI,"status").mockResolvedValue(state());
 const once=vi.spyOn(connectorSyncAPI,"once").mockRejectedValueOnce(new TypeError("response lost"))
  .mockResolvedValueOnce(state(id,{revision:2}));
 const first=render(<ConnectorSyncPanel installationID={id} csrfToken="csrf" canManage />);
 await screen.findByText(/XI outbound confirmed quiet/);fireEvent.click(screen.getByText("Sync once"));
 await screen.findByText("Confirm prior request");const original=once.mock.calls[0][1];first.unmount();
 render(<ConnectorSyncPanel installationID={id} csrfToken="csrf" canManage />);
 await screen.findByText(/XI outbound confirmed quiet/);await screen.findByText("Confirm prior request");
 fireEvent.click(screen.getByText("Confirm prior request"));await waitFor(()=>expect(once).toHaveBeenCalledTimes(2));
 expect(once.mock.calls[1][1]).toEqual(original);
 await waitFor(()=>expect(sessionStorage.getItem(`xinsight.sync.pending.${id}`)).toBeNull());
});

it("an initially running task keeps polling local status and settles after refresh",async()=>{
 vi.useFakeTimers();const read=vi.spyOn(connectorSyncAPI,"status")
  .mockResolvedValueOnce(state(id,{effective:"manual_running",run:{request_id:"r",state:"running",pages:1,attempts:2,items:0,deadline:"2026-10-19T00:00:00Z",results:{}}}))
  .mockResolvedValue(state());
 render(<ConnectorSyncPanel installationID={id} csrfToken="csrf" canManage />);
 await act(async()=>{await Promise.resolve();});expect(read).toHaveBeenCalledTimes(1);
 await act(async()=>{await vi.advanceTimersByTimeAsync(1500);});expect(read).toHaveBeenCalledTimes(2);
 expect(screen.getByText(/XI outbound confirmed quiet/)).toBeTruthy();
 await act(async()=>{await vi.advanceTimersByTimeAsync(5000);});expect(read).toHaveBeenCalledTimes(2);
});

it("changing installation discards the old view and late responses cannot overwrite it",async()=>{
 let resolve!:(v:ConnectorSyncStatus)=>void;
 vi.spyOn(connectorSyncAPI,"status").mockImplementation(async selected=>selected===id?new Promise(r=>{resolve=r;}):state(other,{auto_sync:true,effective:"auto_running"}));
 const view=render(<ConnectorSyncPanel installationID={id} csrfToken="csrf" canManage />);
 view.rerender(<ConnectorSyncPanel installationID={other} csrfToken="csrf" canManage />);
 await screen.findByText(/Auto sync requested/);
 await act(async()=>{resolve(state());await Promise.resolve();});
 expect(screen.queryByText(/XI outbound confirmed quiet/)).toBeNull();
 expect((screen.getByLabelText("Auto sync") as HTMLInputElement).checked).toBe(true);
});

it("returning to an installation cannot let an old success clear a newer pending request",async()=>{
 let resolve!:(v:ConnectorSyncStatus)=>void;
 const once=vi.spyOn(connectorSyncAPI,"once").mockImplementationOnce(()=>new Promise(r=>{resolve=r;}))
  .mockRejectedValueOnce(new TypeError("new response lost"));
 vi.spyOn(connectorSyncAPI,"status").mockImplementation(async selected=>
  state(selected,selected===id&&once.mock.calls.length?{revision:2,last_request_id:once.mock.calls[0][1].request_id}:{}));
 const view=render(<ConnectorSyncPanel installationID={id} csrfToken="csrf" canManage />);
 await screen.findByText(/XI outbound confirmed quiet/);
 fireEvent.click(screen.getByText("Sync once"));await waitFor(()=>expect(once).toHaveBeenCalledTimes(1));
 view.rerender(<ConnectorSyncPanel installationID={other} csrfToken="csrf" canManage />);
 await screen.findByText(/XI outbound confirmed quiet/);
 view.rerender(<ConnectorSyncPanel installationID={id} csrfToken="csrf" canManage />);
 await waitFor(()=>expect((screen.getByText("Sync once") as HTMLButtonElement).disabled).toBe(false));
 fireEvent.click(screen.getByText("Sync once"));await waitFor(()=>expect(once).toHaveBeenCalledTimes(2));
 await screen.findByText("Confirm prior request");
 const newer=once.mock.calls[1][1];expect(newer.request_id).not.toBe(once.mock.calls[0][1].request_id);
 await act(async()=>{resolve(state(id,{revision:2}));await Promise.resolve();});
 expect(screen.getByText("Confirm prior request")).toBeTruthy();
 expect(JSON.parse(sessionStorage.getItem(`xinsight.sync.pending.${id}`)!).body).toEqual(newer);
});

it.each(["en-US","zh-CN"] as const)("explains safe failure and partial results in %s without starting a sync",async locale=>{
 setLocale(locale);vi.spyOn(connectorSyncAPI,"status").mockResolvedValue(state(id,{last_error:"enrollment_list:authorization",run:{request_id:"r",state:"partial",pages:1,attempts:2,items:1,deadline:"2026-10-19T00:00:00Z",results:{projects:"error"}}}));
 const once=vi.spyOn(connectorSyncAPI,"once");render(<ConnectorSyncPanel installationID={id} csrfToken="csrf" canManage/>);
 await screen.findByText(locale==="en-US"?/The source refused access/:/来源拒绝访问/);
 expect(screen.getByText(locale==="en-US"?/partial result retained/:/部分结果已保留/)).toBeTruthy();
 expect(screen.getByText("enrollment_list:authorization").closest("details")).toBeTruthy();expect(once).not.toHaveBeenCalled();
});
it.each(["en-US","zh-CN"] as const)("identifies a completed-execution feed authorization failure in %s",async locale=>{
 setLocale(locale);
 vi.spyOn(connectorSyncAPI,"status").mockResolvedValue(state(id,{last_error:"feed_poll:authorization"}));
 render(<ConnectorSyncPanel installationID={id} csrfToken="csrf" canManage/>);
 await screen.findByText(locale==="en-US"?/reading the completed-execution feed.*source refused access/:/读取已完成执行的数据源.*来源拒绝访问/);
 expect(screen.getByText("feed_poll:authorization").closest("details")).toBeTruthy();
});
it.each(["unknown:unknown","machine_request:unconfirmed","multiple:failures","private token=secret","inspect:unknown::private-tail"])("does not invent a root cause for %s",async code=>{
 vi.spyOn(connectorSyncAPI,"status").mockResolvedValue(state(id,{last_error:code}));render(<ConnectorSyncPanel installationID={id} csrfToken="csrf" canManage/>);
 await screen.findByText(code==="multiple:failures"?/More than one synchronization step failed/:code==="machine_request:unconfirmed"?/outcome is unconfirmed/:/stage or cause is not established/);
 expect(screen.queryByText(/bad password|wrong credential/i)).toBeNull();if(code.includes("secret"))expect(document.body.textContent).not.toContain("secret");if(code.includes("private-tail"))expect(document.body.textContent).not.toContain("private-tail");
});

it("retains current worker failures after a later success clears the aggregate, without showing an old revision",async()=>{
 vi.spyOn(connectorSyncAPI,"status").mockResolvedValue(state(id,{revision:9,last_error:"",workers:[{worker:"activation",revision:8,busy:false,last_error:"source_descriptor:unavailable"},{worker:"projects",revision:9,busy:false,last_error:"enrollment_list:unavailable"},{worker:"source",revision:9,busy:false,last_error:""}],run:{request_id:"r",state:"partial",pages:1,attempts:2,items:1,deadline:"2026-10-19T00:00:00Z",results:{projects:"error",source:"done"}}}));
 render(<ConnectorSyncPanel installationID={id} csrfToken="csrf" canManage/>);await screen.findByText(/Current revision · project discovery/);
 expect(screen.getByText("enrollment_list:unavailable")).toBeTruthy();expect(screen.queryByText("source_descriptor:unavailable")).toBeNull();expect(screen.queryByText(/Current revision · connection activation/)).toBeNull();
});

it("shows a normal cancelled run and marks an unbound aggregate error as historical",async()=>{
 vi.spyOn(connectorSyncAPI,"status").mockResolvedValue(state(id,{revision:10,last_error:"enrollment_list:unavailable",run:{request_id:"r",state:"cancelled",pages:1,attempts:2,items:1,deadline:"2026-10-19T00:00:00Z",results:{}}}));
 render(<ConnectorSyncPanel installationID={id} csrfToken="csrf" canManage/>);
 await screen.findByText(/Last run: cancelled/);expect(screen.getByText(/association with the current revision has not been established/)).toBeTruthy();
});

it.each(["en-US","zh-CN"] as const)("%s shows expired control recovery before once without blocking other channels or starting recovery",async locale=>{
 setLocale(locale);vi.mocked(connectorSetupAPI.status).mockResolvedValue(connection(id,[{code:"control_identity_expired",requires_choice:true}]));
 vi.spyOn(connectorSyncAPI,"status").mockResolvedValue(state());const once=vi.spyOn(connectorSyncAPI,"once").mockResolvedValue(state());const recovery=vi.spyOn(connectorSetupAPI,"recover");
 render(<ConnectorSyncPanel installationID={id} csrfToken="csrf" canManage/>);
 await screen.findByText(locale==="en-US"?/other already authorized channels may still run/:/其它已获授权的通道可能仍可运行/);
 const link=screen.getByRole("link",{name:locale==="en-US"?"View connection status and recovery":"查看连接状态与恢复"});expect(link.getAttribute("href")).toBe(`/connectors/unirouter/setup?installation=${id}`);
 expect(once).not.toHaveBeenCalled();expect(recovery).not.toHaveBeenCalled();const button=screen.getByText(locale==="en-US"?"Sync once":"同步一次") as HTMLButtonElement;expect(button.disabled).toBe(false);fireEvent.click(button);await waitFor(()=>expect(once).toHaveBeenCalledTimes(1));expect(recovery).not.toHaveBeenCalled();
});
it("a failed credential read stays unknown and refresh is read only",async()=>{
 vi.mocked(connectorSetupAPI.status).mockRejectedValue(new TypeError("response lost"));vi.spyOn(connectorSyncAPI,"status").mockResolvedValue(state());const once=vi.spyOn(connectorSyncAPI,"once");const recovery=vi.spyOn(connectorSetupAPI,"recover");render(<ConnectorSyncPanel installationID={id} csrfToken="csrf" canManage/>);
 await screen.findByText(/Connection credential status has not been confirmed/);await screen.findByText(/XI outbound confirmed quiet/);fireEvent.click(screen.getByText("Refresh local status"));await waitFor(()=>expect(connectorSetupAPI.status).toHaveBeenCalledTimes(2));expect(once).not.toHaveBeenCalled();expect(recovery).not.toHaveBeenCalled();
});
it("ignores late credential status from another installation",async()=>{
 let resolve!:(value:Awaited<ReturnType<typeof connectorSetupAPI.status>>)=>void;
 vi.mocked(connectorSetupAPI.status).mockImplementation(async selected=>selected===id?new Promise(r=>{resolve=r;}):connection(other));
 vi.spyOn(connectorSyncAPI,"status").mockImplementation(async selected=>state(selected));const view=render(<ConnectorSyncPanel installationID={id} csrfToken="csrf" canManage/>);view.rerender(<ConnectorSyncPanel installationID={other} csrfToken="csrf" canManage/>);await screen.findByText(/XI outbound confirmed quiet/);
 await act(async()=>resolve(connection(id,[{code:"control_identity_expired",requires_choice:true}])));expect(screen.queryByText(/Activation and project updates need administrator recovery/)).toBeNull();expect(screen.getByRole("link").getAttribute("href")).toBe(`/connectors/unirouter/setup?installation=${other}`);
});
