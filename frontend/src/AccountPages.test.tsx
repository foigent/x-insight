import { StrictMode } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { AccountPasswordPage, AccountsPage } from "./AccountPages";
import { App } from "./App";
import { accessAPI, managedAccountsAPI, type AccessSessionResponse, type ManagedAccount, type ProjectReadOptions } from "./api";
import { setLocale } from "./i18n";

const owner: AccessSessionResponse = {session:{subject_id:"xs_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",session_ref:"xr_synthetic_account_owner",role:"platform_admin",permissions:["accounts.manage","access.sessions.read","workbench.summary.read","workbench.details.read","workbench.coverage.read","evidence.read"],created_at:"2026-11-02T00:00:00Z",last_seen_at:"2026-11-02T00:00:00Z",idle_expires_at:"2026-11-02T01:00:00Z",absolute_expires_at:"2026-11-02T08:00:00Z"},csrf_token:"synthetic-csrf",scope_revision:"no_authorized_scope"};
const accounts:ManagedAccount[]=[{subject_id:owner.session.subject_id,username:"owner",role:"platform_admin",disabled:false,needs_activation:false,revision:1},{subject_id:"xs_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",username:"reader",role:"workbench_investigation_reader",disabled:false,needs_activation:false,revision:1}];
const scopes:ProjectReadOptions={sources:[{id:"11111111-1111-1111-1111-111111111111",name:"Synthetic source"}],projects:[{source_system_id:"11111111-1111-1111-1111-111111111111",source_project_ref:"project-2"}],grants:[{id:"22222222-2222-2222-2222-222222222222",subject_id:accounts[1].subject_id,source_system_id:"11111111-1111-1111-1111-111111111111",source_project_ref:"project-2",cost_project_ref:null,revision:1,active:true,allow_details:true,allow_coverage:true,allow_source_context:true}]};
beforeEach(()=>{setLocale("en-US");window.history.replaceState(null,"","/access/accounts");vi.spyOn(managedAccountsAPI,"list").mockResolvedValue({items:accounts});vi.spyOn(managedAccountsAPI,"grantOptions").mockResolvedValue(scopes);vi.spyOn(accessAPI,"session").mockResolvedValue(owner)});
afterEach(()=>{cleanup();vi.restoreAllMocks()});

it("keeps owner management usable with no project grant",async()=>{
 render(<App/>);expect(await screen.findByRole("heading",{name:"Accounts"})).toBeTruthy();expect(screen.getByRole("heading",{name:"Project reading access"})).toBeTruthy();expect(screen.queryByText("No authorized scope")).toBeNull();
});
it("creates a holder activation link and clears the owner password",async()=>{
 const change=vi.spyOn(managedAccountsAPI,"change").mockResolvedValue({account:accounts[1],activation_link:"https://x-insight.local/access/activate#xi-ac1_synthetic",expires_at:"2026-11-02T01:00:00Z"});
 render(<AccountsPage session={owner}/>);await screen.findByRole("option",{name:"reader"});fireEvent.change(screen.getByLabelText("Your current password"),{target:{value:"synthetic owner confirmation"}});expect(new RegExp((screen.getByLabelText("Username") as HTMLInputElement).pattern,"v").test("browser.reader")).toBe(true);fireEvent.change(screen.getByLabelText("Username"),{target:{value:"another.reader"}});fireEvent.click(screen.getByRole("button",{name:"Create activation link"}));await screen.findByRole("heading",{name:"One-time activation link"});expect(change).toHaveBeenCalledWith(expect.objectContaining({action:"invite",username:"another.reader",role:"workbench_investigation_reader"}),"synthetic-csrf");expect((screen.getByLabelText("Your current password") as HTMLInputElement).value).toBe("");
});
it("previews an owner grant without copying reader fields, then requires explicit confirmation",async()=>{
 const grant=vi.spyOn(managedAccountsAPI,"grant").mockResolvedValue({check_only:true,changed:false});render(<AccountsPage session={owner}/>);await screen.findByRole("option",{name:"reader"});fireEvent.change(screen.getByLabelText("Source"),{target:{value:scopes.sources[0].id}});fireEvent.change(screen.getByLabelText("Project"),{target:{value:"project-2"}});expect((screen.getByLabelText("Context metadata") as HTMLInputElement).checked).toBe(false);fireEvent.click(screen.getByRole("button",{name:"Preview access"}));await screen.findByRole("heading",{name:"Confirm this project and these fields"});expect(grant).toHaveBeenCalledTimes(1);expect(grant.mock.calls[0][0]).toEqual(expect.objectContaining({subject_id:owner.session.subject_id,expected_revision:0,check_only:true,capabilities:["details","coverage"]}));expect((screen.getByRole("button",{name:"Confirm reviewed change"}) as HTMLButtonElement).disabled).toBe(true);fireEvent.change(screen.getByLabelText("Your current password"),{target:{value:"synthetic owner confirmation"}});fireEvent.click(screen.getByRole("button",{name:"Confirm reviewed change"}));await waitFor(()=>expect(grant).toHaveBeenCalledTimes(2));expect(grant.mock.calls[1][0].check_only).toBe(false);expect(grant.mock.calls[1][0].grant_id).toBe(grant.mock.calls[0][0].grant_id);
});
it("takes the one-use token out of the URL and lets its holder set their own password",async()=>{
 const token="xi-ac1_"+"A".repeat(43);window.history.replaceState(null,"","/access/activate#"+token);const save=vi.spyOn(managedAccountsAPI,"setPassword").mockResolvedValue({username:"reader",status:"password_set_sign_in_required"});const storage=vi.spyOn(Storage.prototype,"setItem");render(<StrictMode><AccountPasswordPage/></StrictMode>);expect(window.location.hash).toBe("");fireEvent.change(screen.getByLabelText("New password (at least 12 characters)"),{target:{value:"synthetic holder password"}});fireEvent.change(screen.getByLabelText("Confirm new password"),{target:{value:"synthetic holder password"}});fireEvent.click(screen.getByRole("button",{name:"Set password"}));await screen.findByText("Your password has been set. Sign in to continue.");expect(save).toHaveBeenCalledWith(token,"synthetic holder password","synthetic holder password");expect(storage.mock.calls.some(call=>JSON.stringify(call).includes(token))).toBe(false);expect(JSON.stringify(window.history.state)).not.toContain(token);
});
it("switches the account page to Chinese without changing entered scope",async()=>{
 render(<AccountsPage session={owner}/>);await screen.findByRole("option",{name:"reader"});fireEvent.change(screen.getByLabelText("Source"),{target:{value:scopes.sources[0].id}});fireEvent.change(screen.getByLabelText("Project"),{target:{value:"project-2"}});act(()=>setLocale("zh-CN"));expect(screen.getByRole("heading",{name:"账号"})).toBeTruthy();expect((screen.getByLabelText("项目") as HTMLSelectElement).value).toBe("project-2");
});

it("distinguishes identically named sources before explicit scope approval",async()=>{
 const another="33333333-3333-4333-8333-333333333333";
 vi.spyOn(managedAccountsAPI,"grantOptions").mockResolvedValue({...scopes,sources:[scopes.sources[0],{id:another,name:scopes.sources[0].name}]});
 render(<AccountsPage session={owner}/>);
 await screen.findByRole("option",{name:`Synthetic source · ${another}`});
 expect(screen.getByRole("option",{name:`Synthetic source · ${scopes.sources[0].id}`})).toBeTruthy();
 expect((screen.getByLabelText("Source") as HTMLSelectElement).value).toBe("");
});
