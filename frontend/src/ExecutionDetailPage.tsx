import { t, tx, useLocale } from "./i18n";
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
  useLocale();
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
  const missingContext = projectionError ? t("m0448")
    : !projection ? t("m0449")
    : !contextProjectionReady ? t("m0450")
    : detail?.summary.view_boundary ? t("m0451")
    : t("m0452");

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
      {detail?.summary.view_boundary?<><p className="boundary-note" role="note">{tx("m0453")}</p><details className="technical-reference"><summary>{tx("m0454")}</summary><code>{detail.summary.view_boundary.withheld.join("、")}</code></details></>:null}
      <header className="topbar detail-topbar">
        <div>
          <button type="button" className="back-link" onClick={() => navigate(returnTo?.path ?? "/executions")}>
            {returnTo ? t("m0455", { value0: returnTo.label }) : t("m0456")}
          </button>
          <span className="eyebrow">{tx("m0457")}</span>
          <h1>{tx("m0458")}</h1>
        </div>
        <div className="topbar-actions">
          <button type="button" className="button button-secondary" onClick={retryCurrentSnapshot} disabled={coreLoading}>{tx("m0459")}</button>
          <button type="button" className="button button-primary" onClick={loadLatestSnapshot} disabled={coreLoading}>{tx("m0460")}</button>
        </div>
      </header>

      {coreLoading && <LoadingBlock label={t("m0461")} />}
      {Boolean(coreError) && <ErrorState error={coreError} onRetry={retryCurrentSnapshot} />}

      {detail && (
        <>
          <section className={`execution-hero hero-${detail.summary.freshness.sync_state}`} aria-labelledby="execution-request-id">
            <div className="execution-hero-main">
              <span className="eyebrow">{tx("m0462")}</span>
              <h2 id="execution-request-id">{shortRef(detail.summary.request_id, 16, 8)}</h2>
              <details className="technical-reference"><summary>{tx("m0463")}</summary><p>{t("m0464")}<code>{detail.summary.request_id}</code></p><p>{t("m0465")}<code>{detail.summary.execution_id}</code></p><p>{t("m0466")}<code>{detail.summary.outcome}</code> {t("m0467")}<code>{detail.summary.proof_class}</code> {t("m0468")}<code>{detail.summary.assurance_mode}</code> {t("m0469")}<code>{detail.summary.freshness.sync_state}</code></p></details>
              <div className="hero-badge-row">
                <span>{tx("m0470")}</span><OutcomeBadge outcome={detail.summary.outcome} compact />
                <ProofBadge proofClass={detail.summary.proof_class} compact />
                {detail.summary.assurance_mode !== detail.summary.proof_class ? (
                  <AssuranceBadge assuranceMode={detail.summary.assurance_mode} compact />
                ) : null}
                <FreshnessBadge state={detail.summary.freshness.sync_state} compact />
              </div>
              <p className="boundary-note">{tx("m0471")}</p>
              <p className="proof-explanation">{proofExplanation(detail.summary.proof_class)}</p>
            </div>
            <dl className="execution-facts">
              <div>
                <dt>{tx("m0472")}</dt>
                <dd>{detail.summary.source_display_name || detail.summary.source_type}</dd>
                <small>{detail.summary.source_type}</small>
              </div>
              <div>
                <dt>{tx("m0473")}</dt>
                <dd>{formatDateTime(detail.summary.first_effective_at)}</dd>
                <small>{tx("m0474", { value0: formatDateTime(detail.summary.last_effective_at) })}</small>
              </div>
              <div>
                <dt>{tx("m0475")}</dt>
                <dd>{detail.summary.event_count}</dd>
                <small>{tx("m0476")}</small>
              </div>
              <div>
                <dt>{tx("m0477")}</dt>
                <dd className={detail.summary.gap_count > 0 ? "danger-text" : undefined}>{detail.summary.gap_count}</dd>
                <small>{detail.summary.gap_count > 0 ? t("m0478") : t("m0479")}</small>
              </div>
              <div>
                <dt>{tx("m0480")}</dt>
                <dd>{detail.summary.payload_capture_modes?.map(mode => ({ metadata_only: t("m0481"), none: t("m0482"), redacted: t("m0483"), full: t("m0484"), unknown: t("m0193") } as Record<string,string>)[mode] || t("m0485")).join(" · ") || t("m0193")}</dd>
                <small>{tx("m0486")}</small><details className="technical-reference"><summary>{tx("m0487")}</summary><code>{detail.summary.payload_capture_modes?.join(" · ") || "unknown"}</code></details>
              </div>
              <div>
                <dt>{tx("m0488")}</dt>
                <dd>{formatDateTime(detail.as_of)}</dd>
                <small><code>{formatCanonicalTime(detail.as_of)}</code></small>
              </div>
            </dl>
          </section>

          <section className="execution-context-summary" aria-label={t("m0489")}>
            <h2>{tx("m0490")}</h2>
            <dl>
              <div><dt>{tx("m0491")}</dt><dd>{recordedActor?.actor_ref || recordedActor?.actor_type || missingContext}</dd><small>{tx("m0492")}</small></div>
              <div><dt>{tx("m0493")}</dt><dd>{recordedModel?.model || missingContext}</dd><small>{tx("m0494")}</small></div>
              <div><dt>{tx("m0495")}</dt><dd>{workbenchNavigation&&!navigationInvalid?t("m0496"):t("m0497")}</dd><small>{tx("m0498")}</small></div>
            </dl>
            {selectedEvent && !eventLoading && session.session.permissions.includes("workbench.summary.read") ? <PurposePanel key={`${executionID}:${selectedEvent.event_id}`} executionID={executionID} basis={{evidence_event_id:selectedEvent.event_id,event_hash:selectedEvent.source.event_hash}} session={session} onInvalidated={failure=>{setDetail(undefined);setCoreError(failure)}}/> : null}
          </section>

          <ViewTabs active={activeView} onChange={selectView} contents={session.session.permissions.includes("workbench.details.read")} />
          {activeView === "systems" || activeView === "contents" ? <ExecutionRuntimePanel view={activeView} executionAsOf={detail.as_of} inherited={workbenchNavigation} navigationInvalid={navigationInvalid} navigate={navigate} summary={detail.summary} session={session} context={runtimeContext} setContext={setRuntimeContext} /> : null}
          {reading.evidence && (!projection || !projection.nodes.some(node => node.event_id === reading.evidence)) && (activeView === "story" || activeView === "graph") && <section className="node-detail-panel" aria-label={t("m0499")}>
            <h2>{tx("m0499")}</h2><p>{tx("m0502", { value0: projection ? t("m0500") : t("m0501") })}</p>
            {eventLoading && <LoadingBlock label={t("m0503")}/>}
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
              <LoadingBlock label={t("m0504")} />
            )
          )}

          {activeView === "chronology" && (
            <div className="investigation-layout">
            <section className="timeline-panel" aria-labelledby="timeline-title">
              <div className="section-heading">
                <div>
                  <span className="eyebrow">{tx("m0505")}</span>
                  <h2 id="timeline-title">{tx("m0506")}</h2>
                </div>
                <p>{tx("m0507")}</p>
              </div>

              {Boolean(timelineError) && <ErrorState error={timelineError} compact onRetry={retryCurrentSnapshot} />}
              {!timeline && !timelineError && <LoadingBlock label={t("m0508")} />}
              {timeline && timeline.items.length === 0 && (
                <EmptyState title={t("m0509")} description={t("m0510")} />
              )}
              {timeline && timeline.items.length > 0 && (
                <ol className="timeline-list" aria-label={t("m0511")}>
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
                  <span>{tx("m0512", { value0: timeline.items.length, value1: timeline.total_event_count })}</span>
                  {timeline.page.has_more && (
                    <button
                      type="button"
                      className="button button-secondary"
                      disabled={timelineLoadingMore}
                      onClick={() => void loadMoreTimeline()}
                    >
                      {timelineLoadingMore ? t("m0513") : t("m0514")}
                    </button>
                  )}
                </footer>
              )}
            </section>

            <div className="detail-column">
              <section ref={detailPanelRef} className="node-detail-panel" aria-labelledby="node-detail-title">
                <div className="section-heading">
                  <div>
                    <span className="eyebrow">{tx("m0515")}</span>
                    <h2 id="node-detail-title">{tx("m0516")}</h2>
                  </div>
                  {selectedEventID && <code title={selectedEventID}>{shortRef(selectedEventID, 8, 6)}</code>}
                </div>
                {!selectedEventID && (
                  <EmptyState title={t("m0517")} description={t("m0518")} />
                )}
                {eventLoading && <LoadingBlock label={t("m0519")} />}
                {Boolean(eventError) && <ErrorState error={eventError} compact />}
                {selectedEvent && !eventLoading && <EventDetail event={selectedEvent} />}
              </section>

              <section className="gap-panel" aria-labelledby="gap-panel-title">
                <div className="section-heading">
                  <div>
                    <span className="eyebrow">{tx("m0520")}</span>
                    <h2 id="gap-panel-title">{tx("m0521")}</h2>
                  </div>
                  {gaps && <strong className={gaps.total_gap_count > 0 ? "gap-total" : "muted-count"}>{gaps.total_gap_count}</strong>}
                </div>
                {Boolean(gapsError) && <ErrorState error={gapsError} compact onRetry={retryCurrentSnapshot} />}
                {!gaps && !gapsError && <LoadingBlock label={t("m0522")} />}
                {gaps && gaps.items.length === 0 && (
                  <EmptyState
                    title={t("m0523")}
                    description={
                      detail.summary.freshness.timeline_complete
                        ? t("m0524")
                        : t("m0525")
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
                    <span>{tx("m0526", { value0: gaps.items.length, value1: gaps.total_gap_count })}</span>
                    {gaps.page.has_more && (
                      <button
                        type="button"
                        className="button button-secondary"
                        disabled={gapsLoadingMore}
                        onClick={() => void loadMoreGaps()}
                      >
                        {gapsLoadingMore ? t("m0513") : t("m0527")}
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
                      <span className="eyebrow">{tx("m0528")}</span>
                      <h2 id="proof-node-title">{tx("m0442")}</h2>
                    </div>
                    {selectedEventID && <code title={selectedEventID}>{shortRef(selectedEventID, 8, 6)}</code>}
                  </div>
                  {!selectedEventID && (
                    <EmptyState title={t("m0529")} description={t("m0530")} />
                  )}
                  {eventLoading && <LoadingBlock label={t("m0531")} />}
                  {Boolean(eventError) && <ErrorState error={eventError} compact />}
                  {selectedEvent && !eventLoading && <EventDetail event={selectedEvent} />}
                </section>
                <section className="gap-panel" aria-labelledby="proof-gap-title">
                  <div className="section-heading">
                    <div>
                      <span className="eyebrow">{tx("m0532")}</span>
                      <h2 id="proof-gap-title">{tx("m0477")}</h2>
                    </div>
                    {gaps && <strong className={gaps.total_gap_count > 0 ? "gap-total" : "muted-count"}>{gaps.total_gap_count}</strong>}
                  </div>
                  {Boolean(gapsError) && <ErrorState error={gapsError} compact onRetry={retryCurrentSnapshot} />}
                  {!gaps && !gapsError && <LoadingBlock label={t("m0522")} />}
                  {gaps && gaps.items.length === 0 && (
                    <EmptyState
                      title={t("m0523")}
                      description={t("m0533")}
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
  useLocale();
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
            <span>{tx("m0534")}</span>
            {node.declared_parent_resolved && node.declared_parent_event_id ? (
              <code title={node.declared_parent_event_id}>{shortRef(node.declared_parent_event_id, 8, 6)}</code>
            ) : (
              <strong>{tx("m0535")}</strong>
            )}
          </div>
        )}
      </button>
    </li>
  );
}

function FreshnessPanel({ detail }: { detail: ExecutionDetail }) {
  useLocale();
  const freshness = detail.summary.freshness;
  const unavailableRevision = detail.summary.view_boundary ? t("m0536") : t("m0100");
  return (
    <section className="freshness-panel" aria-labelledby="freshness-panel-title">
      <div className="freshness-summary">
        <div>
          <span className="eyebrow">{tx("m0537")}</span>
          <h2 id="freshness-panel-title">{tx("m0538")}</h2>
        </div>
        <FreshnessBadge state={freshness.sync_state} />
        <p>
          {freshness.timeline_complete
            ? t("m0539")
            : t("m0540")}
        </p>
      </div>
      <dl className="watermark-grid">
        <Fact label={t("m0541")} value={freshness.integration_mode} canonical />
        <Fact label={t("m0542")} value={formatCanonicalTime(freshness.source_generated_at)} canonical />
        <Fact label={t("m0543")} value={formatCanonicalTime(freshness.imported_at)} canonical />
        <Fact label={t("m0544")} value={formatCanonicalTime(freshness.search_visible_at)} canonical />
        <Fact label={t("m0545")} value={freshness.source_revision || unavailableRevision} canonical />
        <Fact label={t("m0546")} value={freshness.imported_revision || unavailableRevision} canonical />
        <Fact label={t("m0547")} value={freshness.source_watermark || unavailableRevision} canonical />
        <Fact label={t("m0548")} value={freshness.import_watermark || unavailableRevision} canonical />
      </dl>
    </section>
  );
}

function EventDetail({ event }: { event: EvidenceEventDetail }) {
  useLocale();
  const states = provenanceStates(event.field_provenance, event.stage);
  const groups = [
    [t("m0549"), event.correlation],
    [t("m0550"), event.identity],
    [t("m0551"), event.capability],
    [t("m0552"), event.decision],
    [t("m0553"), event.usage],
    [t("m0554"), event.outcome],
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
        <h3>{tx("m0555")}</h3>
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
        <h3>{tx("m0556")}</h3>
        <div className="fact-group-list">
          {groups.map(([title, group]) => <FactGroup key={title} title={title} values={group} withheld={!!event.view_boundary?.withheld.length} />)}
        </div>
      </section>

      <section className="detail-section payload-section">
        <div>
          <h3>{tx("m0557")}</h3>
          <p>{tx("m0558")}</p>
        </div>
        <dl className="compact-facts">
          <Fact label={t("m0559")} value={event.capture_mode} canonical />
          <Fact
            label={t("m0560")}
            value={event.payload_policy?.payload_capture_mode || event.payload_capture_mode || "unknown"}
            canonical
          />
          <Fact label={t("m0561")} value={event.payload_policy?.redaction_state || t("m0100")} canonical />
          <Fact label={t("m0562")} value={event.payload_policy?.payload_ref || t("m0100")} canonical />
          <Fact label={t("m0563")} value={event.payload_policy?.retention_policy_ref || t("m0100")} canonical />
        </dl>
      </section>

      <section className="detail-section source-section">
        <div>
          <h3>{tx("m0564")}</h3>
          <span className="fidelity-badge">{event.source.fidelity_level}</span>
        </div>
        <dl className="source-facts">
          <Fact label="Source system" value={event.source.source_system_id} canonical />
          <Fact label="Adapter manifest" value={event.source.adapter_manifest_id} canonical />
          <Fact label="Schema" value={event.source.schema_version} canonical />
          <Fact label="Source type" value={event.source.source_type} canonical />
          <Fact label="Source version" value={event.source.source_version || t("m0100")} canonical />
          <Fact label="Source event ID" value={event.source.source_event_id || t("m0100")} canonical />
          <Fact label="Source event type" value={event.source.source_event_type || t("m0100")} canonical />
          <Fact label="Source ref" value={event.source.source_ref} canonical />
          <Fact label="Event hash" value={event.source.event_hash} canonical />
          <Fact label="Source event hash" value={event.source.source_event_hash || t("m0100")} canonical />
        </dl>
        <p className="allowlist-note">{t("m0565")}<code>raw_source_excerpt</code> {t("m0566")}</p>
      </section>
    </div>
  );
}

function FactGroup({ title, values, withheld = false }: { title: string; values: object | undefined; withheld?: boolean }) {
  useLocale();
  const entries = values
    ? Object.entries(values as Record<string, unknown>).filter(([, value]) => value !== undefined && value !== null && value !== "")
    : [];
  if (entries.length === 0) {
    return (
      <article className="fact-group fact-group-empty">
        <h4>{title}</h4>
        <p>{withheld ? t("m0567") : t("m0568")}</p>
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
  useLocale();
  return (
    <article className={`gap-card gap-severity-${gap.severity}`}>
      <div className="gap-card-heading">
        <div>
          <span>{tx("m0569")}</span>
          <strong>{gap.gap_type}</strong>
        </div>
        <span className="severity-chip">{gap.severity}</span>
      </div>
      <dl>
        <Fact label={t("m0570")} value={gap.affected_claim} />
        <Fact label={t("m0571")} value={gap.gap_reason} />
        <Fact label={t("m0572")} value={gap.recommendation || t("m0573")} />
        <Fact label={t("m0574")} value={formatCanonicalTime(gap.created_at)} canonical />
      </dl>
      {gap.candidate_gap_types && gap.candidate_gap_types.length > 0 && (
        <div className="candidate-gaps">
          <span>{tx("m0575")}</span>
          <div>{gap.candidate_gap_types.map((candidate) => <code key={candidate}>{candidate}</code>)}</div>
        </div>
      )}
      {gap.evidence_event_id && (
        <button type="button" className="button button-quiet" onClick={onSelect}>{tx("m0576")}</button>
      )}
    </article>
  );
}

function Fact({ label, value, canonical = false }: { label: string; value: string | number; canonical?: boolean }) {
  useLocale();
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
