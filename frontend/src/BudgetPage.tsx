import { localizedLabels, t, tx, useLocale } from "./i18n";
import { useCallback, useEffect, useRef, useState } from "react";
import { ApiRequestError, budgetAPI, isAbortError, scopeSessionKey, type AccessSessionResponse } from "./api";
import { budgetObject as object, budgetPercentage, budgetText as text, formatBudgetMoney as money, type BudgetItem, type BudgetItems, type BudgetReading } from "./budget";
import "./budget.css";

const labels: Record<string, string> = localizedLabels({}, {unset: "m0172", unsupported: "m0173", active: "m0174", inactive_zero: "m0175", inactive_period_none: "m0176", not_approved: "m0177", not_comparable: "m0178", inactive_budget: "m0179", coverage_gap: "m0180", invalid_amount: "m0181", unit_mismatch: "m0182", definition_not_authorized: "m0183", source_definition_not_authorized: "m0184", below: "m0185", at: "m0186", above: "m0187", complete_settled_as_of: "m0188", exact: "m0189", known_subtotal_with_gap: "m0190", uncertified: "m0191", partial: "m0192", unknown: "m0193", no_allocation_run: "m0194", period_not_configured: "m0195", writer_uncertified: "m0196", period_before_certification: "m0197", restore_gap: "m0198", financial_journal_unavailable: "m0199", unattributable_financial_outbox: "m0200", financial_materialization_pending: "m0201", financial_materialization_gap: "m0202", allocation_partial: "m0203", open: "m0204", acknowledged: "m0205", resolved: "m0206", not_configured: "m0021", pending: "m0207", delivered: "m0208", failed: "m0209", not_observed: "m0210", none_observed: "m0211", unresolved: "m0212", not_modeled: "m0213", monthly: "m0214", daily: "m0215", fiscal_period: "m0216", none: "m0217", draft: "m0218", approved: "m0219", closed: "m0220"});
const label = (value: unknown) => labels[text(value)] ?? (text(value) || t("m0100"));
const unavailable = (state: string) => state === "not_authorized" ? t("m0221") : t("m0222");
function utc(value: unknown) { const raw = text(value); const d = new Date(raw); return raw && Number.isFinite(d.getTime()) ? d.toISOString().replace("T", " ").replace(/\.\d+Z$/, " UTC") : t("m0100"); }
function errorMessage(error: unknown) {
  if (error instanceof ApiRequestError) {
    if (error.status === 403) return t("m0223");
    if (error.status === 409 || error.status === 410) return t("m0224");
    if (error.status === 401) return t("m0225");
  }
  return t("m0226");
}

function BudgetCard({ item, onEvidence }: { item: BudgetItem; onEvidence?: () => void }) {
  useLocale();
  const d = item.definition.body, s = item.summary.body, comparison = object(s?.comparison), coverage = object(s?.coverage), unresolved = object(s?.unresolved_economics);
  const alerts = item.alerts.body?.entries;
  return <article className="workbench-panel budget-card">
    <header><div>{item.source_state === "bounded_offline" ? <p>{tx("m0227")}</p> : null}<h2>{item.budget_ref}</h2><p>{tx("m0228", { value0: item.source_instance, value1: item.project_id, value2: item.definition_revision })}</p></div>{onEvidence ? <button data-budget-ref={item.ref} onClick={onEvidence}>{tx("m0229")}</button> : null}</header>
    {item.current_retrieval_state === "source_unavailable" ? <p className="budget-gap">{tx("m0230", { value0: utc(item.source_reading_cutoff) })}</p> : null}
    <dl className="budget-amounts">
      <div><dt>{tx("m0231")}</dt><dd>{d ? money(d.amount) : unavailable(item.definition.state)}</dd><small>{d ? `${label(d.setting_state)} · ${label(d.period_kind)}` : t("m0232")}</small></div>
      <div><dt>{tx("m0233")}</dt><dd>{s ? money(s.known_subtotal) : unavailable(item.summary.state)}</dd><small>{s ? label(coverage.state) : t("m0234")}</small></div>
      <div><dt>{tx("m0235")}</dt><dd>{comparison.state === "comparable" ? money(comparison.remaining) : label(comparison.state)}</dd><small>{comparison.state === "comparable" ? `${budgetPercentage(comparison)} · ${label(comparison.threshold_state)}` : t("m0236")}</small></div>
    </dl>
    {s ? <><p>{tx("m0239", { value0: s.authority === "usage_finalization_journal.settled.charged_cost_micros" ? t("m0237") : t("m0238"), value1: utc(s.period_start), value2: utc(s.period_end) })}</p>
      <p>{tx("m0240", { value0: utc(s.amount_cutoff) })}</p>
      {Array.isArray(coverage.reasons) && coverage.reasons.length ? <p className="budget-gap">{tx("m0241", { value0: coverage.reasons.map(label).join("；") })}</p> : null}
      <p className="boundary-note">{tx("m0242", { value0: label(unresolved.state), value1: money(unresolved.reserved_at_read) })}</p>
    </> : null}
    <div className="budget-alerts"><h3>{tx("m0243")}</h3>{item.alerts_page === "continuation_page" ? <p>{tx("m0244")}</p> : null}{Array.isArray(alerts) ? alerts.length ? <ul>{alerts.map((value, index) => { const a = object(value); return <li key={text(a.event_ref) || index}><strong>{label(a.status)}</strong> · {label(a.delivery_state)} {t("m0245")}{utc(a.triggered_at)}<br />{t("m0246")}{money(a.spent_at_trigger)} {t("m0247")}{money(a.budget_at_trigger)}{t("m0248")}</li>; })}</ul> : <p>{tx("m0249")}</p> : <p>{unavailable(item.alerts.state)}</p>}
      {item.alerts.body?.has_more ? <p>{tx("m0250")}</p> : null}
    </div>
    <p className="boundary-note">{tx("m0251")}</p>
  </article>;
}

function BudgetHistory({ readingID, itemRef, onInvalid }: { readingID: string; itemRef: string; onInvalid: (error: unknown) => void }) {
  useLocale();
  const [page, setPage] = useState<BudgetItems | null>(null), [items, setItems] = useState<BudgetItem[]>([]);
  const [busy, setBusy] = useState(false), [error, setError] = useState<unknown>(null);
  const generation = useRef(0), controller = useRef<AbortController | null>(null), failedCursor = useRef<string | undefined>(undefined);
  const load = useCallback(async (cursor?: string) => {
    const seq = ++generation.current; controller.current?.abort(); const abort = new AbortController(); controller.current = abort;
    setBusy(true); setError(null); failedCursor.current = cursor;
    try {
      const next = await budgetAPI.history(readingID, itemRef, cursor, abort.signal);
      if (seq !== generation.current) return;
      setPage(next); setItems(old => cursor ? [...old, ...next.items] : next.items);
    } catch (err) {
      if (seq !== generation.current || isAbortError(err)) return;
      setError(err);
      if (err instanceof ApiRequestError && [401, 403, 409, 410].includes(err.status)) { setItems([]); setPage(null); onInvalid(err); }
    } finally { if (seq === generation.current) setBusy(false); }
  }, [readingID, itemRef, onInvalid]);
  useEffect(() => { void load(); return () => { ++generation.current; controller.current?.abort(); }; }, [load]);
  return <section aria-label={t("m0252")}><h3>{tx("m0253")}</h3><p>{tx("m0254")}</p>
    {error ? <p role="alert">{errorMessage(error)} <button disabled={busy} onClick={() => void load(failedCursor.current)}>{tx("m0255")}</button></p> : null}
    {items.map(item => <section key={item.ref}><p>{tx("m0256", { value0: utc(item.source_reading_cutoff), value1: item.definition_revision })}</p><BudgetCard item={item} /></section>)}
    {busy ? <p role="status">{tx("m0257")}</p> : null}
    {page?.next_cursor ? <button disabled={busy} onClick={() => void load(page.next_cursor)}>{tx("m0258")}</button> : null}
  </section>;
}

export function BudgetPage({ session, navigate }: { session: AccessSessionResponse; navigate: (path: string) => void }) {
  useLocale();
  const [reading, setReading] = useState<BudgetReading | null>(null);
  const [page, setPage] = useState<BudgetItems | null>(null);
  const [items, setItems] = useState<BudgetItem[]>([]);
  const [selected, setSelected] = useState<BudgetItem | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const controller = useRef<AbortController | null>(null), generation = useRef(0);
  const retryCursor = useRef<string | undefined>(undefined);
  const retainedReading = useRef<BudgetReading | null>(null);
  const evidenceHeading = useRef<HTMLHeadingElement>(null), opener = useRef<string | null>(null);
  const scope = scopeSessionKey(session);
  const clear = useCallback(() => { retainedReading.current = null; setReading(null); setPage(null); setItems([]); setSelected(null); }, []);
  const invalidate = useCallback((err: unknown) => { clear(); setError(err); }, [clear]);
  const load = useCallback(async (existing?: string, cursor?: string) => {
    const seq = ++generation.current;
    controller.current?.abort(); const abort = new AbortController(); controller.current = abort;
    setBusy(true); setError(null); retryCursor.current = cursor;
    const known = retainedReading.current;
    if (!cursor) clear();
    try {
      const next = existing ? (known?.reading_id === existing ? known : await budgetAPI.reading(existing, abort.signal)) : await budgetAPI.createReading({ contract_version: "xinsight.budget-reading.v1", filters: { source_ids: [], source_projects: [], cost_projects: [] } }, session.csrf_token, abort.signal);
      if (generation.current !== seq) return;
      retainedReading.current = next;
      const rows = await budgetAPI.items(next.reading_id, cursor, abort.signal);
      if (generation.current !== seq) return;
      setReading(next); setPage(rows); setItems(old => cursor ? [...old, ...rows.items] : rows.items);
      const url = new URL(window.location.href); url.searchParams.set("reading", next.reading_id);
      window.history.replaceState(window.history.state, "", url.pathname + url.search);
    } catch (err) {
      if (generation.current !== seq || isAbortError(err)) return;
      setError(err);
      if (err instanceof ApiRequestError && [401, 403, 409, 410].includes(err.status)) clear();
    } finally { if (generation.current === seq) setBusy(false); }
  }, [clear, session.csrf_token]);
  useEffect(() => {
    const existing = new URLSearchParams(window.location.search).get("reading") ?? undefined;
    void load(existing);
    return () => { ++generation.current; controller.current?.abort(); };
  }, [scope, load]);
  useEffect(() => { if (selected) evidenceHeading.current?.focus(); }, [selected]);

  return <section className="workbench-page budget-page" aria-busy={busy}>
    <div className="workbench-hero"><div><span className="eyebrow">{tx("m0259")}</span><h1>{tx("m0260")}</h1><p>{tx("m0261")}</p></div><button disabled={busy} onClick={() => void load()}>{tx("m0262")}</button></div>
    <nav className="budget-navigation" aria-label={t("m0263")}><a href="/workbench/cost" onClick={e => { if (e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey) { e.preventDefault(); navigate("/workbench/cost"); } }}>{tx("m0117")}</a><span aria-current="page">{tx("m0260")}</span></nav>
    {error ? <div className="budget-gap" role="alert">{errorMessage(error)} <button disabled={busy} onClick={() => void load(retainedReading.current?.reading_id, retryCursor.current)}>{tx("m0255")}</button></div> : null}
    {busy ? <p role="status">{tx("m0264")}</p> : null}
    {reading ? <p className="boundary-note">{tx("m0265", { value0: utc(reading.cutoff), value1: reading.budget_count })}</p> : null}
    {selected ? <section className="workbench-panel budget-evidence" aria-label={t("m0266")}><button onClick={async () => { await load(reading?.reading_id); window.setTimeout(() => { Array.from(document.querySelectorAll<HTMLButtonElement>("button[data-budget-ref]")).find(button => button.dataset.budgetRef === opener.current)?.focus(); }, 0); }}>{tx("m0267")}</button><h2 tabIndex={-1} ref={evidenceHeading}>{tx("m0268", { value0: selected.budget_ref })}</h2>
      <p>{tx("m0269", { value0: selected.source_reading_id, value1: utc(selected.source_reading_cutoff), value2: utc(selected.received_at) })}</p>
      <p>{tx("m0270")}</p>
      {(["definition", "summary", "alerts"] as const).map(key => <details key={key}><summary>{{ definition: t("m0271"), summary: t("m0272"), alerts: t("m0273") }[key]}</summary>{selected[key].body ? <><p>{selected[key].source_hash ? t("m0274", { value0: selected[key].source_hash }) : t("m0275")}</p><pre>{JSON.stringify(selected[key].body, null, 2)}</pre></> : <p>{unavailable(selected[key].state)}</p>}</details>)}
      {reading ? <BudgetHistory readingID={reading.reading_id} itemRef={selected.ref} onInvalid={invalidate} /> : null}
    </section> : <div className="budget-list">{items.map(item => <BudgetCard key={item.ref} item={item} onEvidence={() => { opener.current = item.ref; setSelected(item); }} />)}</div>}
    {!busy && !error && reading && !items.length ? <p>{tx("m0276")}</p> : null}
    {!selected && page?.next_cursor ? <button disabled={busy} onClick={() => void load(reading?.reading_id, page.next_cursor)}>{tx("m0277")}</button> : null}
    <aside className="workbench-panel budget-source-gaps"><h2>{tx("m0278")}</h2><p>{tx("m0279")}</p></aside>
  </section>;
}
