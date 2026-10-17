import { FormEvent, type ReactNode, useEffect, useState } from "react";

import {
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

interface PageProps {
  session: AccessSessionResponse;
  navigate: (path: string) => void;
}

export function CasesPage({ session, navigate }: PageProps) {
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
        <section className="panel" aria-labelledby="new-case-title">
          <PanelHeading eyebrow="New case" title="建立调查范围" id="new-case-title" />
          <form className="stack-form" onSubmit={(event) => void create(event)}>
            <label>标题<input value={title} maxLength={160} onChange={(event) => setTitle(event.target.value)} /></label>
            <label>说明<textarea value={description} maxLength={4000} rows={5} onChange={(event) => setDescription(event.target.value)} /></label>
            <button className="button button-primary" disabled={saving || !title.trim()}>{saving ? "正在创建…" : "创建 Case"}</button>
          </form>
        </section>
        <section className="panel panel-grow" aria-labelledby="case-list-title">
          <PanelHeading eyebrow="Workspace records" title="全部 Cases" id="case-list-title" />
          {loading ? <LoadingBlock label="正在读取 Cases" /> : null}
          {error ? <ErrorState error={error} /> : null}
          {!loading && items.length === 0 ? <EmptyState title="尚无 Case" description="先创建 Case，再从 Execution Story 添加证据。" /> : null}
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
      <button className="back-link" onClick={() => navigate("/cases")}>← 返回 Cases</button>
      {loading && !record ? <LoadingBlock label="正在读取 Case" /> : null}
      {error ? <ErrorState error={error} /> : null}
      {record ? (
        <>
          <PageHeader
            eyebrow={`Case · ${record.case.status}`}
            title={record.case.title}
            description={record.case.description || "该 Case 没有补充说明。"}
            action={
              record.case.status === "open" ? (
                sealArmed ? (
                  <div className="button-cluster">
                    <button className="button button-secondary" disabled={saving} onClick={() => setSealArmed(false)}>取消</button>
                    <button className="button button-danger" disabled={saving} onClick={() => void seal()}>
                      确认封存 revision {record.case.version + 1}
                    </button>
                  </div>
                ) : (
                  <button className="button button-primary" disabled={saving} onClick={() => setSealArmed(true)}>准备封存</button>
                )
              ) : (
                <div className="button-cluster">
                  <a className="button button-secondary" href={`/api/v1/cases/${caseID}/evidence-pack.json`}>下载 JSON</a>
                  <a className="button button-primary" href={`/api/v1/cases/${caseID}/evidence-pack.html`}>下载 HTML</a>
                </div>
              )
            }
          />
          {record.case.sealed_digest ? (
            <div className="sealed-banner"><strong>Sealed revision {record.case.sealed_revision}</strong><code>{record.case.sealed_digest}</code></div>
          ) : null}
          {record.case.status === "open" ? (
            <div className="content-boundary-warning" role="note">
              <strong>Evidence Pack 内容边界</strong>
              <p>标题、说明、Finding 与调查笔记会原样进入封存包。不要粘贴 raw prompt、response、credential 或其他受限来源内容。</p>
              {sealArmed ? <p><strong>封存后当前 revision 不可继续编辑。</strong>请先核对 Findings、Evidence 和 Gaps。</p> : null}
            </div>
          ) : null}
          <div className="case-dossier">
            <section className="panel dossier-main">
              <PanelHeading eyebrow="Findings" title={`发现 ${record.findings.length}`} id="case-findings-title" />
              <div className="dossier-list">
                {record.findings.map((finding) => (
                  <article key={finding.id}>
                    <FindingClassificationBadge classification={finding.classification} />
                    <h3>{finding.title}</h3>
                    <p>{finding.statement}</p>
                    <footer><code>{finding.classification}</code><time>{formatDateTime(finding.created_at)}</time></footer>
                  </article>
                ))}
                {record.findings.length === 0 ? <p className="quiet-copy">尚无 Finding。</p> : null}
              </div>
              {record.case.status === "open" ? (
                <details className="inline-editor disclosure-editor">
                  <summary>添加 Finding</summary>
                  <form className="stack-form" onSubmit={(event) => void addFinding(event)}>
                    <label>标题<input value={findingTitle} maxLength={160} onChange={(event) => setFindingTitle(event.target.value)} /></label>
                    <label>陈述<textarea value={findingStatement} maxLength={8000} rows={4} onChange={(event) => setFindingStatement(event.target.value)} /></label>
                    <label>分类
                      <select
                        value={findingClassification}
                        onChange={(event) => setFindingClassification(event.target.value as "hypothesis" | "unknown")}
                      >
                        <option value="hypothesis">待验证假设 · hypothesis</option>
                        <option value="unknown">尚无法分类 · unknown</option>
                      </select>
                    </label>
                    <p className="quiet-copy">手工陈述不会被自动标为 Evidence-backed 或 deterministic。</p>
                    <button className="button button-secondary" disabled={saving}>添加 Finding</button>
                  </form>
                </details>
              ) : null}
            </section>
            <aside className="dossier-aside">
              <section className="panel">
                <PanelHeading eyebrow="Scope" title="Executions" id="case-executions-title" />
                <div className="compact-list">
                  {record.executions.map((link) => (
                    <button key={`${link.execution_id}:${link.as_of}`} onClick={() => navigate(`/executions/${link.execution_id}?as_of=${encodeURIComponent(link.as_of)}`)}>
                      <span><strong>{shortRef(link.execution_id)}</strong><small>{formatDateTime(link.as_of)}</small></span>
                    </button>
                  ))}
                  {record.executions.length === 0 ? <p className="quiet-copy">尚未链接 Execution。</p> : null}
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
                <PanelHeading eyebrow="Citations" title={`${record.evidence.length} Evidence · ${record.gaps.length} Gaps`} id="case-citations-title" />
                <div className="citation-counts">
                  <strong>{record.evidence.length}<small>immutable citations</small></strong>
                  <strong>{record.gaps.length}<small>explicit limitations</small></strong>
                </div>
                <div className="case-citation-list">
                  {record.evidence.map((citation) => (
                    <article key={`${citation.finding_id ?? "case"}:${citation.evidence_event_id}:${citation.relation}`}>
                      <span className="evidence-kind kind-direct">Evidence · {citation.relation}</span>
                      <small>{citation.finding_id ? `Finding ${shortRef(citation.finding_id)}` : "Case-level citation"}</small>
                      <code title={citation.evidence_event_id}>{shortRef(citation.evidence_event_id)}</code>
                      <TechnicalReference label="Event hash" value={citation.event_hash} />
                      <small title={citation.source_ref}>{citation.source_ref}</small>
                    </article>
                  ))}
                  {record.gaps.map((gap) => (
                    <article className="case-gap-citation" key={`${gap.finding_id ?? "case"}:${gap.gap_id}:${gap.relation}`}>
                      <span className="evidence-kind kind-gap">Gap · {gap.relation}</span>
                      <small>{gap.finding_id ? `Finding ${shortRef(gap.finding_id)}` : "Case-level limitation"}</small>
                      <strong>{gap.gap_type}</strong>
                      <p>{gap.affected_claim}</p>
                      <code title={gap.gap_id}>{shortRef(gap.gap_id)}</code>
                    </article>
                  ))}
                  {record.evidence.length === 0 && record.gaps.length === 0 ? (
                    <p className="quiet-copy">尚未链接 Evidence 或 Gap；零条引用不证明完整。</p>
                  ) : null}
                </div>
              </section>
              <section className="panel">
                <PanelHeading eyebrow="Annotations" title="调查笔记" id="case-notes-title" />
                <div className="note-list">
                  {record.annotations.map((item) => (
                    <article key={item.id}>
                      <p>{item.body}</p>
                      <small>{shortRef(item.created_by_subject)} · {formatDateTime(item.created_at)}</small>
                    </article>
                  ))}
                </div>
                {record.case.status === "open" ? (
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

export function ConnectorsPage({ session }: PageProps) {
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
        description="安装、连接健康与交付状态分开显示。普通同步不依赖 Assurance；高级摘要仍不暴露可变游标。"
      />
      {result?.installation_control?.enabled && canManage ? (
        <section className="panel connector-install-panel">
          <PanelHeading eyebrow="Install Connector" title="连接 UniRouter" id="connector-install-title" />
          <p className="boundary-note">邀请仅绑定当前 X deployment、固定来源与 model.request.completed；UniRouter 用户确认项目后才形成最终授权。</p>
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
              <div><span className={`trust-state trust-${installation.status === "abnormal" ? "degraded" : "inactive"}`}>{installation.status}</span><h2>UniRouter installation</h2></div>
              <code>{shortRef(installation.installation_id)}</code>
            </header>
            <div className="connector-columns">
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
            <p className="boundary-note">Backlog 只接受 UniRouter agent 的显式健康回执；不会从 X 数据量推断。</p>
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
    evidence_backed: { className: "kind-direct", label: "Evidence-backed claim" },
    deterministic_derived: { className: "kind-derived", label: "Deterministic derivation" },
    hypothesis: { className: "kind-hypothesis", label: "Hypothesis · review required" },
    unknown: { className: "kind-gap", label: "Unknown" },
  }[classification] ?? { className: "kind-gap", label: classification };
  return <span className={`evidence-kind ${presentation.className}`}>{presentation.label}</span>;
}
