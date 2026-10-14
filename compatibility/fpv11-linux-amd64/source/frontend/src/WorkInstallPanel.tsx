import { useEffect, useState, type FormEvent } from "react";
import { isAbortError, workInstallAPI, type AccessSessionResponse, type ReviewCaptureProjectCatalog,
  type WorkInstallInvitation, type WorkInstallMapping } from "./api";

interface Props { session: AccessSessionResponse; projects: ReviewCaptureProjectCatalog | undefined }
const sourceKey = (item: {source_system_id:string;source_instance:string;org_id:string;project_id:string}) =>
  JSON.stringify([item.source_system_id,item.source_instance,item.org_id,item.project_id]);
const stateLabel = (state: string) => ({invited:"等待主机登记",candidates:"等待确认项目",approved:"项目已授权",revoked:"连接已撤销",expired:"邀请已过期"}[state] ?? "状态待确认");
const gapLabels: Record<string,string> = {
  body_limit:"正文超过单条上限", body_unavailable:"正文副本未完成", metadata_unavailable:"活动发送未完成",
  receiver_unavailable:"与 XI 的连接未确认", source_mapping_unconfirmed:"项目归属未确认",
  future_scope_unconfirmed:"新增 Agent 接入未确认", renewal_unconfirmed:"连接凭据续期未确认",
  ur_relation_unconfirmed:"模型请求关联未确认", outbox_full:"本机发送队列已满", observation_unavailable:"其他采集步骤未完成",
};
function validURBase(value: string): boolean {
  try { const uri = new URL(value); return uri.protocol === "https:" && uri.pathname === "/v1" && !uri.username &&
    !uri.password && !uri.search && !uri.hash && !uri.hostname.endsWith("."); } catch { return false; }
}

export function WorkInstallPanel({ session, projects }: Props) {
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
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    void workInstallAPI.list(controller.signal).then(response => { setItems(response.items ?? []); setNextCursor(response.next_cursor); })
      .catch(reason => { if (!isAbortError(reason)) setError("待安装列表暂不可读。"); });
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
    setBusy(true); setError("");
    try {
      const response = await workInstallAPI.list(); setItems(response.items ?? []); setNextCursor(response.next_cursor);
      if (selectedID) selectInvitation((await workInstallAPI.read(selectedID)).invitation, true);
    } catch { setError("安装状态暂不可读；不要重发邀请或批准操作，先读回。"); }
    finally { setBusy(false); }
  }
  async function loadMore() {
    if (!nextCursor) return;
    setBusy(true); setError("");
    try {
      const response = await workInstallAPI.list(undefined, nextCursor);
      setItems(previous => [...new Map([...previous, ...(response.items ?? [])].map(item => [item.invitation_id, item])).values()]);
      setNextCursor(response.next_cursor);
    } catch { setError("更多安装暂不可读，已有列表仍保留。"); }
    finally { setBusy(false); }
  }
  async function issue(event: FormEvent) {
    event.preventDefault();
    if (!validURBase(urBase) || window.location.protocol !== "https:") {
      setError("请输入准确的 UniRouter HTTPS /v1 地址，并从 XI HTTPS 页面操作。"); return;
    }
    setBusy(true); setError(""); setNotice("");
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
      setNotice("连接文件已下载。请在受管 OpenClaw 主机上运行下方安装命令，然后刷新本页确认项目。安装程序会自动收紧所选文件的读取权限。");
    } catch { setError("邀请结果未确认。先刷新列表核对是否已创建；已有邀请的 token 不能在页面重取。"); }
    finally { setBusy(false); }
  }
  async function read(id: string) {
    if (!id) return;
    setBusy(true);setError("");setSelectedID(id);
    try { const result=await workInstallAPI.read(id);selectInvitation(result.invitation);
    } catch { setError("该安装状态暂不可读。"); }
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
      setError("首次接入请至少选择一个 Agent。"); return;
    }
    if (complete.some(item=>!item || Boolean(item.xi_project_id)===Boolean(item.xi_project_name))) {
      setError("请为选中的 Agent 选择所属项目；新建项目已按 Agent 名称预填，可直接确认。"); return;
    }
    setBusy(true);setError("");setNotice("");
    try {
      const future=selected.state==="approved";
      const result=future
        ? await workInstallAPI.decideFuture(selected.invitation_id,complete,excludedKeys,session.csrf_token)
        : await workInstallAPI.approve(selected.invitation_id,complete,excludedKeys,futureAgentsAllowed,session.csrf_token);
      if (result.state!=="approved" || result.invitation?.state!=="approved") throw new Error("approval not confirmed");
      selectInvitation(result.invitation);
      setNotice(future
        ? "新增 Agent 的选择已保存。主机会在下一次配置同步后接入选中的项目；排除项不会反复申请。"
        : "项目已授权。请在原主机再次运行同一安装命令以完成接通；收到活动后，XI 才能展示实际采集覆盖。");
    } catch { setApprovalUnknown(true);setError("批准结果未确认；请读回同一邀请，勿立即重复批准。"); }
    finally { setBusy(false); }
  }
  const invitationFile = selected ? `xinsight-openclaw-invitation-${selected.invitation_id}.json` : "xi-invitation.json";
  const installCommand = `node ./install.mjs --invitation-file "/连接文件目录/${invitationFile}" --state-dir "/专用数据目录/xi-openclaw"`;
  const targets=(projects?.items ?? []).filter(item=>item.source_type==="unirouter");
  return <section aria-label="受管 OpenClaw 安装"><h2>受管 OpenClaw 安装</h2>
    <p>将已有 OpenClaw 工作活动接入 XI。保留原有模型、API Key 和调用方式，由管理员确认 Agent 的项目归属。</p>
    {error?<p role="alert">{error}</p>:null}{notice?<p role="status">{notice}</p>:null}
    <form onSubmit={event=>void issue(event)}><h3>1. 下载连接文件</h3>
      <label>连接名称 <input required maxLength={128} value={displayName} disabled={busy}
        onChange={event=>setDisplayName(event.target.value)} /></label>
      <label>UR 网关 API 地址 <input required type="url" placeholder="https://ur.example/v1"
        value={urBase} disabled={busy} onChange={event=>setURBase(event.target.value)} /></label>
      <button type="submit" disabled={busy||!validURBase(urBase)}>下载连接文件</button>
      <p>连接文件用于首次登记，一小时内有效；此期限不代表已安装连接会在一小时后停止。</p>
    </form>
    {selected?<section aria-label="主机安装步骤"><h3>2. 在受管主机安装</h3>
      <p>进入已提供的 XI 接入组件目录，将下方两个目录替换为本机实际路径，再运行命令。专用数据目录应在 Agent 工作目录之外。</p>
      <pre><code>{installCommand}</code></pre>
      <p>首次运行登记已有 Agent。完成下方项目确认后，再运行同一命令即可继续；不需要复制 API Key 或重新创建连接。</p>
    </section>:null}
    <h3>3. 确认项目归属</h3><button type="button" disabled={busy} onClick={()=>void refresh()}>刷新安装状态</button>
    <ul>{items.map(item=><li key={item.invitation_id}>
      <button type="button" disabled={busy} onClick={()=>void read(item.invitation_id)}>{item.display_name}</button>
      {" "}· {stateLabel(item.state)}</li>)}</ul>
    {nextCursor?<button type="button" disabled={busy} onClick={()=>void loadMore()}>加载更多安装</button>:null}
    {selected?<section aria-label="安装候选详情"><h3>{selected.display_name}</h3>
      <p>{stateLabel(selected.state)} · 已发现 {selected.projects?.length??0} 个 Agent。</p>
      {selected.state==="approved"?<section aria-label="最近采集状态"><h4>最近采集状态</h4>
        {selected.latest_status && selected.status_received_at?<>
          <p>XI 最近收到状态：{new Date(selected.status_received_at).toLocaleString("zh-CN")}。</p>
          {Date.now()-Date.parse(selected.status_received_at)>180000?<p role="status">上报已超过 3 分钟，当前连接状态待确认；请检查主机和网络。</p>:null}
          <p>待发送活动 {selected.latest_status.metadata_pending} 条，受阻活动 {selected.latest_status.metadata_blocked} 条，待发送正文 {selected.latest_status.body_pending} 份。</p>
          {selected.latest_status.gaps.length?<><p>主机曾报告以下缺口；累计报告次数不代表丢失事件数，也不表示问题仍在持续。</p>
            <ul>{selected.latest_status.gaps.map(gap=><li key={gap.code}>{gapLabels[gap.code] ?? "其他采集缺口"}：{gap.occurrences} 次</li>)}</ul></>:<p>最近状态中没有报告已知缺口；这不证明所有活动都已被采集。</p>}
        </>:<p>尚未收到主机状态；项目获批不等于采集已经运行。</p>}
      </section>:null}
      <details><summary>连接技术信息</summary><p>编号 {selected.invitation_id}；来源实例 {selected.source_instance}；首次登记截止 {selected.expires_at}。</p></details>
      {candidates.length>0 && ["candidates","approved"].includes(selected.state)?<form onSubmit={event=>void approve(event)}>
        {selected.state==="approved"?<h4>新增 Agent 等待确认</h4>:null}
        <p>默认接入所有已发现的 Agent；取消勾选即可排除，无需填写内部编号。</p>
        {candidates.map(item=>{const mapping=mappings[item.project_key]??{project_key:item.project_key};return <fieldset key={item.project_key}>
          <legend>{item.label}</legend>
          <label><input type="checkbox" checked={!excluded[item.project_key]} disabled={busy}
            onChange={event=>setExcluded(previous=>({...previous,[item.project_key]:!event.target.checked}))} />接入 {item.label}</label>
          <fieldset disabled={busy||excluded[item.project_key]}>
          <label>XI 项目 <select disabled={busy} value={mapping.xi_project_id??"__new__"} onChange={event=>{
            const id=event.target.value;
            updateMapping(item.project_key,id==="__new__"?{xi_project_id:undefined,xi_project_name:item.label}:{xi_project_id:id,xi_project_name:undefined});
          }}><option value="__new__">新建项目</option>{projects?.projects.map(project=><option key={project.id} value={project.id}>{project.display_name}</option>)}</select></label>
          {!mapping.xi_project_id?<label>新项目名称 <input required maxLength={128} disabled={busy}
            value={mapping.xi_project_name??""} onChange={event=>updateMapping(item.project_key,{xi_project_name:event.target.value})} /></label>:null}
          <label>关联的 UR 项目（可选） <select disabled={busy} value={mapping.ur_target?sourceKey(mapping.ur_target):""}
            onChange={event=>{const target=targets.find(candidate=>sourceKey(candidate)===event.target.value);
              updateMapping(item.project_key,{ur_target:target?{source_system_id:target.source_system_id,
                source_instance:target.source_instance,org_id:target.org_id,project_id:target.project_id}:undefined});}}>
            <option value="">暂不关联，仍保存工作活动</option>{targets.map(target=><option key={sourceKey(target)} value={sourceKey(target)}>
              {target.source_name} · {target.xi_project_name || `项目 ${target.project_id}`}</option>)}</select></label>
          </fieldset>
        </fieldset>})}
        {selected.state==="candidates"?<><label><input type="checkbox" checked={futureAgentsAllowed} disabled={busy}
          onChange={event=>setFutureAgentsAllowed(event.target.checked)} />自动接入这台主机今后新增的 Agent</label>
        <p>新增 Agent 会获得独立的工作活动记录。只有能确认关联项目时，才连接到对应 UR 请求；不会自动扩大 UR 授权。</p></>:null}
        <button type="submit" disabled={busy||approvalUnknown}>{selected.state==="approved"?"保存新增 Agent 的选择":"确认所列项目并授权接入"}</button>
      </form>:null}
      {selected.state==="approved"?<p>后续新增 Agent：{selected.future_agents_allowed?"按本次授权自动接入":"等待管理员确认"}。</p>:null}
      {selected.state==="approved" && Boolean(selected.excluded_future_project_keys?.length)?<p>此连接已排除 {selected.excluded_future_project_keys?.length} 个 Agent；不会自动采集其活动。</p>:null}
      {selected.state==="approved"?<ul>{selected.bindings?.map(item=><li key={item.project_key}>
        {item.project_label} → {item.xi_project_name}；
        {item.ur_target?"已选择 UR 关联项目":"工作活动可采集，模型关联尚未配置"}</li>)}</ul>:null}
    </section>:null}
  </section>;
}
