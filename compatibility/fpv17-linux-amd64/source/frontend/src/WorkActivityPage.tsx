import { scheduleBodyExpiry } from "./bodyExpiry";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { currentScopeSessionKey, scopeSessionKey, runtimeAPI, isAbortError, workActivityAPI, type AccessSessionResponse, type WorkActivityDetail, type WorkActivityPage as ActivityPage, type WorkActivityScopes } from "./api";
import { fetchVerifiedWorkReviewBody, type VerifiedWorkReviewBody } from "./workReviewBody";
import { navigationOwner, type RuntimeNavigation } from "./workbenchNavigation";
import type { ReadingRequest } from "./runtime";
import { groupActivityPage, groupWorkRun, workEventLabel, workEventObject, workEventResultNote, workEventResultSummary, type WorkActivityItem } from "./workActivityPresentation";

type Props = { session: AccessSessionResponse; navigate: (path: string, selectedRuntimeRequestRef?: string, activityReading?: RuntimeNavigation) => void };
type Selected = { source: string; instance: string; org: string; project: string; event: string };

const content: Record<string,string> = { not_captured: "来源未报告正文引用", pending: "来源报告正文引用待到", referenced: "来源报告了正文引用；下方另示 XI 复查副本状态" };
const bodyStates: Record<string,string> = { source_reference_only: "来源有正文引用；XI 尚未持有复查副本", content_not_authorized: "当前账户没有正文读取权限", pending: "XI 正在保存复查副本", available: "XI 已保存可核验的复查副本", expired: "XI 复查副本已过期", gap: "XI 复查副本未完成", copy_missing: "XI 副本缺失，需检查完整性" };
const bodyKinds: Record<string,string> = { user_input: "框架报告的用户输入", model_input: "框架报告的模型输入", model_output: "框架报告的模型输出", tool_input: "框架报告的工具参数", tool_output: "框架报告的工具返回", user_output: "框架报告已发送的用户回复" };
const actorLabel = (actor: { kind: string; ref?: string }) => actor.kind === "unknown" ? "身份未知" : `${actor.ref ?? "身份未提供"}（${{ human: "人员", agent: "代理", service: "服务" }[actor.kind as "human" | "agent" | "service"] ?? actor.kind}）`;

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

function selectionFor(item: WorkActivityItem): Selected {
  const reporter = item.event.reporter;
  return { source: reporter.source_system_id, instance: reporter.source_instance,
    org: reporter.org_id, project: reporter.project_id, event: item.event.event_ref };
}

function matchesSelection(item: WorkActivityItem, selected: Selected) {
  const value = selectionFor(item);
  return value.source === selected.source && value.instance === selected.instance &&
    value.org === selected.org && value.project === selected.project && value.event === selected.event;
}

function landOnDetail(element: HTMLElement | null) {
  element?.scrollIntoView?.({ block: "start" });
  element?.focus({ preventScroll: true });
}

function RunActivityRow({ item, selected, onSelect }: { item: WorkActivityItem; selected: boolean; onSelect: (item: WorkActivityItem) => void }) {
  return <li className="activity-run-event"><div><strong>{workEventObject(item.event)}</strong><span>{workEventLabel(item.event)}</span></div>
    <time dateTime={item.event.occurred_at}>{new Date(item.event.occurred_at).toLocaleString("zh-CN")}</time>
    <button type="button" disabled={selected} onClick={() => onSelect(item)}>{selected ? "当前活动" : "查看事件与正文"}</button></li>;
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
  const selectedKey = selected ? JSON.stringify([selected.source, selected.instance, selected.org, selected.project, selected.event]) : "";
  const detailLanding = useRef<HTMLElement>(null);
  const focusDetailOnSelection = useRef(false);
  const selectedKeyRef = useRef(selectedKey);
  selectedKeyRef.current = selectedKey;
  const selectionGeneration = useRef(0);
  const [detail, setDetail] = useState<WorkActivityDetail>();
  const [detailError, setDetailError] = useState("");
  const [runPage, setRunPage] = useState<{ event: string; cutoff?: string; items: WorkActivityItem[]; next?: string; more: boolean }>();
  const [runBusy, setRunBusy] = useState(false);
  const [runError, setRunError] = useState("");
  const runController = useRef<AbortController | null>(null);
  const [bodyRead, setBodyRead] = useState<{ ref: string; result: VerifiedWorkReviewBody }>();
  const [bodyError, setBodyError] = useState("");
  const [bodyBusy, setBodyBusy] = useState(false);
  const bodyController = useRef<AbortController | null>(null);
  const [runtimeBusy, setRuntimeBusy] = useState(false);
  const [runtimeError, setRuntimeError] = useState("");
  const runtimeController = useRef<AbortController | null>(null);

  useEffect(() => {
    if (!selected || !focusDetailOnSelection.current) return;
    landOnDetail(detailLanding.current);
  }, [selectedKey]);

  useLayoutEffect(() => {
    if (!selected) { focusDetailOnSelection.current = false; return; }
    if (!detailError && !(detail && matchesSelection(detail.item, selected))) return;
    detailLanding.current?.style.removeProperty("min-height");
    if (focusDetailOnSelection.current) {
      focusDetailOnSelection.current = false;
      landOnDetail(detailLanding.current);
    }
  }, [selectedKey, detail, detailError]);

  const startSelection = (item: WorkActivityItem) => {
    const landing = detailLanding.current;
    if (landing) landing.style.minHeight = `${Math.ceil(landing.getBoundingClientRect().height)}px`;
    bodyController.current?.abort();
    setBodyRead(undefined);
    setDetail(undefined);
    setDetailError("");
    focusDetailOnSelection.current = true;
    setSelected(selectionFor(item));
  };

  const selectFromList = (item: WorkActivityItem) => {
    if (selected && matchesSelection(item, selected)) {
      landOnDetail(detailLanding.current);
      return;
    }
    startSelection(item);
  };

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
    selectionGeneration.current += 1;
    runController.current?.abort(); runController.current = null;
    setRunPage(undefined); setRunBusy(false); setRunError("");
    if (!selected) { setDetail(undefined); return; }
    const controller = new AbortController();
    setDetail(undefined); setDetailError("");
    const clear = () => { controller.abort(); setDetail(undefined); };
    window.addEventListener("xinsight:scope-changed", clear);
    window.addEventListener("xinsight:unauthorized", clear);
    void workActivityAPI.detail(selected, controller.signal)
      .then(value => { if (!controller.signal.aborted) {
        setDetail(value);
        setRunPage({ event: selected.event, cutoff: value.run_cutoff, items: value.run_items ?? [],
          next: value.run_items_next_cursor, more: value.run_items_more === true });
      } })
      .catch(reason => { if (!controller.signal.aborted && !isAbortError(reason)) setDetailError("当前无权读取或资料已变更，请返回列表重新查询。"); });
    return () => { controller.abort(); runController.current?.abort(); window.removeEventListener("xinsight:scope-changed", clear); window.removeEventListener("xinsight:unauthorized", clear); };
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

  const loadMoreRun = async () => {
    if (!selected || !detail || !runPage?.next || runBusy) return;
    const requestGeneration = selectionGeneration.current;
    const requestSelectedKey = selectedKey;
    runController.current?.abort();
    const controller = new AbortController(); runController.current = controller;
    setRunBusy(true); setRunError("");
    try {
      const next = await workActivityAPI.detail(selected, controller.signal, runPage.next);
      if (controller.signal.aborted || runController.current !== controller || currentScopeSessionKey() !== identity ||
        selectedKeyRef.current !== requestSelectedKey || selectionGeneration.current !== requestGeneration) return;
      const sameReporter = (item: WorkActivityItem) => item.event.reporter.source_system_id === selected.source &&
        item.event.reporter.source_instance === selected.instance && item.event.reporter.org_id === selected.org &&
        item.event.reporter.project_id === selected.project;
      if (!sameReporter(next.item) ||
        next.item.event.event_ref !== detail.item.event.event_ref ||
        next.item.event.revision !== detail.item.event.revision || next.item.event_hash !== detail.item.event_hash ||
        next.item.event.run_id !== detail.item.event.run_id ||
        next.run_cutoff !== runPage.cutoff || !next.run_items ||
        next.run_items.some(item => !sameReporter(item) || item.event.run_id !== detail.item.event.run_id ||
          runPage.items.some(prior => prior.event.event_ref === item.event.event_ref)))
        throw new Error("run_page_mismatch");
      setRunPage({ event: selected.event, cutoff: runPage.cutoff, items: [...runPage.items, ...next.run_items],
        next: next.run_items_next_cursor, more: next.run_items_more === true });
    } catch (reason) {
      if (!controller.signal.aborted && !isAbortError(reason)) setRunError("后续运行资料不可读或阅读范围已变化；已载入部分不能当作完整运行。");
    } finally {
      if (runController.current === controller) { runController.current = null; setRunBusy(false); }
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

  const visibleRun = runPage && runPage.event === selected?.event ? runPage.items : detail?.run_items ?? [];
  const runGroups = groupWorkRun(visibleRun);
  const showRunEvent = (item: WorkActivityItem) => <RunActivityRow key={item.event.event_ref} item={item} selected={item.event.event_ref === detail?.item.event.event_ref} onSelect={startSelection} />;

  return <section className="overview-page overview-page--activity" aria-label="工作活动">
    <header className="overview-heading activity-page-header"><div><span className="eyebrow">当前授权来源</span><h1>工作活动与数据</h1><p>查看对象、动作与同次运行；模型关系和文件结果分别核对。</p></div><button type="button" onClick={() => navigate("/inbox")}>← 返回总览</button></header>
    <form className="overview-filters" onSubmit={event => { event.preventDefault(); const to = new Date(); const chosen = scopes?.items.find(item => `${item.source_id}:${item.project_id}` === scopeChoice); const range = periodChanged ? { from: new Date(to.getTime() - hours * 3600000).toISOString(), to: to.toISOString() } : { from: applied.from, to: applied.to }; setSelected(undefined); setCursor(undefined); setApplied({ ...range, source: chosen?.source_id ?? "", project: chosen?.project_id ?? "", sourceIDs: chosen ? [chosen.source_id] : scopeChoice === "__overview__" ? applied.sourceIDs : [], projectIDs: chosen ? [chosen.project_id] : scopeChoice === "__overview__" ? applied.projectIDs : [], resourceName: resourceName.trim() }); }}>
      <div className="overview-filter-controls"><label>期间<select value={hours} onChange={event => { setHours(Number(event.target.value)); setPeriodChanged(true); }}>{![24,168,720].includes(hours) ? <option value={hours}>来自总览的期间</option> : null}<option value={24}>最近 24 小时</option><option value={168}>最近 7 天</option><option value={720}>最近 30 天</option></select></label>
        <label>来源与项目<select value={scopeChoice} onChange={event => setScopeChoice(event.target.value)} disabled={!scopes?.items.length}><option value="">全部获授权范围</option>{scopeChoice === "__overview__" ? <option value="__overview__">沿用总览的来源与项目筛选</option> : null}{scopes?.items.filter((item, index, all) => all.findIndex(other => other.source_id === item.source_id && other.project_id === item.project_id) === index).map(item => <option key={`${item.source_id}:${item.project_id}`} value={`${item.source_id}:${item.project_id}`}>{item.source_label} · {item.project_label || `项目 ${item.project_id}`}（全部已授权实例）</option>)}</select></label>
        <label className="overview-filter-resource">资源名称<input value={resourceName} onChange={event => setResourceName(event.target.value)} placeholder="按可读名称查找" maxLength={160} /></label>
        <button type="submit" disabled={!scopes?.items.length}>应用范围</button></div>
    </form>
    <p className="activity-range">当前列表 · {new Date(applied.from).toLocaleString("zh-CN")} — {new Date(applied.to).toLocaleString("zh-CN")} · 仅当前授权范围</p>
    {scopeError ? <p role="alert">{scopeError}</p> : null}
    {scopes && !scopes.items.length ? <p>当前账户没有可展示的工作活动来源或项目；不能据此判断来源没有发生工作。</p> : null}
    {selected ? <article className="activity-detail" aria-label="工作过程详情" ref={detailLanding} tabIndex={-1}><button className="activity-detail-back" type="button" onClick={() => { setSelected(undefined); setDetail(undefined); }}>← 返回当前筛选列表</button>
      {detailError ? <p role="alert">{detailError}</p> : !detail ? <p role="status">正在读取工作过程…</p> : <>
        <div className="activity-detail-hero"><span className="eyebrow">活动对象</span><h2>{workEventObject(detail.item.event)}</h2><strong>{workEventLabel(detail.item.event)}</strong><time dateTime={detail.item.event.occurred_at}>{new Date(detail.item.event.occurred_at).toLocaleString("zh-CN")}</time></div>
        <div className="activity-detail-layout"><aside className="activity-detail-rail" aria-label="同次运行过程"><h3>运行过程</h3>
          {detail.item.event.run_id ? <><small>当前已载入 {visibleRun.length} 条同次运行事件；时间先后不证明因果关系。</small>
            <ul>{visibleRun.map(item => <li key={item.event.event_ref}><button type="button" aria-current={item.event.event_ref === detail.item.event.event_ref ? "step" : undefined} disabled={item.event.event_ref === detail.item.event.event_ref} onClick={() => startSelection(item)}><strong>{workEventObject(item.event)}</strong><span>{workEventLabel(item.event)} · {new Date(item.event.occurred_at).toLocaleString("zh-CN")}</span></button></li>)}</ul>
            {(runPage ? runPage.more : detail.run_items_more) ? <small>仍有后续事件；可在下方读取下一页。</small> : null}
          </> : <p>来源未提供运行标识，不能组成同次运行过程。</p>}
        </aside><div className="activity-detail-main"><h3>当前活动与正文</h3>
        <p className="activity-detail-result">{workEventResultNote(detail.item.event)}</p>
        {detail.item.event.subject_kind === "resource_action" ? <details><summary>资源标识与验证边界</summary><p>资源名称来自来源报告；资源标识按观察到的工作目录和路径归一，文件结果需另行验证。</p></details> : null}
        {detail.item.event.resource?.name_state === "hidden" ? <p>资源名称已按来源策略隐藏。</p> : null}
        <details><summary>正文来源与证据边界</summary><p>{content[detail.item.event.content_state] ?? "正文引用状态未知"}。引用状态不代表 XI 已保存副本，也不证明文件结果。</p></details>
        {(detail.review_bodies ?? []).length ? <section className="activity-detail-section activity-review-bodies"><h3>复查正文</h3><ul>{detail.review_bodies.map(item => <li key={`${item.kind}:${item.ref}`}>
          {bodyKinds[item.kind] ?? "来源正文"}：{bodyStates[item.state] ?? "XI 副本状态未知"}。
          {item.can_read ? <button type="button" disabled={bodyBusy} onClick={() => void readBody(item.ref, item.sha256)}>读取并核验正文</button> : null}
          <details><summary>正文来源类型与引用</summary>{item.kind} · {item.ref}</details></li>)}</ul>
          {bodyBusy ? <p role="status">正在读取并核验正文…</p> : null}
          {bodyError ? <p role="alert">{bodyError}</p> : null}
          {bodyRead ? <section aria-label="已核验正文"><p>已核验 {bodyRead.result.bytes.byteLength} 字节，摘要与来源引用一致。</p>
            {bodyRead.result.text !== null ? <><pre className="work-activity-verified-body">{bodyRead.result.text}</pre>{bodyRead.result.previewOnly ? <p>只预览前 64 KiB；已核验完整副本。</p> : null}</> : <p>副本是二进制或非 UTF-8 内容；完整字节已核验。</p>}
            <details><summary>正文技术引用</summary>{bodyRead.ref}</details></section> : null}
        </section> : null}
        <section className="activity-detail-section" aria-label="关联模型调用"><h3>关联模型调用</h3>
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
        </div><aside className="activity-detail-context" aria-label="来源上下文"><h3>来源上下文</h3>
          <dl><div><dt>来源服务与项目</dt><dd>{scopeLabel(detail.item.event.reporter)}</dd></div>
            <div><dt>事件执行身份</dt><dd>{actorLabel(detail.item.event.actor)}</dd></div>
            <div><dt>任务发起人</dt><dd>来源未提供</dd></div>
            <div><dt>正文引用</dt><dd>{content[detail.item.event.content_state] ?? "正文引用状态未知"}</dd></div></dl>
          <details><summary>技术标识与来源记录</summary><p>来源实例 {detail.item.event.reporter.source_instance} · 组织 {detail.item.event.reporter.org_id} · 项目 {detail.item.event.reporter.project_id}</p><p>任务 {detail.item.event.task_ref || "未提供"} · 运行 {detail.item.event.run_id || "未提供"} · 模型调用 {detail.item.event.model_call_id || "未提供"} · 工具调用 {detail.item.event.tool_call_id || "未提供"}</p><p>事件修订 {detail.item.event.revision} · 摘要 {detail.item.event_hash}</p></details>
        </aside></div>
        {detail.item.event.run_id ? <details className="activity-run-expanded"><summary>运行汇总、分类与读取依据</summary><section aria-label="同一次运行的活动" className="activity-run activity-detail-section"><h3>同一次运行的工作过程</h3>
          <p className="activity-run-boundary">仅同一来源实例、组织、项目及运行标识；时间先后不等于因果关系。</p>
          {detail.run_summary ? <div className="activity-run-summary" aria-label="完整受权运行的来源汇总">
            <div><span>资源对象</span><strong>{detail.run_summary.distinct_resources}</strong><small>按准确来源范围和资源引用去重；同名路径不合并</small></div>
            <div><span>读取尝试</span><strong>{Object.values(detail.run_summary.resource_read_outcomes).reduce((sum, value) => sum + value, 0)}</strong><small>来源报告完成 {detail.run_summary.resource_read_outcomes.completed} · 失败 {detail.run_summary.resource_read_outcomes.failed} · 可选不存在 {detail.run_summary.resource_read_outcomes.optional_not_found} · 未确认 {detail.run_summary.resource_read_outcomes.unknown}</small></div>
            <div><span>写入或修改尝试</span><strong>{Object.values(detail.run_summary.resource_change_outcomes).reduce((sum, value) => sum + value, 0)}</strong><small>来源报告完成 {detail.run_summary.resource_change_outcomes.completed} · 失败 {detail.run_summary.resource_change_outcomes.failed} · 未确认 {detail.run_summary.resource_change_outcomes.unknown}；独立文件结果未知</small></div>
            <div><span>模型调用</span><strong>{detail.run_summary.model_calls}</strong><small>工具调用 {detail.run_summary.tool_calls}；另有 {detail.run_summary.unlinked_model_observations} 条未关联调用的模型输入／输出观察</small></div>
            <div><span>任务框架状态</span><strong>正常结束 {detail.run_summary.task_completed}</strong><small>报告失败 {detail.run_summary.task_failed} · 结束未确认 {detail.run_summary.task_unknown}；仅来源报告，不代表业务目标完成</small></div>
          </div> : <p>完整运行汇总未提供；下列已载入事件不能用作全量计数。</p>}
          <p>当前已载入 {visibleRun.length} 条事件{(runPage ? runPage.more : detail.run_items_more) ? "，仍有后续页" : "（本次受权运行已载入完毕）"}；来源期间完整性仍未知。</p>
          <div className="activity-run-groups">
            <section><h4>资源读取</h4>{runGroups.reads.length ? <ul>{runGroups.reads.map(showRunEvent)}</ul> : <p>已载入部分没有资源读取事件。</p>}</section>
            <section><h4>资源写入与修改</h4>{runGroups.changes.length ? <ul>{runGroups.changes.map(showRunEvent)}</ul> : <p>已载入部分没有写入或修改事件。</p>}</section>
            {runGroups.otherResources.length ? <section><h4>其他资源活动</h4><ul>{runGroups.otherResources.map(showRunEvent)}</ul></section> : null}
            <section><h4>模型交互</h4>{runGroups.modelCalls.length ? runGroups.modelCalls.map((call, index) => <div className="activity-model-call" key={call.id}><strong>模型调用 {index + 1}</strong><ul>{call.events.map(showRunEvent)}</ul><details><summary>调用标识</summary><code>{call.id}</code></details></div>) : null}
              {runGroups.modelObservations.length ? <><h5>未关联具体调用的输入／输出观察</h5><ul>{runGroups.modelObservations.map(showRunEvent)}</ul></> : null}
              {!runGroups.modelCalls.length && !runGroups.modelObservations.length ? <p>已载入部分没有模型事件。</p> : null}</section>
            <section><h4>任务、工具与消息</h4>{runGroups.other.length ? <ul>{runGroups.other.map(showRunEvent)}</ul> : <p>已载入部分没有其他观察。</p>}</section>
          </div>
          {runPage?.next ? <button type="button" disabled={runBusy} onClick={() => void loadMoreRun()}>{runBusy ? "正在读取后续事件…" : "加载下一页运行事件"}</button> : null}
          {runPage?.more && !runPage.next ? <p>服务端仅提供前 100 条；后续事件尚未载入。</p> : null}
          {runError ? <p role="alert">{runError}</p> : null}
          <details><summary>按时间查看已载入的原始事件</summary><div className="overview-table-scroll"><table><thead><tr><th>发生时间</th><th>执行身份</th><th>来源事件</th><th>对象</th><th>详情</th></tr></thead><tbody>{visibleRun.map(item => <tr key={item.event.event_ref}>
            <td>{new Date(item.event.occurred_at).toLocaleString("zh-CN")}</td><td>{actorLabel(item.event.actor)}</td><td>{workEventLabel(item.event)}</td><td>{workEventObject(item.event)}</td><td><button type="button" disabled={item.event.event_ref === detail.item.event.event_ref} onClick={() => startSelection(item)}>查看原事件</button></td>
          </tr>)}</tbody></table></div></details>
          <details><summary>运行读取依据</summary><p>运行事件截止 {detail.run_cutoff ? new Date(detail.run_cutoff).toLocaleString("zh-CN") : "未提供"}；分页只冻结已接收修订，不证明来源完整。</p></details>
        </section></details> : null}
      </>}
    </article> : null}
    <article className="overview-panel activity-list-panel"><header><h2>活动记录</h2><small>当前授权来源与项目 · 未关联模型请求的活动仍可见</small></header>
      <details className="activity-list-basis"><summary>列表范围与分组依据</summary><p>同次运行仅按准确来源范围与运行标识汇集当前页；时间相近不证明因果关系。逐条展开可查看来源状态的解释。</p></details>
      {busy ? <p role="status">正在读取活动…</p> : null}
      {error ? <p role="alert">{error}</p> : null}
      {scopes?.items.length && !busy && !error && !page?.items.length ? <p>当前筛选中没有可展示的活动；来源覆盖完整性仍未知。</p> : null}
      {page?.items.length ? <div className="activity-list-columns" aria-hidden="true"><span>对象与动作</span><span>时间与来源</span><span>来源结果</span><span>详情</span></div> : null}
      {page?.items.length ? <div className="activity-records">{groupActivityPage(page.items).map(group => <div className="activity-record-group" key={group.key}>
        {group.runID && group.items.length > 1 ? <h3 className="activity-run-label">同次运行 · 当前页 {group.items.length} 条来源活动</h3> : null}
        <ul>{group.items.map(item => <li className="activity-record" key={`${item.event.reporter.source_system_id}:${item.event.reporter.source_instance}:${item.event.reporter.org_id}:${item.event.reporter.project_id}:${item.event.event_ref}`}>
          <div className="activity-record-main"><strong>{workEventObject(item.event)}</strong><span>{workEventLabel(item.event)}</span></div>
          <div className="activity-record-meta"><div className="activity-record-meta-primary"><time dateTime={item.event.occurred_at}>{new Date(item.event.occurred_at).toLocaleString("zh-CN")}</time><span>{actorLabel(item.event.actor)}</span></div><small>{scopeLabel(item.event.reporter)}</small></div>
          <div className="activity-record-result"><strong>{workEventResultSummary(item.event)}</strong><details><summary>结果与依据</summary><p>{workEventResultNote(item.event)}</p></details></div>
          <button type="button" onClick={() => selectFromList(item)}>查看工作过程</button>
        </li>)}</ul>
      </div>)}</div> : null}
      {page?.next_cursor ? <button type="button" onClick={() => setCursor(page.next_cursor)}>查看下一页</button> : null}
      {cursor ? <button type="button" onClick={() => setCursor(undefined)}>返回第一页</button> : null}
    </article>
  </section>;
}
