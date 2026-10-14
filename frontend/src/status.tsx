import { localizedLabels, t, tx, useLocale } from "./i18n";
import type {
  AssuranceMode,
  ExecutionOutcome,
  FieldProvenance,
  ProofClass,
  SemanticState,
  SyncState,
} from "./api";

const semanticLabels: Record<SemanticState, string> = localizedLabels({}, {direct: "m2966", derived: "m2967", declared: "m2968", suppressed: "m2969", gap: "m0655", unknown: "m0193", not_applicable: "m0919"});

const freshnessLabels: Record<SyncState, string> = localizedLabels({}, {syncing: "m1486", current: "m2970", stale: "m2971", gap: "m2972"});

const outcomeLabels: Record<ExecutionOutcome, string> = localizedLabels({}, {success: "m1091", failure: "m1088", partial: "m1112", unknown: "m0193"});

const proofLabels: Record<ProofClass, string> = localizedLabels({}, {operator_approved_integration: "m2973", synthetic_fixture: "m2974", unclassified: "m2975"});

const assuranceLabels: Record<AssuranceMode, string> = localizedLabels({}, {operator_attested_sidecar: "m2976", contracted_pull: "m2977", bounded_offline_import: "m2978", synthetic_fixture: "m2974", unclassified: "m2979"});

export function SemanticBadge({ state, compact = false }: { state: SemanticState; compact?: boolean }) {
  useLocale();
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

export function FreshnessBadge({ state, compact = false }: { state: SyncState; compact?: boolean }) {
  useLocale();
  return (
    <span className={`status-badge freshness-${state}`} title={`${freshnessLabels[state]} · ${state}`}>
      <span>{freshnessLabels[state]}</span>
      {!compact && <code>{state}</code>}
    </span>
  );
}

export function OutcomeBadge({ outcome, compact = false }: { outcome: ExecutionOutcome; compact?: boolean }) {
  useLocale();
  return (
    <span className={`outcome-badge outcome-${outcome}`} title={`${outcomeLabels[outcome]} · ${outcome}`}>
      <span>{outcomeLabels[outcome]}</span>
      {!compact && <code>{outcome}</code>}
    </span>
  );
}

export function ProofBadge({ proofClass, compact = false }: { proofClass: ProofClass; compact?: boolean }) {
  useLocale();
  return (
    <span className={`proof-badge proof-${proofClass}`} title={`${proofLabels[proofClass]} · ${proofClass}`}>
      <span>{proofLabels[proofClass]}</span>
      {!compact && <code>{proofClass}</code>}
    </span>
  );
}

export function AssuranceBadge({ assuranceMode, compact = false }: { assuranceMode: AssuranceMode; compact?: boolean }) {
  useLocale();
  return (
    <span className="assurance-badge" title={`${assuranceLabels[assuranceMode]} · ${assuranceMode}`}>
      <span>{assuranceLabels[assuranceMode]}</span>
      {!compact && <code>{assuranceMode}</code>}
    </span>
  );
}

export function SemanticLegend() {
  useLocale();
  return (
    <details className="semantic-legend">
      <summary>
        <span>
          <span className="eyebrow">{tx("m2980")}</span>
          <strong id="semantic-legend-title">{tx("m2981")}</strong>
        </span>
        <span>{tx("m2982")}</span>
      </summary>
      <div className="semantic-legend-body" aria-labelledby="semantic-legend-title">
        <div className="badge-row" aria-label={t("m2983")}>
          {(Object.keys(semanticLabels) as SemanticState[]).map((state) => (
            <SemanticBadge key={state} state={state} />
          ))}
        </div>
        <p>{tx("m2984")}</p>
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

export const fieldGroupLabels: Record<keyof FieldProvenance, string> = localizedLabels({}, {correlation: "m0549", identity: "m0550", capability: "m0551", decision: "m0552", usage: "m0553", outcome: "m0554", payload_policy: "m0557"});

export function proofExplanation(proofClass: ProofClass): string {
  switch (proofClass) {
    case "operator_approved_integration":
      return t("m2985");
    case "synthetic_fixture":
      return t("m2986");
    case "unclassified":
      return t("m2987");
  }
}
