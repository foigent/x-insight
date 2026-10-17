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
import { FindingLifecycle } from "./FindingLifecycle";
import { newFindingReferences, ReviewStart } from "./ReviewStart";

interface PageProps {
  session: AccessSessionResponse;
  navigate: (path: string) => void;
}

export function CasesPage({ session, navigate }: PageProps) {
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
        eyebrow="Evidence workspace"
        title="调查案件"
        description="Case 是可写调查记录；它引用 M0 Evidence，却不能修改 M0。封存后形成不可变 revision digest。"
      />
      <div className="split-layout">
        {canWrite ? <section className="panel" aria-labelledby="new-case-title">
          <PanelHeading eyebrow="New case" title="建立调查范围" id="new-case-title" />
          <form className="stack-form" onSubmit={(event) => void create(event)}>
            <label>标题<input value={title} maxLength={160} onChange={(event) => setTitle(event.target.value)} /></label>
            <label>说明<textarea value={description} maxLength={4000} rows={5} onChange={(event) => setDescription(event.target.value)} /></label>
            <button className="button button-primary" disabled={saving || !title.trim()}>{saving ? "正在创建…" : "创建 Case"}</button>
          </form>
          <ReviewStart session={session} available={{ evidence: [], gaps: [] }} navigate={navigate} />
        </section> : null}
        <section className="panel panel-grow" aria-labelledby="case-list-title">
          <PanelHeading eyebrow="Workspace records" title="最近 Cases" id="case-list-title" />
          {loading ? <LoadingBlock label="正在读取 Cases" /> : null}
          {error ? <ErrorState error={error} /> : null}
          {!loading && items.length === 0 ? <EmptyState title="尚无 Case" description={canWrite ? "可以从所选依据开始复核，或先建立调查范围。" : "当前可见列表没有调查记录。"} /> : null}
          <div className="case-card-grid">
            {items.map((item) => (
              <button className="case-card" key={item.id} onClick={() => navigate(`/cases/${item.id}`)}>
                <span className={`case-status case-${item.status}`}>{item.status}</span>
                <strong>{item.title}</strong>
                <p>{item.description || "无补充说明"}</p>
                <footer><code>{shortRef(item.id)}</code><time>{formatDateTime(item.updated_at)}</time></footer>
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
      <button className="back-link" onClick={() => { const target = new URLSearchParams(window.location.search).get("queue_return"); navigate(target === "/investigation/queue" || target?.startsWith("/investigation/queue?") ? target : "/cases"); }}>← 返回{new URLSearchParams(window.location.search).has("queue_return") ? "调查事项" : "调查案件"}</button>
      {loading && !record ? <LoadingBlock label="正在读取调查案件" /> : null}
      {error ? <ErrorState error={error} /> : null}
      {record ? (
        <>
          <PageHeader
            eyebrow={`调查案件 · ${record.case.status === "open" ? "进行中" : record.case.status === "sealed" ? "已封存" : record.case.status === "archived" ? "已归档" : "状态待确认"}`}
            title={record.case.title}
            description={record.case.description || "该案件没有补充说明。"}
            action={
              record.case.status === "open" && canExport ? (
                sealArmed ? (
                  <div className="button-cluster">
                    <button className="button button-secondary" disabled={saving} onClick={() => setSealArmed(false)}>取消</button>
                    <button className="button button-danger" disabled={saving} onClick={() => void seal()}>
                      确认封存版本 {record.case.version + 1}
                    </button>
                  </div>
                ) : (
                  <button className="button button-primary" disabled={saving} onClick={() => setSealArmed(true)}>准备封存</button>
                )
              ) : record.case.status === "sealed" && canExport ? (
                <div className="button-cluster">
                  <a className="button button-secondary" href={`/api/v1/cases/${caseID}/evidence-pack.json`}>下载 JSON</a>
                  <a className="button button-primary" href={`/api/v1/cases/${caseID}/evidence-pack.html`}>下载 HTML</a>
                </div>
              ) : null
            }
          />
          <details className="technical-reference"><summary>案件状态代码</summary><code>{record.case.status}</code></details>
          {record.case.sealed_digest ? (
            <div className="sealed-banner"><strong>已封存版本 {record.case.sealed_revision}</strong><code>{record.case.sealed_digest}</code></div>
          ) : null}
          {record.case.status === "open" ? (
            <div className="content-boundary-warning" role="note">
              <strong>封存内容与敏感信息</strong>
              <p>标题、说明、调查发现与笔记会原样进入封存包。请勿粘贴原始提示词、模型回复、访问凭据或其他受限来源内容。</p>
              {sealArmed ? <p><strong>封存后当前版本不可继续编辑。</strong>请先核对调查发现、证据与缺口。</p> : null}
            </div>
          ) : null}
          <div className="case-dossier">
            <section className="panel dossier-main">
              <PanelHeading eyebrow="调查陈述" title={`发现 ${record.findings.length}`} id="case-findings-title" />
              <div className="dossier-list">
                {record.findings.map((finding) => (
                  <article key={finding.id} id={finding.id} tabIndex={-1}>
                    <FindingClassificationBadge classification={finding.classification} />
                    <h3>{finding.title}</h3>
                    <p>{finding.statement}</p>
                    <footer><time>{formatDateTime(finding.created_at)}</time></footer>
                    <details><summary>发现标识与分类代码</summary><code>{finding.id}</code><br /><code>{finding.classification}</code></details>
                    <FindingLifecycle record={record} findingID={finding.id} session={session} onChanged={() => void reload()} navigate={navigate} />
                  </article>
                ))}
                {record.findings.length === 0 ? <p className="quiet-copy">尚无调查发现。</p> : null}
              </div>
              {record.case.status === "open" && canWrite ? <ReviewStart session={session} available={newFindingReferences(record)} defaultCase={record.case} navigate={navigate} /> : null}
              {record.case.status === "open" && canWrite ? (
                <details className="inline-editor disclosure-editor">
                  <summary>添加调查发现</summary>
                  <form className="stack-form" onSubmit={(event) => void addFinding(event)}>
                    <label>标题<input value={findingTitle} maxLength={160} onChange={(event) => setFindingTitle(event.target.value)} /></label>
                    <label>陈述<textarea value={findingStatement} maxLength={8000} rows={4} onChange={(event) => setFindingStatement(event.target.value)} /></label>
                    <label>分类
                      <select
                        value={findingClassification}
                        onChange={(event) => setFindingClassification(event.target.value as "hypothesis" | "unknown")}
                      >
                        <option value="hypothesis">待验证假设</option>
                        <option value="unknown">尚无法分类</option>
                      </select>
                    </label>
                    <p className="quiet-copy">手工陈述不会自动成为有证据支持的判断或按确定规则推导的结论。</p>
                    <button className="button button-secondary" disabled={saving}>添加调查发现</button>
                  </form>
                </details>
              ) : null}
            </section>
            <aside className="dossier-aside">
              <section className="panel">
                <PanelHeading eyebrow="调查范围" title="关联执行" id="case-executions-title" />
                <div className="compact-list">
                  {record.executions.map((link, index) => (
                    <Fragment key={`${link.execution_id}:${link.as_of}`}><button onClick={() => navigate(`/executions/${link.execution_id}?as_of=${encodeURIComponent(link.as_of)}`)}>
                      <span><strong>查看执行 {index + 1}</strong><small>资料截止：{formatDateTime(link.as_of)}</small></span>
                    </button><TechnicalReference label="执行标识" value={link.execution_id} /></Fragment>
                  ))}
                  {record.executions.length === 0 ? <p className="quiet-copy">{record.evidence.length+record.gaps.length+(record.resources?.length??0)+(record.contents?.length??0)>0?"已保留复核引用，尚未关联执行。":"尚未关联执行。"}</p> : null}
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
                <PanelHeading eyebrow="引用与限制" title={`${record.evidence.length} 条证据 · ${record.gaps.length} 条缺口 · ${record.resources?.length??0} 条资源 · ${record.contents?.length??0} 条内容`} id="case-citations-title" />
                <div className="citation-counts">
                  <strong>{record.evidence.length}<small>保留原始证据引用</small></strong>
                  <strong>{record.gaps.length}<small>明确记录的证据缺口</small></strong>
                  <strong>{record.resources?.length??0}<small>保留资源引用</small></strong>
                  <strong>{record.contents?.length??0}<small>保留内容引用</small></strong>
                </div>
                <div className="case-citation-list">
                  {record.evidence.map((citation) => (
                    <article key={`${citation.finding_id ?? "case"}:${citation.evidence_event_id}:${citation.relation}`}>
                      <span className="evidence-kind kind-direct">证据 · {citation.relation === "supports" ? "支持" : citation.relation === "contradicts" ? "反证" : citation.relation === "context" ? "背景" : "关系待确认"}</span>
                      <small>{citation.finding_id ? `调查发现 ${shortRef(citation.finding_id)}` : "案件级引用"}</small>
                      <code title={citation.evidence_event_id}>{shortRef(citation.evidence_event_id)}</code>
                      <TechnicalReference label="引用关系代码" value={citation.relation} /><TechnicalReference label="证据校验值" value={citation.event_hash} />
                      <small title={citation.source_ref}>{citation.source_ref}</small>
                    </article>
                  ))}
                  {record.gaps.map((gap) => (
                    <article className="case-gap-citation" key={`${gap.finding_id ?? "case"}:${gap.gap_id}:${gap.relation}`}>
                      <span className="evidence-kind kind-gap">证据缺口 · {gap.relation === "limits" ? "限制" : gap.relation === "blocks" ? "阻碍判断" : "背景"}</span>
                      <small>{gap.finding_id ? `调查发现 ${shortRef(gap.finding_id)}` : "案件级限制"}</small>
                      <strong>已记录的证据缺口</strong><TechnicalReference label="缺口类型代码" value={gap.gap_type} /><TechnicalReference label="缺口关系代码" value={gap.relation} />
                      <p>{gap.affected_claim}</p>
                      <code title={gap.gap_id}>{shortRef(gap.gap_id)}</code>
                    </article>
                  ))}
                  {record.evidence.length === 0 && record.gaps.length === 0 && !(record.resources?.length) && !(record.contents?.length) ? (
                    <p className="quiet-copy">尚未链接证据、缺口、资源或内容；零条引用不证明资料完整。</p>
                  ) : null}
                </div>
              </section>
              <section className="panel">
                <PanelHeading eyebrow="人工记录" title="调查笔记" id="case-notes-title" />
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
                    <label>新笔记<textarea value={note} maxLength={4000} rows={3} onChange={(event) => setNote(event.target.value)} /></label>
                    <button className="button button-secondary" disabled={saving || !note.trim()}>保存笔记</button>
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
        title="来源声明会话"
        description="只展示来源明确声明的 Session 或 Thread。时间接近不会自动合并执行。"
      />
      {error ? <ErrorState error={error} /> : null}
      {!result ? <LoadingBlock label="正在读取 authoritative sessions" /> : null}
      {result?.items.length === 0 ? <EmptyState title="没有 authoritative Session" description="当前来源未声明 Session identity；X-Insight 不会按时间猜测。" /> : null}
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
      <button className="back-link" onClick={() => navigate("/sessions")}>← 返回 Sessions</button>
      {error ? <ErrorState error={error} /> : null}
      {!detail ? <LoadingBlock label="正在读取 Session" /> : null}
      {detail ? (
        <>
          <PageHeader
            eyebrow="Authoritative Session"
            title={detail.session.source_session_ref}
            description={`${detail.session.source_display_name} 明确声明的 ${detail.session.source_session_type}。`}
          />
          <section className="panel">
            <PanelHeading eyebrow="Execution membership" title="来源声明的成员" id="session-members-title" />
            {detail.executions.length === 0 ? (
              <EmptyState
                title="该 Session 没有声明 execution 成员"
                description="X-Insight 不会根据时间接近自动补建 membership。"
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
        title="执行对比"
        description="同一维度并排比较；缺失值保持 Missing，未证明完整的可见路径标为 Partial，绝不替换为 0。"
      />
      <section className="panel">
        <form className="compare-form" onSubmit={(event) => void compare(event)}>
          <ExecutionSelector label="Left execution" id={left} asOf={leftAsOf} onID={setLeft} onAsOf={setLeftAsOf} />
          <span className="compare-divider" aria-hidden="true">↔</span>
          <ExecutionSelector label="Right execution" id={right} asOf={rightAsOf} onID={setRight} onAsOf={setRightAsOf} />
          <button className="button button-primary" disabled={loading}>比较固定快照</button>
        </form>
      </section>
      {error ? <ErrorState error={error} /> : null}
      {result ? (
        <section className="panel">
          <PanelHeading eyebrow="Comparison result" title="可验证差异" id="comparison-title" />
          <p className="snapshot-line">
            Left <code>{formatCanonicalTime(result.left_as_of)}</code>
            {" · "}
            Right <code>{formatCanonicalTime(result.right_as_of)}</code>
          </p>
          <div className="comparison-table-wrap">
            <table className="comparison-table">
              <caption>Left 与 Right 的同维度值</caption>
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
            <button className="text-link" onClick={() => navigate(`/executions/${result.left.execution_id}?as_of=${encodeURIComponent(result.left_as_of)}`)}>打开 Left</button>
            <button className="text-link" onClick={() => navigate(`/executions/${result.right.execution_id}?as_of=${encodeURIComponent(result.right_as_of)}`)}>打开 Right</button>
          </div>
        </section>
      ) : null}
    </div>
  );
}

export function ConnectorsPage({ session, navigate }: PageProps) {
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
        title="连接器信任状态"
        description="查看连接配置、实际同步和来源覆盖。配置完成后，尚未发生的事件类别仍可能没有数据。"
      />
      {result?.installation_control?.enabled && canManage ? (
        <section className="panel connector-install-panel">
          <PanelHeading eyebrow="Install Connector" title="连接 UniRouter" id="connector-install-title" />
          <p>先确认 XI 正文保管设置，再由 UniRouter 管理员确认供数范围及策略管理权限。</p>
<a className="button button-primary" href="/connectors/unirouter/setup">开始连接设置</a>
<details><summary>兼容安装邀请</summary>
          <button className="button button-primary" disabled={creating} onClick={() => void createInvitation()}>
            {creating ? "正在创建邀请…" : "创建安装邀请"}
          </button>
          {invitation ? (
            <div className="connector-invitation" role="status">
              <strong>邀请已就绪</strong>
              <span>有效期至 {formatDateTime(invitation.expires_at)}</span>
              <a className="button" href={invitation.install_url} target="_blank" rel="noreferrer">在 UniRouter 中继续</a>
            </div>
          ) : null}
          </details>
        </section>
      ) : null}
      {error ? <ErrorState error={error} /> : null}
      {!result ? <LoadingBlock label="正在读取 Connector trust" /> : null}
      {result?.items.length === 0 && installations.length === 0 ? (
        <EmptyState
          title="没有已登记的 Connector"
          description="尚未安装、读取失败与健康状态保持分离；空列表不会被显示为正常连接。"
        />
      ) : null}
      <div className="connector-detail-list">
        {installations.map((installation) => (
          <article className={`panel connector-detail connector-installation connector-installation-${installation.status}`} key={installation.installation_id}>
            <header>
              <div><span className={`trust-state trust-${installation.status === "abnormal" ? "degraded" : "inactive"}`}>{{pending:"等待确认",connected:"连接已配置",paused:"已暂停",disconnected:"已断开",abnormal:"需要处理"}[installation.status]}</span><h2>UniRouter 连接</h2></div>
              <a href={`/connectors/unirouter/setup?installation=${encodeURIComponent(installation.installation_id)}`}>查看设置进度</a>
            </header>
            <p>最近连接回报：{installation.last_agent_seen_at ? formatDateTime(installation.last_agent_seen_at) : "尚未收到"}。连接回报与实际数据接收分别验证。</p>
            <details><summary>诊断详情</summary><p>安装记录：{shortRef(installation.installation_id)}</p><div className="connector-columns">
              <section className="connector-column">
                <h3>连接状态</h3>
                <dl>
                  <Fact label="Desired" value={installation.desired_state} />
                  <Fact label="Observed" value={installation.observed_state} />
                  <Fact label="Project" value={installation.project_id || "等待 UniRouter 用户确认"} />
                  <Fact label="Last agent sync" value={installation.last_agent_seen_at ? formatDateTime(installation.last_agent_seen_at) : "尚未连接"} />
                </dl>
              </section>
              <section className="connector-column">
                <h3>固定范围</h3>
                <dl>
                  <Fact label="Requested" value={installation.requested_event_scope.join(", ")} />
                  <Fact label="Final" value={installation.final_event_scope?.join(", ") || "等待确认"} />
                  <Fact label="Reason" value={installation.status_reason_code || "—"} />
                </dl>
              </section>
            </div>
            <p className="boundary-note">积压状态来自 UniRouter 的明确回报。</p></details>
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
                <h3>来源契约</h3>
                <dl>
                  <Fact label="Source" value={`${item.source_type} · ${item.source_status}`} />
                  <Fact label="Manifest" value={item.manifest_active ? "active" : "inactive"} />
                  <Fact label="Fidelity" value={item.fidelity_level} />
                  <Fact label="Payload" value={item.payload_behavior} />
                  <Fact label="Idempotency" value={item.idempotency_strategy} />
                </dl>
              </section>
              <section className="connector-column">
                <h3>交付运行状态</h3>
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
                <h3>Checkpoint 摘要</h3>
                <dl>
                  <Fact label="Cursor" value={item.checkpoint.cursor_present ? item.checkpoint.cursor_digest || "digest unavailable" : "absent"} />
                  <Fact label="High-watermark" value={item.checkpoint.high_watermark_present ? item.checkpoint.high_watermark_digest || "digest unavailable" : "absent"} />
                </dl>
              </section>
            </div>
            <p className="boundary-note">{item.delivery_state_boundary}</p>
            <button className="button" onClick={() => navigate(`/executions?source_system_id=${encodeURIComponent(item.source_system_id)}`)}>查看该来源的执行</button>
          </article>
        ))}
      </div>
    </div>
  );
}

export function DiscoveryPage({ session, navigate }: PageProps) {
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
        title="可审查的假设，不是系统事实"
        description="覆盖、流程、主题、异常、联盟与权限漂移都必须带 Evidence、限制和 abstention；不会自动改路由或关闭 Gap。"
      />
      {asOf ? <p className="snapshot-line">Fixed snapshot <code>{formatCanonicalTime(asOf)}</code></p> : null}
      {error ? <ErrorState error={error} /> : null}
      {loading ? <LoadingBlock label="正在读取同一 Discovery 快照" /> : null}
      {!loading ? <div className="discovery-grid">
        <section className="panel">
          <PanelHeading eyebrow="Hypotheses" title="候选与主动弃权" id="hypotheses-title" />
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
                  <summary>限制与方法</summary>
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
            {hypotheses.length === 0 ? <EmptyState title="没有 Discovery 输出" description="没有足够 Evidence 时，系统应保持沉默，而不是生成故事。" /> : null}
          </div>
        </section>
        <section className="panel">
          <PanelHeading eyebrow="Alerts" title="转入 Case" id="alerts-title" />
          <div className="alert-list">
            {alerts.map((item) => (
              <article key={item.id} className={`alert-item alert-${item.severity}`}>
                <header><strong>{item.title}</strong><span>{item.severity}</span></header>
                <p>{item.state} · {formatDateTime(item.observed_at)}</p>
                {item.case_id ? (
                  <button className="text-link" onClick={() => navigate(`/cases/${item.case_id}`)}>打开 Case →</button>
                ) : (
                  <div className="alert-case-link">
                    <label>目标 Case
                      <select
                        value={alertTargets[item.id] ?? ""}
                        onChange={(event) => setAlertTargets((current) => ({ ...current, [item.id]: event.target.value }))}
                      >
                        <option value="">选择 open Case</option>
                        {cases.map((caseItem) => <option key={caseItem.id} value={caseItem.id}>{caseItem.title}</option>)}
                      </select>
                    </label>
                    <button
                      className="button button-secondary"
                      disabled={!alertTargets[item.id]}
                      onClick={() => void link(item.id, alertTargets[item.id] ?? "")}
                    >
                      确认关联
                    </button>
                  </div>
                )}
              </article>
            ))}
            {alerts.length === 0 ? <p className="quiet-copy">当前快照没有 Alert。</p> : null}
          </div>
        </section>
      </div> : null}
    </div>
  );
}

export function ServiceIdentitiesPage({ session }: PageProps) {
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
        title="Agent 身份"
        description="Agent 使用独立的有期限 Bearer credential，只能读取同一 REST Evidence。Token 只在 root CLI 创建时显示一次。"
      />
      {!canRead ? <EmptyState title="没有 service identity 管理权限" description="只有 platform_admin 可列出或撤销；创建仍由 root CLI 执行。" /> : null}
      {loading ? <LoadingBlock label="正在读取 Service identities" /> : null}
      {error ? <ErrorState error={error} /> : null}
      {canRead && !loading && !error && items.length === 0 ? (
        <EmptyState
          title="没有 Service identity"
          description="尚未由 root CLI 创建有期限的只读 Agent credential。"
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
                  <p>撤销后该 credential 立即失效。此操作不会删除历史 Evidence。</p>
                  <div className="button-cluster">
                    <button className="button button-secondary" disabled={revoking === item.id} onClick={() => setPendingRevoke("")}>取消</button>
                    <button className="button button-danger" disabled={revoking === item.id} onClick={() => void revoke(item.id)}>
                      {revoking === item.id ? "正在撤销…" : "确认撤销"}
                    </button>
                  </div>
                </div>
              ) : (
                <button className="button button-danger" onClick={() => setPendingRevoke(item.id)}>准备撤销</button>
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
  return (
    <div className="panel-heading">
      <div><span className="eyebrow">{eyebrow}</span><h2 id={id}>{title}</h2></div>
      {description ? <p>{description}</p> : null}
    </div>
  );
}

function Fact({ label, value }: { label: string; value: string }) {
  return <div><dt>{label}</dt><dd>{value.startsWith("sha256:") ? <code>{value}</code> : value}</dd></div>;
}

function TechnicalReference({ label, value }: { label: string; value: string }) {
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
  return (
    <fieldset>
      <legend>{label}</legend>
      <label>Execution ID<input value={id} onChange={(event) => onID(event.target.value)} placeholder="xe1_…" /></label>
      <label>as_of (UTC RFC3339)<input value={asOf} onChange={(event) => onAsOf(event.target.value)} placeholder="2026-07-31T02:00:00Z" /></label>
    </fieldset>
  );
}

function ComparisonCell({ value }: { value: { state: string; value?: string; unit?: string } }) {
  if (value.state === "missing") return <span className="missing-value">Missing</span>;
  return <>
    <strong>{value.value}</strong>
    {value.unit ? <small>{value.unit}</small> : null}
    {value.state === "partial" ? <span className="missing-value">Partial</span> : null}
  </>;
}

function FindingClassificationBadge({ classification }: { classification: string }) {
  const presentation = {
    evidence_backed: { className: "kind-direct", label: "原始分类：有证据支持的陈述" },
    deterministic_derived: { className: "kind-derived", label: "原始分类：按确定规则推导" },
    hypothesis: { className: "kind-hypothesis", label: "原始分类：假设" },
    unknown: { className: "kind-gap", label: "原始分类：尚无法分类" },
  }[classification] ?? { className: "kind-gap", label: "原始分类待确认" };
  return <span className={`evidence-kind ${presentation.className}`}>{presentation.label}</span>;
}
