import { localizedLabels, getLocale, t, tx, useLocale, useMessageState } from "./i18n";
import { scheduleBodyExpiry } from "./bodyExpiry";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { currentScopeSessionKey, scopeSessionKey, runtimeAPI, isAbortError, workActivityAPI, type AccessSessionResponse, type WorkActivityDetail, type WorkActivityPage as ActivityPage, type WorkActivityScopes } from "./api";
import { fetchVerifiedWorkReviewBody, type VerifiedWorkReviewBody } from "./workReviewBody";
import { navigationOwner, type RuntimeNavigation } from "./workbenchNavigation";
import type { ReadingRequest } from "./runtime";
import { groupActivityPage, groupWorkRun, workEventLabel, workEventObject, workEventResultNote, workEventResultSummary, type WorkActivityItem } from "./workActivityPresentation";

type Props = { session: AccessSessionResponse; navigate: (path: string, selectedRuntimeRequestRef?: string, activityReading?: RuntimeNavigation) => void };
type Selected = { source: string; instance: string; org: string; project: string; event: string };

const content: Record<string,string> = localizedLabels({}, {not_captured: "m2471", pending: "m2472", referenced: "m2473"});
const bodyStates: Record<string,string> = localizedLabels({}, {source_reference_only: "m2474", content_not_authorized: "m2475", pending: "m2476", available: "m2477", expired: "m2478", gap: "m2479", copy_missing: "m2480"});
const bodyKinds: Record<string,string> = localizedLabels({}, {user_input: "m2481", model_input: "m2482", model_output: "m2483", tool_input: "m2484", tool_output: "m2485", user_output: "m2486"});
const actorLabel = (actor: { kind: string; ref?: string }) => actor.kind === "unknown" ? t("m2487") : `${actor.ref ?? t("m2488")}（${{ human: t("m2489"), agent: t("m2490"), service: t("m2491") }[actor.kind as "human" | "agent" | "service"] ?? actor.kind}）`;

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
  useLocale();
  return <li className="activity-run-event"><div><strong>{workEventObject(item.event)}</strong><span>{workEventLabel(item.event)}</span></div>
    <time dateTime={item.event.occurred_at}>{new Date(item.event.occurred_at).toLocaleString(getLocale())}</time>
    <button type="button" disabled={selected} onClick={() => onSelect(item)}>{selected ? t("m2492") : t("m2493")}</button></li>;
}

export function WorkActivityPage({ session, navigate }: Props) {
  useLocale();
  const identity = scopeSessionKey(session);
  const retained = useRef(readActivityContext(identity)).current;
  const entryRange = useRef(retained?.applied ?? initialActivityRange()).current;
  const [hours, setHours] = useState(() => Math.max(24, Math.min(720, Math.round((Date.parse(entryRange.to) - Date.parse(entryRange.from)) / 3600000))));
  const [periodChanged, setPeriodChanged] = useState(false);
  const [scopeChoice, setScopeChoice] = useState(() => entryRange.sourceIDs.length || entryRange.projectIDs.length ? "__overview__" : "");
  const [resourceName, setResourceName] = useState(entryRange.resourceName);
  const [scopes, setScopes] = useState<WorkActivityScopes>();
  const [scopeError, setScopeError] = useMessageState("");
  const [applied, setApplied] = useState(entryRange);
  const [cursor, setCursor] = useState<string | undefined>(retained?.cursor);
  const [page, setPage] = useState<ActivityPage>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useMessageState("");
  const [selected, setSelected] = useState<Selected | undefined>(retained?.selected);
  const selectedKey = selected ? JSON.stringify([selected.source, selected.instance, selected.org, selected.project, selected.event]) : "";
  const detailLanding = useRef<HTMLElement>(null);
  const focusDetailOnSelection = useRef(false);
  const selectedKeyRef = useRef(selectedKey);
  selectedKeyRef.current = selectedKey;
  const selectionGeneration = useRef(0);
  const [detail, setDetail] = useState<WorkActivityDetail>();
  const [detailError, setDetailError] = useMessageState("");
  const [runPage, setRunPage] = useState<{ event: string; cutoff?: string; items: WorkActivityItem[]; next?: string; more: boolean }>();
  const [runBusy, setRunBusy] = useState(false);
  const [runError, setRunError] = useMessageState("");
  const runController = useRef<AbortController | null>(null);
  const [bodyRead, setBodyRead] = useState<{ ref: string; result: VerifiedWorkReviewBody }>();
  const [bodyError, setBodyError] = useMessageState("");
  const [bodyBusy, setBodyBusy] = useState(false);
  const bodyController = useRef<AbortController | null>(null);
  const [runtimeBusy, setRuntimeBusy] = useState(false);
  const [runtimeError, setRuntimeError] = useMessageState("");
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
    setDetailError(() => "");
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
    runtimeController.current?.abort(); setRuntimeBusy(false); setRuntimeError(() => "");
    return () => { runtimeController.current?.abort(); };
  }, [selected, identity]);

  useEffect(() => {
    const controller = new AbortController();
    const clear = () => { controller.abort(); setScopes(undefined); setScopeChoice(""); setSelected(undefined); setDetail(undefined); };
    window.addEventListener("xinsight:scope-changed", clear);
    window.addEventListener("xinsight:unauthorized", clear);
    setScopeError(() => ""); setScopes(undefined);
    void workActivityAPI.scopes(controller.signal)
      .then(value => { if (!controller.signal.aborted) setScopes(value); })
      .catch(reason => { if (!controller.signal.aborted && !isAbortError(reason)) setScopeError(() => t("m2494")); });
    return () => { controller.abort(); window.removeEventListener("xinsight:scope-changed", clear); window.removeEventListener("xinsight:unauthorized", clear); };
  }, [identity]);

  useEffect(() => {
    if (!scopes?.items.length) { setPage(undefined); setBusy(false); return; }
    const controller = new AbortController();
    const clear = () => { controller.abort(); setPage(undefined); setSelected(undefined); setDetail(undefined); setCursor(undefined); };
    window.addEventListener("xinsight:scope-changed", clear);
    window.addEventListener("xinsight:unauthorized", clear);
    setBusy(true); setError(() => ""); setPage(undefined);
    void workActivityAPI.list({ from: applied.from, to: applied.to, source_ids: applied.sourceIDs, project_ids: applied.projectIDs, resource_name: applied.resourceName, cursor }, controller.signal)
      .then(value => { if (!controller.signal.aborted) setPage(value); })
      .catch(reason => { if (!controller.signal.aborted && !isAbortError(reason)) setError(() => t("m2495")); })
      .finally(() => { if (!controller.signal.aborted) setBusy(false); });
    return () => { controller.abort(); window.removeEventListener("xinsight:scope-changed", clear); window.removeEventListener("xinsight:unauthorized", clear); };
  }, [applied, cursor, identity, scopes]);

  useEffect(() => {
    selectionGeneration.current += 1;
    runController.current?.abort(); runController.current = null;
    setRunPage(undefined); setRunBusy(false); setRunError(() => "");
    if (!selected) { setDetail(undefined); return; }
    const controller = new AbortController();
    setDetail(undefined); setDetailError(() => "");
    const clear = () => { controller.abort(); setDetail(undefined); };
    window.addEventListener("xinsight:scope-changed", clear);
    window.addEventListener("xinsight:unauthorized", clear);
    void workActivityAPI.detail(selected, controller.signal)
      .then(value => { if (!controller.signal.aborted) {
        setDetail(value);
        setRunPage({ event: selected.event, cutoff: value.run_cutoff, items: value.run_items ?? [],
          next: value.run_items_next_cursor, more: value.run_items_more === true });
      } })
      .catch(reason => { if (!controller.signal.aborted && !isAbortError(reason)) setDetailError(() => t("m2496")); });
    return () => { controller.abort(); runController.current?.abort(); window.removeEventListener("xinsight:scope-changed", clear); window.removeEventListener("xinsight:unauthorized", clear); };
  }, [selected, identity]);

  useEffect(() => {
    bodyController.current?.abort(); bodyController.current = null;
    setBodyRead(undefined); setBodyError(() => ""); setBodyBusy(false);
    return () => { bodyController.current?.abort(); bodyController.current = null; };
  }, [selected, identity]);

  useEffect(() => {
    if (!bodyRead) return;
    const clearExpired = () => {
      bodyController.current?.abort();
      setBodyRead(undefined); setBodyBusy(false); setBodyError(() => t("m2497"));
    };
    return scheduleBodyExpiry(Date.parse(bodyRead.result.expiresAt), clearExpired);
  }, [bodyRead]);

  const readBody = async (ref: string, digest: string) => {
    bodyController.current?.abort();
    const controller = new AbortController(); bodyController.current = controller;
    setBodyRead(undefined); setBodyError(() => ""); setBodyBusy(true);
    try {
      const result = await fetchVerifiedWorkReviewBody(ref, digest, session, controller.signal);
      if (!controller.signal.aborted) setBodyRead({ ref, result });
    } catch (reason) {
      if (!controller.signal.aborted && !isAbortError(reason)) setBodyError(() => reason instanceof Error ? reason.message : t("m2498"));
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
    setRunBusy(true); setRunError(() => "");
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
      if (!controller.signal.aborted && !isAbortError(reason)) setRunError(() => t("m2499"));
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
    setRuntimeBusy(true); setRuntimeError(() => "");
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
      if (!controller.signal.aborted && !isAbortError(reason)) setRuntimeError(() => t("m2500"));
    } finally {
      if (runtimeController.current === controller) setRuntimeBusy(false);
    }
  };

  const scopeLabel = (reporter: { source_system_id: string; source_instance: string; org_id: string; project_id: string }) => {
    const scope = scopes?.items.find(item => item.source_id === reporter.source_system_id &&
      item.source_instance === reporter.source_instance && item.org_id === reporter.org_id && item.project_id === reporter.project_id);
    return `${scope?.source_label || t("m2501")} · ${scope?.project_label || t("m1154", { value0: reporter.project_id })}`;
  };

  const visibleRun = runPage && runPage.event === selected?.event ? runPage.items : detail?.run_items ?? [];
  const runGroups = groupWorkRun(visibleRun);
  const showRunEvent = (item: WorkActivityItem) => <RunActivityRow key={item.event.event_ref} item={item} selected={item.event.event_ref === detail?.item.event.event_ref} onSelect={startSelection} />;

  return <section className="overview-page overview-page--activity" aria-label={t("m0156")}>
    <header className="overview-heading activity-page-header"><div><span className="eyebrow">{tx("m2502")}</span><h1>{tx("m0115")}</h1><p>{tx("m2503")}</p></div><button type="button" onClick={() => navigate("/inbox")}>{tx("m0167")}</button></header>
    <form className="overview-filters" onSubmit={event => { event.preventDefault(); const to = new Date(); const chosen = scopes?.items.find(item => `${item.source_id}:${item.project_id}` === scopeChoice); const range = periodChanged ? { from: new Date(to.getTime() - hours * 3600000).toISOString(), to: to.toISOString() } : { from: applied.from, to: applied.to }; setSelected(undefined); setCursor(undefined); setApplied({ ...range, source: chosen?.source_id ?? "", project: chosen?.project_id ?? "", sourceIDs: chosen ? [chosen.source_id] : scopeChoice === "__overview__" ? applied.sourceIDs : [], projectIDs: chosen ? [chosen.project_id] : scopeChoice === "__overview__" ? applied.projectIDs : [], resourceName: resourceName.trim() }); }}>
      <div className="overview-filter-controls"><label>{t("m1161")}<select value={hours} onChange={event => { setHours(Number(event.target.value)); setPeriodChanged(true); }}>{![24,168,720].includes(hours) ? <option value={hours}>{tx("m2504")}</option> : null}<option value={24}>{tx("m0590")}</option><option value={168}>{tx("m0591")}</option><option value={720}>{tx("m0592")}</option></select></label>
        <label>{t("m1264")}<select value={scopeChoice} onChange={event => setScopeChoice(event.target.value)} disabled={!scopes?.items.length}><option value="">{tx("m2505")}</option>{scopeChoice === "__overview__" ? <option value="__overview__">{tx("m2506")}</option> : null}{scopes?.items.filter((item, index, all) => all.findIndex(other => other.source_id === item.source_id && other.project_id === item.project_id) === index).map(item => <option key={`${item.source_id}:${item.project_id}`} value={`${item.source_id}:${item.project_id}`}>{tx("m1228", { value0: item.source_label, value1: item.project_label || t("m1154", { value0: item.project_id }) })}</option>)}</select></label>
        <label className="overview-filter-resource">{t("m2507")}<input value={resourceName} onChange={event => setResourceName(event.target.value)} placeholder={t("m2508")} maxLength={160} /></label>
        <button type="submit" disabled={!scopes?.items.length}>{tx("m2509")}</button></div>
    </form>
    <p className="activity-range">{tx("m2510", { value0: new Date(applied.from).toLocaleString(getLocale()), value1: new Date(applied.to).toLocaleString(getLocale()) })}</p>
    {scopeError ? <p role="alert">{scopeError}</p> : null}
    {scopes && !scopes.items.length ? <p>{tx("m2511")}</p> : null}
    {selected ? <article className="activity-detail" aria-label={t("m2512")} ref={detailLanding} tabIndex={-1}><button className="activity-detail-back" type="button" onClick={() => { setSelected(undefined); setDetail(undefined); }}>{tx("m2513")}</button>
      {detailError ? <p role="alert">{detailError}</p> : !detail ? <p role="status">{tx("m2514")}</p> : <>
        <div className="activity-detail-hero"><span className="eyebrow">{tx("m2515")}</span><h2>{workEventObject(detail.item.event)}</h2><strong>{workEventLabel(detail.item.event)}</strong><time dateTime={detail.item.event.occurred_at}>{new Date(detail.item.event.occurred_at).toLocaleString(getLocale())}</time></div>
        <div className="activity-detail-layout"><aside className="activity-detail-rail" aria-label={t("m2516")}><h3>{tx("m2517")}</h3>
          {detail.item.event.run_id ? <><small>{tx("m2518", { value0: visibleRun.length })}</small>
            <ul>{visibleRun.map(item => <li key={item.event.event_ref}><button type="button" aria-current={item.event.event_ref === detail.item.event.event_ref ? "step" : undefined} disabled={item.event.event_ref === detail.item.event.event_ref} onClick={() => startSelection(item)}><strong>{workEventObject(item.event)}</strong><span>{workEventLabel(item.event)} · {new Date(item.event.occurred_at).toLocaleString(getLocale())}</span></button></li>)}</ul>
            {(runPage ? runPage.more : detail.run_items_more) ? <small>{tx("m2519")}</small> : null}
          </> : <p>{tx("m2520")}</p>}
        </aside><div className="activity-detail-main"><h3>{tx("m2521")}</h3>
        <p className="activity-detail-result">{workEventResultNote(detail.item.event)}</p>
        {detail.item.event.subject_kind === "resource_action" ? <details><summary>{tx("m2522")}</summary><p>{tx("m2523")}</p></details> : null}
        {detail.item.event.resource?.name_state === "hidden" ? <p>{tx("m2524")}</p> : null}
        <details><summary>{tx("m2525")}</summary><p>{tx("m2527", { value0: content[detail.item.event.content_state] ?? t("m2526") })}</p></details>
        {(detail.review_bodies ?? []).length ? <section className="activity-detail-section activity-review-bodies"><h3>{tx("m2528")}</h3><ul>{detail.review_bodies.map(item => <li key={`${item.kind}:${item.ref}`}>
          {bodyKinds[item.kind] ?? t("m2529")}：{bodyStates[item.state] ?? t("m2530")}。
          {item.can_read ? <button type="button" disabled={bodyBusy} onClick={() => void readBody(item.ref, item.sha256)}>{tx("m0408")}</button> : null}
          <details><summary>{tx("m2531")}</summary>{item.kind} · {item.ref}</details></li>)}</ul>
          {bodyBusy ? <p role="status">{tx("m2532")}</p> : null}
          {bodyError ? <p role="alert">{bodyError}</p> : null}
          {bodyRead ? <section aria-label={t("m2533")}><p>{tx("m2534", { value0: bodyRead.result.bytes.byteLength })}</p>
            {bodyRead.result.text !== null ? <><pre className="work-activity-verified-body">{bodyRead.result.text}</pre>{bodyRead.result.previewOnly ? <p>{tx("m2535")}</p> : null}</> : <p>{tx("m2536")}</p>}
            <details><summary>{tx("m2537")}</summary>{bodyRead.ref}</details></section> : null}
        </section> : null}
        <section className="activity-detail-section" aria-label={t("m2538")}><h3>{tx("m2538")}</h3>
          {detail.relations.length ? <ul>{detail.relations.map(item => <li key={item.relation.relation_ref}>
            <p>{item.binding_state === "prior_revision" ? t("m2539", { value0: item.relation.event_revision }) :
              item.state === "ur_identifier_matched" ? t("m2540") : t("m2541")}</p>
            {item.binding_state === "current_revision" && item.state === "ur_identifier_matched" && item.runtime_request_ref && item.observed_at && item.relation.ur_target ?
              <button type="button" disabled={runtimeBusy} onClick={() => void openRelatedRequest(item)}>{tx("m2542")}</button> : null}
            <details><summary>{tx("m2543")}</summary>{item.relation.ur_request_id} · {item.relation.model_call_id}</details>
          </li>)}</ul> : <p>{tx("m2544")}</p>}
          {runtimeBusy ? <p role="status">{tx("m2545")}</p> : null}
          {runtimeError ? <p role="alert">{runtimeError}</p> : null}
        </section>
        </div><aside className="activity-detail-context" aria-label={t("m2546")}><h3>{tx("m2546")}</h3>
          <dl><div><dt>{tx("m2547")}</dt><dd>{scopeLabel(detail.item.event.reporter)}</dd></div>
            <div><dt>{tx("m2548")}</dt><dd>{actorLabel(detail.item.event.actor)}</dd></div>
            <div><dt>{tx("m2549")}</dt><dd>{tx("m1013")}</dd></div>
            <div><dt>{tx("m2550")}</dt><dd>{content[detail.item.event.content_state] ?? t("m2526")}</dd></div></dl>
          <details><summary>{tx("m2551")}</summary><p>{tx("m2552", { value0: detail.item.event.reporter.source_instance, value1: detail.item.event.reporter.org_id, value2: detail.item.event.reporter.project_id })}</p><p>{tx("m2553", { value0: detail.item.event.task_ref || t("m0100"), value1: detail.item.event.run_id || t("m0100"), value2: detail.item.event.model_call_id || t("m0100"), value3: detail.item.event.tool_call_id || t("m0100") })}</p><p>{tx("m2554", { value0: detail.item.event.revision, value1: detail.item.event_hash })}</p></details>
        </aside></div>
        {detail.item.event.run_id ? <details className="activity-run-expanded"><summary>{tx("m2555")}</summary><section aria-label={t("m2556")} className="activity-run activity-detail-section"><h3>{tx("m2557")}</h3>
          <p className="activity-run-boundary">{tx("m2558")}</p>
          {detail.run_summary ? <div className="activity-run-summary" aria-label={t("m2559")}>
            <div><span>{tx("m2560")}</span><strong>{detail.run_summary.distinct_resources}</strong><small>{tx("m2561")}</small></div>
            <div><span>{tx("m2562")}</span><strong>{Object.values(detail.run_summary.resource_read_outcomes).reduce((sum, value) => sum + value, 0)}</strong><small>{tx("m2563", { value0: detail.run_summary.resource_read_outcomes.completed, value1: detail.run_summary.resource_read_outcomes.failed, value2: detail.run_summary.resource_read_outcomes.optional_not_found, value3: detail.run_summary.resource_read_outcomes.unknown })}</small></div>
            <div><span>{tx("m2564")}</span><strong>{Object.values(detail.run_summary.resource_change_outcomes).reduce((sum, value) => sum + value, 0)}</strong><small>{tx("m2565", { value0: detail.run_summary.resource_change_outcomes.completed, value1: detail.run_summary.resource_change_outcomes.failed, value2: detail.run_summary.resource_change_outcomes.unknown })}</small></div>
            <div><span>{tx("m2566")}</span><strong>{detail.run_summary.model_calls}</strong><small>{tx("m2567", { value0: detail.run_summary.tool_calls, value1: detail.run_summary.unlinked_model_observations })}</small></div>
            <div><span>{tx("m2568")}</span><strong>{tx("m2569", { value0: detail.run_summary.task_completed })}</strong><small>{tx("m2570", { value0: detail.run_summary.task_failed, value1: detail.run_summary.task_unknown })}</small></div>
          </div> : <p>{tx("m2571")}</p>}
          <p>{tx("m2574", { value0: visibleRun.length, value1: (runPage ? runPage.more : detail.run_items_more) ? t("m2572") : t("m2573") })}</p>
          <div className="activity-run-groups">
            <section><h4>{tx("m1217")}</h4>{runGroups.reads.length ? <ul>{runGroups.reads.map(showRunEvent)}</ul> : <p>{tx("m2575")}</p>}</section>
            <section><h4>{tx("m2576")}</h4>{runGroups.changes.length ? <ul>{runGroups.changes.map(showRunEvent)}</ul> : <p>{tx("m2577")}</p>}</section>
            {runGroups.otherResources.length ? <section><h4>{tx("m2578")}</h4><ul>{runGroups.otherResources.map(showRunEvent)}</ul></section> : null}
            <section><h4>{tx("m2579")}</h4>{runGroups.modelCalls.length ? runGroups.modelCalls.map((call, index) => <div className="activity-model-call" key={call.id}><strong>{tx("m2580", { value0: index + 1 })}</strong><ul>{call.events.map(showRunEvent)}</ul><details><summary>{tx("m2581")}</summary><code>{call.id}</code></details></div>) : null}
              {runGroups.modelObservations.length ? <><h5>{tx("m2582")}</h5><ul>{runGroups.modelObservations.map(showRunEvent)}</ul></> : null}
              {!runGroups.modelCalls.length && !runGroups.modelObservations.length ? <p>{tx("m2583")}</p> : null}</section>
            <section><h4>{tx("m2584")}</h4>{runGroups.other.length ? <ul>{runGroups.other.map(showRunEvent)}</ul> : <p>{tx("m2585")}</p>}</section>
          </div>
          {runPage?.next ? <button type="button" disabled={runBusy} onClick={() => void loadMoreRun()}>{runBusy ? t("m2586") : t("m2587")}</button> : null}
          {runPage?.more && !runPage.next ? <p>{tx("m2588")}</p> : null}
          {runError ? <p role="alert">{runError}</p> : null}
          <details><summary>{tx("m2589")}</summary><div className="overview-table-scroll"><table><thead><tr><th>{tx("m1063")}</th><th>{tx("m2590")}</th><th>{tx("m2591")}</th><th>{tx("m2592")}</th><th>{tx("m2593")}</th></tr></thead><tbody>{visibleRun.map(item => <tr key={item.event.event_ref}>
            <td>{new Date(item.event.occurred_at).toLocaleString(getLocale())}</td><td>{actorLabel(item.event.actor)}</td><td>{workEventLabel(item.event)}</td><td>{workEventObject(item.event)}</td><td><button type="button" disabled={item.event.event_ref === detail.item.event.event_ref} onClick={() => startSelection(item)}>{tx("m2594")}</button></td>
          </tr>)}</tbody></table></div></details>
          <details><summary>{tx("m2595")}</summary><p>{tx("m2596", { value0: detail.run_cutoff ? new Date(detail.run_cutoff).toLocaleString(getLocale()) : t("m0100") })}</p></details>
        </section></details> : null}
      </>}
    </article> : null}
    <article className="overview-panel activity-list-panel"><header><h2>{tx("m2597")}</h2><small>{tx("m2598")}</small></header>
      <details className="activity-list-basis"><summary>{tx("m2599")}</summary><p>{tx("m2600")}</p></details>
      {busy ? <p role="status">{tx("m2601")}</p> : null}
      {error ? <p role="alert">{error}</p> : null}
      {scopes?.items.length && !busy && !error && !page?.items.length ? <p>{tx("m2602")}</p> : null}
      {page?.items.length ? <div className="activity-list-columns" aria-hidden="true"><span>{tx("m2603")}</span><span>{tx("m2604")}</span><span>{tx("m2605")}</span><span>{tx("m2593")}</span></div> : null}
      {page?.items.length ? <div className="activity-records">{groupActivityPage(page.items).map(group => <div className="activity-record-group" key={group.key}>
        {group.runID && group.items.length > 1 ? <h3 className="activity-run-label">{tx("m2606", { value0: group.items.length })}</h3> : null}
        <ul>{group.items.map(item => <li className="activity-record" key={`${item.event.reporter.source_system_id}:${item.event.reporter.source_instance}:${item.event.reporter.org_id}:${item.event.reporter.project_id}:${item.event.event_ref}`}>
          <div className="activity-record-main"><strong>{workEventObject(item.event)}</strong><span>{workEventLabel(item.event)}</span></div>
          <div className="activity-record-meta"><div className="activity-record-meta-primary"><time dateTime={item.event.occurred_at}>{new Date(item.event.occurred_at).toLocaleString(getLocale())}</time><span>{actorLabel(item.event.actor)}</span></div><small>{scopeLabel(item.event.reporter)}</small></div>
          <div className="activity-record-result"><strong>{workEventResultSummary(item.event)}</strong><details><summary>{tx("m2607")}</summary><p>{workEventResultNote(item.event)}</p></details></div>
          <button type="button" onClick={() => selectFromList(item)}>{tx("m2608")}</button>
        </li>)}</ul>
      </div>)}</div> : null}
      {page?.next_cursor ? <button type="button" onClick={() => setCursor(page.next_cursor)}>{tx("m2609")}</button> : null}
      {cursor ? <button type="button" onClick={() => setCursor(undefined)}>{tx("m2610")}</button> : null}
    </article>
  </section>;
}
