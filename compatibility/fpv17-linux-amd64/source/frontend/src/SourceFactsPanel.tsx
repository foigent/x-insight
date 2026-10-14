import { useEffect, useRef, useState } from "react";
import {
  ApiRequestError, type WorkbenchReading, type WorkbenchSourceObservation,
  type WorkbenchSourceObservations, workbenchAPI,
} from "./api";
import "./source-facts.css";

export function SourceFactsPanel({ reading, onInvalidated }: {
  reading: WorkbenchReading; onInvalidated: (failure: unknown) => void;
}) {
  const [page, setPage] = useState<WorkbenchSourceObservations | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const generation = useRef(0);
  const failureHandler = useRef(onInvalidated);
  failureHandler.current = onInvalidated;
  const abort = useRef<AbortController | null>(null);

  async function load(cursor?: string) {
    const sequence = ++generation.current;
    abort.current?.abort();
    const controller = new AbortController();
    abort.current = controller;
    setBusy(true); setError("");
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
      setError(failure instanceof ApiRequestError && failure.status === 403
        ? "当前授权不包含来源字段明细；已授权汇总仍可阅读。"
        : "来源明细暂时不可用，可以重试。");
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
    <header><div><span className="eyebrow">来源解释</span><h2>本阅读的来源事实</h2></div>
      <span>{coverage.known_documents} 份已授权记录</span></header>
    <p>来源阶段和修订不另计调用或金额。迟到事实与更正将在新建阅读后采用，当前阅读保持原解释。</p>
    <div className="source-facts-coverage" aria-label="来源解释覆盖">
      <span>未关联经济事实 <b>{coverage.economic_not_received}</b></span>
      <span>修订待前驱 <b>{coverage.pending_predecessor}</b></span>
      <span>经济引用不匹配 <b>{coverage.economic_hash_conflicts}</b></span>
      <span>独立媒体经济状态 <b>{coverage.media_pending}</b></span>
      <span>原字段未采集 <b>{coverage.not_captured}</b></span>
    </div>
    {coverage.economic_not_received > 0 ? <p className="source-facts-notice">尚未收到可关联的经济事实，不代表零费用；费用图表仅展示已收到的经济事实小计。</p> : null}
    {coverage.media_pending > 0 ? <p className="source-facts-notice">媒体来源使用独立经济依据，未定价或不适用的记录不进入文本费用排名。</p> : null}
    <p className="source-facts-limit">只覆盖已接收且已授权的来源记录，不保证完整请求时间线。这些来源记录不证明实际操作者或单次业务用途；人工用途判断另列。</p>
    {coverage.requires_attention ? <p className="source-facts-notice" role="status">建立本阅读时，存在需管理员处理的采集注册。自动重启不会恢复接收，修正问题并确认后才能继续；历史合法记录仍可阅读。</p> : null}
    {coverage.collection_states?.length ? <div aria-label="本阅读的采集注册状态">
      <p>建立本阅读时，已授权范围内的采集注册状态：{coverage.collection_states.map(state => collectionLabels[state]).join("、")}。</p>
      {coverage.collection_states.includes("paused") ? <p className="source-facts-notice">存在已暂停的采集注册，新事实不会通过这些注册接收；历史合法记录仍可阅读。</p> : null}
      {coverage.collection_states.includes("revoked") ? <p className="source-facts-notice">存在已撤销的采集注册；这不等于人员阅读权限被撤销，已获阅读授权的历史记录继续保留。</p> : null}
    </div> : <p className="source-facts-limit">本阅读未记录采集注册状态，不能据此判断正在正常采集。</p>}
    {coverage.gaps.includes("source_fact_conflict") ? <p className="source-facts-notice">来源内容或修订发生冲突，本次消费已停止，原合法记录与接收位置保持；修复后需成功接收并显式新建阅读。</p> : null}
    {coverage.gaps.some(gap => !["forward_only_capture", "timeline_completeness_unknown", "source_fact_conflict"].includes(gap))
      ? <p className="source-facts-notice">建立本阅读时存在来源连接或游标缺口，历史合法记录继续保留；需要修复来源后显式新建阅读。</p> : null}
    {page?.items.map(fact => <SourceFactCard key={`${fact.source_system_id}:${fact.source_fact_id}`} fact={fact} />)}
    {page?.items.length === 0 ? <p>当前范围没有可展示的来源字段明细。</p> : null}
    {error ? <div role="status"><p>{error}</p><button disabled={busy} onClick={() => void load(page?.next_cursor)}>重试来源明细</button></div> : null}
    {page?.next_cursor ? <button disabled={busy} className="text-button" onClick={() => void load(page.next_cursor)}>加载更多来源记录</button> : null}
  </article>;
}

const collectionLabels = { active: "已启用", paused: "已暂停", revoked: "已撤销" };

const profileNames: Record<string, string> = {
  "unirouter.execution-context.v1": "执行上下文",
  "unirouter.execution-identity.v1": "账户与授权来源",
  "unirouter.cost-basis.v1": "计费依据",
};
const stageNames: Record<string, string> = { authenticated: "认证观察", admission: "准入观察", dispatch: "派发观察", terminal: "终态观察" };
const economics: Record<string, string> = {
  not_evaluated: "本阅读只关联请求身份，尚未核验此记录的经济引用。",
  received: "已关联收到的经济事实", not_received: "经济事实尚未收到或未关联，不代表零费用",
  hash_mismatch: "经济引用不匹配，尚未采用计费解释", media_pricing_pending: "独立媒体经济依据，费率待定",
  media_not_eligible: "未形成可用的媒体经济依据",
};
const contextLabels: Record<string, string> = { workload_application_id: "应用标识", credential_ref: "逻辑凭据引用", credential_id: "逻辑凭据标识", api_key_id: "来源 Key 标识", key_type: "Key 类型", auth_kind: "认证方式", auth_epoch: "捕获时认证版本", protocol: "协议", operation: "操作", captured_at: "捕获时间" };
const identityLabels: Record<string, string> = { owner_user_id: "捕获时账户归属", grant_id: "授权标识", grant_revision: "授权修订", grant_subject_type: "授权主体类型", grant_subject_user_id: "授权用户主体", grant_subject_workload_id: "授权工作负载", sponsor_user_id: "Sponsor 标识", workflow_id: "工作流标识", root_agent_id: "根 Agent", agent_id: "Agent", parent_agent_id: "上级 Agent", authority_version: "来源权威版本", authority_read: "权威读取方式" };
const basisLabels: Record<string, string> = { billing_mode: "计费模式", mode_source: "模式来源", pricing_authority: "定价权威", pricing_version: "冻结定价版本", rate_card_ref: "费率表引用", rate_card_version: "费率表版本", rate_card_layer: "费率层", input_rate_micros_per_million: "输入费率（微元/百万）", output_rate_micros_per_million: "输出费率（微元/百万）", cache_read_rate_micros_per_million: "缓存读取费率（微元/百万）", cache_creation_rate_micros_per_million: "缓存创建费率（微元/百万）", price_multiplier: "价格倍率" };

function object(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}
function valueText(value: unknown): string { return typeof value === "string" ? value || "来源保留为空" : "未知"; }
function Fields({ values, labels }: { values: Record<string, unknown>; labels: Record<string, string> }) {
  return <dl>{Object.entries(labels).filter(([key]) => key in values).map(([key, label]) => <div key={key}><dt>{label}</dt><dd>{valueText(values[key])}</dd></div>)}</dl>;
}

export function SourceFactCard({ fact }: { fact: WorkbenchSourceObservation }) {
  const basis = object(fact.cost_basis?.basis);
  const reference = object(fact.cost_basis?.economic_reference);
  const pricing = basis.pricing_state === "priced" ? "已定价（零费率亦为已定价）" : basis.pricing_state === "unpriced" ? "未定价" : "定价状态未知";
  return <section className="source-fact-card" aria-label={`${profileNames[fact.profile] || "来源记录"} 修订 ${fact.revision}`}>
    <header><strong>{profileNames[fact.profile] || "来源记录"}</strong><span>{stageNames[fact.stage] || "来源阶段"} · 修订 {fact.revision}</span></header>
    <p className="source-fact-request">请求 {fact.request_id}</p>
    <p>{economics[fact.economic_state]}</p>
    {fact.chain_state === "missing_predecessor" ? <p className="source-facts-notice">修订已持久化，前驱尚未收齐；字段暂不用于归属或贡献解释。</p> : null}
    {fact.capture_status === "not_captured" ? <p>原时点未采集这些字段，保持未知。</p> : null}
    {fact.context ? <Fields values={fact.context} labels={contextLabels} /> : null}
    {fact.identity ? <><Fields values={fact.identity} labels={identityLabels} />
      <p>账户归属和 Sponsor 不代表实际操作者；实际操作者未知。</p>
      {Array.isArray(fact.identity.lineage) ? <p>验证过的 Agent 链：{fact.identity.lineage.filter(item => typeof item === "string").join(" → ")}</p> : null}</> : null}
    {fact.cost_basis ? <><p><strong>{pricing}</strong> · 原经济修订 {valueText(reference.revision)}</p>
      <p>{reference.source === "async_media_charge_fact" ? "媒体独立经济合同" : reference.publication_status === "not_published_by_i01" ? "来源未发布 I-01；预期哈希不是已收到金额的证明。" : "引用原 I-01 经济事实；此记录不另加金额。"}</p>
      <details><summary>冻结费率与来源版本</summary><Fields values={basis} labels={basisLabels} />
        <Fields values={fact.cost_basis} labels={{ basis_status: "费率依据状态", journal_billing_mode: "流水计费模式", journal_mode_source: "流水解释来源" }} />
        {Array.isArray(basis.unit_rates) ? <ul>{basis.unit_rates.map((rate, index) => { const r = object(rate); return <li key={index}>{valueText(r.meter)}：每 {valueText(r.unit_quantity)} 单位，{valueText(r.amount_minor)} 微元</li>; })}</ul> : null}
      </details></> : null}
    <details><summary>来源修订与核对引用</summary><dl>
      <div><dt>来源记录</dt><dd>{fact.source_fact_id}</dd></div>
      <div><dt>发生时间</dt><dd>{fact.occurred_at}</dd></div>
      <div><dt>来源记录时间</dt><dd>{fact.recorded_at}</dd></div>
      <div><dt>来源内容摘要</dt><dd>{fact.source_hash}</dd></div>
      <div><dt>传递事件摘要</dt><dd>{fact.envelope_hash}</dd></div>
    </dl></details>
  </section>;
}
