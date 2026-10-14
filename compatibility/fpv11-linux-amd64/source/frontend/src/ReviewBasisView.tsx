import { Identifier } from "./Identifier";
import { contentKey } from "./content";
import { formatDateTime, shortRef } from "./format";
import { conclusionLabels, evidenceKey, gapKey, resourceKey, type BasisSelection, type ReviewBasis, type ReviewPayload } from "./assessmentTypes";

export function ReviewBasisView({ basis, label = "当时采用的依据" }: { basis: ReviewBasis; label?: string }) {
  return <details className="review-basis"><summary>{label} · 调查发现版本 {basis.finding_version}</summary>
    <p>{basis.statement}</p><p className="quiet-copy">当时已知 {basis.known.evidence.length} 条证据、{basis.known.gaps.length} 条缺口、{basis.known.resources?.length ?? 0} 条资源、{basis.known.contents?.length ?? 0} 条内容；实际采用 {basis.selected.evidence.length} 条证据、{basis.selected.gaps.length} 条缺口、{basis.selected.resources?.length ?? 0} 条资源、{basis.selected.contents?.length ?? 0} 条内容。</p>
    {basis.resolved_execution_context ? <div aria-label="执行阅读关联"><Identifier value={basis.resolved_execution_context.context.execution_id} label="关联执行" /><p>原执行阅读截止 <time title={basis.resolved_execution_context.context.execution_as_of}>{formatDateTime(basis.resolved_execution_context.context.execution_as_of)}</time></p><p>相关资料阅读截止 <time title={basis.resolved_execution_context.runtime_cutoff}>{formatDateTime(basis.resolved_execution_context.runtime_cutoff)}</time></p><p>两份阅读各自保留截止。创建后的复核沿用已保存的执行关系和原资源或内容修订，并继续核对当前权限。</p></div> : null}
    <SelectionSummary selection={basis.selected} />
    {basis.adopted_resources?.map(r => <article key={resourceKey(r.reference)}><strong>{r.record.resource.name} · 修订 {r.reference.revision}</strong><p>当时来源声明：{r.record.action} / {r.record.access_outcome}；撤回标记：{r.record.withdrawn ? "是" : "否"}。</p><p>合成资料；本次采用不证明实际内容、脱敏效果或外发许可。</p></article>)}
    {basis.adopted_gaps.map((gap) => <article className="case-gap-citation" key={gapKey(gap.reference)}>
      <strong>{gap.reference.gap_type}</strong><p>{gap.reference.affected_claim}</p>
      {gap.resolution_recorded ? <><p>解决记录版本 {gap.resolution_version} · {gap.state}</p><p>{gap.note}</p><small>{gap.updated_by_subject} · {gap.updated_at ? formatDateTime(gap.updated_at) : ""}</small></> : <p>当时未记录解决说明；不推定为待解决。</p>}
    </article>)}<small>依据摘要 <code>{basis.digest}</code></small>
  </details>;
}

export function SelectionSummary({ selection }: { selection: BasisSelection }) {
  return <div className="review-reference-list">
    {selection.evidence.map(e => <div key={evidenceKey(e)}><strong>{e.scope === "case" ? "案件引用" : "调查发现引用"} · {referenceRelationLabel(e.relation)}</strong><p>{shortRef(e.source_ref, 20, 8)}</p><details className="technical-reference"><summary>完整证据引用</summary><p>{e.source_ref}</p><code>{e.evidence_event_id}</code><br /><code>{e.event_hash}</code><p>引用关系 <code>{e.relation}</code></p></details></div>)}
    {selection.gaps.map(g => <div key={gapKey(g)}><strong>{g.scope === "case" ? "案件缺口" : "调查发现缺口"} · {referenceRelationLabel(g.relation)}</strong><p>{g.affected_claim}</p><details className="technical-reference"><summary>完整缺口引用</summary><code>{g.gap_id}</code><p>引用关系 <code>{g.relation}</code> · 类型 <code>{g.gap_type}</code></p></details></div>)}
    {selection.resources?.map(r=><div key={resourceKey(r)}><strong>资源引用 · {referenceRelationLabel(r.relation)}</strong><p>{shortRef(r.record_ref, 20, 8)} · 修订 {r.revision}</p><details className="technical-reference"><summary>完整资源引用</summary><p>引用关系 <code>{r.relation}</code></p>{r.record_ref} · 修订 {r.revision}<br />{r.reporter.source_system_id} / {r.reporter.source_instance} / {r.reporter.org_id} / {r.reporter.project_id}<br />目标 {r.target.source_system_id} / {r.target.source_instance} / {r.target.org_id} / {r.target.project_id} / {r.request_id}<br /><code>{r.record_hash}</code></details></div>)}
    {selection.contents?.map(c=><div key={contentKey(c)}><strong>内容引用 · {referenceRelationLabel(c.relation)}</strong><p>{shortRef(c.reference.source_fact_id, 20, 8)} · 修订 {c.reference.revision}</p><details className="technical-reference"><summary>完整内容引用</summary><p>引用关系 <code>{c.relation}</code></p>{c.reference.source_fact_id} · 修订 {c.reference.revision}<br />{c.reference.source_system_id} / {c.reference.source_instance} / {c.reference.org_id} / {c.reference.project_id} / {c.reference.request_id}<br />{c.location.kind==="byte_range"?`保存字节 ${c.location.offset} 起，长度 ${c.location.length}`:`JSON 位置 ${c.location.pointer}`}<br /><code>{c.reference.source_sha256}</code><br /><code>{c.reference.representation?.digest}</code></details></div>)}
    {!selection.evidence.length && !selection.gaps.length && !selection.resources?.length && !selection.contents?.length ? <p className="quiet-copy">本次没有采用来源引用。可以记录待复核或明确的缺证说明。</p> : null}
  </div>;
}

export function ReferencePicker({ available, selected, onChange, disabled = false, editableRelations = false }: {
  available: BasisSelection; selected: BasisSelection; onChange: (value: BasisSelection) => void; disabled?: boolean; editableRelations?: boolean;
}) {
  return <fieldset className="review-reference-picker" disabled={disabled}><legend>本次采用的证据、缺口、资源与内容（最多 32 条）</legend>
    <p className="quiet-copy">只采用勾选的引用；案件级引用需要明确选择。取消勾选不会删除原引用。</p>
    {available.evidence.map((e) => {
      const picked = selected.evidence.find((v) => editableRelations ? v.scope === e.scope && v.evidence_event_id === e.evidence_event_id : evidenceKey(v) === evidenceKey(e));
      return <div key={evidenceKey(e)}><label className="review-checkbox"><input type="checkbox" checked={Boolean(picked)} onChange={(event) => onChange({ ...selected, evidence: event.target.checked ? [...selected.evidence, e] : selected.evidence.filter((v) => v !== picked) })} /><span>{e.scope === "case" ? "案件" : "调查发现"} · {referenceRelationLabel(picked?.relation ?? e.relation)} · {shortRef(e.source_ref, 20, 8)}</span></label><details className="technical-reference"><summary>证据引用详情</summary><p>{e.source_ref}</p><code>{e.evidence_event_id}</code><br /><code>{e.event_hash}</code><p>引用关系 <code>{picked?.relation ?? e.relation}</code></p></details>
        {editableRelations && picked ? <label>引用关系<select value={picked.relation} onChange={(event) => onChange({ ...selected, evidence: selected.evidence.map((v) => v === picked ? { ...v, relation: event.target.value as typeof e.relation } : v) })}><option value="supports">支持</option><option value="contradicts">反证</option><option value="context">背景</option></select></label> : null}</div>;
    })}
    {available.gaps.map((g) => {
      const picked = selected.gaps.find((v) => gapKey(v) === gapKey(g));
      return <div key={gapKey(g)}><label className="review-checkbox"><input type="checkbox" checked={Boolean(picked)} onChange={(event) => onChange({ ...selected, gaps: event.target.checked ? [...selected.gaps, g] : selected.gaps.filter((v) => v !== picked) })} /><span>{g.scope === "case" ? "案件" : "调查发现"}缺口 · {referenceRelationLabel(g.relation)}<br />{g.affected_claim}</span></label><details className="technical-reference"><summary>缺口引用详情</summary><code>{g.gap_id}</code><p>类型 <code>{g.gap_type}</code> · 关系 <code>{g.relation}</code></p></details></div>;
    })}
    {available.resources?.map(r=>{
      const picked=selected.resources?.find(v=>editableRelations ? resourceKey({...v,relation:r.relation})===resourceKey(r) : resourceKey(v)===resourceKey(r));
      return <div key={resourceKey(r)}><label className="review-checkbox"><input type="checkbox" checked={Boolean(picked)} onChange={e=>onChange({...selected,resources:e.target.checked?[...(selected.resources??[]),r]:(selected.resources??[]).filter(v=>v!==picked)})}/><span>资源 {shortRef(r.record_ref, 20, 8)} · 修订 {r.revision} · {referenceRelationLabel(picked?.relation ?? r.relation)}</span></label><details className="technical-reference"><summary>资源引用详情</summary><p>{r.record_ref}</p><code>{r.record_hash}</code><p>引用关系 <code>{picked?.relation ?? r.relation}</code></p></details>{editableRelations&&picked?<label>资源引用关系<select value={picked.relation} onChange={e=>onChange({...selected,resources:selected.resources?.map(v=>v===picked?{...v,relation:e.target.value as typeof r.relation}:v)})}><option value="supports">支持</option><option value="contradicts">反证</option><option value="context">背景</option></select></label>:null}</div>;
    })}
    {available.contents?.map(c=>{
      const picked=selected.contents?.find(v=>contentKey(editableRelations?{...v,relation:c.relation}:v)===contentKey(c));
      return <div key={contentKey(c)}><label className="review-checkbox"><input type="checkbox" checked={Boolean(picked)} onChange={e=>onChange({...selected,contents:e.target.checked?[...(selected.contents??[]),c]:(selected.contents??[]).filter(v=>v!==picked)})}/><span>内容 {shortRef(c.reference.source_fact_id, 20, 8)} · 修订 {c.reference.revision} · {referenceRelationLabel(picked?.relation ?? c.relation)}<br />{c.location.kind==="byte_range"?`字节 ${c.location.offset} 起，长度 ${c.location.length}`:`JSON 位置 ${c.location.pointer}`}</span></label><details className="technical-reference"><summary>内容引用详情</summary><p>{c.reference.source_fact_id}</p><code>{c.reference.source_sha256}</code><br /><code>{c.reference.representation?.digest}</code><p>引用关系 <code>{picked?.relation ?? c.relation}</code></p></details>{editableRelations&&picked?<label>内容引用关系<select value={picked.relation} onChange={e=>onChange({...selected,contents:selected.contents?.map(v=>v===picked?{...v,relation:e.target.value as typeof c.relation}:v)})}><option value="supports">支持</option><option value="contradicts">反证</option><option value="context">背景</option></select></label>:null}</div>;
    })}
    {selected.evidence.length + selected.gaps.length + (selected.resources?.length ?? 0) + (selected.contents?.length ?? 0) > 32 ? <p role="alert">本次选择超过 32 条，请缩小采用范围。</p> : null}
  </fieldset>;
}

export function ReviewFields({ value, onChange, disabled }: { value: ReviewPayload; onChange: (v: ReviewPayload) => void; disabled?: boolean }) {
  return <fieldset disabled={disabled} className="stack-form"><legend>人工复核结论</legend>
    <label>结论<select value={value.conclusion} onChange={(event) => onChange({ ...value, conclusion: event.target.value as ReviewPayload["conclusion"] })}>{Object.entries(conclusionLabels).map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select></label>
    <label>复核理由<textarea value={value.reason} maxLength={4000} rows={3} onChange={(event) => onChange({ ...value, reason: event.target.value })} /></label>
    <label>缺少什么（证据不足时可明确说明）<textarea value={value.missing_evidence} maxLength={4000} rows={2} onChange={(event) => onChange({ ...value, missing_evidence: event.target.value })} /></label>
    <p className="quiet-copy">证据支持须有支持引用，证据不支持须有反证。结论属于本次人工复核，不改变源事实。</p>
  </fieldset>;
}

export function ResourceUpdates({ updates }: { updates?: import("./assessmentTypes").ResourceUpdate[] }) {
  if (!updates?.length) return null;
  const unique = [...new Map(updates.map(u => [resourceKey(u.reference), u])).values()];
  return <div className="resource-updates" aria-label="资源当前状态">{unique.map(u => <p key={resourceKey(u.reference)}>
    资源 {u.reference.record_ref}：已采用修订 {u.reference.revision}；{u.state === "unavailable" ? "当前状态不可用" : u.state === "withdrawn" ? `后续修订 ${u.latest_revision} 已声明撤回` : `已有后续修订 ${u.latest_revision}`}。历史采用与判断保持不变，需人工决定是否重新复核。
  </p>)}</div>;
}

function referenceRelationLabel(relation: string) {
  return ({supports:"支持",contradicts:"反证",context:"背景",limits:"限制",blocks:"阻断"} as Record<string,string>)[relation] || "关系待确认";
}
