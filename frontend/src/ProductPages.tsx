import { getLocale, t, tx, useLocale } from "./i18n";
import { Fragment, FormEvent, type ReactNode, useEffect, useState } from "react";

import {
	ApiRequestError,
  investigationAPI,
  isAbortError,
  workspaceAPI,
  type AccessSessionResponse,
  type AuthoritativeSessionDetail,
  type AuthoritativeSessionListResponse,
  type CaseRecord,
  type ConnectorListResponse,
  type ConnectorInvitation,
  type DiscoveryHypothesis,
  type ExecutionComparison,
  type InvestigationAlert,
  type InvestigationCase,
  type ServiceIdentity,
} from "./api";
import { EmptyState, ErrorState, LoadingBlock } from "./Feedback";
import { formatCanonicalTime, formatDateTime, shortRef } from "./format";
import { ConnectorSyncPanel } from "./ConnectorSyncPanel";
import { FindingLifecycle } from "./FindingLifecycle";
import { newFindingReferences, ReviewStart } from "./ReviewStart";

interface PageProps {
  session: AccessSessionResponse;
  navigate: (path: string) => void;
}

export function CasesPage({ session, navigate }: PageProps) {
  useLocale();
  const canWrite = session.session.permissions.includes("investigation.write");
  const [items, setItems] = useState<InvestigationCase[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    workspaceAPI.listCases(controller.signal)
      .then((response) => setItems(response.items))
      .catch((caught) => {
        if (!isAbortError(caught) && !controller.signal.aborted) setError(caught);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, []);

  async function create(event: FormEvent) {
    event.preventDefault();
    if (!title.trim()) return;
    setSaving(true);
    setError(undefined);
    try {
      const item = await workspaceAPI.createCase(title.trim(), description, session.csrf_token);
      setItems((current) => [item, ...current]);
      setTitle("");
      setDescription("");
      navigate(`/cases/${item.id}`);
    } catch (caught) {
      setError(caught);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow={t("m1495")}
        title={t("m0161")}
        description={t("m1496")}
      />
      <div className={canWrite ? "split-layout" : "split-layout split-layout--list-only"}>
        {canWrite ? <section className="panel" aria-labelledby="new-case-title">
          <PanelHeading eyebrow={t("m1497")} title={t("m1498")} id="new-case-title" />
          <form className="stack-form" onSubmit={(event) => void create(event)}>
            <label>{t("m1499")}<input value={title} maxLength={160} onChange={(event) => setTitle(event.target.value)} /></label>
            <label>{t("m1352")}<textarea value={description} maxLength={4000} rows={5} onChange={(event) => setDescription(event.target.value)} /></label>
            <button className="button button-primary" disabled={saving || !title.trim()}>{saving ? t("m1500") : t("m1501")}</button>
          </form>
          <ReviewStart session={session} available={{ evidence: [], gaps: [] }} navigate={navigate} />
        </section> : null}
        <section className="panel panel-grow" aria-labelledby="case-list-title">
          <PanelHeading eyebrow={t("m1502")} title={t("m1503")} id="case-list-title" />
          {loading ? <LoadingBlock label={t("m1504")} /> : null}
          {error ? <ErrorState error={error} /> : null}
          {!loading && items.length === 0 ? <EmptyState title={t("m1505")} description={canWrite ? t("m1506") : t("m1507")} /> : null}
          <div className="case-card-grid">
            {items.map((item) => (
              <button className="case-card" key={item.id} title={t("m1508", { value0: item.id })} onClick={() => navigate(`/cases/${item.id}`)}>
                <span className={`case-status case-${item.status}`}>{{open:t("m1509"),sealed:t("m0840"),archived:t("m1510")}[item.status]}</span>
                <strong>{item.title}</strong>
                <p>{item.description || t("m1511")}</p>
                <footer><span>{tx("m1512")}</span><time>{formatDateTime(item.updated_at)}</time></footer>
              </button>
            ))}
          </div>
        </section>
      </div>
    </div>
  );
}

export function CaseDetailPage({
  caseID,
  session,
  navigate,
}: PageProps & { caseID: string }) {
  useLocale();
	const canWrite = session.session.permissions.includes("investigation.write") || session.session.permissions.includes("investigation.scoped.write");
 const canExport = session.session.permissions.includes("investigation.write");
  const [record, setRecord] = useState<CaseRecord>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>();
  const [findingTitle, setFindingTitle] = useState("");
  const [findingStatement, setFindingStatement] = useState("");
  const [findingClassification, setFindingClassification] = useState<"hypothesis" | "unknown">("hypothesis");
  const [note, setNote] = useState("");
  const [saving, setSaving] = useState(false);
  const [sealArmed, setSealArmed] = useState(false);

  const reload = async () => {
    setLoading(true);
    try {
      setRecord(await workspaceAPI.getCase(caseID));
      setError(undefined);
    } catch (caught) {
		if (caught instanceof ApiRequestError && [401, 403, 404].includes(caught.status)) setRecord(undefined);
      setError(caught);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void reload();
    // caseID is the stable resource identity for this page.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [caseID]);

  useEffect(() => {
    const findingID = new URLSearchParams(window.location.search).get("finding");
    if (!record || !findingID || !record.findings.some(item => item.id === findingID)) return;
    const element = document.getElementById(findingID);
    element?.scrollIntoView?.({ block: "start" });
    element?.focus({ preventScroll: true });
  }, [record?.case.id]);

  async function addFinding(event: FormEvent) {
    event.preventDefault();
    if (!findingTitle.trim() || !findingStatement.trim()) return;
    setSaving(true);
    try {
      await workspaceAPI.createFinding(
        caseID,
        findingTitle.trim(),
        findingStatement.trim(),
        findingClassification,
        session.csrf_token,
      );
      setFindingTitle("");
      setFindingStatement("");
      setFindingClassification("hypothesis");
      await reload();
    } catch (caught) {
      setError(caught);
    } finally {
      setSaving(false);
    }
  }

  async function addNote(event: FormEvent) {
    event.preventDefault();
    if (!note.trim()) return;
    setSaving(true);
    try {
      await workspaceAPI.createAnnotation(caseID, "case", caseID, note.trim(), session.csrf_token);
      setNote("");
      await reload();
    } catch (caught) {
      setError(caught);
    } finally {
      setSaving(false);
    }
  }

  async function seal() {
    setSaving(true);
    try {
      await workspaceAPI.sealCase(caseID, session.csrf_token);
      setSealArmed(false);
      await reload();
    } catch (caught) {
      setError(caught);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="page-stack">
      <button className="back-link" onClick={() => { const target = new URLSearchParams(window.location.search).get("queue_return"); navigate(target === "/investigation/queue" || target?.startsWith("/investigation/queue?") ? target : "/cases"); }}>{tx("m0455", { value0: new URLSearchParams(window.location.search).has("queue_return") ? t("m0134") : t("m0161") })}</button>
      {loading && !record ? <LoadingBlock label={t("m1513")} /> : null}
      {error ? <ErrorState error={error} /> : null}
      {record ? (
        <>
          <PageHeader
            eyebrow={t("m1514", { value0: record.case.status === "open" ? t("m1509") : record.case.status === "sealed" ? t("m0840") : record.case.status === "archived" ? t("m1510") : t("m0965") })}
            title={record.case.title}
            description={record.case.description || t("m1515")}
            action={
              record.case.status === "open" && canExport ? (
                sealArmed ? (
                  <div className="button-cluster">
                    <button className="button button-secondary" disabled={saving} onClick={() => setSealArmed(false)}>{tx("m1516")}</button>
                    <button className="button button-danger" disabled={saving} onClick={() => void seal()}>{tx("m1517", { value0: record.case.version + 1 })}</button>
                  </div>
                ) : (
                  <button className="button button-primary" disabled={saving} onClick={() => setSealArmed(true)}>{tx("m1518")}</button>
                )
              ) : record.case.status === "sealed" && canExport ? (
                <div className="button-cluster">
                  <a className="button button-secondary" href={`/api/v1/cases/${caseID}/evidence-pack.json`}>{tx("m1519")}</a>
                  <a className="button button-primary" href={`/api/v1/cases/${caseID}/evidence-pack.html?lang=${getLocale()}`}>{tx("m1520")}</a>
                </div>
              ) : null
            }
          />
          <details className="technical-reference"><summary>{tx("m1521")}</summary><code>{record.case.status}</code></details>
          {record.case.sealed_digest ? (
            <div className="sealed-banner"><strong>{tx("m1522", { value0: record.case.sealed_revision })}</strong><code>{record.case.sealed_digest}</code></div>
          ) : null}
          {record.case.status === "open" ? (
            <div className="content-boundary-warning" role="note">
              <strong>{tx("m1523")}</strong>
              <p>{tx("m1524")}</p>
              {sealArmed ? <p><strong>{tx("m1525")}</strong>{t("m1526")}</p> : null}
            </div>
          ) : null}
          <div className="case-dossier">
            <section className="panel dossier-main">
              <PanelHeading eyebrow={t("m1527")} title={t("m1528", { value0: record.findings.length })} id="case-findings-title" />
              <div className="dossier-list">
                {record.findings.map((finding) => (
                  <article key={finding.id} id={finding.id} tabIndex={-1}>
                    <FindingClassificationBadge classification={finding.classification} />
                    <h3>{finding.title}</h3>
                    <p>{finding.statement}</p>
                    <footer><time>{formatDateTime(finding.created_at)}</time></footer>
                    <details><summary>{tx("m1529")}</summary><code>{finding.id}</code><br /><code>{finding.classification}</code></details>
                    <FindingLifecycle record={record} findingID={finding.id} session={session} onChanged={() => void reload()} navigate={navigate} />
                  </article>
                ))}
                {record.findings.length === 0 ? <p className="quiet-copy">{tx("m1530")}</p> : null}
              </div>
              {record.case.status === "open" && canWrite ? <ReviewStart session={session} available={newFindingReferences(record)} defaultCase={record.case} navigate={navigate} /> : null}
              {record.case.status === "open" && canWrite ? (
                <details className="inline-editor disclosure-editor">
                  <summary>{tx("m1531")}</summary>
                  <form className="stack-form" onSubmit={(event) => void addFinding(event)}>
                    <label>{t("m1499")}<input value={findingTitle} maxLength={160} onChange={(event) => setFindingTitle(event.target.value)} /></label>
                    <label>{t("m1532")}<textarea value={findingStatement} maxLength={8000} rows={4} onChange={(event) => setFindingStatement(event.target.value)} /></label>
                    <label>{t("m1533")}<select
                        value={findingClassification}
                        onChange={(event) => setFindingClassification(event.target.value as "hypothesis" | "unknown")}
                      >
                        <option value="hypothesis">{tx("m1534")}</option>
                        <option value="unknown">{tx("m1535")}</option>
                      </select>
                    </label>
                    <p className="quiet-copy">{tx("m1536")}</p>
                    <button className="button button-secondary" disabled={saving}>{tx("m1531")}</button>
                  </form>
                </details>
              ) : null}
            </section>
            <aside className="dossier-aside">
              <section className="panel">
                <PanelHeading eyebrow={t("m1537")} title={t("m1416")} id="case-executions-title" />
                <div className="compact-list">
                  {record.executions.map((link, index) => (
                    <Fragment key={`${link.execution_id}:${link.as_of}`}><button onClick={() => navigate(`/executions/${link.execution_id}?as_of=${encodeURIComponent(link.as_of)}`)}>
                      <span><strong>{tx("m1538", { value0: index + 1 })}</strong><small>{tx("m1539", { value0: formatDateTime(link.as_of) })}</small></span>
                    </button><TechnicalReference label={t("m0462")} value={link.execution_id} /></Fragment>
                  ))}
                  {record.executions.length === 0 ? <p className="quiet-copy">{record.evidence.length+record.gaps.length+(record.resources?.length??0)+(record.contents?.length??0)>0?t("m1540"):t("m1541")}</p> : null}
                </div>
                {record.alerts.length > 0 ? (
                  <div className="compact-list">
                    {record.alerts.map((link) => (
                      <div key={link.alert_id}>
                        <span><strong>Alert {shortRef(link.alert_id)}</strong><small>{link.alert_state} · {formatDateTime(link.linked_at)}</small></span>
                      </div>
                    ))}
                  </div>
                ) : null}
              </section>
              <section className="panel">
                <PanelHeading eyebrow={t("m1542")} title={t("m1543", { value0: record.evidence.length, value1: record.gaps.length, value2: record.resources?.length??0, value3: record.contents?.length??0 })} id="case-citations-title" />
                <div className="citation-counts">
                  <strong>{record.evidence.length}<small>{tx("m1544")}</small></strong>
                  <strong>{record.gaps.length}<small>{tx("m1545")}</small></strong>
                  <strong>{record.resources?.length??0}<small>{tx("m1546")}</small></strong>
                  <strong>{record.contents?.length??0}<small>{tx("m1547")}</small></strong>
                </div>
                <div className="case-citation-list">
                  {record.evidence.map((citation) => (
                    <article key={`${citation.finding_id ?? "case"}:${citation.evidence_event_id}:${citation.relation}`}>
                      <span className="evidence-kind kind-direct">{tx("m1552", { value0: citation.relation === "supports" ? t("m1548") : citation.relation === "contradicts" ? t("m1549") : citation.relation === "context" ? t("m1550") : t("m1551") })}</span>
                      <small>{citation.finding_id ? t("m1553", { value0: shortRef(citation.finding_id) }) : t("m1554")}</small>
                      <code title={citation.evidence_event_id}>{shortRef(citation.evidence_event_id)}</code>
                      <TechnicalReference label={t("m1555")} value={citation.relation} /><TechnicalReference label={t("m1556")} value={citation.event_hash} />
                      <small title={citation.source_ref}>{citation.source_ref}</small>
                    </article>
                  ))}
                  {record.gaps.map((gap) => (
                    <article className="case-gap-citation" key={`${gap.finding_id ?? "case"}:${gap.gap_id}:${gap.relation}`}>
                      <span className="evidence-kind kind-gap">{tx("m1559", { value0: gap.relation === "limits" ? t("m1557") : gap.relation === "blocks" ? t("m1558") : t("m1550") })}</span>
                      <small>{gap.finding_id ? t("m1553", { value0: shortRef(gap.finding_id) }) : t("m1560")}</small>
                      <strong>{tx("m1561")}</strong><TechnicalReference label={t("m1562")} value={gap.gap_type} /><TechnicalReference label={t("m1563")} value={gap.relation} />
                      <p>{gap.affected_claim}</p>
                      <code title={gap.gap_id}>{shortRef(gap.gap_id)}</code>
                    </article>
                  ))}
                  {record.evidence.length === 0 && record.gaps.length === 0 && !(record.resources?.length) && !(record.contents?.length) ? (
                    <p className="quiet-copy">{tx("m1564")}</p>
                  ) : null}
                </div>
              </section>
              <section className="panel">
                <PanelHeading eyebrow={t("m1565")} title={t("m1566")} id="case-notes-title" />
                <div className="note-list">
                  {record.annotations.map((item) => (
                    <article key={item.id}>
                      <p>{item.body}</p>
                      <small>{shortRef(item.created_by_subject)} · {formatDateTime(item.created_at)}</small>
                    </article>
                  ))}
                </div>
                {record.case.status === "open" && canWrite ? (
                  <form className="stack-form" onSubmit={(event) => void addNote(event)}>
                    <label>{t("m1567")}<textarea value={note} maxLength={4000} rows={3} onChange={(event) => setNote(event.target.value)} /></label>
                    <button className="button button-secondary" disabled={saving || !note.trim()}>{tx("m1568")}</button>
                  </form>
                ) : null}
              </section>
            </aside>
          </div>
        </>
      ) : null}
    </div>
  );
}

export function SessionsPage({ navigate }: PageProps) {
  useLocale();
  const [result, setResult] = useState<AuthoritativeSessionListResponse>();
  const [error, setError] = useState<unknown>();
  useEffect(() => {
    const controller = new AbortController();
    investigationAPI.listSessions(controller.signal)
      .then(setResult)
      .catch((caught) => {
        if (!isAbortError(caught) && !controller.signal.aborted) setError(caught);
      });
    return () => controller.abort();
  }, []);
  return (
    <div className="page-stack">
      <PageHeader
        eyebrow="Source-declared identity"
        title={t("m1569")}
        description={t("m1570")}
      />
      {error ? <ErrorState error={error} /> : null}
      {!result ? <LoadingBlock label={t("m1571")} /> : null}
      {result?.items.length === 0 ? <EmptyState title={t("m1572")} description={t("m1573")} /> : null}
      <div className="session-grid">
        {result?.items.map((item) => (
          <button className="session-item" key={item.session_id} onClick={() => navigate(`/sessions/${item.session_id}`)}>
            <span className="evidence-kind kind-declared">Source-declared</span>
            <strong>{item.source_session_ref}</strong>
            <p>{item.source_display_name} · {item.source_session_type}</p>
            <footer><span>{item.execution_count} executions</span><time>{formatDateTime(item.observed_at)}</time></footer>
          </button>
        ))}
      </div>
    </div>
  );
}

export function SessionDetailPage({ sessionID, navigate }: PageProps & { sessionID: string }) {
  useLocale();
  const [detail, setDetail] = useState<AuthoritativeSessionDetail>();
  const [error, setError] = useState<unknown>();
  useEffect(() => {
    const controller = new AbortController();
    investigationAPI.getSession(sessionID, controller.signal)
      .then(setDetail)
      .catch((caught) => {
        if (!isAbortError(caught) && !controller.signal.aborted) setError(caught);
      });
    return () => controller.abort();
  }, [sessionID]);
  return (
    <div className="page-stack">
      <button className="back-link" onClick={() => navigate("/sessions")}>{tx("m1574")}</button>
      {error ? <ErrorState error={error} /> : null}
      {!detail ? <LoadingBlock label={t("m1575")} /> : null}
      {detail ? (
        <>
          <PageHeader
            eyebrow="Authoritative Session"
            title={detail.session.source_session_ref}
            description={t("m1576", { value0: detail.session.source_display_name, value1: detail.session.source_session_type })}
          />
          <section className="panel">
            <PanelHeading eyebrow="Execution membership" title={t("m1577")} id="session-members-title" />
            {detail.executions.length === 0 ? (
              <EmptyState
                title={t("m1578")}
                description={t("m1579")}
              />
            ) : (
              <div className="record-list">
                {detail.executions.map((link) => (
                  <button className="record-row" key={link.execution_id} onClick={() => navigate(`/executions/${link.execution_id}`)}>
                    <span><strong>{shortRef(link.execution_id)}</strong><small>{link.source_link_ref}</small></span>
                    <time>{formatDateTime(link.linked_at)}</time>
                  </button>
                ))}
              </div>
            )}
          </section>
        </>
      ) : null}
    </div>
  );
}

export function ComparePage({ navigate }: PageProps) {
  useLocale();
  const [left, setLeft] = useState("");
  const [right, setRight] = useState("");
  const [leftAsOf, setLeftAsOf] = useState("");
  const [rightAsOf, setRightAsOf] = useState("");
  const [result, setResult] = useState<ExecutionComparison>();
  const [error, setError] = useState<unknown>();
  const [loading, setLoading] = useState(false);

  async function compare(event: FormEvent) {
    event.preventDefault();
    if (!left || !right || !leftAsOf || !rightAsOf) return;
    setLoading(true);
    setError(undefined);
    try {
      setResult(await investigationAPI.compare(left, leftAsOf, right, rightAsOf));
    } catch (caught) {
      setError(caught);
    } finally {
      setLoading(false);
    }
  }
  return (
    <div className="page-stack">
      <PageHeader
        eyebrow="Deterministic compare"
        title={t("m0160")}
        description={t("m1580")}
      />
      <section className="panel">
        <form className="compare-form" onSubmit={(event) => void compare(event)}>
          <ExecutionSelector label="Left execution" id={left} asOf={leftAsOf} onID={setLeft} onAsOf={setLeftAsOf} />
          <span className="compare-divider" aria-hidden="true">↔</span>
          <ExecutionSelector label="Right execution" id={right} asOf={rightAsOf} onID={setRight} onAsOf={setRightAsOf} />
          <button className="button button-primary" disabled={loading}>{tx("m1581")}</button>
        </form>
      </section>
      {error ? <ErrorState error={error} /> : null}
      {result ? (
        <section className="panel">
          <PanelHeading eyebrow="Comparison result" title={t("m1582")} id="comparison-title" />
          <p className="snapshot-line">
            Left <code>{formatCanonicalTime(result.left_as_of)}</code>
            {" · "}
            Right <code>{formatCanonicalTime(result.right_as_of)}</code>
          </p>
          <div className="comparison-table-wrap">
            <table className="comparison-table">
              <caption>{tx("m1583")}</caption>
              <thead><tr><th>Dimension</th><th>Left</th><th>Relation</th><th>Right</th></tr></thead>
              <tbody>
                {result.dimensions.map((item) => (
                  <tr key={item.dimension}>
                    <th>{item.dimension}</th>
                    <td><ComparisonCell value={item.left} /></td>
                    <td><span className={`relation relation-${item.relation}`}>{item.relation}</span></td>
                    <td><ComparisonCell value={item.right} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="button-cluster">
            <button className="text-link" onClick={() => navigate(`/executions/${result.left.execution_id}?as_of=${encodeURIComponent(result.left_as_of)}`)}>{tx("m1584")}</button>
            <button className="text-link" onClick={() => navigate(`/executions/${result.right.execution_id}?as_of=${encodeURIComponent(result.right_as_of)}`)}>{tx("m1585")}</button>
          </div>
        </section>
      ) : null}
    </div>
  );
}

export function ConnectorsPage({ session, navigate }: PageProps) {
  useLocale();
  const [result, setResult] = useState<ConnectorListResponse>();
  const [error, setError] = useState<unknown>();
  const [invitation, setInvitation] = useState<ConnectorInvitation>();
  const [creating, setCreating] = useState(false);
  const canManage = session.session.permissions.includes("connector.installations.manage");
  const installations = result?.installations ?? [];

  const load = (signal?: AbortSignal) => investigationAPI.listConnectors(signal).then(setResult);
  useEffect(() => {
    const controller = new AbortController();
    load(controller.signal)
      .catch((caught) => {
        if (!isAbortError(caught) && !controller.signal.aborted) setError(caught);
      });
    return () => controller.abort();
  }, []);

  async function createInvitation() {
    setCreating(true);
    setError(undefined);
    try {
      const created = await workspaceAPI.createConnectorInvitation(session.csrf_token);
      setInvitation(created.invitation);
      await load();
    } catch (caught) {
      setError(caught);
    } finally {
      setCreating(false);
    }
  }
  return (
    <div className="page-stack">
      <PageHeader
        eyebrow="Trust and delivery"
        title={t("m1586")}
        description={t("m1587")}
      />
      {result?.installation_control?.enabled && canManage ? (
        <section className="panel connector-install-panel">
          <PanelHeading eyebrow="Install Connector" title={t("m1588")} id="connector-install-title" />
          <p>{tx("m1589")}</p>
<a className="button button-primary" href="/connectors/unirouter/setup">{tx("m1590")}</a>
<details><summary>{tx("m1591")}</summary>
          <button className="button button-primary" disabled={creating} onClick={() => void createInvitation()}>
            {creating ? t("m1592") : t("m1593")}
          </button>
          {invitation ? (
            <div className="connector-invitation" role="status">
              <strong>{tx("m1594")}</strong>
              <span>{tx("m1595", { value0: formatDateTime(invitation.expires_at) })}</span>
              <a className="button" href={invitation.install_url} target="_blank" rel="noreferrer">{tx("m0328")}</a>
            </div>
          ) : null}
          </details>
        </section>
      ) : null}
      {error ? <ErrorState error={error} /> : null}
      {!result ? <LoadingBlock label={t("m1596")} /> : null}
      {result?.items.length === 0 && installations.length === 0 ? (
        <EmptyState
          title={t("m1597")}
          description={t("m1598")}
        />
      ) : null}
      <div className="connector-detail-list">
        {installations.map((installation) => (
          <article className={`panel connector-detail connector-installation connector-installation-${installation.status}`} key={installation.installation_id}>
            <header>
              <div><span className={`trust-state trust-${installation.status === "abnormal" ? "degraded" : "inactive"}`}>{{pending:t("m1599"),connected:t("m1600"),paused:t("m1601"),disconnected:t("m1602"),abnormal:t("m0282")}[installation.status]}</span><h2>{tx("m1603")}</h2></div>
              <a href={`/connectors/unirouter/setup?installation=${encodeURIComponent(installation.installation_id)}`}>{tx("m1604")}</a>
            </header>
            <ConnectorSyncPanel installationID={installation.installation_id} csrfToken={session.csrf_token} canManage={canManage} />
            <p>{tx("m1606", { value0: installation.last_agent_seen_at ? formatDateTime(installation.last_agent_seen_at) : t("m1605") })}</p>
            <details><summary>{tx("m0339")}</summary><p>{tx("m0340", { value0: shortRef(installation.installation_id) })}</p><div className="connector-columns">
              <section className="connector-column">
                <h3>{tx("m1607")}</h3>
                <dl>
                  <Fact label="Desired" value={installation.desired_state} />
                  <Fact label="Observed" value={installation.observed_state} />
                  <Fact label="Project" value={installation.project_id || t("m1608")} />
                  <Fact label="Last agent sync" value={installation.last_agent_seen_at ? formatDateTime(installation.last_agent_seen_at) : t("m1609")} />
                </dl>
              </section>
              <section className="connector-column">
                <h3>{tx("m1610")}</h3>
                <dl>
                  <Fact label="Requested" value={installation.requested_event_scope.join(", ")} />
                  <Fact label="Final" value={installation.final_event_scope?.join(", ") || t("m1599")} />
                  <Fact label="Reason" value={installation.status_reason_code || "—"} />
                </dl>
              </section>
            </div>
            <p className="boundary-note">{tx("m1611")}</p></details>
          </article>
        ))}
      </div>
      <div className="connector-detail-list">
        {result?.items.map((item) => (
          <article className="panel connector-detail" key={item.connector_key}>
            <header>
              <div><span className={`trust-state trust-${item.trust_state}`}>{item.trust_state}</span><h2>{item.source_display_name}</h2></div>
              <code>{item.adapter_id}@{item.adapter_version}</code>
            </header>
            <div className="connector-columns">
              <section className="connector-column">
                <h3>{tx("m1612")}</h3>
                <dl>
                  <Fact label="Source" value={`${item.source_type} · ${item.source_status}`} />
                  <Fact label="Manifest" value={item.manifest_active ? "active" : "inactive"} />
                  <Fact label="Fidelity" value={item.fidelity_level} />
                  <Fact label="Payload" value={item.payload_behavior} />
                  <Fact label="Idempotency" value={item.idempotency_strategy} />
                </dl>
              </section>
              <section className="connector-column">
                <h3>{tx("m1613")}</h3>
                <dl>
                  <Fact label="Worker" value={item.checkpoint.worker_status} />
                  <Fact label="Import" value={item.checkpoint.import_status} />
                  <Fact label="Checkpoint epoch" value={String(item.checkpoint.epoch)} />
                  <Fact label="Pages" value={String(item.checkpoint.pages_committed)} />
                  <Fact label="Committed" value={String(item.checkpoint.items_committed)} />
                  <Fact label="Duplicate no-op" value={String(item.checkpoint.duplicate_noop)} />
                </dl>
              </section>
              <section className="connector-column digest-list">
                <h3>{tx("m1614")}</h3>
                <dl>
                  <Fact label="Cursor" value={item.checkpoint.cursor_present ? item.checkpoint.cursor_digest || "digest unavailable" : "absent"} />
                  <Fact label="High-watermark" value={item.checkpoint.high_watermark_present ? item.checkpoint.high_watermark_digest || "digest unavailable" : "absent"} />
                </dl>
              </section>
            </div>
            <p className="boundary-note">{item.delivery_state_boundary}</p>
            <button className="button" onClick={() => navigate(`/executions?source_system_id=${encodeURIComponent(item.source_system_id)}`)}>{tx("m1615")}</button>
          </article>
        ))}
      </div>
    </div>
  );
}

export function DiscoveryPage({ session, navigate }: PageProps) {
  useLocale();
  const [hypotheses, setHypotheses] = useState<DiscoveryHypothesis[]>([]);
  const [alerts, setAlerts] = useState<InvestigationAlert[]>([]);
  const [cases, setCases] = useState<InvestigationCase[]>([]);
  const [asOf, setAsOf] = useState("");
  const [error, setError] = useState<unknown>();
  const [loading, setLoading] = useState(true);
  const [alertTargets, setAlertTargets] = useState<Record<string, string>>({});
  useEffect(() => {
    const controller = new AbortController();
    async function load() {
      try {
        const hypothesisResult = await investigationAPI.listHypotheses(undefined, controller.signal);
        const [alertResult, caseResult] = await Promise.all([
          investigationAPI.listAlerts(hypothesisResult.as_of, controller.signal),
          workspaceAPI.listCases(controller.signal),
        ]);
        if (controller.signal.aborted) return;
        setHypotheses(hypothesisResult.items);
        setAlerts(alertResult.items);
        setCases(caseResult.items.filter((item) => item.status === "open"));
        setAsOf(hypothesisResult.as_of);
      } catch (caught) {
        if (!controller.signal.aborted) setError(caught);
      } finally {
        if (!controller.signal.aborted) setLoading(false);
      }
    }
    void load();
    return () => controller.abort();
  }, []);

  async function link(alertID: string, caseID: string) {
    if (!caseID) return;
    try {
      const linked = await workspaceAPI.linkAlertToCase(alertID, caseID, session.csrf_token);
      setAlerts((current) => current.map((item) => item.id === alertID ? { ...item, case_id: linked.case_id, state: linked.alert_state } : item));
      setAlertTargets((current) => ({ ...current, [alertID]: "" }));
    } catch (caught) {
      setError(caught);
    }
  }

  return (
    <div className="page-stack">
      <PageHeader
        eyebrow="Known / Unknown discovery"
        title={t("m1616")}
        description={t("m1617")}
      />
      {asOf ? <p className="snapshot-line">Fixed snapshot <code>{formatCanonicalTime(asOf)}</code></p> : null}
      {error ? <ErrorState error={error} /> : null}
      {loading ? <LoadingBlock label={t("m1618")} /> : null}
      {!loading ? <div className="discovery-grid">
        <section className="panel">
          <PanelHeading eyebrow="Hypotheses" title={t("m1619")} id="hypotheses-title" />
          <div className="hypothesis-list">
            {hypotheses.map((item) => (
              <article className={`hypothesis hypothesis-${item.status}`} key={item.id}>
                <header>
                  <span className="evidence-kind kind-derived">{item.hypothesis_type}</span>
                  <span className="hypothesis-state">{item.status}</span>
                </header>
                <h3>{item.title}</h3>
                <p>{item.statement}</p>
                {item.status === "abstained" ? (
                  <div className="abstention"><strong>Abstained</strong><p>{item.abstention_reason}</p></div>
                ) : (
                  <p className="confidence">Evidence support score <strong>{item.support_score_decimal}</strong> · {item.evidence_event_ids.length} Evidence refs</p>
                )}
                <details>
                  <summary>{tx("m1620")}</summary>
                  <ul>{item.limitations.map((limitation) => <li key={limitation}>{limitation}</li>)}</ul>
                  <dl className="method-facts">
                    <Fact label="Evidence snapshot" value={formatCanonicalTime(item.analysis_as_of)} />
                    <Fact label="Sample" value={String(item.sample_size)} />
                    <Fact label="Method" value={item.method_version} />
                    <Fact label="Input digest" value={item.input_digest} />
                  </dl>
                </details>
              </article>
            ))}
            {hypotheses.length === 0 ? <EmptyState title={t("m1621")} description={t("m1622")} /> : null}
          </div>
        </section>
        <section className="panel">
          <PanelHeading eyebrow="Alerts" title={t("m1623")} id="alerts-title" />
          <div className="alert-list">
            {alerts.map((item) => (
              <article key={item.id} className={`alert-item alert-${item.severity}`}>
                <header><strong>{item.title}</strong><span>{item.severity}</span></header>
                <p>{item.state} · {formatDateTime(item.observed_at)}</p>
                {item.case_id ? (
                  <button className="text-link" onClick={() => navigate(`/cases/${item.case_id}`)}>{tx("m1624")}</button>
                ) : (
                  <div className="alert-case-link">
                    <label>{t("m1625")}<select
                        value={alertTargets[item.id] ?? ""}
                        onChange={(event) => setAlertTargets((current) => ({ ...current, [item.id]: event.target.value }))}
                      >
                        <option value="">{tx("m1626")}</option>
                        {cases.map((caseItem) => <option key={caseItem.id} value={caseItem.id}>{caseItem.title}</option>)}
                      </select>
                    </label>
                    <button
                      className="button button-secondary"
                      disabled={!alertTargets[item.id]}
                      onClick={() => void link(item.id, alertTargets[item.id] ?? "")}
                    >{tx("m1627")}</button>
                  </div>
                )}
              </article>
            ))}
            {alerts.length === 0 ? <p className="quiet-copy">{tx("m1628")}</p> : null}
          </div>
        </section>
      </div> : null}
    </div>
  );
}

export function ServiceIdentitiesPage({ session }: PageProps) {
  useLocale();
  const [items, setItems] = useState<ServiceIdentity[]>([]);
  const [error, setError] = useState<unknown>();
  const [loading, setLoading] = useState(true);
  const [pendingRevoke, setPendingRevoke] = useState("");
  const [revoking, setRevoking] = useState("");
  const canRead = session.session.permissions.includes("service_identities.read");
  const canRevoke = session.session.permissions.includes("service_identities.revoke");
  useEffect(() => {
    if (!canRead) {
      setLoading(false);
      return;
    }
    const controller = new AbortController();
    workspaceAPI.listServiceIdentities(controller.signal)
      .then((response) => setItems(response.items))
      .catch((caught) => {
        if (!controller.signal.aborted) setError(caught);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [canRead]);

  async function revoke(id: string) {
    setRevoking(id);
    setError(undefined);
    try {
      const updated = await workspaceAPI.revokeServiceIdentity(id, "revoked_by_platform_admin", session.csrf_token);
      setItems((current) => current.map((item) => item.id === id ? updated : item));
      setPendingRevoke("");
    } catch (caught) {
      setError(caught);
    } finally {
      setRevoking("");
    }
  }
  return (
    <div className="page-stack">
      <PageHeader
        eyebrow="Human / Agent same truth"
        title={t("m0142")}
        description={t("m1629")}
      />
      {!canRead ? <EmptyState title={t("m1630")} description={t("m1631")} /> : null}
      {loading ? <LoadingBlock label={t("m1632")} /> : null}
      {error ? <ErrorState error={error} /> : null}
      {canRead && !loading && !error && items.length === 0 ? (
        <EmptyState
          title={t("m1633")}
          description={t("m1634")}
        />
      ) : null}
      <div className="identity-list">
        {items.map((item) => (
          <article className="panel identity-card" key={item.id}>
            <header><div><span className={`identity-status status-${item.status}`}>{item.status}</span><h2>{item.display_name}</h2></div><code>{item.id}</code></header>
            <dl>
              <Fact label="Expires" value={item.expires_at ? formatCanonicalTime(item.expires_at) : "not set"} />
              <Fact label="Last used" value={item.last_used_at ? formatCanonicalTime(item.last_used_at) : "never"} />
              <Fact label="Permissions" value={item.permissions.join(", ")} />
            </dl>
            {canRevoke && item.status === "active" ? (
              pendingRevoke === item.id ? (
                <div className="destructive-confirm" role="alert">
                  <p>{tx("m1635")}</p>
                  <div className="button-cluster">
                    <button className="button button-secondary" disabled={revoking === item.id} onClick={() => setPendingRevoke("")}>{tx("m1516")}</button>
                    <button className="button button-danger" disabled={revoking === item.id} onClick={() => void revoke(item.id)}>
                      {revoking === item.id ? t("m0035") : t("m1636")}
                    </button>
                  </div>
                </div>
              ) : (
                <button className="button button-danger" onClick={() => setPendingRevoke(item.id)}>{tx("m1637")}</button>
              )
            ) : null}
          </article>
        ))}
      </div>
    </div>
  );
}

function PageHeader({
  eyebrow,
  title,
  description,
  action,
}: {
  eyebrow: string;
  title: string;
  description: string;
  action?: ReactNode;
}) {
  useLocale();
  return (
    <header className="page-header">
      <div><span className="eyebrow">{eyebrow}</span><h1>{title}</h1><p>{description}</p></div>
      {action ? <div className="page-header-action">{action}</div> : null}
    </header>
  );
}

function PanelHeading({
  eyebrow,
  title,
  id,
  description,
}: {
  eyebrow: string;
  title: string;
  id: string;
  description?: string;
}) {
  useLocale();
  return (
    <div className="panel-heading">
      <div><span className="eyebrow">{eyebrow}</span><h2 id={id}>{title}</h2></div>
      {description ? <p>{description}</p> : null}
    </div>
  );
}

function Fact({ label, value }: { label: string; value: string }) {
  useLocale();
  return <div><dt>{label}</dt><dd>{value.startsWith("sha256:") ? <code>{value}</code> : value}</dd></div>;
}

function TechnicalReference({ label, value }: { label: string; value: string }) {
  useLocale();
  return (
    <details className="technical-reference">
      <summary><span>{label}</span><code>{shortRef(value, 12, 10)}</code></summary>
      <code>{value}</code>
    </details>
  );
}

function ExecutionSelector({
  label,
  id,
  asOf,
  onID,
  onAsOf,
}: {
  label: string;
  id: string;
  asOf: string;
  onID: (value: string) => void;
  onAsOf: (value: string) => void;
}) {
  useLocale();
  return (
    <fieldset>
      <legend>{label}</legend>
      <label>Execution ID<input value={id} onChange={(event) => onID(event.target.value)} placeholder="xe1_…" /></label>
      <label>as_of (UTC RFC3339)<input value={asOf} onChange={(event) => onAsOf(event.target.value)} placeholder="2026-07-31T02:00:00Z" /></label>
    </fieldset>
  );
}

function ComparisonCell({ value }: { value: { state: string; value?: string; unit?: string } }) {
  useLocale();
  if (value.state === "missing") return <span className="missing-value">Missing</span>;
  return <>
    <strong>{value.value}</strong>
    {value.unit ? <small>{value.unit}</small> : null}
    {value.state === "partial" ? <span className="missing-value">Partial</span> : null}
  </>;
}

function FindingClassificationBadge({ classification }: { classification: string }) {
  useLocale();
  const presentation = {
    evidence_backed: { className: "kind-direct", label: t("m1638") },
    deterministic_derived: { className: "kind-derived", label: t("m1639") },
    hypothesis: { className: "kind-hypothesis", label: t("m1640") },
    unknown: { className: "kind-gap", label: t("m1641") },
  }[classification] ?? { className: "kind-gap", label: t("m1642") };
  return <span className={`evidence-kind ${presentation.className}`}>{presentation.label}</span>;
}
