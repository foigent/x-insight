import { localizedLabels, t, tx, useLocale } from "./i18n";
import { declaredPath, graphPositions } from "./declaredGraph";
import { KeyboardEvent, useId, useEffect, useMemo, useState } from "react";

import {
  isAbortError,
  workspaceAPI,
  type AccessSessionResponse,
  type CaptureGap,
  type EvidenceEventDetail,
  type InvestigationCase,
  type InvestigationNode,
  type InvestigationProjection,
  type StageCoverage,
  type TraceJourney,
  type TraceJourneyNode,
} from "./api";
import {
  displayValue,
  formatDateTime,
  formatMicroCNY,
  formatShortTime,
  shortRef,
} from "./format";
import { ReviewStart } from "./ReviewStart";

type InspectorTab = "facts" | "evidence" | "gaps";

const stageLabels: Record<string, string> = localizedLabels({}, {identity: "m0550", routing: "m0913", provider: "m0914", retry_fallback: "m0915", billing: "m0916", usage: "m0553", logging: "m0917"});

const coverageLabels: Record<string, string> = localizedLabels({}, {present: "m0918", gap: "m0619", not_applicable: "m0919", unknown: "m0193", complete: "m0920", incomplete: "m0921", not_available: "m0922"});

const nodeLabels: Record<string, string> = localizedLabels({}, {identity_bound: "m0923", route_selected: "m0924", provider_failed: "m0925", provider_succeeded: "m0926", retry_scheduled: "m0927", fallback_selected: "m0928", billing_recorded: "m0929", execution_completed: "m0930", capture_gap: "m0931", evidence_observed: "m0932"});

const completenessBasisLabels: Record<string, string> = localizedLabels({}, {source_profile_declares_timeline_complete: "m0933", source_profile_declares_partial_or_unknown: "m0934", connector_checkpoint_is_current: "m0935", connector_checkpoint_not_current: "m0936", bounded_projection_contains_all_imported_records: "m0937", projection_truncated_or_count_mismatch: "m0938", source_profile_declared_no_causal_edges: "m0939", all_source_declared_parents_resolved: "m0940", source_declared_parent_unresolved: "m0941"});
const unprovableLabels: Record<string, string> = localizedLabels({}, {import_not_current: "m0942", projection_bounded: "m0943", capture_gap_limits_claim: "m0944"});

export function InvestigationView({
  mode,
  projection,
  traceJourney,
  traceJourneyError,
  selectedEventID,
  selectedEvent,
  eventLoading,
  eventError,
  onSelect,
  session,
  executionID,
  asOf,
  navigate,
}: {
  mode: "story" | "graph";
  projection: InvestigationProjection;
  traceJourney?: TraceJourney;
  traceJourneyError?: unknown;
  selectedEventID?: string;
  selectedEvent?: EvidenceEventDetail;
  eventLoading: boolean;
  eventError?: unknown;
  onSelect: (eventID: string) => void;
  session: AccessSessionResponse;
  executionID: string;
  asOf: string;
  navigate: (path: string) => void;
}) {
  useLocale();
  const [inspectorTab, setInspectorTab] = useState<InspectorTab>("facts");
	const canWrite = (session.session.permissions.includes("investigation.write") || session.session.permissions.includes("investigation.scoped.write"));
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">("idle");
  const [cases, setCases] = useState<InvestigationCase[]>([]);
  const [selectedCaseID, setSelectedCaseID] = useState("");
  const [caseState, setCaseState] = useState<"idle" | "saving" | "saved" | "error">("idle");
  const [gapCaseState, setGapCaseState] = useState<"idle" | "saving" | "saved" | "error">("idle");
  const selectedNode = projection.nodes.find((node) => node.event_id === selectedEventID);
  const selectedGaps = useMemo(
    () => gapsForNode(projection, selectedNode),
    [projection, selectedNode],
  );
  const finalNode = [...projection.nodes]
    .reverse()
    .find((node) => node.kind === "execution_completed" || node.kind === "provider_succeeded");
  const failedNode = projection.nodes.find((node) => node.kind === "provider_failed");
  const fallbackNode = projection.nodes.find((node) => node.kind === "fallback_selected");
  const fallbackSequenceClaim = projection.summary_claims.find(
    (claim) => claim.code === "primary_provider_failed_then_fallback_succeeded",
  );
  const fallbackSequence = Boolean(fallbackSequenceClaim);
  const fallbackSequenceIsDeclaredPath = fallbackSequenceClaim
    ? isDeclaredPath(projection, fallbackSequenceClaim.evidence_event_ids)
    : false;
  const integrityComplete =
    projection.integrity.source_profile_complete &&
    projection.integrity.import_complete &&
    projection.integrity.projection_complete &&
    projection.integrity.causal_complete;
  const coverageShouldOpen = !integrityComplete || projection.gaps.length > 0;
  const [coverageOpen, setCoverageOpen] = useState(coverageShouldOpen);
  const journey = executionJourney(projection);
  const coveredStages = projection.stage_coverage.filter((item) => item.status === "present").length;

  useEffect(() => {
    setCoverageOpen(coverageShouldOpen);
  }, [asOf, coverageShouldOpen, executionID]);

  useEffect(() => {
    const controller = new AbortController();
    workspaceAPI.listCases(controller.signal)
      .then((response) => {
        const open = response.items.filter((item) => item.status === "open");
        setCases(open);
        setSelectedCaseID((current) => current || open[0]?.id || "");
      })
      .catch((error) => {
        if (!isAbortError(error) && !controller.signal.aborted) setCases([]);
      });
    return () => controller.abort();
  }, []);

  async function copyCitation() {
    if (!selectedNode) {
      return;
    }
    const citation = [
      `evidence_event_id=${selectedNode.citation.evidence_event_id}`,
      `source_ref=${selectedNode.citation.source_ref}`,
      `event_hash=${selectedNode.citation.event_hash}`,
      `deep_link=${selectedNode.citation.deep_link}`,
    ].join("\n");
    try {
      if (!navigator.clipboard) {
        throw new Error("clipboard unavailable");
      }
      await navigator.clipboard.writeText(citation);
      setCopyState("copied");
    } catch {
      setCopyState("failed");
    }
  }

  async function addEvidenceToCase() {
    if (!selectedNode || !selectedCaseID) return;
    setCaseState("saving");
    try {
      await workspaceAPI.addExecution(selectedCaseID, executionID, asOf, session.csrf_token);
      await workspaceAPI.addEvidence(selectedCaseID, selectedNode.citation, session.csrf_token);
      for (const gap of selectedGaps) {
        await workspaceAPI.addGap(selectedCaseID, gap, session.csrf_token);
      }
      setCaseState("saved");
    } catch {
      setCaseState("error");
    }
  }

  async function addAllGapsToCase() {
    if (!selectedCaseID || projection.gaps.length === 0) return;
    setGapCaseState("saving");
    try {
      await workspaceAPI.addExecution(selectedCaseID, executionID, asOf, session.csrf_token);
      for (const gap of projection.gaps) {
        await workspaceAPI.addGap(selectedCaseID, gap, session.csrf_token);
      }
      setGapCaseState("saved");
    } catch {
      setGapCaseState("error");
    }
  }

  return (
    <>
      <section
        className={`investigation-overview presentation-${projection.presentation_mode}`}
        aria-labelledby="investigation-headline"
      >
        <div className="investigation-overview-main">
          <div className="overview-kicker-row">
            <span className="eyebrow">{tx("m0945")}</span>
            <span className="presentation-chip">{presentationLabel(projection.presentation_mode)}</span>
            {projection.execution.proof_class === "synthetic_fixture" ? (
              <span className="demo-data-chip">{tx("m0946")}</span>
            ) : null}
          </div>
          <h2 id="investigation-headline">
            {headline(projection, failedNode, fallbackNode, fallbackSequenceIsDeclaredPath)}
          </h2>
          <p>{subheadline(projection, failedNode, finalNode, fallbackSequence, fallbackSequenceIsDeclaredPath)}</p><details className="technical-reference"><summary>{tx("m0947")}</summary><code>{projection.presentation_mode}</code></details>
          <ol className="journey-strip" aria-label={t("m0948")}>
            {journey.map((item, index) => (
              <li key={item.label} className={item.uncertain ? "journey-step-uncertain" : undefined}>
                <span className="journey-index" aria-hidden="true">{String(index + 1).padStart(2, "0")}</span>
                <span className="journey-label">{item.label}</span>
                <strong>{item.value}</strong>
                <small>{item.note}</small>
              </li>
            ))}
          </ol>
          <p className="journey-boundary">{executionStory(projection)}</p>
          {fallbackSequenceClaim ? (
            <p className="claim-basis">
              <strong>{fallbackSequenceIsDeclaredPath ? t("m0949") : t("m0950")}</strong>
              <span>{fallbackSequenceClaim.derivation_basis}</span>
            </p>
          ) : null}
          {!integrityComplete ? (
            <div className="overview-badges">
              <span className="integrity-warning">{tx("m0951")}</span>
            </div>
          ) : null}
        </div>
        <dl className="investigation-metrics">
          <Metric label={t("m0952")} value={metricLatency(finalNode)} />
          <Metric label={t("m0953")} value={finalNode?.total_tokens ?? t("m0100")} />
          <Metric
            label={t("m0954")}
            value={finalNode?.cost_micros !== undefined ? formatMicroCNY(finalNode.cost_micros) : t("m0100")}
          />
          <Metric
            label={t("m0521")}
            value={projection.gaps.length}
            alert={projection.gaps.length > 0}
          />
          <Metric
            label={t("m0955")}
            value={`${projection.nodes.length} / ${projection.gaps.length}`}
          />
        </dl>
      </section>

      {projection.presentation_mode === "terminal_snapshot" ? (
        <section className="terminal-snapshot-notice" role="status">
          <strong>{tx("m0956")}</strong>
          <p>{tx("m0957")}</p>
        </section>
      ) : null}

      <TraceJourneyPanel
        journey={traceJourney}
        unavailable={Boolean(traceJourneyError)}
        executionID={executionID}
        asOf={asOf}
        onSelect={onSelect}
        navigate={navigate}
      />

      <details
        className="coverage-section"
        open={coverageOpen}
        onToggle={(event) => setCoverageOpen(event.currentTarget.open)}
      >
        <summary className="coverage-summary">
          <div>
            <span className="eyebrow">{tx("m0958")}</span>
            <h2 id="coverage-title">{tx("m0959")}</h2>
          </div>
          <span className={`coverage-summary-state${integrityComplete ? " coverage-summary-complete" : " coverage-summary-limited"}`}>{tx("m0962", { value0: coveredStages, value1: projection.stage_coverage.length, value2: integrityComplete ? t("m0960") : t("m0961") })}</span>
        </summary>
        <div className="coverage-content" aria-labelledby="coverage-title">
          <p>{tx("m0963")}</p>
          {projection.gaps.length ? <ReviewStart key={`${executionID}:gaps`} session={session} navigate={navigate} available={{ evidence: [], gaps: projection.gaps.map((g) => ({ scope: "finding", gap_id: g.gap_id, relation: "limits", affected_claim: g.affected_claim, gap_type: g.gap_type })) }} /> : null}
          <div className="coverage-strip">
            {projection.stage_coverage.map((coverage) => (
              <CoverageItem key={coverage.stage} coverage={coverage} onSelect={onSelect} />
            ))}
          </div>
          <div className="completeness-grid" aria-label={t("m0964")}>
            {completenessAxes(projection).map((axis) => (
              <article key={axis.axis} className={`axis-card axis-${axis.status}`}>
                <span>{axisLabel(axis.axis)}</span>
                <strong>{coverageLabels[axis.status] || t("m0965")}</strong>
                <p>{completenessBasisLabels[axis.basis] || t("m0966")}</p><details className="technical-reference"><summary>{tx("m0967")}</summary><code>{axis.axis} · {axis.status} · {axis.basis}</code></details>
              </article>
            ))}
          </div>
        </div>
      </details>

      <div className={`investigation-workbench investigation-workbench-${mode}`}>
        <section className="trace-story" aria-labelledby="trace-story-title">
          <div className="section-heading">
            <div>
              <span className="eyebrow">{tx("m0968")}</span>
              <h2 id="trace-story-title">{mode === "graph" ? t("m0969") : t("m0970")}</h2>
            </div>
            <p>{tx("m0971", { value0: projection.edges.length })}</p>
          </div>
          {projection.nodes.length === 0 ? (
            <p className="trace-empty">{tx("m0972")}</p>
          ) : mode === "graph" ? (
            <CausalGraph
              projection={projection}
              selectedEventID={selectedEventID}
              onSelect={onSelect}
            />
          ) : (
            <ol className="trace-rail" aria-label={t("m0973")}>
              {projection.nodes.map((node, index) => (
                <TraceNode
                  key={node.event_id}
                  node={node}
                  index={index}
                  selected={node.event_id === selectedEventID}
                  declaredParentID={
                    projection.edges.find((edge) => edge.to === node.event_id)?.from
                  }
                  onSelect={() => onSelect(node.event_id)}
                />
              ))}
            </ol>
          )}
        </section>

        <section className="investigation-inspector" aria-labelledby="inspector-title">
          <div className="section-heading inspector-heading">
            <div>
              <span className="eyebrow">{tx("m0974")}</span>
              <h2 id="inspector-title">{tx("m0975")}</h2>
            </div>
            {selectedNode ? <code>{shortRef(selectedNode.event_id, 8, 6)}</code> : null}
          </div>
          {!selectedNode ? (
            <p className="inspector-empty">{tx("m0976")}</p>
          ) : (
            <>
              <div className="inspector-summary">
                <strong>{nodeLabels[selectedNode.kind] || selectedNode.event_type}</strong>
                <span className={`severity-chip severity-${selectedNode.severity}`}>
                  {({info:t("m0977"),warning:t("m0978"),error:t("m0979"),critical:t("m0980")} as Record<string,string>)[selectedNode.severity] || t("m0981")}
                </span>
                <details className="technical-reference"><summary>{tx("m0982")}</summary><code>{selectedNode.event_type} · {selectedNode.severity} · {selectedNode.proof_basis} · {selectedNode.outcome_status}</code></details>
              </div>
              <div className="inspector-tabs" role="tablist" aria-label={t("m0983")}>
                {([
                  ["facts", t("m0984")],
                  ["evidence", t("m0985")],
                  ["gaps", t("m0986", { value0: selectedGaps.length })],
                ] as const).map(([tab, label]) => (
                  <button
                    key={tab}
                    type="button"
                    role="tab"
                    aria-selected={inspectorTab === tab}
                    className={inspectorTab === tab ? "inspector-tab-active" : undefined}
                    onClick={() => setInspectorTab(tab)}
                  >
                    {label}
                  </button>
                ))}
              </div>
              {eventLoading ? <p className="inspector-loading">{tx("m0987")}</p> : null}
              {eventError ? <p className="inspector-error">{tx("m0988")}</p> : null}
              {!eventLoading && inspectorTab === "facts" ? (
                <FactsPanel node={selectedNode} event={selectedEvent} />
              ) : null}
              {!eventLoading && inspectorTab === "evidence" ? (
                <EvidencePanel
                  node={selectedNode}
                  event={selectedEvent}
                  copyState={copyState}
                  onCopy={() => void copyCitation()}
                />
              ) : null}
              {!eventLoading && inspectorTab === "gaps" ? (
                <GapsPanel gaps={selectedGaps} />
              ) : null}
              <ReviewStart key={selectedNode.citation.evidence_event_id} session={session} navigate={navigate} available={{ evidence: [{ scope: "finding", evidence_event_id: selectedNode.citation.evidence_event_id, event_hash: selectedNode.citation.event_hash, source_ref: selectedNode.citation.source_ref, relation: "supports" }], gaps: selectedGaps.map((g) => ({ scope: "finding", gap_id: g.gap_id, relation: "limits", affected_claim: g.affected_claim, gap_type: g.gap_type })) }} />
              {canWrite ? <div className="case-capture">
                <span className="eyebrow">{tx("m0989")}</span>
                {cases.length > 0 ? (
                  <>
                    <label htmlFor="case-target">{t("m0990")}<select id="case-target" value={selectedCaseID} onChange={(event) => {
                        setSelectedCaseID(event.target.value);
                        setCaseState("idle");
                        setGapCaseState("idle");
                      }}>
                        {cases.map((item) => <option key={item.id} value={item.id}>{item.title}</option>)}
                      </select>
                    </label>
                    <button
                      className="button button-primary"
                      type="button"
                      disabled={caseState === "saving"}
                      onClick={() => void addEvidenceToCase()}
                    >
                      {caseState === "saving" ? t("m0991") : t("m0992")}
                    </button>
                    <p role="status">
                      {caseState === "saved"
                        ? t("m0993")
                        : caseState === "error"
                          ? t("m0994")
                          : t("m0995")}
                    </p>
                    {projection.gaps.length > selectedGaps.length ? (
                      <>
                        <button
                          className="button button-secondary"
                          type="button"
                          disabled={gapCaseState === "saving"}
                          onClick={() => void addAllGapsToCase()}
                        >
                          {gapCaseState === "saving"
                            ? t("m0996")
                            : t("m0997", { value0: projection.gaps.length })}
                        </button>
                        <p role="status">
                          {gapCaseState === "saved"
                            ? t("m0998")
                            : gapCaseState === "error"
                              ? t("m0999")
                              : t("m1000")}
                        </p>
                      </>
                    ) : null}
                  </>
                ) : session.session.permissions.includes("investigation.write") ? (
                  <button className="button button-secondary" type="button" onClick={() => navigate("/cases")}>{tx("m1001")}</button>
                ) : <p>{tx("m1002")}</p>}
              </div> : null}
            </>
          )}
        </section>
      </div>

      <section className="unprovable-section" aria-labelledby="unprovable-title">
        <div className="section-heading">
          <div><span className="eyebrow">{tx("m1003")}</span><h2 id="unprovable-title">{tx("m1004")}</h2></div>
          <p>{tx("m1005")}</p>
        </div>
        <div className="unprovable-grid">
          {(projection.unprovable_actions ?? []).map((item) => (
            <article key={item.code}>
              <span className="missing-mark" aria-hidden="true">∕</span>
              <div><strong>{unprovableLabels[item.code] || item.statement}</strong><details className="technical-reference"><summary>{tx("m1006")}</summary><p>{item.statement}</p><code>{item.code}</code></details><small>{tx("m1007", { value0: item.gap_ids?.length ?? 0 })}</small></div>
            </article>
          ))}
          {(projection.unprovable_actions?.length ?? 0) === 0 ? (
            <p className="quiet-copy">{tx("m1008")}</p>
          ) : null}
        </div>
      </section>
    </>
  );
}

function TraceJourneyPanel({
  journey,
  unavailable,
  executionID,
  asOf,
  onSelect,
  navigate,
}: {
  journey?: TraceJourney;
  unavailable: boolean;
  executionID: string;
  asOf: string;
  onSelect: (eventID: string) => void;
  navigate: (path: string) => void;
}) {
  useLocale();
  if (unavailable) {
    return (
      <section className="trace-journey trace-journey-unavailable" aria-label={t("m1009")}>
        <div>
          <span className="eyebrow">{tx("m1010")}</span>
          <h2>{tx("m1011")}</h2>
        </div>
        <p>{tx("m1012")}</p>
      </section>
    );
  }
  if (!journey?.available || journey.nodes.length === 0) {
    return null;
  }

  const path = declaredTraceJourneyPath(journey);
  const declaredEdges = new Set(journey.edges.map((edge) => `${edge.from_event_id}\u0000${edge.to_event_id}`));
  const pathIsFullyDeclared = path.length > 1 && path.slice(0, -1).every((node, index) =>
    declaredEdges.has(`${node.event_id}\u0000${path[index + 1].event_id}`),
  );
  const outsidePathCount = Math.max(0, journey.nodes.length - path.length);
  const root = path[0];
  const terminal = path[path.length - 1];
  const actor = root?.actor_ref || root?.actor_type || t("m1013");
  const action = root?.action || t("m1014");
  const systems = path
    .map((node) => node.service_name || node.provider || node.model || node.source_type)
    .filter((value, index, values) => Boolean(value) && values.indexOf(value) === index);
  const end = terminal
    ? `${terminal.service_name || terminal.provider || terminal.model || terminal.source_type} · ${journeyOutcomeLabel(terminal.outcome || "unknown")}`
    : t("m1015");

  function openNode(node: TraceJourneyNode) {
    if (node.execution_id === executionID) {
      onSelect(node.event_id);
      return;
    }
    const params = new URLSearchParams({ as_of: asOf, evidence: node.event_id });
    navigate(`/executions/${encodeURIComponent(node.execution_id)}?${params.toString()}`);
  }

  return (
    <section className="trace-journey" aria-labelledby="trace-journey-title">
      <div className="trace-journey-heading">
        <div>
          <span className="eyebrow">{tx("m1016")}</span>
          <h2 id="trace-journey-title">{tx("m1017")}</h2>
          <p>{tx("m1018")}</p>
        </div>
        <div className="trace-journey-integrity">
          <strong>{tx("m1019", { value0: journey.integrity.execution_count })}</strong>
          <span>{tx("m1020", { value0: journey.integrity.resolved_parent_count, value1: journey.integrity.declared_parent_count })}</span>
          {journey.integrity.cycle_detected ? <span className="trace-cycle-warning">{tx("m1021")}</span> : null}
        </div>
      </div>

      <dl className="trace-journey-summary">
        <div><dt>{tx("m1022")}</dt><dd>{actor}</dd></div>
        <div><dt>{tx("m1023")}</dt><dd>{action}</dd></div>
        <div><dt>{tx("m1024")}</dt><dd>{systems.length > 0 ? systems.join(pathIsFullyDeclared ? " → " : " · ") : t("m1013")}</dd></div>
        <div><dt>{tx("m1025")}</dt><dd>{end}</dd></div>
      </dl>

      <ol className="trace-journey-path" aria-label={t("m1026")}>
        {path.map((node, index) => {
          const parentResolved = journey.edges.some((edge) => edge.to_event_id === node.event_id);
          const nextNode = path[index + 1];
          const nextEdgeDeclared = Boolean(nextNode) && declaredEdges.has(`${node.event_id}\u0000${nextNode.event_id}`);
          return (
            <li key={node.event_id}>
              <button type="button" onClick={() => openNode(node)}>
                <span className="trace-journey-index">{String(index + 1).padStart(2, "0")}</span>
                <span className="trace-journey-system">{node.service_name || node.provider || node.source_type}</span>
                <strong>{node.action || node.model || "model request"}</strong>
                <small>{node.provider || "provider unknown"} · {node.model || "model unknown"}</small>
                <code>{shortRef(node.execution_id, 10, 6)}</code>
                {node.parent_span_id ? (
                  <span className={parentResolved ? "trace-parent-resolved" : "trace-parent-unresolved"}>
                    {parentResolved ? t("m1027") : t("m1028")}
                  </span>
                ) : <span className="trace-root-label">{tx("m1029")}</span>}
              </button>
              {nextNode ? (
                <span
                  className={nextEdgeDeclared ? "trace-journey-arrow" : "trace-journey-break"}
                  aria-label={nextEdgeDeclared ? t("m1030") : t("m1031")}
                >
                  {nextEdgeDeclared ? "→" : "⋯"}
                </span>
              ) : null}
            </li>
          );
        })}
      </ol>

      <details className="trace-journey-all"><summary>{tx("m1032", { value0: journey.nodes.length })}</summary>
        <p>{tx("m1033")}</p>
        <TraceJourneyGraph journey={journey} onSelect={openNode} />
      </details>
      <p className="trace-journey-boundary">
        <strong>
          {journey.integrity.cycle_detected
            ? t("m1034")
            : journey.integrity.visible_parent_resolution_complete
              ? t("m1035")
              : t("m1036")}
        </strong>
        <span>{tx("m1038", { value0: journey.evidence_boundary, value1: outsidePathCount > 0 ? t("m1037", { value0: outsidePathCount }) : "" })}</span>
      </p>
    </section>
  );
}

function declaredTraceJourneyPath(journey: TraceJourney): TraceJourneyNode[] {
  const path = declaredPath(journey.nodes, journey.edges.map(e => ({ from: e.from_event_id, to: e.to_event_id })));
  return path.length >= 2 ? path : journey.nodes;
}

function TraceNode({
  node,
  index,
  selected,
  declaredParentID,
  onSelect,
}: {
  node: InvestigationNode;
  index: number;
  selected: boolean;
  declaredParentID?: string;
  onSelect: () => void;
}) {
  useLocale();
  const lane = node.kind === "retry_scheduled" || node.kind === "fallback_selected"
    ? "fallback"
    : node.kind === "provider_failed"
      ? "failed"
      : "main";
  return (
    <li className={`trace-step trace-lane-${lane}${selected ? " trace-step-selected" : ""}`}>
      <span className="trace-step-marker" aria-hidden="true">
        {String(index + 1).padStart(2, "0")}
      </span>
      <button type="button" onClick={onSelect} aria-pressed={selected}>
        <div className="trace-step-heading">
          <div>
            <span className={`trace-kind trace-kind-${node.kind}`}>
              {nodeLabels[node.kind] || node.kind}
            </span>
            <strong>{node.provider || node.model || node.stage}</strong>
          </div>
          <time dateTime={node.effective_at}>{formatShortTime(node.effective_at)}</time>
        </div>
        <div className="trace-step-facts">
          {node.status_code !== undefined ? <span>HTTP {node.status_code}</span> : null}
          {node.latency_ms !== undefined ? <span>{node.latency_ms} ms</span> : null}
          {node.decision_result ? <span>{node.decision_result}</span> : null}
          {node.total_tokens !== undefined ? <span>{node.total_tokens} tokens</span> : null}
          {node.cost_micros ? <span>{formatMicroCNY(node.cost_micros)}</span> : null}
        </div>
        <div className="trace-step-foot">
          <span>{tx("m0475")}</span>
          {declaredParentID ? (
            <span title={declaredParentID}>{tx("m1039", { value0: shortRef(declaredParentID, 6, 4) })}</span>
          ) : (
            <span>{tx("m1040")}</span>
          )}
        </div>
        <span className={`proof-basis proof-basis-${node.proof_basis || "unknown"}`}>
          {proofBasisLabel(node.proof_basis)}
        </span>
      </button>
      <details className="technical-reference"><summary>{tx("m1041")}</summary><p>{t("m1042")}<code>{node.event_id}</code></p><p>{t("m1043")}<code>{node.event_type}</code> {t("m1044")}<code>{node.proof_basis || "unknown"}</code> {t("m1045")}<code>{node.outcome_status}</code></p>{declaredParentID ? <p>{t("m1046")}<code>{declaredParentID}</code></p> : null}</details>
    </li>
  );
}

function CausalGraph({
  projection,
  selectedEventID,
  onSelect,
}: {
  projection: InvestigationProjection;
  selectedEventID?: string;
  onSelect: (eventID: string) => void;
}) {
  useLocale();
  const nodes = projection.nodes.filter((node) => node.kind !== "capture_gap");
  const positions = graphPositions(nodes, projection.edges);
  const height = Math.max(240, ...positions.map((position) => position.y + 92));
  const width = Math.max(760, ...positions.map((position) => position.x + 180));
  const nodeIDs = new Set(nodes.map((node) => node.event_id));
  const incoming = new Set(projection.edges.map((edge) => edge.to));

  function keyboardSelect(event: KeyboardEvent<SVGGElement>, eventID: string) {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onSelect(eventID);
    }
  }

  return (
    <div className="causal-graph-shell">
      <svg
        className="causal-graph"
        role="img"
        aria-labelledby="causal-graph-title causal-graph-description"
        viewBox={`0 0 ${width} ${height}`}
      >
        <title id="causal-graph-title">{tx("m1047")}</title>
        <desc id="causal-graph-description">{tx("m1048")}</desc>
        <defs>
          <marker id="graph-arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto">
            <path d="M0,0 L8,4 L0,8 z" className="graph-arrow" />
          </marker>
        </defs>
        {projection.edges.map((edge) => {
          const from = positions.find((position) => position.id === edge.from);
          const to = positions.find((position) => position.id === edge.to);
          if (!from || !to || !nodeIDs.has(edge.from) || !nodeIDs.has(edge.to)) return null;
          return (
            <g key={`${edge.from}:${edge.to}`}>
              <line
                className="graph-edge"
                x1={from.x + 150}
                y1={from.y + 34}
                x2={to.x}
                y2={to.y + 34}
                markerEnd="url(#graph-arrow)"
              />
              <text className="graph-edge-label" x={(from.x + to.x + 150) / 2} y={(from.y + to.y) / 2 + 28}>
                declared parent
              </text>
            </g>
          );
        })}
        {nodes.map((node) => {
          const position = positions.find((candidate) => candidate.id === node.event_id)!;
          const missingParent = Boolean(node.declared_parent_event_id) && !incoming.has(node.event_id);
          return (
            <g
              key={node.event_id}
              className={`graph-node graph-node-${node.severity}${selectedEventID === node.event_id ? " graph-node-selected" : ""}${missingParent ? " graph-node-missing-parent" : ""}`}
              transform={`translate(${position.x} ${position.y})`}
              role="button"
              tabIndex={0}
              aria-label={`${nodeLabels[node.kind] || node.event_type}; ${proofBasisLabel(node.proof_basis)}${missingParent ? "; parent missing" : ""}`}
              onClick={() => onSelect(node.event_id)}
              onKeyDown={(event) => keyboardSelect(event, node.event_id)}
            >
              <rect width="150" height="68" rx="7" />
              <text className="graph-node-index" x="12" y="18">{String(position.index + 1).padStart(2, "0")}</text>
              <text className="graph-node-title" x="12" y="38">{truncateSVG(nodeLabels[node.kind] || node.kind, 18)}</text>
              <text className="graph-node-meta" x="12" y="56">{truncateSVG(node.provider || node.model || node.stage, 20)}</text>
              {missingParent ? <text className="graph-missing-label" x="75" y="-8">{tx("m1049")}</text> : null}
            </g>
          );
        })}
      </svg>
      <ol className="graph-accessible-list" aria-label={t("m1050")}>
        {nodes.map((node) => {
          const parent = projection.edges.find((edge) => edge.to === node.event_id)?.from;
          return (
            <li key={node.event_id}>
              <button onClick={() => onSelect(node.event_id)}>
                <strong>{nodeLabels[node.kind] || node.event_type}</strong>
                <span>{parent ? t("m1051", { value0: shortRef(parent) }) : node.declared_parent_event_id ? t("m1052") : t("m1053")}</span>
              </button>
            </li>
          );
        })}
      </ol>
    </div>
  );
}

function CoverageItem({
  coverage,
  onSelect,
}: {
  coverage: StageCoverage;
  onSelect: (eventID: string) => void;
}) {
  useLocale();
  const firstEvidence = coverage.evidence_event_ids?.[0];
  const content = (
    <>
      <span className="coverage-icon" aria-hidden="true">
        {coverage.status === "present" ? "✓" : coverage.status === "gap" ? "!" : coverage.status === "not_applicable" ? "—" : "?"}
      </span>
      <strong>{stageLabels[coverage.stage] || coverage.stage}</strong>
      <small>{coverageLabels[coverage.status] || coverage.status}</small>
    </>
  );
  return firstEvidence ? (
    <button
      type="button"
      className={`coverage-item coverage-${coverage.status}`}
      onClick={() => onSelect(firstEvidence)}
      title={t("m1054")}
    >
      {content}
    </button>
  ) : (
    <div className={`coverage-item coverage-${coverage.status}`}>{content}</div>
  );
}

function FactsPanel({
  node,
  event,
}: {
  node: InvestigationNode;
  event?: EvidenceEventDetail;
}) {
  useLocale();
  const facts: Array<[string, unknown]> = [
    [t("m0914"), event?.capability?.provider || node.provider],
    [t("m1055"), event?.capability?.model || node.model],
    [t("m1056"), event?.capability?.requested_model || node.requested_model],
    [t("m1057"), event?.identity?.actor_type || node.actor_type],
    [t("m1058"), event?.identity?.actor_ref || node.actor_ref],
    [t("m1059"), event?.identity?.org_id || node.org_id],
    [t("m1060"), event?.identity?.project_id || node.project_id],
    [t("m0552"), event?.decision?.decision_result || node.decision_result],
    [t("m1061"), event?.decision?.decision_reason || node.decision_reason],
    ["HTTP", event?.outcome?.status_code ?? node.status_code],
    [t("m0554"), journeyOutcomeLabel(event?.outcome?.outcome_status || node.outcome_status || "unknown")],
    [t("m1062"), node.latency_ms !== undefined ? `${node.latency_ms} ms` : undefined],
    [t("m0953"), node.total_tokens],
    [t("m1063"), formatDateTime(node.effective_at)],
  ];
  return (
    <dl className="inspector-facts">
      {facts
        .filter(([, value]) => value !== undefined && value !== null && value !== "")
        .map(([label, value]) => (
          <div key={label}>
            <dt>{label}</dt>
            <dd>{displayValue(value)}</dd>
          </div>
        ))}
    </dl>
  );
}

function EvidencePanel({
  node,
  event,
  copyState,
  onCopy,
}: {
  node: InvestigationNode;
  event?: EvidenceEventDetail;
  copyState: "idle" | "copied" | "failed";
  onCopy: () => void;
}) {
  useLocale();
  return (
    <div className="citation-panel">
      <p>{tx("m1064")}</p>
      <dl>
        <CitationFact label={t("m1065")} value={node.citation.evidence_event_id} />
        <CitationFact label={t("m0097")} value={node.citation.source_ref} />
        <CitationFact label={t("m1066")} value={node.citation.event_hash} />
        <CitationFact label={t("m1067")} value={node.citation.deep_link} />
        <CitationFact label={t("m1068")} value={event?.source.schema_version || t("m1069")} />
        <CitationFact label={t("m1070")} value={event?.source.fidelity_level || t("m1069")} />
      </dl>
      <button type="button" className="button button-secondary" onClick={onCopy}>
        {copyState === "copied" ? t("m1071") : copyState === "failed" ? t("m1072") : t("m1073")}
      </button>
    </div>
  );
}

function GapsPanel({ gaps }: { gaps: CaptureGap[] }) {
  useLocale();
  if (gaps.length === 0) {
    return (
      <div className="inspector-no-gaps">
        <strong>{tx("m1074")}</strong>
        <p>{tx("m1075")}</p>
      </div>
    );
  }
  return (
    <div className="inspector-gap-list">
      {gaps.map((gap) => (
        <article key={gap.gap_id}>
          <strong>{gap.gap_type}</strong>
          <p>{gap.affected_claim}</p>
          <small>{gap.gap_reason}</small>
          <span className="gap-resolution">{gap.resolution_state || "open"}</span>
        </article>
      ))}
    </div>
  );
}

function CitationFact({ label, value }: { label: string; value: string }) {
  useLocale();
  return (
    <div>
      <dt>{label}</dt>
      <dd><code title={value}>{value}</code></dd>
    </div>
  );
}

function Metric({
  label,
  value,
  alert = false,
}: {
  label: string;
  value: string | number;
  alert?: boolean;
}) {
  useLocale();
  return (
    <div>
      <dt>{label}</dt>
      <dd className={alert ? "metric-alert" : undefined}>{value}</dd>
    </div>
  );
}

function presentationLabel(mode: InvestigationProjection["presentation_mode"]) {
  switch (mode) {
    case "causal_trace":
      return t("m1076");
    case "chronology":
      return t("m1077");
    case "terminal_snapshot":
      return t("m1078");
    default:
      return t("m1079");
  }
}

function headline(
  projection: InvestigationProjection,
  failedNode?: InvestigationNode,
  fallbackNode?: InvestigationNode,
  fallbackSequenceIsDeclaredPath = false,
) {
  if (projection.summary_claims.some((claim) => claim.code === "primary_provider_failed_then_fallback_succeeded")) {
    return fallbackSequenceIsDeclaredPath
      ? t("m1080")
      : t("m1081");
  }
  if (projection.presentation_mode === "terminal_snapshot") {
    return t("m1082");
  }
  if (failedNode && fallbackNode) {
    return t("m1083");
  }
  if (projection.execution.outcome === "failure") {
    return t("m1084");
  }
  if (projection.execution.outcome === "success") {
    return t("m1085");
  }
  return t("m1086");
}

function subheadline(
  projection: InvestigationProjection,
  failedNode?: InvestigationNode,
  finalNode?: InvestigationNode,
  fallbackSequence = false,
  fallbackSequenceIsDeclaredPath = false,
) {
  if (fallbackSequence) {
    return [
      failedNode?.provider || t("m1087"),
      failedNode?.status_code ? `HTTP ${failedNode.status_code}` : t("m1088"),
      t("m1089"),
      finalNode?.provider || t("m1090"),
      t("m1091"),
      fallbackSequenceIsDeclaredPath ? t("m1092") : t("m1093"),
    ].join(" ");
  }
  if (projection.presentation_mode === "terminal_snapshot") {
    return t("m1094", { value0: projection.nodes.length, value1: projection.gaps.length });
  }
  return t("m1095", { value0: projection.edges.length, value1: projection.gaps.length, value2: formatDateTime(projection.as_of) });
}

function metricLatency(node?: InvestigationNode) {
  return node?.latency_ms !== undefined ? `${node.latency_ms} ms` : t("m0100");
}

function gapsForNode(
  projection: InvestigationProjection,
  node?: InvestigationNode,
) {
  if (!node) {
    return [];
  }
  const direct = projection.gaps.filter((gap) => gap.evidence_event_id === node.event_id);
  if (direct.length > 0) {
    return direct;
  }
  const stage = coverageStageForNode(node);
  const gapIDs = projection.stage_coverage.find((item) => item.stage === stage)?.gap_ids ?? [];
  return projection.gaps.filter((gap) => gapIDs.includes(gap.gap_id));
}

function coverageStageForNode(node: InvestigationNode) {
  switch (node.kind) {
    case "retry_scheduled":
    case "fallback_selected":
      return "retry_fallback";
    case "execution_completed":
      return "logging";
    default:
      return node.stage;
  }
}

function proofBasisLabel(value?: string) {
  switch (value) {
    case "direct":
      return t("m1096");
    case "source_declared":
    case "declared":
      return t("m1097");
    case "deterministic":
    case "derived":
      return t("m0950");
    case "gap":
      return t("m1098");
    default:
      return t("m1099");
  }
}

function executionJourney(projection: InvestigationProjection) {
  const nodes = projection.nodes.filter((node) => node.kind !== "capture_gap");
  const declaredPath = longestDeclaredPath(nodes, projection.edges);
  const first = declaredPath[0] || nodes[0];
  const terminal = nodes[nodes.length - 1];
  const actor = first?.actor_ref || first?.actor_type;
  const declaredPathAvailable = projection.edges.length > 0 && declaredPath.length >= 2;
  const systems = declaredPath
    .map((node) => node.provider || node.model || stageLabels[node.stage] || node.stage)
    .filter((value, index, values) => Boolean(value) && (index === 0 || value !== values[index - 1]));
  const terminalOnPath = Boolean(terminal) && declaredPath.some((node) => node.event_id === terminal.event_id);
  const terminalSystem = terminal?.provider || terminal?.model || (terminal ? stageLabels[terminal.stage] || terminal.stage : undefined);
  const terminalOutcome = terminal?.outcome_status || projection.execution.outcome;
  return [
    {
      label: t("m1100"),
      value: actor || t("m1013"),
      note: actor ? t("m1101") : t("m1102"),
      uncertain: !actor,
    },
    {
      label: t("m1103"),
      value: t("m1104"),
      note: t("m1105", { value0: shortRef(projection.execution.request_id, 12, 8) }),
      uncertain: false,
    },
    {
      label: t("m1024"),
      value: declaredPathAvailable && systems.length > 0 ? systems.join(" → ") : t("m1106"),
      note: declaredPathAvailable ? t("m1107") : t("m1108"),
      uncertain: !declaredPathAvailable,
    },
    {
      label: t("m1109"),
      value: `${terminalSystem || t("m1015")} · ${journeyOutcomeLabel(terminalOutcome)}`,
      note: terminalOnPath ? t("m1110") : t("m1111"),
      uncertain: !terminalOnPath,
    },
  ];
}

function journeyOutcomeLabel(outcome: string) {
  return ({ success: t("m1091"), failure: t("m1088"), partial: t("m1112"), unknown: t("m0193") } as Record<string, string>)[outcome] || outcome;
}

function executionStory(projection: InvestigationProjection) {
  const nodes = projection.nodes.filter((node) => node.kind !== "capture_gap");
  if (nodes.length === 0) {
    return t("m1113");
  }
  const declaredPath = longestDeclaredPath(nodes, projection.edges);
  const first = declaredPath[0] || nodes[0];
  const last = nodes[nodes.length - 1];
  const actor = first?.actor_ref || first?.actor_type || t("m1114");
  const end = last?.outcome_status || projection.execution.outcome;
  if (projection.edges.length === 0 || declaredPath.length < 2) {
    return t("m1115", { value0: actor, value1: nodes.length, value2: journeyOutcomeLabel(end || "unknown") });
  }
  const path = declaredPath
    .map((node) => node.provider || node.model || nodeLabels[node.kind] || node.stage)
    .filter((value, index, values) => values.indexOf(value) === index)
    .join(" → ");
  const terminalOnPath = declaredPath.some((node) => node.event_id === last?.event_id);
  return terminalOnPath
    ? t("m1116", { value0: actor, value1: path, value2: journeyOutcomeLabel(end || "unknown") })
    : t("m1117", { value0: actor, value1: path, value2: journeyOutcomeLabel(end || "unknown") });
}

function isDeclaredPath(
  projection: InvestigationProjection,
  evidenceEventIDs: string[],
) {
  if (evidenceEventIDs.length < 2) return false;
  const edges = new Set(projection.edges.map((edge) => `${edge.from}\u0000${edge.to}`));
  return evidenceEventIDs
    .slice(1)
    .every((eventID, index) => edges.has(`${evidenceEventIDs[index]}\u0000${eventID}`));
}

function longestDeclaredPath(nodes: InvestigationNode[], edges: InvestigationProjection["edges"]) {
  return declaredPath(nodes, edges);
}

function completenessAxes(projection: InvestigationProjection) {
  if (projection.completeness?.length) return projection.completeness;
  return [
    {
      axis: "source",
      status: projection.integrity.source_profile_complete ? "present" : "gap",
      basis: projection.integrity.source_profile_complete ? "source profile reports required coverage" : "source coverage is incomplete",
    },
    {
      axis: "import",
      status: projection.integrity.import_complete ? "present" : "gap",
      basis: projection.integrity.import_complete ? "import converged for this item" : "import convergence is incomplete",
    },
    {
      axis: "projection",
      status: projection.integrity.projection_complete ? "present" : "gap",
      basis: projection.integrity.projection_complete ? "all bounded Evidence projected" : "projection truncated or inconsistent",
    },
    {
      axis: "causal",
      status: projection.integrity.causal_complete ? "present" : "gap",
      basis: projection.integrity.causal_complete ? "all declared parents resolved" : `${projection.integrity.unresolved_parent_count} declared parents unresolved`,
    },
  ];
}

function axisLabel(axis: string) {
  return ({ source: t("m0472"), import: t("m1118"), projection: t("m1119"), causal: t("m1120") } as Record<string, string>)[axis] || axis;
}


function truncateSVG(value: string, length: number) {
  return value.length <= length ? value : `${value.slice(0, length - 1)}…`;
}


function TraceJourneyGraph({ journey, onSelect }: { journey: TraceJourney; onSelect: (node: TraceJourneyNode) => void }) {
  useLocale();
  const marker = useId().replaceAll(":", "");
  const edges = journey.edges.map(e => ({ from: e.from_event_id, to: e.to_event_id }));
  const positions = graphPositions(journey.nodes, edges);
  const byID = new Map(positions.map(p => [p.id, p]));
  const height = Math.max(240, ...positions.map(p => p.y + 92)), width = Math.max(760, ...positions.map(p => p.x + 180));
  return <div className="causal-graph-shell" role="group" aria-label={t("m1121")}><svg className="causal-graph" style={{ minWidth: width, minHeight: height }} viewBox={`0 0 ${width} ${height}`}>
    <defs><marker id={marker} markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto"><path d="M0,0 L8,4 L0,8 z" className="graph-arrow" /></marker></defs>
    {edges.map(edge => { const from = byID.get(edge.from), to = byID.get(edge.to); return from && to ? <line key={`${edge.from}:${edge.to}`} className="graph-edge" aria-label={t("m1122")} x1={from.x + 150} y1={from.y + 34} x2={to.x} y2={to.y + 34} markerEnd={`url(#${marker})`} /> : null; })}
    {journey.nodes.map(node => { const p = byID.get(node.event_id)!; return <g key={node.event_id} className="graph-node" transform={`translate(${p.x} ${p.y})`} role="button" tabIndex={0}
      aria-label={`${node.service_name || node.source_type}；${node.action || node.model || node.event_id}；${node.event_id}`} onClick={() => onSelect(node)}
      onKeyDown={e => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onSelect(node); } }}>
      <rect width="150" height="68" rx="7" /><title>{`${node.service_name || node.source_type}；${node.action || node.model || node.event_id}；${node.event_id}`}</title>
      <text className="graph-node-title" x="10" y="23">{truncateSVG(node.service_name || node.source_type, 22)}</text><text className="graph-node-meta" x="10" y="43">{truncateSVG(node.action || node.model || node.event_id, 22)}</text>
      <text x="10" y="59" className="graph-node-meta">{shortRef(node.execution_id, 10, 6)}</text>
    </g>; })}
  </svg><div className="graph-accessible-list">
    {journey.nodes.map(node => <button key={node.event_id} onClick={() => onSelect(node)} aria-label={`${node.service_name || node.source_type}；${node.action || node.model || node.event_id}；${node.event_id}`}>
      <strong>{node.service_name || node.source_type}</strong><span>{node.action || node.model || node.event_id}</span><span>{shortRef(node.execution_id, 10, 6)}</span>
    </button>)}
    <ul aria-label={t("m1123")}>{journey.edges.map(edge => {
      const from = journey.nodes.find(node => node.event_id === edge.from_event_id);
      const to = journey.nodes.find(node => node.event_id === edge.to_event_id);
      if (!from || !to) return null;
      return <li key={`${edge.from_event_id}:${edge.to_event_id}`}>{from.service_name || from.source_type} → {to.service_name || to.source_type}</li>;
    })}</ul>
  </div></div>;
}
