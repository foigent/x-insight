import type { ReadingRequest as RuntimeReadingRequest, EntryTime } from "./runtime";
import type { ExecutionView } from "./executionReading";
import { accessAPI, scopeSessionKey, workbenchAPI, type AccessSessionResponse, type WorkbenchReading, type WorkbenchReadingRequest } from "./api";

// Browser history contains navigation references only. Every restoration fetches
// the reading and its data again under the current server authorization.
export interface WorkbenchNavigation {
  version: 1;
  owner: string;
  readingID: string;
  request: WorkbenchReadingRequest;
  dimension: "model" | "source_project" | "cost_project" | "application" | "account" | "purpose";
  mode: "top5" | "top10" | "all";
  resolution: "overview" | "hourly";
  activeBucket: string;
  selectedRef?: string;
  contributionPages: number;
  detailPages: number;
  related?: { executionID: string; readingID: string };
}
export const navigationOwner = (session: AccessSessionResponse) => JSON.stringify([session.session.subject_id, session.session.session_ref]);
export function usageDimension(reading: WorkbenchReading, actorKnownRequests = 0): "purpose" | "account" | "source_project" {
  if (reading.dimensions.includes("purpose")) return "purpose";
  if (actorKnownRequests > 0 && reading.dimensions.includes("account")) return "account";
  return "source_project";
}
export function restoredDimension(reading: WorkbenchReading, navigation: WorkbenchNavigation, usage: boolean, actorKnownRequests: number): WorkbenchNavigation["dimension"] {
  if (reading.dimensions.includes(navigation.dimension)) return navigation.dimension;
  // Older overview links selected purpose before checking the reading. Only an
  // unselected first page may fall back; selected evidence must never change.
  if (usage && navigation.dimension === "purpose" && !navigation.selectedRef && navigation.contributionPages === 1 && navigation.detailPages === 1) {
    return usageDimension(reading, actorKnownRequests);
  }
  throw new Error("navigation_access_changed");
}
export function fixedReadingRequest(reading: WorkbenchReading, request: WorkbenchReadingRequest): WorkbenchReadingRequest {
  return { contract_version: "xinsight.workbench.v1", page: "cost", metric_version: "xinsight.workbench.cost.v1",
    money_basis: reading.money_basis, currency: reading.currency,
    period: { start_inclusive: reading.period.start_inclusive, end_exclusive: reading.period.end_exclusive, display_timezone: reading.period.timezone },
    filters: { source_ids: [...request.filters.source_ids], source_projects: [...request.filters.source_projects], cost_projects: [...request.filters.cost_projects] } };
}
export function readWorkbenchNavigation(value: unknown): WorkbenchNavigation | undefined {
  if (value === undefined) return undefined;
  const v = value as WorkbenchNavigation;
  const strings = (items: unknown): items is string[] => Array.isArray(items) && items.every(item => typeof item === "string");
  if (!v || v.version !== 1 || typeof v.owner !== "string" || typeof v.readingID !== "string" || !/^xwr_[A-Za-z0-9_-]+$/.test(v.readingID) ||
      !["model", "source_project", "cost_project", "application", "account", "purpose"].includes(v.dimension) ||
      !["top5", "top10", "all"].includes(v.mode) || !["overview", "hourly"].includes(v.resolution) || typeof v.activeBucket !== "string" ||
      (v.related !== undefined && (!v.related || typeof v.related.executionID !== "string" || typeof v.related.readingID !== "string" || !/^xrt_[A-Za-z0-9_-]+$/.test(v.related.readingID))) ||
      (v.selectedRef !== undefined && typeof v.selectedRef !== "string") ||
      ![v.contributionPages, v.detailPages].every(n => Number.isSafeInteger(n) && n > 0) ||
      !v.request || v.request.contract_version !== "xinsight.workbench.v1" || v.request.page !== "cost" || v.request.metric_version !== "xinsight.workbench.cost.v1" ||
      !["gateway_charged", "calculated_reference"].includes(v.request.money_basis) || v.request.currency !== "CNY" ||
      !v.request.period || !Number.isFinite(Date.parse(v.request.period.start_inclusive ?? "")) || !Number.isFinite(Date.parse(v.request.period.end_exclusive ?? "")) || typeof v.request.period.display_timezone !== "string" ||
      !v.request.filters || !strings(v.request.filters.source_ids) || !strings(v.request.filters.source_projects) || !strings(v.request.filters.cost_projects)) throw new Error("navigation_access_changed");
  // Reconstruct the allowlist: even an altered history entry cannot propagate
  // a cached body, a role, or an authorization claim into later history entries.
  return { version: 1, owner: v.owner, readingID: v.readingID,
    request: { contract_version: "xinsight.workbench.v1", page: "cost", metric_version: "xinsight.workbench.cost.v1", money_basis: v.request.money_basis, currency: "CNY",
      period: { start_inclusive: v.request.period.start_inclusive, end_exclusive: v.request.period.end_exclusive, display_timezone: v.request.period.display_timezone },
      filters: { source_ids: [...v.request.filters.source_ids], source_projects: [...v.request.filters.source_projects], cost_projects: [...v.request.filters.cost_projects] } },
    dimension: v.dimension, mode: v.mode, resolution: v.resolution, activeBucket: v.activeBucket,
    related: v.related ? {executionID:v.related.executionID, readingID:v.related.readingID} : undefined,
    selectedRef: v.selectedRef, contributionPages: v.contributionPages, detailPages: v.detailPages };
}
export async function authorizedNavigationReading(value: WorkbenchNavigation, session: AccessSessionResponse, signal: AbortSignal) {
  if (value.owner !== navigationOwner(session)) throw new Error("navigation_access_changed");
  const [current, reading] = await Promise.all([accessAPI.session(signal), workbenchAPI.reading(value.readingID, signal)]);
  if (signal.aborted) throw new DOMException("Aborted", "AbortError");
  if (scopeSessionKey(current) !== scopeSessionKey(session) || reading.reading_id !== value.readingID ||
      reading.contract_version !== "xinsight.workbench.v1" || reading.metric_version !== "xinsight.workbench.cost.v1" ||
      !(Date.parse(reading.expires_at) > Date.now())) throw new Error("navigation_access_changed");
  return reading;
}


// Same history-reference mechanism as cost navigation; no request data or body.
export interface RuntimeNavigation {
  version: 1; owner: string; readingID: string; request: RuntimeReadingRequest;
  entry: EntryTime; selectedRef?: string; detailView: ExecutionView;
}
export function readRuntimeNavigation(value: unknown): RuntimeNavigation | undefined {
  if (value === undefined) return undefined;
  const v=value as RuntimeNavigation;
  const strings=(items:unknown):items is string[]=>Array.isArray(items)&&items.every(item=>typeof item==="string");
  if(!v||v.version!==1||typeof v.owner!=="string"||typeof v.readingID!=="string"||!/^xrt_[A-Za-z0-9_-]+$/.test(v.readingID)||
    !["known","unknown","all"].includes(v.entry)||!["story","graph","chronology","proof","systems","contents"].includes(v.detailView)||
    (v.selectedRef!==undefined&&(typeof v.selectedRef!=="string"||!/^xrr_[A-Za-z0-9_-]+$/.test(v.selectedRef)))||
    !v.request||v.request.contract_version!=="xinsight.runtime-reading.v1"||v.request.metric_version!=="xinsight.runtime.request-observations.v1"||
    !v.request.period||!Number.isFinite(Date.parse(v.request.period.start_inclusive??""))||!Number.isFinite(Date.parse(v.request.period.end_exclusive??""))||typeof v.request.period.display_timezone!=="string"||
    !v.request.filters||!strings(v.request.filters.source_ids)||!strings(v.request.filters.source_projects)||!strings(v.request.filters.cost_projects))throw new Error("navigation_access_changed");
  return {version:1,owner:v.owner,readingID:v.readingID,entry:v.entry,detailView:v.detailView,selectedRef:v.selectedRef,
    request:{contract_version:"xinsight.runtime-reading.v1",metric_version:"xinsight.runtime.request-observations.v1",
      period:{start_inclusive:v.request.period.start_inclusive,end_exclusive:v.request.period.end_exclusive,display_timezone:v.request.period.display_timezone},
      filters:{source_ids:[...v.request.filters.source_ids],source_projects:[...v.request.filters.source_projects],cost_projects:[...v.request.filters.cost_projects]}}};
}
