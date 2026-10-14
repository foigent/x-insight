import type { ReviewCaptureDefaultView, ReviewCaptureSettings, UROperation, URPolicyConfig, URPolicyView } from "./api";

export interface PendingURWrite {
  operationRef: string;
  scopeKind?: "enterprise" | "project" | "user";
  orgID?: string;
  projectID: string;
  userID?: string;
  delegationID: string;
  delegationRevision: string;
  policy: Record<string, unknown>;
  jointXI?: { settings: ReviewCaptureSettings; expectedRevision: number };
}

export function newUROperationRef(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  return `xipw_${Array.from(bytes, value => value.toString(16).padStart(2, "0")).join("")}`;
}

export function currentURPolicyRelation(operation: UROperation | undefined, view: URPolicyView | undefined,
  xi: ReviewCaptureDefaultView | undefined): "unread" | "unconfirmed" | "changed" | "pending" | "current" {
  if (!operation || !view || !xi) return "unread";
  if (operation.state !== "effective" && operation.state !== "pending") return "unconfirmed";
  const scopeID = operation.scope_kind === "enterprise" ? view.org_id :
    operation.scope_kind === "user" ? `${view.project_id}:${view.user_id}` : view.project_id;
  if (operation.scope_id !== scopeID ||
    operation.source_instance !== view.source_instance || operation.org_id !== view.org_id) return "changed";
  if (xi.state === "unconfigured" || xi.policy.effective_revision !== operation.xi_desired_revision || xi.drift) return "changed";
  const result = operation.ur_result as { policy_ref?: string; desired_version?: string; effective_version?: string } | undefined;
  const layer = operation.scope_kind === "enterprise" ? view.enterprise :
    operation.scope_kind === "user" ? view.user : view.project;
  if (!result?.policy_ref || result.policy_ref !== layer?.policy_ref ||
    result.desired_version !== layer?.desired?.policy_version) return "changed";
  if (layer?.effective?.policy_version !== result.desired_version ||
    layer?.application_status !== "applied") return "pending";
  const before = parseVersionVector(operation.ur_expected_vector);
  const current = parseVersionVector(view.version_vector);
  if (!before || !current || !("enterprise" in before) || !("enterprise" in current)) return "unread";
  if (operation.scope_kind === "enterprise") {
    return String(current.enterprise) === result.desired_version ? "current" : "changed";
  }
  if (!("project" in before) || !("project" in current)) return "unread";
  if (JSON.stringify(before.enterprise) !== JSON.stringify(current.enterprise)) return "changed";
  if (operation.scope_kind === "user") {
    if (!("user" in current) || String(current.user) !== result.desired_version ||
      String(current.user_id) !== view.user_id || layer?.explicit !== true) return "unread";
    for (const key of ["project", "project_explicit", "project_quota_bytes"]) {
      if (JSON.stringify(before[key]) !== JSON.stringify(current[key])) return "changed";
    }
    return "current";
  }
  // An inherited project vector may be 1 while its own CAS version is 0.
  // Explicitness and quota are this write's fields, not parent drift.
  if (String(current.project) !== result.desired_version || current.project_explicit !== true ||
    view.project?.explicit !== true) return "unread";
  return "current";
}

function parseVersionVector(value: unknown): Record<string, unknown> | undefined {
  try {
    const parsed = typeof value === "string" ? JSON.parse(value) as unknown : value;
    return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed as Record<string, unknown> : undefined;
  } catch { return undefined; }
}

export function makeURProjectPut(view: URPolicyView, config: URPolicyConfig, executionMode: string,
  projectQuotaBytes?: number): Record<string, unknown> {
  return makeURScopedPut(view, "project", config, executionMode, projectQuotaBytes);
}

export function makeURScopedPut(view: URPolicyView, kind: "enterprise" | "project" | "user",
  config: URPolicyConfig, executionMode: string, projectQuotaBytes?: number): Record<string, unknown> {
  const layer = kind === "enterprise" ? view.enterprise : kind === "user" ? view.user : view.project;
  if (!layer?.policy_ref) throw new Error("当前 UR 策略缺少可写引用");
  const desired = kind === "user" ? { ...config, quota_bytes: undefined } : config;
  const policy: Record<string, unknown> = {
    schema_version: "unirouter.payload-policy.v2",
    expected_policy_ref: layer.policy_ref,
    expected_version: layer.explicit ? layer.desired?.policy_version ?? "0" : "0",
    expected_vector: view.version_vector,
    desired,
    execution_storage_mode: executionMode,
  };
  if (kind !== "user" && projectQuotaBytes !== undefined) policy.project_quota_bytes = projectQuotaBytes;
  return policy;
}
