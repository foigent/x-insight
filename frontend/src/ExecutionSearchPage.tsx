import { t, tx, useLocale, useMessageState } from "./i18n";
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
  useLocale();
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
  const [validationError, setValidationError] = useMessageState("");
  const [sources, setSources] = useState<SourceOption[]>([]);
  const [sourcesError, setSourcesError] = useState<unknown>();
  const canSaveView = session.session.permissions.includes("evidence.read") && session.session.permissions.includes("investigation.write");
  const [savedViewName, setSavedViewName] = useState("");
  const [saveState, setSaveState] = useState<"idle" | "saving" | "saved" | "error">("idle");

  const currentCursor = reading.cursor;
  const cursorFailure = error instanceof ApiRequestError && ["cursor_expired", "cursor_mismatch", "invalid_cursor"].includes(error.problem?.code ?? "");
  const requestKey = `${filterKey}|${currentCursor ?? ""}|${refreshRevision}`;
  const accessStatus = window.location.protocol === "https:"
    ? t("m0597")
    : t("m0598");

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

  useEffect(() => { setDraft(draftFromFilters(activeFilters)); setValidationError(() => ""); }, [draftKey]);
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
      setValidationError(() => validateDraft(draft));
      return;
    }
    setValidationError(() => "");
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
    setValidationError(() => "");
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
          <span className="eyebrow">{tx("m0599")}</span>
          <h1>{tx("m0600")}</h1>
          <p className="page-intro">{tx("m0601")}</p>
        </div>
        <div className="environment-chip" aria-label={t("m0602")}>
          <span className="status-dot" aria-hidden="true" />
          {accessStatus}
        </div>
      </header>

      <section className="search-panel" aria-labelledby="search-title">
        <div className="section-heading">
          <div>
            <span className="eyebrow">{tx("m0603")}</span>
            <h2 id="search-title">{tx("m0604")}</h2>
          </div>
          <details><summary>{tx("m0605")}</summary><p>{tx("m0606")}</p></details>
        </div>

        <form className="filter-form" onSubmit={submit} noValidate>
          <div className="filter-field filter-field-wide">
            <label htmlFor="request-search">Request ID</label>
            <div className="compound-input">
              <select
                aria-label={t("m0607")}
                value={draft.idMode}
                onChange={(event) => setDraft({ ...draft, idMode: event.target.value as IDMode })}
              >
                <option value="exact">{tx("m0608")}</option>
                <option value="prefix">{tx("m0609")}</option>
              </select>
              <input
                id="request-search"
                value={draft.requestText}
                maxLength={draft.idMode === "exact" ? 256 : 128}
                placeholder={draft.idMode === "exact" ? t("m0610") : t("m0611")}
                onChange={(event) => setDraft({ ...draft, requestText: event.target.value })}
              />
            </div>
          </div>

          <div className="filter-field">
            <label htmlFor="source-filter">{tx("m0472")}</label>
            <select
              id="source-filter"
              value={draft.sourceSystemID}
              onChange={(event) => setDraft({ ...draft, sourceSystemID: event.target.value })}
            >
              <option value="">{tx("m0612")}</option>
              {sources.map((source) => (
                <option key={source.source_system_id} value={source.source_system_id}>
                  {source.display_name} · {source.source_type}
                </option>
              ))}
            </select>
          </div>

          <div className="filter-field">
            <label htmlFor="outcome-filter">{tx("m0554")}</label>
            <select
              id="outcome-filter"
              value={draft.outcome}
              onChange={(event) => setDraft({ ...draft, outcome: event.target.value as DraftFilters["outcome"] })}
            >
              <option value="">{tx("m0613")}</option>
              <option value="success">{tx("m0614")}</option>
              <option value="failure">{tx("m0615")}</option>
              <option value="partial">{tx("m0616")}</option>
              <option value="unknown">{tx("m0617")}</option>
            </select>
          </div>

          <div className="filter-field">
            <label htmlFor="gap-filter">Capture gaps</label>
            <select
              id="gap-filter"
              value={draft.hasGaps}
              onChange={(event) => setDraft({ ...draft, hasGaps: event.target.value as DraftFilters["hasGaps"] })}
            >
              <option value="">{tx("m0618")}</option>
              <option value="true">{tx("m0619")}</option>
              <option value="false">{tx("m0620")}</option>
            </select>
          </div>

          <div className="filter-field">
            <label htmlFor="from-filter">{tx("m0621")}</label>
            <input
              id="from-filter"
              type="datetime-local"
              value={draft.fromLocal}
              onChange={(event) => setDraft({ ...draft, fromLocal: event.target.value })}
            />
          </div>

          <div className="filter-field">
            <label htmlFor="to-filter">{tx("m0622")}</label>
            <input
              id="to-filter"
              type="datetime-local"
              value={draft.toLocal}
              onChange={(event) => setDraft({ ...draft, toLocal: event.target.value })}
            />
          </div>

          <div className="filter-actions">
            <button className="button button-primary" type="submit">{tx("m0623")}</button>
            <button className="button button-secondary" type="button" onClick={resetFilters}>{tx("m0624")}</button>
            <span>{filterCount > 0 ? t("m0625", { value0: filterCount }) : t("m0626")}</span>
          </div>
        </form>
        {validationError && <p className="inline-validation" role="alert">{validationError}</p>}
        {Boolean(sourcesError) && (
          <div className="degraded-note" role="status">{tx("m0627")}</div>
        )}
      </section>

      <section className="result-panel" aria-labelledby="result-title" aria-busy={loading}>
        <div className="section-heading result-heading">
          <div>
            <span className="eyebrow">{tx("m0628")}</span>
            <h2 id="result-title">{tx("m0158")}</h2>
          </div>
          <div className="result-meta" aria-live="polite">
            {result && (
              <>
                <span>{reading.index === null ? t("m0629") : t("m0630", { value0: reading.index + 1 })}</span>
                <span>{tx("m0631", { value0: result.page.count })}</span>
                <span>{result.page.has_more ? t("m0632") : t("m0633")}</span>
              </>
            )}
            <button
              className="button button-quiet"
              type="button"
              onClick={() => setRefreshRevision((current) => current + 1)}
              disabled={loading}
            >{tx("m0634")}</button>
          </div>
        </div>

        {canSaveView && <details className="saved-view-disclosure">
          <summary>
            <span>{tx("m0635")}</span>
            <small>{tx("m0636")}</small>
          </summary>
          <div className="saved-view-bar">
            <label htmlFor="saved-view-name">{t("m0637")}<input
                id="saved-view-name"
                value={savedViewName}
                maxLength={120}
                placeholder={t("m0638")}
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
              {saveState === "saving" ? t("m0639") : t("m0640")}
            </button>
            <span role="status">
              {saveState === "saved" ? t("m0641") : saveState === "error" ? t("m0642") : t("m0643")}
            </span>
          </div>
        </details>}

        {loading && !result && <LoadingBlock label={t("m0644")} />}
        {Boolean(error) && <><ErrorState error={error} onRetry={cursorFailure ? undefined : () => setRefreshRevision(current => current + 1)} />
          <div className="reading-recovery"><p>{tx("m0645")}</p><button className="button button-secondary" onClick={() => restart()}>{tx("m0646")}</button><button className="button button-secondary" onClick={() => restart(true)}>{tx("m0647")}</button></div></>}
        {!error && result && result.items.length === 0 && (
          <EmptyState
            title={t("m0648")}
            description={t("m0649")}
          />
        )}
        <div className={`execution-reading-layout${reading.selected ? " has-preview" : ""}`}>
        {!error && result && result.items.length > 0 && (
          <div className={`table-shell${loading ? " table-shell-loading" : ""}`}>
            <table className="execution-table">
              <caption>{tx("m0650")}</caption>
              <thead>
                <tr>
                  <th>{tx("m0651")}</th>
                  <th>{tx("m0652")}</th>
                  <th>{tx("m0472")}</th>
                  <th>{tx("m0554")}</th>
                  <th>{tx("m0653")}</th>
                  <th>{tx("m0654")}</th>
                  <th>{tx("m0655")}</th>
                </tr>
              </thead>
              <tbody>
                {result.items.map((execution) => (
                  <tr key={execution.execution_id} className={`${execution.has_gaps ? "row-has-gap " : ""}${reading.selected === execution.execution_id ? "execution-row-selected" : ""}`}>
                    <td data-label={t("m0651")}>
                      <time dateTime={execution.last_effective_at} title={formatCanonicalTime(execution.last_effective_at)}>
                        {formatDateTime(execution.last_effective_at)}
                      </time>
                      <small>{tx("m0656", { value0: execution.event_count })}</small>
                    </td>
                    <td data-label={t("m0652")}>
                      <button
                        type="button"
                        className="execution-link"
                        aria-pressed={reading.selected === execution.execution_id}
                        onClick={() => writeExecutionList({ ...reading, selected: execution.execution_id })}
                      >
                        {execution.request_id}
                      </button>
                      <details><summary>{tx("m0462")}</summary><code>{execution.execution_id}</code></details>
                    </td>
                    <td data-label={t("m0472")}>
                      <strong>{execution.source_display_name || execution.source_type}</strong>
                      <small>{execution.source_type}</small>
                    </td>
                    <td data-label={t("m0554")}><OutcomeBadge outcome={execution.outcome} compact /></td>
                    <td data-label={t("m0653")}>
                      <div className="stacked-badges">
                        <ProofBadge proofClass={execution.proof_class} compact />
                        <FreshnessBadge state={execution.freshness.sync_state} compact />
                      </div>
                      <details><summary>{tx("m0657")}</summary><code>{execution.proof_class}</code> · <code>{execution.freshness.sync_state}</code></details>
                    </td>
                    <td data-label={t("m0654")}>
                      <div className="payload-list">
                        {(execution.payload_capture_modes?.length ? execution.payload_capture_modes : ["unknown"]).map(
                          (mode) => <span key={mode} title={mode}>{mode === "metadata_only" ? t("m0481") : mode === "unknown" ? t("m0658") : t("m0659")}</span>,
                        )}
                      </div>
                      <details><summary>{tx("m0660")}</summary>{(execution.payload_capture_modes?.length ? execution.payload_capture_modes : ["unknown"]).map(mode => <code key={mode}>{mode}</code>)}</details>
                    </td>
                    <td data-label={t("m0655")}>
                      <span className={`gap-summary${execution.gap_count > 0 ? " gap-summary-present" : " gap-summary-none"}`}>
                        <strong>{execution.gap_count}</strong>
                        <small>{execution.gap_count > 0 ? t("m0661") : t("m0662")}</small>
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {reading.selected && !loading && !error && <aside className="execution-preview" aria-label={t("m0663")}>
          <div className="section-heading"><h2>{tx("m0664")}</h2><button className="button button-secondary" onClick={() => writeExecutionList({ ...reading, selected: undefined })}>{tx("m0665")}</button></div>
          {previewLoading && <LoadingBlock label={t("m0666")} />}
          {Boolean(previewError) && <><ErrorState error={previewError} compact /><p>{tx("m0667")}</p></>}
          {preview && <><h3>{preview.summary.request_id}</h3><OutcomeBadge outcome={preview.summary.outcome}/><ProofBadge proofClass={preview.summary.proof_class}/>
            <dl><div><dt>{tx("m0472")}</dt><dd>{preview.summary.source_display_name || preview.summary.source_type}</dd></div>
            <div><dt>{tx("m0668")}</dt><dd>{formatDateTime(preview.summary.last_effective_at)}</dd></div>
            <div><dt>{tx("m0669")}</dt><dd>{preview.summary.event_count} / {preview.summary.gap_count}</dd></div>
            <div><dt>{tx("m0670")}</dt><dd><FreshnessBadge state={preview.summary.freshness.sync_state}/></dd></div></dl>
            <p>{tx("m0671")}</p>
            <button className="button button-primary" onClick={() => navigate(`/executions/${encodeURIComponent(preview.summary.execution_id)}?as_of=${encodeURIComponent(preview.as_of)}`)}>{tx("m0672")}</button>
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
            >{tx("m0673")}</button>
            <span>
              {reading.index === null ? t("m0629") : t("m0630", { value0: reading.index + 1 })} · as_of <code>{formatCanonicalTime(result.page.as_of)}</code>
            </span>
            <button
              type="button"
              className="button button-secondary"
              onClick={nextPage}
              disabled={!result.page.has_more || !result.page.next_cursor || loading}
            >{tx("m0674")}</button>
          </footer>
        )}
        {reading.index === null && !error && <div className="reading-recovery"><p>{tx("m0675")}</p><button className="button button-secondary" onClick={() => restart()}>{tx("m0646")}</button></div>}
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
    return t("m0676");
  }
  if (draft.idMode === "prefix" && draft.requestText && new TextEncoder().encode(draft.requestText).length < 3) {
    return t("m0677");
  }
  if (draft.fromLocal && draft.toLocal) {
    const from = new Date(draft.fromLocal).getTime();
    const to = new Date(draft.toLocal).getTime();
    if (!Number.isFinite(from) || !Number.isFinite(to) || from >= to) {
      return t("m0678");
    }
    if (to - from > 31 * 24 * 60 * 60 * 1000) {
      return t("m0679");
    }
  }
  return "";
}
