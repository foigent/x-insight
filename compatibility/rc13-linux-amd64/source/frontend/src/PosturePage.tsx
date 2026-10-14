import { useEffect, useMemo, useState } from "react";

import {
  investigationAPI,
  isAbortError,
  workspaceAPI,
  type AccessSessionResponse,
  type ConnectorListResponse,
  type ExecutionSearchResponse,
  type InvestigationAlert,
  type InvestigationCase,
  type PostureCoverage,
  type PostureSnapshot,
  type PostureTrendBucket,
  type SavedView,
  type SourceOption,
  type TraceJourney,
  type TraceJourneyNode,
} from "./api";
import { EmptyState, ErrorState, LoadingBlock } from "./Feedback";
import { formatCanonicalTime, formatDateTime, shortRef } from "./format";
import { OutcomeBadge } from "./status";

interface PosturePageProps {
  session: AccessSessionResponse;
  navigate: (path: string) => void;
}

type RangeKey = "24h" | "7d";
type TrendSignal = "failures" | "gaps" | "alerts";

const rangeDuration: Record<RangeKey, number> = {
  "24h": 24 * 60 * 60 * 1000,
  "7d": 7 * 24 * 60 * 60 * 1000,
};

export function PosturePage({ session, navigate }: PosturePageProps) {
  const [range, setRange] = useState<RangeKey>("24h");
  const [sourceSystemID, setSourceSystemID] = useState("");
  const [posture, setPosture] = useState<PostureSnapshot>();
  const [executions, setExecutions] = useState<ExecutionSearchResponse>();
  const [cases, setCases] = useState<InvestigationCase[]>([]);
  const [connectors, setConnectors] = useState<ConnectorListResponse>();
  const [views, setViews] = useState<SavedView[]>([]);
  const [sources, setSources] = useState<SourceOption[]>([]);
  const [partialErrors, setPartialErrors] = useState<Record<string, unknown>>({});
  const [loading, setLoading] = useState(true);
  const [selectedAlertID, setSelectedAlertID] = useState("");
  const [journey, setJourney] = useState<TraceJourney>();
  const [journeyError, setJourneyError] = useState<unknown>();
  const [journeyLoading, setJourneyLoading] = useState(false);
  const [trendSignal, setTrendSignal] = useState<TrendSignal>("failures");

  useEffect(() => {
    const controller = new AbortController();
    const asOf = new Date().toISOString();
    const from = new Date(Date.parse(asOf) - rangeDuration[range]).toISOString();
    setLoading(true);
    setPartialErrors({});
    Promise.allSettled([
      investigationAPI.getPosture({ asOf, from, to: asOf, sourceSystemID }, controller.signal),
      investigationAPI.search({ asOf, from, to: asOf, sourceSystemID }, undefined, controller.signal),
      workspaceAPI.listCases(controller.signal),
      investigationAPI.listConnectors(controller.signal),
      workspaceAPI.listSavedViews(controller.signal),
      investigationAPI.listSources(controller.signal),
    ])
      .then(([postureResult, executionResult, caseResult, connectorResult, savedResult, sourceResult]) => {
        if (controller.signal.aborted) return;
        const failures: Record<string, unknown> = {};
        if (postureResult.status === "fulfilled") {
          setPosture(postureResult.value);
          setSelectedAlertID((current) => (
            postureResult.value.risk.items.some((item) => item.id === current)
              ? current
              : postureResult.value.risk.items[0]?.id ?? ""
          ));
        } else {
          failures.posture = postureResult.reason;
          setPosture(undefined);
        }
        if (executionResult.status === "fulfilled") setExecutions(executionResult.value);
        else failures.executions = executionResult.reason;
        if (caseResult.status === "fulfilled") setCases(caseResult.value.items);
        else failures.cases = caseResult.reason;
        if (connectorResult.status === "fulfilled") setConnectors(connectorResult.value);
        else failures.connectors = connectorResult.reason;
        if (savedResult.status === "fulfilled") setViews(savedResult.value.items);
        else failures.views = savedResult.reason;
        if (sourceResult.status === "fulfilled") setSources(sourceResult.value.items);
        else failures.sources = sourceResult.reason;
        setPartialErrors(failures);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [range, sourceSystemID]);

  const attentionExecutions = useMemo(
    () => executions?.items.filter((item) => item.has_gaps || item.outcome !== "success").slice(0, 5) ?? [],
    [executions],
  );
  const selectedAlert = posture?.risk.items.find((item) => item.id === selectedAlertID) ?? posture?.risk.items[0];
  const journeyExecutionID = selectedAlert
    ? selectedAlert.execution_id ?? ""
    : attentionExecutions[0]?.execution_id || "";

  useEffect(() => {
    const controller = new AbortController();
    if (!posture || !journeyExecutionID) {
      setJourney(undefined);
      setJourneyError(undefined);
      setJourneyLoading(false);
      return () => controller.abort();
    }
    setJourney(undefined);
    setJourneyError(undefined);
    setJourneyLoading(true);
    investigationAPI.getTraceJourney(journeyExecutionID, posture.as_of, controller.signal)
      .then((result) => setJourney(result))
      .catch((caught) => {
        if (!isAbortError(caught) && !controller.signal.aborted) setJourneyError(caught);
      })
      .finally(() => {
        if (!controller.signal.aborted) setJourneyLoading(false);
      });
    return () => controller.abort();
  }, [journeyExecutionID, posture]);

  const openCases = cases.filter((item) => item.status === "open");
  const postureState = posture ? summarizePosture(posture) : undefined;

  return (
    <div className="posture-page">
      <header className="posture-intro">
        <div>
          <span className="eyebrow">Cross-system posture</span>
          <h1>运行态势</h1>
          <p>从 MaaS、身份、策略与执行证据中判断哪里值得调查；来源运行指标只作为上下文，不复制 MaaS 控制台。</p>
        </div>
        <div className="posture-scope" aria-label="态势范围">
          <label>
            <span>时间范围</span>
            <select value={range} onChange={(event) => setRange(event.target.value as RangeKey)}>
              <option value="24h">最近 24 小时</option>
              <option value="7d">最近 7 天</option>
            </select>
          </label>
          <label>
            <span>来源</span>
            <select value={sourceSystemID} onChange={(event) => setSourceSystemID(event.target.value)}>
              <option value="">全部来源</option>
              {sources.map((source) => (
                <option key={source.source_system_id} value={source.source_system_id}>{source.display_name}</option>
              ))}
            </select>
          </label>
        </div>
      </header>

      {loading ? <LoadingBlock label="正在构建固定态势快照" /> : null}
      {!loading && Object.keys(partialErrors).length > 0 ? (
        <div className="degraded-note" role="status">
          部分区域暂不可用：{Object.keys(partialErrors).join("、")}。失败读取不会被显示为零或健康。
        </div>
      ) : null}
      {!loading && partialErrors.posture ? <ErrorState error={partialErrors.posture} /> : null}

      {posture ? (
        <>
          <p className="posture-snapshot-line">
            固定快照 <code>{formatCanonicalTime(posture.as_of)}</code>
            <span>{formatDateTime(posture.from)} — {formatDateTime(posture.to)}</span>
          </p>

          <section className="posture-summary-grid" aria-label="态势摘要">
            <article className={`posture-hero posture-hero-${postureState?.tone}`}>
              <div className="posture-hero-status">
                <span><i aria-hidden="true" />{postureState?.label}</span>
                <small>{range === "24h" ? "24 小时固定窗口" : "7 天固定窗口"}</small>
              </div>
              <div className="posture-hero-copy">
                <strong>{formatCount(posture.risk.open_total)}</strong>
                <div>
                  <h2>项开放 Alert 值得审阅</h2>
                  <p>{postureState?.description}</p>
                </div>
              </div>
              <dl className="posture-hero-metrics">
                <div><dt>严重 / 错误</dt><dd>{formatCount(posture.risk.critical + posture.risk.error)}</dd></div>
                <div><dt>关联执行</dt><dd>{formatCount(posture.risk.affected_executions)}</dd></div>
                <div><dt>有 Gap 执行</dt><dd>{formatCount(posture.executions.with_gaps)}</dd></div>
              </dl>
              <button
                type="button"
                className="posture-hero-action"
                disabled={!selectedAlert && attentionExecutions.length === 0}
                onClick={() => openSelectedEvidence(selectedAlert, selectedAlert ? undefined : attentionExecutions[0]?.execution_id, posture.as_of, navigate)}
              >
                从最高优先级证据开始 <span aria-hidden="true">→</span>
              </button>
            </article>

            <CoverageCard coverage={posture.coverage} />
            <SourceTrustCard
              posture={posture}
              connectors={connectors}
              connectorError={partialErrors.connectors}
              selectedSourceSystemID={sourceSystemID}
              navigate={navigate}
            />
          </section>

          <RuntimeJourney
            journey={journey}
            loading={journeyLoading}
            error={journeyError}
            executionID={journeyExecutionID}
            asOf={posture.as_of}
            navigate={navigate}
          />

          <section className="posture-analysis-grid">
            <RiskQueue
              posture={posture}
              executions={executions}
              selectedAlertID={selectedAlert?.id ?? ""}
              onSelect={setSelectedAlertID}
              navigate={navigate}
            />
            <TrendPanel posture={posture} signal={trendSignal} onSignal={setTrendSignal} />
          </section>

          <section className="posture-work-grid" aria-label="调查工作区">
            <article className="posture-panel">
              <SectionHeading eyebrow="Investigation workspace" title="进行中的 Cases" />
              {partialErrors.cases ? (
                <p className="quiet-copy">Cases 暂不可用；没有把读取失败显示为空列表。</p>
              ) : openCases.length === 0 ? (
                <p className="quiet-copy">还没有 open Case。可从 Alert 或 Execution Story 建立证据包。</p>
              ) : (
                <div className="posture-compact-list">
                  {openCases.slice(0, 5).map((item) => (
                    <button type="button" key={item.id} onClick={() => navigate(`/cases/${item.id}`)}>
                      <span><strong>{item.title}</strong><small>revision {item.version} · {formatDateTime(item.updated_at)}</small></span>
                      <span aria-hidden="true">→</span>
                    </button>
                  ))}
                </div>
              )}
              <button type="button" className="posture-text-link" onClick={() => navigate("/cases")}>查看全部 Cases →</button>
            </article>

            <article className="posture-panel">
              <SectionHeading eyebrow="Saved entry points" title="已保存的调查视图" />
              {partialErrors.views ? (
                <p className="quiet-copy">Saved Views 暂不可用；没有把读取失败显示为空列表。</p>
              ) : views.length === 0 ? (
                <p className="quiet-copy">可在 Execution Explorer 保存固定筛选入口。</p>
              ) : (
                <div className="posture-compact-list">
                  {views.slice(0, 5).map((view) => (
                    <button type="button" key={view.id} onClick={() => navigate(`/executions?${savedViewSearch(view)}`)}>
                      <span><strong>{view.name}</strong><small>{view.presentation}</small></span>
                      <code>{shortRef(view.id)}</code>
                    </button>
                  ))}
                </div>
              )}
              <button type="button" className="posture-text-link" onClick={() => navigate("/executions")}>打开 Execution Explorer →</button>
            </article>

            <article className="posture-panel posture-boundary-panel">
              <SectionHeading eyebrow="Evidence contract" title="这张首页没有声称什么" />
              <ul>
                <li>Alert 是分析记录，不是 M0 Event。</li>
                <li>Alert 以 observed 窗口筛选，并读取当前状态；不重建历史状态。</li>
                <li>覆盖率只衡量已观察字段与声明关系，不代表现实世界完整。</li>
                <li>Connector 状态是当前运行状态，不进入事件 hash。</li>
                <li>MaaS 指标不在这里重复成为第二套运营真相。</li>
              </ul>
              <span className="posture-role">当前角色 · {session.session.role}</span>
            </article>
          </section>
        </>
      ) : null}
    </div>
  );
}

function CoverageCard({ coverage }: { coverage: PostureCoverage[] }) {
  const available = coverage.filter((item) => item.available && item.ratio_decimal !== undefined);
  const weakest = [...available].sort((left, right) => Number(left.ratio_decimal) - Number(right.ratio_decimal))[0];
  return (
    <article className="posture-card posture-coverage-card">
      <header>
        <div><span className="posture-card-label">Evidence posture</span><h2>证据覆盖</h2></div>
        <strong>{weakest ? `最弱 ${formatPercent(weakest.ratio_decimal)}` : "口径不可用"}</strong>
      </header>
      <p className="posture-card-copy">四个口径不做无意义平均；每个比率都保留自己的分子、分母与限制。</p>
      <div className="posture-axis-list">
        {coverage.map((axis) => {
          const width = axis.available ? Math.max(0, Math.min(100, Number(axis.ratio_decimal) * 100)) : 0;
          return (
            <div key={axis.dimension} className={!axis.available || width < 80 ? "axis-limited" : undefined} title={`${axis.basis}。${axis.limitation}`}>
              <span>{coverageLabel(axis.dimension)}</span>
              <i aria-hidden="true"><b style={{ width: `${width}%` }} /></i>
              <strong>{axis.available ? formatPercent(axis.ratio_decimal) : "N/A"}</strong>
              <small>{axis.available ? `${axis.numerator} / ${axis.denominator}` : "无合格分母"}</small>
            </div>
          );
        })}
      </div>
    </article>
  );
}

function SourceTrustCard({
  posture,
  connectors,
  connectorError,
  selectedSourceSystemID,
  navigate,
}: {
  posture: PostureSnapshot;
  connectors?: ConnectorListResponse;
  connectorError?: unknown;
  selectedSourceSystemID: string;
  navigate: (path: string) => void;
}) {
  const visibleConnectors = (connectors?.items ?? []).filter((item) => (
    selectedSourceSystemID === "" || item.source_system_id === selectedSourceSystemID
  ));
  const connectorBySource = new Map(visibleConnectors.map((item) => [item.source_system_id, item]));
  const postureBySource = new Map(posture.sources.map((item) => [item.source_system_id, item]));
  const sourceRows = [
    ...posture.sources.map((source) => ({
      sourceSystemID: source.source_system_id,
      displayName: source.source_display_name,
      executionTotal: source.execution_total,
      failureTotal: source.failure_total,
      executionsWithGaps: source.executions_with_gaps,
    })),
    ...visibleConnectors
      .filter((connector) => !postureBySource.has(connector.source_system_id))
      .map((connector) => ({
        sourceSystemID: connector.source_system_id,
        displayName: connector.source_display_name,
        executionTotal: 0,
        failureTotal: 0,
        executionsWithGaps: 0,
      })),
  ];
  const displayedRows = sourceRows.slice(0, 5);
  return (
    <article className="posture-card posture-source-card">
      <header>
        <div><span className="posture-card-label">Source trust</span><h2>来源与交付边界</h2></div>
        <button type="button" onClick={() => navigate("/connectors")}>查看详情</button>
      </header>
      <p className="posture-card-copy">执行统计属于固定快照；Connector trust 是当前运行回执，两者不会混成一个健康分。</p>
      {connectorError ? <p className="quiet-copy">Connector 状态暂不可用，未显示为健康。</p> : null}
      <div className="posture-source-list">
        {displayedRows.map((source) => {
          const connector = connectorBySource.get(source.sourceSystemID);
          const trust = connector?.trust_state ?? "not_observed";
          return (
            <div key={source.sourceSystemID}>
              <span><i className={`posture-source-dot source-${trust}`} aria-hidden="true" />{source.displayName}</span>
              <span><strong>{formatCount(source.executionTotal)} exec</strong><small>{source.failureTotal} 失败 · {source.executionsWithGaps} 有 Gap</small></span>
              <em>{trustLabel(trust)}</em>
            </div>
          );
        })}
        {sourceRows.length === 0 ? <p className="quiet-copy">当前窗口没有可归属的执行来源，也没有已配置 Connector。</p> : null}
        {sourceRows.length > displayedRows.length ? (
          <button type="button" className="posture-source-more" onClick={() => navigate("/connectors")}>还有 {sourceRows.length - displayedRows.length} 个来源 →</button>
        ) : null}
      </div>
    </article>
  );
}

function RuntimeJourney({
  journey,
  loading,
  error,
  executionID,
  asOf,
  navigate,
}: {
  journey?: TraceJourney;
  loading: boolean;
  error?: unknown;
  executionID: string;
  asOf: string;
  navigate: (path: string) => void;
}) {
  const path = journey ? declaredJourneyPath(journey) : [];
  return (
    <section className="posture-runtime" aria-labelledby="runtime-title">
      <header className="posture-section-header">
        <div>
          <span className="posture-card-label">Source-declared relationships</span>
          <h2 id="runtime-title">AI Runtime Path</h2>
          <p>展示当前优先项关联 execution 的最长已声明父子链；时间接近本身不会产生连线。</p>
        </div>
        {journey?.available ? (
          <div className="posture-map-legend">
            <span><i className="legend-declared" />声明父子关系</span>
            <span><i className="legend-unresolved" />未解析关系</span>
          </div>
        ) : null}
      </header>
      <div className="posture-runtime-canvas">
        {loading ? <LoadingBlock label="正在读取声明路径" /> : null}
        {error ? <ErrorState error={error} /> : null}
        {!loading && !error && !executionID ? (
          <EmptyState title="没有可锚定的 execution" description="当前 Alert 未关联 execution，且有限执行快照没有需要解释的记录。" />
        ) : null}
        {!loading && journey && !journey.available ? (
          <EmptyState title="来源没有提供可解析 Trace 身份" description={`未生成路径：${journey.unavailable_reason ?? "unknown reason"}。没有用 chronology 猜测因果。`} />
        ) : null}
        {!loading && journey?.available ? (
          <>
            <div className={`posture-runtime-chain${path.length < 2 ? " runtime-chain-unlinked" : ""}`} role="list" aria-label="来源声明的执行路径">
              {(path.length > 0 ? path : journey.nodes.slice(0, 6)).map((node, index) => (
                <div className="posture-runtime-step" role="listitem" key={node.event_id}>
                  {index > 0 && path.length > 1 ? <span className="posture-runtime-edge" aria-hidden="true"><i /></span> : null}
                  <button
                    type="button"
                    onClick={() => navigate(`/executions/${encodeURIComponent(node.execution_id)}?as_of=${encodeURIComponent(asOf)}`)}
                  >
                    <small>{nodeRole(node)}</small>
                    <strong>{nodeLabel(node)}</strong>
                    <span>{node.outcome || node.action || node.source_type}</span>
                  </button>
                </div>
              ))}
            </div>
            <div className="posture-runtime-context">
              <span><strong>{journey.integrity.execution_count}</strong><small>关联 executions</small></span>
              <span><strong>{journey.integrity.resolved_parent_count}</strong><small>已解析 parent</small></span>
              <span className={journey.integrity.unresolved_parent_count > 0 ? "context-limited" : undefined}>
                <strong>{journey.integrity.unresolved_parent_count}</strong><small>未解析 parent</small>
              </span>
              <span><strong>{journey.integrity.node_count}</strong><small>Evidence nodes</small></span>
              <button type="button" onClick={() => navigate(`/executions/${encodeURIComponent(executionID)}?as_of=${encodeURIComponent(asOf)}`)}>打开完整调查 →</button>
            </div>
            <p className="posture-runtime-boundary">{journey.evidence_boundary}</p>
          </>
        ) : null}
      </div>
    </section>
  );
}

function RiskQueue({
  posture,
  executions,
  selectedAlertID,
  onSelect,
  navigate,
}: {
  posture: PostureSnapshot;
  executions?: ExecutionSearchResponse;
  selectedAlertID: string;
  onSelect: (id: string) => void;
  navigate: (path: string) => void;
}) {
  const attention = executions?.items.filter((item) => item.has_gaps || item.outcome !== "success").slice(0, 5) ?? [];
  return (
    <article className="posture-panel posture-risk-panel">
      <header className="posture-section-header compact">
        <div>
          <span className="posture-card-label">Investigate next</span>
          <h2>{posture.risk.items.length > 0 ? "优先调查" : attention.length > 0 ? "最近需要解释" : "调查入口"}</h2>
        </div>
        <button type="button" onClick={() => navigate("/discovery")}>查看 Discovery</button>
      </header>
      {posture.risk.items.length > 0 ? (
        <div className="posture-risk-list">
          {posture.risk.items.slice(0, 5).map((alert, index) => (
            <button
              type="button"
              key={alert.id}
              className={`posture-risk-row risk-${alert.severity}${selectedAlertID === alert.id ? " selected" : ""}`}
              onClick={() => onSelect(alert.id)}
            >
              <span className="posture-risk-rank">{String(index + 1).padStart(2, "0")}</span>
              <span className="posture-risk-copy">
                <span className={`posture-severity severity-${alert.severity}`}>{severityLabel(alert.severity)}</span>
                <strong>{alert.title}</strong>
                <small>{alert.execution_id ? `Execution ${shortRef(alert.execution_id)}` : "尚未关联 execution"}</small>
              </span>
              <span className="posture-risk-meta"><strong>{alert.state}</strong><small>{formatDateTime(alert.observed_at)}</small></span>
              <span aria-hidden="true">→</span>
            </button>
          ))}
        </div>
      ) : attention.length > 0 ? (
        <div className="posture-risk-list">
          {attention.map((execution, index) => (
            <button
              type="button"
              key={execution.execution_id}
              className={`posture-risk-row risk-${execution.outcome === "failure" ? "error" : "warning"}`}
              onClick={() => navigate(`/executions/${encodeURIComponent(execution.execution_id)}?as_of=${encodeURIComponent(posture.as_of)}`)}
            >
              <span className="posture-risk-rank">{String(index + 1).padStart(2, "0")}</span>
              <span className="posture-risk-copy"><OutcomeBadge outcome={execution.outcome} /><strong>{execution.request_id}</strong><small>{execution.source_display_name || execution.source_type}</small></span>
              <span className="posture-risk-meta"><strong>{execution.gap_count} Gaps</strong><small>{formatDateTime(execution.last_effective_at)}</small></span>
              <span aria-hidden="true">→</span>
            </button>
          ))}
        </div>
      ) : (
        <EmptyState title="当前窗口没有开放 Alert" description="这不是全局健康证明；请结合来源状态和覆盖口径阅读。" />
      )}
    </article>
  );
}

function TrendPanel({ posture, signal, onSignal }: { posture: PostureSnapshot; signal: TrendSignal; onSignal: (value: TrendSignal) => void }) {
  const values = posture.trend.items.map((item) => trendValue(item, signal));
  const maximum = Math.max(1, ...values);
  const total = values.reduce((sum, value) => sum + value, 0);
  return (
    <article className="posture-panel posture-trend-panel">
      <header className="posture-section-header compact">
        <div><span className="posture-card-label">Fixed-window series</span><h2>变化与影响</h2></div>
        <span className="posture-sample-note">{posture.trend.bucket_unit === "hour" ? "小时" : "日"}粒度 · 无推断基线</span>
      </header>
      <div className="posture-signal-tabs" role="tablist" aria-label="趋势信号">
        <TrendTab active={signal === "failures"} label="失败" onClick={() => onSignal("failures")} />
        <TrendTab active={signal === "gaps"} label="有 Gap 执行" onClick={() => onSignal("gaps")} />
        <TrendTab active={signal === "alerts"} label="开放 Alert" onClick={() => onSignal("alerts")} />
      </div>
      <div className="posture-trend-reading">
        <div><span>当前窗口合计</span><strong>{formatCount(total)}</strong><p>{trendExplanation(signal)}</p></div>
        <div className="posture-chart" role="img" aria-label={`${trendLabel(signal)}固定窗口柱状图`}>
          {posture.trend.items.length === 0 ? (
            <span className="posture-chart-empty">当前窗口没有合格 bucket；未绘制零基线。</span>
          ) : (
            <>
              <div className="posture-chart-bars">
                {posture.trend.items.map((item) => {
                  const value = trendValue(item, signal);
                  return <i key={item.bucket_start} style={{ height: `${value === 0 ? 2 : Math.max(8, value / maximum * 100)}%` }} title={`${formatDateTime(item.bucket_start)} · ${value}`} />;
                })}
              </div>
              <div className="posture-chart-axis"><span>{formatBucket(posture.trend.items[0]?.bucket_start)}</span><span>{formatBucket(posture.trend.items.at(-1)?.bucket_start)}</span></div>
            </>
          )}
        </div>
      </div>
      <p className="posture-chart-boundary">{posture.trend.boundary}</p>
    </article>
  );
}

function TrendTab({ active, label, onClick }: { active: boolean; label: string; onClick: () => void }) {
  return <button type="button" role="tab" aria-selected={active} onClick={onClick}>{label}</button>;
}

function SectionHeading({ eyebrow, title }: { eyebrow: string; title: string }) {
  return <header className="posture-section-heading"><span className="posture-card-label">{eyebrow}</span><h2>{title}</h2></header>;
}

function summarizePosture(posture: PostureSnapshot) {
  if (posture.risk.critical + posture.risk.error > 0) {
    return { tone: "critical", label: "需要立即审阅", description: "存在 critical / error Alert；每一项都保留来源、关联 execution 与证据边界。" };
  }
  if (posture.risk.open_total > 0) {
    return { tone: "attention", label: "需要审阅", description: "存在开放 Alert；它们是调查入口，不是自动执行或定罪结论。" };
  }
  if (posture.executions.failure + posture.executions.partial + posture.executions.with_gaps > 0) {
    return { tone: "attention", label: "需要解释", description: "没有开放 Alert，但当前窗口仍有失败、部分结果或 Evidence Gap。" };
  }
  if (posture.executions.total === 0) {
    return { tone: "unknown", label: "证据不足", description: "当前窗口没有可聚合 execution；不能把空数据解释为正常。" };
  }
  return { tone: "quiet", label: "当前未见开放 Alert", description: "只描述固定窗口与已观察来源，不是企业 AI 全局健康证明。" };
}

function openSelectedEvidence(
  alert: InvestigationAlert | undefined,
  fallbackExecutionID: string | undefined,
  asOf: string,
  navigate: (path: string) => void,
) {
  if (alert) {
    if (alert.execution_id) {
      navigate(`/executions/${encodeURIComponent(alert.execution_id)}?as_of=${encodeURIComponent(asOf)}`);
      return;
    }
    if (alert.case_id) {
      navigate(`/cases/${encodeURIComponent(alert.case_id)}`);
      return;
    }
    navigate("/discovery");
    return;
  }
  if (fallbackExecutionID) {
    navigate(`/executions/${encodeURIComponent(fallbackExecutionID)}?as_of=${encodeURIComponent(asOf)}`);
    return;
  }
  navigate("/discovery");
}

function declaredJourneyPath(journey: TraceJourney): TraceJourneyNode[] {
  if (!journey.available || journey.integrity.cycle_detected || journey.nodes.length === 0) return [];
  const nodes = new Map(journey.nodes.map((node) => [node.event_id, node]));
  const next = new Map<string, string[]>();
  const incoming = new Set<string>();
  for (const edge of journey.edges) {
    if (!nodes.has(edge.from_event_id) || !nodes.has(edge.to_event_id)) continue;
    next.set(edge.from_event_id, [...(next.get(edge.from_event_id) ?? []), edge.to_event_id]);
    incoming.add(edge.to_event_id);
  }
  const roots = journey.nodes.filter((node) => !incoming.has(node.event_id));
  let longest: string[] = [];
  function visit(eventID: string, path: string[], seen: Set<string>) {
    if (seen.has(eventID)) return;
    const current = [...path, eventID];
    if (current.length > longest.length) longest = current;
    const nextSeen = new Set(seen).add(eventID);
    for (const child of next.get(eventID) ?? []) visit(child, current, nextSeen);
  }
  for (const root of roots) visit(root.event_id, [], new Set());
  return longest.map((eventID) => nodes.get(eventID)).filter((node): node is TraceJourneyNode => node !== undefined);
}

function nodeLabel(node: TraceJourneyNode) {
  return node.service_name || node.actor_ref || node.provider || node.model || node.action || node.source_type;
}

function nodeRole(node: TraceJourneyNode) {
  if (node.actor_ref) return node.actor_type || "Identity";
  if (node.service_name) return "Service";
  if (node.provider) return "Provider";
  if (node.model) return "Model";
  return "Evidence node";
}

function coverageLabel(dimension: PostureCoverage["dimension"]) {
  return ({ temporal: "Direct time", identity: "Identity", outcome: "Outcome", causal: "Causal" })[dimension];
}

function trustLabel(value: string) {
  return ({
    reported_current: "当前",
    syncing: "同步中",
    degraded: "降级",
    inactive: "未启用",
    not_observed: "未观察",
  } as Record<string, string>)[value] ?? value;
}

function severityLabel(value: InvestigationAlert["severity"]) {
  return ({ critical: "严重", error: "错误", warning: "关注", info: "信息" })[value];
}

function trendValue(item: PostureTrendBucket, signal: TrendSignal) {
  if (signal === "failures") return item.failure_total;
  if (signal === "gaps") return item.executions_with_gaps;
  return item.open_alerts;
}

function trendLabel(signal: TrendSignal) {
  return signal === "failures" ? "失败执行" : signal === "gaps" ? "有 Gap 执行" : "开放 Alert";
}

function trendExplanation(signal: TrendSignal) {
  if (signal === "failures") return "确定性 outcome rollup 为 failure 的 executions。";
  if (signal === "gaps") return "至少关联一个一等 Capture Gap 的 executions。";
  return "窗口内 observed、在本次读取时尚未 resolved 的产品 Alert；不重建历史状态。";
}

function formatPercent(value: string | undefined) {
  if (value === undefined || Number.isNaN(Number(value))) return "N/A";
  return `${Math.round(Number(value) * 100)}%`;
}

function formatCount(value: number) {
  return new Intl.NumberFormat("zh-CN").format(value);
}

function formatBucket(value: string | undefined) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false }).format(date);
}

function savedViewSearch(view: SavedView) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(view.filters)) {
    if (typeof value === "string" || typeof value === "boolean") params.set(key, String(value));
  }
  return params.toString();
}
