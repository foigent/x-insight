import { useEffect, useRef, useState } from "react";

import {
  investigationAPI,
  type AccessSessionResponse,
  type CaptureGap,
  type EvidenceEventDetail,
  type ExecutionDetail,
  type FieldProvenance,
  type GapResponse,
  type InvestigationProjection,
  type TraceJourney,
  type TimelineNode,
  type TimelineResponse,
} from "./api";
import { EmptyState, ErrorState, LoadingBlock } from "./Feedback";
import { InvestigationView } from "./InvestigationView";
import {
  detailFieldLabels,
  displayValue,
  eventTitle,
  formatCanonicalTime,
  formatDateTime,
  formatMicroCNY,
  formatShortTime,
  shortRef,
} from "./format";
import {
  AssuranceBadge,
  fieldGroupLabels,
  FreshnessBadge,
  OutcomeBadge,
  ProofBadge,
  proofExplanation,
  provenanceStates,
  SemanticBadge,
} from "./status";

export function ExecutionDetailPage({
  executionID,
  asOf,
  navigate,
  session,
}: {
  executionID: string;
  asOf?: string;
  navigate: (path: string) => void;
  session: AccessSessionResponse;
}) {
  const requestedSnapshot = useRef<string | undefined>(asOf);
  const detailPanelRef = useRef<HTMLElement>(null);
  const [revision, setRevision] = useState(0);
  const [detail, setDetail] = useState<ExecutionDetail>();
  const [projection, setProjection] = useState<InvestigationProjection>();
  const [traceJourney, setTraceJourney] = useState<TraceJourney>();
  const [timeline, setTimeline] = useState<TimelineResponse>();
  const [gaps, setGaps] = useState<GapResponse>();
  const [activeView, setActiveView] = useState<"story" | "graph" | "chronology" | "proof">("story");
  const [coreLoading, setCoreLoading] = useState(true);
  const [coreError, setCoreError] = useState<unknown>();
  const [projectionError, setProjectionError] = useState<unknown>();
  const [traceJourneyError, setTraceJourneyError] = useState<unknown>();
  const [timelineError, setTimelineError] = useState<unknown>();
  const [gapsError, setGapsError] = useState<unknown>();
  const [timelineLoadingMore, setTimelineLoadingMore] = useState(false);
  const [gapsLoadingMore, setGapsLoadingMore] = useState(false);
  const [selectedEventID, setSelectedEventID] = useState<string>();
  const [selectedEvent, setSelectedEvent] = useState<EvidenceEventDetail>();
  const [eventLoading, setEventLoading] = useState(false);
  const [eventError, setEventError] = useState<unknown>();

  useEffect(() => {
    const controller = new AbortController();
    setCoreLoading(true);
    setCoreError(undefined);
    setProjectionError(undefined);
    setTraceJourneyError(undefined);
    setTimelineError(undefined);
    setGapsError(undefined);
    setDetail(undefined);
    setProjection(undefined);
    setTraceJourney(undefined);
    setTimeline(undefined);
    setGaps(undefined);
    setSelectedEventID(undefined);
    setSelectedEvent(undefined);

    async function load() {
      try {
        const nextDetail = await investigationAPI.getExecution(
          executionID,
          requestedSnapshot.current,
          controller.signal,
        );
        if (controller.signal.aborted) {
          return;
        }
        requestedSnapshot.current = nextDetail.as_of;
        setDetail(nextDetail);
        const requestedEvidence = new URLSearchParams(window.location.search).get("evidence") || undefined;
        const canonicalParams = new URLSearchParams({ as_of: nextDetail.as_of });
        if (requestedEvidence) canonicalParams.set("evidence", requestedEvidence);
        const canonicalURL = `/executions/${executionID}?${canonicalParams.toString()}`;
        window.history.replaceState(null, "", canonicalURL);

        const [projectionResult, traceJourneyResult, timelineResult, gapResult] = await Promise.allSettled([
          investigationAPI.getInvestigation(executionID, nextDetail.as_of, controller.signal),
          investigationAPI.getTraceJourney(executionID, nextDetail.as_of, controller.signal),
          investigationAPI.listTimeline(executionID, nextDetail.as_of, undefined, controller.signal),
          investigationAPI.listGaps(executionID, nextDetail.as_of, undefined, controller.signal),
        ]);
        if (controller.signal.aborted) {
          return;
        }
        if (projectionResult.status === "fulfilled") {
          setProjection(projectionResult.value);
          const initialNode = projectionResult.value.nodes.find((node) => node.event_id === requestedEvidence)
            ?? projectionResult.value.nodes.find((node) => node.kind === "provider_failed")
            ?? projectionResult.value.nodes[0];
          setSelectedEventID(initialNode?.event_id);
        } else {
          setProjectionError(projectionResult.reason);
        }
        if (traceJourneyResult.status === "fulfilled") {
          setTraceJourney(traceJourneyResult.value);
        } else {
          setTraceJourneyError(traceJourneyResult.reason);
        }
        if (timelineResult.status === "fulfilled") {
          setTimeline(timelineResult.value);
          if (projectionResult.status !== "fulfilled") {
            setSelectedEventID(timelineResult.value.items[0]?.event_id);
          }
        } else {
          setTimelineError(timelineResult.reason);
        }
        if (gapResult.status === "fulfilled") {
          setGaps(gapResult.value);
        } else {
          setGapsError(gapResult.reason);
        }
      } catch (caught: unknown) {
        if (!controller.signal.aborted) {
          setCoreError(caught);
        }
      } finally {
        if (!controller.signal.aborted) {
          setCoreLoading(false);
        }
      }
    }

    void load();
    return () => controller.abort();
  }, [executionID, revision]);

  useEffect(() => {
    if (!selectedEventID || !detail) {
      return;
    }
    const controller = new AbortController();
    setEventLoading(true);
    setEventError(undefined);
    setSelectedEvent(undefined);
    investigationAPI
      .getEvent(executionID, selectedEventID, detail.as_of, controller.signal)
      .then((event) => setSelectedEvent(event))
      .catch((caught: unknown) => {
        if (!controller.signal.aborted) {
          setEventError(caught);
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setEventLoading(false);
        }
      });
    return () => controller.abort();
  }, [detail, executionID, selectedEventID]);

  function retryCurrentSnapshot() {
    setRevision((current) => current + 1);
  }

  function loadLatestSnapshot() {
    requestedSnapshot.current = undefined;
    setRevision((current) => current + 1);
  }

  async function loadMoreTimeline() {
    const cursor = timeline?.page.next_cursor;
    if (!cursor || !detail) {
      return;
    }
    setTimelineLoadingMore(true);
    setTimelineError(undefined);
    try {
      const next = await investigationAPI.listTimeline(executionID, detail.as_of, cursor);
      setTimeline((current) =>
        current
          ? { ...next, items: mergeByID(current.items, next.items, (item) => item.event_id) }
          : next,
      );
    } catch (caught: unknown) {
      setTimelineError(caught);
    } finally {
      setTimelineLoadingMore(false);
    }
  }

  async function loadMoreGaps() {
    const cursor = gaps?.page.next_cursor;
    if (!cursor || !detail) {
      return;
    }
    setGapsLoadingMore(true);
    setGapsError(undefined);
    try {
      const next = await investigationAPI.listGaps(executionID, detail.as_of, cursor);
      setGaps((current) =>
        current ? { ...next, items: mergeByID(current.items, next.items, (item) => item.gap_id) } : next,
      );
    } catch (caught: unknown) {
      setGapsError(caught);
    } finally {
      setGapsLoadingMore(false);
    }
  }

  function selectGapEvent(gap: CaptureGap) {
    if (!gap.evidence_event_id) {
      return;
    }
    selectEvent(gap.evidence_event_id);
    window.requestAnimationFrame(() => detailPanelRef.current?.scrollIntoView?.({ behavior: "smooth", block: "start" }));
  }

  function selectEvent(eventID: string) {
    setSelectedEventID(eventID);
    if (!detail) return;
    const params = new URLSearchParams({ as_of: detail.as_of, evidence: eventID });
    window.history.replaceState(null, "", `/executions/${executionID}?${params.toString()}`);
  }

  return (
    <>
      <header className="topbar detail-topbar">
        <div>
          <button type="button" className="back-link" onClick={() => navigate("/executions")}>
            ← 返回 execution 搜索
          </button>
          <span className="eyebrow">Trace Investigation · fixed Evidence snapshot</span>
          <h1>执行路径调查</h1>
        </div>
        <div className="topbar-actions">
          <button type="button" className="button button-secondary" onClick={retryCurrentSnapshot} disabled={coreLoading}>
            重读当前快照
          </button>
          <button type="button" className="button button-primary" onClick={loadLatestSnapshot} disabled={coreLoading}>
            读取最新快照
          </button>
        </div>
      </header>

      {coreLoading && <LoadingBlock label="正在固定 execution 快照" />}
      {Boolean(coreError) && <ErrorState error={coreError} onRetry={retryCurrentSnapshot} />}

      {detail && (
        <>
          <section className={`execution-hero hero-${detail.summary.freshness.sync_state}`} aria-labelledby="execution-request-id">
            <div className="execution-hero-main">
              <span className="eyebrow">Execution ref</span>
              <h2 id="execution-request-id">{detail.summary.request_id}</h2>
              <code title={detail.summary.execution_id}>{detail.summary.execution_id}</code>
              <div className="hero-badge-row">
                <OutcomeBadge outcome={detail.summary.outcome} />
                <ProofBadge proofClass={detail.summary.proof_class} />
                {detail.summary.assurance_mode !== detail.summary.proof_class ? (
                  <AssuranceBadge assuranceMode={detail.summary.assurance_mode} />
                ) : null}
                <FreshnessBadge state={detail.summary.freshness.sync_state} />
              </div>
              <p className="proof-explanation">{proofExplanation(detail.summary.proof_class)}</p>
            </div>
            <dl className="execution-facts">
              <div>
                <dt>来源</dt>
                <dd>{detail.summary.source_display_name || detail.summary.source_type}</dd>
                <small>{detail.summary.source_type}</small>
              </div>
              <div>
                <dt>时间范围</dt>
                <dd>{formatDateTime(detail.summary.first_effective_at)}</dd>
                <small>至 {formatDateTime(detail.summary.last_effective_at)}</small>
              </div>
              <div>
                <dt>Evidence events</dt>
                <dd>{detail.summary.event_count}</dd>
                <small>来自 persisted evidence</small>
              </div>
              <div>
                <dt>Capture gaps</dt>
                <dd className={detail.summary.gap_count > 0 ? "danger-text" : undefined}>{detail.summary.gap_count}</dd>
                <small>{detail.summary.gap_count > 0 ? "必须调查" : "当前未记录"}</small>
              </div>
              <div>
                <dt>Payload</dt>
                <dd>{detail.summary.payload_capture_modes?.join(" · ") || "unknown"}</dd>
                <small>不提供 raw 内容查看器</small>
              </div>
              <div>
                <dt>as_of</dt>
                <dd>{formatDateTime(detail.as_of)}</dd>
                <small><code>{formatCanonicalTime(detail.as_of)}</code></small>
              </div>
            </dl>
          </section>

          <ViewTabs active={activeView} onChange={setActiveView} />

          {(activeView === "story" || activeView === "graph") && (
            projection ? (
              <InvestigationView
                mode={activeView}
                projection={projection}
                traceJourney={traceJourney}
                traceJourneyError={traceJourneyError}
                selectedEventID={selectedEventID}
                selectedEvent={selectedEvent}
                eventLoading={eventLoading}
                eventError={eventError}
                onSelect={selectEvent}
                session={session}
                executionID={executionID}
                asOf={detail.as_of}
                navigate={navigate}
              />
            ) : projectionError ? (
              <ErrorState error={projectionError} onRetry={retryCurrentSnapshot} />
            ) : (
              <LoadingBlock label="正在构建确定性 Investigation 投影" />
            )
          )}

          {activeView === "chronology" && (
            <div className="investigation-layout">
            <section className="timeline-panel" aria-labelledby="timeline-title">
              <div className="section-heading">
                <div>
                  <span className="eyebrow">Evidence-derived chronology</span>
                  <h2 id="timeline-title">证据时间线</h2>
                </div>
                <p>顺序表示 chronology；只有来源明确声明的 parent 才显示因果引用。</p>
              </div>

              {Boolean(timelineError) && <ErrorState error={timelineError} compact onRetry={retryCurrentSnapshot} />}
              {!timeline && !timelineError && <LoadingBlock label="正在读取 timeline" />}
              {timeline && timeline.items.length === 0 && (
                <EmptyState title="没有可显示的 evidence event" description="当前 execution 快照存在，但未返回时间线节点。" />
              )}
              {timeline && timeline.items.length > 0 && (
                <ol className="timeline-list" aria-label="按时间升序排列的 evidence events">
                  {timeline.items.map((node, index) => (
                    <TimelineItem
                      key={node.event_id}
                      node={node}
                      index={index}
                      selected={selectedEventID === node.event_id}
                      onSelect={() => selectEvent(node.event_id)}
                    />
                  ))}
                </ol>
              )}
              {timeline && (
                <footer className="collection-footer">
                  <span>
                    已加载 {timeline.items.length} / {timeline.total_event_count} 个事件
                  </span>
                  {timeline.page.has_more && (
                    <button
                      type="button"
                      className="button button-secondary"
                      disabled={timelineLoadingMore}
                      onClick={() => void loadMoreTimeline()}
                    >
                      {timelineLoadingMore ? "正在加载…" : "加载更多时间线"}
                    </button>
                  )}
                </footer>
              )}
            </section>

            <div className="detail-column">
              <section ref={detailPanelRef} className="node-detail-panel" aria-labelledby="node-detail-title">
                <div className="section-heading">
                  <div>
                    <span className="eyebrow">Selected evidence event</span>
                    <h2 id="node-detail-title">节点明细</h2>
                  </div>
                  {selectedEventID && <code title={selectedEventID}>{shortRef(selectedEventID, 8, 6)}</code>}
                </div>
                {!selectedEventID && (
                  <EmptyState title="请选择时间线节点" description="节点明细只展示 allowlisted metadata 和来源证明。" />
                )}
                {eventLoading && <LoadingBlock label="正在读取节点明细" />}
                {Boolean(eventError) && <ErrorState error={eventError} compact />}
                {selectedEvent && !eventLoading && <EventDetail event={selectedEvent} />}
              </section>

              <section className="gap-panel" aria-labelledby="gap-panel-title">
                <div className="section-heading">
                  <div>
                    <span className="eyebrow">Capture-gap inspection</span>
                    <h2 id="gap-panel-title">证据缺口</h2>
                  </div>
                  {gaps && <strong className={gaps.total_gap_count > 0 ? "gap-total" : "muted-count"}>{gaps.total_gap_count}</strong>}
                </div>
                {Boolean(gapsError) && <ErrorState error={gapsError} compact onRetry={retryCurrentSnapshot} />}
                {!gaps && !gapsError && <LoadingBlock label="正在读取 capture gaps" />}
                {gaps && gaps.items.length === 0 && (
                  <EmptyState
                    title="当前快照未记录具体缺口"
                    description={
                      detail.summary.freshness.timeline_complete
                        ? "导入记录声明 timeline_complete；这仍只说明当前来源与导入边界。"
                        : "时间线仍未标记完整，系统不会把零条 gap 解释为完整。"
                    }
                  />
                )}
                {gaps && gaps.items.length > 0 && (
                  <div className="gap-list">
                    {gaps.items.map((gap) => (
                      <GapCard key={gap.gap_id} gap={gap} onSelect={() => selectGapEvent(gap)} />
                    ))}
                  </div>
                )}
                {gaps && (
                  <footer className="collection-footer">
                    <span>已加载 {gaps.items.length} / {gaps.total_gap_count} 个缺口</span>
                    {gaps.page.has_more && (
                      <button
                        type="button"
                        className="button button-secondary"
                        disabled={gapsLoadingMore}
                        onClick={() => void loadMoreGaps()}
                      >
                        {gapsLoadingMore ? "正在加载…" : "加载更多缺口"}
                      </button>
                    )}
                  </footer>
                )}
              </section>
            </div>
            </div>
          )}

          {activeView === "proof" && (
            <>
              <FreshnessPanel detail={detail} />
              <div className="proof-layout">
                <section ref={detailPanelRef} className="node-detail-panel" aria-labelledby="proof-node-title">
                  <div className="section-heading">
                    <div>
                      <span className="eyebrow">Allowlisted source proof</span>
                      <h2 id="proof-node-title">Evidence 证明</h2>
                    </div>
                    {selectedEventID && <code title={selectedEventID}>{shortRef(selectedEventID, 8, 6)}</code>}
                  </div>
                  {!selectedEventID && (
                    <EmptyState title="请选择 Evidence node" description="先在 Investigation 或 Chronology 中选择节点。" />
                  )}
                  {eventLoading && <LoadingBlock label="正在读取 Evidence 证明" />}
                  {Boolean(eventError) && <ErrorState error={eventError} compact />}
                  {selectedEvent && !eventLoading && <EventDetail event={selectedEvent} />}
                </section>
                <section className="gap-panel" aria-labelledby="proof-gap-title">
                  <div className="section-heading">
                    <div>
                      <span className="eyebrow">Explicit unknowns</span>
                      <h2 id="proof-gap-title">Capture Gaps</h2>
                    </div>
                    {gaps && <strong className={gaps.total_gap_count > 0 ? "gap-total" : "muted-count"}>{gaps.total_gap_count}</strong>}
                  </div>
                  {Boolean(gapsError) && <ErrorState error={gapsError} compact onRetry={retryCurrentSnapshot} />}
                  {!gaps && !gapsError && <LoadingBlock label="正在读取 capture gaps" />}
                  {gaps && gaps.items.length === 0 && (
                    <EmptyState
                      title="当前快照未记录具体缺口"
                      description="零条 gap 不会被单独解释为全局完整。"
                    />
                  )}
                  {gaps && gaps.items.length > 0 && (
                    <div className="gap-list">
                      {gaps.items.map((gap) => (
                        <GapCard key={gap.gap_id} gap={gap} onSelect={() => selectGapEvent(gap)} />
                      ))}
                    </div>
                  )}
                </section>
              </div>
            </>
          )}
        </>
      )}
    </>
  );
}

function ViewTabs({
  active,
  onChange,
}: {
  active: "story" | "graph" | "chronology" | "proof";
  onChange: (view: "story" | "graph" | "chronology" | "proof") => void;
}) {
  const tabs = [
    ["story", "Story", "结果、路径与未知"],
    ["graph", "Causal graph", "仅显式因果边"],
    ["chronology", "Chronology", "原始时间顺序"],
    ["proof", "Proof", "来源、哈希与缺口"],
  ] as const;
  return (
    <nav className="execution-view-tabs" aria-label="Execution detail views">
      {tabs.map(([view, label, description]) => (
        <button
          key={view}
          type="button"
          className={active === view ? "execution-view-tab-active" : undefined}
          aria-current={active === view ? "page" : undefined}
          onClick={() => onChange(view)}
        >
          <strong>{label}</strong>
          <small>{description}</small>
        </button>
      ))}
    </nav>
  );
}

function TimelineItem({
  node,
  index,
  selected,
  onSelect,
}: {
  node: TimelineNode;
  index: number;
  selected: boolean;
  onSelect: () => void;
}) {
  const states = provenanceStates(node.field_provenance, node.stage);
  return (
    <li className={`timeline-item severity-${node.severity}${selected ? " timeline-item-selected" : ""}`}>
      <span className="timeline-index" aria-hidden="true">{String(index + 1).padStart(2, "0")}</span>
      <button type="button" onClick={onSelect} aria-pressed={selected}>
        <div className="timeline-item-heading">
          <div>
            <time dateTime={node.effective_at} title={formatCanonicalTime(node.effective_at)}>
              {formatShortTime(node.effective_at)}
            </time>
            <strong>{eventTitle(node)}</strong>
          </div>
          <code>{node.stage}</code>
        </div>
        <div className="timeline-node-meta">
          <span>{node.event_type}</span>
          {node.outcome_status && <OutcomeBadge outcome={node.outcome_status} />}
          {node.decision_result && <span>decision: {node.decision_result}</span>}
        </div>
        <div className="badge-row">
          {states.map((state) => <SemanticBadge key={state} state={state} compact />)}
        </div>
        {node.declared_parent_resolved !== undefined && (
          <div className={`declared-parent${node.declared_parent_resolved ? "" : " declared-parent-unresolved"}`}>
            <span>声明父节点</span>
            {node.declared_parent_resolved && node.declared_parent_event_id ? (
              <code title={node.declared_parent_event_id}>{shortRef(node.declared_parent_event_id, 8, 6)}</code>
            ) : (
              <strong>未解析；不推断因果</strong>
            )}
          </div>
        )}
      </button>
    </li>
  );
}

function FreshnessPanel({ detail }: { detail: ExecutionDetail }) {
  const freshness = detail.summary.freshness;
  return (
    <section className="freshness-panel" aria-labelledby="freshness-panel-title">
      <div className="freshness-summary">
        <div>
          <span className="eyebrow">Revision & watermarks</span>
          <h2 id="freshness-panel-title">新鲜度与完整性</h2>
        </div>
        <FreshnessBadge state={freshness.sync_state} />
        <p>
          {freshness.timeline_complete
            ? "当前导入记录声明时间线完整。"
            : "当前时间线未声明完整；已显示内容仍可调查，但不可补全缺失事实。"}
        </p>
      </div>
      <dl className="watermark-grid">
        <Fact label="集成模式" value={freshness.integration_mode} canonical />
        <Fact label="来源生成" value={formatCanonicalTime(freshness.source_generated_at)} canonical />
        <Fact label="导入时间" value={formatCanonicalTime(freshness.imported_at)} canonical />
        <Fact label="搜索可见" value={formatCanonicalTime(freshness.search_visible_at)} canonical />
        <Fact label="来源修订" value={freshness.source_revision || "未提供"} canonical />
        <Fact label="导入修订" value={freshness.imported_revision || "未提供"} canonical />
        <Fact label="来源水位" value={freshness.source_watermark || "未提供"} canonical />
        <Fact label="导入水位" value={freshness.import_watermark || "未提供"} canonical />
      </dl>
    </section>
  );
}

function EventDetail({ event }: { event: EvidenceEventDetail }) {
  const states = provenanceStates(event.field_provenance, event.stage);
  const groups = [
    ["关联标识", event.correlation],
    ["身份", event.identity],
    ["能力与模型", event.capability],
    ["决策", event.decision],
    ["用量", event.usage],
    ["结果", event.outcome],
  ] as const;
  return (
    <div className="event-detail">
      <div className="event-detail-header">
        <div>
          <strong>{eventTitle(event)}</strong>
          <code>{event.event_type}</code>
        </div>
        <span className={`severity-chip severity-${event.severity}`}>{event.severity}</span>
      </div>
      <div className="badge-row">
        {states.map((state) => <SemanticBadge key={state} state={state} />)}
      </div>

      <section className="detail-section">
        <h3>字段组来源</h3>
        <dl className="provenance-list">
          {(Object.keys(fieldGroupLabels) as (keyof FieldProvenance)[]).map((field) => (
            <div key={field}>
              <dt>{fieldGroupLabels[field]}</dt>
              <dd><SemanticBadge state={event.field_provenance[field] || "unknown"} compact /></dd>
            </div>
          ))}
        </dl>
      </section>

      <section className="detail-section">
        <h3>关键事实</h3>
        <div className="fact-group-list">
          {groups.map(([title, group]) => <FactGroup key={title} title={title} values={group} />)}
        </div>
      </section>

      <section className="detail-section payload-section">
        <div>
          <h3>载荷策略</h3>
          <p>不显示空白 prompt/response 区域；本页没有 raw payload 请求。</p>
        </div>
        <dl className="compact-facts">
          <Fact label="事件捕获模式" value={event.capture_mode} canonical />
          <Fact
            label="Payload 状态"
            value={event.payload_policy?.payload_capture_mode || event.payload_capture_mode || "unknown"}
            canonical
          />
          <Fact label="脱敏状态" value={event.payload_policy?.redaction_state || "未提供"} canonical />
          <Fact label="Payload 引用" value={event.payload_policy?.payload_ref || "未提供"} canonical />
          <Fact label="保留策略" value={event.payload_policy?.retention_policy_ref || "未提供"} canonical />
        </dl>
      </section>

      <section className="detail-section source-section">
        <div>
          <h3>来源与完整性</h3>
          <span className="fidelity-badge">{event.source.fidelity_level}</span>
        </div>
        <dl className="source-facts">
          <Fact label="Source system" value={event.source.source_system_id} canonical />
          <Fact label="Adapter manifest" value={event.source.adapter_manifest_id} canonical />
          <Fact label="Schema" value={event.source.schema_version} canonical />
          <Fact label="Source type" value={event.source.source_type} canonical />
          <Fact label="Source version" value={event.source.source_version || "未提供"} canonical />
          <Fact label="Source event ID" value={event.source.source_event_id || "未提供"} canonical />
          <Fact label="Source event type" value={event.source.source_event_type || "未提供"} canonical />
          <Fact label="Source ref" value={event.source.source_ref} canonical />
          <Fact label="Event hash" value={event.source.event_hash} canonical />
          <Fact label="Source event hash" value={event.source.source_event_hash || "未提供"} canonical />
        </dl>
        <p className="allowlist-note">仅展示 versioned API allowlist；<code>raw_source_excerpt</code> 不在响应 DTO 中。</p>
      </section>
    </div>
  );
}

function FactGroup({ title, values }: { title: string; values: object | undefined }) {
  const entries = values
    ? Object.entries(values as Record<string, unknown>).filter(([, value]) => value !== undefined && value !== null && value !== "")
    : [];
  if (entries.length === 0) {
    return (
      <article className="fact-group fact-group-empty">
        <h4>{title}</h4>
        <p>未提供；请结合 provenance 或 capture gap 判断。</p>
      </article>
    );
  }
  return (
    <article className="fact-group">
      <h4>{title}</h4>
      <dl>
        {entries.map(([key, value]) => (
          <Fact
            key={key}
            label={detailFieldLabels[key] || key}
            value={key === "cost_micros" && typeof value === "string" ? formatMicroCNY(value) : displayValue(value)}
            canonical
          />
        ))}
      </dl>
    </article>
  );
}

function GapCard({ gap, onSelect }: { gap: CaptureGap; onSelect: () => void }) {
  return (
    <article className={`gap-card gap-severity-${gap.severity}`}>
      <div className="gap-card-heading">
        <div>
          <span>已确认缺口类型</span>
          <strong>{gap.gap_type}</strong>
        </div>
        <span className="severity-chip">{gap.severity}</span>
      </div>
      <dl>
        <Fact label="受影响声明" value={gap.affected_claim} />
        <Fact label="原因" value={gap.gap_reason} />
        <Fact label="建议" value={gap.recommendation || "暂无额外建议"} />
        <Fact label="记录时间" value={formatCanonicalTime(gap.created_at)} canonical />
      </dl>
      {gap.candidate_gap_types && gap.candidate_gap_types.length > 0 && (
        <div className="candidate-gaps">
          <span>候选原因（未确认）</span>
          <div>{gap.candidate_gap_types.map((candidate) => <code key={candidate}>{candidate}</code>)}</div>
        </div>
      )}
      {gap.evidence_event_id && (
        <button type="button" className="button button-quiet" onClick={onSelect}>
          查看关联 evidence event
        </button>
      )}
    </article>
  );
}

function Fact({ label, value, canonical = false }: { label: string; value: string | number; canonical?: boolean }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd className={canonical ? "canonical-value" : undefined} title={String(value)}>
        {canonical ? <code>{value}</code> : value}
      </dd>
    </div>
  );
}

function mergeByID<T>(current: T[], next: T[], key: (item: T) => string): T[] {
  const seen = new Set(current.map(key));
  return [...current, ...next.filter((item) => !seen.has(key(item)))];
}
