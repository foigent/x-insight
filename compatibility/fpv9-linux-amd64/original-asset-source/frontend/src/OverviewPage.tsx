import { useEffect, useRef, useState, type FormEvent } from "react";
import { ApiRequestError, investigationQueueAPI, isAbortError, runtimeAPI, scopeSessionKey, workbenchAPI,
  type AccessSessionResponse, type InvestigationQueuePage, type WorkbenchContributions, type WorkbenchReading,
  type WorkbenchReadingRequest, type WorkbenchSeries, type WorkbenchSourceStatuses, type WorkbenchSummary,
  type OverviewRecentRequests } from "./api";
import type * as Runtime from "./runtime";
import { formatDateTime } from "./format";
import { purposeLabels } from "./PurposePanel";
import "./overview.css";

export interface OverviewContext { request: WorkbenchReadingRequest; hours: number; contribution: "purpose" | "source_project"; contributionMetric?: ContributionMetric; economicReadingID?: string; runtimeReadingID?: string }
interface Props { session: AccessSessionResponse; navigate: (path: string, selectedRuntimeRequestRef?: string) => void; context?: OverviewContext; onContext: (value: OverviewContext) => void }
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
  try { const denominator = BigInt(total); return denominator > 0n ? `${Math.min(100, Math.max(0, Number(BigInt(value) * 100n / denominator)))}%` : "0%"; }
  catch { return "0%"; }
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
  const [costDimension, setCostDimension] = useState<"application" | "source_project">("application");
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
  const identity = scopeSessionKey(session);
  const sink = useRef(onContext); sink.current = onContext;
  const permissions = session.session.permissions;
  const canRuntime = permissions.includes("workbench.summary.read") && permissions.includes("workbench.details.read");
  const canDetails = permissions.includes("workbench.details.read");
  const canQueue = permissions.includes("investigation.scoped.read") || permissions.includes("evidence.read");
  const canSources = permissions.includes("workbench.coverage.read");
  useEffect(() => { sink.current({ request, hours: Math.round((Date.parse(request.period.end_exclusive!) - Date.parse(request.period.start_inclusive!)) / 3600000), contribution, contributionMetric, economicReadingID: data.reading?.reading_id ?? (busy && revision === 0 ? initial?.economicReadingID : undefined), runtimeReadingID: data.runtimeReading?.reading_id ?? (busy && revision === 0 ? initial?.runtimeReadingID : undefined) }); }, [request, contribution, contributionMetric, data.reading, data.runtimeReading, busy, revision]);
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
  const costContributionMessage = data.errors.economic ?? currentCostContributions?.errors[appliedCostDimension] ?? (data.reading?.dimensions.includes(appliedCostDimension) ? "正在读取…" : message(appliedCostDimension, "当前读取不支持此维度"));
  const localCompleted = data.runtime?.outcomes.find(item => item.outcome === "local_delivery_completed")?.requests;
  const series = data[metric];
  const sourceChoices = data.sources?.items ?? [];
  const selectedSource = !source ? "" : sourceChoices.some(item => item.source_system_id === source) ? source : "__precise__";
  const appliedFilters = request.filters;
  const activeFilterCount = appliedFilters.source_ids.length + appliedFilters.source_projects.length + appliedFilters.cost_projects.length;
  const appliedSourceNames = appliedFilters.source_ids.map(id => sourceChoices.find(item => item.source_system_id === id)?.display_name ?? id);
  return <section className="overview-page" aria-label="组合总览">
    <div className="overview-top"><header className="overview-heading"><div><span className="eyebrow">工作台 · 当前授权范围</span><h1>总览</h1></div><p>使用、运行、费用与需要跟进的事</p></header>
    <form className="overview-filters" onSubmit={apply}>
      <div className="overview-filter-controls"><label>期间<select value={hours} onChange={event => setHours(Number(event.target.value))}><option value={24}>最近24小时</option><option value={168}>最近7天</option><option value={720}>最近30天</option></select></label>
      {canSources ? <label>来源<select aria-label="来源" value={selectedSource} disabled={!data.sources} onChange={event => setSource(event.target.value)}><option value="">全部已授权来源</option>{selectedSource === "__precise__" ? <option value="__precise__" disabled>已设精确来源条件</option> : null}{sourceChoices.map(item => <option key={item.source_system_id} value={item.source_system_id}>来源 · {item.display_name}</option>)}</select></label> : null}
      <label>费用口径<select value={moneyBasis} onChange={event => setMoneyBasis(event.target.value as typeof moneyBasis)}><option value="calculated_reference">计算参考</option><option value="gateway_charged">网关已计</option></select></label>
      <button type="submit">应用并读取</button></div>
      <div className="overview-filter-meta"><div className="overview-active-filters"><span>生效范围：{activeFilterCount ? `来源 ${appliedSourceNames.join("、") || "全部已授权"}；来源项目 ${appliedFilters.source_projects.join("、") || "全部"}；费用项目 ${appliedFilters.cost_projects.join("、") || "全部"}` : "全部已授权来源与项目"} · {formatDateTime(request.period.start_inclusive)} — {formatDateTime(request.period.end_exclusive)} · 金额口径：{moneyTitle}</span>{activeFilterCount ? <button type="button" onClick={clearFilters}>清除项目与来源筛选</button> : null}</div>
      <details className="overview-advanced" open={advancedOpen} onToggle={event => setAdvancedOpen(event.currentTarget.open)}><summary>高级筛选与口径说明</summary><div className="overview-advanced-content">
        <label>来源筛选<input value={source} onChange={event => setSource(event.target.value)} placeholder="来源标识，逗号分隔" /></label>
        <label>来源项目<input value={project} onChange={event => setProject(event.target.value)} placeholder="项目标识" /></label>
        <label>费用项目<input value={costProject} onChange={event => setCostProject(event.target.value)} placeholder="项目标识" /></label>
        <p className="overview-period">当前期间：{formatDateTime(request.period.start_inclusive)} — {formatDateTime(request.period.end_exclusive)}。专题沿用此起止时间，重新读取时显示各自截止。</p>
        <p>当前金额口径：{moneyTitle}。计算参考费用用于参考，不代表网关实际扣费或最终结算。</p>
      </div></details></div>
    </form></div>
    {invalid ? <p role="alert">{invalid}</p> : null}
    <div className="overview-stats">
      <Metric title="费用资料中的请求" value={data.summary?.request_count} note={data.summary?.request_total_complete ? "本次经济阅读成员" : "已知经济成员；不等于所有运行请求或尝试"} unavailable={message("summary")} />
      <Metric title="运行本地交付完成" value={data.runtime ? `${localCompleted ?? 0} / ${data.runtime.requests}` : undefined} note="已知入场请求的请求级结果；不代表业务成功" unavailable={canRuntime ? message("runtime") : "无运行详情读取权限"} />
      <Metric title={data.reading?.money_title ?? moneyTitle} value={data.summary ? overviewMoney(data.summary.selected_amount_micros) : undefined} note={data.summary ? `${request.money_basis === "calculated_reference" ? "参考金额，非扣费" : "网关已计金额"}；另一口径 ${overviewMoney(request.money_basis === "calculated_reference" ? data.summary.gateway_charged_micros : data.summary.calculated_reference_micros)}；未覆盖不计零` : ""} unavailable={message("summary")} />
      <Metric title="已记录应用组" value={currentContributions?.values.application?.authorized_group_count} note={`经济资料中的应用分组；未归属请求 ${currentContributions?.values.application?.unassigned_requests ?? "未知"}`} unavailable={contributionMessage("application")} />
      <Metric title="当前复核待办" value={data.queue?.total} note="当前队列；不受上方期间与项目筛选限制" unavailable={canQueue ? message("queue") : "无复核读取权限"} />
    </div>
    <div className="overview-primary">
      <article className="overview-panel"><header><h2>{metric === "requests" ? "已知请求趋势" : "已知费用趋势"}</h2><select aria-label="趋势指标" value={metric} onChange={e => setMetric(e.target.value as typeof metric)}><option value="requests">请求</option><option value="amount">费用</option></select></header>
        {series ? <Trend series={series} /> : <p role="status">{message(metric)}</p>}
        <p>费用资料成员按发生时间分桶；缺失资料不补为真实零，不用于计算运行失败率。</p>
        <button type="button" onClick={() => navigate("/workbench/cost")}>查看费用与项目贡献</button>
      </article>
      <article className="overview-panel"><header><h2>待关注事项</h2><button type="button" disabled={!canQueue} onClick={() => navigate("/investigation/queue")}>进入复核</button></header>
        {data.runtime ? <p>运行需核对：{data.runtime.outcomes.filter(item => ["technical_failure", "interrupted", "output_held"].includes(item.outcome)).map(item => `${outcomes[item.outcome]} ${item.requests}`).join(" · ") || "当前无已归类的失败或中断"}。<button type="button" onClick={() => navigate("/workbench/runtime")}>查看运行请求</button></p> : null}
        {data.queue ? <><p>复核队列截至 {formatDateTime(data.queue.evaluated_at)}；与上方期间独立。</p><ul className="overview-attention">{data.queue.items.slice(0, 2).map(item => <li key={item.finding.id}><button className="overview-attention-link" type="button" onClick={() => navigate(`/cases/${encodeURIComponent(item.finding.case_id)}`)}>{item.finding.title || "未命名复核事项"}</button><span>{item.case_title} · {item.overdue ? "已逾期，需跟进" : "查看依据与下一步"}</span></li>)}</ul>{!data.queue.items.length ? <p>当前返回范围没有待办。</p> : null}<details><summary>队列范围说明</summary><CoverageNotes codes={data.queue.coverage.split(/[;,]\s*/)} /><small>此处仅展示前2项；队列总数与筛选以复核页为准。</small></details></> : <p>{canQueue ? message("queue") : "当前账户无复核读取权限。"}</p>}
      </article>
    </div>
    <div className="overview-comparison-heading"><h2>构成与贡献</h2><small>模型与用途按已知请求比较；费用按所选口径比较。请求不是尝试，未归属仍在分母中。</small></div>
    <div className="overview-secondary">
      <Contribution title="模型使用" metric="requests" value={currentContributions?.values.model} unavailable={contributionMessage("model")} />
      <article className="overview-panel"><header><h2>AI 用途</h2></header><Contribution metric="requests" value={currentContributions?.values.purpose} unavailable={contributionMessage("purpose")} nested /><small>已采用用途 {data.summary?.purpose_known_requests ?? "—"} / {data.summary?.request_count ?? "—"} 条已知经济请求。</small><button type="button" onClick={() => navigate("/workbench/cost?view=usage")}>查看使用与目的</button></article>
      <article className="overview-panel"><header><h2>费用贡献</h2><select aria-label="费用贡献维度" value={appliedCostDimension} onChange={event => setCostDimension(event.target.value as typeof costDimension)}><option value="application" disabled={!data.reading?.dimensions.includes("application")}>应用</option><option value="source_project">来源项目</option></select></header><Contribution metric="amount" value={currentCostContributions?.values[appliedCostDimension]} unavailable={costContributionMessage} nested /><small>{appliedCostDimension === "application" ? "按已记录应用" : "按来源项目"} · {moneyTitle}；参考金额不是网关扣费。</small><button type="button" onClick={() => navigate("/workbench/cost")}>查看费用与项目贡献</button></article>
    </div>
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
  return <article className="overview-metric"><span>{title}</span><strong>{value ?? "—"}</strong><small>{value === undefined ? unavailable : note}</small></article>;
}
function Contribution({ title, value, unavailable, nested, metric }: { title?: string; value?: WorkbenchContributions; unavailable: string; nested?: boolean; metric: ContributionMetric }) {
  const counts = metric === "requests";
  // An older response has no complete request denominator: never infer it from Top-N.
  const requestsAvailable = value?.order_by === "requests" && value.denominator_requests !== undefined && value.unassigned_requests !== undefined && value.other_known_requests !== undefined;
  const content = <>{title ? <header><h2>{title}</h2></header> : null}{value && (!counts || requestsAvailable) ? <>
    <ul className="overview-contributions">{value.rows.map(row => <li key={row.ref}>
      <span>{value.dimension === "purpose" ? purposeLabels[row.label] ?? row.label : row.label || "未命名项目"}</span>
      <strong>{counts ? `${row.request_count} 条已知请求` : overviewMoney(row.selected_amount_micros)}</strong>
      <small>{counts ? `占当前资料请求 ${overviewShare(String(row.request_count), String(value.denominator_requests))} · ${overviewMoney(row.selected_amount_micros)}` : `${row.request_count} 条已知请求 · ${overviewShare(row.share.numerator, row.share.denominator)}`}</small>
      <span className="overview-contribution-bar" aria-hidden="true"><i style={{ width: shareWidth(counts ? String(row.request_count) : row.share.numerator, counts ? String(value.denominator_requests) : row.share.denominator) }} /></span>
    </li>)}</ul>
    {counts ? <><p>其他已知组 {value.other_known_requests} 条 · 未归属 {value.unassigned_requests} 条</p><small>当前范围请求 {value.denominator_requests} 条；展示前5组，其他组与未归属均保留在分母中。</small></> : <><p>其他已知组 {overviewMoney(value.other_known_micros)} · 未归属 {overviewMoney(value.unassigned_micros)}</p><small>当前范围金额 {overviewMoney(value.denominator_micros)}；展示前5组，未归属金额保留。</small></>}
  </> : <p>{value && counts ? "当前响应未提供完整请求数口径，无法计算构成。" : unavailable}</p>}</>;
  return nested ? <div>{content}</div> : <article className="overview-panel">{content}</article>;
}
function Trend({ series }: { series: WorkbenchSeries }) {
  const max = Math.max(0, ...series.points.map(point => Number(point.known_value)));
  return <><div className="overview-trend-frame"><div className="overview-trend-axis" aria-hidden="true"><span>{series.unit === "micros" ? overviewMoney(String(max)) : max}</span><span>{series.unit === "micros" ? overviewMoney("0") : "0"}</span></div><div className="overview-trend" role="img" aria-label={`${series.metric === "requests" ? "已知请求" : "已知费用"}趋势，逐桶数值可聚焦查看`}>
    {series.points.map(point => { const value = Number(point.known_value); const label = `${formatDateTime(point.start_inclusive)} 至 ${formatDateTime(point.end_exclusive)}：${series.unit === "micros" ? overviewMoney(point.known_value) : `${point.known_value} 条`}${point.complete ? "" : "，完整性未知"}`; return <div className="overview-bucket" key={point.start_inclusive} tabIndex={0} aria-label={label} title={label}><span style={{ height: `${max > 0 && Number.isFinite(value) ? Math.max(0, value / max) * 100 : 0}%` }} /><small>{point.complete ? "" : "·"}</small></div>; })}
  </div></div><div className="overview-trend-range"><span>{formatDateTime(series.points[0]?.start_inclusive)}</span><span>{formatDateTime(series.points[Math.floor((series.points.length - 1) / 2)]?.start_inclusive)}</span><span>{formatDateTime(series.points.at(-1)?.end_exclusive)}</span></div><p>{coverageNames[series.coverage.status]}。{series.points.some(point => !point.complete) ? "标点表示该桶期间完整性未知；零只表示已接收资料的已知值。" : "各桶完整性按来源返回值呈现。"}</p></>;
}
