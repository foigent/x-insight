import { describe, expect, it } from "vitest";
import { currentURPolicyRelation, makeURProjectPut, makeURScopedPut } from "./reviewURPolicy";
import type { ReviewCaptureDefaultView, UROperation, URPolicyView } from "./api";

const xi: ReviewCaptureDefaultView = { state: "effective", drift: false, policy: {
  desired: { mode: "full_content", retention_seconds: 86400, project_quota_bytes: 1024, max_item_bytes: 128 },
  desired_revision: 3, effective: { mode: "full_content", retention_seconds: 86400, project_quota_bytes: 1024, max_item_bytes: 128 },
  effective_revision: 3, requested_at: "2026-09-29T00:00:00Z",
} };
const config = { mode: "full_content" as const, supported_protocols: ["responses"],
  observation_points: ["client_request"], allowed_purposes: ["human_review"],
  retention_seconds: 86400, quota_bytes: 1024, max_saved_bytes: 128, on_limit: "record_capture_gap" };
const view: URPolicyView = { schema_version: "unirouter.payload-policy.v2", source_instance: "source-1", org_id: "7", project_id: "9",
  enterprise: { policy_ref: "content-policy:7:enterprise", explicit: true },
  project: { policy_ref: "content-policy:7:9", explicit: true, desired: { policy_version: "2", config },
    effective: { policy_version: "2", config }, application_status: "applied" },
  version_vector: '{"enterprise":1,"project":2,"project_explicit":true,"user":0,"user_id":0,"project_quota_bytes":2048}',
  storage_status: "ready", deployment_mode: "single", full_content_enabled: true };
const operation: UROperation = { installation_id: "xci_test", operation_ref: "xipw_" + "1".repeat(32), source_instance: "source-1",
  org_id: "7", scope_kind: "project", scope_id: "9", operator_subject: "xs_test", request_sha256: "sha256:" + "a".repeat(64),
  xi_desired_revision: 3, ur_expected_ref: "content-policy:7:9", ur_expected_version: 1,
  ur_expected_vector: { enterprise: 1, project: 1, project_explicit: true, user: 0, user_id: 0, project_quota_bytes: 1024 },
  state: "effective", ur_result: { policy_ref: "content-policy:7:9", desired_version: "2", effective_version: "2" } };

describe("current UR policy relation", () => {
  it("requires fresh project and XI effective readback, not only the historical committed operation", () => {
    expect(currentURPolicyRelation(operation, undefined, xi)).toBe("unread");
    expect(currentURPolicyRelation(operation, view, xi)).toBe("current");
    expect(currentURPolicyRelation({ ...operation, state: "pending", ur_result: {
      policy_ref: "content-policy:7:9", desired_version: "2", effective_version: "1" } }, view, xi)).toBe("current");
    expect(currentURPolicyRelation(operation, { ...view, project: { ...view.project!, desired: { ...view.project!.desired!, policy_version: "3" } } }, xi)).toBe("changed");
    expect(currentURPolicyRelation(operation, view, { ...xi, policy: { ...xi.policy, effective_revision: 4 } })).toBe("changed");
    expect(currentURPolicyRelation(operation, { ...view, project: { ...view.project!, application_status: "pending" } }, xi)).toBe("pending");
    expect(currentURPolicyRelation(operation, { ...view, version_vector: '{"enterprise":2,"project":2,"project_explicit":true,"user":0,"user_id":0,"project_quota_bytes":2048}' }, xi)).toBe("changed");
    expect(currentURPolicyRelation(operation, { ...view, version_vector: "unparseable" }, xi)).toBe("unread");
  });
  it("preserves the exact observed CAS identity in a project write", () => {
    expect(makeURProjectPut(view, config, "temporary_allowed", 2048)).toMatchObject({
      expected_policy_ref: "content-policy:7:9", expected_version: "2", expected_vector: view.version_vector,
      project_quota_bytes: 2048, desired: config,
    });
  });
  it("accepts the first explicit project write from an inherited vector", () => {
    const first = { ...operation, ur_expected_version: 0,
      ur_expected_vector: { enterprise: 1, project: 1, project_explicit: false, user: 0, user_id: 0,
        project_quota_bytes: 1024 },
      ur_result: { policy_ref: "content-policy:7:9", desired_version: "1", effective_version: "1" } };
    const current = { ...view, project: { ...view.project!, desired: { ...view.project!.desired!, policy_version: "1" },
      effective: { ...view.project!.effective!, policy_version: "1" } },
      version_vector: '{"enterprise":1,"project":1,"project_explicit":true,"user":0,"user_id":0,"project_quota_bytes":2048}' };
    expect(currentURPolicyRelation(first, current, xi)).toBe("current");
  });
  it("omits a private quota from user exceptions", () => {
    const userView = { ...view, user_id: "5", user: { policy_ref: "content-policy:7:9:5", explicit: false } };
    const put = makeURScopedPut(userView, "user", config, "temporary_allowed", 2048);
    expect(put).not.toHaveProperty("project_quota_bytes");
    expect(put.desired).toMatchObject({ mode: "full_content", quota_bytes: undefined });
    expect(JSON.stringify(put.desired)).not.toContain("quota_bytes");
  });
});
