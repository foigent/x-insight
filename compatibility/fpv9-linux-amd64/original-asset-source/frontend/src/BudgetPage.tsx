import { useCallback, useEffect, useRef, useState } from "react";
import { ApiRequestError, budgetAPI, isAbortError, scopeSessionKey, type AccessSessionResponse } from "./api";
import { budgetObject as object, budgetPercentage, budgetText as text, formatBudgetMoney as money, type BudgetItem, type BudgetItems, type BudgetReading } from "./budget";
import "./budget.css";

const labels: Record<string, string> = {
  unset: "未设置", unsupported: "原预算暂不支持", active: "已设置", inactive_zero: "显式为零，未启用", inactive_period_none: "未启用周期", not_approved: "未批准",
  not_comparable: "当前不可比较", inactive_budget: "预算未启用", coverage_gap: "覆盖不足，差额不确定", invalid_amount: "金额无效", unit_mismatch: "币种或精度不一致",
  definition_not_authorized: "没有预算定义查看权", source_definition_not_authorized: "来源未授权预算定义", below: "未到预算", at: "达到预算", above: "超过预算",
  complete_settled_as_of: "截至来源时间，已结算范围完整", exact: "来源确认的完整分摊", known_subtotal_with_gap: "已知小计，仍有缺口", uncertified: "完整周期未获来源确认", partial: "部分覆盖", unknown: "未知", no_allocation_run: "暂无分摊批次",
  period_not_configured: "未配置期间", writer_uncertified: "写入方未完成完整性确认", period_before_certification: "期间早于可确认范围", restore_gap: "恢复边界存在缺口", financial_journal_unavailable: "财务记录不可用", unattributable_financial_outbox: "存在无法归属的财务记录", financial_materialization_pending: "财务记录待落地", financial_materialization_gap: "财务落地存在缺口", allocation_partial: "分摊不完整",
  open: "未处理", acknowledged: "已确认", resolved: "已解决", not_configured: "未配置", pending: "待投递", delivered: "已投递", failed: "投递失败", not_observed: "未观察到", none_observed: "未观察到未决项", unresolved: "存在未决项", not_modeled: "来源未建模",
  monthly: "月度", daily: "每日", fiscal_period: "财务期间", none: "无周期", draft: "草案", approved: "已批准", closed: "已关闭",
};
const label = (value: unknown) => labels[text(value)] ?? (text(value) || "未提供");
const unavailable = (state: string) => state === "not_authorized" ? "当前没有查看权限" : "来源未授权这部分资料";
function utc(value: unknown) { const raw = text(value); const d = new Date(raw); return raw && Number.isFinite(d.getTime()) ? d.toISOString().replace("T", " ").replace(/\.\d+Z$/, " UTC") : "未提供"; }
function errorMessage(error: unknown) {
  if (error instanceof ApiRequestError) {
    if (error.status === 403) return "当前没有预算资料的查看权限。";
    if (error.status === 409 || error.status === 410) return "授权或阅读已变化，请重新读取预算。";
    if (error.status === 401) return "请重新登录。";
  }
  return "预算暂时无法读取，请重试。";
}

function BudgetCard({ item, onEvidence }: { item: BudgetItem; onEvidence?: () => void }) {
  const d = item.definition.body, s = item.summary.body, comparison = object(s?.comparison), coverage = object(s?.coverage), unresolved = object(s?.unresolved_economics);
  const alerts = item.alerts.body?.entries;
  return <article className="workbench-panel budget-card">
    <header><div>{item.source_state === "bounded_offline" ? <p>受控离线快照 · 不表示当前在线状态</p> : null}<h2>{item.budget_ref}</h2><p>{item.source_instance} · 项目 {item.project_id} · 定义版本 {item.definition_revision}</p></div>{onEvidence ? <button data-budget-ref={item.ref} onClick={onEvidence}>查看依据</button> : null}</header>
    {item.current_retrieval_state === "source_unavailable" ? <p className="budget-gap">来源目前不可达；以下仅为已保存快照，截止 {utc(item.source_reading_cutoff)}，不代表当前已用。</p> : null}
    <dl className="budget-amounts">
      <div><dt>原预算</dt><dd>{d ? money(d.amount) : unavailable(item.definition.state)}</dd><small>{d ? `${label(d.setting_state)} · ${label(d.period_kind)}` : "不会由其他快照补齐"}</small></div>
      <div><dt>来源已知小计</dt><dd>{s ? money(s.known_subtotal) : unavailable(item.summary.state)}</dd><small>{s ? label(coverage.state) : "未知不等于零"}</small></div>
      <div><dt>计划差额</dt><dd>{comparison.state === "comparable" ? money(comparison.remaining) : label(comparison.state)}</dd><small>{comparison.state === "comparable" ? `${budgetPercentage(comparison)} · ${label(comparison.threshold_state)}` : "仅在口径和完整性满足时比较"}</small></div>
    </dl>
    {s ? <><p>口径：{s.authority === "usage_finalization_journal.settled.charged_cost_micros" ? "网关已结算扣费" : "企业项目分摊"}。原期间：{utc(s.period_start)} 至 {utc(s.period_end)}（不含结束时刻）。</p>
      <p>金额截至 {utc(s.amount_cutoff)}。全局近 7 天筛选不会改变此预算周期。</p>
      {Array.isArray(coverage.reasons) && coverage.reasons.length ? <p className="budget-gap">缺口：{coverage.reasons.map(label).join("；")}。已知小计不能当作完整已用。</p> : null}
      <p className="boundary-note">未决经济事项：{label(unresolved.state)}；预留 {money(unresolved.reserved_at_read)}。计划差额不代表钱包余额、预留或可调用额度。</p>
    </> : null}
    <div className="budget-alerts"><h3>来源提醒</h3>{item.alerts_page === "continuation_page" ? <p>这是来源后续提醒页，之前的提醒保留在各自快照中。</p> : null}{Array.isArray(alerts) ? alerts.length ? <ul>{alerts.map((value, index) => { const a = object(value); return <li key={text(a.event_ref) || index}><strong>{label(a.status)}</strong> · {label(a.delivery_state)} · 触发于 {utc(a.triggered_at)}<br />当时已用 {money(a.spent_at_trigger)} / 当时预算 {money(a.budget_at_trigger)}。当时预算版本未知；未观察到人工阅读。</li>; })}</ul> : <p>本页未返回提醒。</p> : <p>{unavailable(item.alerts.state)}</p>}
      {item.alerts.body?.has_more ? <p>来源还有提醒，本页未包含全部提醒；可在依据中的保留快照里逐页核对。</p> : null}
    </div>
    <p className="boundary-note">这些资料用于比较和说明。提醒已确认、已解决或已投递，不证明实际限制了调用，也不会消除当前超额。</p>
  </article>;
}

function BudgetHistory({ readingID, itemRef, onInvalid }: { readingID: string; itemRef: string; onInvalid: (error: unknown) => void }) {
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
  return <section aria-label="保留预算快照"><h3>各次来源快照</h3><p>以下资料来自不同时间，分别保留提醒页和预算修订；它们不合并为一次比较，也不补齐当前阅读。</p>
    {error ? <p role="alert">{errorMessage(error)} <button disabled={busy} onClick={() => void load(failedCursor.current)}>重试</button></p> : null}
    {items.map(item => <section key={item.ref}><p>快照截止 {utc(item.source_reading_cutoff)} · 定义版本 {item.definition_revision}</p><BudgetCard item={item} /></section>)}
    {busy ? <p role="status">正在读取保留快照…</p> : null}
    {page?.next_cursor ? <button disabled={busy} onClick={() => void load(page.next_cursor)}>加载更多保留快照</button> : null}
  </section>;
}

export function BudgetPage({ session, navigate }: { session: AccessSessionResponse; navigate: (path: string) => void }) {
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
    <div className="workbench-hero"><div><span className="eyebrow">全周期预算</span><h1>预算与差额</h1><p>每个预算保留来源期间、口径、版本和截止；预算之间不自动加总。</p></div><button disabled={busy} onClick={() => void load()}>重新读取预算</button></div>
    <nav className="budget-navigation" aria-label="费用视图"><a href="/workbench/cost" onClick={e => { if (e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey) { e.preventDefault(); navigate("/workbench/cost"); } }}>费用与使用</a><span aria-current="page">预算与差额</span></nav>
    {error ? <div className="budget-gap" role="alert">{errorMessage(error)} <button disabled={busy} onClick={() => void load(retainedReading.current?.reading_id, retryCursor.current)}>重试</button></div> : null}
    {busy ? <p role="status">正在读取预算资料…</p> : null}
    {reading ? <p className="boundary-note">XI 阅读截止 {utc(reading.cutoff)} · {reading.budget_count} 个可访问预算。来源快照的截止时间分别列出。</p> : null}
    {selected ? <section className="workbench-panel budget-evidence" aria-label="预算依据"><button onClick={async () => { await load(reading?.reading_id); window.setTimeout(() => { Array.from(document.querySelectorAll<HTMLButtonElement>("button[data-budget-ref]")).find(button => button.dataset.budgetRef === opener.current)?.focus(); }, 0); }}>← 返回预算比较</button><h2 tabIndex={-1} ref={evidenceHeading}>预算依据：{selected.budget_ref}</h2>
      <p>来源阅读 {selected.source_reading_id} · 截止 {utc(selected.source_reading_cutoff)} · XI 收到 {utc(selected.received_at)}。</p>
      <p>来源没有提供逐请求成员关系。XI 已接收的费用明细不能与此摘要相加，也不能据两个总额推断缺失明细金额。</p>
      {(["definition", "summary", "alerts"] as const).map(key => <details key={key}><summary>{{ definition: "预算定义", summary: "全周期摘要", alerts: "提醒依据" }[key]}</summary>{selected[key].body ? <><p>{selected[key].source_hash ? `来源摘要：${selected[key].source_hash}` : "按当前查看权展示的字段投影"}</p><pre>{JSON.stringify(selected[key].body, null, 2)}</pre></> : <p>{unavailable(selected[key].state)}</p>}</details>)}
      {reading ? <BudgetHistory readingID={reading.reading_id} itemRef={selected.ref} onInvalid={invalidate} /> : null}
    </section> : <div className="budget-list">{items.map(item => <BudgetCard key={item.ref} item={item} onEvidence={() => { opener.current = item.ref; setSelected(item); }} />)}</div>}
    {!busy && !error && reading && !items.length ? <p>当前范围没有已接收且可访问的预算快照。这不代表预算为零或没有费用。</p> : null}
    {!selected && page?.next_cursor ? <button disabled={busy} onClick={() => void load(reading?.reading_id, page.next_cursor)}>加载更多预算</button> : null}
    <aside className="workbench-panel budget-source-gaps"><h2>仍缺少的来源</h2><p>计算费用预算尚缺正式来源定义。独立账单来源尚未绑定；网关扣费和计算参考值不能替代供应商账单。</p></aside>
  </section>;
}
