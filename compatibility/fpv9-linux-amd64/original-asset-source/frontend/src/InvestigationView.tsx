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

const stageLabels: Record<string, string> = {
  identity: "身份",
  routing: "路由",
  provider: "模型提供方",
  retry_fallback: "重试／备用路由",
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
  provider_failed: "主提供方调用失败",
  provider_succeeded: "提供方调用成功",
  retry_scheduled: "重试已安排",
  fallback_selected: "切换备用路由",
  billing_recorded: "计费结果已记录",
  execution_completed: "执行完成并记录用量",
  capture_gap: "检测到证据缺口",
  evidence_observed: "记录证据",
};

const completenessBasisLabels: Record<string, string> = {
  source_profile_declares_timeline_complete: "来源声明时间线完整。",
  source_profile_declares_partial_or_unknown: "来源声明时间线不完整，或完整性未知。",
  connector_checkpoint_is_current: "当前已知导入检查点为最新状态。",
  connector_checkpoint_not_current: "当前导入检查点未确认追平来源。",
  bounded_projection_contains_all_imported_records: "当前范围内已导入记录均已纳入视图。",
  projection_truncated_or_count_mismatch: "当前视图被截断或记录数不一致。",
  source_profile_declared_no_causal_edges: "当前来源没有可用的因果边声明。",
  all_source_declared_parents_resolved: "来源声明的父节点均已解析。",
  source_declared_parent_unresolved: "来源声明的部分父节点尚未解析。",
};
const unprovableLabels: Record<string, string> = {
  import_not_current: "当前导入检查点未确认追平来源，无法证明导入已完整。",
  projection_bounded: "当前视图被截断或记录数不一致，无法证明所有已导入证据均已展示。",
  capture_gap_limits_claim: "采集缺口限制了相关判断；具体限制见原始声明。",
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
            <span className="eyebrow">当前执行 · 确定性调查视图</span>
            <span className="presentation-chip">{presentationLabel(projection.presentation_mode)}</span>
            {projection.execution.proof_class === "synthetic_fixture" ? (
              <span className="demo-data-chip">演示数据 · 合成资料</span>
            ) : null}
          </div>
          <h2 id="investigation-headline">
            {headline(projection, failedNode, fallbackNode, fallbackSequenceIsDeclaredPath)}
          </h2>
          <p>{subheadline(projection, failedNode, finalNode, fallbackSequence, fallbackSequenceIsDeclaredPath)}</p><details className="technical-reference"><summary>执行视图代码</summary><code>{projection.presentation_mode}</code></details>
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
          <Metric label="词元数" value={finalNode?.total_tokens ?? "未提供"} />
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
            label="本次执行步骤／缺口"
            value={`${projection.nodes.length} / ${projection.gaps.length}`}
          />
        </dl>
      </section>

      {projection.presentation_mode === "terminal_snapshot" ? (
        <section className="terminal-snapshot-notice" role="status">
          <strong>来源未提供可验证的执行链路</strong>
          <p>以下内容是终态快照，不会把用量、计费或缺口记录伪装成因果节点。</p>
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
            <span className="eyebrow">来源证据的阶段覆盖</span>
            <h2 id="coverage-title">证据覆盖与完整性</h2>
          </div>
          <span className={`coverage-summary-state${integrityComplete ? " coverage-summary-complete" : " coverage-summary-limited"}`}>
            {coveredStages}/{projection.stage_coverage.length} 阶段有证据 · {integrityComplete ? "四轴完整" : "存在未闭合轴"}
          </span>
        </summary>
        <div className="coverage-content" aria-labelledby="coverage-title">
          <p>绿色表示有证据；黄色表示明确缺口；灰色不会被解释为成功。</p>
          {projection.gaps.length ? <ReviewStart key={`${executionID}:gaps`} session={session} navigate={navigate} available={{ evidence: [], gaps: projection.gaps.map((g) => ({ scope: "finding", gap_id: g.gap_id, relation: "limits", affected_claim: g.affected_claim, gap_type: g.gap_type })) }} /> : null}
          <div className="coverage-strip">
            {projection.stage_coverage.map((coverage) => (
              <CoverageItem key={coverage.stage} coverage={coverage} onSelect={onSelect} />
            ))}
          </div>
          <div className="completeness-grid" aria-label="四轴完整性">
            {completenessAxes(projection).map((axis) => (
              <article key={axis.axis} className={`axis-card axis-${axis.status}`}>
                <span>{axisLabel(axis.axis)}</span>
                <strong>{coverageLabels[axis.status] || "状态待确认"}</strong>
                <p>{completenessBasisLabels[axis.basis] || "完整性依据尚无可读解释，请查看原始代码。"}</p><details className="technical-reference"><summary>完整性代码</summary><code>{axis.axis} · {axis.status} · {axis.basis}</code></details>
              </article>
            ))}
          </div>
        </div>
      </details>

      <div className={`investigation-workbench investigation-workbench-${mode}`}>
        <section className="trace-story" aria-labelledby="trace-story-title">
          <div className="section-heading">
            <div>
              <span className="eyebrow">证据路径</span>
              <h2 id="trace-story-title">{mode === "graph" ? "来源声明的因果图" : "可阅读的执行路径"}</h2>
            </div>
            <p>
              当前执行有 {projection.edges.length} 条来源声明的因果边；时间相邻不会自动连线。
            </p>
          </div>
          {projection.nodes.length === 0 ? (
            <p className="trace-empty">当前快照没有可显示的证据节点。</p>
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
              <span className="eyebrow">选中的证据</span>
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
                  {({info:"信息",warning:"警告",error:"错误",critical:"严重"} as Record<string,string>)[selectedNode.severity] || "级别待确认"}
                </span>
                <details className="technical-reference"><summary>节点技术代码</summary><code>{selectedNode.event_type} · {selectedNode.severity} · {selectedNode.proof_basis} · {selectedNode.outcome_status}</code></details>
              </div>
              <div className="inspector-tabs" role="tablist" aria-label="节点调查视图">
                {([
                  ["facts", "事实"],
                  ["evidence", "证据"],
                  ["gaps", `缺口 ${selectedGaps.length}`],
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
              {eventLoading ? <p className="inspector-loading">正在读取节点证据…</p> : null}
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
              <ReviewStart key={selectedNode.citation.evidence_event_id} session={session} navigate={navigate} available={{ evidence: [{ scope: "finding", evidence_event_id: selectedNode.citation.evidence_event_id, event_hash: selectedNode.citation.event_hash, source_ref: selectedNode.citation.source_ref, relation: "supports" }], gaps: selectedGaps.map((g) => ({ scope: "finding", gap_id: g.gap_id, relation: "limits", affected_claim: g.affected_claim, gap_type: g.gap_type })) }} />
              {canWrite ? <div className="case-capture">
                <span className="eyebrow">加入案件</span>
                {cases.length > 0 ? (
                  <>
                    <label htmlFor="case-target">目标案件
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
                      {caseState === "saving" ? "正在引用…" : "引用证据与相关缺口"}
                    </button>
                    <p role="status">
                      {caseState === "saved"
                        ? "已加入案件；证据本体未被修改。"
                        : caseState === "error"
                          ? "加入失败；案件与证据保持原状。"
                          : "同时固定执行快照截止、事件哈希与来源引用。"}
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
                            ? "正在引用全部缺口…"
                            : `单独引用全部 ${projection.gaps.length} 个执行缺口`}
                        </button>
                        <p role="status">
                          {gapCaseState === "saved"
                            ? "全部执行缺口已作为限制加入案件。"
                            : gapCaseState === "error"
                              ? "缺口引用失败；未将案件修订伪报为成功。"
                              : "缺口作为独立证据记录保留，不会简化成标签。"}
                        </p>
                      </>
                    ) : null}
                  </>
                ) : session.session.permissions.includes("investigation.write") ? (
                  <button className="button button-secondary" type="button" onClick={() => navigate("/cases")}>先创建案件</button>
                ) : <p>勾选当前依据并使用“从所选依据开始复核”建立案件。</p>}
              </div> : null}
            </>
          )}
        </section>
      </div>

      <section className="unprovable-section" aria-labelledby="unprovable-title">
        <div className="section-heading">
          <div><span className="eyebrow">未知／无法证明</span><h2 id="unprovable-title">不能证明的动作</h2></div>
          <p>未知不是失败，也不是零；它保留在调查结论中。</p>
        </div>
        <div className="unprovable-grid">
          {(projection.unprovable_actions ?? []).map((item) => (
            <article key={item.code}>
              <span className="missing-mark" aria-hidden="true">∕</span>
              <div><strong>{unprovableLabels[item.code] || item.statement}</strong><details className="technical-reference"><summary>原始限制声明</summary><p>{item.statement}</p><code>{item.code}</code></details><small>{item.gap_ids?.length ?? 0} 个关联缺口</small></div>
            </article>
          ))}
          {(projection.unprovable_actions?.length ?? 0) === 0 ? (
            <p className="quiet-copy">当前视图未列出额外无法证明的动作；这不扩大来源覆盖范围。</p>
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
      <section className="trace-journey trace-journey-unavailable" aria-label="跨系统路径状态">
        <div>
          <span className="eyebrow">跨执行的来源路径</span>
          <h2>跨系统路径暂不可读</h2>
        </div>
        <p>当前执行的既有证据视图仍可调查；系统不会用时间相邻关系补造跨系统路径。</p>
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
  const action = root?.action || "AI 模型请求";
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
          <span className="eyebrow">来源声明 · 跨执行</span>
          <h2 id="trace-journey-title">跨系统执行路径</h2>
          <p>从谁发起、做了什么、经过哪些系统，到哪里结束；只连接来源明确声明的父子步骤。</p>
        </div>
        <div className="trace-journey-integrity">
          <strong>{journey.integrity.execution_count} 次执行</strong>
          <span>{journey.integrity.resolved_parent_count}/{journey.integrity.declared_parent_count} 个父节点已解析</span>
          {journey.integrity.cycle_detected ? <span className="trace-cycle-warning">检测到循环关系</span> : null}
        </div>
      </div>

      <dl className="trace-journey-summary">
        <div><dt>谁发起</dt><dd>{actor}</dd></div>
        <div><dt>做了什么</dt><dd>{action}</dd></div>
        <div><dt>经过系统</dt><dd>{systems.length > 0 ? systems.join(pathIsFullyDeclared ? " → " : " · ") : "来源未提供"}</dd></div>
        <div><dt>到哪里结束</dt><dd>{end}</dd></div>
      </dl>

      <ol className="trace-journey-path" aria-label="跨执行数据流路径">
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
                    {parentResolved ? "来源声明的父节点" : "未解析父节点"}
                  </span>
                ) : <span className="trace-root-label">路径起点</span>}
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

      <details className="trace-journey-all"><summary>全部可见分支与关系（{journey.nodes.length} 个节点）</summary>
        <p>上方为路径摘要。下图保留全部当前可见节点与来源声明的边；折叠不改变成员或费用，缺边不按时间补连。</p>
        <TraceJourneyGraph journey={journey} onSelect={openNode} />
      </details>
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
          <span>证据事件</span>
          {declaredParentID ? (
            <span title={declaredParentID}>
              ↳ 来源声明的父节点 {shortRef(declaredParentID, 6, 4)}
            </span>
          ) : (
            <span>仅时间顺序</span>
          )}
        </div>
        <span className={`proof-basis proof-basis-${node.proof_basis || "unknown"}`}>
          {proofBasisLabel(node.proof_basis)}
        </span>
      </button>
      <details className="technical-reference"><summary>节点引用与代码</summary><p>事件 <code>{node.event_id}</code></p><p>类型 <code>{node.event_type}</code> · 依据 <code>{node.proof_basis || "unknown"}</code> · 结果 <code>{node.outcome_status}</code></p>{declaredParentID ? <p>声明父节点 <code>{declaredParentID}</code></p> : null}</details>
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
        <desc id="causal-graph-description">只包含接口明确返回的关系；虚线边界表示声明父节点未解析。</desc>
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
              {missingParent ? <text className="graph-missing-label" x="75" y="-8">父节点缺失</text> : null}
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
                <span>{parent ? `来源声明的父节点 ${shortRef(parent)}` : node.declared_parent_event_id ? "声明父节点未解析" : "未声明父节点"}</span>
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
      title="查看支持该阶段的首个证据事件"
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
    ["模型提供方", event?.capability?.provider || node.provider],
    ["实际模型", event?.capability?.model || node.model],
    ["请求模型", event?.capability?.requested_model || node.requested_model],
    ["发起者类型", event?.identity?.actor_type || node.actor_type],
    ["发起者引用", event?.identity?.actor_ref || node.actor_ref],
    ["组织", event?.identity?.org_id || node.org_id],
    ["项目", event?.identity?.project_id || node.project_id],
    ["决策", event?.decision?.decision_result || node.decision_result],
    ["决策原因", event?.decision?.decision_reason || node.decision_reason],
    ["HTTP", event?.outcome?.status_code ?? node.status_code],
    ["结果", journeyOutcomeLabel(event?.outcome?.outcome_status || node.outcome_status || "unknown")],
    ["延迟", node.latency_ms !== undefined ? `${node.latency_ms} ms` : undefined],
    ["词元数", node.total_tokens],
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
      <p>以下三个字段组成可回查的证据引用；不包含原始内容。</p>
      <dl>
        <CitationFact label="证据事件标识" value={node.citation.evidence_event_id} />
        <CitationFact label="来源引用" value={node.citation.source_ref} />
        <CitationFact label="事件哈希" value={node.citation.event_hash} />
        <CitationFact label="固定阅读链接" value={node.citation.deep_link} />
        <CitationFact label="来源结构版本" value={event?.source.schema_version || "正在读取"} />
        <CitationFact label="来源保真度" value={event?.source.fidelity_level || "正在读取"} />
      </dl>
      <button type="button" className="button button-secondary" onClick={onCopy}>
        {copyState === "copied" ? "已复制引用" : copyState === "failed" ? "复制失败，请手动选择" : "复制证据引用"}
      </button>
    </div>
  );
}

function GapsPanel({ gaps }: { gaps: CaptureGap[] }) {
  if (gaps.length === 0) {
    return (
      <div className="inspector-no-gaps">
        <strong>此节点没有已记录的采集缺口</strong>
        <p>这不等于全局完整；请同时查看阶段覆盖与证据依据。</p>
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
      return "来源声明的因果路径";
    case "chronology":
      return "时间顺序";
    case "terminal_snapshot":
      return "终态快照";
    default:
      return "视图类型待确认";
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
      ? "最终成功：来源声明路径显示备用路由完成恢复"
      : "最终成功：同一完整时间线中观察到失败与备用路由";
  }
  if (projection.presentation_mode === "terminal_snapshot") {
    return "终态快照：当前来源没有提供执行链路";
  }
  if (failedNode && fallbackNode) {
    return "发现提供方失败与备用路由，但完整恢复链尚不可证明";
  }
  if (projection.execution.outcome === "failure") {
    return "执行失败：查看失败节点与证据缺口";
  }
  if (projection.execution.outcome === "success") {
    return "执行成功：按证据路径核对关键决策";
  }
  return "执行结果未知：仅展示可验证证据";
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
      failedNode?.provider || "主提供方",
      failedNode?.status_code ? `HTTP ${failedNode.status_code}` : "失败",
      "→ 重试 →",
      finalNode?.provider || "备用提供方",
      "成功",
      fallbackSequenceIsDeclaredPath ? "（来源声明父子关系）" : "（确定性时间顺序，不代表因果边）",
    ].join(" ");
  }
  if (projection.presentation_mode === "terminal_snapshot") {
    return `保留 ${projection.nodes.length} 个终态证据事件 与 ${projection.gaps.length} 个显式缺口；未生成伪因果边。`;
  }
  return `${projection.edges.length} 条本次执行显式因果边，${projection.gaps.length} 个采集缺口，快照 ${formatDateTime(projection.as_of)}。`;
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
      return "直接观察";
    case "source_declared":
    case "declared":
      return "来源声明";
    case "deterministic":
    case "derived":
      return "确定性推导";
    case "gap":
      return "缺口／未知";
    default:
      return "依据待确认";
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
      note: actor ? "来自证据中的身份记录" : "不会根据上下文猜测身份",
      uncertain: !actor,
    },
    {
      label: "动作",
      value: "发起 AI 执行请求",
      note: `请求 ${shortRef(projection.execution.request_id, 12, 8)}`,
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
      note: terminalOnPath ? "声明路径到达该终态" : "终态与声明路径关系未解析",
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
    return `${actor} 发起 AI 执行；按时间观察到 ${nodes.length} 个步骤，但来源未声明步骤间的因果路径；终态为 ${journeyOutcomeLabel(end || "unknown")}。`;
  }
  const path = declaredPath
    .map((node) => node.provider || node.model || nodeLabels[node.kind] || node.stage)
    .filter((value, index, values) => values.indexOf(value) === index)
    .join(" → ");
  const terminalOnPath = declaredPath.some((node) => node.event_id === last?.event_id);
  return terminalOnPath
    ? `${actor} 发起 AI 执行；来源声明的最长可验证路径为 ${path}；该路径到达终态 ${journeyOutcomeLabel(end || "unknown")}。`
    : `${actor} 发起 AI 执行；来源声明的最长可验证路径为 ${path}；终态为 ${journeyOutcomeLabel(end || "unknown")}，但它与该路径的父子关系未声明或未解析。`;
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
  return ({ source: "来源", import: "导入", projection: "视图范围", causal: "因果关系" } as Record<string, string>)[axis] || axis;
}


function truncateSVG(value: string, length: number) {
  return value.length <= length ? value : `${value.slice(0, length - 1)}…`;
}


function TraceJourneyGraph({ journey, onSelect }: { journey: TraceJourney; onSelect: (node: TraceJourneyNode) => void }) {
  const marker = useId().replaceAll(":", "");
  const edges = journey.edges.map(e => ({ from: e.from_event_id, to: e.to_event_id }));
  const positions = graphPositions(journey.nodes, edges);
  const byID = new Map(positions.map(p => [p.id, p]));
  const height = Math.max(240, ...positions.map(p => p.y + 92)), width = Math.max(760, ...positions.map(p => p.x + 180));
  return <div className="causal-graph-shell" role="group" aria-label="跨系统全部可见节点与声明关系"><svg className="causal-graph" style={{ minWidth: width, minHeight: height }} viewBox={`0 0 ${width} ${height}`}>
    <defs><marker id={marker} markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto"><path d="M0,0 L8,4 L0,8 z" className="graph-arrow" /></marker></defs>
    {edges.map(edge => { const from = byID.get(edge.from), to = byID.get(edge.to); return from && to ? <line key={`${edge.from}:${edge.to}`} className="graph-edge" aria-label="来源声明的跨系统关系" x1={from.x + 150} y1={from.y + 34} x2={to.x} y2={to.y + 34} markerEnd={`url(#${marker})`} /> : null; })}
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
    <ul aria-label="来源声明的跨系统关系列表">{journey.edges.map(edge => {
      const from = journey.nodes.find(node => node.event_id === edge.from_event_id);
      const to = journey.nodes.find(node => node.event_id === edge.to_event_id);
      if (!from || !to) return null;
      return <li key={`${edge.from_event_id}:${edge.to_event_id}`}>{from.service_name || from.source_type} → {to.service_name || to.source_type}</li>;
    })}</ul>
  </div></div>;
}
