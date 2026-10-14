import { t, tx, useLocale } from "./i18n";
import { Identifier } from "./Identifier";
import { contentKey } from "./content";
import { formatDateTime, shortRef } from "./format";
import { conclusionLabels, evidenceKey, gapKey, resourceKey, type BasisSelection, type ReviewBasis, type ReviewPayload } from "./assessmentTypes";

export function ReviewBasisView({ basis, label = t("m1692") }: { basis: ReviewBasis; label?: string }) {
  useLocale();
  return <details className="review-basis"><summary>{tx("m1693", { value0: label, value1: basis.finding_version })}</summary>
    <p>{basis.statement}</p><p className="quiet-copy">{tx("m1694", { value0: basis.known.evidence.length, value1: basis.known.gaps.length, value2: basis.known.resources?.length ?? 0, value3: basis.known.contents?.length ?? 0, value4: basis.selected.evidence.length, value5: basis.selected.gaps.length, value6: basis.selected.resources?.length ?? 0, value7: basis.selected.contents?.length ?? 0 })}</p>
    {basis.resolved_execution_context ? <div aria-label={t("m1695")}><Identifier value={basis.resolved_execution_context.context.execution_id} label={t("m1416")} /><p>{t("m1696")}<time title={basis.resolved_execution_context.context.execution_as_of}>{formatDateTime(basis.resolved_execution_context.context.execution_as_of)}</time></p><p>{t("m1697")}<time title={basis.resolved_execution_context.runtime_cutoff}>{formatDateTime(basis.resolved_execution_context.runtime_cutoff)}</time></p><p>{tx("m1698")}</p></div> : null}
    <SelectionSummary selection={basis.selected} />
    {basis.adopted_resources?.map(r => <article key={resourceKey(r.reference)}><strong>{tx("m1670", { value0: r.record.resource.name, value1: r.reference.revision })}</strong><p>{tx("m1701", { value0: r.record.action, value1: r.record.access_outcome, value2: r.record.withdrawn ? t("m1699") : t("m1700") })}</p><p>{tx("m1702")}</p></article>)}
    {basis.adopted_gaps.map((gap) => <article className="case-gap-citation" key={gapKey(gap.reference)}>
      <strong>{gap.reference.gap_type}</strong><p>{gap.reference.affected_claim}</p>
      {gap.resolution_recorded ? <><p>{tx("m1703", { value0: gap.resolution_version, value1: gap.state })}</p><p>{gap.note}</p><small>{gap.updated_by_subject} · {gap.updated_at ? formatDateTime(gap.updated_at) : ""}</small></> : <p>{tx("m1704")}</p>}
    </article>)}<small>{t("m1705")}<code>{basis.digest}</code></small>
  </details>;
}

export function SelectionSummary({ selection }: { selection: BasisSelection }) {
  useLocale();
  return <div className="review-reference-list">
    {selection.evidence.map(e => <div key={evidenceKey(e)}><strong>{e.scope === "case" ? t("m1706") : t("m1707")} · {referenceRelationLabel(e.relation)}</strong><p>{shortRef(e.source_ref, 20, 8)}</p><details className="technical-reference"><summary>{tx("m1708")}</summary><p>{e.source_ref}</p><code>{e.evidence_event_id}</code><br /><code>{e.event_hash}</code><p>{t("m1709")}<code>{e.relation}</code></p></details></div>)}
    {selection.gaps.map(g => <div key={gapKey(g)}><strong>{g.scope === "case" ? t("m1710") : t("m1711")} · {referenceRelationLabel(g.relation)}</strong><p>{g.affected_claim}</p><details className="technical-reference"><summary>{tx("m1712")}</summary><code>{g.gap_id}</code><p>{t("m1709")}<code>{g.relation}</code> {t("m1713")}<code>{g.gap_type}</code></p></details></div>)}
    {selection.resources?.map(r=><div key={resourceKey(r)}><strong>{tx("m1714", { value0: referenceRelationLabel(r.relation) })}</strong><p>{tx("m1670", { value0: shortRef(r.record_ref, 20, 8), value1: r.revision })}</p><details className="technical-reference"><summary>{tx("m1715")}</summary><p>{t("m1709")}<code>{r.relation}</code></p>{r.record_ref} {t("m1716")}{r.revision}<br />{r.reporter.source_system_id} / {r.reporter.source_instance} / {r.reporter.org_id} / {r.reporter.project_id}<br />{t("m1717")}{r.target.source_system_id} / {r.target.source_instance} / {r.target.org_id} / {r.target.project_id} / {r.request_id}<br /><code>{r.record_hash}</code></details></div>)}
    {selection.contents?.map(c=><div key={contentKey(c)}><strong>{tx("m1718", { value0: referenceRelationLabel(c.relation) })}</strong><p>{tx("m1670", { value0: shortRef(c.reference.source_fact_id, 20, 8), value1: c.reference.revision })}</p><details className="technical-reference"><summary>{tx("m1719")}</summary><p>{t("m1709")}<code>{c.relation}</code></p>{c.reference.source_fact_id} {t("m1716")}{c.reference.revision}<br />{c.reference.source_system_id} / {c.reference.source_instance} / {c.reference.org_id} / {c.reference.project_id} / {c.reference.request_id}<br />{c.location.kind==="byte_range"?t("m1720", { value0: c.location.offset, value1: c.location.length }):t("m1721", { value0: c.location.pointer })}<br /><code>{c.reference.source_sha256}</code><br /><code>{c.reference.representation?.digest}</code></details></div>)}
    {!selection.evidence.length && !selection.gaps.length && !selection.resources?.length && !selection.contents?.length ? <p className="quiet-copy">{tx("m1722")}</p> : null}
  </div>;
}

export function ReferencePicker({ available, selected, onChange, disabled = false, editableRelations = false }: {
  available: BasisSelection; selected: BasisSelection; onChange: (value: BasisSelection) => void; disabled?: boolean; editableRelations?: boolean;
}) {
  useLocale();
  return <fieldset className="review-reference-picker" disabled={disabled}><legend>{tx("m1723")}</legend>
    <p className="quiet-copy">{tx("m1724")}</p>
    {available.evidence.map((e) => {
      const picked = selected.evidence.find((v) => editableRelations ? v.scope === e.scope && v.evidence_event_id === e.evidence_event_id : evidenceKey(v) === evidenceKey(e));
      return <div key={evidenceKey(e)}><label className="review-checkbox"><input type="checkbox" checked={Boolean(picked)} onChange={(event) => onChange({ ...selected, evidence: event.target.checked ? [...selected.evidence, e] : selected.evidence.filter((v) => v !== picked) })} /><span>{e.scope === "case" ? t("m1725") : t("m0162")} · {referenceRelationLabel(picked?.relation ?? e.relation)} · {shortRef(e.source_ref, 20, 8)}</span></label><details className="technical-reference"><summary>{tx("m1726")}</summary><p>{e.source_ref}</p><code>{e.evidence_event_id}</code><br /><code>{e.event_hash}</code><p>{t("m1709")}<code>{picked?.relation ?? e.relation}</code></p></details>
        {editableRelations && picked ? <label>{t("m1727")}<select value={picked.relation} onChange={(event) => onChange({ ...selected, evidence: selected.evidence.map((v) => v === picked ? { ...v, relation: event.target.value as typeof e.relation } : v) })}><option value="supports">{tx("m1548")}</option><option value="contradicts">{tx("m1549")}</option><option value="context">{tx("m1550")}</option></select></label> : null}</div>;
    })}
    {available.gaps.map((g) => {
      const picked = selected.gaps.find((v) => gapKey(v) === gapKey(g));
      return <div key={gapKey(g)}><label className="review-checkbox"><input type="checkbox" checked={Boolean(picked)} onChange={(event) => onChange({ ...selected, gaps: event.target.checked ? [...selected.gaps, g] : selected.gaps.filter((v) => v !== picked) })} /><span>{g.scope === "case" ? t("m1725") : t("m0162")}{t("m1728")}{referenceRelationLabel(g.relation)}<br />{g.affected_claim}</span></label><details className="technical-reference"><summary>{tx("m1729")}</summary><code>{g.gap_id}</code><p>{t("m1043")}<code>{g.gap_type}</code> {t("m1730")}<code>{g.relation}</code></p></details></div>;
    })}
    {available.resources?.map(r=>{
      const picked=selected.resources?.find(v=>editableRelations ? resourceKey({...v,relation:r.relation})===resourceKey(r) : resourceKey(v)===resourceKey(r));
      return <div key={resourceKey(r)}><label className="review-checkbox"><input type="checkbox" checked={Boolean(picked)} onChange={e=>onChange({...selected,resources:e.target.checked?[...(selected.resources??[]),r]:(selected.resources??[]).filter(v=>v!==picked)})}/><span>{tx("m1731", { value0: shortRef(r.record_ref, 20, 8), value1: r.revision, value2: referenceRelationLabel(picked?.relation ?? r.relation) })}</span></label><details className="technical-reference"><summary>{tx("m1732")}</summary><p>{r.record_ref}</p><code>{r.record_hash}</code><p>{t("m1709")}<code>{picked?.relation ?? r.relation}</code></p></details>{editableRelations&&picked?<label>{t("m1733")}<select value={picked.relation} onChange={e=>onChange({...selected,resources:selected.resources?.map(v=>v===picked?{...v,relation:e.target.value as typeof r.relation}:v)})}><option value="supports">{tx("m1548")}</option><option value="contradicts">{tx("m1549")}</option><option value="context">{tx("m1550")}</option></select></label>:null}</div>;
    })}
    {available.contents?.map(c=>{
      const picked=selected.contents?.find(v=>contentKey(editableRelations?{...v,relation:c.relation}:v)===contentKey(c));
      return <div key={contentKey(c)}><label className="review-checkbox"><input type="checkbox" checked={Boolean(picked)} onChange={e=>onChange({...selected,contents:e.target.checked?[...(selected.contents??[]),c]:(selected.contents??[]).filter(v=>v!==picked)})}/><span>{t("m1734")}{shortRef(c.reference.source_fact_id, 20, 8)} {t("m1716")}{c.reference.revision} · {referenceRelationLabel(picked?.relation ?? c.relation)}<br />{c.location.kind==="byte_range"?t("m0430", { value0: c.location.offset, value1: c.location.length }):t("m1721", { value0: c.location.pointer })}</span></label><details className="technical-reference"><summary>{tx("m1735")}</summary><p>{c.reference.source_fact_id}</p><code>{c.reference.source_sha256}</code><br /><code>{c.reference.representation?.digest}</code><p>{t("m1709")}<code>{picked?.relation ?? c.relation}</code></p></details>{editableRelations&&picked?<label>{t("m1736")}<select value={picked.relation} onChange={e=>onChange({...selected,contents:selected.contents?.map(v=>v===picked?{...v,relation:e.target.value as typeof c.relation}:v)})}><option value="supports">{tx("m1548")}</option><option value="contradicts">{tx("m1549")}</option><option value="context">{tx("m1550")}</option></select></label>:null}</div>;
    })}
    {selected.evidence.length + selected.gaps.length + (selected.resources?.length ?? 0) + (selected.contents?.length ?? 0) > 32 ? <p role="alert">{tx("m1737")}</p> : null}
  </fieldset>;
}

export function ReviewFields({ value, onChange, disabled }: { value: ReviewPayload; onChange: (v: ReviewPayload) => void; disabled?: boolean }) {
  useLocale();
  return <fieldset disabled={disabled} className="stack-form"><legend>{tx("m1738")}</legend>
    <label>{t("m1739")}<select value={value.conclusion} onChange={(event) => onChange({ ...value, conclusion: event.target.value as ReviewPayload["conclusion"] })}>{Object.entries(conclusionLabels).map(([key, label]) => <option key={key} value={key}>{label}</option>)}</select></label>
    <label>{t("m1740")}<textarea value={value.reason} maxLength={4000} rows={3} onChange={(event) => onChange({ ...value, reason: event.target.value })} /></label>
    <label>{t("m1741")}<textarea value={value.missing_evidence} maxLength={4000} rows={2} onChange={(event) => onChange({ ...value, missing_evidence: event.target.value })} /></label>
    <p className="quiet-copy">{tx("m1742")}</p>
  </fieldset>;
}

export function ResourceUpdates({ updates }: { updates?: import("./assessmentTypes").ResourceUpdate[] }) {
  useLocale();
  if (!updates?.length) return null;
  const unique = [...new Map(updates.map(u => [resourceKey(u.reference), u])).values()];
  return <div className="resource-updates" aria-label={t("m1743")}>{unique.map(u => <p key={resourceKey(u.reference)}>{tx("m1747", { value0: u.reference.record_ref, value1: u.reference.revision, value2: u.state === "unavailable" ? t("m1744") : u.state === "withdrawn" ? t("m1745", { value0: u.latest_revision }) : t("m1746", { value0: u.latest_revision }) })}</p>)}</div>;
}

function referenceRelationLabel(relation: string) {
  return ({supports:t("m1548"),contradicts:t("m1549"),context:t("m1550"),limits:t("m1557"),blocks:t("m1748")} as Record<string,string>)[relation] || t("m1551");
}
