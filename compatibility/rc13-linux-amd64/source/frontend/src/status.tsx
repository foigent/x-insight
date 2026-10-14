import type {
  AssuranceMode,
  ExecutionOutcome,
  FieldProvenance,
  ProofClass,
  SemanticState,
  SyncState,
} from "./api";

const semanticLabels: Record<SemanticState, string> = {
  direct: "直接证据",
  derived: "推导",
  declared: "声明",
  suppressed: "已抑制",
  gap: "缺口",
  unknown: "未知",
  not_applicable: "不适用",
};

const freshnessLabels: Record<SyncState, string> = {
  syncing: "同步中",
  current: "当前修订已同步",
  stale: "数据可能陈旧",
  gap: "存在证据缺口",
};

const outcomeLabels: Record<ExecutionOutcome, string> = {
  success: "成功",
  failure: "失败",
  partial: "部分完成",
  unknown: "未知",
};

const proofLabels: Record<ProofClass, string> = {
  operator_approved_integration: "已批准集成证据",
  synthetic_fixture: "合成夹具",
  unclassified: "未分类证据",
};

const assuranceLabels: Record<AssuranceMode, string> = {
  operator_attested_sidecar: "操作员见证 sidecar",
  contracted_pull: "已签约 pull",
  bounded_offline_import: "受限离线导入",
  synthetic_fixture: "合成夹具",
  unclassified: "未分类保证",
};

export function SemanticBadge({ state, compact = false }: { state: SemanticState; compact?: boolean }) {
  return (
    <span
      className={`status-badge semantic-${state}${compact ? " status-badge-compact" : ""}`}
      title={`${semanticLabels[state]} · ${state}`}
    >
      <span>{semanticLabels[state]}</span>
      {!compact && <code>{state}</code>}
    </span>
  );
}

export function FreshnessBadge({ state }: { state: SyncState }) {
  return (
    <span className={`status-badge freshness-${state}`} title={`${freshnessLabels[state]} · ${state}`}>
      <span>{freshnessLabels[state]}</span>
      <code>{state}</code>
    </span>
  );
}

export function OutcomeBadge({ outcome }: { outcome: ExecutionOutcome }) {
  return (
    <span className={`outcome-badge outcome-${outcome}`} title={`${outcomeLabels[outcome]} · ${outcome}`}>
      <span>{outcomeLabels[outcome]}</span>
      <code>{outcome}</code>
    </span>
  );
}

export function ProofBadge({ proofClass }: { proofClass: ProofClass }) {
  return (
    <span className={`proof-badge proof-${proofClass}`} title={`${proofLabels[proofClass]} · ${proofClass}`}>
      <span>{proofLabels[proofClass]}</span>
      <code>{proofClass}</code>
    </span>
  );
}

export function AssuranceBadge({ assuranceMode }: { assuranceMode: AssuranceMode }) {
  return (
    <span className="assurance-badge" title={`${assuranceLabels[assuranceMode]} · ${assuranceMode}`}>
      <span>{assuranceLabels[assuranceMode]}</span>
      <code>{assuranceMode}</code>
    </span>
  );
}

export function SemanticLegend() {
  return (
    <details className="semantic-legend">
      <summary>
        <span>
          <span className="eyebrow">证据语义</span>
          <strong id="semantic-legend-title">如何理解证据标签</strong>
        </span>
        <span>查看 canonical code 与边界</span>
      </summary>
      <div className="semantic-legend-body" aria-labelledby="semantic-legend-title">
        <div className="badge-row" aria-label="证据语义标签">
          {(Object.keys(semanticLabels) as SemanticState[]).map((state) => (
            <SemanticBadge key={state} state={state} />
          ))}
        </div>
        <p>标签说明字段来源或缺失状态，不是可信度分数；颜色之外始终保留文字与 canonical code。</p>
      </div>
    </details>
  );
}

export function provenanceStates(provenance: FieldProvenance, stage?: string): SemanticState[] {
  const states = new Set<SemanticState>();
  for (const value of Object.values(provenance)) {
    if (value) {
      states.add(value);
    }
  }
  if (stage === "gap") {
    states.add("gap");
  }
  if (states.size === 0) {
    states.add("unknown");
  }
  return [...states];
}

export const fieldGroupLabels: Record<keyof FieldProvenance, string> = {
  correlation: "关联标识",
  identity: "身份",
  capability: "能力与模型",
  decision: "决策",
  usage: "用量",
  outcome: "结果",
  payload_policy: "载荷策略",
};

export function proofExplanation(proofClass: ProofClass): string {
  switch (proofClass) {
    case "operator_approved_integration":
      return "该快照由操作员批准的集成证据覆盖；这不等于 Production verified。";
    case "synthetic_fixture":
      return "该执行来自明确标记的合成夹具，只用于功能与语义验证。";
    case "unclassified":
      return "当前快照没有被同一类不可变导入 provenance 完整覆盖，系统不会猜测证明等级。";
  }
}
