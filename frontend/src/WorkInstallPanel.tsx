import { localizedLabels, getLocale, t, tx, useLocale, useMessageState } from "./i18n";
import { useEffect, useState, type FormEvent } from "react";
import { isAbortError, workInstallAPI, type AccessSessionResponse, type ReviewCaptureProjectCatalog,
  type WorkInstallInvitation, type WorkInstallMapping } from "./api";

interface Props { session: AccessSessionResponse; projects: ReviewCaptureProjectCatalog | undefined }
const sourceKey = (item: {source_system_id:string;source_instance:string;org_id:string;project_id:string}) =>
  JSON.stringify([item.source_system_id,item.source_instance,item.org_id,item.project_id]);
const stateLabel = (state: string) => ({invited:t("m2611"),candidates:t("m2612"),approved:t("m2613"),revoked:t("m2614"),expired:t("m2615")}[state] ?? t("m0965"));
const gapLabels: Record<string,string> = localizedLabels({}, {body_limit: "m2616", body_unavailable: "m2617", metadata_unavailable: "m2618", receiver_unavailable: "m2619", source_mapping_unconfirmed: "m2620", future_scope_unconfirmed: "m2621", renewal_unconfirmed: "m2622", ur_relation_unconfirmed: "m2623", outbox_full: "m2624", observation_unavailable: "m2625"});
function validURBase(value: string): boolean {
  try { const uri = new URL(value); return uri.protocol === "https:" && uri.pathname === "/v1" && !uri.username &&
    !uri.password && !uri.search && !uri.hash && !uri.hostname.endsWith("."); } catch { return false; }
}

export function WorkInstallPanel({ session, projects }: Props) {
  useLocale();
  const [items, setItems] = useState<WorkInstallInvitation[]>([]);
  const [nextCursor, setNextCursor] = useState<string>();
  const [selectedID, setSelectedID] = useState("");
  const [selected, setSelected] = useState<WorkInstallInvitation>();
  const [displayName, setDisplayName] = useState("Managed OpenClaw");
  const [urBase, setURBase] = useState("");
  const [mappings, setMappings] = useState<Record<string, WorkInstallMapping>>({});
  const [excluded, setExcluded] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState(false);
  const [approvalUnknown, setApprovalUnknown] = useState(false);
  const [futureAgentsAllowed, setFutureAgentsAllowed] = useState(true);
  const [error, setError] = useMessageState("");
  const [notice, setNotice] = useMessageState("");
  useEffect(() => {
    const controller = new AbortController();
    void workInstallAPI.list(controller.signal).then(response => { setItems(response.items ?? []); setNextCursor(response.next_cursor); })
      .catch(reason => { if (!isAbortError(reason)) setError(() => t("m2626")); });
    return () => controller.abort();
  }, [session.session.session_ref]);
  function selectInvitation(value: WorkInstallInvitation | undefined, preserveDraft = false) {
    setSelected(value);
    setApprovalUnknown(false);
    if (value?.state === "approved") setFutureAgentsAllowed(value.future_agents_allowed);
    else if (!preserveDraft) setFutureAgentsAllowed(true);
    const candidates = value?.state === "approved" ? value.future_candidates ?? [] : value?.projects ?? [];
    setExcluded(previous => Object.fromEntries(candidates.map(item => [item.project_key,
      preserveDraft ? previous[item.project_key] ?? false : false])));
    setMappings(previous => Object.fromEntries(candidates.map(item => [item.project_key,
      preserveDraft && previous[item.project_key] ? previous[item.project_key] :
        {project_key:item.project_key,xi_project_name:item.label} as WorkInstallMapping])));
  }
  async function refresh() {
    setBusy(true); setError(() => "");
    try {
      const response = await workInstallAPI.list(); setItems(response.items ?? []); setNextCursor(response.next_cursor);
      if (selectedID) selectInvitation((await workInstallAPI.read(selectedID)).invitation, true);
    } catch { setError(() => t("m2627")); }
    finally { setBusy(false); }
  }
  async function loadMore() {
    if (!nextCursor) return;
    setBusy(true); setError(() => "");
    try {
      const response = await workInstallAPI.list(undefined, nextCursor);
      setItems(previous => [...new Map([...previous, ...(response.items ?? [])].map(item => [item.invitation_id, item])).values()]);
      setNextCursor(response.next_cursor);
    } catch { setError(() => t("m2628")); }
    finally { setBusy(false); }
  }
  async function issue(event: FormEvent) {
    event.preventDefault();
    if (!validURBase(urBase) || window.location.protocol !== "https:") {
      setError(() => t("m2629")); return;
    }
    setBusy(true); setError(() => ""); setNotice(() => "");
    try {
      const response = await workInstallAPI.issue(displayName.trim(),3600,session.csrf_token);
      const invitation = response.invitation;
      if (!invitation?.invitation_token) throw new Error("missing one-time token");
      const payload = {schema_version:"xinsight.openclaw-invitation.v1", invitation_id:invitation.invitation_id,
        invitation_token:invitation.invitation_token,origin:window.location.origin,
        ur_base_url:urBase,expires_at:invitation.expires_at};
      const url=URL.createObjectURL(new Blob([JSON.stringify(payload,null,2)+"\n"],{type:"application/json"}));
      const anchor=document.createElement("a"); anchor.href=url;
      anchor.download=`xinsight-openclaw-invitation-${invitation.invitation_id}.json`;
      document.body.appendChild(anchor);anchor.click();anchor.remove();
      setTimeout(()=>URL.revokeObjectURL(url),1000);
      setSelectedID(invitation.invitation_id);selectInvitation({...invitation,invitation_token:undefined});
      setItems(previous=>[{...invitation,invitation_token:undefined},...previous]);
      setNotice(() => t("m2630"));
    } catch { setError(() => t("m2631")); }
    finally { setBusy(false); }
  }
  async function read(id: string) {
    if (!id) return;
    setBusy(true);setError(() => "");setSelectedID(id);
    try { const result=await workInstallAPI.read(id);selectInvitation(result.invitation);
    } catch { setError(() => t("m2632")); }
    finally { setBusy(false); }
  }
  const updateMapping = (key:string,change:Partial<WorkInstallMapping>) => setMappings(previous=>({
    ...previous,[key]:{...(previous[key] ?? {project_key:key}),...change},
  }));
  const candidates = selected?.state === "approved" ? selected.future_candidates ?? [] : selected?.projects ?? [];
  async function approve(event:FormEvent) {
    event.preventDefault();
    if (!selected || !["candidates","approved"].includes(selected.state) || candidates.length===0) return;
    const excludedKeys=candidates.filter(item=>excluded[item.project_key]).map(item=>item.project_key);
    const complete=candidates.filter(item=>!excluded[item.project_key]).map(item=>mappings[item.project_key]);
    if (selected.state==="candidates" && complete.length===0) {
      setError(() => t("m2633")); return;
    }
    if (complete.some(item=>!item || Boolean(item.xi_project_id)===Boolean(item.xi_project_name))) {
      setError(() => t("m2634")); return;
    }
    setBusy(true);setError(() => "");setNotice(() => "");
    try {
      const future=selected.state==="approved";
      const result=future
        ? await workInstallAPI.decideFuture(selected.invitation_id,complete,excludedKeys,session.csrf_token)
        : await workInstallAPI.approve(selected.invitation_id,complete,excludedKeys,futureAgentsAllowed,session.csrf_token);
      if (result.state!=="approved" || result.invitation?.state!=="approved") throw new Error("approval not confirmed");
      selectInvitation(result.invitation);
      setNotice(() => future
        ? t("m2635")
        : t("m2636"));
    } catch { setApprovalUnknown(true);setError(() => t("m2637")); }
    finally { setBusy(false); }
  }
  const invitationFile = selected ? `xinsight-openclaw-invitation-${selected.invitation_id}.json` : "xi-invitation.json";
  const installCommand = t("m2638", { value0: invitationFile });
  const targets=(projects?.items ?? []).filter(item=>item.source_type==="unirouter");
  return <section aria-label={t("m2639")}><h2>{tx("m2639")}</h2>
    <p>{tx("m2640")}</p>
    {error?<p role="alert">{error}</p>:null}{notice?<p role="status">{notice}</p>:null}
    <form onSubmit={event=>void issue(event)}><h3>{tx("m2641")}</h3>
      <label>{t("m2642")}<input required maxLength={128} value={displayName} disabled={busy}
        onChange={event=>setDisplayName(event.target.value)} /></label>
      <label>{t("m2643")}<input required type="url" placeholder="https://ur.example/v1"
        value={urBase} disabled={busy} onChange={event=>setURBase(event.target.value)} /></label>
      <button type="submit" disabled={busy||!validURBase(urBase)}>{tx("m2644")}</button>
      <p>{tx("m2645")}</p>
    </form>
    {selected?<section aria-label={t("m2646")}><h3>{tx("m2647")}</h3>
      <p>{tx("m2648")}</p>
      <pre><code>{installCommand}</code></pre>
      <p>{tx("m2649")}</p>
    </section>:null}
    <h3>{tx("m2650")}</h3><button type="button" disabled={busy} onClick={()=>void refresh()}>{tx("m2651")}</button>
    <ul>{items.map(item=><li key={item.invitation_id}>
      <button type="button" disabled={busy} onClick={()=>void read(item.invitation_id)}>{item.display_name}</button>
      {" "}· {stateLabel(item.state)}</li>)}</ul>
    {nextCursor?<button type="button" disabled={busy} onClick={()=>void loadMore()}>{tx("m2652")}</button>:null}
    {selected?<section aria-label={t("m2653")}><h3>{selected.display_name}</h3>
      <p>{tx("m2654", { value0: stateLabel(selected.state), value1: selected.projects?.length??0 })}</p>
      {selected.state==="approved"?<section aria-label={t("m2655")}><h4>{tx("m2655")}</h4>
        {selected.latest_status && selected.status_received_at?<>
          <p>{tx("m2656", { value0: new Date(selected.status_received_at).toLocaleString(getLocale()) })}</p>
          {Date.now()-Date.parse(selected.status_received_at)>180000?<p role="status">{tx("m2657")}</p>:null}
          <p>{tx("m2658", { value0: selected.latest_status.metadata_pending, value1: selected.latest_status.metadata_blocked, value2: selected.latest_status.body_pending })}</p>
          {selected.latest_status.gaps.length?<><p>{tx("m2659")}</p>
            <ul>{selected.latest_status.gaps.map(gap=><li key={gap.code}>{tx("m2661", { value0: gapLabels[gap.code] ?? t("m2660"), value1: gap.occurrences })}</li>)}</ul></>:<p>{tx("m2662")}</p>}
        </>:<p>{tx("m2663")}</p>}
      </section>:null}
      <details><summary>{tx("m2664")}</summary><p>{tx("m2665", { value0: selected.invitation_id, value1: selected.source_instance, value2: selected.expires_at })}</p></details>
      {candidates.length>0 && ["candidates","approved"].includes(selected.state)?<form onSubmit={event=>void approve(event)}>
        {selected.state==="approved"?<h4>{tx("m2666")}</h4>:null}
        <p>{tx("m2667")}</p>
        {candidates.map(item=>{const mapping=mappings[item.project_key]??{project_key:item.project_key};return <fieldset key={item.project_key}>
          <legend>{item.label}</legend>
          <label><input type="checkbox" checked={!excluded[item.project_key]} disabled={busy}
            onChange={event=>setExcluded(previous=>({...previous,[item.project_key]:!event.target.checked}))} />{t("m2668")}{item.label}</label>
          <fieldset disabled={busy||excluded[item.project_key]}>
          <label>{t("m1843")}<select disabled={busy} value={mapping.xi_project_id??"__new__"} onChange={event=>{
            const id=event.target.value;
            updateMapping(item.project_key,id==="__new__"?{xi_project_id:undefined,xi_project_name:item.label}:{xi_project_id:id,xi_project_name:undefined});
          }}><option value="__new__">{tx("m2669")}</option>{projects?.projects.map(project=><option key={project.id} value={project.id}>{project.display_name}</option>)}</select></label>
          {!mapping.xi_project_id?<label>{t("m1845")}<input required maxLength={128} disabled={busy}
            value={mapping.xi_project_name??""} onChange={event=>updateMapping(item.project_key,{xi_project_name:event.target.value})} /></label>:null}
          <label>{t("m2670")}<select disabled={busy} value={mapping.ur_target?sourceKey(mapping.ur_target):""}
            onChange={event=>{const target=targets.find(candidate=>sourceKey(candidate)===event.target.value);
              updateMapping(item.project_key,{ur_target:target?{source_system_id:target.source_system_id,
                source_instance:target.source_instance,org_id:target.org_id,project_id:target.project_id}:undefined});}}>
            <option value="">{tx("m2671")}</option>{targets.map(target=><option key={sourceKey(target)} value={sourceKey(target)}>
              {target.source_name} · {target.xi_project_name || t("m1154", { value0: target.project_id })}</option>)}</select></label>
          </fieldset>
        </fieldset>})}
        {selected.state==="candidates"?<><label><input type="checkbox" checked={futureAgentsAllowed} disabled={busy}
          onChange={event=>setFutureAgentsAllowed(event.target.checked)} />{t("m2672")}</label>
        <p>{tx("m2673")}</p></>:null}
        <button type="submit" disabled={busy||approvalUnknown}>{selected.state==="approved"?t("m2674"):t("m2675")}</button>
      </form>:null}
      {selected.state==="approved"?<p>{tx("m2678", { value0: selected.future_agents_allowed?t("m2676"):t("m2677") })}</p>:null}
      {selected.state==="approved" && Boolean(selected.excluded_future_project_keys?.length)?<p>{tx("m2679", { value0: selected.excluded_future_project_keys?.length })}</p>:null}
      {selected.state==="approved"?<ul>{selected.bindings?.map(item=><li key={item.project_key}>
        {item.project_label} → {item.xi_project_name}；
        {item.ur_target?t("m2680"):t("m2681")}</li>)}</ul>:null}
    </section>:null}
  </section>;
}
