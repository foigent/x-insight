import { useSyncExternalStore } from "react";
import type { SearchFilters } from "./api";

const changedEvent = "xinsight:reading-changed";
const locationKey = () => window.location.pathname + window.location.search;
function subscribe(listener: () => void) {
  window.addEventListener("popstate", listener);
  window.addEventListener(changedEvent, listener);
  return () => { window.removeEventListener("popstate", listener); window.removeEventListener(changedEvent, listener); };
}
export function useExecutionLocation() { return useSyncExternalStore(subscribe, locationKey); }
export function writeReading(path: string, replace = false, state = window.history.state) {
  if (replace) window.history.replaceState(state, "", path);
  else window.history.pushState(state, "", path);
  window.dispatchEvent(new Event(changedEvent));
}
export function readExecutionFilters(search: string): SearchFilters {
  const params = new URLSearchParams(search), outcome = params.get("outcome"), hasGaps = params.get("has_gaps");
  return {
    asOf: params.get("as_of") || undefined,
    requestID: params.get("request_id") || undefined,
    requestIDPrefix: params.get("request_id_prefix") || undefined,
    sourceSystemID: params.get("source_system_id") || undefined,
    outcome: outcome === "success" || outcome === "failure" || outcome === "partial" || outcome === "unknown" ? outcome : undefined,
    hasGaps: hasGaps === "true" ? true : hasGaps === "false" ? false : undefined,
    from: params.get("from") || undefined, to: params.get("to") || undefined,
  };
}
export function executionFilterQuery(filters: SearchFilters) {
  const params = new URLSearchParams();
  const fields = { as_of: filters.asOf, request_id: filters.requestID, request_id_prefix: filters.requestIDPrefix,
    source_system_id: filters.sourceSystemID, outcome: filters.outcome, has_gaps: filters.hasGaps, from: filters.from, to: filters.to };
  for (const [key, value] of Object.entries(fields)) if (value !== undefined && value !== "") params.set(key, String(value));
  return params.toString();
}
export interface ExecutionListReading {
  filters: SearchFilters; cursor?: string; selected?: string; cursors: string[]; index: number | null;
}
export function readExecutionList(path = locationKey(), state = window.history.state): ExecutionListReading {
  const search = path.includes("?") ? path.slice(path.indexOf("?") + 1) : "", params = new URLSearchParams(search), filters = readExecutionFilters(search);
  const cursor = params.get("cursor") || undefined, saved = state?.executionList;
  const validStack = saved?.filters === executionFilterQuery(filters) && Array.isArray(saved.cursors)
    && saved.cursors.every((value: unknown) => typeof value === "string") && saved.cursors[0] === ""
    && Number.isInteger(saved.index) && saved.index >= 0 && saved.cursors[saved.index] === (cursor ?? "");
  return { filters, cursor, selected: params.get("selected") || undefined,
    cursors: validStack ? saved.cursors : [cursor ?? ""], index: validStack ? saved.index : cursor ? null : 0 };
}
export function writeExecutionList(reading: ExecutionListReading, replace = false) {
  const params = new URLSearchParams(executionFilterQuery(reading.filters));
  if (reading.cursor) params.set("cursor", reading.cursor);
  if (reading.selected) params.set("selected", reading.selected);
  const executionList = reading.index === null ? undefined : { filters: executionFilterQuery(reading.filters), cursors: reading.cursors, index: reading.index };
  writeReading(`/executions${params.size ? "?" + params : ""}`, replace, { ...window.history.state, executionList });
}
export type ExecutionView = "contents" | "story" | "graph" | "chronology" | "proof" | "systems";
export function readExecutionDetail(path = locationKey()) {
  const params = new URLSearchParams(path.includes("?") ? path.slice(path.indexOf("?") + 1) : ""), view = params.get("view");
  return { asOf: params.get("as_of") || undefined, evidence: params.get("evidence") || undefined,
    view: (view === "graph" || view === "chronology" || view === "proof" || view === "systems" || view === "contents" ? view : "story") as ExecutionView };
}
export function writeExecutionDetail(executionID: string, reading: ReturnType<typeof readExecutionDetail>, replace = false) {
  const params = new URLSearchParams();
  if (reading.asOf) params.set("as_of", reading.asOf);
  if (reading.evidence) params.set("evidence", reading.evidence);
  if (reading.view !== "story") params.set("view", reading.view);
  writeReading(`/executions/${encodeURIComponent(executionID)}${params.size ? "?" + params : ""}`, replace);
}
