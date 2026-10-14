import { getLocale, t, tx, useLocale } from "./i18n";
import { declaredPath } from "./declaredGraph";
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
  useLocale();
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
          <h1>{tx("m1404")}</h1>
          <p>{tx("m1405")}</p>
        </div>
        <div className="posture-scope" aria-label={t("m1406")}>
          <label>
            <span>{tx("m0473")}</span>
            <select value={range} onChange={(event) => setRange(event.target.value as RangeKey)}>
              <option value="24h">{tx("m0590")}</option>
              <option value="7d">{tx("m0591")}</option>
            </select>
          </label>
          <label>
            <span>{tx("m0472")}</span>
            <select value={sourceSystemID} onChange={(event) => setSourceSystemID(event.target.value)}>
              <option value="">{tx("m1407")}</option>
              {sources.map((source) => (
                <option key={source.source_system_id} value={source.source_system_id}>{source.display_name}</option>
              ))}
            </select>
          </label>
        </div>
      </header>

      {loading ? <LoadingBlock label={t("m1408")} /> : null}
      {!loading && Object.keys(partialErrors).length > 0 ? (
        <div className="degraded-note" role="status">{tx("m1409", { value0: Object.keys(partialErrors).join("、") })}</div>
      ) : null}
      {!loading && partialErrors.posture ? <ErrorState error={partialErrors.posture} /> : null}

      {posture ? (
        <>
          <p className="posture-snapshot-line">
            {t("m1410")}<code>{formatCanonicalTime(posture.as_of)}</code>
            <span>{formatDateTime(posture.from)} — {formatDateTime(posture.to)}</span>
          </p>

          <section className="posture-summary-grid" aria-label={t("m1411")}>
            <article className={`posture-hero posture-hero-${postureState?.tone}`}>
              <div className="posture-hero-status">
                <span><i aria-hidden="true" />{postureState?.label}</span>
                <small>{range === "24h" ? t("m1412") : t("m1413")}</small>
              </div>
              <div className="posture-hero-copy">
                <strong>{formatCount(posture.risk.open_total)}</strong>
                <div>
                  <h2>{tx("m1414")}</h2>
                  <p>{postureState?.description}</p>
                </div>
              </div>
              <dl className="posture-hero-metrics">
                <div><dt>{tx("m1415")}</dt><dd>{formatCount(posture.risk.critical + posture.risk.error)}</dd></div>
                <div><dt>{tx("m1416")}</dt><dd>{formatCount(posture.risk.affected_executions)}</dd></div>
                <div><dt>{tx("m1417")}</dt><dd>{formatCount(posture.executions.with_gaps)}</dd></div>
              </dl>
              <button
                type="button"
                className="posture-hero-action"
                disabled={!selectedAlert && attentionExecutions.length === 0}
                onClick={() => openSelectedEvidence(selectedAlert, selectedAlert ? undefined : attentionExecutions[0]?.execution_id, posture.as_of, navigate)}
              >
                {t("m1418")}<span aria-hidden="true">→</span>
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

          <section className="posture-work-grid" aria-label={t("m1419")}>
            <article className="posture-panel">
              <SectionHeading eyebrow="Investigation workspace" title={t("m1420")} />
              {partialErrors.cases ? (
                <p className="quiet-copy">{tx("m1421")}</p>
              ) : openCases.length === 0 ? (
                <p className="quiet-copy">{tx("m1422")}</p>
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
              <button type="button" className="posture-text-link" onClick={() => navigate("/cases")}>{tx("m1423")}</button>
            </article>

            <article className="posture-panel">
              <SectionHeading eyebrow="Saved entry points" title={t("m1424")} />
              {partialErrors.views ? (
                <p className="quiet-copy">{tx("m1425")}</p>
              ) : views.length === 0 ? (
                <p className="quiet-copy">{tx("m1426")}</p>
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
              <button type="button" className="posture-text-link" onClick={() => navigate("/executions")}>{tx("m1427")}</button>
            </article>

            <article className="posture-panel posture-boundary-panel">
              <SectionHeading eyebrow="Evidence contract" title={t("m1428")} />
              <ul>
                <li>{tx("m1429")}</li>
                <li>{tx("m1430")}</li>
                <li>{tx("m1431")}</li>
                <li>{tx("m1432")}</li>
                <li>{tx("m1433")}</li>
              </ul>
              <span className="posture-role">{tx("m1434", { value0: session.session.role })}</span>
            </article>
          </section>
        </>
      ) : null}
    </div>
  );
}

function CoverageCard({ coverage }: { coverage: PostureCoverage[] }) {
  useLocale();
  const available = coverage.filter((item) => item.available && item.ratio_decimal !== undefined);
  const weakest = [...available].sort((left, right) => Number(left.ratio_decimal) - Number(right.ratio_decimal))[0];
  return (
    <article className="posture-card posture-coverage-card">
      <header>
        <div><span className="posture-card-label">Evidence posture</span><h2>{tx("m1435")}</h2></div>
        <strong>{weakest ? t("m1436", { value0: formatPercent(weakest.ratio_decimal) }) : t("m1437")}</strong>
      </header>
      <p className="posture-card-copy">{tx("m1438")}</p>
      <div className="posture-axis-list">
        {coverage.map((axis) => {
          const width = axis.available ? Math.max(0, Math.min(100, Number(axis.ratio_decimal) * 100)) : 0;
          return (
            <div key={axis.dimension} className={!axis.available || width < 80 ? "axis-limited" : undefined} title={`${axis.basis}。${axis.limitation}`}>
              <span>{coverageLabel(axis.dimension)}</span>
              <i aria-hidden="true"><b style={{ width: `${width}%` }} /></i>
              <strong>{axis.available ? formatPercent(axis.ratio_decimal) : "N/A"}</strong>
              <small>{axis.available ? `${axis.numerator} / ${axis.denominator}` : t("m1439")}</small>
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
  useLocale();
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
        <div><span className="posture-card-label">Source trust</span><h2>{tx("m1440")}</h2></div>
        <button type="button" onClick={() => navigate("/connectors")}>{tx("m1441")}</button>
      </header>
      <p className="posture-card-copy">{tx("m1442")}</p>
      {connectorError ? <p className="quiet-copy">{tx("m1443")}</p> : null}
      <div className="posture-source-list">
        {displayedRows.map((source) => {
          const connector = connectorBySource.get(source.sourceSystemID);
          const trust = connector?.trust_state ?? "not_observed";
          return (
            <div key={source.sourceSystemID}>
              <span><i className={`posture-source-dot source-${trust}`} aria-hidden="true" />{source.displayName}</span>
              <span><strong>{formatCount(source.executionTotal)} exec</strong><small>{tx("m1444", { value0: source.failureTotal, value1: source.executionsWithGaps })}</small></span>
              <em>{trustLabel(trust)}</em>
            </div>
          );
        })}
        {sourceRows.length === 0 ? <p className="quiet-copy">{tx("m1445")}</p> : null}
        {sourceRows.length > displayedRows.length ? (
          <button type="button" className="posture-source-more" onClick={() => navigate("/connectors")}>{tx("m1446", { value0: sourceRows.length - displayedRows.length })}</button>
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
  useLocale();
  const path = journey ? declaredJourneyPath(journey) : [];
  return (
    <section className="posture-runtime" aria-labelledby="runtime-title">
      <header className="posture-section-header">
        <div>
          <span className="posture-card-label">Source-declared relationships</span>
          <h2 id="runtime-title">AI Runtime Path</h2>
          <p>{tx("m1447")}</p>
        </div>
        {journey?.available ? (
          <div className="posture-map-legend">
            <span><i className="legend-declared" />{t("m1448")}</span>
            <span><i className="legend-unresolved" />{t("m1449")}</span>
          </div>
        ) : null}
      </header>
      <div className="posture-runtime-canvas">
        {loading ? <LoadingBlock label={t("m1450")} /> : null}
        {error ? <ErrorState error={error} /> : null}
        {!loading && !error && !executionID ? (
          <EmptyState title={t("m1451")} description={t("m1452")} />
        ) : null}
        {!loading && journey && !journey.available ? (
          <EmptyState title={t("m1453")} description={t("m1454", { value0: journey.unavailable_reason ?? "unknown reason" })} />
        ) : null}
        {!loading && journey?.available ? (
          <>
            <div className={`posture-runtime-chain${path.length < 2 ? " runtime-chain-unlinked" : ""}`} role="list" aria-label={t("m1455")}>
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
              <span><strong>{journey.integrity.execution_count}</strong><small>{tx("m1456")}</small></span>
              <span><strong>{journey.integrity.resolved_parent_count}</strong><small>{tx("m1457")}</small></span>
              <span className={journey.integrity.unresolved_parent_count > 0 ? "context-limited" : undefined}>
                <strong>{journey.integrity.unresolved_parent_count}</strong><small>{tx("m1458")}</small>
              </span>
              <span><strong>{journey.integrity.node_count}</strong><small>Evidence nodes</small></span>
              <button type="button" onClick={() => navigate(`/executions/${encodeURIComponent(executionID)}?as_of=${encodeURIComponent(asOf)}`)}>{tx("m1459")}</button>
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
  useLocale();
  const attention = executions?.items.filter((item) => item.has_gaps || item.outcome !== "success").slice(0, 5) ?? [];
  return (
    <article className="posture-panel posture-risk-panel">
      <header className="posture-section-header compact">
        <div>
          <span className="posture-card-label">Investigate next</span>
          <h2>{posture.risk.items.length > 0 ? t("m1460") : attention.length > 0 ? t("m1461") : t("m1462")}</h2>
        </div>
        <button type="button" onClick={() => navigate("/discovery")}>{tx("m1463")}</button>
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
                <small>{alert.execution_id ? `Execution ${shortRef(alert.execution_id)}` : t("m1464")}</small>
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
        <EmptyState title={t("m1465")} description={t("m1466")} />
      )}
    </article>
  );
}

function TrendPanel({ posture, signal, onSignal }: { posture: PostureSnapshot; signal: TrendSignal; onSignal: (value: TrendSignal) => void }) {
  useLocale();
  const values = posture.trend.items.map((item) => trendValue(item, signal));
  const maximum = Math.max(1, ...values);
  const total = values.reduce((sum, value) => sum + value, 0);
  return (
    <article className="posture-panel posture-trend-panel">
      <header className="posture-section-header compact">
        <div><span className="posture-card-label">Fixed-window series</span><h2>{tx("m1467")}</h2></div>
        <span className="posture-sample-note">{tx("m1470", { value0: posture.trend.bucket_unit === "hour" ? t("m1468") : t("m1469") })}</span>
      </header>
      <div className="posture-signal-tabs" role="tablist" aria-label={t("m1471")}>
        <TrendTab active={signal === "failures"} label={t("m1088")} onClick={() => onSignal("failures")} />
        <TrendTab active={signal === "gaps"} label={t("m1417")} onClick={() => onSignal("gaps")} />
        <TrendTab active={signal === "alerts"} label={t("m1472")} onClick={() => onSignal("alerts")} />
      </div>
      <div className="posture-trend-reading">
        <div><span>{tx("m1473")}</span><strong>{formatCount(total)}</strong><p>{trendExplanation(signal)}</p></div>
        <div className="posture-chart" role="img" aria-label={t("m1474", { value0: trendLabel(signal) })}>
          {posture.trend.items.length === 0 ? (
            <span className="posture-chart-empty">{tx("m1475")}</span>
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
  useLocale();
  return <button type="button" role="tab" aria-selected={active} onClick={onClick}>{label}</button>;
}

function SectionHeading({ eyebrow, title }: { eyebrow: string; title: string }) {
  useLocale();
  return <header className="posture-section-heading"><span className="posture-card-label">{eyebrow}</span><h2>{title}</h2></header>;
}

function summarizePosture(posture: PostureSnapshot) {
  if (posture.risk.critical + posture.risk.error > 0) {
    return { tone: "critical", label: t("m1476"), description: t("m1477") };
  }
  if (posture.risk.open_total > 0) {
    return { tone: "attention", label: t("m1478"), description: t("m1479") };
  }
  if (posture.executions.failure + posture.executions.partial + posture.executions.with_gaps > 0) {
    return { tone: "attention", label: t("m1480"), description: t("m1481") };
  }
  if (posture.executions.total === 0) {
    return { tone: "unknown", label: t("m0832"), description: t("m1482") };
  }
  return { tone: "quiet", label: t("m1483"), description: t("m1484") };
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
  if (!journey.available || journey.integrity.cycle_detected) return [];
  return declaredPath(journey.nodes, journey.edges.map(e => ({ from: e.from_event_id, to: e.to_event_id })));
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
    reported_current: t("m1485"),
    syncing: t("m1486"),
    degraded: t("m1487"),
    inactive: t("m1488"),
    not_observed: t("m1489"),
  } as Record<string, string>)[value] ?? value;
}

function severityLabel(value: InvestigationAlert["severity"]) {
  return ({ critical: t("m0980"), error: t("m0979"), warning: t("m1490"), info: t("m0977") })[value];
}

function trendValue(item: PostureTrendBucket, signal: TrendSignal) {
  if (signal === "failures") return item.failure_total;
  if (signal === "gaps") return item.executions_with_gaps;
  return item.open_alerts;
}

function trendLabel(signal: TrendSignal) {
  return signal === "failures" ? t("m1491") : signal === "gaps" ? t("m1417") : t("m1472");
}

function trendExplanation(signal: TrendSignal) {
  if (signal === "failures") return t("m1492");
  if (signal === "gaps") return t("m1493");
  return t("m1494");
}

function formatPercent(value: string | undefined) {
  if (value === undefined || Number.isNaN(Number(value))) return "N/A";
  return `${Math.round(Number(value) * 100)}%`;
}

function formatCount(value: number) {
  return new Intl.NumberFormat(getLocale()).format(value);
}

function formatBucket(value: string | undefined) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(getLocale(), { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false }).format(date);
}

function savedViewSearch(view: SavedView) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(view.filters)) {
    if (typeof value === "string" || typeof value === "boolean") params.set(key, String(value));
  }
  return params.toString();
}
