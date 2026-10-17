import { FormEvent, useEffect, useMemo, useState } from "react";

import {
  investigationAPI,
  workspaceAPI,
  type AccessSessionResponse,
  type ExecutionOutcome,
  type ExecutionSearchResponse,
  type SearchFilters,
  type SourceOption,
} from "./api";
import { EmptyState, ErrorState, LoadingBlock } from "./Feedback";
import { formatCanonicalTime, formatDateTime, localInputToUTC, shortRef } from "./format";
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
  const initialFilters = useMemo(() => readFiltersFromURL(), []);
  const [draft, setDraft] = useState(() => draftFromFilters(initialFilters));
  const [activeFilters, setActiveFilters] = useState<SearchFilters>(initialFilters);
  const [cursorStack, setCursorStack] = useState([""]);
  const [pageIndex, setPageIndex] = useState(0);
  const [refreshRevision, setRefreshRevision] = useState(0);
  const [result, setResult] = useState<ExecutionSearchResponse>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<unknown>();
  const [validationError, setValidationError] = useState("");
  const [sources, setSources] = useState<SourceOption[]>([]);
  const [sourcesError, setSourcesError] = useState<unknown>();
  const [savedViewName, setSavedViewName] = useState("");
  const [saveState, setSaveState] = useState<"idle" | "saving" | "saved" | "error">("idle");

  const currentCursor = cursorStack[pageIndex] || undefined;
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

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError(undefined);
    investigationAPI
      .search(activeFilters, currentCursor, controller.signal)
      .then((response) => {
        setResult(response);
        const canonicalFilters = { ...activeFilters, asOf: response.page.as_of };
        window.history.replaceState(null, "", `/executions?${filtersToQuery(canonicalFilters)}`);
        if (activeFilters.asOf !== response.page.as_of) {
          setActiveFilters(canonicalFilters);
        }
      })
      .catch((caught: unknown) => {
        if (!controller.signal.aborted) {
          setError(caught);
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setLoading(false);
        }
      });
    return () => controller.abort();
  }, [activeFilters, currentCursor, refreshRevision]);

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
    setCursorStack([""]);
    setPageIndex(0);
    setActiveFilters(filters);
    window.history.replaceState(null, "", `/executions?${filtersToQuery(filters)}`);
  }

  function resetFilters() {
    setDraft(initialDraft);
    setValidationError("");
    setCursorStack([""]);
    setPageIndex(0);
    setActiveFilters({});
    window.history.replaceState(null, "", "/executions");
  }

  async function saveView() {
    const name = savedViewName.trim();
    if (!name) return;
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
    setCursorStack((current) => [...current.slice(0, pageIndex + 1), cursor]);
    setPageIndex((current) => current + 1);
  }

  return (
    <>
      <header className="topbar">
        <div>
          <span className="eyebrow">Evidence-backed investigation</span>
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
            <span className="eyebrow">Bounded search</span>
            <h2 id="search-title">查找 execution</h2>
          </div>
          <p>只支持已接受的 ID、时间、来源、结果和缺口筛选；不执行全文检索或查询 DSL。</p>
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
            <h2 id="result-title">Execution 结果</h2>
          </div>
          <div className="result-meta" aria-live="polite">
            {result && (
              <>
                <span>第 {pageIndex + 1} 页</span>
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

        <details className="saved-view-disclosure">
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
        </details>

        {loading && !result && <LoadingBlock label="正在查询 executions" />}
        {Boolean(error) && <ErrorState error={error} onRetry={() => setRefreshRevision((current) => current + 1)} />}
        {!error && result && result.items.length === 0 && (
          <EmptyState
            title="当前条件下没有 execution"
            description="这可能表示所选时间内没有证据、来源尚未导入，或筛选条件过窄；空结果不等于来源完整。"
          />
        )}
        {!error && result && result.items.length > 0 && (
          <div className={`table-shell${loading ? " table-shell-loading" : ""}`}>
            <table className="execution-table">
              <caption>固定 Evidence 快照中的 execution 查询结果</caption>
              <thead>
                <tr>
                  <th>时间</th>
                  <th>Execution / Request</th>
                  <th>来源</th>
                  <th>结果</th>
                  <th>证据状态</th>
                  <th>Payload</th>
                  <th>缺口</th>
                </tr>
              </thead>
              <tbody>
                {result.items.map((execution) => (
                  <tr key={execution.execution_id} className={execution.has_gaps ? "row-has-gap" : undefined}>
                    <td>
                      <time dateTime={execution.last_effective_at} title={formatCanonicalTime(execution.last_effective_at)}>
                        {formatDateTime(execution.last_effective_at)}
                      </time>
                      <small>{execution.event_count} 个 evidence events</small>
                    </td>
                    <td>
                      <button
                        type="button"
                        className="execution-link"
                        onClick={() =>
                          navigate(
                            `/executions/${execution.execution_id}?as_of=${encodeURIComponent(result.page.as_of)}`,
                          )
                        }
                      >
                        {execution.request_id}
                      </button>
                      <code title={execution.execution_id}>{shortRef(execution.execution_id)}</code>
                    </td>
                    <td>
                      <strong>{execution.source_display_name || execution.source_type}</strong>
                      <small>{execution.source_type}</small>
                    </td>
                    <td><OutcomeBadge outcome={execution.outcome} /></td>
                    <td>
                      <div className="stacked-badges">
                        <ProofBadge proofClass={execution.proof_class} />
                        <FreshnessBadge state={execution.freshness.sync_state} />
                      </div>
                    </td>
                    <td>
                      <div className="payload-list">
                        {(execution.payload_capture_modes?.length ? execution.payload_capture_modes : ["unknown"]).map(
                          (mode) => <code key={mode}>{mode}</code>,
                        )}
                      </div>
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

        {result && !error && (
          <footer className="pagination-bar">
            <button
              type="button"
              className="button button-secondary"
              onClick={() => setPageIndex((current) => Math.max(0, current - 1))}
              disabled={pageIndex === 0 || loading}
            >
              上一页
            </button>
            <span>
              as_of <code>{formatCanonicalTime(result.page.as_of)}</code>
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
      </section>

      <SemanticLegend />
    </>
  );
}

function readFiltersFromURL(): SearchFilters {
  const params = new URLSearchParams(window.location.search);
  const outcome = params.get("outcome");
  const hasGaps = params.get("has_gaps");
  return {
    asOf: params.get("as_of") || undefined,
    requestID: params.get("request_id") || undefined,
    requestIDPrefix: params.get("request_id_prefix") || undefined,
    sourceSystemID: params.get("source_system_id") || undefined,
    outcome: outcome === "success" || outcome === "failure" || outcome === "partial" || outcome === "unknown"
      ? outcome
      : undefined,
    hasGaps: hasGaps === "true" ? true : hasGaps === "false" ? false : undefined,
    from: params.get("from") || undefined,
    to: params.get("to") || undefined,
  };
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

function filtersToQuery(filters: SearchFilters) {
  const params = new URLSearchParams();
  if (filters.asOf) params.set("as_of", filters.asOf);
  if (filters.requestID) params.set("request_id", filters.requestID);
  if (filters.requestIDPrefix) params.set("request_id_prefix", filters.requestIDPrefix);
  if (filters.sourceSystemID) params.set("source_system_id", filters.sourceSystemID);
  if (filters.outcome) params.set("outcome", filters.outcome);
  if (filters.hasGaps !== undefined) params.set("has_gaps", String(filters.hasGaps));
  if (filters.from) params.set("from", filters.from);
  if (filters.to) params.set("to", filters.to);
  return params.toString();
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
