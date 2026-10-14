import { localizedLabels, getLocale, t, tx, useLocale, useMessageState } from "./i18n";
import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { ApiRequestError, investigationQueueAPI, isAbortError, runtimeAPI, scopeSessionKey, workbenchAPI, workActivityAPI,
  type AccessSessionResponse, type InvestigationQueuePage, type WorkbenchContribution, type WorkbenchContributions, type WorkbenchReading,
  type WorkbenchReadingRequest, type WorkbenchSeries, type WorkbenchSourceStatuses, type WorkbenchSummary,
  type OverviewRecentRequests, type WorkActivitySummary, type WorkActivityScopes } from "./api";
import type * as Runtime from "./runtime";
import { formatDateTime } from "./format";
import { purposeLabels } from "./PurposePanel";
import { fixedReadingRequest, navigationOwner, usageDimension, type WorkbenchNavigation } from "./workbenchNavigation";
import "./overview.css";

export interface OverviewContext { request: WorkbenchReadingRequest; hours: number; contribution: "purpose" | "source_project"; contributionMetric?: ContributionMetric; economicReadingID?: string; usageDimension?: "purpose" | "account" | "source_project"; runtimeReadingID?: string; activitySourceChoice?: string }
interface Props { session: AccessSessionResponse; navigate: (path: string, selectedRuntimeRequestRef?: string, activityReading?: undefined, workbenchSelection?: WorkbenchNavigation) => void; context?: OverviewContext; onContext: (value: OverviewContext) => void }
type Dimension = WorkbenchContributions["dimension"];
type ContributionMetric = "amount" | "requests";
interface ContributionData { readingID: string; order: ContributionMetric; values: Partial<Record<Dimension, WorkbenchContributions>>; errors: Partial<Record<Dimension, string>> }
interface Data {
  reading?: WorkbenchReading; summary?: WorkbenchSummary; requests?: WorkbenchSeries; amount?: WorkbenchSeries;
  sources?: WorkbenchSourceStatuses;
  runtimeReading?: Runtime.Reading; runtime?: Runtime.Summary; queue?: InvestigationQueuePage; errors: Record<string, string>;
}
const empty = (): Data => ({ errors: {} });
const parts = (value: string) => value.split(",").map(item => item.trim()).filter(Boolean);
const outcomes: Record<string, string> = localizedLabels({}, {local_delivery_completed: "m1124", technical_failure: "m1125", admission_rejected: "m1126", output_held: "m1127", interrupted: "m1128", context_done_unresolved: "m1129", unresolved: "m1130", conflicting: "m1131", missing_predecessor: "m1132"});
const coverageNames: Record<string, string> = localizedLabels({}, {observed: "m1133", observed_with_gaps: "m1134", scope_limited_unknown: "m1135", disconnected: "m1136", not_connected: "m1137", period_completeness_unknown: "m1138"});
export function overviewMoney(value: string) {
  try { const n = BigInt(value); const digits = (n < 0n ? -n : n).toString().padStart(7, "0"); return `${n < 0n ? "-" : ""}¥${digits.slice(0, -6)}.${digits.slice(-6)}`; }
  catch { return t("m1139"); }
}
export function overviewShare(value: string, total: string) {
  try { const denominator = BigInt(total); return denominator === 0n ? t("m1139") : `${Number(BigInt(value) * 1000n / denominator) / 10}%`; }
  catch { return t("m1139"); }
}
function shareWidth(value: string, total: string) {
  try { const denominator = BigInt(total); return denominator > 0n ? `${Math.min(100, Math.max(0, Number(BigInt(value) * 100n / denominator)))}%` : undefined; }
  catch { return undefined; }
}
function failureMessage(error: unknown) {
  if (error instanceof ApiRequestError) {
    if (error.status === 403) return t("m1140");
    if ([409, 410].includes(error.status)) return t("m1141");
  }
  return t("m1142");
}
export function OverviewPage({ session, navigate, context, onContext }: Props) {
  useLocale();
  const initial = useRef(context).current;
  const [hours, setHours] = useState(initial?.hours ?? 168);
  const [source, setSource] = useState(initial?.request.filters.source_ids.join(",") ?? "");
  const [project, setProject] = useState(initial?.request.filters.source_projects.join(",") ?? "");
  const [costProject, setCostProject] = useState(initial?.request.filters.cost_projects.join(",") ?? "");
  const [advancedOpen, setAdvancedOpen] = useState(Boolean(initial?.request.filters.source_projects.length || initial?.request.filters.cost_projects.length || (initial?.request.filters.source_ids.length ?? 0) > 1));
  const [contribution, setContribution] = useState<"purpose" | "source_project">(initial?.contribution ?? "purpose");
  const contributionMetric: ContributionMetric = "requests";
  const [costDimension, setCostDimension] = useState<"application" | "source_project">("source_project");
  const [contributions, setContributions] = useState<ContributionData>();
  const [costContributions, setCostContributions] = useState<ContributionData>();
  const invalidateEconomic = useRef<(reason: string) => void>(() => {});
  const [moneyBasis, setMoneyBasis] = useState<WorkbenchReadingRequest["money_basis"]>(initial?.request.money_basis ?? "calculated_reference");
  const [metric, setMetric] = useState<"requests" | "amount">("requests");
  const [request, setRequest] = useState<WorkbenchReadingRequest>(() => initial?.request ?? makeRequest(168, "", "", "", "calculated_reference"));
  const [revision, setRevision] = useState(0);
  const [data, setData] = useState<Data>(empty);
  const [busy, setBusy] = useState(true);
  const [invalid, setInvalid] = useState("");
  const [recent, setRecent] = useState<OverviewRecentRequests>();
  const [recentCursor, setRecentCursor] = useState<string>();
  const [recentBusy, setRecentBusy] = useState(false);
  const [recentError, setRecentError] = useMessageState("");
  const [recentRuntimeUnavailable, setRecentRuntimeUnavailable] = useState(false);
  const [recentView, setRecentView] = useState<"runtime" | "economic">(session.session.permissions.includes("workbench.details.read") ? "runtime" : "economic");
  const [runtimeRecent, setRuntimeRecent] = useState<Runtime.Page<Runtime.Request>>();
  const [runtimeRecentCursor, setRuntimeRecentCursor] = useState<string>();
  const [runtimeRecentBusy, setRuntimeRecentBusy] = useState(false);
  const [runtimeRecentError, setRuntimeRecentError] = useMessageState("");
  const [activitySummary, setActivitySummary] = useState<WorkActivitySummary>();
  const [activityError, setActivityError] = useMessageState("");
  const [activityScopes, setActivityScopes] = useState<WorkActivityScopes>();
  const [activitySourceChoice, setActivitySourceChoice] = useState(initial?.activitySourceChoice ?? "__overview__");
  const identity = scopeSessionKey(session);
  const sink = useRef(onContext); sink.current = onContext;
  const permissions = session.session.permissions;
  const canRuntime = permissions.includes("workbench.summary.read") && permissions.includes("workbench.details.read");
  const canDetails = permissions.includes("workbench.details.read");
  const canQueue = permissions.includes("investigation.scoped.read") || permissions.includes("evidence.read");
  const canSources = permissions.includes("workbench.coverage.read");
  const activityChoiceKey = (item: { source_id: string; project_id: string }) => `${item.source_id}:${item.project_id}`;
  const chosenActivitySource = activityScopes?.items.find(item => activityChoiceKey(item) === activitySourceChoice);
  const activitySourceIDs = chosenActivitySource ? [chosenActivitySource.source_id] : activitySourceChoice === "__all__" ? [] : request.filters.source_ids;
  const activityProjectIDs = chosenActivitySource ? [chosenActivitySource.project_id] : activitySourceChoice === "__all__" ? [] :
    request.filters.source_projects.map(value => value.startsWith("project-") ? value.slice(8) : value);
  useEffect(() => {
    if (!canRuntime) return;
    const controller = new AbortController();
    void workActivityAPI.scopes(controller.signal).then(value => { if (!controller.signal.aborted) {
      setActivityScopes(value);
      setActivitySourceChoice(previous => previous === "__overview__" || previous === "__all__" ||
        value.items.some(item => activityChoiceKey(item) === previous) ? previous : "__overview__");
    } })
      .catch(() => { if (!controller.signal.aborted) setActivityScopes(undefined); });
    return () => controller.abort();
  }, [identity, canRuntime]);
  useEffect(() => {
    if (!canRuntime) { setActivitySummary(undefined); setActivityError(() => ""); return; }
    const controller = new AbortController();
    setActivitySummary(undefined); setActivityError(() => "");
    if (activitySourceChoice === "__overview__" && request.filters.cost_projects.length) {
      setActivityError(() => t("m1143"));
      return () => controller.abort();
    }
    const projects = activityProjectIDs;
    if (projects.some(value => !/^[1-9][0-9]{0,18}$/.test(value))) {
      setActivityError(() => t("m1144"));
      return () => controller.abort();
    }
    void workActivityAPI.summary({ from: request.period.start_inclusive ?? "", to: request.period.end_exclusive ?? "",
      source_ids: activitySourceIDs, project_ids: projects }, controller.signal)
      .then(value => { if (!controller.signal.aborted) setActivitySummary(value); })
      .catch(reason => { if (!controller.signal.aborted && !isAbortError(reason)) setActivityError(() => failureMessage(reason)); });
    return () => controller.abort();
  }, [request, identity, canRuntime, activitySourceChoice, activityScopes]);
  useEffect(() => { sink.current({ request, hours: Math.round((Date.parse(request.period.end_exclusive!) - Date.parse(request.period.start_inclusive!)) / 3600000), contribution, contributionMetric, economicReadingID: data.reading?.reading_id ?? (busy && revision === 0 ? initial?.economicReadingID : undefined), usageDimension: data.reading ? usageDimension(data.reading,data.summary?.actor_known_requests) : initial?.usageDimension, runtimeReadingID: data.runtimeReading?.reading_id ?? (busy && revision === 0 ? initial?.runtimeReadingID : undefined), activitySourceChoice }); }, [request, contribution, contributionMetric, data.reading, data.summary, data.runtimeReading, busy, revision, activitySourceChoice]);
  useEffect(() => { if (!canRuntime) setRecentView("economic"); }, [canRuntime]);
  useEffect(() => {
    const controller = new AbortController(); const signal = controller.signal;
    const timers: ReturnType<typeof setTimeout>[] = [];
    const expired = new Set<string>();
    const group = (key:string) => key === "queue" ? "queue" : key === "runtime" ? "runtime" : "economic";
    setData(empty()); setInvalid(""); setBusy(true);
    const update = (fn: (previous: Data) => Data) => { if (!signal.aborted) setData(fn); };
    const invalidate = () => { controller.abort(); setData(empty()); setBusy(false); setInvalid(t("m1145")); };
    const invalidateReading = (key: "economic" | "runtime", reason: string) => {
      expired.add(key);
      update(previous => key === "economic"
        ? { ...previous, reading: undefined, summary: undefined, requests: undefined, amount: undefined, sources: undefined, errors: { ...previous.errors, economic: reason } }
        : { ...previous, runtime: undefined, runtimeReading: undefined, errors: { ...previous.errors, runtime: reason } });
    };
    invalidateEconomic.current = reason => invalidateReading("economic", reason);
    const expire = (key: "economic" | "runtime", reading: { cutoff: string; expires_at: string }) => {
      const remaining = Date.parse(reading.expires_at) - Date.now();
      const reason = key === "economic" ? t("m1146") : t("m1147");
      if (!(remaining > 0)) { invalidateReading(key, reason); throw new Error("Reading expired before arrival"); }
      timers.push(setTimeout(() => invalidateReading(key, reason), Math.min(remaining, 2_147_483_647)));
    };
    const collect = async <T,>(key: string, load: () => Promise<T>, apply: (previous: Data, value: T) => Data) => {
      try { const value = await load(); if (!expired.has(group(key))) update(previous => apply(previous, value)); }
      catch (error) {
        if (signal.aborted || expired.has(group(key)) || isAbortError(error)) return;
        const readingInvalid = error instanceof ApiRequestError && (
          error.status === 410 && error.problem?.code === "workbench_reading_expired" ||
          error.status === 409 && error.problem?.code === "workbench_access_changed" ||
          error.status === 404 && error.problem?.code === "workbench_record_not_found");
        const keyGroup = group(key);
        if (readingInvalid && keyGroup !== "queue") invalidateReading(keyGroup, t("m1148"));
        else update(previous => ({ ...previous, errors: { ...previous.errors, [key]: failureMessage(error) } }));
      }
    };
    window.addEventListener("xinsight:scope-changed", invalidate);
    window.addEventListener("xinsight:unauthorized", invalidate);
    const economic = collect("economic", async () => {
      const reading = revision === 0 && initial?.economicReadingID ? await workbenchAPI.reading(initial.economicReadingID, signal) : await workbenchAPI.createReading(request, session.csrf_token, signal);
      if (signal.aborted) return;
      expire("economic", reading); update(previous => ({ ...previous, reading }));
      const bucket = Math.max(3600, Math.ceil((Date.parse(reading.period.end_exclusive) - Date.parse(reading.period.start_inclusive)) / 1000 / 48));
      await Promise.all([
        collect("summary", () => workbenchAPI.summary(reading.reading_id, signal), (d, summary) => ({ ...d, summary })),
        ...(["requests", "amount"] as const).map(kind => collect(kind, () => workbenchAPI.series(reading.reading_id, signal, bucket, kind), (d, series) => ({ ...d, [kind]: series }))),
        ...(canSources ? [collect("sources", () => workbenchAPI.sources(reading.reading_id, signal), (d, sources) => ({ ...d, sources }))] : []),
      ]);
    }, d => d);
    const runtime = canRuntime ? collect("runtime", async () => {
      const reading = revision === 0 && initial?.runtimeReadingID ? await runtimeAPI.reading(initial.runtimeReadingID, signal) : await runtimeAPI.createReading({ contract_version: "xinsight.runtime-reading.v1", metric_version: "xinsight.runtime.request-observations.v1", period: request.period, filters: request.filters }, session.csrf_token, signal);
      if (signal.aborted) return undefined;
      expire("runtime", reading);
      update(previous => ({ ...previous, runtimeReading: reading }));
      const summary = await runtimeAPI.summary(reading.reading_id, "known", signal);
      return { reading, summary };
    }, (d, value) => value ? { ...d, runtimeReading: value.reading, runtime: value.summary } : d) : Promise.resolve();
    const queue = canQueue ? collect("queue", () => investigationQueueAPI.list({ mode: "current", period: "24h", sort: "created_desc" }, undefined, signal), (d, value) => ({ ...d, queue: value })) : Promise.resolve();
    void Promise.all([economic, runtime, queue]).finally(() => { if (!signal.aborted) setBusy(false); });
    return () => { controller.abort(); timers.forEach(clearTimeout); window.removeEventListener("xinsight:scope-changed", invalidate); window.removeEventListener("xinsight:unauthorized", invalidate); };
  }, [request, revision, identity, session.csrf_token, canRuntime, canQueue, canSources]);
  // Load only the selected ranking, retaining the reading's frozen membership.
  // Separate cancellation also prevents a slower previous sort from repainting.
  useEffect(() => {
    const reading = data.reading;
    setContributions(undefined);
    if (!reading) return;
    const controller = new AbortController();
    const signal = controller.signal;
    const invalidate = () => { controller.abort(); setContributions(undefined); };
    window.addEventListener("xinsight:scope-changed", invalidate);
    window.addEventListener("xinsight:unauthorized", invalidate);
    setContributions({ readingID: reading.reading_id, order: contributionMetric, values: {}, errors: {} });
    for (const dimension of (["model", "application", "purpose"] as const).filter(value => reading.dimensions.includes(value))) {
      void workbenchAPI.contributions(reading.reading_id, dimension, { topN: 5, orderBy: contributionMetric }, signal).then(value => {
        if (!signal.aborted) setContributions(previous => previous ? { ...previous, values: { ...previous.values, [dimension]: value } } : previous);
      }).catch(error => {
        if (signal.aborted || isAbortError(error)) return;
        if (error instanceof ApiRequestError && (
          error.status === 410 && error.problem?.code === "workbench_reading_expired" ||
          error.status === 409 && error.problem?.code === "workbench_access_changed" ||
          error.status === 404 && error.problem?.code === "workbench_record_not_found")) {
          invalidate();
          invalidateEconomic.current(t("m1148"));
        } else setContributions(previous => previous ? { ...previous, errors: { ...previous.errors, [dimension]: failureMessage(error) } } : previous);
      });
    }
    return () => { controller.abort(); window.removeEventListener("xinsight:scope-changed", invalidate); window.removeEventListener("xinsight:unauthorized", invalidate); };
  }, [data.reading, contributionMetric, identity]);
  useEffect(() => {
    const reading = data.reading;
    setCostContributions(undefined);
    if (!reading) return;
    const controller = new AbortController();
    const signal = controller.signal;
    const invalidate = () => { controller.abort(); setCostContributions(undefined); };
    window.addEventListener("xinsight:scope-changed", invalidate);
    window.addEventListener("xinsight:unauthorized", invalidate);
    setCostContributions({ readingID: reading.reading_id, order: "amount", values: {}, errors: {} });
    const dimension = reading.dimensions.includes(costDimension) ? costDimension : "source_project";
    if (reading.dimensions.includes(dimension)) {
      void workbenchAPI.contributions(reading.reading_id, dimension, { topN: 5, orderBy: "amount" }, signal).then(value => {
        if (!signal.aborted) setCostContributions(previous => previous ? { ...previous, values: { ...previous.values, [dimension]: value } } : previous);
      }).catch(error => {
        if (signal.aborted || isAbortError(error)) return;
        if (error instanceof ApiRequestError && (error.status === 410 && error.problem?.code === "workbench_reading_expired" || error.status === 409 && error.problem?.code === "workbench_access_changed" || error.status === 404 && error.problem?.code === "workbench_record_not_found")) {
          invalidate(); invalidateEconomic.current(t("m1148"));
        } else setCostContributions(previous => previous ? { ...previous, errors: { ...previous.errors, [dimension]: failureMessage(error) } } : previous);
      });
    }
    return () => { controller.abort(); window.removeEventListener("xinsight:scope-changed", invalidate); window.removeEventListener("xinsight:unauthorized", invalidate); };
  }, [data.reading, costDimension, identity]);
  useEffect(() => { setRecent(undefined); setRecentCursor(undefined); setRecentRuntimeUnavailable(false); }, [data.reading?.reading_id, data.runtimeReading?.reading_id]);
  useEffect(() => {
    if (busy || !data.reading || !canDetails) { if (!data.reading) setRecent(undefined); return; }
    const controller = new AbortController();
    const reading = data.reading;
    const runtimeID = recentRuntimeUnavailable ? undefined : data.runtimeReading?.reading_id;
    if (!recentCursor) setRecent(undefined);
    setRecentBusy(true); setRecentError(() => "");
    void (async () => {
      let page: OverviewRecentRequests;
      try { page = await workbenchAPI.recentRequests(reading.reading_id, runtimeID, recentCursor, controller.signal); }
      catch (runtimeError) {
        if (!runtimeID || controller.signal.aborted || isAbortError(runtimeError)) throw runtimeError;
        if (!(runtimeError instanceof ApiRequestError) || ![400, 403, 404, 409, 410].includes(runtimeError.status)) throw runtimeError;
        setRecent(undefined); setRecentCursor(undefined); setRecentRuntimeUnavailable(true); return;
      }
      if (controller.signal.aborted) return;
      if (page.reading_id !== reading.reading_id || page.membership_ref !== reading.membership_ref || page.runtime_reading_id !== runtimeID) throw new Error("Reading mismatch");
      setRecent(previous => recentCursor && previous ? { ...page, items: [...previous.items, ...page.items] } : page);
    })().catch(error => {
      if (controller.signal.aborted || isAbortError(error)) return;
      setRecentError(() => failureMessage(error));
      if (error instanceof ApiRequestError && [409, 410].includes(error.status)) setRecent(undefined);
    }).finally(() => { if (!controller.signal.aborted) setRecentBusy(false); });
    return () => controller.abort();
  }, [busy, data.reading, data.runtimeReading, canDetails, recentCursor, recentRuntimeUnavailable, identity]);
  useEffect(() => { setRuntimeRecent(undefined); setRuntimeRecentCursor(undefined); }, [data.runtimeReading?.reading_id, identity]);
  useEffect(() => {
    if (!data.runtimeReading || !canDetails) { if (!data.runtimeReading) setRuntimeRecent(undefined); return; }
    const controller = new AbortController();
    const reading = data.runtimeReading;
    if (!runtimeRecentCursor) setRuntimeRecent(undefined);
    setRuntimeRecentBusy(true); setRuntimeRecentError(() => "");
    void runtimeAPI.requests(reading.reading_id, "all", runtimeRecentCursor, controller.signal).then(page => {
      if (controller.signal.aborted) return;
      if (page.reading_id !== reading.reading_id) throw new Error("Reading mismatch");
      setRuntimeRecent(previous => runtimeRecentCursor && previous?.reading_id === page.reading_id ? { ...page, items: [...previous.items, ...page.items] } : page);
    }).catch(error => {
      if (controller.signal.aborted || isAbortError(error)) return;
      setRuntimeRecentError(() => failureMessage(error));
      if (error instanceof ApiRequestError && [403, 409, 410].includes(error.status)) setRuntimeRecent(undefined);
    }).finally(() => { if (!controller.signal.aborted) setRuntimeRecentBusy(false); });
    return () => controller.abort();
  }, [data.runtimeReading, canDetails, runtimeRecentCursor, identity]);
  function apply(event: FormEvent) { event.preventDefault(); setRecent(undefined); setRecentCursor(undefined); setRuntimeRecent(undefined); setRuntimeRecentCursor(undefined); setRequest(makeRequest(hours, source, project, costProject, moneyBasis)); setRevision(value => value + 1); }
  function clearFilters() { setSource(""); setProject(""); setCostProject(""); setRecent(undefined); setRecentCursor(undefined); setRuntimeRecent(undefined); setRuntimeRecentCursor(undefined); setRequest(makeRequest(hours, "", "", "", moneyBasis)); setRevision(value => value + 1); }
  const message = (key: string, fallback = t("m1149")) => data.errors.economic && !["runtime", "queue"].includes(key) ? data.errors.economic : data.errors[key] ?? (busy ? t("m1150") : fallback);
  const currentContributions = contributions?.readingID === data.reading?.reading_id && contributions?.order === contributionMetric ? contributions : undefined;
  const currentCostContributions = costContributions?.readingID === data.reading?.reading_id ? costContributions : undefined;
  const appliedCostDimension = data.reading?.dimensions.includes(costDimension) ? costDimension : "source_project";
  const contributionMessage = (dimension: Dimension) => data.errors.economic ?? currentContributions?.errors[dimension] ?? (data.reading?.dimensions.includes(dimension) ? t("m1150") : message(dimension, t("m1151")));
  const moneyTitle = request.money_basis === "calculated_reference" ? t("m1152") : t("m1153");
  const displayMoneyTitle = data.reading ? (data.reading.money_basis === "calculated_reference" ? t("m1152") : t("m1153")) : moneyTitle;
  const costContributionMessage = data.errors.economic ?? currentCostContributions?.errors[appliedCostDimension] ?? (data.reading?.dimensions.includes(appliedCostDimension) ? t("m1150") : message(appliedCostDimension, t("m1151")));
  const localCompleted = data.runtime?.outcomes.find(item => item.outcome === "local_delivery_completed")?.requests;
  const resultAttention = data.runtime?.outcomes.filter(item => ["technical_failure", "interrupted", "output_held"].includes(item.outcome)).reduce((sum, item) => sum + item.requests, 0);
  const series = data[metric];
  const sourceChoices = data.sources?.items ?? [];
  const selectedSource = !source ? "" : sourceChoices.some(item => item.source_system_id === source) ? source : "__precise__";
  const appliedFilters = request.filters;
  const activeFilterCount = appliedFilters.source_ids.length + appliedFilters.source_projects.length + appliedFilters.cost_projects.length;
  const appliedSourceNames = appliedFilters.source_ids.map(id => sourceChoices.find(item => item.source_system_id === id)?.display_name ?? id);
  const activityScopeLabel = chosenActivitySource ? `${chosenActivitySource.source_label} · ${chosenActivitySource.project_label || t("m1154", { value0: chosenActivitySource.project_id })}` : activitySourceChoice === "__all__" ? t("m1155") : t("m1156");
  const activityCoverageLabels = activitySummary ? [...new Set(activitySummary.coverage.map(code => coverageNotes[code] ?? t("m1157")))] : [];
  const openWorkbench = (dimension: WorkbenchNavigation["dimension"], usage: boolean, selectedRef?: string, activeBucket = "") => {
    if (!data.reading?.dimensions.includes(dimension)) return;
    const selection: WorkbenchNavigation = { version: 1, owner: navigationOwner(session), readingID: data.reading.reading_id,
      request: fixedReadingRequest(data.reading, request), dimension, mode: "top5", resolution: "overview", activeBucket,
      selectedRef, contributionPages: 1, detailPages: 1 };
    navigate(usage ? "/workbench/cost?view=usage" : "/workbench/cost", undefined, undefined, selection);
  };
  return <section className="overview-page overview-page--home" aria-label={t("m1158")}>
    <div className="overview-top"><header className="overview-heading"><span className="eyebrow">{tx("m1159")}</span><h1>{tx("m0121")}</h1><p>{tx("m1160")}</p></header>
    <form className="overview-filters" onSubmit={apply}>
      <div className="overview-filter-controls"><label>{t("m1161")}<select value={hours} onChange={event => setHours(Number(event.target.value))}><option value={24}>{tx("m1162")}</option><option value={168}>{tx("m1163")}</option><option value={720}>{tx("m1164")}</option></select></label>
      {canSources ? <label className="overview-filter-source">{t("m0472")}<select aria-label={t("m0472")} value={selectedSource} disabled={!data.sources} onChange={event => setSource(event.target.value)}><option value="">{tx("m1165")}</option>{selectedSource === "__precise__" ? <option value="__precise__" disabled>{tx("m1166")}</option> : null}{sourceChoices.map(item => <option key={item.source_system_id} value={item.source_system_id}>{tx("m1167", { value0: item.display_name })}</option>)}</select></label> : null}
      <label>{t("m1168")}<select value={moneyBasis} onChange={event => setMoneyBasis(event.target.value as typeof moneyBasis)}><option value="calculated_reference">{tx("m1169")}</option><option value="gateway_charged">{tx("m1170")}</option></select></label>
      <button type="submit">{tx("m1171")}</button></div>
      <div className="overview-filter-meta"><div className="overview-active-filters"><span>{tx("m1175", { value0: activeFilterCount ? t("m1173", { value0: appliedSourceNames.join("、") || t("m1172"), value1: appliedFilters.source_projects.join("、") || t("m0618"), value2: appliedFilters.cost_projects.join("、") || t("m0618") }) : t("m1174"), value1: formatDateTime(request.period.start_inclusive), value2: formatDateTime(request.period.end_exclusive), value3: moneyTitle })}</span>{activeFilterCount ? <button type="button" onClick={clearFilters}>{tx("m1176")}</button> : null}</div>
      <details className="overview-advanced" open={advancedOpen} onToggle={event => setAdvancedOpen(event.currentTarget.open)}><summary>{tx("m1177")}</summary><div className="overview-advanced-content">
        <label>{t("m1178")}<input value={source} onChange={event => setSource(event.target.value)} placeholder={t("m1179")} /></label>
        <label>{t("m1180")}<input value={project} onChange={event => setProject(event.target.value)} placeholder={t("m1181")} /></label>
        <label>{t("m1182")}<input value={costProject} onChange={event => setCostProject(event.target.value)} placeholder={t("m1181")} /></label>
        <p className="overview-period">{tx("m1183", { value0: formatDateTime(request.period.start_inclusive), value1: formatDateTime(request.period.end_exclusive) })}</p>
      </div></details></div>
    </form></div>
    {invalid ? <p role="alert">{invalid}</p> : null}
    <div className="overview-quick-scan" aria-label={t("m1184")}><div className="overview-stats">
      <Metric title={t("m1185")} value={data.summary?.request_count} note={data.summary?.request_total_complete ? t("m1186") : t("m1187")} unavailable={message("summary")} />
      <Metric title={displayMoneyTitle} value={data.summary ? overviewMoney(data.summary.selected_amount_micros) : undefined} note="" unavailable={message("summary")} />
      <Metric title={t("m1188")} value={data.runtime ? `${localCompleted ?? 0} / ${data.runtime.requests}` : undefined} note={data.runtime ? t("m1189", { value0: resultAttention ?? 0 }) : ""} unavailable={canRuntime ? message("runtime") : t("m1190")} />
      <Metric title={t("m1191")} value={data.queue?.total} note={t("m1192")} unavailable={canQueue ? message("queue") : t("m1193")} />
    </div><OverviewInfo label={t("m1194")}><p>{tx("m1195")}</p></OverviewInfo></div>
    <p className="overview-coverage-note" role="status">{data.reading ? t("m1201", { value0: coverageNames[data.reading.coverage.status] ?? t("m1196"), value1: data.reading.coverage.period_completeness === "unknown" ? t("m1197") : t("m1198"), value2: data.summary?.request_total_complete ? t("m1199") : t("m1200") }) : t("m1202", { value0: message("economic") })}
      {canSources ? data.sources?.items.some(item => item.status === "disconnected" || item.status === "not_connected") ? t("m1203") : data.sources ? t("m1204") : t("m1205") : t("m1206")}</p>
    <section className="overview-observatory-row" aria-label={t("m1207")}>
      <article className="overview-panel overview-trend-panel"><header><h2>{metric === "requests" ? t("m1208") : t("m1209", { value0: displayMoneyTitle })}</h2><select aria-label={t("m1210")} value={metric} onChange={e => setMetric(e.target.value as typeof metric)}><option value="requests">{tx("m1211")}</option><option value="amount">{tx("m0495")}</option></select></header>
        <OverviewInfo label={t("m1212")}><p>{tx("m1215", { value0: displayMoneyTitle, value1: request.money_basis === "calculated_reference" ? t("m1213") : t("m1214") })}</p></OverviewInfo>
        {series ? <Trend key={`${series.reading_id}:${series.metric}`} series={series} moneyTitle={displayMoneyTitle} onOpen={bucket => openWorkbench("source_project", metric === "requests", undefined, bucket)} /> : <p role="status">{message(metric)}</p>}
      </article>
      <article className="overview-panel overview-activity-entry"><header><h2>{tx("m0115")}</h2><small>{tx("m1216")}</small></header>
        {activitySummary ? <div className="overview-resource-summaries"><ResourceOutcomeRow title={t("m1217")} total={activitySummary.reported_resource_reads} outcomes={activitySummary.resource_read_outcomes} /><ResourceOutcomeRow title={t("m1218")} total={activitySummary.reported_resource_changes} outcomes={activitySummary.resource_change_outcomes} /></div> : null}
        <div className="overview-activity-attention"><span>{tx("m1219")}</span><strong>{activitySummary?.sources_needing_attention ?? "—"}</strong><small>{activitySummary ? t("m1220") : activityError || t("m1221")}</small></div>
        {activitySummary ? <><p className="overview-activity-scope">{tx("m1222", { value0: activityScopeLabel, value1: activitySummary.reported_actions })}</p><p className="overview-activity-boundary" role="status">{activityCoverageLabels.length ? `${activityCoverageLabels.join("；")}。` : t("m1223")}</p></> : <p role={activityError ? "alert" : "status"}>{activityError || (canRuntime ? t("m1224") : t("m1225"))}</p>}
        {activityScopes?.items.length ? <details className="overview-activity-sources"><summary>{tx("m1226")}</summary><label className="overview-activity-source">{t("m1227")}<select value={activitySourceChoice} onChange={event => setActivitySourceChoice(event.target.value)}><option value="__overview__">{tx("m1156")}</option><option value="__all__">{tx("m1155")}</option>{activityScopes.items.filter((item, index, all) => all.findIndex(other => activityChoiceKey(other) === activityChoiceKey(item)) === index).map(item => <option key={activityChoiceKey(item)} value={activityChoiceKey(item)}>{tx("m1228", { value0: item.source_label, value1: item.project_label || t("m1154", { value0: item.project_id }) })}</option>)}</select></label></details> : null}
        <button type="button" disabled={!activitySummary} onClick={() => { const params = new URLSearchParams({ from: activitySummary!.from, to: activitySummary!.to }); if (activitySourceIDs.length) params.set("source_ids", activitySourceIDs.join(",")); if (activityProjectIDs.length) params.set("project_ids", activityProjectIDs.join(",")); navigate(`/work/activity?${params}`); }}>{tx("m1229")}</button>
        <OverviewInfo label={t("m0156")}><p>{tx("m1231", { value0: activitySummary ? outcomeZeroLabels(activitySummary.resource_read_outcomes) : t("m1230"), value1: activitySummary ? outcomeZeroLabels(activitySummary.resource_change_outcomes) : t("m1230") })}</p>
          {activitySummary ? <><p>{tx("m1232", { value0: activitySummary.reported_linked_events, value1: activitySummary.reported_model_events, value2: activitySummary.reported_unlinked_events, value3: formatDateTime(activitySummary.cutoff) })}</p><p>{tx("m1234", { value0: activitySummary.coverage.length ? activitySummary.coverage.map(code => <code key={code}>{code} </code>) : t("m1233") })}</p></> : null}
          <Metric title={t("m1235")} value={currentContributions?.values.application?.authorized_group_count} note={t("m1236", { value0: currentContributions?.values.application?.unassigned_requests ?? t("m0193") })} unavailable={contributionMessage("application")} />
        </OverviewInfo>
      </article>
    </section>
    <section className="overview-observatory-row overview-observatory-row--comparison" aria-label={t("m1237")}>
      <article className="overview-panel overview-cost-card"><header><h2>{appliedCostDimension === "application" ? t("m1238") : t("m1239")}</h2><select aria-label={t("m1240")} value={appliedCostDimension} onChange={event => setCostDimension(event.target.value as typeof costDimension)}><option value="source_project">{tx("m1180")}</option><option value="application" disabled={!data.reading?.dimensions.includes("application")}>{tx("m1241")}</option></select></header>
        <CostComposition value={currentCostContributions?.values[appliedCostDimension]} unavailable={costContributionMessage} moneyTitle={displayMoneyTitle} onSelect={canDetails ? row => openWorkbench(appliedCostDimension, false, row.ref) : undefined} />
        <OverviewInfo label={t("m1242")}><p>{tx("m1244", { value0: displayMoneyTitle, value1: data.summary ? overviewMoney(request.money_basis === "calculated_reference" ? data.summary.gateway_charged_micros : data.summary.calculated_reference_micros) : t("m1243") })}</p></OverviewInfo>
      </article>
      <article className="overview-panel overview-model-card"><header><h2>{tx("m1245")}</h2></header><Contribution value={currentContributions?.values.model} unavailable={contributionMessage("model")} nested onSelect={canDetails ? row => openWorkbench("model", true, row.ref) : undefined} /><OverviewInfo label={t("m1246")}><p>{tx("m1247")}</p></OverviewInfo></article>
    </section>
    <section className={`overview-purpose-section${data.reading && !data.reading.dimensions.includes("purpose") ? " overview-purpose-section--unavailable" : ""}`} aria-label={t("m1248")}>
      <h2>{tx("m1248")}</h2>{data.reading && !data.reading.dimensions.includes("purpose") ? <p>{tx("m1249")}</p> : <div><Contribution value={currentContributions?.values.purpose} unavailable={contributionMessage("purpose")} nested onSelect={canDetails ? row => openWorkbench("purpose", true, row.ref) : undefined} /><small>{tx("m1250", { value0: data.summary?.purpose_known_requests ?? "—", value1: data.summary?.request_count ?? "—" })}</small></div>}<OverviewInfo label={t("m1248")}><p>{tx("m1251")}</p></OverviewInfo>
    </section>
    <section className="overview-business-section" aria-label={t("m1252")}>
      <details className="overview-recent-disclosure"><summary>{t("m1253")}<small>{tx("m1254")}</small></summary>
    <article className="overview-panel overview-recent"><header><h2>{tx("m1255")}</h2><small>{tx("m1256")}</small></header>
      <div role="tablist" aria-label={t("m1257")}><button type="button" role="tab" aria-selected={recentView === "runtime"} onClick={() => setRecentView("runtime")} disabled={!canRuntime}>{tx("m1258")}</button><button type="button" role="tab" aria-selected={recentView === "economic"} onClick={() => setRecentView("economic")}>{tx("m1259")}</button></div>
      {recentView === "runtime" ? <div role="tabpanel" aria-label={t("m1260")}>
        <details><summary>{tx("m1261")}</summary><p>{tx("m1262")}</p></details>
        {runtimeRecent?.items.length ? <div className="overview-table-scroll"><table><thead><tr><th scope="col">{tx("m1263")}</th><th scope="col">{tx("m1264")}</th><th scope="col">{tx("m1265")}</th><th scope="col">{tx("m0902")}</th></tr></thead><tbody>{runtimeRecent.items.map(row => <tr key={row.request_ref}>
          <td>{row.entry_time_state === "observed" && row.entry_at ? formatDateTime(row.entry_at) : t("m1266")}<small>{row.request_id}</small></td>
          <td>{row.source_instance}<small>{tx("m1267", { value0: row.org_id, value1: row.project_id })}</small></td>
          <td>{row.outcome_basis === "no_request_observation" || row.observation_count === 0 ? t("m1268") : row.outcome ? outcomes[row.outcome] ?? t("m1269") : t("m1269")}</td>
          <td><button type="button" onClick={() => navigate("/workbench/runtime", row.request_ref)}>{tx("m1270")}</button></td>
        </tr>)}</tbody></table></div> : runtimeRecent ? <p>{tx("m1271")}</p> : <p>{canRuntime ? runtimeRecentError || (runtimeRecentBusy ? t("m1272") : message("runtime", t("m1273"))) : t("m1190")}</p>}
        {runtimeRecentError && runtimeRecent ? <p role="alert">{runtimeRecentError}</p> : null}
        {runtimeRecent?.next_cursor ? <button type="button" disabled={runtimeRecentBusy} onClick={() => setRuntimeRecentCursor(runtimeRecent.next_cursor)}>{tx("m1274")}</button> : null}
        {runtimeRecent && data.runtimeReading ? <><small>{tx("m1275", { value0: formatDateTime(data.runtimeReading.cutoff) })}</small><details><summary>{tx("m1276")}</summary><small>{tx("m1277", { value0: data.runtimeReading.membership_ref })}</small></details></> : null}
      </div> : <div role="tabpanel" aria-label={t("m1278")}><p>{tx("m1279")}</p>
      {recent?.items.length ? <div className="overview-table-scroll"><table><thead><tr><th scope="col">{tx("m1263")}</th><th scope="col">{tx("m1280")}</th><th scope="col">{tx("m1248")}</th><th scope="col">{tx("m1281")}</th><th scope="col">{tx("m0495")}</th><th scope="col">{tx("m0902")}</th></tr></thead><tbody>{recent.items.map(row => <tr key={`${row.source_system_id}:${row.evidence_event_id}`}>
        <td>{formatDateTime(row.occurred_at)}<small>{row.request_id}</small></td>
        <td>{row.model || t("m1282")}<small>{tx("m1285", { value0: row.source_project || t("m0193"), value1: row.application_id ? t("m1283", { value0: row.application_id }) : t("m1284") })}</small></td>
        <td>{row.purpose_state === "adopted" ? purposeLabels[row.purpose_category || ""] ?? row.purpose_category : row.purpose_state === "unavailable" ? t("m1286") : t("m1287")}</td>
        <td>{row.runtime.match_state === "matched" ? outcomes[row.runtime.outcome || ""] ?? t("m1130") : row.runtime.match_state === "not_in_runtime_reading" ? t("m1288") : row.runtime.match_state === "ambiguous" ? t("m1289") : row.runtime.match_state === "no_request_observation" ? t("m1268") : t("m1290")}</td>
        <td>{overviewMoney(request.money_basis === "calculated_reference" ? row.calculated_reference_micros : row.gateway_charged_micros)}<small>{moneyTitle}</small></td>
        <td><button type="button" disabled={!permissions.includes("evidence.read") && !(permissions.includes("investigation.scoped.read") && row.investigation_available === true)} onClick={() => navigate(`/executions/${encodeURIComponent(row.execution_id)}?as_of=${encodeURIComponent(recent.cutoff)}&evidence=${encodeURIComponent(row.evidence_event_id)}`)}>{tx("m1291")}</button></td>
      </tr>)}</tbody></table></div> : recent ? <p>{tx("m1292")}</p> : <p>{canDetails ? recentError || data.errors.economic || (recentBusy ? t("m1293") : t("m1294")) : t("m1295")}</p>}
      {recentRuntimeUnavailable ? <p role="status">{tx("m1296")}</p> : null}{recentError ? <p role="alert">{recentError}</p> : null}{recent?.next_cursor ? <button type="button" disabled={recentBusy} onClick={() => setRecentCursor(recent.next_cursor)}>{tx("m1297")}</button> : null}
      {recent ? <small>{tx("m1298", { value0: formatDateTime(recent.cutoff), value1: formatDateTime(recent.runtime_cutoff) })}</small> : null}
      </div>}
    </article>
      </details>
    </section>
    <section className="overview-business-section" aria-label={t("m1299")}>
      <details className="overview-attention-disclosure"><summary>{t("m1300")}<small>{tx("m1301")}</small></summary>
      <article className="overview-panel"><header><h2>{tx("m1302")}</h2><button type="button" disabled={!canQueue} onClick={() => navigate("/investigation/queue")}>{tx("m1303")}</button></header>
        {data.runtime ? <p>{t("m1304")}{data.runtime.outcomes.filter(item => ["technical_failure", "interrupted", "output_held"].includes(item.outcome)).map(item => `${outcomes[item.outcome]} ${item.requests}`).join(" · ") || t("m1305")}。<button type="button" onClick={() => navigate("/workbench/runtime")}>{tx("m1306")}</button></p> : null}
        {data.queue ? <><p>{tx("m1307", { value0: formatDateTime(data.queue.evaluated_at) })}</p><ul className="overview-attention">{data.queue.items.slice(0, 2).map(item => <li key={item.finding.id}><button className="overview-attention-link" type="button" onClick={() => navigate(`/cases/${encodeURIComponent(item.finding.case_id)}`)}>{item.finding.title || t("m1308")}</button><span>{item.case_title} · {item.overdue ? t("m1309") : t("m1310")}</span></li>)}</ul>{!data.queue.items.length ? <p>{tx("m1311")}</p> : null}<details><summary>{tx("m1312")}</summary><CoverageNotes codes={data.queue.coverage.split(/[;,]\s*/)} /><small>{tx("m1313")}</small></details></> : <p>{canQueue ? message("queue") : t("m1314")}</p>}
      </article>
      </details>
    </section>
    <details className="overview-more"><summary>{tx("m1315")}</summary><article className="overview-panel"><header><h2>{tx("m1316")}</h2><button type="button" disabled={!canRuntime} onClick={() => navigate("/workbench/runtime")}>{tx("m1317")}</button></header>
      {data.runtime ? <><div className="overview-outcomes">{data.runtime.outcomes.map(item => <div key={item.outcome}><span>{outcomes[item.outcome] ?? item.outcome}</span><strong>{item.requests}</strong></div>)}</div><p>{tx("m1318", { value0: formatDateTime(data.runtimeReading?.cutoff), value1: data.runtimeReading?.requests_with_unknown_entry_time })}</p><CoverageNotes codes={data.runtime.coverage.gaps} /></> : <p>{canRuntime ? message("runtime") : t("m1319")}</p>}
    </article>
    <article className="overview-panel"><header><h2>{tx("m1320")}</h2></header>
      {data.sources ? <ul className="overview-sources">{data.sources.items.map(item => <li key={item.source_system_id}><strong>{item.display_name || t("m1321")}</strong><span>{tx("m1323", { value0: coverageNames[item.status] ?? t("m1322"), value1: formatDateTime(item.last_received_at ?? undefined) })}</span><small>{item.scope_label === "project_restricted" ? t("m1324") : t("m1325")}</small><CoverageNotes codes={item.gaps} />{!coverageNames[item.status] ? <details><summary>{tx("m1326")}</summary><code>{item.status}</code></details> : null}</li>)}</ul> : <p>{canSources ? message("sources") : t("m1327")}</p>}
      <p>{tx("m1329", { value0: data.reading ? t("m1328", { value0: coverageNames[data.reading.coverage.status], value1: formatDateTime(data.reading.cutoff) }) : message("economic") })}</p>
      {data.reading ? <details><summary>{tx("m1330")}</summary><dl><dt>{tx("m1331")}</dt><dd>{data.reading.reading_id}</dd><dt>{tx("m1332")}</dt><dd>{data.reading.authorization_revision}</dd><dt>{tx("m1333")}</dt><dd>{data.reading.coverage.gaps.join("、") || t("m1334")}</dd><dt>{tx("m1335")}</dt><dd>{data.reading.interpretation_revision}</dd></dl></details> : null}
    </article></details>
  </section>;
}
const coverageNotes: Record<string, string> = localizedLabels({}, {retained_investigation_records: "m1336", security_source_coverage_unconfirmed: "m1337", forward_only_capture: "m1338", timeline_completeness_unknown: "m1339", synchronous_requests_not_covered_by_lease_inventory: "m1340", source_fact_conflict: "m1341", period_completeness_unknown: "m1342", authorized: "m1343", source_period_completeness_not_asserted: "m1344", period_precedes_capture_start: "m1345", source_profile_capture_gaps_present: "m1346", source_not_currently_connected: "m1347", scope_limited_source_state_unknown: "m1348"});
function CoverageNotes({ codes }: { codes: string[] }) {
  useLocale();
  if (!codes.length) return <p>{tx("m1349")}</p>;
  const notes = [...new Set(codes.map(code => coverageNotes[code] ?? t("m1350")))];
  return <><p>{notes.join("；")}。</p><details><summary>{tx("m1351")}</summary><ul>{codes.map((code, index) => <li key={`${code}:${index}`}><code>{code}</code></li>)}</ul></details></>;
}
function makeRequest(hours: number, source: string, project: string, costProject: string, moneyBasis: WorkbenchReadingRequest["money_basis"] = "gateway_charged"): WorkbenchReadingRequest {
  const end = new Date();
  return { contract_version: "xinsight.workbench.v1", page: "cost", metric_version: "xinsight.workbench.cost.v1", period: { start_inclusive: new Date(end.getTime() - hours * 3600000).toISOString(), end_exclusive: end.toISOString(), display_timezone: Intl.DateTimeFormat().resolvedOptions().timeZone }, money_basis: moneyBasis, currency: "CNY", filters: { source_ids: parts(source), source_projects: parts(project), cost_projects: parts(costProject) } };
}
function Metric({ title, value, note, unavailable }: { title: string; value?: string | number; note: string; unavailable: string }) {
  useLocale();
  const detail = value === undefined ? unavailable : note;
  return <article className="overview-metric"><span>{title}</span><strong>{value ?? "—"}</strong>{detail ? <small>{detail}</small> : null}</article>;
}
function OverviewInfo({ label, children }: { label: string; children: ReactNode }) {
  useLocale();
  return <details className="overview-help"><summary aria-label={t("m1353", { value0: label })}>{tx("m1352")}</summary><div className="overview-help-content">{children}</div></details>;
}
function CostComposition({ value, unavailable, moneyTitle, onSelect }: { value?: WorkbenchContributions; unavailable: string; moneyTitle: string; onSelect?: (row: WorkbenchContribution) => void }) {
  useLocale();
  const [focusedKey, setFocusedKey] = useState<string>();
  const [hoveredKey, setHoveredKey] = useState<string>();
  if (!value) return <p role="status">{unavailable}</p>;
  const items = [
    ...value.rows.map(row => ({ key: row.ref, label: row.label || t("m1354"), amount: row.selected_amount_micros, row })),
    { key: "__other__", label: t("m1355"), amount: value.other_known_micros, row: undefined },
    { key: "__unassigned__", label: t("m1356"), amount: value.unassigned_micros, row: undefined },
  ];
  let total: bigint | undefined;
  let values: bigint[] = [];
  try { total = BigInt(value.denominator_micros); values = items.map(item => BigInt(item.amount)); } catch { /* Keep exact strings in the list. */ }
  const totalAmount = total;
  const positive = totalAmount !== undefined && totalAmount > 0n && values.length === items.length && values.every(amount => amount >= 0n);
  const reconciled = positive && values.reduce((sum, amount) => sum + amount, 0n) === totalAmount;
  const reason = totalAmount === undefined || values.length !== items.length ? t("m1357") : totalAmount <= 0n ? t("m1358") : values.some(amount => amount < 0n) ? t("m1359") : !reconciled ? t("m1360") : "";
  let offset = 0;
  const active = items.find(item => item.key === focusedKey) ?? items.find(item => item.key === hoveredKey);
  const percent = (amount: bigint) => totalAmount && totalAmount > 0n ? Number(amount * 1000n / totalAmount) / 10 : 0;
  const percentLabel = (amount: bigint) => amount > 0n && percent(amount) === 0 ? "<0.1%" : `${percent(amount)}%`;
  const tinyAmount = reconciled && values.some(amount => amount > 0n && percent(amount) === 0);
  return <div className="overview-cost-composition"><p className="overview-cost-total"><span>{tx("m1361", { value0: moneyTitle })}</span><strong>{overviewMoney(value.denominator_micros)}</strong></p>
    <div className="overview-cost-body">{reconciled ? <div className="overview-donut-wrap"><svg className="overview-donut" viewBox="0 0 120 120" role="group" aria-label={t("m1362", { value0: value.dimension === "application" ? t("m1241") : t("m1180") })}>
      <circle className="overview-donut-base" cx="60" cy="60" r="43" fill="none" strokeWidth="16" />
      {items.map((item, index) => { const amount = values[index]; const width = Number(amount * 1000000n / totalAmount!) / 10000; const start = offset; offset += width; return amount > 0n ? <circle key={item.key} className={`overview-donut-arc overview-donut-arc--${item.row ? index % 5 : item.key === "__other__" ? "other" : "unassigned"}`} cx="60" cy="60" r="43" fill="none" strokeWidth="16" pathLength={100} strokeDasharray={`${width} ${100 - width}`} strokeDashoffset={-start} tabIndex={0} role="img" aria-label={t("m1363", { value0: item.label, value1: overviewMoney(item.amount), value2: percentLabel(amount) })} onFocus={() => setFocusedKey(item.key)} onBlur={() => setFocusedKey(undefined)} onMouseEnter={() => setHoveredKey(item.key)} onMouseLeave={() => setHoveredKey(current => current === item.key ? undefined : current)}><title>{item.label} · {overviewMoney(item.amount)} · {percentLabel(amount)}</title></circle> : null; })}
      <text className="overview-donut-center" x="60" y="61" textAnchor="middle">{tx("m1242")}</text>
    </svg><output className="overview-donut-readout" aria-live="polite">{active ? `${active.label} · ${overviewMoney(active.amount)} · ${percentLabel(BigInt(active.amount))}` : t("m1364")}</output></div> : <p role="status" className="overview-donut-unavailable">{tx("m1365", { value0: reason })}</p>}
      <ul className="overview-cost-list">{items.map((item, index) => <li key={item.key} onMouseEnter={() => setHoveredKey(item.key)} onMouseLeave={() => setHoveredKey(current => current === item.key ? undefined : current)}><span className={`overview-cost-key overview-cost-key--${item.row ? index % 5 : item.key === "__other__" ? "other" : "unassigned"}`} aria-hidden="true" />
        <span className="overview-cost-name">{item.row && onSelect && item.row.drilldown_allowed ? <button type="button" onClick={() => onSelect(item.row!)} onFocus={() => setFocusedKey(item.key)} onBlur={() => setFocusedKey(undefined)} aria-label={t("m1366", { value0: item.label })}>{item.label}</button> : item.label}</span>
        <strong>{overviewMoney(item.amount)}</strong><small>{reconciled ? percentLabel(values[index]) : t("m1367")}</small>
      </li>)}</ul></div>
    {tinyAmount ? <small>{tx("m1368")}</small> : null}
  </div>;
}
function resourceOutcomeParts(outcomes?: NonNullable<WorkActivitySummary["resource_read_outcomes"]>) {
  return [
    { key: "completed", label: t("m1369"), shortLabel: t("m1370"), value: outcomes?.completed ?? 0 },
    { key: "failed", label: t("m1088"), shortLabel: t("m1088"), value: outcomes?.failed ?? 0 },
    { key: "optional", label: t("m1371"), shortLabel: t("m1372"), value: outcomes?.optional_not_found ?? 0 },
    { key: "unknown", label: t("m0098"), shortLabel: t("m0098"), value: outcomes?.unknown ?? 0 },
  ];
}
function outcomeZeroLabels(outcomes?: NonNullable<WorkActivitySummary["resource_read_outcomes"]>) {
  if (!outcomes) return t("m1373");
  return resourceOutcomeParts(outcomes).filter(part => part.value === 0).map(part => t("m1374", { value0: part.shortLabel })).join("、") || t("m1375");
}
function ResourceOutcomeRow({ title, total, outcomes }: { title: string; total: number; outcomes?: NonNullable<WorkActivitySummary["resource_read_outcomes"]> }) {
  useLocale();
  const parts = resourceOutcomeParts(outcomes);
  const sum = parts.reduce((count, part) => count + part.value, 0);
  return <div className="overview-resource-row" role="group" aria-label={t("m1376", { value0: title, value1: total, value2: outcomes ? parts.map(part => `${part.label} ${part.value}`).join("，") : t("m1373") })}><div className="overview-resource-head"><span>{title}</span><div><strong>{total}</strong><span>{tx("m1377")}</span></div></div>
    {!outcomes ? <p role="status">{tx("m1378")}</p> : <><div className="overview-resource-counts">
      {parts.filter(part => part.value > 0).map(part => <span key={part.key} className={`overview-resource-${part.key}`}><i aria-hidden="true" />{part.shortLabel}<strong>{part.value}</strong></span>)}</div>
      {sum !== total ? <p role="status">{tx("m1379", { value0: sum, value1: total })}</p> : null}</>}
  </div>;
}
function Contribution({ value, unavailable, nested, onSelect }: { value?: WorkbenchContributions; unavailable: string; nested?: boolean; onSelect?: (row: WorkbenchContribution) => void }) {
  useLocale();
  // Do not infer the complete request denominator from the visible Top-N rows.
  const requestsAvailable = value?.order_by === "requests" && value.denominator_requests !== undefined && value.unassigned_requests !== undefined && value.other_known_requests !== undefined;
  const requestsReconciled = requestsAvailable && [value.denominator_requests!, value.other_known_requests!, value.unassigned_requests!, ...value.rows.map(row => row.request_count)].every(count => Number.isSafeInteger(count) && count >= 0) && value.rows.reduce((total, row) => total + row.request_count, value.other_known_requests! + value.unassigned_requests!) === value.denominator_requests;
  const maxRequests = requestsReconciled ? Math.max(0, ...value!.rows.map(row => row.request_count)) : 0;
  const content = value && requestsAvailable ? <>
    <div className="overview-contribution-scale"><span>0</span><span>{requestsReconciled ? t("m1380", { value0: maxRequests }) : t("m1381")}</span></div>
    {!requestsReconciled ? <p role="status">{tx("m1382")}</p> : null}
    <ul className="overview-contributions">{value.rows.map(row => {
      const label = value.dimension === "purpose" ? purposeLabels[row.label] ?? row.label : row.label || t("m1354");
      const share = requestsReconciled ? overviewShare(String(row.request_count), String(value.denominator_requests)) : t("m1367");
      const width = requestsReconciled ? shareWidth(String(row.request_count), String(maxRequests)) : undefined;
      return <li key={row.ref}><div className="overview-contribution-line">
        {onSelect && row.drilldown_allowed ? <button type="button" onClick={() => onSelect(row)} aria-label={t("m1366", { value0: label })}>{label}</button> : <span title={label}>{label}</span>}
        <span className="overview-contribution-values"><strong>{tx("m1383", { value0: row.request_count })}</strong><small>{share}</small><small>{overviewMoney(row.selected_amount_micros)}</small>{!row.drilldown_allowed ? <small>{tx("m1384")}</small> : null}</span>
      </div><span className="overview-contribution-bar" aria-hidden="true">{width ? <i style={{ width }} /> : null}</span></li>;
    })}</ul>
    <div className="overview-contribution-footer"><span>{tx("m1385", { value0: value.denominator_requests, value1: value.other_known_requests, value2: value.unassigned_requests })}</span></div>
  </> : <p>{value ? t("m1386") : unavailable}</p>;
  return nested ? <div>{content}</div> : <article className="overview-panel">{content}</article>;
}
function Trend({ series, moneyTitle, onOpen }: { series: WorkbenchSeries; moneyTitle: string; onOpen: (activeBucket: string) => void }) {
  useLocale();
  const [focusedIndex, setFocusedIndex] = useState<number>();
  const [hoveredIndex, setHoveredIndex] = useState<number>();
  const points = series.points.map((point, index) => {
    const start = Date.parse(point.start_inclusive);
    const end = Date.parse(point.end_exclusive);
    const validTime = Number.isFinite(start) && Number.isFinite(end) && end > start;
    const validValue = /^-?\d+$/.test(point.known_value) && (series.unit === "micros" || !point.known_value.startsWith("-"));
    const value = validValue ? BigInt(point.known_value) : undefined;
    return { ...point, index, start, end, validTime, value, visible: validTime && value !== undefined };
  });
  const dated = points.filter(point => point.validTime);
  const visible = points.filter(point => point.visible && point.value !== undefined);
  const firstTime = dated.length ? Math.min(...dated.map(point => point.start)) : 0;
  const lastTime = dated.length ? Math.max(...dated.map(point => point.end)) : 0;
  const observedValues = visible.map(point => point.value!);
  const minimum = observedValues.reduce((min, value) => value < min ? value : min, 0n);
  const maximum = observedValues.reduce((max, value) => value > max ? value : max, 0n);
  const domain = maximum - minimum;
  const yFor = (value: bigint) => domain > 0n ? Number((maximum - value) * 100000n / domain) * 140 / 100000 : 140;
  const xFor = (start: number, end: number) => lastTime > firstTime ? Math.max(0, Math.min(100, ((start + end) / 2 - firstTime) * 100 / (lastTime - firstTime))) : 50;
  const plotted = visible.map(point => ({ ...point, x: xFor(point.start, point.end), y: yFor(point.value!) }));
  const byIndex = new Map(plotted.map(point => [point.index, point]));
  const segments = points.slice(1).flatMap((point, offset) => {
    const previous = points[offset]; const from = byIndex.get(previous.index); const to = byIndex.get(point.index);
    return previous.end === point.start && from && to ? [{ from, to }] : [];
  });
  const active = plotted.find(point => point.index === focusedIndex) ?? plotted.find(point => point.index === hoveredIndex);
  const labelFor = (point: WorkbenchSeries["points"][number]) => t("m1390", { value0: formatDateTime(point.start_inclusive), value1: formatDateTime(point.end_exclusive), value2: series.unit === "micros" ? `${moneyTitle} ${overviewMoney(point.known_value)}` : t("m1387", { value0: point.known_value }), value3: point.complete ? t("m1388") : t("m1389") });
  const axisLabel = (value: bigint) => series.unit === "micros" ? overviewMoney(String(value)) : String(value);
  const shortTime = (value: number) => new Intl.DateTimeFormat(getLocale(), { month: "numeric", day: "numeric", ...(lastTime - firstTime <= 172800000 ? { hour: "2-digit" as const, minute: "2-digit" as const } : {}), hour12: false }).format(value);
  const invalidCount = points.filter(point => !point.validTime || point.value === undefined).length;
  const incomplete = points.some(point => !point.complete);
  const coverageLabel = coverageNames[series.coverage.status] ?? t("m1196");
  const caption = [incomplete && !coverageLabel.includes(t("m1391")) ? t("m1392", { value0: coverageLabel }) : coverageLabel,
    plotted.length <= 1 ? plotted.length ? t("m1393") : t("m1394") : segments.length === 0 ? t("m1395") : undefined,
    incomplete && plotted.some(point => point.value === 0n) ? t("m1396") : undefined,
    invalidCount ? t("m1397", { value0: invalidCount }) : undefined].filter(Boolean).join(" · ");
  return <>{dated.length ? <><div className="overview-trend-frame"><div className="overview-trend-axis" aria-hidden="true"><span>{domain > 0n ? axisLabel(maximum) : ""}</span><span>{axisLabel(minimum)}</span></div><div className="overview-trend-plot"><div className="overview-trend" role="group" aria-label={t("m1400", { value0: series.metric === "requests" ? t("m1398") : t("m1399") })}>
    <svg className="overview-trend-lines" viewBox="0 0 1000 140" preserveAspectRatio="none" aria-hidden="true"><line className="overview-trend-zero" x1="0" x2="1000" y1={yFor(0n)} y2={yFor(0n)} />{segments.map(({ from, to }) => <line key={`${from.index}:${to.index}`} className="overview-trend-segment" data-from={from.index} data-to={to.index} x1={from.x * 10} y1={from.y} x2={to.x * 10} y2={to.y} />)}</svg>
    {plotted.map(point => <button key={`${point.start_inclusive}:${point.index}`} className={`overview-trend-point${point.complete ? "" : " overview-trend-point--partial"}`} type="button" style={{ left: `${point.x}%`, top: `${point.y + 12}px` }} aria-label={labelFor(point)} title={labelFor(point)} onFocus={() => setFocusedIndex(point.index)} onBlur={() => setFocusedIndex(undefined)} onMouseEnter={() => setHoveredIndex(point.index)} onMouseLeave={() => setHoveredIndex(current => current === point.index ? undefined : current)}><span aria-hidden="true" /></button>)}
  </div><output className="overview-trend-tooltip" aria-live="polite">{active ? labelFor(active) : t("m1401")}</output></div></div><div className="overview-trend-range"><span>{shortTime(firstTime)}</span><span>{shortTime(lastTime)}</span></div></> : <p role="status">{tx("m1402")}</p>}
    <p className="overview-chart-caption" role="status">{caption}</p>
    <button type="button" onClick={() => onOpen(active ? labelFor(active) : "")}>{tx("m1403", { value0: series.metric === "requests" ? t("m1211") : t("m0495") })}</button></>;
}
