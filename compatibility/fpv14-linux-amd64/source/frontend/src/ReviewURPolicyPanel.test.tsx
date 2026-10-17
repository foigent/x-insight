import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ReviewURPolicyPanel } from "./ReviewURPolicyPanel";
import { reviewPolicyAPI, type AccessSessionResponse, type ReviewCaptureDefaultView, type URPolicyView } from "./api";

afterEach(() => { cleanup(); vi.restoreAllMocks(); sessionStorage.clear(); });

it("reads applied enterprise disabled mode when the aggregate effective view is null", async () => {
  const session = { session: { subject_id: "xs_1111111111111111", session_ref: "session-disabled-enterprise" },
    csrf_token: "csrf" } as AccessSessionResponse;
  const xiDraft = { mode: "full_content" as const, retention_seconds: 86400,
    project_quota_bytes: 1048576, max_item_bytes: 1024 };
  const xi = { state: "effective", drift: false, policy: { desired_revision: 1, effective_revision: 1,
    desired: xiDraft, effective: xiDraft, requested_at: "2026-09-29T00:00:00Z" } } as ReviewCaptureDefaultView;
  const config = { mode: "disabled" as const, supported_protocols: ["responses"],
    observation_points: ["client_request"], allowed_purposes: [] };
  const view = { schema_version: "unirouter.payload-policy.v2", source_instance: "source-1", org_id: "7",
    project_id: "", enterprise: { policy_ref: "content-policy:7:enterprise", explicit: true,
      desired: { policy_version: "2", config }, effective: { policy_version: "2", config },
      application_status: "applied", desired_default_project_quota_bytes: 1048576 },
    effective: null, version_vector: "vector", storage_status: "ready", deployment_mode: "single",
    full_content_enabled: true } as URPolicyView;
  vi.spyOn(reviewPolicyAPI, "urScopes").mockResolvedValue({ state: "read", ur: { scopes: {
    schema_version: "unirouter.xinsight.policy-scopes.v1", installation_id: "xci_test",
    source_instance: "source-1", org_id: "7", projects: [], next_cursor: "",
    grants: [{ grant_id: "xipg_1", revision: "1", target_kind: "enterprise_default",
      coverage: "all_current_future", project_id: "", actions: ["read", "write"] }],
  } } });
  vi.spyOn(reviewPolicyAPI, "urEnterprise").mockResolvedValue({ state: "read", ur: { view } });
  const onXIDraft = vi.fn();
  const user = userEvent.setup();
  render(<ReviewURPolicyPanel session={session} xi={xi} xiDraft={xiDraft} onXIDraft={onXIDraft} />);
  await user.selectOptions(await screen.findByLabelText("授权委托"), "xipg_1");
  await user.click(screen.getByRole("button", { name: "读取当前来源策略" }));
  expect(await screen.findByText(/当前生效：disabled/)).toBeTruthy();
  expect(screen.getAllByRole("alert").some(item => item.textContent?.includes("当前正文开关"))).toBe(true);
  await user.click(screen.getByRole("button", { name: "按 UR 当前开关调整 XI 草稿" }));
  expect(onXIDraft).toHaveBeenCalledWith({ ...xiDraft, mode: "disabled" });
});

it("offers a source-backed first enterprise draft without saving it or copying XI limits", async () => {
  const session = { session: { subject_id: "xs_1111111111111111", session_ref: "session-bootstrap" }, csrf_token: "csrf" } as AccessSessionResponse;
  const suggested = { mode: "full_content" as const, retention_seconds: 7 * 86400,
    project_quota_bytes: 1024 * 1024 * 1024, max_item_bytes: 2 * 1024 * 1024 };
  const xi = { state: "unconfigured", suggested } as ReviewCaptureDefaultView;
  const view = { schema_version: "unirouter.payload-policy.v2", source_instance: "source-1", org_id: "7", project_id: "",
    enterprise: { policy_ref: "content-policy:7:enterprise", explicit: false }, version_vector: "{}",
    storage_status: "ready", deployment_mode: "single", full_content_enabled: true,
    limits: { max_saved_bytes: 16 * 1024 * 1024 },
    policy_capabilities: { supported_protocols: ["responses", "chat_completions"],
      observation_points: ["client_request", "client_response"] } } as URPolicyView;
  vi.spyOn(reviewPolicyAPI, "urScopes").mockResolvedValue({ state: "read", ur: { scopes: {
    schema_version: "unirouter.xinsight.policy-scopes.v1", installation_id: "xci_test", source_instance: "source-1", org_id: "7",
    grants: [{ grant_id: "xipg_1", revision: "1", target_kind: "enterprise_default", coverage: "all_current_future",
      project_id: "", actions: ["read", "write"] }], projects: [], next_cursor: "",
  } } });
  vi.spyOn(reviewPolicyAPI, "urEnterprise").mockResolvedValue({ state: "read", ur: { view } });
  const apply = vi.spyOn(reviewPolicyAPI, "urApplyEnterprise").mockRejectedValue(new TypeError("lost response"));
  const user = userEvent.setup();
  render(<ReviewURPolicyPanel session={session} xi={xi} xiDraft={suggested} />);
  await user.selectOptions(await screen.findByLabelText("授权委托"), "xipg_1");
  await user.click(screen.getByRole("button", { name: "读取当前来源策略" }));
  expect(await screen.findByText(/UR 企业策略尚未配置/)).toBeTruthy();
  expect(screen.getByLabelText("来源留存（小时）")).toHaveProperty("value", "720");
  expect(screen.getByLabelText("UR 每项目正文配额（GiB）")).toHaveProperty("value", "5");
  expect(screen.getByLabelText("来源单条上限（MiB）")).toHaveProperty("value", "16");
  expect(apply).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "联合保存 XI 期望与 UR 策略" }));
  await waitFor(() => expect(apply).toHaveBeenCalledTimes(1));
  const policy = apply.mock.calls[0][4];
  expect(policy.expected_version).toBe("0");
  expect(policy.project_quota_bytes).toBe(5 * 1024 * 1024 * 1024);
  expect(policy.desired).toMatchObject({ mode: "full_content", supported_protocols: ["responses", "chat_completions"],
    observation_points: ["client_request", "client_response"], retention_seconds: 30 * 86400,
    quota_bytes: 5 * 1024 * 1024 * 1024, max_saved_bytes: 16 * 1024 * 1024 });
  expect(apply.mock.calls[0][7]).toEqual({ settings: suggested, expectedRevision: 0 });
});

it("keeps the original operation ref after a lost browser response and reads it before another write", async () => {
  const session = { session: { subject_id: "xs_1111111111111111", session_ref: "session-1" }, csrf_token: "csrf" } as AccessSessionResponse;
  const xi = { state: "effective", drift: false, policy: { desired_revision: 1, effective_revision: 1,
    desired: { mode: "full_content", retention_seconds: 86400, project_quota_bytes: 1048576, max_item_bytes: 1024 },
    effective: { mode: "full_content", retention_seconds: 86400, project_quota_bytes: 1048576, max_item_bytes: 1024 },
    requested_at: "2026-09-29T00:00:00Z" } } as ReviewCaptureDefaultView;
  const config = { mode: "full_content" as const, supported_protocols: ["responses"], observation_points: ["client_request"],
    allowed_purposes: ["human_review"], retention_seconds: 86400, quota_bytes: 1048576, max_saved_bytes: 1024,
    on_limit: "record_capture_gap" };
  const view = { schema_version: "unirouter.payload-policy.v2", source_instance: "source-1", org_id: "7", project_id: "9",
    enterprise: { policy_ref: "content-policy:7:enterprise", explicit: true },
    project: { policy_ref: "content-policy:7:9", explicit: true, desired: { policy_version: "1", config },
      effective: { policy_version: "1", config }, application_status: "applied" },
    effective: { effective: { policy_version: "1", config }, project_quota_bytes: 1048576,
      execution_storage_mode: "temporary_allowed" }, version_vector: "vector", storage_status: "ready",
    deployment_mode: "single", full_content_enabled: true } as URPolicyView;
  vi.spyOn(reviewPolicyAPI, "urScopes").mockResolvedValue({ state: "read", ur: { scopes: {
    schema_version: "unirouter.xinsight.policy-scopes.v1", installation_id: "xci_test", source_instance: "source-1", org_id: "7",
    grants: [{ grant_id: "xipg_1", revision: "1", target_kind: "project_policy", coverage: "exact", project_id: "9", actions: ["read", "write"] }],
    projects: [{ project_id: "9", display_name: "test project" }], next_cursor: "",
  } } });
  vi.spyOn(reviewPolicyAPI, "urProject").mockResolvedValue({ state: "read", ur: { view } });
  const apply = vi.spyOn(reviewPolicyAPI, "urApply").mockRejectedValue(new TypeError("lost response"));
  const read = vi.spyOn(reviewPolicyAPI, "urOperation").mockRejectedValue(new Error("temporarily unavailable"));
  const user = userEvent.setup();
  render(<ReviewURPolicyPanel session={session} xi={xi} />);
  await user.selectOptions(await screen.findByLabelText("策略范围"), "project");
  await user.selectOptions(await screen.findByLabelText("来源项目"), "9");
  await user.selectOptions(screen.getByLabelText("授权委托"), "xipg_1");
  await user.click(screen.getByRole("button", { name: "读取当前来源策略" }));
  await user.click(await screen.findByRole("button", { name: "按当前版本提交来源策略" }));
  await waitFor(() => expect(apply).toHaveBeenCalledTimes(1));
  const ref = apply.mock.calls[0][3];
  expect(ref).toMatch(/^xipw_[a-f0-9]{32}$/);
  expect(JSON.stringify(sessionStorage)).not.toContain("csrf");
  await user.click(screen.getByRole("button", { name: "用原引用读回" }));
  await waitFor(() => expect(read).toHaveBeenCalledWith(ref));
  expect(apply).toHaveBeenCalledTimes(1);
  expect(screen.queryByRole("button", { name: "按当前版本提交来源策略" })).toBeNull();
});
