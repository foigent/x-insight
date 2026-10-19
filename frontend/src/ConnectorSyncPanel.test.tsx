import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ConnectorSyncPanel } from "./ConnectorSyncPanel";
import { connectorSyncAPI, type ConnectorSyncStatus } from "./api";
import { setLocale } from "./i18n";

const id="xci_AAAAAAAAAAAAAAAAAAAAAA",other="xci_BBBBBBBBBBBBBBBBBBBBBB";
function state(installation=id,extra:Partial<ConnectorSyncStatus>={}):ConnectorSyncStatus{return {
 installation_id:installation,auto_sync:false,cadence_seconds:null,revision:1,last_request_id:"",
 effective:"quiet",peer:"unknown",last_checked_at:null,last_data_at:null,last_error:"",required_workers:[],workers:[],...extra,
};}
beforeEach(()=>{setLocale("en-US");sessionStorage.clear();});
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
