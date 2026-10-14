import { localizedLabels, getLocale, t, tx, useLocale } from "./i18n";
import { navigationOwner, type RuntimeNavigation } from "./workbenchNavigation";
import { ContentPanel } from "./ContentPanel";
import { ActionWitnessPanel } from "./ActionWitnessPanel";
import { Identifier, compactIdentifier } from "./Identifier";
import type { ExecutionContext } from "./assessmentTypes";
import { ReviewStart } from "./ReviewStart";
import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { accessAPI, ApiRequestError, isAbortError, runtimeAPI, scopeSessionKey, type AccessSessionResponse } from "./api";
import type * as Runtime from "./runtime";
import "./runtime.css";
import { ViewTabs } from "./DetailViewTabs";
import type { ExecutionView } from "./executionReading";
import { SourceFactCard } from "./SourceFactsPanel";

const dimensionNames: Record<Runtime.Dimension, string> = localizedLabels({}, {operation: "m0079", selected_model: "m1983", selected_provider: "m1984", required_capability: "m1985", enabled_capability: "m1986", destination_provider: "m1987", destination_origin: "m1988"});
const outcomeNames: Record<string, string> = localizedLabels({}, {local_delivery_completed: "m1124", technical_failure: "m1125", admission_rejected: "m1126", output_held: "m1127", interrupted: "m1128", context_done_unresolved: "m1129", unresolved: "m1130", conflicting: "m1131", missing_predecessor: "m1132"});
const stateNames: Record<string, string> = localizedLabels({}, {not_available: "m1989", not_observed: "m0210", unknown: "m0193", not_started: "m0826", registered: "m1990", available: "m1991", partial: "m1992", authenticated: "m1993", admitted: "m1994", rejected: "m1651", completed: "m1995", write_failed: "m1996", interrupted: "m1997", observed: "m1133", reserved: "m1998", settled: "m1999", not_created: "m2000", verified: "m2001", enabled: "m2002", not_applicable: "m0919", lease_valid_execution: "m2003", leased_pending: "m2004", last_known: "m2005", not_current: "m2006"});
const stateName = (s?: string) => s ? stateNames[s] ?? s : t("m0100");
const operationNames: Record<string,string> = localizedLabels({}, {"openai.chat_completions.v1": "m2007", "openai.responses.v1": "m2008", "openai.embeddings.v1": "m2009", chat: "m2007"});
const measurementNames: Record<string,string> = localizedLabels({}, {request_response_complete: "m2010", stream_first_valid_output: "m2011", stream_complete: "m2012", attempt_http_round_trip: "m2013"});
const observationNames: Record<string,string> = localizedLabels({}, {request_started: "m2014", admission_rejected: "m1126", request_terminal: "m2015", delivery_started: "m2016", local_delivery: "m2017", output_held: "m1127", request_context_done: "m2018", response_measurement: "m2019", first_output_measurement: "m2020", stream_measurement: "m2021", media_running: "m2022", media_terminal: "m2023", state_reconciled: "m2024", attempt_registered: "m2025", provider_response_received: "m2026", provider_rejected: "m2027", provider_outcome_unknown: "m2028", provider_response_unusable: "m2029", financial_result: "m2030", round_trip_measurement: "m2031", media_attempt_registered: "m2032", media_provider_succeeded: "m2033", media_retry_scheduled: "m2034", media_result: "m2035", selection: "m2036", input_result: "m2037", output_result: "m2038", stream_result: "m2039", dispatch_registered: "m2040", dispatch_result: "m2041", local_delivery_completed: "m1124"});
const readableStates: Record<string,string> = localizedLabels({}, {streaming: "m2042", non_streaming: "m2043", response_received: "m2044", accepted: "m1649", write_started: "m2045", prepared: "m2046", pending: "m2047", cancelled: "m2048", request_response_complete: "m2049", protocol_response_written: "m2050", protocol_terminal_written_and_flushed: "m2051", first_meaningful_frame_written_and_flushed: "m2052", http_do_returned: "m2053", local_completed: "m2054", legacy_unknown: "m2055", invalid: "m2056", configured: "m2057", do_observed: "m2058", response_observed: "m2059", known: "m2060", input: "m2061", output: "m2062", stream: "m2063", shadow: "m2064", enforce: "m2065", allow: "m1776", audit: "m2066", block: "m2067", replace: "m2068", mask: "m2069", match: "m2070", error: "m0979", not_covered: "m2071"});
const readableState = (value?:string) => value ? readableStates[value] ?? stateNames[value] ?? t("m2072") : t("m0100");
const operationName = (value:string) => operationNames[value] ?? t("m2073");
const measurementName = (value:string) => measurementNames[value] ?? t("m2074");
function displayMilliseconds(value?: string) {
  if (value === undefined) return t("m1139");
  const number = Number(value);
  if (!Number.isFinite(number)) return t("m1139");
  if (number > 0 && number < 0.1) return "<0.1";
  return new Intl.NumberFormat(getLocale(), { maximumFractionDigits: 1 }).format(number);
}

function time(value: string | undefined, timezone: string) {
  if (!value) return t("m0193");
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return t("m2075");
  return new Intl.DateTimeFormat(getLocale(), { dateStyle: "short", timeStyle: "medium", timeZone: timezone, hour12: false }).format(date);
}
function invalidReading(error: unknown) {
  return error instanceof ApiRequestError && [401, 403, 409, 410].includes(error.status);
}
function problem(error: unknown) {
  if (error instanceof ApiRequestError) {
    if (error.status === 403) return t("m2076");
    if (error.status === 409 || error.status === 410) return t("m2077");
    if (error.status === 401) return t("m0225");
    if (error.status === 400) return t("m2078");
  }
  return t("m2079");
}

// Each panel progresses independently. Reading/profile changes cancel old
// work and erase old values; a transient page failure can retry that cursor.
function usePage<P extends { items: unknown[]; next_cursor?: string }>(loader: (cursor: string | undefined, signal: AbortSignal) => Promise<P>, onInvalidated: (error: unknown) => void) {
  const [items, setItems] = useState<P["items"][number][]>([]);
  const [page, setPage] = useState<P | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);
  const generation = useRef(0);
  const controller = useRef<AbortController | null>(null);
  const failedCursor = useRef<string | undefined>(undefined);
  const load = useCallback((cursor?: string) => {
    const seq = ++generation.current;
    controller.current?.abort();
    const abort = new AbortController(); controller.current = abort;
    setBusy(true); setError(null); failedCursor.current = cursor;
    if (!cursor) { setItems([]); setPage(null); }
    void loader(cursor, abort.signal).then(next => {
      if (seq !== generation.current) return;
      setItems(previous => cursor ? [...previous, ...next.items] : next.items);
      setPage(next);
    }).catch(e => {
      if (seq !== generation.current || isAbortError(e)) return;
      setError(e);
      if (invalidReading(e)) { setItems([]); setPage(null); onInvalidated(e); }
    }).finally(() => { if (seq === generation.current) setBusy(false); });
  }, [loader, onInvalidated]);
  useEffect(() => { load(); return () => { generation.current++; controller.current?.abort(); }; }, [load]);
  return { items, page, error, busy, refresh: () => load(), more: () => load(page?.next_cursor), retry: () => load(failedCursor.current) };
}
function PageActions({ state, noun }: { state: { busy: boolean; error: unknown; page: { next_cursor?: string } | null; more: () => void; retry: () => void }; noun: string }) {
  useLocale();
  return <div className="runtime-page-actions">
    {state.busy ? <span role="status">{tx("m2080", { value0: noun })}</span> : null}
    {state.error ? <><p role="alert">{problem(state.error)}</p><button disabled={state.busy} onClick={state.retry}>{tx("m2081", { value0: noun })}</button></> : null}
    {!state.error && state.page?.next_cursor ? <button disabled={state.busy} onClick={state.more}>{tx("m2082", { value0: noun })}</button> : null}
  </div>;
}
function Fields({ rows }: { rows: [string, ReactNode][] }) {
  useLocale();
  return <dl className="runtime-fields">{rows.map(([name, value]) => <div key={name}><dt>{name}</dt><dd>{value ?? t("m0100")}</dd></div>)}</dl>;
}

interface RuntimeView {
  entry: Runtime.EntryTime;
  dimensions: { runtime: Runtime.Dimension; capabilities: Runtime.Dimension };
  selected: Runtime.Request | null;
  detailView?: ExecutionView;
}
const initialView = (): RuntimeView => ({ entry: "known", dimensions: { runtime: "operation", capabilities: "selected_model" }, selected: null });
// Kept only in this authenticated App instance. Returning rechecks the same
// server reading before restoring any request or source data into the DOM.
export interface RuntimeSnapshot {
  readingID?: string; // Retains the identity when invalidation clears its data.
  identity: string; hours: number; inheritedPeriod?: Runtime.ReadingRequest["period"]; source: string; project: string; costProject: string;
  reading: Runtime.Reading | null; applied: Runtime.ReadingRequest | null;
  deadline: number; view: RuntimeView; error: unknown;
}
export function RuntimePage({ session, capabilities = false, navigate, canInvestigate = false, snapshot, onSnapshot, initialRequest, navigation, navigationInvalid = false }: { navigation?: RuntimeNavigation; navigationInvalid?: boolean; initialRequest?: Runtime.ReadingRequest; session: AccessSessionResponse; capabilities?: boolean; navigate?: (path: string) => void; canInvestigate?: boolean; snapshot?: RuntimeSnapshot | null; onSnapshot?: (value: RuntimeSnapshot) => void }) {
  useLocale();
  // Within the same App lifetime, retain the original monotonic deadline,
  // failure and controls. History references restore only after that snapshot
  // is unavailable (for example, a refresh), and never override its failure.
  const saved = useRef(!navigationInvalid && snapshot && (!navigation ||
    navigation.owner === navigationOwner(session) && (snapshot.reading?.reading_id ?? snapshot.readingID) === navigation.readingID) ? snapshot : null);
  const initial = useRef(navigation?.request ?? initialRequest);
  const retainedNavigation = useRef(saved.current ? undefined : navigation);
  const snapshotReadingID = useRef(saved.current?.reading?.reading_id ?? saved.current?.readingID ?? navigation?.readingID);
  const timezone = saved.current?.applied?.period.display_timezone ?? navigation?.request.period.display_timezone ?? initial.current?.period.display_timezone ?? (Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC");
  const [owner, setOwner] = useState(saved.current?.identity ?? scopeSessionKey(session));
  const [view, setView] = useState<RuntimeView>(() => saved.current?.view ?? initialView());
  const [hours, setHours] = useState(saved.current?.hours ?? initial.current?.period.relative_hours ?? 24);
  const [inheritedPeriod, setInheritedPeriod] = useState<Runtime.ReadingRequest["period"] | undefined>(saved.current ? saved.current.inheritedPeriod : initial.current?.period.start_inclusive ? initial.current.period : undefined);
  const [source, setSource] = useState(saved.current?.source ?? initial.current?.filters.source_ids.join(", ") ?? "");
  const [project, setProject] = useState(saved.current?.project ?? initial.current?.filters.source_projects.join(", ") ?? "");
  const [costProject, setCostProject] = useState(saved.current?.costProject ?? initial.current?.filters.cost_projects.join(", ") ?? "");
  const [reading, setReading] = useState<Runtime.Reading | null>(null);
  const [applied, setApplied] = useState<Runtime.ReadingRequest | null>(null);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<unknown>(null);
  const [currentFilters, setCurrentFilters] = useState<Runtime.ReadingRequest["filters"] | null>(null);
  const controller = useRef<AbortController | null>(null);
  const generation = useRef(0);
  const readingDeadline = useRef(saved.current?.deadline ?? 0);
  const identity = scopeSessionKey(session);
  const csv = (value: string) => [...new Set(value.split(",").map(v => v.trim()).filter(Boolean))];
  const filters = () => ({ source_ids: csv(source), source_projects: csv(project), cost_projects: csv(costProject) });
  const invalidate = useCallback((e: unknown) => { setReading(null); setApplied(null); setView(initialView()); setError(e); }, []);
  const create = useCallback((input: Runtime.ReadingRequest) => {
    const seq = ++generation.current;
    controller.current?.abort(); controller.current = new AbortController();
    const sentAt = performance.now();
    setReading(null); setApplied(null); setView(initialView()); setError(null); setBusy(true);
    void runtimeAPI.createReading(input, session.csrf_token, controller.current.signal).then(next => {
      if (seq === generation.current) {
        // Use the server's lifetime and conservatively include request transit;
        // a workstation clock change must not extend reading eligibility.
        readingDeadline.current = sentAt + Date.parse(next.expires_at) - Date.parse(next.cutoff);
        if (!(readingDeadline.current > performance.now())) throw new ApiRequestError(410);
        retainedNavigation.current=undefined;
        snapshotReadingID.current=next.reading_id;
        setReading(next); setApplied(input);
      }
    }).catch(e => { if (seq === generation.current && !isAbortError(e)) setError(e); })
      .finally(() => { if (seq === generation.current) setBusy(false); });
  }, [session.csrf_token]);
  useEffect(() => {
    setCurrentFilters(null);
    const previous = saved.current;
    setOwner(identity);
    const retained=retainedNavigation.current;
    if(navigationInvalid){
      invalidate(new ApiRequestError(409));setBusy(false);
    } else if(retained){
      const seq=++generation.current;
      controller.current?.abort();const control=new AbortController();controller.current=control;
      setReading(null);setApplied(null);setBusy(true);setError(null);
      void (async()=>{
        if(retained.owner!==navigationOwner(session))throw new ApiRequestError(403);
        const [current,next]=await Promise.all([accessAPI.session(control.signal),runtimeAPI.reading(retained.readingID,control.signal)]);
        if(seq!==generation.current||control.signal.aborted)return;
        if(scopeSessionKey(current)!==identity||next.reading_id!==retained.readingID||next.contract_version!=="xinsight.runtime-reading.v1"||next.metric_version!=="xinsight.runtime.request-observations.v1"||
          next.period.start_inclusive!==retained.request.period.start_inclusive||next.period.end_exclusive!==retained.request.period.end_exclusive||next.period.timezone!==retained.request.period.display_timezone)throw new ApiRequestError(409);
        const deadline=performance.now()+Math.min(Date.parse(next.expires_at)-Date.now(),Date.parse(next.expires_at)-Date.parse(next.cutoff));
        if(!(deadline>performance.now()))throw new ApiRequestError(410);
        let selected:Runtime.Request|null=null;
        if(retained.selectedRef){
          let cursor:string|undefined;const seen=new Set<string>();
          do{
            const page=await runtimeAPI.requests(next.reading_id,retained.entry,cursor,control.signal);
            if(seq!==generation.current||control.signal.aborted)return;
            if(page.reading_id!==next.reading_id)throw new ApiRequestError(409);
            selected=page.items.find(item=>item.request_ref===retained.selectedRef)??null;
            if(selected)break;
            cursor=page.next_cursor;
            if(cursor&&seen.has(cursor))throw new ApiRequestError(409);
            if(cursor)seen.add(cursor);
          }while(cursor);
          if(!selected)throw new ApiRequestError(409);
        }
        if(!(deadline>performance.now()))throw new ApiRequestError(410);
        readingDeadline.current=deadline;setReading(next);setApplied(retained.request);
        setView({...initialView(),entry:retained.entry,selected,detailView:retained.detailView});
      })().catch(e=>{if(seq===generation.current&&!isAbortError(e))invalidate(e)})
        .finally(()=>{if(seq===generation.current)setBusy(false)});
    } else if (!previous) {
      create(initial.current ?? { contract_version: "xinsight.runtime-reading.v1", metric_version: "xinsight.runtime.request-observations.v1", period: { relative_hours: 24, display_timezone: timezone }, filters: { source_ids: [], source_projects: [], cost_projects: [] } });
    } else {
      const seq = ++generation.current;
      controller.current?.abort(); controller.current = new AbortController();
      setReading(null); setApplied(null); setBusy(true); setError(null);
      void (async () => {
        if (previous.identity !== identity) throw new ApiRequestError(403);
        if (!previous.reading || !previous.applied || !(previous.deadline > performance.now())) throw previous.error ?? new ApiRequestError(410);
        const next = await runtimeAPI.reading(previous.reading.reading_id, controller.current!.signal);
        if (seq !== generation.current) return;
        const stable = ["reading_id", "membership_ref", "authorization_revision", "interpretation_revision", "contract_version", "metric_version", "cutoff", "expires_at", "period"] as const;
        if (stable.some(key => JSON.stringify(next[key]) !== JSON.stringify(previous.reading![key]))) throw new ApiRequestError(409);
        if (!(previous.deadline > performance.now())) throw new ApiRequestError(410);
        readingDeadline.current = previous.deadline;
        setReading(next); setApplied(previous.applied); setView(previous.view);
      })().catch(e => { if (seq === generation.current && !isAbortError(e)) invalidate(e); })
        .finally(() => { if (seq === generation.current) setBusy(false); });
    }
    return () => { generation.current++; controller.current?.abort(); };
  }, [identity]); // CSRF refresh preserves the same reading; changed grants invalidate it.
  useEffect(() => {
    if (busy || owner !== identity) return;
    const value = { identity, hours, inheritedPeriod, source, project, costProject, reading, readingID: snapshotReadingID.current, applied, deadline: readingDeadline.current, view, error };
    saved.current = value;
    onSnapshot?.(value);
  }, [busy, owner, identity, hours, inheritedPeriod, source, project, costProject, reading, applied, view, error, onSnapshot]);
  useEffect(() => {
    if (!reading) return;
    const delay = Math.max(0, readingDeadline.current - performance.now());
    const timer = window.setTimeout(() => invalidate(new ApiRequestError(410)), Math.min(delay, 2_147_483_647));
    return () => window.clearTimeout(timer);
  }, [reading, invalidate]);

  return <div className="runtime-workbench">
    <header className="runtime-heading"><div><h1>{capabilities ? t("m0157") : t("m0155")}</h1><p>{capabilities ? t("m2083") : t("m2084")}</p></div></header>
    <form className="runtime-filters" onSubmit={event => { event.preventDefault(); create({ contract_version: "xinsight.runtime-reading.v1", metric_version: "xinsight.runtime.request-observations.v1", period: inheritedPeriod ?? { relative_hours: hours, display_timezone: timezone }, filters: filters() }); }}>
      <label>{t("m0473")}<select value={inheritedPeriod?"inherited":hours} onChange={e => { setInheritedPeriod(undefined); setHours(Number(e.target.value)); }}>{inheritedPeriod?<option value="inherited">{tx("m2085")}</option>:null}<option value={24}>{tx("m0590")}</option><option value={168}>{tx("m0591")}</option><option value={720}>{tx("m0592")}</option></select></label>
      <label>{t("m2086")}<input value={source} onChange={e => setSource(e.target.value)} placeholder={t("m2087")} /></label>
      <label>{t("m1180")}<input value={project} onChange={e => setProject(e.target.value)} placeholder={t("m2088")} /></label>
      <label>{t("m1182")}<input value={costProject} onChange={e => setCostProject(e.target.value)} placeholder={t("m2089")} /></label>
      <button className="primary-button" disabled={busy}>{tx("m2090")}</button>
      <p>{tx("m2092", { value0: inheritedPeriod?t("m2091"):"", value1: timezone })}</p>
    </form>
    {busy ? <p role="status">{retainedNavigation.current ? t("m2093") : t("m2094")}</p> : null}
    {error ? <p role="alert">{problem(error)}{retainedNavigation.current || navigationInvalid ? t("m2095") : ""}</p> : null}
    {reading && applied ? <RuntimeHistory session={session} navigate={navigate} canInvestigate={canInvestigate} key={reading.reading_id} view={view} setView={setView} reading={reading} filters={applied.filters} capabilities={capabilities} onInvalidated={invalidate} /> : null}
    {!capabilities ? <section className="runtime-panel" aria-labelledby="current-title"><h2 id="current-title">{tx("m2096")}</h2><p>{tx("m2097")}</p><button onClick={() => setCurrentFilters(filters())}>{tx("m2098")}</button>{currentFilters ? <CurrentPanel key={JSON.stringify(currentFilters)} filters={currentFilters} timezone={timezone} /> : null}</section> : null}
  </div>;
}

function RuntimeHistory({ session, reading, filters, capabilities, onInvalidated, navigate, canInvestigate, view, setView }: { session:AccessSessionResponse; view: RuntimeView; setView: (value: RuntimeView) => void; navigate?: (path: string) => void; canInvestigate: boolean; reading: Runtime.Reading; filters: Runtime.ReadingRequest["filters"]; capabilities: boolean; onInvalidated: (e: unknown) => void }) {
  useLocale();
  const { entry, selected } = view;
  const topic = capabilities ? "capabilities" : "runtime";
  const dimension = view.dimensions[topic];
  const setDimension = (value: Runtime.Dimension) => setView({ ...view, dimensions: { ...view.dimensions, [topic]: value } });
  const setSelected = (value: Runtime.Request | null) => setView({ ...view, selected: value });
  const zone = reading.period.timezone;
  return <>
    <section className="runtime-reading" aria-label={t("m2099")}><div className="runtime-reading-summary"><p><strong>{time(reading.period.start_inclusive, zone)} — {time(reading.period.end_exclusive, zone)}</strong> · {zone}</p><p>{tx("m2100", { value0: time(reading.cutoff, zone), value1: reading.requests_with_entry_time, value2: reading.requests_with_unknown_entry_time })}</p>
      {reading.coverage.missing_predecessors > 0 ? <p>{tx("m2101", { value0: reading.coverage.missing_predecessors })}</p> : null}
      {reading.coverage.streams.some(s => s.requires_attention || s.state !== "active") ? <p>{tx("m2102")}</p> : null}
      </div><div className="runtime-entry"><label>{t("m2103")}<select value={entry} onChange={e => { setView({ ...view, entry: e.target.value as Runtime.EntryTime, selected: null }); }}><option value="known">{tx("m2104")}</option><option value="unknown">{tx("m1266")}</option><option value="all">{tx("m2105")}</option></select></label>
      <p>{entry === "known" ? t("m2106") : entry === "unknown" ? t("m2107") : t("m2108")}</p>
      </div><details><summary>{tx("m2109")}</summary><p>{tx("m2111", { value0: filters.source_ids.join("、") || t("m2110"), value1: filters.source_projects.join("、") || t("m2110"), value2: filters.cost_projects.join("、") || t("m2110") })}</p><Fields rows={[[t("m2112"), <Identifier value={reading.reading_id} label={t("m2113")} />], [t("m2114"), reading.membership_ref], [t("m2115"), reading.interpretation_revision], [t("m2116"), time(reading.expires_at, zone)]]} />{reading.coverage.streams.map(s => <p key={[s.source_system_id, s.org_id, s.project_id, s.profile].join(":")}>{tx("m2117", { value0: s.source_instance, value1: s.org_id, value2: s.project_id, value3: s.profile, value4: stateName(s.state) })}</p>)}</details>
    </section>
    <div className="runtime-primary-comparison">
      <SummaryPanel key={`summary:${entry}`} reading={reading} entry={entry} onInvalidated={onInvalidated} />
      {!capabilities ? <MeasurementsPanel key={`measure:${entry}`} reading={reading} entry={entry} onInvalidated={onInvalidated} /> : null}
    </div>
    <section className="runtime-panel" aria-labelledby="dimensions-title"><h2 id="dimensions-title">{capabilities ? t("m2118") : t("m2119")}</h2><label>{t("m2120")}<select value={dimension} onChange={e => setDimension(e.target.value as Runtime.Dimension)}>{Object.entries(dimensionNames).map(([key, name]) => <option key={key} value={key}>{name}</option>)}</select></label>
      <DimensionsPanel key={`${dimension}:${entry}`} reading={reading} entry={entry} dimension={dimension} onInvalidated={onInvalidated} />
    </section>
    <RequestsPanel key={`requests:${entry}`} reading={reading} entry={entry} onSelect={setSelected} onInvalidated={onInvalidated} />
    {selected ? <section className="runtime-panel runtime-detail" aria-labelledby="runtime-detail-title"><div className="runtime-section-heading"><h2 id="runtime-detail-title">{tx("m2121")}</h2><button onClick={() => setSelected(null)}>{tx("m2122")}</button></div>
      <RuntimeRequestDetail session={session} reading={reading} request={selected} view={view.detailView ?? "story"} onViewChange={detailView => setView({ ...view, detailView })} onInvalidated={onInvalidated} navigate={navigate} canInvestigate={canInvestigate} />
    </section> : null}
  </>;
}

function SummaryPanel({ reading, entry, onInvalidated }: { reading: Runtime.Reading; entry: Runtime.EntryTime; onInvalidated: (e: unknown) => void }) {
  useLocale();
  const [summary, setSummary] = useState<Runtime.Summary | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController(); let live = true;
    setSummary(null); setError(null);
    void runtimeAPI.summary(reading.reading_id, entry, controller.signal).then(s => { if (live) setSummary(s); }).catch(e => { if (!live || isAbortError(e)) return; setError(e); if (invalidReading(e)) onInvalidated(e); });
    return () => { live = false; controller.abort(); };
  }, [reading.reading_id, entry, retry, onInvalidated]);
  const failure = summary?.diagnostic_failure;
  return <section className="runtime-panel" aria-labelledby="summary-title"><h2 id="summary-title">{tx("m2123")}</h2>{error ? <><p role="alert">{problem(error)}</p><button onClick={() => setRetry(n => n + 1)}>{tx("m2124")}</button></> : !summary ? <p role="status">{tx("m2125")}</p> : <>
    <div className="runtime-metrics"><div><span>{tx("m2126")}</span><strong>{summary.requests}</strong></div><div><span>{tx("m2127")}</span><strong>{failure!.numerator} / {failure!.denominator}</strong><small>{tx("m2128", { value0: failure!.state === "computed" && failure!.denominator > 0 ? `${(100 * failure!.numerator / failure!.denominator).toFixed(1)}%` : t("m1139"), value1: failure!.excluded_requests })}</small></div></div>
    <details><summary>{tx("m2129")}</summary><p>{tx("m2130")}</p></details>
    <ul className="runtime-outcomes">{summary.outcomes.map(o => <li key={o.outcome}><span>{outcomeNames[o.outcome] ?? o.outcome}</span><strong>{o.requests}</strong></li>)}</ul>
    {summary.requests === 0 ? <p>{tx("m2131")}</p> : null}
  </>}</section>;
}

function DimensionsPanel({ reading, entry, dimension, onInvalidated }: { reading: Runtime.Reading; entry: Runtime.EntryTime; dimension: Runtime.Dimension; onInvalidated: (e: unknown) => void }) {
  useLocale();
  const loader = useCallback((cursor: string | undefined, signal: AbortSignal) => runtimeAPI.dimensions(reading.reading_id, dimension, entry, cursor, signal), [reading.reading_id, dimension, entry]);
  const state = usePage(loader, onInvalidated);
  return <>{state.page ? <p>{tx("m2133", { value0: state.page.requests, value1: state.page.requests_with_values, value2: state.page.requests_without_values, value3: state.page.missing_predecessors > 0 ? t("m2132", { value0: state.page.missing_predecessors }) : "" })}</p> : null}
    <details><summary>{tx("m2134")}</summary><p>{dimension.startsWith("selected_") ? t("m2135") : dimension.startsWith("destination_") ? t("m2136") : dimension === "enabled_capability" ? t("m2137") : t("m2138")}</p></details>
    {state.items.length ? <div className="runtime-table-scroll"><table><thead><tr><th scope="col">{dimensionNames[dimension]}</th><th scope="col">{tx("m2139")}</th><th scope="col">{tx("m2140")}</th><th scope="col">{tx("m2141")}</th><th scope="col">{tx("m2142")}</th></tr></thead><tbody>{state.items.map(d => <tr key={JSON.stringify([d.value, d.evidence_state])}><td>{dimension === "selected_model" ? <><span>{tx("m2143")}</span><br /></> : null}<code>{d.value}</code></td><td>{stateName(d.evidence_state)}</td><td>{d.requests}</td><td>{d.subjects}</td><td>{d.observation_count}</td></tr>)}</tbody></table></div> : state.page ? <p>{tx("m2144")}</p> : null}
    <PageActions state={state} noun={t("m2145")} /></>;
}
function MeasurementBasis({ group: g }: { group: Runtime.MeasurementGroup }) {
  useLocale();
  const exclusionNames: Record<string,string> = { not_applicable:t("m0919"),not_observed:t("m1489"),legacy_unknown:t("m2146"),invalid:t("m2147"),missing_predecessor:t("m1132"),missing_context:t("m2148"),conflicting_context:t("m2149"),incompatible_context:t("m2150") };
  return <details><summary>{tx("m2151")}</summary>
    <Fields rows={[[t("m2152"),g.operation],[t("m2153"),g.stream_mode],[t("m2154"),`${g.measurement_kind} / ${g.measurement_version}`],[t("m2155"),g.completion_evidence],[t("m2156"),g.entry_time_state],[t("m2157"),`${g.percentiles.state} / ${g.percentiles.version}`],[t("m2158"),g.percentiles.unit]]}/>
    <Fields rows={Object.entries(g.excluded).map(([key,count])=>[exclusionNames[key]??key,count])}/>
  </details>;
}
function MeasurementsPanel({ reading, entry, onInvalidated }: { reading: Runtime.Reading; entry: Runtime.EntryTime; onInvalidated: (e: unknown) => void }) {
  useLocale();
  const loader = useCallback((cursor: string | undefined, signal: AbortSignal) => runtimeAPI.measurements(reading.reading_id, entry, cursor, signal), [reading.reading_id, entry]);
  const state = usePage(loader, onInvalidated);
  const comparable = state.items.filter(g => g.percentiles.sample_count > 0);
  const empty = state.items.filter(g => g.percentiles.sample_count === 0);
  const key = (g: Runtime.MeasurementGroup) => JSON.stringify([g.operation,g.stream_mode,g.measurement_kind,g.measurement_version,g.completion_evidence,g.entry_time_state]);
  return <section className="runtime-panel" aria-labelledby="measure-title"><h2 id="measure-title">{tx("m2159")}</h2>
    <details><summary>{tx("m2160")}</summary><p>{tx("m2161")}</p></details>
    {comparable.map(g => <article className="runtime-measurement" key={key(g)}><h3>{operationName(g.operation)} · {measurementName(g.measurement_kind)}</h3><p>{readableState(g.stream_mode)} · {readableState(g.completion_evidence)}</p>
      <Fields rows={[[t("m2162"),g.percentiles.sample_count],[t("m2163"),<span title={g.percentiles.p50}>{displayMilliseconds(g.percentiles.p50)}</span>],[t("m2164"),<span title={g.percentiles.p95}>{displayMilliseconds(g.percentiles.p95)}</span>]]}/>
      <MeasurementBasis group={g}/></article>)}
    {empty.length ? <details><summary>{tx("m2165", { value0: empty.length })}</summary>{empty.map(g => <article className="runtime-measurement" key={key(g)}><h3>{operationName(g.operation)} · {measurementName(g.measurement_kind)}</h3><Fields rows={[[t("m2162"),0],[t("m2142"),g.observation_count]]}/><MeasurementBasis group={g}/></article>)}</details> : null}
    {state.page && !comparable.length ? <p>{tx("m2166")}</p> : null}<PageActions state={state} noun={t("m2167")} />
  </section>;
}
function RequestsPanel({ reading, entry, onSelect, onInvalidated }: { reading: Runtime.Reading; entry: Runtime.EntryTime; onSelect: (r: Runtime.Request) => void; onInvalidated: (e: unknown) => void }) {
  useLocale();
  const loader = useCallback((cursor: string | undefined, signal: AbortSignal) => runtimeAPI.requests(reading.reading_id, entry, cursor, signal), [reading.reading_id, entry]);
  const state = usePage(loader, onInvalidated);
  return <section className="runtime-panel" aria-labelledby="requests-title"><h2 id="requests-title">{tx("m2168")}</h2>{state.items.length ? <div className="runtime-table-scroll"><table><thead><tr><th scope="col">{tx("m1211")}</th><th scope="col">{tx("m2169")}</th><th scope="col">{tx("m2170")}</th><th scope="col">{tx("m2142")}</th></tr></thead><tbody>{state.items.map(r => <tr key={r.request_ref}><td><button className="runtime-text-button" onClick={() => onSelect(r)} title={r.request_id}>{compactIdentifier(r.request_id)}</button>{r.request_id.length > 32 ? <Identifier value={r.request_id} label={t("m2171")} /> : null}</td><td>{r.source_instance} / {r.org_id} / {r.project_id}</td><td>{r.observation_count===0&&r.content_eligible_at?t("m2172", { value0: time(r.content_eligible_at,reading.period.timezone) }):r.entry_time_state === "observed" ? time(r.entry_at, reading.period.timezone) : t("m0193")}</td><td>{r.observation_count===0&&r.content_eligible_at?t("m2173"):r.observation_count}</td></tr>)}</tbody></table></div> : state.page ? <p>{tx("m2174")}</p> : null}<PageActions state={state} noun={t("m1211")} /></section>;
}

export function RuntimeRequestDetail({ executionContext, session, reading, request, view, onViewChange, onInvalidated, navigate, canInvestigate = false }: { executionContext?: ExecutionContext; session?: AccessSessionResponse; reading: Runtime.Reading; request: Runtime.Request; view: ExecutionView; onViewChange?: (view: ExecutionView) => void; onInvalidated: (e: unknown) => void; navigate?: (path: string) => void; canInvestigate?: boolean }) {
  useLocale();
  const contentOnly=request.observation_count===0;
  const active=contentOnly?"contents":view;
  return <><Identifier value={request.request_id} label={t("m2171")} /><p>{tx("m2175", { value0: request.source_instance, value1: request.org_id, value2: request.project_id })}</p>
    <p role="status">{tx("m2178", { value0: contentOnly ? t("m2176") : request.outcome ? outcomeNames[request.outcome] ?? t("m1269") : t("m1269"), value1: request.outcome === "unresolved" ? t("m2177") : null })}</p>
    <p>{contentOnly?t("m2179"):t("m2180")}</p>
    {onViewChange ? <ViewTabs active={active} onChange={onViewChange} contents={Boolean(session)} contentOnly={contentOnly} /> : null}
    {active==="contents" ? session ? <ContentPanel executionContext={executionContext} session={session} reading={reading} request={request} onInvalidated={onInvalidated} navigate={navigate}/> : <p>{tx("m2181")}</p> : <ObservationsPanel key={`${active}:${request.request_ref}`} reading={reading} request={request} mode={active} onInvalidated={onInvalidated} />}
    {session && (active === "story" || active === "systems") ? <ActionWitnessPanel reading={reading} request={request} session={session} onInvalidated={onInvalidated} /> : null}
    {(active === "story" || active === "systems") ? <ResourcesPanel executionContext={executionContext} session={session} navigate={navigate} key={JSON.stringify(["resources", reading.reading_id, reading.membership_ref, request.request_ref])} reading={reading} request={request} onInvalidated={onInvalidated} /> : null}
    {active === "story" ? <AssociationsPanel key={`associations:${request.request_ref}`} reading={reading} request={request} onInvalidated={onInvalidated} navigate={navigate} canInvestigate={canInvestigate} /> : null}
  </>;
}
function ObservationsPanel({ reading, request, onInvalidated, mode = "story" }: { reading: Runtime.Reading; request: Runtime.Request; onInvalidated: (e: unknown) => void; mode?: ExecutionView }) {
  useLocale();
  const loader = useCallback(async (cursor: string | undefined, signal: AbortSignal) => {
    const page = await runtimeAPI.observations(reading.reading_id, "observations", request.request_ref, cursor, signal);
    if (page.reading_id !== reading.reading_id || page.items.some(o => o.request_ref !== request.request_ref || o.source_system_id !== request.source_system_id ||
      o.document.source_instance !== request.source_instance || o.document.org_id !== request.org_id || o.document.project_id !== request.project_id || o.document.request_id !== request.request_id)) throw new ApiRequestError(409);
    return page;
  }, [reading.reading_id, request]);
  const state = usePage(loader, onInvalidated);
  const ordered = [...state.items].sort((a, b) => a.document.occurred_at.localeCompare(b.document.occurred_at) || a.document.event_id.localeCompare(b.document.event_id));
  const requestStages = ordered.filter(o => o.chain_state === "complete" && o.document.profile === "unirouter.runtime-observation.v1" && o.document.subject_kind === "request" && o.document.subject_id === request.request_id && o.document.runtime);
  const started = requestStages.some(o => o.document.observation_kind === "request_started");
  const ended = requestStages.some(o => ["request_terminal", "admission_rejected", "local_delivery", "output_held", "request_context_done"].includes(o.document.observation_kind));
  const latestStage = requestStages.at(-1)?.document;
  const allObservationsLoaded = Boolean(state.page && !state.page.next_cursor && !state.error);
  const groups = new Map<string, Runtime.Observation[]>();
  for (const o of ordered) { const key = `${o.document.subject_kind}:${o.document.subject_id}`; groups.set(key, [...(groups.get(key) ?? []), o]); }
  return <>
    {mode === "story" ? <section aria-label={t("m2182")}><h3>{tx("m2183")}</h3>
      <p>{tx("m2184", { value0: request.outcome ? outcomeNames[request.outcome] ?? t("m1269") : t("m1269") })}</p>
      {started && allObservationsLoaded && !ended ? <p>{tx("m2185")}</p> : null}
      {started && !allObservationsLoaded ? <p>{tx("m2186")}</p> : null}
      {!started && allObservationsLoaded ? <p>{tx("m2187")}</p> : null}
      {latestStage?.runtime ? <p>{tx("m2188", { value0: observationNames[latestStage.observation_kind] ?? latestStage.observation_kind, value1: readableState(latestStage.runtime.provider) })}</p> : null}
      <p>{tx("m2189")}</p>
    </section> : null}
    {mode === "graph" ? <section aria-label={t("m2190")}><h3>{tx("m2191")}</h3><p>{tx("m2192")}</p>
      {[...groups].map(([key, items]) => <details key={key}><summary>{tx("m2193", { value0: key, value1: items.length })}</summary>{items.map(o => <ObservationCard key={o.document.event_id} observation={o} timezone={reading.period.timezone} />)}</details>)}
    </section> : mode === "proof" ? <section aria-label={t("m2194")}><h3>{tx("m2195")}</h3><p>{tx("m2196")}</p>{ordered.map(o => <details key={o.document.event_id}><summary>{o.document.event_id}</summary><pre>{JSON.stringify(o, null, 2)}</pre></details>)}</section> :
      <>{mode === "systems" ? <><h3>{tx("m2197")}</h3><p>{tx("m2198")}</p></> : null}
        {ordered.filter(o => mode !== "systems" || o.document.profile === "unirouter.destination-observation.v1").map(o => <ObservationCard key={`${o.source_system_id}:${o.document.event_id}`} observation={o} timezone={reading.period.timezone} />)}</>}
    {state.page?.next_cursor ? <p>{tx("m2199")}</p> : null}<PageActions state={state} noun={t("m2200")} />
  </>;
}

export function ObservationCard({ observation: o, timezone }: { observation: Runtime.Observation; timezone: string }) {
  useLocale();
  const d = o.document;
  const kind = d.capability ? t("m2201") : d.attempt ? t("m2202") : d.policy ? t("m2203") : d.destination ? t("m2204") : t("m2205");
  return <article className="runtime-observation"><header><h3 title={d.subject_id}>{kind} · {compactIdentifier(d.subject_id)}</h3><span>{time(d.occurred_at, timezone)}</span></header>
    <p>{tx("m1670", { value0: observationNames[d.observation_kind] ?? t("m2206"), value1: d.revision })}</p>
    {o.chain_state !== "complete" ? <p>{tx("m2207")}</p> : <>
      {d.runtime ? <Fields rows={[[t("m0079"),operationName(d.runtime.operation)],[t("m2208"),readableState(d.runtime.stream_mode)],[t("m2209"),readableState(d.runtime.admission)],[t("m0352"),readableState(d.runtime.provider)],[t("m2210"),readableState(d.runtime.local_delivery)],[t("m2211"),readableState(d.runtime.financial)],[t("m2212"),readableState(d.runtime.context_done_reason)]]}/> : null}
      {d.attempt ? <><Fields rows={[[t("m0079"),operationName(d.attempt.operation)],[t("m2213"),`${d.attempt.route_sequence ?? t("m0193")} / ${d.attempt.route_total ?? t("m0193")}`],[t("m0352"),readableState(d.attempt.provider)],[t("m2214"),d.attempt.http_status],[t("m2215"),d.attempt.retry_authorized?t("m1699"):t("m1700")],[t("m2210"),readableState(d.attempt.local_delivery)],[t("m2211"),readableState(d.attempt.financial)]]}/><p>{tx("m2216")}</p></> : null}
      {d.capability ? <Fields rows={[[t("m1983"),d.capability.model_ref],[t("m1984"),d.capability.provider],[t("m0079"),operationName(d.capability.operation)],[t("m2217"),readableState(d.capability.evidence_state)],[t("m1985"),d.capability.required_capabilities.length?t("m2218", { value0: d.capability.required_capabilities.length }) : t("m2219")]]}/> : null}
      {d.policy ? <Fields rows={[[t("m2220"),readableState(d.policy.stage)],[t("m2221"),readableState(d.policy.mode)],[t("m2222"),readableState(d.policy.configured_action)],[t("m0554"),readableState(d.policy.outcome)],[t("m2223"),d.policy.effects.length?t("m2224", { value0: d.policy.effects.length }):t("m1489")],[t("m1487"),d.policy.degraded?t("m1699"):t("m1700")],[t("m2225"),d.policy.mandatory?t("m1699"):t("m1700")],[t("m2217"),readableState(d.policy.evidence_state)]]}/> : null}
      {d.destination ? <><Fields rows={[[t("m2226"),d.destination.provider],[t("m2227"),d.destination.origin],[t("m2228"),d.destination.region],[t("m2229"),readableState(d.destination.region_source)],[t("m2230"),readableState(d.destination.dispatch)],[t("m2231"),readableState(d.destination.policy_evidence)],[t("m2232"),readableState(d.destination.input_binding_state)]]}/><p>{tx("m2233")}</p></> : null}
      {d.measurement ? <Fields rows={[[t("m2167"),measurementName(d.measurement.kind)],[t("m2234"),readableState(d.measurement.state)],[t("m2235"),d.measurement.value ?? t("m0193")],[t("m2236"),d.measurement.unit==="nanoseconds"?t("m2237"):t("m2238")]]}/> : null}
      <details><summary>{tx("m2239")}</summary><Fields rows={[[t("m2240"),d.observation_kind],[t("m2241"),d.profile]]}/>
      {d.runtime ? <Fields rows={[[t("m0079"), d.runtime.operation], [t("m2208"), d.runtime.stream_mode], [t("m2209"), d.runtime.admission], [t("m0352"), d.runtime.provider], [t("m2210"), d.runtime.local_delivery], [t("m2211"), d.runtime.financial], [t("m2212"), d.runtime.context_done_reason]]} /> : null}
      {d.attempt ? <><Fields rows={[[t("m2242"), d.attempt.attempt_id], [t("m0079"), d.attempt.operation], [t("m2213"), `${d.attempt.route_sequence ?? t("m0193")} / ${d.attempt.route_total ?? t("m0193")}`], [t("m0352"), d.attempt.provider], [t("m2214"), d.attempt.http_status], [t("m2215"), d.attempt.retry_authorized ? t("m1699") : t("m1700")], [t("m2210"), d.attempt.local_delivery], [t("m2211"), d.attempt.financial]]} /></> : null}
      {d.capability ? <><Fields rows={[[t("m2143"), d.capability.model_ref], [t("m1984"), d.capability.provider], [t("m0079"), d.capability.operation], [t("m2217"), d.capability.evidence_state], [t("m2243"), `${d.capability.protocol.id} / ${d.capability.protocol.version}`], [t("m2244"), `${d.capability.execution_profile.id} / ${d.capability.execution_profile.version}`], [t("m1985"), d.capability.required_capabilities.join("、") || t("m2219")], [t("m2245"), d.capability.release_ref], [t("m2246"), d.capability.binding_ref], [t("m2247"), d.capability.probe_ref], [t("m2248"), d.capability.approval_ref]]} />{d.capability.capabilities.map((c, index) => <div className="runtime-capability" key={`${c.capability_id}:${index}`}><strong>{c.capability_id}</strong> · {c.support_state} · {c.evidence_state}<p>{tx("m2249", { value0: time(c.verified_at, timezone), value1: c.revision })}</p><details><summary>{tx("m2250")}</summary><code>{c.evidence_ref_sha256}</code><p>{tx("m2251")}</p></details></div>)}</> : null}
      {d.policy ? <Fields rows={[[t("m2252"), `${d.policy.policy_ref} / ${d.policy.policy_version}`], [t("m2253"), `${d.policy.detector_ref} / ${d.policy.detector_version}`], [t("m2220"), d.policy.stage], [t("m2221"), d.policy.mode], [t("m2222"), d.policy.configured_action], [t("m0554"), d.policy.outcome], [t("m2223"), d.policy.effects.join("、") || t("m1489")], [t("m1487"), d.policy.degraded ? t("m1699") : t("m1700")], [t("m2225"), d.policy.mandatory ? t("m1699") : t("m1700")], [t("m2217"), d.policy.evidence_state]]} /> : null}
      {d.destination ? <><Fields rows={[[t("m2226"), d.destination.provider], [t("m2227"), d.destination.origin], [t("m2228"), d.destination.region], [t("m2229"), d.destination.region_source], [t("m2230"), d.destination.dispatch], [t("m2231"), d.destination.policy_evidence], [t("m2232"), d.destination.input_binding_state]]} /></> : null}
      {d.measurement ? <Fields rows={[[t("m2167"), d.measurement.kind], [t("m2234"), d.measurement.state], [t("m2235"), d.measurement.value ?? t("m0193")], [t("m2236"), d.measurement.unit], [t("m2254"), d.measurement.start_boundary], [t("m2255"), d.measurement.end_boundary]]} /> : null}
      </details>
    </>}
    <details><summary>{tx("m2256")}</summary><Identifier value={d.subject_id} label={t("m2257")} /><Fields rows={[[t("m2258"), d.source_instance], [t("m2259"), o.source_system_id], [t("m2260"), `${d.org_id} / ${d.project_id}`], [t("m2261"), d.source_fact_id], [t("m2262"), d.event_id], [t("m2263"), o.source_hash], [t("m2264"), o.envelope_hash], [t("m0574"), time(d.recorded_at, timezone)], [t("m2265"), d.correction_reason]]} /></details>
  </article>;
}

function CurrentPanel({ filters, timezone }: { filters: Runtime.ReadingRequest["filters"]; timezone: string }) {
  useLocale();
  const loader = useCallback(async (cursor: string | undefined, signal: AbortSignal) => {
    const sentAt = performance.now();
    const page = await runtimeAPI.current(filters, cursor, signal);
    // Each page has its own server evaluation and request start. Charging the
    // whole round trip is conservative and does not depend on the host clock.
    return { ...page, items: page.items.map(item => ({ ...item, deadline: sentAt + Date.parse(item.entry.valid_until ?? "") - Date.parse(page.evaluated_at) })) };
  }, [filters]);
  const noHistoryInvalidation = useCallback(() => {}, []);
  const state = usePage(loader, noHistoryInvalidation);
  const [, updateClock] = useState(0);
  const now = performance.now();
  useEffect(() => {
    const next = Math.min(...state.items.map(i => i.deadline).filter(t => t > now));
    if (!Number.isFinite(next)) return;
    const timer = window.setTimeout(() => updateClock(n => n + 1), Math.min(Math.max(0, next - now + 1), 2_147_483_647));
    return () => window.clearTimeout(timer);
  }, [state.items, state.page, now]);
  return <div className="runtime-current"><p>{tx("m2266", { value0: filters.source_ids.join("、") || t("m2110"), value1: filters.source_projects.join("、") || t("m2110"), value2: filters.cost_projects.join("、") || t("m2110") })}</p><p>{tx("m2267")}</p>{state.page ? <><p>{tx("m2268", { value0: time(state.page.evaluated_at, timezone) })}</p><button disabled={state.busy} onClick={state.refresh}>{tx("m2269")}</button></> : null}
    {state.error && state.items.length > 0 ? <p>{tx("m2270")}</p> : null}
    {state.items.map(i => {
      const active = ["lease_valid_execution", "leased_pending"].includes(i.current_state);
      const effective = active && (state.busy || state.error || !Number.isFinite(i.deadline) || !(i.deadline > now)) ? "not_current" : i.current_state;
      return <article className="runtime-current-item" key={i.ref}><h3>{i.entry.request_id} · {stateName(effective)}</h3><p>{tx("m2271", { value0: i.source_instance, value1: i.org_id, value2: i.project_id, value3: i.entry.subject_id })}</p><Fields rows={[[t("m0079"), i.entry.operation], [t("m2272"), stateName(i.entry.original_state)], [t("m2273"), time(i.snapshot_observed_at, timezone)], [t("m2274"), time(i.entry.last_observed_at, timezone)], [t("m2275"), time(i.entry.valid_until, timezone)]]} /></article>;
    })}
    {state.page && !state.items.length ? <p>{tx("m2276")}</p> : null}<PageActions state={state} noun={t("m2096")} />
  </div>;
}

function AssociationsPanel({ reading, request, onInvalidated, navigate, canInvestigate }: { reading: Runtime.Reading; request: Runtime.Request; onInvalidated: (e: unknown) => void; navigate?: (path: string) => void; canInvestigate: boolean }) {
  useLocale();
 const loader = useCallback(async (cursor: string | undefined, signal: AbortSignal) => {
   const page = await runtimeAPI.associations(reading.reading_id, request.request_ref, cursor, signal);
   if (page.reading_id !== reading.reading_id || page.request_ref !== request.request_ref || page.membership_ref !== reading.membership_ref || page.associations_version !== "xinsight.runtime.associations.v1" || page.basis !== "same_source_request_identity") throw new ApiRequestError(409);
   return page;
 }, [reading.reading_id, reading.membership_ref, request.request_ref]);
 const state = usePage(loader, onInvalidated);
 return <section aria-label={t("m2277")}><h3>{tx("m2278")}</h3><p>{tx("m2279")}</p>
 {state.items.map(item => item.kind === "source" ? <SourceFactCard key={`${item.source.source_system_id}:${item.source.source_fact_id}`} fact={item.source} /> : <section className="source-fact-card" key={item.economic.economic_fact_id}><h4>{tx("m2280")}</h4><Fields rows={[[t("m2281"), item.economic.economic_fact_id], [t("m2282"), item.economic.execution_id], [t("m2263"), item.economic.source_event_hash]]} />
 {canInvestigate && navigate ? <button onClick={() => navigate(`/executions/${encodeURIComponent(item.economic.execution_id)}?as_of=${encodeURIComponent(reading.cutoff)}`)}>{tx("m2283")}</button> : null}</section>)}
 {state.page && state.items.length === 0 ? <p>{tx("m2284")}</p> : null}<PageActions state={state} noun={t("m2285")} /></section>;
}


const resourceInputNames: Record<Runtime.ResourceItem["input_state"], string> = localizedLabels({}, {not_confirmed: "m2286", reference_matched: "m2287", reference_mismatch: "m2288", conflicting_references: "m2289", withdrawn: "m2290", reassessment_required: "m2291", missing_predecessor: "m2292"});
const resourceNames: Record<string, string> = localizedLabels({}, {source_audit: "m2293", client_assertion: "m2294", enterprise_service: "m2295", pc: "m2296", known: "m2297", unknown: "m0193", managed: "m2298", unmanaged: "m2299", read: "m0083", search: "m2300", download: "m2301", denied: "m2302", failed: "m1088", allow: "m1776", deny: "m2303", provider_attempt: "m2304", media_attempt: "m2305", extract: "m2306", summarize: "m1194", redact: "m2307", translate: "m2308", truncate: "m2309"});
const resourceName = (value: string) => resourceNames[value] ?? value;
function resourceSelectionKey(item: Runtime.ResourceItem) {
  const { reporter, record_ref } = item.record;
  return JSON.stringify([reporter.source_system_id, reporter.source_instance, reporter.org_id, reporter.project_id, record_ref]);
}
function ResourcesPanel({ executionContext,session,navigate,reading, request, onInvalidated }: { executionContext?:ExecutionContext; session?:AccessSessionResponse; navigate?:(path:string)=>void; reading: Runtime.Reading; request: Runtime.Request; onInvalidated: (e: unknown) => void }) {
  useLocale();
  const loader = useCallback(async (cursor: string | undefined, signal: AbortSignal) => {
    const page = await runtimeAPI.resources(reading.reading_id, request.request_ref, cursor, signal);
    if (page.reading_id !== reading.reading_id || page.request_ref !== request.request_ref || page.membership_ref !== reading.membership_ref ||
        page.resources_version !== "xinsight.runtime.resources.v1" || page.coverage !== "synthetic_internal_records_only") throw new ApiRequestError(409);
    for (const item of page.items) {
      const record = item.record;
      if (record.schema_version !== "xinsight.resource-access.internal.v1" || record.synthetic !== true || item.content_verification !== "not_performed" ||
          item.input_evidence_basis !== "frozen_reference_witnesses" || record.request_id !== request.request_id ||
          ["source_system_id", "source_instance", "org_id", "project_id"].some(key => record.target[key as keyof Runtime.ResourceScope] !== request[key as keyof Runtime.ResourceScope])) throw new ApiRequestError(409);
    }
    return page;
  }, [reading.reading_id, reading.membership_ref, request]);
  const state = usePage(loader, onInvalidated);
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const selected = state.items.find(item => resourceSelectionKey(item) === selectedKey);
  useEffect(() => { if (selectedKey && !selected) setSelectedKey(null); }, [selectedKey, selected]);
  return <section aria-label={t("m0444")}><h3>{tx("m0444")}</h3>
    <p>{tx("m2310")}</p>
    <div className="runtime-resource-workspace">
      <div className="runtime-resource-list">
        {state.items.length ? <div className="runtime-table-scroll" role="region" aria-label={t("m2311")} tabIndex={0}><table>
          <caption>{tx("m2312")}</caption>
          <thead><tr><th scope="col">{tx("m2313")}</th><th scope="col">{tx("m2314")}</th><th scope="col">{tx("m2315")}</th><th scope="col">{tx("m2316")}</th><th scope="col">{tx("m2317")}</th><th scope="col">{tx("m2318")}</th><th scope="col">{tx("m2319")}</th></tr></thead>
          <tbody>{state.items.map(item => {
            const record = item.record, key = resourceSelectionKey(item);
            return <tr key={key} aria-selected={selectedKey === key}>
              <td><button type="button" className="runtime-text-button" aria-pressed={selectedKey === key} onClick={() => setSelectedKey(key)}>{record.resource.name}</button><br /><small>{tx("m2320", { value0: record.resource.version, value1: record.revision })}</small></td>
              <td>{resourceName(record.resource.location)}</td>
              <td>{resourceName(record.action)} / {resourceName(record.access_outcome)}</td>
              <td>{resourceName(record.access_decision)}</td>
              <td>{record.resource.classification === "known" ? record.resource.classification_ref || t("m2321") : t("m0193")}</td>
              <td>{resourceInputNames[item.input_state]}</td>
              <td>{item.changed_since_reading ? t("m2322") : t("m2323")}{record.withdrawn ? t("m2324") : ""}{item.chain_state !== "complete" ? t("m2325") : ""}</td>
            </tr>;
          })}</tbody>
        </table></div> : null}
        {state.page && !state.items.length ? <p>{tx("m2326")}</p> : null}
        <PageActions state={state} noun={t("m2327")} />
      </div>
      <aside className="runtime-resource-detail" aria-label={t("m2328")}>
        {!selected ? <p>{tx("m2329")}</p> : [selected].map(item => {
      const record = item.record, object = record.resource;
      return <article className="source-fact-card" key={JSON.stringify([resourceSelectionKey(item), record.revision, item.record_hash])}>
        <h4>{tx("m2330", { value0: object.name })}</h4><p><strong>{resourceInputNames[item.input_state]}</strong></p>
        {item.changed_since_reading ? <p>{tx("m2331")}</p> : null}
        <Fields rows={[[t("m1097"), resourceName(record.report_kind)], [t("m2332"), `${record.application_ref} / ${record.actor_ref}`],
          [t("m2333"), resourceName(object.location)], [t("m2313"), `${object.ref} / ${object.version}`],
          [t("m2334"), object.ownership === "known" ? object.owner_ref : t("m0193")], [t("m2317"), object.classification === "known" ? object.classification_ref : t("m0193")],
          [t("m2335"), resourceName(object.device_management)], [t("m2315"), `${resourceName(record.action)} / ${resourceName(record.access_outcome)}`],
          [t("m2316"), resourceName(record.access_decision)], [t("m2336"), record.decision_ref ? `${record.decision_ref} / ${record.policy_version}` : t("m0193")],
          [t("m2337"), t("m2338")], [t("m2339"), t("m2340")], [t("m1063"), time(record.occurred_at, reading.period.timezone)]]} />
        {record.access_outcome === "denied" ? <p>{tx("m2341")}</p> : null}
        {record.input ? <><Fields rows={[[t("m2342"), `${record.input.fragment_ref} / ${record.input.fragment_version}`],
          [t("m2343"), record.input.reported_input_ref], [t("m2344"), `${resourceName(record.input.attempt_kind)} / ${record.input.attempt_id}`]]} />
          <p>{tx("m2345")}</p>{record.input.transforms.length ? <ol>{record.input.transforms.map((step, i) => <li key={i}>{tx("m2346", { value0: resourceName(step.kind), value1: step.input_version, value2: step.output_version, value3: step.receipt_ref })}</li>)}</ol> : <p>{tx("m2347")}</p>}
        </> : null}
        {item.captured_input_evidence_refs.length ? <p>{tx("m2348", { value0: item.captured_input_evidence_refs.join("、") })}</p> : null}
        <details><summary>{tx("m2349")}</summary><Fields rows={[[t("m0472"), record.reporter.source_system_id], [t("m1325"), `${record.reporter.source_instance} / ${record.reporter.org_id} / ${record.reporter.project_id}`],
          [t("m2350"), `${record.record_ref} / ${record.revision}`], [t("m1194"), item.record_hash], [t("m2351"), record.previous_hash || t("m2352")],
          [t("m2353"), item.chain_state === "complete" ? t("m0920") : t("m1132")], [t("m2354"), record.withdrawn ? t("m1699") : t("m1700")]]} /></details>
        {session&&navigate?<ReviewStart key={executionContext ? JSON.stringify(executionContext) : "resource-only"} executionContext={executionContext} session={session} navigate={navigate} available={{evidence:[],gaps:[],resources:[{scope:"finding",reporter:record.reporter,target:record.target,request_id:record.request_id,record_ref:record.record_ref,revision:record.revision,record_hash:item.record_hash,relation:"context"}]}}/>:null}
      </article>;
    })}</aside>
    </div>
  </section>;
}
