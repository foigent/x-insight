import { beforeEach as beforeLegacyLocale } from "vitest";
import { setLocale as setLegacyLocale } from "./i18n";
// These existing semantic assertions explicitly exercise the Chinese translation.
beforeLegacyLocale(() => setLegacyLocale("zh-CN"));
import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ReviewPolicyPage } from "./ReviewPolicyPage";
import { investigationAPI, reviewPolicyAPI, type AccessSessionResponse,
  type ReviewCaptureDefaultView, type URPolicyView } from "./api";

vi.mock("./ReviewExceptionPanel", () => ({ ReviewExceptionPanel: () => null }));
vi.mock("./WorkInstallPanel", () => ({ WorkInstallPanel: () => null }));
afterEach(() => { cleanup(); vi.restoreAllMocks(); sessionStorage.clear(); });

const session = { session: { subject_id: "xs_1111111111111111", session_ref: "policy-session" },
  csrf_token: "csrf" } as AccessSessionResponse;
const settings = { mode: "disabled" as const, retention_seconds: 30 * 86400,
  project_quota_bytes: 5 * 1024 ** 3, max_item_bytes: 32 * 1024 ** 2 };
const xi = { state: "effective", drift: false, policy: { desired: settings, effective: settings,
  desired_revision: 1, effective_revision: 1, requested_at: "2026-09-30T00:00:00Z" } } as ReviewCaptureDefaultView;
const connectors = (paired: boolean): Awaited<ReturnType<typeof investigationAPI.listConnectors>> => ({
  items: [], delivery_state_boundary: "synthetic-test", installation_control: { enabled: true,
    connector_type: "unirouter", event_scope: [], ordinary_sync_requires_assurance: false },
  installations: paired ? [{ schema_version: "xinsight.connector-installation.v1", installation_id: "xci_test",
    deployment_id: "xid_test", connector_type: "unirouter", requested_event_scope: [],
    installation_acknowledgement_sha256: "sha256:synthetic", status: "connected", desired_state: "active",
    observed_state: "connected", created_at: "2026-09-30T00:00:00Z", updated_at: "2026-09-30T00:00:00Z",
    version: 1 }] : [] });
function mockPolicy(paired: boolean | "unknown") {
  vi.spyOn(reviewPolicyAPI, "current").mockResolvedValue(xi);
  vi.spyOn(reviewPolicyAPI, "projects").mockResolvedValue({ items: [], projects: [] });
  vi.spyOn(investigationAPI, "listConnectors").mockImplementation(async () => {
    if (paired === "unknown") throw new Error("connector inventory unavailable");
    return connectors(paired);
  });
  vi.spyOn(reviewPolicyAPI, "urScopes").mockImplementation(async () => {
    if (paired !== true) throw new Error("UR policy scope unavailable");
    return { state: "read", ur: { scopes: { schema_version: "unirouter.xinsight.policy-scopes.v1",
      installation_id: "xci_test", source_instance: "ur-test", org_id: "7",
      grants: [{ grant_id: "xipg_1", revision: "1", target_kind: "enterprise_default",
        coverage: "all_current_future", project_id: "", actions: ["read", "write"] }],
      projects: [], next_cursor: "" } } };
  });
  return vi.spyOn(reviewPolicyAPI, "apply").mockResolvedValue(xi);
}

it("routes a paired mode change to enterprise joint save and preserves the XI numeric draft", async () => {
  const saveXI = mockPolicy(true);
  const source = { mode: "metadata_only" as const, supported_protocols: ["responses"],
    observation_points: ["client_request"], allowed_purposes: [] };
  const view = { schema_version: "unirouter.payload-policy.v2", source_instance: "ur-test", org_id: "7", project_id: "",
    enterprise: { policy_ref: "content-policy:7:enterprise", explicit: true, desired: { policy_version: "1", config: source },
      effective: { policy_version: "1", config: source }, application_status: "applied",
      desired_default_project_quota_bytes: 5 * 1024 ** 3 },
    effective: { effective: { policy_version: "1", config: source }, project_quota_bytes: 5 * 1024 ** 3,
      execution_storage_mode: "temporary_allowed" }, version_vector: "{}", storage_status: "ready",
    deployment_mode: "single", full_content_enabled: true } as URPolicyView;
  vi.spyOn(reviewPolicyAPI, "urEnterprise").mockResolvedValue({ state: "read", ur: { view } });
  const user = userEvent.setup();
  render(<ReviewPolicyPage session={session} />);
  await user.selectOptions(await screen.findByLabelText("正文复制模式"), "full_content");
  await user.clear(screen.getByLabelText("项目容量（GiB）"));
  await user.type(screen.getByLabelText("项目容量（GiB）"), "3");
  await user.click(await screen.findByRole("button", { name: "前往企业联合保存" }));
  expect(saveXI).not.toHaveBeenCalled();
  expect(screen.getByLabelText("项目容量（GiB）")).toHaveProperty("value", "3");
  expect(screen.getByLabelText("策略范围")).toHaveProperty("value", "enterprise");
  await user.selectOptions(screen.getByLabelText("授权委托"), "xipg_1");
  await user.click(screen.getByRole("button", { name: "读取当前来源策略" }));
  expect(await screen.findByRole("button", { name: "联合保存 XI 期望与 UR 策略" })).toBeTruthy();
  expect(saveXI).not.toHaveBeenCalled();
});

it("aligns a paired disable draft and blocks a later UR full-content mismatch", async () => {
  const enabledSettings = { ...settings, mode: "full_content" as const };
  vi.spyOn(reviewPolicyAPI, "current").mockResolvedValue({ state: "effective", drift: false,
    policy: { desired: enabledSettings, effective: enabledSettings,
      desired_revision: 1, effective_revision: 1, requested_at: "2026-09-30T00:00:00Z" } });
  vi.spyOn(reviewPolicyAPI, "projects").mockResolvedValue({ items: [], projects: [] });
  vi.spyOn(investigationAPI, "listConnectors").mockResolvedValue(connectors(true));
  vi.spyOn(reviewPolicyAPI, "urScopes").mockResolvedValue({ state: "read", ur: { scopes: {
    schema_version: "unirouter.xinsight.policy-scopes.v1", installation_id: "xci_test",
    source_instance: "ur-test", org_id: "7", projects: [], next_cursor: "",
    grants: [{ grant_id: "xipg_1", revision: "1", target_kind: "enterprise_default",
      coverage: "all_current_future", project_id: "", actions: ["read", "write"] }] } } });
  const source = { mode: "full_content" as const, supported_protocols: ["responses"],
    observation_points: ["client_request"], allowed_purposes: ["human_review"],
    retention_seconds: 7 * 86400, quota_bytes: 2 * 1024 ** 3,
    max_saved_bytes: 4 * 1024, on_limit: "record_capture_gap" as const };
  vi.spyOn(reviewPolicyAPI, "urEnterprise").mockResolvedValue({ state: "read", ur: { view: {
    schema_version: "unirouter.payload-policy.v2", source_instance: "ur-test", org_id: "7", project_id: "",
    enterprise: { policy_ref: "content-policy:7:enterprise", explicit: true,
      desired: { policy_version: "1", config: source }, effective: { policy_version: "1", config: source },
      application_status: "applied", desired_default_project_quota_bytes: 2 * 1024 ** 3 },
    effective: { effective: { policy_version: "1", config: source }, project_quota_bytes: 2 * 1024 ** 3,
      execution_storage_mode: "temporary_allowed" }, version_vector: "{}", storage_status: "ready",
    deployment_mode: "single", full_content_enabled: true } as URPolicyView } });
  const saveXI = vi.spyOn(reviewPolicyAPI, "apply").mockResolvedValue(xi);
  const saveUR = vi.spyOn(reviewPolicyAPI, "urApplyEnterprise");
  const user = userEvent.setup();
  render(<ReviewPolicyPage session={session} />);
  await user.selectOptions(await screen.findByLabelText("正文复制模式"), "disabled");
  await user.click(await screen.findByRole("button", { name: "前往企业联合保存" }));
  await user.selectOptions(screen.getByLabelText("授权委托"), "xipg_1");
  await user.click(screen.getByRole("button", { name: "读取当前来源策略" }));
  expect(await screen.findByLabelText("来源正文模式")).toHaveProperty("value", "metadata_only");
  expect(saveXI).not.toHaveBeenCalled();
  await user.selectOptions(screen.getByLabelText("来源正文模式"), "full_content");
  expect(screen.getAllByRole("alert").some(item => item.textContent?.includes("不能联合保存"))).toBe(true);
  await user.click(screen.getByRole("button", { name: "联合保存 XI 期望与 UR 策略" }));
  expect(saveUR).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "按本次 XI 草稿开关调整 UR 草稿" }));
  expect(screen.getByLabelText("来源正文模式")).toHaveProperty("value", "metadata_only");
});

it("keeps mode changes blocked when pairing cannot be read, without losing the draft", async () => {
  const saveXI = mockPolicy("unknown");
  const user = userEvent.setup();
  render(<ReviewPolicyPage session={session} />);
  await user.selectOptions(await screen.findByLabelText("正文复制模式"), "full_content");
  expect(screen.getByRole("button", { name: "保存 XI 正文模式" })).toHaveProperty("disabled", true);
  await user.click(screen.getByRole("button", { name: "重新读取配对状态" }));
  expect(screen.getByLabelText("正文复制模式")).toHaveProperty("value", "full_content");
  expect(saveXI).not.toHaveBeenCalled();
});

it("saves XI numeric settings independently with a paired UR", async () => {
  const saveXI = mockPolicy(true);
  const user = userEvent.setup();
  render(<ReviewPolicyPage session={session} />);
  await user.clear(await screen.findByLabelText("留存时间"));
  await user.type(screen.getByLabelText("留存时间"), "20");
  await user.click(screen.getByRole("button", { name: "仅保存 XI 留存设置" }));
  await waitFor(() => expect(saveXI).toHaveBeenCalledTimes(1));
  expect(saveXI.mock.calls[0][0]).toMatchObject({ mode: "disabled", retention_seconds: 20 * 86400 });
});

it("does not treat a pending mode change as a numeric-only XI save", async () => {
  const saveXI = mockPolicy(true);
  const enabledSettings = { ...settings, mode: "full_content" as const };
  vi.spyOn(reviewPolicyAPI, "current").mockResolvedValue({ state: "pending", drift: true,
    policy: { desired: settings, effective: enabledSettings,
      desired_revision: 2, effective_revision: 1, requested_at: "2026-09-30T00:00:00Z" } });
  const user = userEvent.setup();
  render(<ReviewPolicyPage session={session} />);
  await user.clear(await screen.findByLabelText("留存时间"));
  await user.type(screen.getByLabelText("留存时间"), "20");
  await user.click(await screen.findByRole("button", { name: "前往企业联合保存" }));
  expect(saveXI).not.toHaveBeenCalled();
  expect(screen.getByLabelText("留存时间")).toHaveProperty("value", "20");
});

it("allows an XI-only mode save when a complete connector inventory proves no pairing", async () => {
  const saveXI = mockPolicy(false);
  const user = userEvent.setup();
  render(<ReviewPolicyPage session={session} />);
  await user.selectOptions(await screen.findByLabelText("正文复制模式"), "full_content");
  await user.click(await screen.findByRole("button", { name: "保存 XI 正文模式" }));
  await waitFor(() => expect(saveXI).toHaveBeenCalledTimes(1));
  expect(saveXI.mock.calls[0][0].mode).toBe("full_content");
});

for (const scopeState of ["denied", "unknown"] as const) {
  it(`blocks a paired body-mode change before any write when enterprise scope is ${scopeState}`, async () => {
    const saveXI = mockPolicy(true);
    vi.mocked(reviewPolicyAPI.urScopes).mockImplementation(async () => {
      if (scopeState === "unknown") throw new Error("scope unavailable");
      return { state: "read", ur: { scopes: { schema_version: "unirouter.xinsight.policy-scopes.v1",
        installation_id: "xci_test", source_instance: "ur-test", org_id: "7", grants: [], projects: [], next_cursor: "" } } };
    });
    const saveUR = vi.spyOn(reviewPolicyAPI, "urApplyEnterprise");
    const user = userEvent.setup();
    render(<ReviewPolicyPage session={session} />);
    await user.selectOptions(await screen.findByLabelText("正文复制模式"), "full_content");
    const button = await screen.findByRole("button", { name: "前往企业联合保存" });
    expect(button).toHaveProperty("disabled", true);
    await user.click(button);
    expect(saveXI).not.toHaveBeenCalled();
    expect(saveUR).not.toHaveBeenCalled();
    expect(screen.getByLabelText("正文复制模式")).toHaveProperty("value", "full_content");
    expect(screen.getByText(scopeState === "denied"
      ? "当前连接没有企业正文联管写权限，请由 UniRouter 组织管理员授予企业默认策略的读取与修改权限。供数与其他已有授权独立运行。"
      : "正在核对企业正文联管权限；总开关在权限读回前不可提交。")).toBeTruthy();
  });
}
