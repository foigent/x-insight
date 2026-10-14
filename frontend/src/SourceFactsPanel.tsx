import { localizedLabels, t, tx, useLocale, useMessageState } from "./i18n";
import { useEffect, useRef, useState } from "react";
import {
  ApiRequestError, type WorkbenchReading, type WorkbenchSourceObservation,
  type WorkbenchSourceObservations, workbenchAPI,
} from "./api";
import "./source-facts.css";

export function SourceFactsPanel({ reading, onInvalidated }: {
  reading: WorkbenchReading; onInvalidated: (failure: unknown) => void;
}) {
  useLocale();
  const [page, setPage] = useState<WorkbenchSourceObservations | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useMessageState("");
  const generation = useRef(0);
  const failureHandler = useRef(onInvalidated);
  failureHandler.current = onInvalidated;
  const abort = useRef<AbortController | null>(null);

  async function load(cursor?: string) {
    const sequence = ++generation.current;
    abort.current?.abort();
    const controller = new AbortController();
    abort.current = controller;
    setBusy(true); setError(() => "");
    try {
      const next = await workbenchAPI.sourceFacts(reading.reading_id, cursor, controller.signal);
      if (sequence !== generation.current) return;
      if (next.reading_id !== reading.reading_id || next.manifest_ref !== reading.source_manifest_ref) {
        throw new Error("source reading changed");
      }
      setPage(previous => cursor && previous ? { ...next, items: [...previous.items, ...next.items] } : next);
    } catch (failure) {
      if (sequence !== generation.current || controller.signal.aborted) return;
      if (failure instanceof ApiRequestError &&
        ([401, 404, 410].includes(failure.status) || failure.problem?.code === "workbench_access_changed")) {
        setPage(null); failureHandler.current(failure); return;
      }
      if (failure instanceof ApiRequestError && failure.status === 403) setPage(null);
      setError(() => failure instanceof ApiRequestError && failure.status === 403
        ? t("m2379")
        : t("m2380"));
    } finally { if (sequence === generation.current) setBusy(false); }
  }

  useEffect(() => {
    setPage(null); void load();
    return () => { ++generation.current; abort.current?.abort(); };
    // Each mounted panel is tied to one immutable reading by the parent key.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reading.reading_id]);

  const coverage = reading.source_coverage;
  if (!coverage) return null;
  return <article className="workbench-panel source-facts-panel" aria-busy={busy}>
    <header><div><span className="eyebrow">{tx("m2381")}</span><h2>{tx("m2382")}</h2></div>
      <span>{tx("m2383", { value0: coverage.known_documents })}</span></header>
    <p>{tx("m2384")}</p>
    <div className="source-facts-coverage" aria-label={t("m2385")}>
      <span>{t("m2386")}<b>{coverage.economic_not_received}</b></span>
      <span>{t("m2387")}<b>{coverage.pending_predecessor}</b></span>
      <span>{t("m2388")}<b>{coverage.economic_hash_conflicts}</b></span>
      <span>{t("m2389")}<b>{coverage.media_pending}</b></span>
      <span>{t("m2390")}<b>{coverage.not_captured}</b></span>
    </div>
    {coverage.economic_not_received > 0 ? <p className="source-facts-notice">{tx("m2391")}</p> : null}
    {coverage.media_pending > 0 ? <p className="source-facts-notice">{tx("m2392")}</p> : null}
    <p className="source-facts-limit">{tx("m2393")}</p>
    {coverage.requires_attention ? <p className="source-facts-notice" role="status">{tx("m2394")}</p> : null}
    {coverage.collection_states?.length ? <div aria-label={t("m2395")}>
      <p>{tx("m2396", { value0: coverage.collection_states.map(state => collectionLabels[state]).join("、") })}</p>
      {coverage.collection_states.includes("paused") ? <p className="source-facts-notice">{tx("m2397")}</p> : null}
      {coverage.collection_states.includes("revoked") ? <p className="source-facts-notice">{tx("m2398")}</p> : null}
    </div> : <p className="source-facts-limit">{tx("m2399")}</p>}
    {coverage.gaps.includes("source_fact_conflict") ? <p className="source-facts-notice">{tx("m2400")}</p> : null}
    {coverage.gaps.some(gap => !["forward_only_capture", "timeline_completeness_unknown", "source_fact_conflict"].includes(gap))
      ? <p className="source-facts-notice">{tx("m2401")}</p> : null}
    {page?.items.map(fact => <SourceFactCard key={`${fact.source_system_id}:${fact.source_fact_id}`} fact={fact} />)}
    {page?.items.length === 0 ? <p>{tx("m2402")}</p> : null}
    {error ? <div role="status"><p>{error}</p><button disabled={busy} onClick={() => void load(page?.next_cursor)}>{tx("m2403")}</button></div> : null}
    {page?.next_cursor ? <button disabled={busy} className="text-button" onClick={() => void load(page.next_cursor)}>{tx("m2404")}</button> : null}
  </article>;
}

const collectionLabels = localizedLabels({}, {active: "m2002", paused: "m1601", revoked: "m2358"});

const profileNames: Record<string, string> = localizedLabels({}, {"unirouter.execution-context.v1": "m2355", "unirouter.execution-identity.v1": "m2356", "unirouter.cost-basis.v1": "m2357"});
const stageNames: Record<string, string> = localizedLabels({}, {authenticated: "m2405", admission: "m2406", dispatch: "m2407", terminal: "m2408"});
const economics: Record<string, string> = localizedLabels({}, {not_evaluated: "m2409", received: "m2410", not_received: "m2411", hash_mismatch: "m2412", media_pricing_pending: "m2413", media_not_eligible: "m2414"});
const contextLabels: Record<string, string> = localizedLabels({}, {workload_application_id: "m2415", credential_ref: "m2416", credential_id: "m2417", api_key_id: "m2418", key_type: "m2419", auth_kind: "m2420", auth_epoch: "m2421", protocol: "m2243", operation: "m0079", captured_at: "m2422"});
const identityLabels: Record<string, string> = localizedLabels({agent_id: "Agent"}, {owner_user_id: "m2423", grant_id: "m2424", grant_revision: "m2425", grant_subject_type: "m2426", grant_subject_user_id: "m2427", grant_subject_workload_id: "m2428", sponsor_user_id: "m2429", workflow_id: "m2430", root_agent_id: "m2431", parent_agent_id: "m2432", authority_version: "m2433", authority_read: "m2434"});
const basisLabels: Record<string, string> = localizedLabels({}, {billing_mode: "m2435", mode_source: "m2436", pricing_authority: "m2437", pricing_version: "m2438", rate_card_ref: "m2439", rate_card_version: "m2440", rate_card_layer: "m2441", input_rate_micros_per_million: "m2442", output_rate_micros_per_million: "m2443", cache_read_rate_micros_per_million: "m2444", cache_creation_rate_micros_per_million: "m2445", price_multiplier: "m2446"});

function object(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}
function valueText(value: unknown): string { return typeof value === "string" ? value || t("m2447") : t("m0193"); }
function Fields({ values, labels }: { values: Record<string, unknown>; labels: Record<string, string> }) {
  useLocale();
  return <dl>{Object.entries(labels).filter(([key]) => key in values).map(([key, label]) => <div key={key}><dt>{label}</dt><dd>{valueText(values[key])}</dd></div>)}</dl>;
}

export function SourceFactCard({ fact }: { fact: WorkbenchSourceObservation }) {
  useLocale();
  const basis = object(fact.cost_basis?.basis);
  const reference = object(fact.cost_basis?.economic_reference);
  const pricing = basis.pricing_state === "priced" ? t("m2448") : basis.pricing_state === "unpriced" ? t("m2449") : t("m2450");
  return <section className="source-fact-card" aria-label={t("m2452", { value0: profileNames[fact.profile] || t("m2451"), value1: fact.revision })}>
    <header><strong>{profileNames[fact.profile] || t("m2451")}</strong><span>{tx("m1670", { value0: stageNames[fact.stage] || t("m2453"), value1: fact.revision })}</span></header>
    <p className="source-fact-request">{tx("m1105", { value0: fact.request_id })}</p>
    <p>{economics[fact.economic_state]}</p>
    {fact.chain_state === "missing_predecessor" ? <p className="source-facts-notice">{tx("m2454")}</p> : null}
    {fact.capture_status === "not_captured" ? <p>{tx("m2455")}</p> : null}
    {fact.context ? <Fields values={fact.context} labels={contextLabels} /> : null}
    {fact.identity ? <><Fields values={fact.identity} labels={identityLabels} />
      <p>{tx("m2456")}</p>
      {Array.isArray(fact.identity.lineage) ? <p>{tx("m2457", { value0: fact.identity.lineage.filter(item => typeof item === "string").join(" → ") })}</p> : null}</> : null}
    {fact.cost_basis ? <><p><strong>{pricing}</strong> {t("m2458")}{valueText(reference.revision)}</p>
      <p>{reference.source === "async_media_charge_fact" ? t("m2459") : reference.publication_status === "not_published_by_i01" ? t("m2460") : t("m2461")}</p>
      <details><summary>{tx("m2462")}</summary><Fields values={basis} labels={basisLabels} />
        <Fields values={fact.cost_basis} labels={{ basis_status: t("m2463"), journal_billing_mode: t("m2464"), journal_mode_source: t("m2465") }} />
        {Array.isArray(basis.unit_rates) ? <ul>{basis.unit_rates.map((rate, index) => { const r = object(rate); return <li key={index}>{tx("m2466", { value0: valueText(r.meter), value1: valueText(r.unit_quantity), value2: valueText(r.amount_minor) })}</li>; })}</ul> : null}
      </details></> : null}
    <details><summary>{tx("m2467")}</summary><dl>
      <div><dt>{tx("m2451")}</dt><dd>{fact.source_fact_id}</dd></div>
      <div><dt>{tx("m1063")}</dt><dd>{fact.occurred_at}</dd></div>
      <div><dt>{tx("m2468")}</dt><dd>{fact.recorded_at}</dd></div>
      <div><dt>{tx("m2469")}</dt><dd>{fact.source_hash}</dd></div>
      <div><dt>{tx("m2470")}</dt><dd>{fact.envelope_hash}</dd></div>
    </dl></details>
  </section>;
}
