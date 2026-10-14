import { scheduleBodyExpiry } from "./bodyExpiry";
import { useEffect, useRef, useState } from "react";
import { currentScopeSessionKey, scopeSessionKey, runtimeAPI, isAbortError, workActivityAPI, type AccessSessionResponse, type WorkActivityDetail, type WorkActivityPage as ActivityPage, type WorkActivityScopes } from "./api";
import { fetchVerifiedWorkReviewBody, type VerifiedWorkReviewBody } from "./workReviewBody";
import { navigationOwner, type RuntimeNavigation } from "./workbenchNavigation";
import type { ReadingRequest } from "./runtime";

type Props = { session: AccessSessionResponse; navigate: (path: string, selectedRuntimeRequestRef?: string, activityReading?: RuntimeNavigation) => void };
type Selected = { source: string; instance: string; org: string; project: string; event: string };

const lifecycle: Record<string,string> = { started: "来源报告已开始", completed: "来源报告完成", failed: "来源报告失败", unknown: "来源状态未知" };
const content: Record<string,string> = { not_captured: "来源未报告正文引用", pending: "来源报告正文引用待到", referenced: "来源报告了正文引用；下方另示 XI 复查副本状态" };
const kinds: Record<string,string> = { task: "任务", message: "消息观察", model_call: "模型活动", tool_call: "工具调用", resource_action: "资源操作" };
const actions: Record<string,string> = { "task.started": "开始任务", "task.completed": "完成任务", "message.received_observed": "观察到收到消息（任务未关联）", "message.sent_observed": "观察到发送消息（不代表任务完成）", "model.requested": "请求模型", "model.started": "开始模型调用", "model.completed": "模型返回", "model.input_observed": "观察到模型输入（调用未关联，非网络原始请求）", "model.output_observed": "观察到模型输出（调用未关联，非网络原始响应）", "tool.invoked": "调用工具", "tool.completed": "工具返回", "resource.read": "读取资源", "resource.write": "写入资源", "resource.edit": "编辑资源", "resource.read.failed": "读取资源失败", "resource.write.failed": "写入资源失败", "resource.edit.failed": "编辑资源失败" };
const bodyStates: Record<string,string> = { source_reference_only: "来源有正文引用；XI 尚未持有复查副本", content_not_authorized: "当前账户没有正文读取权限", pending: "XI 正在保存复查副本", available: "XI 已保存可核验的复查副本", expired: "XI 复查副本已过期", gap: "XI 复查副本未完成", copy_missing: "XI 副本缺失，需检查完整性" };
const bodyKinds: Record<string,string> = { user_input: "框架报告的用户输入", model_input: "框架报告的模型输入", model_output: "框架报告的模型输出", tool_input: "框架报告的工具参数", tool_output: "框架报告的工具返回", user_output: "框架报告已发送的用户回复" };
const actorLabel = (actor: { kind: string; ref?: string }) => actor.kind === "unknown" ? "身份未知" : `${actor.ref ?? "身份未提供"}（${{ human: "人员", agent: "代理", service: "服务" }[actor.kind as "human" | "agent" | "service"] ?? actor.kind}）`;
const activityLabel = (kind: string, action: string) => actions[action] ?? `${kinds[kind] ?? "工作"}活动`;

function initialActivityRange() {
  const params = new URLSearchParams(window.location.search);
  const from = params.get("from") ?? "";
  const to = params.get("to") ?? "";
  const sourceIDs = (params.get("source_ids") ?? "").split(",").filter(Boolean);
  const projectIDs = (params.get("project_ids") ?? "").split(",").filter(Boolean);
  const validSources = sourceIDs.length <= 16 && sourceIDs.every(id => /^[0-9a-f-]{36}$/.test(id));
  const validProjects = projectIDs.length <= 16 && projectIDs.every(id => /^[1-9][0-9]{0,18}$/.test(id));
  const duration = Date.parse(to) - Date.parse(from);
  if (Number.isFinite(duration) && duration > 0 && duration <= 31 * 86400000 && Date.parse(to) <= Date.now() + 1000 && validSources && validProjects) {
    return { from, to, source: "", project: "", sourceIDs, projectIDs, resourceName: "" };
  }
  const now = new Date();
  return { from: new Date(now.getTime() - 86400000).toISOString(), to: now.toISOString(), source: "", project: "", sourceIDs: [] as string[], projectIDs: [] as string[], resourceName: "" };
}

type ActivityContext = { owner: string; applied: ReturnType<typeof initialActivityRange>; selected?: Selected; cursor?: string };
function readActivityContext(owner: string): ActivityContext | undefined {
  const value = window.history.state?.workActivityContext as ActivityContext | undefined;
  if (!value || value.owner !== owner || !value.applied) return;
  const range = value.applied;
  const refs = (items: unknown): items is string[] => Array.isArray(items) && items.length <= 16 && items.every(item => typeof item === "string" && item.length <= 128);
  const duration = Date.parse(range.to) - Date.parse(range.from);
  if (!(duration > 0 && duration <= 31 * 86400000) || !refs(range.sourceIDs) || !refs(range.projectIDs) ||
    typeof range.resourceName !== "string" || range.resourceName.length > 160 ||
    (value.cursor !== undefined && (typeof value.cursor !== "string" || value.cursor.length > 4096))) return;
  if (value.selected && !["source", "instance", "org", "project", "event"].every(key =>
    typeof value.selected?.[key as keyof Selected] === "string" && value.selected[key as keyof Selected].length <= 128)) return;
  return value;
}

export function WorkActivityPage({ session, navigate }: Props) {
  const identity = scopeSessionKey(session);
  const retained = useRef(readActivityContext(identity)).current;
  const entryRange = useRef(retained?.applied ?? initialActivityRange()).current;
  const [hours, setHours] = useState(() => Math.max(24, Math.min(720, Math.round((Date.parse(entryRange.to) - Date.parse(entryRange.from)) / 3600000))));
  const [periodChanged, setPeriodChanged] = useState(false);
  const [scopeChoice, setScopeChoice] = useState(() => entryRange.sourceIDs.length || entryRange.projectIDs.length ? "__overview__" : "");
  const [resourceName, setResourceName] = useState(entryRange.resourceName);
  const [scopes, setScopes] = useState<WorkActivityScopes>();
  const [scopeError, setScopeError] = useState("");
  const [applied, setApplied] = useState(entryRange);
  const [cursor, setCursor] = useState<string | undefined>(retained?.cursor);
  const [page, setPage] = useState<ActivityPage>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState<Selected | undefined>(retained?.selected);
  const [detail, setDetail] = useState<WorkActivityDetail>();
  const [detailError, setDetailError] = useState("");
  const [bodyRead, setBodyRead] = useState<{ ref: string; result: VerifiedWorkReviewBody }>();
  const [bodyError, setBodyError] = useState("");
  const [bodyBusy, setBodyBusy] = useState(false);
  const bodyController = useRef<AbortController | null>(null);
  const [runtimeBusy, setRuntimeBusy] = useState(false);
  const [runtimeError, setRuntimeError] = useState("");
  const runtimeController = useRef<AbortController | null>(null);

  useEffect(() => {
    window.history.replaceState({ ...window.history.state,
      workActivityContext: { owner: identity, applied, selected, cursor } }, "", window.location.href);
  }, [identity, applied, selected, cursor]);
  useEffect(() => {
    runtimeController.current?.abort(); setRuntimeBusy(false); setRuntimeError("");
    return () => { runtimeController.current?.abort(); };
  }, [selected, identity]);

  useEffect(() => {
    const controller = new AbortController();
    const clear = () => { controller.abort(); setScopes(undefined); setScopeChoice(""); setSelected(undefined); setDetail(undefined); };
    window.addEventListener("xinsight:scope-changed", clear);
    window.addEventListener("xinsight:unauthorized", clear);
    setScopeError(""); setScopes(undefined);
    void workActivityAPI.scopes(controller.signal)
      .then(value => { if (!controller.signal.aborted) setScopes(value); })
      .catch(reason => { if (!controller.signal.aborted && !isAbortError(reason)) setScopeError("当前授权来源不可读，请稍后重试。"); });
    return () => { controller.abort(); window.removeEventListener("xinsight:scope-changed", clear); window.removeEventListener("xinsight:unauthorized", clear); };
  }, [identity]);

  useEffect(() => {
    if (!scopes?.items.length) { setPage(undefined); setBusy(false); return; }
    const controller = new AbortController();
    const clear = () => { controller.abort(); setPage(undefined); setSelected(undefined); setDetail(undefined); setCursor(undefined); };
    window.addEventListener("xinsight:scope-changed", clear);
    window.addEventListener("xinsight:unauthorized", clear);
    setBusy(true); setError(""); setPage(undefined);
    void workActivityAPI.list({ from: applied.from, to: applied.to, source_ids: applied.sourceIDs, project_ids: applied.projectIDs, resource_name: applied.resourceName, cursor }, controller.signal)
      .then(value => { if (!controller.signal.aborted) setPage(value); })
      .catch(reason => { if (!controller.signal.aborted && !isAbortError(reason)) setError("当前活动资料不可读；不能据此判断没有发生工作。"); })
      .finally(() => { if (!controller.signal.aborted) setBusy(false); });
    return () => { controller.abort(); window.removeEventListener("xinsight:scope-changed", clear); window.removeEventListener("xinsight:unauthorized", clear); };
  }, [applied, cursor, identity, scopes]);

  useEffect(() => {
    if (!selected) { setDetail(undefined); return; }
    const controller = new AbortController();
    setDetail(undefined); setDetailError("");
    const clear = () => { controller.abort(); setDetail(undefined); };
    window.addEventListener("xinsight:scope-changed", clear);
    window.addEventListener("xinsight:unauthorized", clear);
    void workActivityAPI.detail(selected, controller.signal)
      .then(value => { if (!controller.signal.aborted) setDetail(value); })
      .catch(reason => { if (!controller.signal.aborted && !isAbortError(reason)) setDetailError("当前无权读取或资料已变更，请返回列表重新查询。"); });
    return () => { controller.abort(); window.removeEventListener("xinsight:scope-changed", clear); window.removeEventListener("xinsight:unauthorized", clear); };
  }, [selected, identity]);

  useEffect(() => {
    bodyController.current?.abort(); bodyController.current = null;
    setBodyRead(undefined); setBodyError(""); setBodyBusy(false);
    return () => { bodyController.current?.abort(); bodyController.current = null; };
  }, [selected, identity]);

  useEffect(() => {
    if (!bodyRead) return;
    const clearExpired = () => {
      bodyController.current?.abort();
      setBodyRead(undefined); setBodyBusy(false); setBodyError("XI 复查副本已过期，请重新核对副本状态。");
    };
    return scheduleBodyExpiry(Date.parse(bodyRead.result.expiresAt), clearExpired);
  }, [bodyRead]);

  const readBody = async (ref: string, digest: string) => {
    bodyController.current?.abort();
    const controller = new AbortController(); bodyController.current = controller;
    setBodyRead(undefined); setBodyError(""); setBodyBusy(true);
    try {
      const result = await fetchVerifiedWorkReviewBody(ref, digest, session, controller.signal);
      if (!controller.signal.aborted) setBodyRead({ ref, result });
    } catch (reason) {
      if (!controller.signal.aborted && !isAbortError(reason)) setBodyError(reason instanceof Error ? reason.message : "正文读取失败");
    } finally {
      if (bodyController.current === controller) { bodyController.current = null; setBodyBusy(false); }
    }
  };

  const openRelatedRequest = async (item: WorkActivityDetail["relations"][number]) => {
    const observed = Date.parse(item.observed_at ?? "");
    if (item.state !== "ur_identifier_matched" || item.binding_state !== "current_revision" ||
      !item.runtime_request_ref || !item.relation.ur_target || !Number.isFinite(observed)) return;
    runtimeController.current?.abort();
    const controller = new AbortController(); runtimeController.current = controller;
    setRuntimeBusy(true); setRuntimeError("");
    const target = item.relation.ur_target;
    const request: ReadingRequest = {
      contract_version: "xinsight.runtime-reading.v1", metric_version: "xinsight.runtime.request-observations.v1",
      period: { start_inclusive: new Date(observed - 60000).toISOString(),
        end_exclusive: new Date(Math.min(Date.now(), observed - 60000 + 31 * 86400000)).toISOString(),
        display_timezone: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC" },
      filters: { source_ids: [target.source_system_id], source_projects: [`project-${target.project_id}`], cost_projects: [] },
    };
    try {
      const reading = await runtimeAPI.createReading(request, session.csrf_token, controller.signal);
      if (controller.signal.aborted || currentScopeSessionKey() !== identity) return;
      navigate("/workbench/runtime", undefined, { version: 1, owner: navigationOwner(session),
        readingID: reading.reading_id, request, entry: "all", selectedRef: item.runtime_request_ref, detailView: "story" });
    } catch (reason) {
      if (!controller.signal.aborted && !isAbortError(reason)) setRuntimeError("关联请求当前不可读，可能已撤权或资料已变化；原活动仍可独立查看。");
    } finally {
      if (runtimeController.current === controller) setRuntimeBusy(false);
    }
  };

  const scopeLabel = (reporter: { source_system_id: string; source_instance: string; org_id: string; project_id: string }) => {
    const scope = scopes?.items.find(item => item.source_id === reporter.source_system_id &&
      item.source_instance === reporter.source_instance && item.org_id === reporter.org_id && item.project_id === reporter.project_id);
    return `${scope?.source_label || "已授权来源"} · ${scope?.project_label || `项目 ${reporter.project_id}`}`;
  };

  return <section className="overview-page" aria-label="工作活动">
    <header className="overview-heading"><span className="eyebrow">当前授权来源</span><h1>工作活动与数据</h1><p>独立查找工具与资源来源报告；模型请求关系和文件结果另行核对。</p><button type="button" onClick={() => navigate("/inbox")}>← 返回总览</button></header>
    <form className="overview-filters" onSubmit={event => { event.preventDefault(); const to = new Date(); const chosen = scopes?.items.find(item => `${item.source_id}:${item.project_id}` === scopeChoice); const range = periodChanged ? { from: new Date(to.getTime() - hours * 3600000).toISOString(), to: to.toISOString() } : { from: applied.from, to: applied.to }; setSelected(undefined); setCursor(undefined); setApplied({ ...range, source: chosen?.source_id ?? "", project: chosen?.project_id ?? "", sourceIDs: chosen ? [chosen.source_id] : scopeChoice === "__overview__" ? applied.sourceIDs : [], projectIDs: chosen ? [chosen.project_id] : scopeChoice === "__overview__" ? applied.projectIDs : [], resourceName: resourceName.trim() }); }}>
      <div className="overview-filter-controls"><label>期间<select value={hours} onChange={event => { setHours(Number(event.target.value)); setPeriodChanged(true); }}>{![24,168,720].includes(hours) ? <option value={hours}>来自总览的期间</option> : null}<option value={24}>最近 24 小时</option><option value={168}>最近 7 天</option><option value={720}>最近 30 天</option></select></label>
        <label>来源与项目<select value={scopeChoice} onChange={event => setScopeChoice(event.target.value)} disabled={!scopes?.items.length}><option value="">全部获授权范围</option>{scopeChoice === "__overview__" ? <option value="__overview__">沿用总览的来源与项目筛选</option> : null}{scopes?.items.filter((item, index, all) => all.findIndex(other => other.source_id === item.source_id && other.project_id === item.project_id) === index).map(item => <option key={`${item.source_id}:${item.project_id}`} value={`${item.source_id}:${item.project_id}`}>{item.source_label} · {item.project_label || `项目 ${item.project_id}`}（全部已授权实例）</option>)}</select></label>
        <label>资源名称<input value={resourceName} onChange={event => setResourceName(event.target.value)} placeholder="按可读名称查找" maxLength={160} /></label>
        <button type="submit" disabled={!scopes?.items.length}>应用范围</button></div>
    </form>
    <p>当前列表期间：{new Date(applied.from).toLocaleString("zh-CN")} — {new Date(applied.to).toLocaleString("zh-CN")}；来源与项目按当前授权及所选条件过滤。</p>
    {scopeError ? <p role="alert">{scopeError}</p> : null}
    {scopes && !scopes.items.length ? <p>当前账户没有可展示的工作活动来源或项目；不能据此判断来源没有发生工作。</p> : null}
    {selected ? <article className="overview-panel"><button type="button" onClick={() => { setSelected(undefined); setDetail(undefined); }}>← 返回当前筛选列表</button>
      {detailError ? <p role="alert">{detailError}</p> : !detail ? <p role="status">正在读取工作过程…</p> : <>
        <h2>{detail.item.event.resource?.name_state === "readable" ? detail.item.event.resource.name : kinds[detail.item.event.subject_kind] ?? "工作活动"}</h2>
        <p>{activityLabel(detail.item.event.subject_kind, detail.item.event.action_kind)} · {lifecycle[detail.item.event.lifecycle_state]} · {new Date(detail.item.event.occurred_at).toLocaleString("zh-CN")}</p>
        <p>执行身份：{actorLabel(detail.item.event.actor)} · {scopeLabel(detail.item.event.reporter)}</p>
        {detail.item.event.subject_kind === "resource_action" ? <p>资源名称是来源报告的路径；同一资源标识仅按观察到的工作目录和路径字面归一，不证明实际磁盘文件或修改结果。</p> : null}
        {detail.item.event.resource?.name_state === "hidden" ? <p>资源名称已按来源策略隐藏。</p> : null}
        <p>{content[detail.item.event.content_state] ?? "正文引用状态未知"}。此处只说明来源事件的引用状态，不代表执行必需暂存、复查正文副本或独立文件结果核验。</p>
        {(detail.review_bodies ?? []).length ? <div><h3>复查正文</h3><ul>{detail.review_bodies.map(item => <li key={`${item.kind}:${item.ref}`}>
          {bodyKinds[item.kind] ?? "来源正文"}：{bodyStates[item.state] ?? "XI 副本状态未知"}。
          {item.can_read ? <button type="button" disabled={bodyBusy} onClick={() => void readBody(item.ref, item.sha256)}>读取并核验正文</button> : null}
          <details><summary>正文来源类型与引用</summary>{item.kind} · {item.ref}</details></li>)}</ul>
          {bodyBusy ? <p role="status">正在读取并核验正文…</p> : null}
          {bodyError ? <p role="alert">{bodyError}</p> : null}
          {bodyRead ? <section aria-label="已核验正文"><p>已核验 {bodyRead.result.bytes.byteLength} 字节，摘要与来源引用一致。</p>
            {bodyRead.result.text !== null ? <><pre>{bodyRead.result.text}</pre>{bodyRead.result.previewOnly ? <p>只预览前 64 KiB；已核验完整副本。</p> : null}</> : <p>副本是二进制或非 UTF-8 内容；完整字节已核验。</p>}
            <details><summary>正文技术引用</summary>{bodyRead.ref}</details></section> : null}
        </div> : null}
        <section aria-label="关联模型调用"><h3>关联模型调用</h3>
          {detail.relations.length ? <ul>{detail.relations.map(item => <li key={item.relation.relation_ref}>
            <p>{item.binding_state === "prior_revision" ? `该关联属于较早修订 ${item.relation.event_revision}，不代表当前活动的关联。` :
              item.state === "ur_identifier_matched" ? "已与 UR 的同一请求标识匹配；这不证明工具操作由该模型引起。" : "来源报告了模型关系，XI 尚未匹配到对应请求。"}</p>
            {item.binding_state === "current_revision" && item.state === "ur_identifier_matched" && item.runtime_request_ref && item.observed_at && item.relation.ur_target ?
              <button type="button" disabled={runtimeBusy} onClick={() => void openRelatedRequest(item)}>查看关联模型调用</button> : null}
            <details><summary>关联依据</summary>{item.relation.ur_request_id} · {item.relation.model_call_id}</details>
          </li>)}</ul> : <p>当前没有可展示的模型请求关系；可以查看同一次运行中的其他活动。</p>}
          {runtimeBusy ? <p role="status">正在读取关联模型调用…</p> : null}
          {runtimeError ? <p role="alert">{runtimeError}</p> : null}
        </section>
        {detail.run_items?.length ? <section aria-label="同一次运行的活动"><h3>同一次运行的活动</h3>
          <p>这些活动由同一来源报告为同一次运行，按发生时间排列；先后顺序不等于因果关系。</p>
          <div className="overview-table-scroll"><table><thead><tr><th>发生时间</th><th>谁</th><th>活动</th><th>数据或资源</th><th>查看</th></tr></thead>
            <tbody>{detail.run_items.map(item => <tr key={item.event.event_ref}>
              <td>{new Date(item.event.occurred_at).toLocaleString("zh-CN")}</td><td>{actorLabel(item.event.actor)}</td>
              <td>{activityLabel(item.event.subject_kind, item.event.action_kind)} · {lifecycle[item.event.lifecycle_state]}</td>
              <td>{item.event.resource?.name_state === "readable" ? item.event.resource.name : item.event.resource ? "名称已隐藏" : "未报告资源"}</td>
              <td><button type="button" disabled={item.event.event_ref === detail.item.event.event_ref} onClick={() => setSelected({
                source: item.event.reporter.source_system_id, instance: item.event.reporter.source_instance,
                org: item.event.reporter.org_id, project: item.event.reporter.project_id, event: item.event.event_ref,
              })}>{item.event.event_ref === detail.item.event.event_ref ? "当前活动" : "查看活动详情"}</button></td>
            </tr>)}</tbody></table></div>
          {detail.run_items_more ? <p>本页仅显示前 100 项，仍有其他活动未展示。</p> : null}
        </section> : null}
        <details><summary>技术标识与来源记录</summary><p>来源实例 {detail.item.event.reporter.source_instance} · 组织 {detail.item.event.reporter.org_id} · 项目 {detail.item.event.reporter.project_id}</p><p>任务 {detail.item.event.task_ref || "未提供"} · 运行 {detail.item.event.run_id || "未提供"} · 模型调用 {detail.item.event.model_call_id || "未提供"} · 工具调用 {detail.item.event.tool_call_id || "未提供"}</p><p>事件修订 {detail.item.event.revision} · 摘要 {detail.item.event_hash}</p></details>
      </>}
    </article> : null}
    <article className="overview-panel overview-recent"><header><h2>活动记录</h2><small>只显示当前账户获授权的来源与项目；未关联模型请求的活动也在这里。</small></header>
      {busy ? <p role="status">正在读取活动…</p> : null}
      {error ? <p role="alert">{error}</p> : null}
      {scopes?.items.length && !busy && !error && !page?.items.length ? <p>当前筛选中没有可展示的活动；来源覆盖完整性仍未知。</p> : null}
      {page?.items.length ? <div className="overview-table-scroll"><table><thead><tr><th>发生时间</th><th>谁</th><th>来源项目</th><th>资源</th><th>做了什么</th><th>来源状态</th><th>查看</th></tr></thead><tbody>{page.items.map(item => <tr key={`${item.event.reporter.source_system_id}:${item.event.reporter.source_instance}:${item.event.reporter.org_id}:${item.event.reporter.project_id}:${item.event.event_ref}`}>
        <td>{new Date(item.event.occurred_at).toLocaleString("zh-CN")}</td><td>{actorLabel(item.event.actor)}</td><td>{scopeLabel(item.event.reporter)}</td><td>{item.event.resource?.name_state === "readable" ? item.event.resource.name : item.event.resource ? "名称已隐藏" : "未报告资源"}</td><td>{activityLabel(item.event.subject_kind, item.event.action_kind)}</td><td>{lifecycle[item.event.lifecycle_state]}</td>
        <td><button type="button" onClick={() => setSelected({ source: item.event.reporter.source_system_id, instance: item.event.reporter.source_instance, org: item.event.reporter.org_id, project: item.event.reporter.project_id, event: item.event.event_ref })}>查看工作过程</button></td>
      </tr>)}</tbody></table></div> : null}
      {page?.next_cursor ? <button type="button" onClick={() => setCursor(page.next_cursor)}>查看下一页</button> : null}
      {cursor ? <button type="button" onClick={() => setCursor(undefined)}>返回第一页</button> : null}
    </article>
  </section>;
}
