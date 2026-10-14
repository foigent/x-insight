import { FormEvent, useEffect, useMemo, useRef, useState } from "react";

import {
  ApiRequestError,
  investigationAPI,
  workspaceAPI,
  type AccessSessionResponse,
  type ExecutionOutcome,
  type ExecutionDetail,
  type ExecutionSearchResponse,
  type SearchFilters,
  type SourceOption,
} from "./api";
import { executionFilterQuery, readExecutionList, useExecutionLocation, writeExecutionList } from "./executionReading";
import { EmptyState, ErrorState, LoadingBlock } from "./Feedback";
import { formatCanonicalTime, formatDateTime, localInputToUTC } from "./format";
import { FreshnessBadge, OutcomeBadge, ProofBadge, SemanticLegend } from "./status";

type IDMode = "exact" | "prefix";

interface DraftFilters {
  idMode: IDMode;
  requestText: string;
  sourceSystemID: string;
  outcome: "" | ExecutionOutcome;
  hasGaps: "" | "true" | "false";
  fromLocal: string;
  toLocal: string;
}

const initialDraft: DraftFilters = {
  idMode: "exact",
  requestText: "",
  sourceSystemID: "",
  outcome: "",
  hasGaps: "",
  fromLocal: "",
  toLocal: "",
};

export function ExecutionSearchPage({
  navigate,
  session,
}: {
  navigate: (path: string) => void;
  session: AccessSessionResponse;
}) {
  const location = useExecutionLocation();
  const reading = readExecutionList(location);
  const activeFilters = reading.filters;
  const filterKey = executionFilterQuery(activeFilters);
  const draftKey = executionFilterQuery({ ...activeFilters, asOf: undefined });
  const [draft, setDraft] = useState(() => draftFromFilters(activeFilters));
  const resolvedQuery = useRef("");
  const [preview, setPreview] = useState<ExecutionDetail>();
  const [previewError, setPreviewError] = useState<unknown>();
  const [previewLoading, setPreviewLoading] = useState(false);
  const [refreshRevision, setRefreshRevision] = useState(0);
  const [result, setResult] = useState<ExecutionSearchResponse>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>();
  const [validationError, setValidationError] = useState("");
  const [sources, setSources] = useState<SourceOption[]>([]);
  const [sourcesError, setSourcesError] = useState<unknown>();
  const canSaveView = session.session.permissions.includes("evidence.read") && session.session.permissions.includes("investigation.write");
  const [savedViewName, setSavedViewName] = useState("");
  const [saveState, setSaveState] = useState<"idle" | "saving" | "saved" | "error">("idle");

  const currentCursor = reading.cursor;
  const cursorFailure = error instanceof ApiRequestError && ["cursor_expired", "cursor_mismatch", "invalid_cursor"].includes(error.problem?.code ?? "");
  const requestKey = `${filterKey}|${currentCursor ?? ""}|${refreshRevision}`;
  const accessStatus = window.location.protocol === "https:"
    ? "HTTPS · 产品 Session"
    : "本机开发入口";

  useEffect(() => {
    const controller = new AbortController();
    investigationAPI
      .listSources(controller.signal)
      .then((response) => {
        setSources(response.items);
        setSourcesError(undefined);
      })
      .catch((caught: unknown) => {
        if (!controller.signal.aborted) {
          setSourcesError(caught);
        }
      });
    return () => controller.abort();
  }, []);

  useEffect(() => { setDraft(draftFromFilters(activeFilters)); setValidationError(""); }, [draftKey]);
  useEffect(() => {
    if (resolvedQuery.current === requestKey && result) return;
    const controller = new AbortController();
    setLoading(true); setError(undefined); setResult(undefined);
    investigationAPI.search(activeFilters, currentCursor, controller.signal).then(response => {
      if (controller.signal.aborted) return;
      if (activeFilters.asOf && Date.parse(activeFilters.asOf) !== Date.parse(response.page.as_of)) throw new Error("Requested reading cutoff was not returned");
      const canonicalFilters = { ...activeFilters, asOf: response.page.as_of };
      resolvedQuery.current = `${executionFilterQuery(canonicalFilters)}|${currentCursor ?? ""}|${refreshRevision}`;
      setResult(response); setLoading(false);
      writeExecutionList({ ...readExecutionList(), filters: canonicalFilters }, true);
    }).catch((caught: unknown) => { if (!controller.signal.aborted) { setError(caught); setLoading(false); } });
    return () => controller.abort();
  }, [requestKey]);

  useEffect(() => {
    setPreview(undefined); setPreviewError(undefined); setPreviewLoading(false);
    if (!reading.selected || !activeFilters.asOf || loading || error) return;
    const controller = new AbortController(); setPreviewLoading(true);
    investigationAPI.getExecution(reading.selected, activeFilters.asOf, controller.signal).then(value => {
      if (controller.signal.aborted) return;
      if (Date.parse(value.as_of) !== Date.parse(activeFilters.asOf!)) throw new Error("Requested preview cutoff was not returned");
      setPreview(value);
    }).catch(caught => { if (!controller.signal.aborted) setPreviewError(caught); })
      .finally(() => { if (!controller.signal.aborted) setPreviewLoading(false); });
    return () => controller.abort();
  }, [reading.selected, activeFilters.asOf, loading, error]);

  function restart(latest = false) {
    const filters = { ...activeFilters }; if (latest) delete filters.asOf;
    resolvedQuery.current = "";
    writeExecutionList({ filters, cursors: [""], index: 0 });
    setRefreshRevision(value => value + 1);
  }

  const filterCount = useMemo(
    () => Object.values(activeFilters).filter((value) => value !== undefined && value !== "").length,
    [activeFilters],
  );

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const validation = validateDraft(draft);
    if (validation) {
      setValidationError(validation);
      return;
    }
    setValidationError("");
    const requestText = draft.requestText;
    const filters: SearchFilters = {
      requestID: draft.idMode === "exact" && requestText ? requestText : undefined,
      requestIDPrefix: draft.idMode === "prefix" && requestText ? requestText : undefined,
      sourceSystemID: draft.sourceSystemID || undefined,
      outcome: draft.outcome || undefined,
      hasGaps: draft.hasGaps === "" ? undefined : draft.hasGaps === "true",
      from: localInputToUTC(draft.fromLocal),
      to: localInputToUTC(draft.toLocal),
    };
    writeExecutionList({ filters, cursors: [""], index: 0 });
  }

  function resetFilters() {
    setDraft(initialDraft);
    setValidationError("");
    writeExecutionList({ filters: {}, cursors: [""], index: 0 });
  }

  async function saveView() {
    const name = savedViewName.trim();
    if (!name || !canSaveView) return;
    setSaveState("saving");
    try {
      const persistedFilters = { ...activeFilters };
      delete persistedFilters.asOf;
      await workspaceAPI.createSavedView(
        name,
        persistedFilters,
        "explorer",
        session.csrf_token,
      );
      setSavedViewName("");
      setSaveState("saved");
    } catch {
      setSaveState("error");
    }
  }

  function nextPage() {
    const cursor = result?.page.next_cursor;
    if (!cursor) {
      return;
    }
    const cursors = reading.index === null ? [cursor] : [...reading.cursors.slice(0, reading.index + 1), cursor];
    writeExecutionList({ ...reading, cursor, selected: undefined, cursors, index: reading.index === null ? null : reading.index + 1 });
  }

  return (
    <>
      <header className="topbar">
        <div>
          <span className="eyebrow">有依据的执行查询</span>
          <h1>执行调查</h1>
          <p className="page-intro">按受限字段定位执行，再进入证据时间线；默认只查询最近 24 小时。</p>
        </div>
        <div className="environment-chip" aria-label="访问状态">
          <span className="status-dot" aria-hidden="true" />
          {accessStatus}
        </div>
      </header>

      <section className="search-panel" aria-labelledby="search-title">
        <div className="section-heading">
          <div>
            <span className="eyebrow">受限范围查询</span>
            <h2 id="search-title">查找请求</h2>
          </div>
          <details><summary>筛选范围</summary><p>只支持已接受的标识、时间、来源、结果和缺口筛选；不执行全文检索。</p></details>
        </div>

        <form className="filter-form" onSubmit={submit} noValidate>
          <div className="filter-field filter-field-wide">
            <label htmlFor="request-search">Request ID</label>
            <div className="compound-input">
              <select
                aria-label="Request ID 匹配方式"
                value={draft.idMode}
                onChange={(event) => setDraft({ ...draft, idMode: event.target.value as IDMode })}
              >
                <option value="exact">精确</option>
                <option value="prefix">前缀</option>
              </select>
              <input
                id="request-search"
                value={draft.requestText}
                maxLength={draft.idMode === "exact" ? 256 : 128}
                placeholder={draft.idMode === "exact" ? "例如 req-auth-denied-001" : "至少 3 个 UTF-8 字节"}
                onChange={(event) => setDraft({ ...draft, requestText: event.target.value })}
              />
            </div>
          </div>

          <div className="filter-field">
            <label htmlFor="source-filter">来源</label>
            <select
              id="source-filter"
              value={draft.sourceSystemID}
              onChange={(event) => setDraft({ ...draft, sourceSystemID: event.target.value })}
            >
              <option value="">全部可见来源</option>
              {sources.map((source) => (
                <option key={source.source_system_id} value={source.source_system_id}>
                  {source.display_name} · {source.source_type}
                </option>
              ))}
            </select>
          </div>

          <div className="filter-field">
            <label htmlFor="outcome-filter">结果</label>
            <select
              id="outcome-filter"
              value={draft.outcome}
              onChange={(event) => setDraft({ ...draft, outcome: event.target.value as DraftFilters["outcome"] })}
            >
              <option value="">全部结果</option>
              <option value="success">成功 · success</option>
              <option value="failure">失败 · failure</option>
              <option value="partial">部分完成 · partial</option>
              <option value="unknown">未知 · unknown</option>
            </select>
          </div>

          <div className="filter-field">
            <label htmlFor="gap-filter">Capture gaps</label>
            <select
              id="gap-filter"
              value={draft.hasGaps}
              onChange={(event) => setDraft({ ...draft, hasGaps: event.target.value as DraftFilters["hasGaps"] })}
            >
              <option value="">全部</option>
              <option value="true">存在缺口</option>
              <option value="false">未记录缺口</option>
            </select>
          </div>

          <div className="filter-field">
            <label htmlFor="from-filter">开始时间（本地）</label>
            <input
              id="from-filter"
              type="datetime-local"
              value={draft.fromLocal}
              onChange={(event) => setDraft({ ...draft, fromLocal: event.target.value })}
            />
          </div>

          <div className="filter-field">
            <label htmlFor="to-filter">结束时间（本地）</label>
            <input
              id="to-filter"
              type="datetime-local"
              value={draft.toLocal}
              onChange={(event) => setDraft({ ...draft, toLocal: event.target.value })}
            />
          </div>

          <div className="filter-actions">
            <button className="button button-primary" type="submit">
              查询
            </button>
            <button className="button button-secondary" type="button" onClick={resetFilters}>
              清空
            </button>
            <span>{filterCount > 0 ? `已启用 ${filterCount} 个筛选值` : "默认最近 24 小时"}</span>
          </div>
        </form>
        {validationError && <p className="inline-validation" role="alert">{validationError}</p>}
        {Boolean(sourcesError) && (
          <div className="degraded-note" role="status">
            来源选项暂时不可用；Request ID、时间、结果和缺口筛选仍可继续。
          </div>
        )}
      </section>

      <section className="result-panel" aria-labelledby="result-title" aria-busy={loading}>
        <div className="section-heading result-heading">
          <div>
            <span className="eyebrow">固定快照</span>
            <h2 id="result-title">执行记录</h2>
          </div>
          <div className="result-meta" aria-live="polite">
            {result && (
              <>
                <span>{reading.index === null ? "当前页（页序未知）" : `第 ${reading.index + 1} 页`}</span>
                <span>本页 {result.page.count} 条</span>
                <span>{result.page.has_more ? "还有下一页" : "已到当前结果末尾"}</span>
              </>
            )}
            <button
              className="button button-quiet"
              type="button"
              onClick={() => setRefreshRevision((current) => current + 1)}
              disabled={loading}
            >
              重新读取本页
            </button>
          </div>
        </div>

        {canSaveView && <details className="saved-view-disclosure">
          <summary>
            <span>保存为调查入口</span>
            <small>只保存筛选与呈现方式，不复制结果快照</small>
          </summary>
          <div className="saved-view-bar">
            <label htmlFor="saved-view-name">视图名称
              <input
                id="saved-view-name"
                value={savedViewName}
                maxLength={120}
                placeholder="例如：生产失败与缺口"
                onChange={(event) => {
                  setSavedViewName(event.target.value);
                  setSaveState("idle");
                }}
              />
            </label>
            <button
              className="button button-secondary"
              type="button"
              disabled={!savedViewName.trim() || saveState === "saving"}
              onClick={() => void saveView()}
            >
              {saveState === "saving" ? "正在保存…" : "保存视图"}
            </button>
            <span role="status">
              {saveState === "saved" ? "已保存；结果本身未被复制。" : saveState === "error" ? "保存失败。" : "后续打开时重新读取一个固定快照。"}
            </span>
          </div>
        </details>}

        {loading && !result && <LoadingBlock label="正在查询 executions" />}
        {Boolean(error) && <><ErrorState error={error} onRetry={cursorFailure ? undefined : () => setRefreshRevision(current => current + 1)} />
          <div className="reading-recovery"><p>原阅读尚未恢复。回到首批保留截止与筛选；读取最新会建立新的阅读。</p><button className="button button-secondary" onClick={() => restart()}>从此截止第一页重新读取</button><button className="button button-secondary" onClick={() => restart(true)}>读取最新结果</button></div></>}
        {!error && result && result.items.length === 0 && (
          <EmptyState
            title="当前条件下没有 execution"
            description="这可能表示所选时间内没有证据、来源尚未导入，或筛选条件过窄；空结果不等于来源完整。"
          />
        )}
        <div className={`execution-reading-layout${reading.selected ? " has-preview" : ""}`}>
        {!error && result && result.items.length > 0 && (
          <div className={`table-shell${loading ? " table-shell-loading" : ""}`}>
            <table className="execution-table">
              <caption>固定依据快照中的执行查询结果</caption>
              <thead>
                <tr>
                  <th>时间</th>
                  <th>请求与执行</th>
                  <th>来源</th>
                  <th>结果</th>
                  <th>证据状态</th>
                  <th>正文采集</th>
                  <th>缺口</th>
                </tr>
              </thead>
              <tbody>
                {result.items.map((execution) => (
                  <tr key={execution.execution_id} className={`${execution.has_gaps ? "row-has-gap " : ""}${reading.selected === execution.execution_id ? "execution-row-selected" : ""}`}>
                    <td>
                      <time dateTime={execution.last_effective_at} title={formatCanonicalTime(execution.last_effective_at)}>
                        {formatDateTime(execution.last_effective_at)}
                      </time>
                      <small>{execution.event_count} 条证据事件</small>
                    </td>
                    <td>
                      <button
                        type="button"
                        className="execution-link"
                        aria-pressed={reading.selected === execution.execution_id}
                        onClick={() => writeExecutionList({ ...reading, selected: execution.execution_id })}
                      >
                        {execution.request_id}
                      </button>
                      <details><summary>执行标识</summary><code>{execution.execution_id}</code></details>
                    </td>
                    <td>
                      <strong>{execution.source_display_name || execution.source_type}</strong>
                      <small>{execution.source_type}</small>
                    </td>
                    <td><OutcomeBadge outcome={execution.outcome} compact /></td>
                    <td>
                      <div className="stacked-badges">
                        <ProofBadge proofClass={execution.proof_class} compact />
                        <FreshnessBadge state={execution.freshness.sync_state} compact />
                      </div>
                      <details><summary>状态代码</summary><code>{execution.proof_class}</code> · <code>{execution.freshness.sync_state}</code></details>
                    </td>
                    <td>
                      <div className="payload-list">
                        {(execution.payload_capture_modes?.length ? execution.payload_capture_modes : ["unknown"]).map(
                          (mode) => <span key={mode} title={mode}>{mode === "metadata_only" ? "仅元数据" : mode === "unknown" ? "状态未知" : "来源正文模式"}</span>,
                        )}
                      </div>
                      <details><summary>采集代码</summary>{(execution.payload_capture_modes?.length ? execution.payload_capture_modes : ["unknown"]).map(mode => <code key={mode}>{mode}</code>)}</details>
                    </td>
                    <td>
                      <span className={`gap-summary${execution.gap_count > 0 ? " gap-summary-present" : " gap-summary-none"}`}>
                        <strong>{execution.gap_count}</strong>
                        <small>{execution.gap_count > 0 ? "个已记录缺口" : "无已记录缺口"}</small>
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {reading.selected && !loading && !error && <aside className="execution-preview" aria-label="所选运行摘要">
          <div className="section-heading"><h2>所选运行</h2><button className="button button-secondary" onClick={() => writeExecutionList({ ...reading, selected: undefined })}>关闭摘要</button></div>
          {previewLoading && <LoadingBlock label="正在读取所选运行" />}
          {Boolean(previewError) && <><ErrorState error={previewError} compact /><p>当前选择无法在此截止与授权下读取；列表仍保留，未改选其他请求。</p></>}
          {preview && <><h3>{preview.summary.request_id}</h3><OutcomeBadge outcome={preview.summary.outcome}/><ProofBadge proofClass={preview.summary.proof_class}/>
            <dl><div><dt>来源</dt><dd>{preview.summary.source_display_name || preview.summary.source_type}</dd></div>
            <div><dt>最后记录</dt><dd>{formatDateTime(preview.summary.last_effective_at)}</dd></div>
            <div><dt>已记录事件 / 缺口</dt><dd>{preview.summary.event_count} / {preview.summary.gap_count}</dd></div>
            <div><dt>证据同步</dt><dd><FreshnessBadge state={preview.summary.freshness.sync_state}/></dd></div></dl>
            <p>摘要限于当前授权和固定截止，事件数量不代表请求总数或过程完整。</p>
            <button className="button button-primary" onClick={() => navigate(`/executions/${encodeURIComponent(preview.summary.execution_id)}?as_of=${encodeURIComponent(preview.as_of)}`)}>进入完整执行详情</button>
          </>}
        </aside>}
        </div>

        {result && !error && (
          <footer className="pagination-bar">
            <button
              type="button"
              className="button button-secondary"
              onClick={() => { if (reading.index !== null && reading.index > 0) writeExecutionList({ ...reading, cursor: reading.cursors[reading.index - 1] || undefined, selected: undefined, index: reading.index - 1 }); }}
              disabled={reading.index === null || reading.index === 0 || loading}
            >
              上一页
            </button>
            <span>
              {reading.index === null ? "当前页（页序未知）" : `第 ${reading.index + 1} 页`} · as_of <code>{formatCanonicalTime(result.page.as_of)}</code>
            </span>
            <button
              type="button"
              className="button button-secondary"
              onClick={nextPage}
              disabled={!result.page.has_more || !result.page.next_cursor || loading}
            >
              下一页
            </button>
          </footer>
        )}
        {reading.index === null && !error && <div className="reading-recovery"><p>此链接没有可用的上一页游标记录；无法恢复页号或推算上一页。</p><button className="button button-secondary" onClick={() => restart()}>从此截止第一页重新读取</button></div>}
      </section>

      <SemanticLegend />
    </>
  );
}

function draftFromFilters(filters: SearchFilters): DraftFilters {
  return {
    idMode: filters.requestIDPrefix ? "prefix" : "exact",
    requestText: filters.requestIDPrefix || filters.requestID || "",
    sourceSystemID: filters.sourceSystemID || "",
    outcome: filters.outcome || "",
    hasGaps: filters.hasGaps === undefined ? "" : String(filters.hasGaps) as "true" | "false",
    fromLocal: utcToLocalInput(filters.from),
    toLocal: utcToLocalInput(filters.to),
  };
}

function utcToLocalInput(value?: string) {
  if (!value) return "";
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return "";
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

function validateDraft(draft: DraftFilters): string {
  if (draft.requestText !== draft.requestText.trim()) {
    return "Request ID 不允许前后空白。";
  }
  if (draft.idMode === "prefix" && draft.requestText && new TextEncoder().encode(draft.requestText).length < 3) {
    return "Request ID 前缀至少需要 3 个 UTF-8 字节。";
  }
  if (draft.fromLocal && draft.toLocal) {
    const from = new Date(draft.fromLocal).getTime();
    const to = new Date(draft.toLocal).getTime();
    if (!Number.isFinite(from) || !Number.isFinite(to) || from >= to) {
      return "开始时间必须早于结束时间。";
    }
    if (to - from > 31 * 24 * 60 * 60 * 1000) {
      return "单次查询时间范围不能超过 31 天。";
    }
  }
  return "";
}
