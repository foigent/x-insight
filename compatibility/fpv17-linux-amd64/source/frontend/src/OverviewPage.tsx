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
const outcomes: Record<string, string> = { local_delivery_completed: "本地交付完成", technical_failure: "技术失败", admission_rejected: "准入拒绝", output_held: "输出留置", interrupted: "交付中断", context_done_unresolved: "上下文结束，结果未决", unresolved: "结果未决", conflicting: "证据冲突", missing_predecessor: "前驱缺失" };
const coverageNames: Record<string, string> = { observed: "已观察到", observed_with_gaps: "已有资料，但有缺口", scope_limited_unknown: "仅当前授权范围，完整性未知", disconnected: "来源已断开", not_connected: "尚未接入", period_completeness_unknown: "期间完整性未知" };
export function overviewMoney(value: string) {
  try { const n = BigInt(value); const digits = (n < 0n ? -n : n).toString().padStart(7, "0"); return `${n < 0n ? "-" : ""}¥${digits.slice(0, -6)}.${digits.slice(-6)}`; }
  catch { return "不可计算"; }
}
export function overviewShare(value: string, total: string) {
  try { const denominator = BigInt(total); return denominator === 0n ? "不可计算" : `${Number(BigInt(value) * 1000n / denominator) / 10}%`; }
  catch { return "不可计算"; }
}
function shareWidth(value: string, total: string) {
  try { const denominator = BigInt(total); return denominator > 0n ? `${Math.min(100, Math.max(0, Number(BigInt(value) * 100n / denominator)))}%` : undefined; }
  catch { return undefined; }
}
function failureMessage(error: unknown) {
  if (error instanceof ApiRequestError) {
    if (error.status === 403) return "当前账户无权读取此部分。";
    if ([409, 410].includes(error.status)) return "读取依据已变更或过期，请重新读取。";
  }
  return "此部分暂不可用，请稍后重新读取。";
}
export function OverviewPage({ session, navigate, context, onContext }: Props) {
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
  const [recentError, setRecentError] = useState("");
  const [recentRuntimeUnavailable, setRecentRuntimeUnavailable] = useState(false);
  const [recentView, setRecentView] = useState<"runtime" | "economic">(session.session.permissions.includes("workbench.details.read") ? "runtime" : "economic");
  const [runtimeRecent, setRuntimeRecent] = useState<Runtime.Page<Runtime.Request>>();
  const [runtimeRecentCursor, setRuntimeRecentCursor] = useState<string>();
  const [runtimeRecentBusy, setRuntimeRecentBusy] = useState(false);
  const [runtimeRecentError, setRuntimeRecentError] = useState("");
  const [activitySummary, setActivitySummary] = useState<WorkActivitySummary>();
  const [activityError, setActivityError] = useState("");
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
    if (!canRuntime) { setActivitySummary(undefined); setActivityError(""); return; }
    const controller = new AbortController();
    setActivitySummary(undefined); setActivityError("");
    if (activitySourceChoice === "__overview__" && request.filters.cost_projects.length) {
      setActivityError("工作活动尚不能按费用项目准确归属；当前费用项目筛选下不显示活动汇总。");
      return () => controller.abort();
    }
    const projects = activityProjectIDs;
    if (projects.some(value => !/^[1-9][0-9]{0,18}$/.test(value))) {
      setActivityError("当前来源项目条件无法用于工作活动汇总，请调整筛选。");
      return () => controller.abort();
    }
    void workActivityAPI.summary({ from: request.period.start_inclusive ?? "", to: request.period.end_exclusive ?? "",
      source_ids: activitySourceIDs, project_ids: projects }, controller.signal)
      .then(value => { if (!controller.signal.aborted) setActivitySummary(value); })
      .catch(reason => { if (!controller.signal.aborted && !isAbortError(reason)) setActivityError(failureMessage(reason)); });
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
    const invalidate = () => { controller.abort(); setData(empty()); setBusy(false); setInvalid("登录或授权范围已变更，旧资料已移除，请重新读取。"); };
    const invalidateReading = (key: "economic" | "runtime", reason: string) => {
      expired.add(key);
      update(previous => key === "economic"
        ? { ...previous, reading: undefined, summary: undefined, requests: undefined, amount: undefined, sources: undefined, errors: { ...previous.errors, economic: reason } }
        : { ...previous, runtime: undefined, runtimeReading: undefined, errors: { ...previous.errors, runtime: reason } });
    };
    invalidateEconomic.current = reason => invalidateReading("economic", reason);
    const expire = (key: "economic" | "runtime", reading: { cutoff: string; expires_at: string }) => {
      const remaining = Date.parse(reading.expires_at) - Date.now();
      const reason = key === "economic" ? "本次费用与使用读取已过期，请重新读取。" : "运行读取已过期，请重新读取。";
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
        if (readingInvalid && keyGroup !== "queue") invalidateReading(keyGroup, "本次读取已失效，已移除相关资料，请重新读取。");
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
          invalidateEconomic.current("本次读取已失效，已移除相关资料，请重新读取。");
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
          invalidate(); invalidateEconomic.current("本次读取已失效，已移除相关资料，请重新读取。");
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
    setRecentBusy(true); setRecentError("");
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
      setRecentError(failureMessage(error));
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
    setRuntimeRecentBusy(true); setRuntimeRecentError("");
    void runtimeAPI.requests(reading.reading_id, "all", runtimeRecentCursor, controller.signal).then(page => {
      if (controller.signal.aborted) return;
      if (page.reading_id !== reading.reading_id) throw new Error("Reading mismatch");
      setRuntimeRecent(previous => runtimeRecentCursor && previous?.reading_id === page.reading_id ? { ...page, items: [...previous.items, ...page.items] } : page);
    }).catch(error => {
      if (controller.signal.aborted || isAbortError(error)) return;
      setRuntimeRecentError(failureMessage(error));
      if (error instanceof ApiRequestError && [403, 409, 410].includes(error.status)) setRuntimeRecent(undefined);
    }).finally(() => { if (!controller.signal.aborted) setRuntimeRecentBusy(false); });
    return () => controller.abort();
  }, [data.runtimeReading, canDetails, runtimeRecentCursor, identity]);
  function apply(event: FormEvent) { event.preventDefault(); setRecent(undefined); setRecentCursor(undefined); setRuntimeRecent(undefined); setRuntimeRecentCursor(undefined); setRequest(makeRequest(hours, source, project, costProject, moneyBasis)); setRevision(value => value + 1); }
  function clearFilters() { setSource(""); setProject(""); setCostProject(""); setRecent(undefined); setRecentCursor(undefined); setRuntimeRecent(undefined); setRuntimeRecentCursor(undefined); setRequest(makeRequest(hours, "", "", "", moneyBasis)); setRevision(value => value + 1); }
  const message = (key: string, fallback = "尚未读取") => data.errors.economic && !["runtime", "queue"].includes(key) ? data.errors.economic : data.errors[key] ?? (busy ? "正在读取…" : fallback);
  const currentContributions = contributions?.readingID === data.reading?.reading_id && contributions?.order === contributionMetric ? contributions : undefined;
  const currentCostContributions = costContributions?.readingID === data.reading?.reading_id ? costContributions : undefined;
  const appliedCostDimension = data.reading?.dimensions.includes(costDimension) ? costDimension : "source_project";
  const contributionMessage = (dimension: Dimension) => data.errors.economic ?? currentContributions?.errors[dimension] ?? (data.reading?.dimensions.includes(dimension) ? "正在读取…" : message(dimension, "当前读取不支持此维度"));
  const moneyTitle = request.money_basis === "calculated_reference" ? "计算参考费用" : "网关已计费用";
  const displayMoneyTitle = data.reading?.money_title ?? moneyTitle;
  const costContributionMessage = data.errors.economic ?? currentCostContributions?.errors[appliedCostDimension] ?? (data.reading?.dimensions.includes(appliedCostDimension) ? "正在读取…" : message(appliedCostDimension, "当前读取不支持此维度"));
  const localCompleted = data.runtime?.outcomes.find(item => item.outcome === "local_delivery_completed")?.requests;
  const resultAttention = data.runtime?.outcomes.filter(item => ["technical_failure", "interrupted", "output_held"].includes(item.outcome)).reduce((sum, item) => sum + item.requests, 0);
  const series = data[metric];
  const sourceChoices = data.sources?.items ?? [];
  const selectedSource = !source ? "" : sourceChoices.some(item => item.source_system_id === source) ? source : "__precise__";
  const appliedFilters = request.filters;
  const activeFilterCount = appliedFilters.source_ids.length + appliedFilters.source_projects.length + appliedFilters.cost_projects.length;
  const appliedSourceNames = appliedFilters.source_ids.map(id => sourceChoices.find(item => item.source_system_id === id)?.display_name ?? id);
  const activityScopeLabel = chosenActivitySource ? `${chosenActivitySource.source_label} · ${chosenActivitySource.project_label || `项目 ${chosenActivitySource.project_id}`}` : activitySourceChoice === "__all__" ? "全部已授权工作来源" : "沿用总览来源筛选";
  const activityCoverageLabels = activitySummary ? [...new Set(activitySummary.coverage.map(code => coverageNotes[code] ?? "另有来源报告的范围限制"))] : [];
  const openWorkbench = (dimension: WorkbenchNavigation["dimension"], usage: boolean, selectedRef?: string, activeBucket = "") => {
    if (!data.reading?.dimensions.includes(dimension)) return;
    const selection: WorkbenchNavigation = { version: 1, owner: navigationOwner(session), readingID: data.reading.reading_id,
      request: fixedReadingRequest(data.reading, request), dimension, mode: "top5", resolution: "overview", activeBucket,
      selectedRef, contributionPages: 1, detailPages: 1 };
    navigate(usage ? "/workbench/cost?view=usage" : "/workbench/cost", undefined, undefined, selection);
  };
  return <section className="overview-page overview-page--home" aria-label="组合总览">
    <div className="overview-top"><header className="overview-heading"><span className="eyebrow">工作台 · 当前授权范围</span><h1>总览</h1><p>使用、运行、费用与需要跟进的事</p></header>
    <form className="overview-filters" onSubmit={apply}>
      <div className="overview-filter-controls"><label>期间<select value={hours} onChange={event => setHours(Number(event.target.value))}><option value={24}>最近24小时</option><option value={168}>最近7天</option><option value={720}>最近30天</option></select></label>
      {canSources ? <label className="overview-filter-source">来源<select aria-label="来源" value={selectedSource} disabled={!data.sources} onChange={event => setSource(event.target.value)}><option value="">全部已授权来源</option>{selectedSource === "__precise__" ? <option value="__precise__" disabled>已设精确来源条件</option> : null}{sourceChoices.map(item => <option key={item.source_system_id} value={item.source_system_id}>来源 · {item.display_name}</option>)}</select></label> : null}
      <label>费用口径<select value={moneyBasis} onChange={event => setMoneyBasis(event.target.value as typeof moneyBasis)}><option value="calculated_reference">计算参考</option><option value="gateway_charged">网关已计</option></select></label>
      <button type="submit">应用并读取</button></div>
      <div className="overview-filter-meta"><div className="overview-active-filters"><span>生效范围：{activeFilterCount ? `来源 ${appliedSourceNames.join("、") || "全部已授权"}；来源项目 ${appliedFilters.source_projects.join("、") || "全部"}；费用项目 ${appliedFilters.cost_projects.join("、") || "全部"}` : "全部已授权来源与项目"} · {formatDateTime(request.period.start_inclusive)} — {formatDateTime(request.period.end_exclusive)} · 金额口径：{moneyTitle}</span>{activeFilterCount ? <button type="button" onClick={clearFilters}>清除项目与来源筛选</button> : null}</div>
      <details className="overview-advanced" open={advancedOpen} onToggle={event => setAdvancedOpen(event.currentTarget.open)}><summary>高级筛选</summary><div className="overview-advanced-content">
        <label>来源筛选<input value={source} onChange={event => setSource(event.target.value)} placeholder="来源标识，逗号分隔" /></label>
        <label>来源项目<input value={project} onChange={event => setProject(event.target.value)} placeholder="项目标识" /></label>
        <label>费用项目<input value={costProject} onChange={event => setCostProject(event.target.value)} placeholder="项目标识" /></label>
        <p className="overview-period">当前期间：{formatDateTime(request.period.start_inclusive)} — {formatDateTime(request.period.end_exclusive)}。专题沿用此起止时间，重新读取时显示各自截止。</p>
      </div></details></div>
    </form></div>
    {invalid ? <p role="alert">{invalid}</p> : null}
    <div className="overview-quick-scan" aria-label="本次跨业务摘要"><div className="overview-stats">
      <Metric title="已记录模型请求" value={data.summary?.request_count} note={data.summary?.request_total_complete ? "总数已确认" : "总数未确认完整"} unavailable={message("summary")} />
      <Metric title={displayMoneyTitle} value={data.summary ? overviewMoney(data.summary.selected_amount_micros) : undefined} note="" unavailable={message("summary")} />
      <Metric title="运行本地交付完成" value={data.runtime ? `${localCompleted ?? 0} / ${data.runtime.requests}` : undefined} note={data.runtime ? `另有 ${resultAttention ?? 0} 条需核对` : ""} unavailable={canRuntime ? message("runtime") : "无运行详情读取权限"} />
      <Metric title="当前复核待办" value={data.queue?.total} note="当前队列" unavailable={canQueue ? message("queue") : "无复核读取权限"} />
    </div><OverviewInfo label="摘要"><p>“已记录模型请求”只统计当前费用资料中已知的模型请求，不等于所有运行尝试；计算参考费用用于比较，不代表实际扣费或最终结算。运行本地交付完成也不是业务成功数。复核待办是当前队列，不随上方期间和项目筛选。</p></OverviewInfo></div>
    <p className="overview-coverage-note" role="status">{data.reading ? `费用资料：${coverageNames[data.reading.coverage.status] ?? "覆盖状态待确认"}；${data.reading.coverage.period_completeness === "unknown" ? "所选期间完整性未知" : "所选期间覆盖状态待核对"}；${data.summary?.request_total_complete ? "费用请求总数已确认" : "费用请求总数未确认完整"}。` : `费用资料：${message("economic")}。`}
      {canSources ? data.sources?.items.some(item => item.status === "disconnected" || item.status === "not_connected") ? " 有来源断开或未接入。" : data.sources ? " 来源覆盖以当前授权范围为准。" : " 来源覆盖暂不可用。" : " 当前账户不能读取来源覆盖。"}</p>
    <section className="overview-observatory-row" aria-label="趋势与工作活动">
      <article className="overview-panel overview-trend-panel"><header><h2>{metric === "requests" ? "模型请求趋势" : `${displayMoneyTitle}趋势`}</h2><select aria-label="趋势指标" value={metric} onChange={e => setMetric(e.target.value as typeof metric)}><option value="requests">请求</option><option value="amount">费用</option></select></header>
        <OverviewInfo label="趋势"><p>每个点表示所示时间段内已知的请求数或{displayMoneyTitle}。完整时段的真实零可以显示；完整性未知且值为零不当作真实零。只连接相邻且完整的时段；聚焦点只显示准确时间和值，进入专题仍查看整个当前范围。{request.money_basis === "calculated_reference" ? "计算参考费用可能有冲回，不代表实际扣费。" : "网关已计费用可能有冲回，不代表最终结算。"}</p></OverviewInfo>
        {series ? <Trend key={`${series.reading_id}:${series.metric}`} series={series} moneyTitle={displayMoneyTitle} onOpen={bucket => openWorkbench("source_project", metric === "requests", undefined, bucket)} /> : <p role="status">{message(metric)}</p>}
      </article>
      <article className="overview-panel overview-activity-entry"><header><h2>工作活动与数据</h2><small>来源报告的操作结果</small></header>
        {activitySummary ? <div className="overview-resource-summaries"><ResourceOutcomeRow title="资源读取" total={activitySummary.reported_resource_reads} outcomes={activitySummary.resource_read_outcomes} /><ResourceOutcomeRow title="资源写入或修改" total={activitySummary.reported_resource_changes} outcomes={activitySummary.resource_change_outcomes} /></div> : null}
        <div className="overview-activity-attention"><span>资料需要关注</span><strong>{activitySummary?.sources_needing_attention ?? "—"}</strong><small>{activitySummary ? "当前可见范围" : activityError || "活动资料尚未读取"}</small></div>
        {activitySummary ? <><p className="overview-activity-scope">当前活动范围：{activityScopeLabel} · 已接收 {activitySummary.reported_actions} 条来源活动。</p><p className="overview-activity-boundary" role="status">{activityCoverageLabels.length ? `${activityCoverageLabels.join("；")}。` : "未报告具体缺口；完整性未知。"}</p></> : <p role={activityError ? "alert" : "status"}>{activityError || (canRuntime ? "正在读取工作活动…" : "当前账户无工作活动读取权限。")}</p>}
        {activityScopes?.items.length ? <details className="overview-activity-sources"><summary>选择活动来源</summary><label className="overview-activity-source">工作活动来源 <select value={activitySourceChoice} onChange={event => setActivitySourceChoice(event.target.value)}><option value="__overview__">沿用总览来源筛选</option><option value="__all__">全部已授权工作来源</option>{activityScopes.items.filter((item, index, all) => all.findIndex(other => activityChoiceKey(other) === activityChoiceKey(item)) === index).map(item => <option key={activityChoiceKey(item)} value={activityChoiceKey(item)}>{item.source_label} · {item.project_label || `项目 ${item.project_id}`}（全部已授权实例）</option>)}</select></label></details> : null}
        <button type="button" disabled={!activitySummary} onClick={() => { const params = new URLSearchParams({ from: activitySummary!.from, to: activitySummary!.to }); if (activitySourceIDs.length) params.set("source_ids", activitySourceIDs.join(",")); if (activityProjectIDs.length) params.set("project_ids", activityProjectIDs.join(",")); navigate(`/work/activity?${params}`); }}>查看该范围全部活动</button>
        <OverviewInfo label="工作活动"><p>这里统计来源报告的资源操作尝试次数，不是任务数或文件数；“完成”也不表示已独立核验文件改变。仅显示非零结果，零分类如下：读取 {activitySummary ? outcomeZeroLabels(activitySummary.resource_read_outcomes) : "待读取"}；写入或修改 {activitySummary ? outcomeZeroLabels(activitySummary.resource_change_outcomes) : "待读取"}。费用项目筛选不适用于工作活动。</p>
          {activitySummary ? <><p>模型活动中已有来源关系 {activitySummary.reported_linked_events} / {activitySummary.reported_model_events} 条；另有 {activitySummary.reported_unlinked_events} 条未报告关系。关系不证明模型引起工具操作。截止 {formatDateTime(activitySummary.cutoff)}。</p><p>来源范围代码：{activitySummary.coverage.length ? activitySummary.coverage.map(code => <code key={code}>{code} </code>) : "未报告"}</p></> : null}
          <Metric title="已记录应用组" value={currentContributions?.values.application?.authorized_group_count} note={`经济资料中的应用分组；未归属请求 ${currentContributions?.values.application?.unassigned_requests ?? "未知"}`} unavailable={contributionMessage("application")} />
        </OverviewInfo>
      </article>
    </section>
    <section className="overview-observatory-row overview-observatory-row--comparison" aria-label="项目费用与模型请求">
      <article className="overview-panel overview-cost-card"><header><h2>{appliedCostDimension === "application" ? "应用费用构成" : "项目费用构成"}</h2><select aria-label="费用贡献维度" value={appliedCostDimension} onChange={event => setCostDimension(event.target.value as typeof costDimension)}><option value="source_project">来源项目</option><option value="application" disabled={!data.reading?.dimensions.includes("application")}>应用</option></select></header>
        <CostComposition value={currentCostContributions?.values[appliedCostDimension]} unavailable={costContributionMessage} moneyTitle={displayMoneyTitle} onSelect={canDetails ? row => openWorkbench(appliedCostDimension, false, row.ref) : undefined} />
        <OverviewInfo label="费用构成"><p>只统计有费用资料的请求，按当前选择的来源项目或应用显示前 5 组；“其他已知组”和“未归属”也计入总金额。零额、负额、金额缺失或合计不符时不画环。当前为{displayMoneyTitle}；另一口径金额：{data.summary ? overviewMoney(request.money_basis === "calculated_reference" ? data.summary.gateway_charged_micros : data.summary.calculated_reference_micros) : "未读取"}。计算参考费用不是实际扣费或最终结算。</p></OverviewInfo>
      </article>
      <article className="overview-panel overview-model-card"><header><h2>模型请求分布</h2></header><Contribution value={currentContributions?.values.model} unavailable={contributionMessage("model")} nested onSelect={canDetails ? row => openWorkbench("model", true, row.ref) : undefined} /><OverviewInfo label="模型请求"><p>各条从零起点按相同刻度比较请求数；占比以当前范围全部已知请求计算，含前 5 组、其他已知组和未归属。请求数不是尝试数；资料不足或合计不符时不画比较条或占比。</p></OverviewInfo></article>
    </section>
    <section className={`overview-purpose-section${data.reading && !data.reading.dimensions.includes("purpose") ? " overview-purpose-section--unavailable" : ""}`} aria-label="AI 用途">
      <h2>AI 用途</h2>{data.reading && !data.reading.dimensions.includes("purpose") ? <p>本次阅读未提供用途维度；不能判断没有用途。</p> : <div><Contribution value={currentContributions?.values.purpose} unavailable={contributionMessage("purpose")} nested onSelect={canDetails ? row => openWorkbench("purpose", true, row.ref) : undefined} /><small>已采用用途 {data.summary?.purpose_known_requests ?? "—"} / {data.summary?.request_count ?? "—"} 条已知费用请求。</small></div>}<OverviewInfo label="AI 用途"><p>只根据已有用途资料归类，未确认用途仍留在请求总数中。用途组的条长按请求数比较，占比按全部已知费用请求计算；未提供用途维度不表示没有用途。</p></OverviewInfo>
    </section>
    <section className="overview-business-section" aria-label="近期运行与费用记录">
      <details className="overview-recent-disclosure"><summary>近期运行与费用记录 <small>展开逐条记录</small></summary>
    <article className="overview-panel overview-recent"><header><h2>近期记录</h2><small>运行请求可包含尚无费用记录的请求。</small></header>
      <div role="tablist" aria-label="近期记录视图"><button type="button" role="tab" aria-selected={recentView === "runtime"} onClick={() => setRecentView("runtime")} disabled={!canRuntime}>运行请求</button><button type="button" role="tab" aria-selected={recentView === "economic"} onClick={() => setRecentView("economic")}>费用记录</button></div>
      {recentView === "runtime" ? <div role="tabpanel" aria-label="近期运行请求">
        <details><summary>时间与筛选说明</summary><p>运行与费用各按本次授权阅读独立冻结。运行请求按已观察到的进入或最近观察时间排序；进入时间未知的请求单独标明，混合列表不用于计算统一开始时间指标。费用项目筛选需要真实经济关联，会排除无经济记录的请求。</p></details>
        {runtimeRecent?.items.length ? <div className="overview-table-scroll"><table><thead><tr><th scope="col">时间 / 请求</th><th scope="col">来源与项目</th><th scope="col">请求级结果</th><th scope="col">依据</th></tr></thead><tbody>{runtimeRecent.items.map(row => <tr key={row.request_ref}>
          <td>{row.entry_time_state === "observed" && row.entry_at ? formatDateTime(row.entry_at) : "进入时间未知"}<small>{row.request_id}</small></td>
          <td>{row.source_instance}<small>组织 {row.org_id} · 项目 {row.project_id}</small></td>
          <td>{row.outcome_basis === "no_request_observation" || row.observation_count === 0 ? "无请求级运行观察" : row.outcome ? outcomes[row.outcome] ?? "结果未核定" : "结果未核定"}</td>
          <td><button type="button" onClick={() => navigate("/workbench/runtime", row.request_ref)}>查看运行依据</button></td>
        </tr>)}</tbody></table></div> : runtimeRecent ? <p>当前运行阅读范围内没有请求；不代表来源没有活动。</p> : <p>{canRuntime ? runtimeRecentError || (runtimeRecentBusy ? "正在读取近期运行请求…" : message("runtime", "近期运行请求暂不可用")) : "无运行详情读取权限"}</p>}
        {runtimeRecentError && runtimeRecent ? <p role="alert">{runtimeRecentError}</p> : null}
        {runtimeRecent?.next_cursor ? <button type="button" disabled={runtimeRecentBusy} onClick={() => setRuntimeRecentCursor(runtimeRecent.next_cursor)}>加载更多运行请求</button> : null}
        {runtimeRecent && data.runtimeReading ? <><small>运行记录截至 {formatDateTime(data.runtimeReading.cutoff)}。结果未决表示当前观察不足，不能判定请求成功。</small><details><summary>本次阅读依据</summary><small>阅读成员 {data.runtimeReading.membership_ref}。</small></details></> : null}
      </div> : <div role="tabpanel" aria-label="近期费用记录"><p>按本次授权经济资料发生时间倒序；费用与运行关联分别标明。</p>
      {recent?.items.length ? <div className="overview-table-scroll"><table><thead><tr><th scope="col">时间 / 请求</th><th scope="col">模型与归属</th><th scope="col">AI 用途</th><th scope="col">请求级运行结果</th><th scope="col">费用</th><th scope="col">依据</th></tr></thead><tbody>{recent.items.map(row => <tr key={`${row.source_system_id}:${row.evidence_event_id}`}>
        <td>{formatDateTime(row.occurred_at)}<small>{row.request_id}</small></td>
        <td>{row.model || "模型未知"}<small>来源项目 {row.source_project || "未知"} · {row.application_id ? `应用 ID ${row.application_id}` : "应用归属未知"} · 实际操作者未知</small></td>
        <td>{row.purpose_state === "adopted" ? purposeLabels[row.purpose_category || ""] ?? row.purpose_category : row.purpose_state === "unavailable" ? "用途资料不可读" : "用途未确认"}</td>
        <td>{row.runtime.match_state === "matched" ? outcomes[row.runtime.outcome || ""] ?? "结果未决" : row.runtime.match_state === "not_in_runtime_reading" ? "本次运行阅读未匹配" : row.runtime.match_state === "ambiguous" ? "运行关联有歧义" : row.runtime.match_state === "no_request_observation" ? "无请求级运行观察" : "运行阅读不可用"}</td>
        <td>{overviewMoney(request.money_basis === "calculated_reference" ? row.calculated_reference_micros : row.gateway_charged_micros)}<small>{moneyTitle}</small></td>
        <td><button type="button" disabled={!permissions.includes("evidence.read") && !(permissions.includes("investigation.scoped.read") && row.investigation_available === true)} onClick={() => navigate(`/executions/${encodeURIComponent(row.execution_id)}?as_of=${encodeURIComponent(recent.cutoff)}&evidence=${encodeURIComponent(row.evidence_event_id)}`)}>查看执行</button></td>
      </tr>)}</tbody></table></div> : recent ? <p>当前明细授权范围内没有近期经济请求。</p> : <p>{canDetails ? recentError || data.errors.economic || (recentBusy ? "正在读取近期请求…" : "近期请求暂不可用") : "无请求明细读取权限"}</p>}
      {recentRuntimeUnavailable ? <p role="status">运行关联暂不可用；下表仍按经济资料显示，运行结果未核对。</p> : null}{recentError ? <p role="alert">{recentError}</p> : null}{recent?.next_cursor ? <button type="button" disabled={recentBusy} onClick={() => setRecentCursor(recent.next_cursor)}>加载更多近期请求</button> : null}
      {recent ? <small>经济截止 {formatDateTime(recent.cutoff)}；运行截止 {formatDateTime(recent.runtime_cutoff)}。未匹配只表示本次运行阅读无关联，不表示没有运行。</small> : null}
      </div>}
    </article>
      </details>
    </section>
    <section className="overview-business-section" aria-label="待关注与复核">
      <details className="overview-attention-disclosure"><summary>待关注与复核 <small>当前队列；不随期间与项目筛选</small></summary>
      <article className="overview-panel"><header><h2>待关注事项</h2><button type="button" disabled={!canQueue} onClick={() => navigate("/investigation/queue")}>进入复核</button></header>
        {data.runtime ? <p>运行需核对：{data.runtime.outcomes.filter(item => ["technical_failure", "interrupted", "output_held"].includes(item.outcome)).map(item => `${outcomes[item.outcome]} ${item.requests}`).join(" · ") || "当前无已归类的失败或中断"}。<button type="button" onClick={() => navigate("/workbench/runtime")}>查看运行请求</button></p> : null}
        {data.queue ? <><p>复核队列截至 {formatDateTime(data.queue.evaluated_at)}；与上方期间独立。</p><ul className="overview-attention">{data.queue.items.slice(0, 2).map(item => <li key={item.finding.id}><button className="overview-attention-link" type="button" onClick={() => navigate(`/cases/${encodeURIComponent(item.finding.case_id)}`)}>{item.finding.title || "未命名复核事项"}</button><span>{item.case_title} · {item.overdue ? "已逾期，需跟进" : "查看依据与下一步"}</span></li>)}</ul>{!data.queue.items.length ? <p>当前返回范围没有待办。</p> : null}<details><summary>队列范围说明</summary><CoverageNotes codes={data.queue.coverage.split(/[;,]\s*/)} /><small>此处仅展示前2项；队列总数与筛选以复核页为准。</small></details></> : <p>{canQueue ? message("queue") : "当前账户无复核读取权限。"}</p>}
      </article>
      </details>
    </section>
    <details className="overview-more"><summary>运行结果和来源范围的详细依据</summary><article className="overview-panel"><header><h2>运行结果</h2><button type="button" disabled={!canRuntime} onClick={() => navigate("/workbench/runtime")}>查看受影响请求与可比时延</button></header>
      {data.runtime ? <><div className="overview-outcomes">{data.runtime.outcomes.map(item => <div key={item.outcome}><span>{outcomes[item.outcome] ?? item.outcome}</span><strong>{item.requests}</strong></div>)}</div><p>截止 {formatDateTime(data.runtimeReading?.cutoff)}；未知入场时间请求另列：{data.runtimeReading?.requests_with_unknown_entry_time}。</p><CoverageNotes codes={data.runtime.coverage.gaps} /></> : <p>{canRuntime ? message("runtime") : "当前账户无运行详情读取权限。"}</p>}
    </article>
    <article className="overview-panel"><header><h2>来源与资料范围</h2></header>
      {data.sources ? <ul className="overview-sources">{data.sources.items.map(item => <li key={item.source_system_id}><strong>{item.display_name || "未命名来源"}</strong><span>{coverageNames[item.status] ?? "来源状态待确认"} · 更新 {formatDateTime(item.last_received_at ?? undefined)}</span><small>{item.scope_label === "project_restricted" ? "仅授权项目" : "来源范围"}</small><CoverageNotes codes={item.gaps} />{!coverageNames[item.status] ? <details><summary>来源状态代码</summary><code>{item.status}</code></details> : null}</li>)}</ul> : <p>{canSources ? message("sources") : "当前账户不能读取来源覆盖；未查询或枚举来源。"}</p>}
      <p>{data.reading ? `${coverageNames[data.reading.coverage.status]}；费用资料截止 ${formatDateTime(data.reading.cutoff)}。` : message("economic")} 汇总接口未提供合成/真实分类，请在来源明细核对标记；此处不能确认是生产事实。</p>
      {data.reading ? <details><summary>读取依据与限制</summary><dl><dt>费用资料读取</dt><dd>{data.reading.reading_id}</dd><dt>范围版本</dt><dd>{data.reading.authorization_revision}</dd><dt>费用资料缺口</dt><dd>{data.reading.coverage.gaps.join("、") || "未报告具体缺口"}</dd><dt>资料解释</dt><dd>{data.reading.interpretation_revision}</dd></dl></details> : null}
    </article></details>
  </section>;
}
const coverageNotes: Record<string, string> = {
  retained_investigation_records: "仅统计已保存且当前可见的调查记录",
  security_source_coverage_unconfirmed: "安全相关来源的覆盖范围尚未确认",
  forward_only_capture: "仅从启用采集后向前接收，未补采此前记录",
  timeline_completeness_unknown: "尚不能确认请求时间线完整",
  synchronous_requests_not_covered_by_lease_inventory: "运行任务清单不覆盖同步请求",
  source_fact_conflict: "来源记录存在冲突",
  period_completeness_unknown: "尚不能确认整个期间的资料完整",
  authorized: "仅当前授权范围",
  source_period_completeness_not_asserted: "来源未保证所选期间已完整采集",
  period_precedes_capture_start: "部分所选期间早于来源采集起点，历史覆盖未确认",
  source_profile_capture_gaps_present: "已观察到来源字段缺项",
  source_not_currently_connected: "建立本次阅读时来源已断开",
  scope_limited_source_state_unknown: "当前授权不足以判断整个来源的覆盖范围",
};
function CoverageNotes({ codes }: { codes: string[] }) {
  if (!codes.length) return <p>未报告具体缺口；不代表资料完整。</p>;
  const notes = [...new Set(codes.map(code => coverageNotes[code] ?? "另有来源报告的范围限制，请展开查看"))];
  return <><p>{notes.join("；")}。</p><details><summary>范围与采集状态码</summary><ul>{codes.map((code, index) => <li key={`${code}:${index}`}><code>{code}</code></li>)}</ul></details></>;
}
function makeRequest(hours: number, source: string, project: string, costProject: string, moneyBasis: WorkbenchReadingRequest["money_basis"] = "gateway_charged"): WorkbenchReadingRequest {
  const end = new Date();
  return { contract_version: "xinsight.workbench.v1", page: "cost", metric_version: "xinsight.workbench.cost.v1", period: { start_inclusive: new Date(end.getTime() - hours * 3600000).toISOString(), end_exclusive: end.toISOString(), display_timezone: Intl.DateTimeFormat().resolvedOptions().timeZone }, money_basis: moneyBasis, currency: "CNY", filters: { source_ids: parts(source), source_projects: parts(project), cost_projects: parts(costProject) } };
}
function Metric({ title, value, note, unavailable }: { title: string; value?: string | number; note: string; unavailable: string }) {
  const detail = value === undefined ? unavailable : note;
  return <article className="overview-metric"><span>{title}</span><strong>{value ?? "—"}</strong>{detail ? <small>{detail}</small> : null}</article>;
}
function OverviewInfo({ label, children }: { label: string; children: ReactNode }) {
  return <details className="overview-help"><summary aria-label={`${label}说明`}>说明</summary><div className="overview-help-content">{children}</div></details>;
}
function CostComposition({ value, unavailable, moneyTitle, onSelect }: { value?: WorkbenchContributions; unavailable: string; moneyTitle: string; onSelect?: (row: WorkbenchContribution) => void }) {
  const [focusedKey, setFocusedKey] = useState<string>();
  const [hoveredKey, setHoveredKey] = useState<string>();
  if (!value) return <p role="status">{unavailable}</p>;
  const items = [
    ...value.rows.map(row => ({ key: row.ref, label: row.label || "未命名项目", amount: row.selected_amount_micros, row })),
    { key: "__other__", label: "其他已知组", amount: value.other_known_micros, row: undefined },
    { key: "__unassigned__", label: "未归属", amount: value.unassigned_micros, row: undefined },
  ];
  let total: bigint | undefined;
  let values: bigint[] = [];
  try { total = BigInt(value.denominator_micros); values = items.map(item => BigInt(item.amount)); } catch { /* Keep exact strings in the list. */ }
  const totalAmount = total;
  const positive = totalAmount !== undefined && totalAmount > 0n && values.length === items.length && values.every(amount => amount >= 0n);
  const reconciled = positive && values.reduce((sum, amount) => sum + amount, 0n) === totalAmount;
  const reason = totalAmount === undefined || values.length !== items.length ? "金额资料不可计算，暂不绘制环形。" : totalAmount <= 0n ? "总金额为零或负值，无法绘制金额构成。" : values.some(amount => amount < 0n) ? "金额含负值或冲回，暂不绘制环形。" : !reconciled ? "分组、其他与未归属金额合计和总金额不一致，暂不绘制环形。" : "";
  let offset = 0;
  const active = items.find(item => item.key === focusedKey) ?? items.find(item => item.key === hoveredKey);
  const percent = (amount: bigint) => totalAmount && totalAmount > 0n ? Number(amount * 1000n / totalAmount) / 10 : 0;
  const percentLabel = (amount: bigint) => amount > 0n && percent(amount) === 0 ? "<0.1%" : `${percent(amount)}%`;
  const tinyAmount = reconciled && values.some(amount => amount > 0n && percent(amount) === 0);
  return <div className="overview-cost-composition"><p className="overview-cost-total"><span>{moneyTitle} · 当前范围金额</span><strong>{overviewMoney(value.denominator_micros)}</strong></p>
    <div className="overview-cost-body">{reconciled ? <div className="overview-donut-wrap"><svg className="overview-donut" viewBox="0 0 120 120" role="group" aria-label={`${value.dimension === "application" ? "应用" : "来源项目"}费用构成；分组、其他与未归属均按当前范围总金额绘制`}>
      <circle className="overview-donut-base" cx="60" cy="60" r="43" fill="none" strokeWidth="16" />
      {items.map((item, index) => { const amount = values[index]; const width = Number(amount * 1000000n / totalAmount!) / 10000; const start = offset; offset += width; return amount > 0n ? <circle key={item.key} className={`overview-donut-arc overview-donut-arc--${item.row ? index % 5 : item.key === "__other__" ? "other" : "unassigned"}`} cx="60" cy="60" r="43" fill="none" strokeWidth="16" pathLength={100} strokeDasharray={`${width} ${100 - width}`} strokeDashoffset={-start} tabIndex={0} role="img" aria-label={`${item.label} ${overviewMoney(item.amount)}，占当前总金额 ${percentLabel(amount)}`} onFocus={() => setFocusedKey(item.key)} onBlur={() => setFocusedKey(undefined)} onMouseEnter={() => setHoveredKey(item.key)} onMouseLeave={() => setHoveredKey(current => current === item.key ? undefined : current)}><title>{item.label} · {overviewMoney(item.amount)} · {percentLabel(amount)}</title></circle> : null; })}
      <text className="overview-donut-center" x="60" y="61" textAnchor="middle">费用构成</text>
    </svg><output className="overview-donut-readout" aria-live="polite">{active ? `${active.label} · ${overviewMoney(active.amount)} · ${percentLabel(BigInt(active.amount))}` : "指向或聚焦色段查看金额与占比"}</output></div> : <p role="status" className="overview-donut-unavailable">{reason}下方仍列出原始金额。</p>}
      <ul className="overview-cost-list">{items.map((item, index) => <li key={item.key} onMouseEnter={() => setHoveredKey(item.key)} onMouseLeave={() => setHoveredKey(current => current === item.key ? undefined : current)}><span className={`overview-cost-key overview-cost-key--${item.row ? index % 5 : item.key === "__other__" ? "other" : "unassigned"}`} aria-hidden="true" />
        <span className="overview-cost-name">{item.row && onSelect && item.row.drilldown_allowed ? <button type="button" onClick={() => onSelect(item.row!)} onFocus={() => setFocusedKey(item.key)} onBlur={() => setFocusedKey(undefined)} aria-label={`查看${item.label}的同一阅读依据`}>{item.label}</button> : item.label}</span>
        <strong>{overviewMoney(item.amount)}</strong><small>{reconciled ? percentLabel(values[index]) : "占比暂不可用"}</small>
      </li>)}</ul></div>
    {tinyAmount ? <small>极小金额请以列表数值为准。</small> : null}
  </div>;
}
function resourceOutcomeParts(outcomes?: NonNullable<WorkActivitySummary["resource_read_outcomes"]>) {
  return [
    { key: "completed", label: "来源报告完成", shortLabel: "完成", value: outcomes?.completed ?? 0 },
    { key: "failed", label: "失败", shortLabel: "失败", value: outcomes?.failed ?? 0 },
    { key: "optional", label: "可选不存在", shortLabel: "可选缺失", value: outcomes?.optional_not_found ?? 0 },
    { key: "unknown", label: "未确认", shortLabel: "未确认", value: outcomes?.unknown ?? 0 },
  ];
}
function outcomeZeroLabels(outcomes?: NonNullable<WorkActivitySummary["resource_read_outcomes"]>) {
  if (!outcomes) return "结果分布未提供";
  return resourceOutcomeParts(outcomes).filter(part => part.value === 0).map(part => `${part.shortLabel} 0 次`).join("、") || "无零分类";
}
function ResourceOutcomeRow({ title, total, outcomes }: { title: string; total: number; outcomes?: NonNullable<WorkActivitySummary["resource_read_outcomes"]> }) {
  const parts = resourceOutcomeParts(outcomes);
  const sum = parts.reduce((count, part) => count + part.value, 0);
  return <div className="overview-resource-row" role="group" aria-label={`${title} ${total} 次尝试：${outcomes ? parts.map(part => `${part.label} ${part.value}`).join("，") : "结果分布未提供"}`}><div className="overview-resource-head"><span>{title}</span><div><strong>{total}</strong><span>次尝试</span></div></div>
    {!outcomes ? <p role="status">结果分布未提供；不能把尝试当作已完成。</p> : <><div className="overview-resource-counts">
      {parts.filter(part => part.value > 0).map(part => <span key={part.key} className={`overview-resource-${part.key}`}><i aria-hidden="true" />{part.shortLabel}<strong>{part.value}</strong></span>)}</div>
      {sum !== total ? <p role="status">结果分型合计 {sum} 与尝试数 {total} 不一致，请核对来源。</p> : null}</>}
  </div>;
}
function Contribution({ value, unavailable, nested, onSelect }: { value?: WorkbenchContributions; unavailable: string; nested?: boolean; onSelect?: (row: WorkbenchContribution) => void }) {
  // Do not infer the complete request denominator from the visible Top-N rows.
  const requestsAvailable = value?.order_by === "requests" && value.denominator_requests !== undefined && value.unassigned_requests !== undefined && value.other_known_requests !== undefined;
  const requestsReconciled = requestsAvailable && [value.denominator_requests!, value.other_known_requests!, value.unassigned_requests!, ...value.rows.map(row => row.request_count)].every(count => Number.isSafeInteger(count) && count >= 0) && value.rows.reduce((total, row) => total + row.request_count, value.other_known_requests! + value.unassigned_requests!) === value.denominator_requests;
  const maxRequests = requestsReconciled ? Math.max(0, ...value!.rows.map(row => row.request_count)) : 0;
  const content = value && requestsAvailable ? <>
    <div className="overview-contribution-scale"><span>0</span><span>{requestsReconciled ? `${maxRequests} 条请求` : "分组总数待核对"}</span></div>
    {!requestsReconciled ? <p role="status">分组、其他与未归属请求数合计和当前范围不一致；暂不绘制比较条或占比。</p> : null}
    <ul className="overview-contributions">{value.rows.map(row => {
      const label = value.dimension === "purpose" ? purposeLabels[row.label] ?? row.label : row.label || "未命名项目";
      const share = requestsReconciled ? overviewShare(String(row.request_count), String(value.denominator_requests)) : "占比暂不可用";
      const width = requestsReconciled ? shareWidth(String(row.request_count), String(maxRequests)) : undefined;
      return <li key={row.ref}><div className="overview-contribution-line">
        {onSelect && row.drilldown_allowed ? <button type="button" onClick={() => onSelect(row)} aria-label={`查看${label}的同一阅读依据`}>{label}</button> : <span title={label}>{label}</span>}
        <span className="overview-contribution-values"><strong>{row.request_count} 条</strong><small>{share}</small><small>{overviewMoney(row.selected_amount_micros)}</small>{!row.drilldown_allowed ? <small>仅汇总</small> : null}</span>
      </div><span className="overview-contribution-bar" aria-hidden="true">{width ? <i style={{ width }} /> : null}</span></li>;
    })}</ul>
    <div className="overview-contribution-footer"><span>当前范围请求 {value.denominator_requests} 条 · 其他已知组 {value.other_known_requests} 条 · 未归属 {value.unassigned_requests} 条</span></div>
  </> : <p>{value ? "当前资料未提供请求总数，无法计算构成。" : unavailable}</p>;
  return nested ? <div>{content}</div> : <article className="overview-panel">{content}</article>;
}
function Trend({ series, moneyTitle, onOpen }: { series: WorkbenchSeries; moneyTitle: string; onOpen: (activeBucket: string) => void }) {
  const [focusedIndex, setFocusedIndex] = useState<number>();
  const [hoveredIndex, setHoveredIndex] = useState<number>();
  const points = series.points.map((point, index) => {
    const start = Date.parse(point.start_inclusive);
    const end = Date.parse(point.end_exclusive);
    const validTime = Number.isFinite(start) && Number.isFinite(end) && end > start;
    const validValue = /^-?\d+$/.test(point.known_value) && (series.unit === "micros" || !point.known_value.startsWith("-"));
    const value = validValue ? BigInt(point.known_value) : undefined;
    return { ...point, index, start, end, validTime, value, visible: validTime && value !== undefined && (point.complete || value !== 0n) };
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
    return previous.complete && point.complete && previous.end === point.start && from && to ? [{ from, to }] : [];
  });
  const active = plotted.find(point => point.index === focusedIndex) ?? plotted.find(point => point.index === hoveredIndex);
  const labelFor = (point: WorkbenchSeries["points"][number]) => `${formatDateTime(point.start_inclusive)} 至 ${formatDateTime(point.end_exclusive)}：${series.unit === "micros" ? `${moneyTitle} ${overviewMoney(point.known_value)}` : `${point.known_value} 条已知请求`}${point.complete ? "，时段资料完整" : "，仅为已观察值，时段完整性未知"}`;
  const axisLabel = (value: bigint) => series.unit === "micros" ? overviewMoney(String(value)) : String(value);
  const shortTime = (value: number) => new Intl.DateTimeFormat("zh-CN", { month: "numeric", day: "numeric", ...(lastTime - firstTime <= 172800000 ? { hour: "2-digit" as const, minute: "2-digit" as const } : {}), hour12: false }).format(value);
  const unknownZeroCount = points.filter(point => !point.complete && point.value === 0n).length;
  const invalidCount = points.filter(point => !point.validTime || point.value === undefined).length;
  const incomplete = points.some(point => !point.complete);
  const coverageLabel = coverageNames[series.coverage.status] ?? "覆盖状态待确认";
  const caption = [incomplete && !coverageLabel.includes("完整性未知") ? `${coverageLabel}，完整性未确认` : coverageLabel,
    plotted.length <= 1 ? plotted.length ? "仅1个时段有记录，暂不足以形成趋势" : "暂无可确认的时段值" : segments.length === 0 ? "时段未确认连续，未连线" : incomplete ? "仅完整连续时段连线" : undefined,
    unknownZeroCount && plotted.length > 1 ? "另有未确认空时段" : undefined, invalidCount ? `${invalidCount}个时段无法计算` : undefined].filter(Boolean).join(" · ");
  return <>{dated.length ? <><div className="overview-trend-frame"><div className="overview-trend-axis" aria-hidden="true"><span>{domain > 0n ? axisLabel(maximum) : ""}</span><span>{axisLabel(minimum)}</span></div><div className="overview-trend-plot"><div className="overview-trend" role="group" aria-label={`${series.metric === "requests" ? "模型已知请求" : "已知费用"}时段，逐段可聚焦`}>
    <svg className="overview-trend-lines" viewBox="0 0 1000 140" preserveAspectRatio="none" aria-hidden="true"><line className="overview-trend-zero" x1="0" x2="1000" y1={yFor(0n)} y2={yFor(0n)} />{segments.map(({ from, to }) => <line key={`${from.index}:${to.index}`} className="overview-trend-segment" data-from={from.index} data-to={to.index} x1={from.x * 10} y1={from.y} x2={to.x * 10} y2={to.y} />)}</svg>
    {plotted.map(point => <button key={`${point.start_inclusive}:${point.index}`} className={`overview-trend-point${point.complete ? "" : " overview-trend-point--partial"}`} type="button" style={{ left: `${point.x}%`, top: `${point.y + 12}px` }} aria-label={labelFor(point)} title={labelFor(point)} onFocus={() => setFocusedIndex(point.index)} onBlur={() => setFocusedIndex(undefined)} onMouseEnter={() => setHoveredIndex(point.index)} onMouseLeave={() => setHoveredIndex(current => current === point.index ? undefined : current)}><span aria-hidden="true" /></button>)}
  </div><output className="overview-trend-tooltip" aria-live="polite">{active ? labelFor(active) : "聚焦数据点查看准确时间和值"}</output></div></div><div className="overview-trend-range"><span>{shortTime(firstTime)}</span><span>{shortTime(lastTime)}</span></div></> : <p role="status">本次阅读没有可用的时间范围；不推断期间没有请求。</p>}
    <p className="overview-chart-caption" role="status">{caption}</p>
    <button type="button" onClick={() => onOpen(active ? labelFor(active) : "")}>查看整个当前范围的{series.metric === "requests" ? "请求" : "费用"}专题</button></>;
}
