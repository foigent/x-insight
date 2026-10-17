import type { WorkbenchNavigation } from "./workbenchNavigation";
import { ViewTabs } from "./DetailViewTabs";
import { ExecutionRuntimePanel, type RuntimeDetailContext } from "./ExecutionRuntimePanel";
import type { RuntimeSnapshot } from "./RuntimePage";
import { PurposePanel } from "./PurposePanel";
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
import { readExecutionDetail, useExecutionLocation, writeExecutionDetail, type ExecutionView } from "./executionReading";
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
  navigate,
  session,
  returnTo,
  runtimeSnapshot,
  workbenchNavigation,
  navigationInvalid,
}: {
  executionID: string;
  navigate: (path: string) => void;
  session: AccessSessionResponse;
  returnTo?: { path: string; label: string };
  runtimeSnapshot?: RuntimeSnapshot | null;
  workbenchNavigation?: WorkbenchNavigation;
  navigationInvalid?: boolean;
}) {
  const location = useExecutionLocation();
  const reading = readExecutionDetail(location);
  const asOf = reading.asOf;
  const activeView = reading.view;
  const [runtimeContext, setRuntimeContext] = useState<RuntimeDetailContext | null>(() => runtimeSnapshot?.reading ? { reading: runtimeSnapshot.reading, deadline: runtimeSnapshot.deadline, identity: runtimeSnapshot.identity } : null);
  const loadedReading = useRef("");
  const pagingController = useRef<AbortController | null>(null);
  const detailPanelRef = useRef<HTMLElement>(null);
  const [revision, setRevision] = useState(0);
  const [detail, setDetail] = useState<ExecutionDetail>();
  const [projection, setProjection] = useState<InvestigationProjection>();
  const [traceJourney, setTraceJourney] = useState<TraceJourney>();
  const [timeline, setTimeline] = useState<TimelineResponse>();
  const [gaps, setGaps] = useState<GapResponse>();
  const [coreLoading, setCoreLoading] = useState(true);
  const [coreError, setCoreError] = useState<unknown>();
  const [projectionError, setProjectionError] = useState<unknown>();
  const [traceJourneyError, setTraceJourneyError] = useState<unknown>();
  const [timelineError, setTimelineError] = useState<unknown>();
  const [gapsError, setGapsError] = useState<unknown>();
  const [timelineLoadingMore, setTimelineLoadingMore] = useState(false);
  const [gapsLoadingMore, setGapsLoadingMore] = useState(false);
  const [defaultEventID, setDefaultEventID] = useState<string>();
  const selectedEventID = reading.evidence || defaultEventID;
  const [selectedEvent, setSelectedEvent] = useState<EvidenceEventDetail>();
  const [eventLoading, setEventLoading] = useState(false);
  const [eventError, setEventError] = useState<unknown>();
  const contextProjectionReady = Boolean(projection && detail && Date.parse(projection.as_of) === Date.parse(detail.as_of));
  const contextNodes = contextProjectionReady ? projection!.nodes.filter(node => node.kind !== "capture_gap") : [];
  const recordedActor = contextNodes.find(node => node.actor_ref || node.actor_type);
  const recordedModel = contextNodes.find(node => node.model);
  const missingContext = projectionError ? "过程投影未能读取，暂无法判断"
    : !projection ? "正在读取过程投影"
    : !contextProjectionReady ? "过程投影与执行快照截止不一致，暂无法判断"
    : detail?.summary.view_boundary ? "当前授权视图未显示"
    : "当前已加载的过程证据未显示";

  useEffect(() => {
    const controller = new AbortController(); pagingController.current = controller;
    setTimelineLoadingMore(false); setGapsLoadingMore(false);
    return () => controller.abort();
  }, [executionID, asOf, revision]);

  useEffect(() => {
    if (loadedReading.current === `${executionID}|${asOf ?? ""}|${revision}` && detail) return;
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
    setDefaultEventID(undefined);
    setSelectedEvent(undefined);

    async function load() {
      try {
        const nextDetail = await investigationAPI.getExecution(
          executionID,
          asOf,
          controller.signal,
        );
        if (controller.signal.aborted) {
          return;
        }
        if (asOf && Date.parse(asOf) !== Date.parse(nextDetail.as_of)) throw new Error("Requested reading cutoff was not returned");
        setDetail(nextDetail);
        const requestedEvidence = readExecutionDetail().evidence;

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
          setDefaultEventID(initialNode?.event_id);
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
            setDefaultEventID(timelineResult.value.items[0]?.event_id);
          }
        } else {
          setTimelineError(timelineResult.reason);
        }
        if (gapResult.status === "fulfilled") {
          setGaps(gapResult.value);
        } else {
          setGapsError(gapResult.reason);
        }
        loadedReading.current = `${executionID}|${nextDetail.as_of}|${revision}`;
        writeExecutionDetail(executionID, { ...readExecutionDetail(), asOf: nextDetail.as_of }, true);
        setCoreLoading(false);
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
  }, [executionID, asOf, revision]);

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
      .then((event) => { if (!controller.signal.aborted) setSelectedEvent(event); })
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
    writeExecutionDetail(executionID, { view: activeView, asOf: undefined, evidence: undefined });
  }

  async function loadMoreTimeline() {
    const cursor = timeline?.page.next_cursor;
    const controller = pagingController.current;
    if (!cursor || !detail || !controller || controller.signal.aborted) {
      return;
    }
    setTimelineLoadingMore(true);
    setTimelineError(undefined);
    try {
      const next = await investigationAPI.listTimeline(executionID, detail.as_of, cursor, controller.signal);
      if (controller.signal.aborted) return;
      setTimeline((current) =>
        current
          ? { ...next, items: mergeByID(current.items, next.items, (item) => item.event_id) }
          : next,
      );
    } catch (caught: unknown) {
      if (!controller.signal.aborted) setTimelineError(caught);
    } finally {
      if (!controller.signal.aborted) setTimelineLoadingMore(false);
    }
  }

  async function loadMoreGaps() {
    const cursor = gaps?.page.next_cursor;
    const controller = pagingController.current;
    if (!cursor || !detail || !controller || controller.signal.aborted) {
      return;
    }
    setGapsLoadingMore(true);
    setGapsError(undefined);
    try {
      const next = await investigationAPI.listGaps(executionID, detail.as_of, cursor, controller.signal);
      if (controller.signal.aborted) return;
      setGaps((current) =>
        current ? { ...next, items: mergeByID(current.items, next.items, (item) => item.gap_id) } : next,
      );
    } catch (caught: unknown) {
      if (!controller.signal.aborted) setGapsError(caught);
    } finally {
      if (!controller.signal.aborted) setGapsLoadingMore(false);
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
    if (detail) writeExecutionDetail(executionID, { ...reading, asOf: detail.as_of, evidence: eventID });
  }
  function selectView(view: ExecutionView) {
    writeExecutionDetail(executionID, { ...reading, asOf: detail?.as_of ?? asOf, evidence: selectedEventID, view });
  }
  function navigateRelated(path: string) {
    if (path.startsWith("/executions/")) {
      const target = new URL(path, window.location.origin);
      if (activeView !== "story") target.searchParams.set("view", activeView);
      navigate(target.pathname + target.search);
    } else navigate(path);
  }

  return (
    <>
      {detail?.summary.view_boundary?<><p className="boundary-note" role="note">当前为授权范围内的证据视图。未获授权的字段已隐藏，不代表来源未采集；事件哈希仍标识原始事件，不能用本视图重算。</p><details className="technical-reference"><summary>未展示的字段名称</summary><code>{detail.summary.view_boundary.withheld.join("、")}</code></details></>:null}
      <header className="topbar detail-topbar">
        <div>
          <button type="button" className="back-link" onClick={() => navigate(returnTo?.path ?? "/executions")}>
            {returnTo ? `← 返回${returnTo.label}` : "← 返回执行搜索"}
          </button>
          <span className="eyebrow">执行调查 · 固定证据快照</span>
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

      {coreLoading && <LoadingBlock label="正在固定执行快照" />}
      {Boolean(coreError) && <ErrorState error={coreError} onRetry={retryCurrentSnapshot} />}

      {detail && (
        <>
          <section className={`execution-hero hero-${detail.summary.freshness.sync_state}`} aria-labelledby="execution-request-id">
            <div className="execution-hero-main">
              <span className="eyebrow">执行标识</span>
              <h2 id="execution-request-id">{shortRef(detail.summary.request_id, 16, 8)}</h2>
              <details className="technical-reference"><summary>完整执行引用与状态代码</summary><p>请求标识 <code>{detail.summary.request_id}</code></p><p>执行标识 <code>{detail.summary.execution_id}</code></p><p>结果 <code>{detail.summary.outcome}</code> · 证据等级 <code>{detail.summary.proof_class}</code> · 集成方式 <code>{detail.summary.assurance_mode}</code> · 同步状态 <code>{detail.summary.freshness.sync_state}</code></p></details>
              <div className="hero-badge-row">
                <span>原执行证据结果：</span><OutcomeBadge outcome={detail.summary.outcome} compact />
                <ProofBadge proofClass={detail.summary.proof_class} compact />
                {detail.summary.assurance_mode !== detail.summary.proof_class ? (
                  <AssuranceBadge assuranceMode={detail.summary.assurance_mode} compact />
                ) : null}
                <FreshnessBadge state={detail.summary.freshness.sync_state} compact />
              </div>
              <p className="boundary-note">上述结果来自原执行证据；不代表运行本地交付完成或业务成功。运行请求级结果请在“系统与数据”中按独立阅读核对。</p>
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
                <dt>证据事件</dt>
                <dd>{detail.summary.event_count}</dd>
                <small>来自已保存的证据</small>
              </div>
              <div>
                <dt>采集缺口</dt>
                <dd className={detail.summary.gap_count > 0 ? "danger-text" : undefined}>{detail.summary.gap_count}</dd>
                <small>{detail.summary.gap_count > 0 ? "必须调查" : "当前未记录"}</small>
              </div>
              <div>
                <dt>证据记录的采集模式</dt>
                <dd>{detail.summary.payload_capture_modes?.map(mode => ({ metadata_only: "仅元数据", none: "未采集内容", redacted: "脱敏采集模式", full: "完整内容", unknown: "未知" } as Record<string,string>)[mode] || "采集方式待确认").join(" · ") || "未知"}</dd>
                <small>仅描述当前固定证据快照，不代表独立内容源的保存或可读状态。</small><details className="technical-reference"><summary>采集模式代码</summary><code>{detail.summary.payload_capture_modes?.join(" · ") || "unknown"}</code></details>
              </div>
              <div>
                <dt>快照截止时间</dt>
                <dd>{formatDateTime(detail.as_of)}</dd>
                <small><code>{formatCanonicalTime(detail.as_of)}</code></small>
              </div>
            </dl>
          </section>

          <section className="execution-context-summary" aria-label="执行上下文摘要">
            <h2>已记录的执行上下文</h2>
            <dl>
              <div><dt>来源中的发起者</dt><dd>{recordedActor?.actor_ref || recordedActor?.actor_type || missingContext}</dd><small>引用或类型不等于实际自然人身份。</small></div>
              <div><dt>已记录模型</dt><dd>{recordedModel?.model || missingContext}</dd><small>仅表示当前证据中的模型记录，不推定全部尝试。</small></div>
              <div><dt>费用</dt><dd>{workbenchNavigation&&!navigationInvalid?"返回原费用阅读查看":"当前调查快照未提供单次金额"}</dd><small>费用阅读有独立成员、权限和截止，不并入调查快照。</small></div>
            </dl>
            {selectedEvent && !eventLoading && session.session.permissions.includes("workbench.summary.read") ? <PurposePanel key={`${executionID}:${selectedEvent.event_id}`} executionID={executionID} basis={{evidence_event_id:selectedEvent.event_id,event_hash:selectedEvent.source.event_hash}} session={session} onInvalidated={failure=>{setDetail(undefined);setCoreError(failure)}}/> : null}
          </section>

          <ViewTabs active={activeView} onChange={selectView} contents={session.session.permissions.includes("workbench.details.read")} />
          {activeView === "systems" || activeView === "contents" ? <ExecutionRuntimePanel view={activeView} executionAsOf={detail.as_of} inherited={workbenchNavigation} navigationInvalid={navigationInvalid} navigate={navigate} summary={detail.summary} session={session} context={runtimeContext} setContext={setRuntimeContext} /> : null}
          {reading.evidence && (!projection || !projection.nodes.some(node => node.event_id === reading.evidence)) && (activeView === "story" || activeView === "graph") && <section className="node-detail-panel" aria-label="链接指定的证据事件">
            <h2>链接指定的证据事件</h2><p>{projection ? "此选择不在首批过程投影中" : "过程投影尚未可用"}，通过现有事件接口独立定位；不补造过程节点或因果边。</p>
            {eventLoading && <LoadingBlock label="正在定位链接指定的证据"/>}
            {Boolean(eventError) && <ErrorState error={eventError} compact/>}
            {selectedEvent && !eventLoading && <EventDetail event={selectedEvent}/>}
          </section>}

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
                navigate={navigateRelated}
              />
            ) : projectionError ? (
              <ErrorState error={projectionError} onRetry={retryCurrentSnapshot} />
            ) : (
              <LoadingBlock label="正在构建确定性调查视图" />
            )
          )}

          {activeView === "chronology" && (
            <div className="investigation-layout">
            <section className="timeline-panel" aria-labelledby="timeline-title">
              <div className="section-heading">
                <div>
                  <span className="eyebrow">证据时间顺序</span>
                  <h2 id="timeline-title">证据时间线</h2>
                </div>
                <p>顺序表示时间先后；只有来源明确声明的父节点才显示因果引用。</p>
              </div>

              {Boolean(timelineError) && <ErrorState error={timelineError} compact onRetry={retryCurrentSnapshot} />}
              {!timeline && !timelineError && <LoadingBlock label="正在读取时间线" />}
              {timeline && timeline.items.length === 0 && (
                <EmptyState title="没有可显示的证据事件" description="当前执行快照存在，但未返回时间线节点。" />
              )}
              {timeline && timeline.items.length > 0 && (
                <ol className="timeline-list" aria-label="按时间升序排列的证据事件">
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
                    <span className="eyebrow">选中的证据事件</span>
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
                    <span className="eyebrow">采集缺口核查</span>
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
                      <span className="eyebrow">授权范围内的来源依据</span>
                      <h2 id="proof-node-title">证据依据</h2>
                    </div>
                    {selectedEventID && <code title={selectedEventID}>{shortRef(selectedEventID, 8, 6)}</code>}
                  </div>
                  {!selectedEventID && (
                    <EmptyState title="请选择证据节点" description="先在执行概况或时间线中选择节点。" />
                  )}
                  {eventLoading && <LoadingBlock label="正在读取 证据依据" />}
                  {Boolean(eventError) && <ErrorState error={eventError} compact />}
                  {selectedEvent && !eventLoading && <EventDetail event={selectedEvent} />}
                </section>
                <section className="gap-panel" aria-labelledby="proof-gap-title">
                  <div className="section-heading">
                    <div>
                      <span className="eyebrow">明确保留的未知</span>
                      <h2 id="proof-gap-title">采集缺口</h2>
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
  const unavailableRevision = detail.summary.view_boundary ? "当前授权视图不展示" : "未提供";
  return (
    <section className="freshness-panel" aria-labelledby="freshness-panel-title">
      <div className="freshness-summary">
        <div>
          <span className="eyebrow">修订与同步水位</span>
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
        <Fact label="来源修订" value={freshness.source_revision || unavailableRevision} canonical />
        <Fact label="导入修订" value={freshness.imported_revision || unavailableRevision} canonical />
        <Fact label="来源水位" value={freshness.source_watermark || unavailableRevision} canonical />
        <Fact label="导入水位" value={freshness.import_watermark || unavailableRevision} canonical />
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
          {groups.map(([title, group]) => <FactGroup key={title} title={title} values={group} withheld={!!event.view_boundary?.withheld.length} />)}
        </div>
      </section>

      <section className="detail-section payload-section">
        <div>
          <h3>载荷策略</h3>
          <p>本页不展示请求或响应原文，也不请求原始内容。</p>
        </div>
        <dl className="compact-facts">
          <Fact label="事件捕获模式" value={event.capture_mode} canonical />
          <Fact
            label="内容采集状态"
            value={event.payload_policy?.payload_capture_mode || event.payload_capture_mode || "unknown"}
            canonical
          />
          <Fact label="脱敏状态" value={event.payload_policy?.redaction_state || "未提供"} canonical />
          <Fact label="内容引用" value={event.payload_policy?.payload_ref || "未提供"} canonical />
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

function FactGroup({ title, values, withheld = false }: { title: string; values: object | undefined; withheld?: boolean }) {
  const entries = values
    ? Object.entries(values as Record<string, unknown>).filter(([, value]) => value !== undefined && value !== null && value !== "")
    : [];
  if (entries.length === 0) {
    return (
      <article className="fact-group fact-group-empty">
        <h4>{title}</h4>
        <p>{withheld ? "当前授权视图未显示此组值；不能据此判断来源未采集。" : "未提供；请结合来源记录或采集缺口判断。"}</p>
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
