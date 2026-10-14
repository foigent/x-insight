import { act,cleanup,fireEvent,render,screen } from "@testing-library/react";
import { afterEach,beforeEach,expect,it,vi } from "vitest";
import { acceptScopeSession,scopeSessionKey,workActivityAPI,type AccessSessionResponse,type WorkActivityDetail,type WorkActivityScopes,type WorkActivityPage as Page } from "./api";
import { WorkActivityPage } from "./WorkActivityPage";
import * as body from "./workReviewBody";
const session={session:{subject_id:"reader",session_ref:"one",role:"workbench_reader",permissions:["workbench.details.read"]},scope_revision:"scope-one"} as AccessSessionResponse;
beforeEach(()=>{
 acceptScopeSession(session);
 const now=Date.now();window.history.replaceState({workActivityContext:{owner:scopeSessionKey(session),applied:{from:new Date(now-60_000).toISOString(),to:new Date(now).toISOString(),sourceIDs:[],projectIDs:[],resourceName:""},selected:{source:"source",instance:"instance",org:"7",project:"9",event:"event"}}},"",window.location.href);
 vi.spyOn(workActivityAPI,"scopes").mockResolvedValue({items:[{source_id:"source",source_label:"Fixture",source_instance:"instance",org_id:"7",project_id:"9",project_label:"Project"}]} as WorkActivityScopes);
 vi.spyOn(workActivityAPI,"list").mockResolvedValue({items:[]} as unknown as Page);
 vi.spyOn(workActivityAPI,"detail").mockResolvedValue({item:{event:{subject_kind:"message",action_kind:"message.sent_observed",lifecycle_state:"completed",occurred_at:new Date(now).toISOString(),actor:{kind:"unknown"},reporter:{source_system_id:"source",source_instance:"instance",org_id:"7",project_id:"9"},content_state:"referenced"}},review_bodies:[{kind:"user_output",ref:"body",sha256:"sha256:fixture",can_read:true,state:"available"}],relations:[]} as unknown as WorkActivityDetail);
});
afterEach(()=>{cleanup();acceptScopeSession(null);vi.restoreAllMocks();vi.useRealTimers();window.history.replaceState({},"",window.location.href);});
it.each([1000,90*86400_000])("clears displayed bytes only at the actual XI deadline (%s ms)",async duration=>{
 vi.spyOn(body,"fetchVerifiedWorkReviewBody").mockImplementation(async()=>({bytes:new Uint8Array([1]),text:"temporary synthetic body",previewOnly:false,expiresAt:new Date(Date.now()+duration).toISOString()}));
 render(<WorkActivityPage session={session} navigate={vi.fn()}/>);
 const button=await screen.findByRole("button",{name:"读取并核验正文"});vi.useFakeTimers();
 await act(async()=>fireEvent.click(button));expect(screen.getByText("temporary synthetic body")).toBeTruthy();
 let elapsed=0;
 if(duration>2147483647){elapsed=2147483647;await act(async()=>{await vi.advanceTimersByTimeAsync(elapsed);});expect(screen.getByText("temporary synthetic body")).toBeTruthy();}
 await act(async()=>{await vi.advanceTimersByTimeAsync(duration-elapsed+1);});
 expect(screen.queryByText("temporary synthetic body")).toBeNull();expect(screen.getByRole("alert").textContent).toMatch(/已过期/);
});
it("aborts an unmounted read and never displays its delayed result",async()=>{
 let complete!:(value:body.VerifiedWorkReviewBody)=>void;let signal!:AbortSignal;
 vi.spyOn(body,"fetchVerifiedWorkReviewBody").mockImplementation((_ref,_digest,_session,current)=>{signal=current;return new Promise(resolve=>{complete=resolve;});});
 const view=render(<WorkActivityPage session={session} navigate={vi.fn()}/>);
 fireEvent.click(await screen.findByRole("button",{name:"读取并核验正文"}));view.unmount();expect(signal.aborted).toBe(true);
 await act(async()=>complete({bytes:new Uint8Array([1]),text:"delayed synthetic body",previewOnly:false,expiresAt:new Date(Date.now()+60_000).toISOString()}));
 expect(screen.queryByText("delayed synthetic body")).toBeNull();
});
