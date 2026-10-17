import { KeyboardEvent, useEffect, useMemo, useState } from "react";

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

type InspectorTab = "facts" | "evidence" | "gaps";

const stageLabels: Record<string, string> = {
  identity: "身份",
  routing: "路由",
  provider: "Provider",
  retry_fallback: "Retry / Fallback",
  billing: "计费",
  usage: "用量",
  logging: "日志",
};

const coverageLabels: Record<string, string> = {
  present: "有证据",
  gap: "存在缺口",
  not_applicable: "不适用",
  unknown: "未知",
  complete: "完整",
  incomplete: "不完整",
  not_available: "不可用",
};

const nodeLabels: Record<string, string> = {
  identity_bound: "身份上下文已绑定",
  route_selected: "选择主路由",
  provider_failed: "主 Provider 调用失败",
  provider_succeeded: "Provider 调用成功",
  retry_scheduled: "重试已安排",
  fallback_selected: "切换 Fallback 路由",
  billing_recorded: "计费结果已记录",
  execution_completed: "执行完成并记录用量",
  capture_gap: "检测到证据缺口",
  evidence_observed: "记录 Evidence",
};

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
  const [inspectorTab, setInspectorTab] = useState<InspectorTab>("facts");
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
            <span className="eyebrow">Current execution · deterministic investigation</span>
            <span className="presentation-chip">{presentationLabel(projection.presentation_mode)}</span>
            {projection.execution.proof_class === "synthetic_fixture" ? (
              <span className="demo-data-chip">Demo data · synthetic</span>
            ) : null}
          </div>
          <h2 id="investigation-headline">
            {headline(projection, failedNode, fallbackNode, fallbackSequenceIsDeclaredPath)}
          </h2>
          <p>{subheadline(projection, failedNode, finalNode, fallbackSequence, fallbackSequenceIsDeclaredPath)}</p>
          <ol className="journey-strip" aria-label="执行路径摘要">
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
              <strong>{fallbackSequenceIsDeclaredPath ? "来源声明路径" : "确定性推导"}</strong>
              <span>{fallbackSequenceClaim.derivation_basis}</span>
            </p>
          ) : null}
          {!integrityComplete ? (
            <div className="overview-badges">
              <span className="integrity-warning">四轴完整性未闭合，不生成完整因果结论</span>
            </div>
          ) : null}
        </div>
        <dl className="investigation-metrics">
          <Metric label="总耗时" value={metricLatency(finalNode)} />
          <Metric label="Tokens" value={finalNode?.total_tokens ?? "未提供"} />
          <Metric
            label="精确成本"
            value={finalNode?.cost_micros !== undefined ? formatMicroCNY(finalNode.cost_micros) : "未提供"}
          />
          <Metric
            label="证据缺口"
            value={projection.gaps.length}
            alert={projection.gaps.length > 0}
          />
          <Metric
            label="本 execution 步骤 / Gaps"
            value={`${projection.nodes.length} / ${projection.gaps.length}`}
          />
        </dl>
      </section>

      {projection.presentation_mode === "terminal_snapshot" ? (
        <section className="terminal-snapshot-notice" role="status">
          <strong>来源未提供可验证 Trace</strong>
          <p>以下内容是终态快照，不会把 usage、billing 或 gap 伪装成因果节点。</p>
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
            <span className="eyebrow">Source-backed stage coverage</span>
            <h2 id="coverage-title">证据覆盖与完整性</h2>
          </div>
          <span className={`coverage-summary-state${integrityComplete ? " coverage-summary-complete" : " coverage-summary-limited"}`}>
            {coveredStages}/{projection.stage_coverage.length} 阶段有 Evidence · {integrityComplete ? "四轴完整" : "存在未闭合轴"}
          </span>
        </summary>
        <div className="coverage-content" aria-labelledby="coverage-title">
          <p>绿色表示有 Evidence；黄色表示明确缺口；灰色不会被解释为成功。</p>
          <div className="coverage-strip">
            {projection.stage_coverage.map((coverage) => (
              <CoverageItem key={coverage.stage} coverage={coverage} onSelect={onSelect} />
            ))}
          </div>
          <div className="completeness-grid" aria-label="四轴完整性">
            {completenessAxes(projection).map((axis) => (
              <article key={axis.axis} className={`axis-card axis-${axis.status}`}>
                <span>{axisLabel(axis.axis)}</span>
                <strong>{coverageLabels[axis.status] || axis.status}</strong>
                <p>{axis.basis}</p>
              </article>
            ))}
          </div>
        </div>
      </details>

      <div className={`investigation-workbench investigation-workbench-${mode}`}>
        <section className="trace-story" aria-labelledby="trace-story-title">
          <div className="section-heading">
            <div>
              <span className="eyebrow">Evidence path</span>
              <h2 id="trace-story-title">{mode === "graph" ? "来源声明的因果图" : "可阅读的执行路径"}</h2>
            </div>
            <p>
              当前 execution 有 {projection.edges.length} 条来源声明的因果边；时间相邻不会自动连线。
            </p>
          </div>
          {projection.nodes.length === 0 ? (
            <p className="trace-empty">当前快照没有可显示的 Evidence node。</p>
          ) : mode === "graph" ? (
            <CausalGraph
              projection={projection}
              selectedEventID={selectedEventID}
              onSelect={onSelect}
            />
          ) : (
            <ol className="trace-rail" aria-label="执行证据路径">
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
              <span className="eyebrow">Selected Evidence</span>
              <h2 id="inspector-title">节点调查</h2>
            </div>
            {selectedNode ? <code>{shortRef(selectedNode.event_id, 8, 6)}</code> : null}
          </div>
          {!selectedNode ? (
            <p className="inspector-empty">选择左侧节点查看可引用事实。</p>
          ) : (
            <>
              <div className="inspector-summary">
                <strong>{nodeLabels[selectedNode.kind] || selectedNode.event_type}</strong>
                <span className={`severity-chip severity-${selectedNode.severity}`}>
                  {selectedNode.severity}
                </span>
                <small>{selectedNode.event_type}</small>
              </div>
              <div className="inspector-tabs" role="tablist" aria-label="节点调查视图">
                {([
                  ["facts", "Facts"],
                  ["evidence", "Evidence"],
                  ["gaps", `Gaps ${selectedGaps.length}`],
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
              {eventLoading ? <p className="inspector-loading">正在读取节点 Evidence…</p> : null}
              {eventError ? <p className="inspector-error">节点明细暂时不可用；投影事实仍保持原样。</p> : null}
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
              <div className="case-capture">
                <span className="eyebrow">Add to Case</span>
                {cases.length > 0 ? (
                  <>
                    <label htmlFor="case-target">目标 Case
                      <select id="case-target" value={selectedCaseID} onChange={(event) => {
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
                      {caseState === "saving" ? "正在引用…" : "引用 Evidence 与相关 Gaps"}
                    </button>
                    <p role="status">
                      {caseState === "saved"
                        ? "已加入 Case；Evidence 本体未被修改。"
                        : caseState === "error"
                          ? "加入失败；Case 与 Evidence 保持原状。"
                          : "同时固定 execution as_of、event hash 与 source ref。"}
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
                            ? "正在引用全部 Gaps…"
                            : `单独引用全部 ${projection.gaps.length} 个 execution Gaps`}
                        </button>
                        <p role="status">
                          {gapCaseState === "saved"
                            ? "全部 execution Gaps 已作为限制加入 Case。"
                            : gapCaseState === "error"
                              ? "Gap 引用失败；现有 Case revision 未被伪报为成功。"
                              : "Gap 是一等证据记录，不会被压缩成 record 标签。"}
                        </p>
                      </>
                    ) : null}
                  </>
                ) : (
                  <button className="button button-secondary" type="button" onClick={() => navigate("/cases")}>先创建 Case</button>
                )}
              </div>
            </>
          )}
        </section>
      </div>

      <section className="unprovable-section" aria-labelledby="unprovable-title">
        <div className="section-heading">
          <div><span className="eyebrow">Unknown / Unprovable</span><h2 id="unprovable-title">不能证明的动作</h2></div>
          <p>未知不是失败，也不是零；它保留在调查结论中。</p>
        </div>
        <div className="unprovable-grid">
          {(projection.unprovable_actions ?? []).map((item) => (
            <article key={item.code}>
              <span className="missing-mark" aria-hidden="true">∕</span>
              <div><strong>{item.statement}</strong><code>{item.code}</code><small>{item.gap_ids?.length ?? 0} linked gaps</small></div>
            </article>
          ))}
          {(projection.unprovable_actions?.length ?? 0) === 0 ? (
            <p className="quiet-copy">当前投影未列出额外 unprovable action；这不扩大来源覆盖范围。</p>
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
  if (unavailable) {
    return (
      <section className="trace-journey trace-journey-unavailable" aria-label="跨系统 Trace Journey 状态">
        <div>
          <span className="eyebrow">Cross-execution Trace Journey</span>
          <h2>跨系统路径暂不可读</h2>
        </div>
        <p>当前 execution 的既有证据投影仍可调查；系统不会用时间相邻关系补造跨系统路径。</p>
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
  const actor = root?.actor_ref || root?.actor_type || "来源未提供";
  const action = root?.action || "AI model request";
  const systems = path
    .map((node) => node.service_name || node.provider || node.model || node.source_type)
    .filter((value, index, values) => Boolean(value) && values.indexOf(value) === index);
  const end = terminal
    ? `${terminal.service_name || terminal.provider || terminal.model || terminal.source_type} · ${journeyOutcomeLabel(terminal.outcome || "unknown")}`
    : "终点未知";

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
          <span className="eyebrow">Source-declared · cross-execution</span>
          <h2 id="trace-journey-title">跨系统 Trace Journey</h2>
          <p>从谁发起、做了什么、经过哪些系统，到哪里结束；只连接来源明确声明的父子 Span。</p>
        </div>
        <div className="trace-journey-integrity">
          <strong>{journey.integrity.execution_count} executions</strong>
          <span>{journey.integrity.resolved_parent_count}/{journey.integrity.declared_parent_count} parents resolved</span>
          {journey.integrity.cycle_detected ? <span className="trace-cycle-warning">cycle detected</span> : null}
        </div>
      </div>

      <dl className="trace-journey-summary">
        <div><dt>谁发起</dt><dd>{actor}</dd></div>
        <div><dt>做了什么</dt><dd>{action}</dd></div>
        <div><dt>经过系统</dt><dd>{systems.length > 0 ? systems.join(pathIsFullyDeclared ? " → " : " · ") : "来源未提供"}</dd></div>
        <div><dt>到哪里结束</dt><dd>{end}</dd></div>
      </dl>

      <ol className="trace-journey-path" aria-label="跨 execution 数据流路径">
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
                    {parentResolved ? "source-declared parent" : "parent unresolved"}
                  </span>
                ) : <span className="trace-root-label">trace root</span>}
              </button>
              {nextNode ? (
                <span
                  className={nextEdgeDeclared ? "trace-journey-arrow" : "trace-journey-break"}
                  aria-label={nextEdgeDeclared ? "来源声明父子关系" : "未声明节点关系"}
                >
                  {nextEdgeDeclared ? "→" : "⋯"}
                </span>
              ) : null}
            </li>
          );
        })}
      </ol>

      <p className="trace-journey-boundary">
        <strong>
          {journey.integrity.cycle_detected
            ? "来源父子关系存在环"
            : journey.integrity.visible_parent_resolution_complete
              ? "可见父子关系已闭合"
              : "可见父子关系未闭合"}
        </strong>
        <span>
          {journey.evidence_boundary}；bounded export 不声明完整来源时间线
          {outsidePathCount > 0 ? `；另有 ${outsidePathCount} 个可见节点不在主声明路径中` : ""}。
        </span>
      </p>
    </section>
  );
}

function declaredTraceJourneyPath(journey: TraceJourney): TraceJourneyNode[] {
  const nodeByID = new Map(journey.nodes.map((node) => [node.event_id, node]));
  const order = new Map(journey.nodes.map((node, index) => [node.event_id, index]));
  const adjacency = new Map<string, string[]>();
  const incoming = new Set<string>();
  for (const edge of journey.edges) {
    if (!nodeByID.has(edge.from_event_id) || !nodeByID.has(edge.to_event_id)) continue;
    const children = adjacency.get(edge.from_event_id) ?? [];
    children.push(edge.to_event_id);
    children.sort((left, right) => (order.get(left) ?? 0) - (order.get(right) ?? 0));
    adjacency.set(edge.from_event_id, children);
    incoming.add(edge.to_event_id);
  }
  const roots = journey.nodes
    .map((node) => node.event_id)
    .filter((eventID) => !incoming.has(eventID));
  let longest: string[] = [];
  const visit = (eventID: string, path: string[], visited: Set<string>) => {
    if (visited.has(eventID)) return;
    const nextPath = [...path, eventID];
    if (nextPath.length > longest.length) longest = nextPath;
    const nextVisited = new Set(visited).add(eventID);
    for (const child of adjacency.get(eventID) ?? []) visit(child, nextPath, nextVisited);
  };
  for (const root of roots) visit(root, [], new Set());
  const declared = longest.map((eventID) => nodeByID.get(eventID)).filter((node): node is TraceJourneyNode => Boolean(node));
  return declared.length >= 2 ? declared : journey.nodes;
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
          <code>{node.event_type}</code>
          {declaredParentID ? (
            <span title={declaredParentID}>
              ↳ source-declared parent {shortRef(declaredParentID, 6, 4)}
            </span>
          ) : (
            <span>chronology only</span>
          )}
        </div>
        <span className={`proof-basis proof-basis-${node.proof_basis || "unknown"}`}>
          {proofBasisLabel(node.proof_basis)}
        </span>
      </button>
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
        <title id="causal-graph-title">来源声明的执行因果图</title>
        <desc id="causal-graph-description">只包含 API edges 数组中的关系；虚线边界表示声明父节点未解析。</desc>
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
              {missingParent ? <text className="graph-missing-label" x="75" y="-8">missing parent</text> : null}
            </g>
          );
        })}
      </svg>
      <ol className="graph-accessible-list" aria-label="因果图节点与来源声明关系">
        {nodes.map((node) => {
          const parent = projection.edges.find((edge) => edge.to === node.event_id)?.from;
          return (
            <li key={node.event_id}>
              <button onClick={() => onSelect(node.event_id)}>
                <strong>{nodeLabels[node.kind] || node.event_type}</strong>
                <span>{parent ? `source-declared parent ${shortRef(parent)}` : node.declared_parent_event_id ? "declared parent unresolved" : "root / no declared parent"}</span>
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
      title="查看支持该阶段的首个 Evidence event"
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
  const facts: Array<[string, unknown]> = [
    ["Provider", event?.capability?.provider || node.provider],
    ["实际模型", event?.capability?.model || node.model],
    ["请求模型", event?.capability?.requested_model || node.requested_model],
    ["Actor", event?.identity?.actor_type || node.actor_type],
    ["Actor ref", event?.identity?.actor_ref || node.actor_ref],
    ["组织", event?.identity?.org_id || node.org_id],
    ["项目", event?.identity?.project_id || node.project_id],
    ["决策", event?.decision?.decision_result || node.decision_result],
    ["决策原因", event?.decision?.decision_reason || node.decision_reason],
    ["HTTP", event?.outcome?.status_code ?? node.status_code],
    ["结果", event?.outcome?.outcome_status || node.outcome_status],
    ["延迟", node.latency_ms !== undefined ? `${node.latency_ms} ms` : undefined],
    ["Tokens", node.total_tokens],
    ["发生时间", formatDateTime(node.effective_at)],
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
  return (
    <div className="citation-panel">
      <p>以下三个字段组成可回查的 Evidence citation；不包含 raw payload。</p>
      <dl>
        <CitationFact label="Evidence event ID" value={node.citation.evidence_event_id} />
        <CitationFact label="Source ref" value={node.citation.source_ref} />
        <CitationFact label="Event hash" value={node.citation.event_hash} />
        <CitationFact label="Stable deep link" value={node.citation.deep_link} />
        <CitationFact label="Source schema" value={event?.source.schema_version || "正在读取"} />
        <CitationFact label="Fidelity" value={event?.source.fidelity_level || "正在读取"} />
      </dl>
      <button type="button" className="button button-secondary" onClick={onCopy}>
        {copyState === "copied" ? "已复制 Citation" : copyState === "failed" ? "复制失败，请手动选择" : "复制 Evidence Citation"}
      </button>
    </div>
  );
}

function GapsPanel({ gaps }: { gaps: CaptureGap[] }) {
  if (gaps.length === 0) {
    return (
      <div className="inspector-no-gaps">
        <strong>此节点没有已记录的 Capture Gap</strong>
        <p>这不等于全局完整；请同时查看阶段覆盖与 Proof。</p>
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
      return "Causal trace";
    case "chronology":
      return "Chronology";
    default:
      return "Terminal snapshot";
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
      ? "最终成功：来源声明路径显示 Fallback 完成恢复"
      : "最终成功：同一完整时间线中观察到失败与 Fallback";
  }
  if (projection.presentation_mode === "terminal_snapshot") {
    return "终态快照：当前来源没有提供 Trace";
  }
  if (failedNode && fallbackNode) {
    return "发现 Provider 失败与 Fallback，但完整恢复链尚不可证明";
  }
  if (projection.execution.outcome === "failure") {
    return "执行失败：查看失败节点与证据缺口";
  }
  if (projection.execution.outcome === "success") {
    return "执行成功：按 Evidence 路径核对关键决策";
  }
  return "执行结果未知：仅展示可验证 Evidence";
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
      failedNode?.provider || "主 Provider",
      failedNode?.status_code ? `HTTP ${failedNode.status_code}` : "失败",
      "→ retry →",
      finalNode?.provider || "Fallback Provider",
      "成功",
      fallbackSequenceIsDeclaredPath ? "（来源声明父子关系）" : "（确定性时间顺序，不代表因果边）",
    ].join(" ");
  }
  if (projection.presentation_mode === "terminal_snapshot") {
    return `保留 ${projection.nodes.length} 个终态 Evidence event 与 ${projection.gaps.length} 个显式缺口；未生成伪因果边。`;
  }
  return `${projection.edges.length} 条本 execution 显式因果边，${projection.gaps.length} 个 Capture Gap，快照 ${formatDateTime(projection.as_of)}。`;
}

function metricLatency(node?: InvestigationNode) {
  return node?.latency_ms !== undefined ? `${node.latency_ms} ms` : "未提供";
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
      return "Observed";
    case "source_declared":
    case "declared":
      return "Source-declared";
    case "deterministic":
    case "derived":
      return "Deterministic";
    case "gap":
      return "Gap / Unknown";
    default:
      return value || "Unknown";
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
      label: "发起者",
      value: actor || "来源未提供",
      note: actor ? "来自 Evidence identity" : "不会根据上下文猜测身份",
      uncertain: !actor,
    },
    {
      label: "动作",
      value: "发起 AI execution 请求",
      note: `Request ${projection.execution.request_id}`,
      uncertain: false,
    },
    {
      label: "经过系统",
      value: declaredPathAvailable && systems.length > 0 ? systems.join(" → ") : "完整路径未声明",
      note: declaredPathAvailable ? "只使用来源声明父子边" : "时间相邻不会自动连线",
      uncertain: !declaredPathAvailable,
    },
    {
      label: "终点",
      value: `${terminalSystem || "终点未知"} · ${journeyOutcomeLabel(terminalOutcome)}`,
      note: `${terminalOnPath ? "声明路径到达该终态" : "终态与声明路径关系未解析"} · ${terminalOutcome}`,
      uncertain: !terminalOnPath,
    },
  ];
}

function journeyOutcomeLabel(outcome: string) {
  return ({ success: "成功", failure: "失败", partial: "部分完成", unknown: "未知" } as Record<string, string>)[outcome] || outcome;
}

function executionStory(projection: InvestigationProjection) {
  const nodes = projection.nodes.filter((node) => node.kind !== "capture_gap");
  if (nodes.length === 0) {
    return "当前快照没有可展示的执行步骤；不会从缺失数据推断发起者、系统路径或终态。";
  }
  const declaredPath = longestDeclaredPath(nodes, projection.edges);
  const first = declaredPath[0] || nodes[0];
  const last = nodes[nodes.length - 1];
  const actor = first?.actor_ref || first?.actor_type || "发起者未知";
  const end = last?.outcome_status || projection.execution.outcome;
  if (projection.edges.length === 0 || declaredPath.length < 2) {
    return `${actor} 发起 AI execution；按时间观察到 ${nodes.length} 个步骤，但来源未声明步骤间的因果路径；终态为 ${end || "未知"}。`;
  }
  const path = declaredPath
    .map((node) => node.provider || node.model || nodeLabels[node.kind] || node.stage)
    .filter((value, index, values) => values.indexOf(value) === index)
    .join(" → ");
  const terminalOnPath = declaredPath.some((node) => node.event_id === last?.event_id);
  return terminalOnPath
    ? `${actor} 发起 AI execution；来源声明的最长可验证路径为 ${path}；该路径到达终态 ${end || "未知"}。`
    : `${actor} 发起 AI execution；来源声明的最长可验证路径为 ${path}；终态为 ${end || "未知"}，但它与该路径的父子关系未声明或未解析。`;
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

function longestDeclaredPath(
  nodes: InvestigationNode[],
  edges: InvestigationProjection["edges"],
) {
  const nodeByID = new Map(nodes.map((node) => [node.event_id, node]));
  const order = new Map(nodes.map((node, index) => [node.event_id, index]));
  const adjacency = new Map<string, string[]>();
  const incoming = new Set<string>();
  for (const edge of edges) {
    if (!nodeByID.has(edge.from) || !nodeByID.has(edge.to)) continue;
    const children = adjacency.get(edge.from) ?? [];
    children.push(edge.to);
    children.sort((left, right) => (order.get(left) ?? 0) - (order.get(right) ?? 0));
    adjacency.set(edge.from, children);
    incoming.add(edge.to);
  }
  const roots = nodes
    .map((node) => node.event_id)
    .filter((eventID) => !incoming.has(eventID));
  let longest: string[] = [];
  function visit(eventID: string, path: string[], active: Set<string>) {
    if (active.has(eventID)) return;
    const nextPath = [...path, eventID];
    if (
      nextPath.length > longest.length ||
      (nextPath.length === longest.length && nextPath.join("\u0000") < longest.join("\u0000"))
    ) {
      longest = nextPath;
    }
    const nextActive = new Set(active);
    nextActive.add(eventID);
    for (const child of adjacency.get(eventID) ?? []) {
      visit(child, nextPath, nextActive);
    }
  }
  for (const root of roots) visit(root, [], new Set());
  if (longest.length < 2) {
    for (const node of nodes) visit(node.event_id, [], new Set());
  }
  return longest.map((eventID) => nodeByID.get(eventID)!).filter(Boolean);
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
  return ({ source: "Source", import: "Import", projection: "Projection", causal: "Causal" } as Record<string, string>)[axis] || axis;
}

function graphPositions(nodes: InvestigationNode[], edges: InvestigationProjection["edges"]) {
  const layer = new Map(nodes.map((node) => [node.event_id, 0]));
  for (let pass = 0; pass < nodes.length; pass += 1) {
    let changed = false;
    for (const edge of edges) {
      const from = layer.get(edge.from);
      const to = layer.get(edge.to);
      if (from === undefined || to === undefined) continue;
      const next = Math.min(nodes.length - 1, from + 1);
      if (next > to) {
        layer.set(edge.to, next);
        changed = true;
      }
    }
    if (!changed) break;
  }
  const rows = new Map<number, number>();
  return nodes.map((node, index) => {
    const nodeLayer = layer.get(node.event_id) ?? 0;
    const row = rows.get(nodeLayer) ?? 0;
    rows.set(nodeLayer, row + 1);
    return { id: node.event_id, index, x: 30 + nodeLayer * 190, y: 34 + row * 94 };
  });
}

function truncateSVG(value: string, length: number) {
  return value.length <= length ? value : `${value.slice(0, length - 1)}…`;
}
