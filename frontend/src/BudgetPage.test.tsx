import { beforeEach as beforeLegacyLocale } from "vitest";
import { setLocale as setLegacyLocale } from "./i18n";
// These existing semantic assertions explicitly exercise the Chinese translation.
beforeLegacyLocale(() => setLegacyLocale("zh-CN"));
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { ApiRequestError, budgetAPI, type AccessSessionResponse } from "./api";
import { BudgetPage } from "./BudgetPage";
import { budgetPercentage, formatBudgetMoney, type BudgetItem, type BudgetReading } from "./budget";

const session: AccessSessionResponse = { session: { session_ref: "session", subject_id: "reader", role: "workbench_reader", permissions: ["workbench.summary.read", "workbench.details.read"], created_at: "", last_seen_at: "", idle_expires_at: "", absolute_expires_at: "" }, csrf_token: "csrf" };
const amount = (value: string) => ({ value_minor: value, currency: "CNY", scale: "1000000" });
const reading: BudgetReading = { reading_id: "xbd_aaaaaaaaaaaaaaaaaaaaaa", contract_version: "xinsight.budget-reading.v1", cutoff: "2026-10-02T00:01:00Z", expires_at: "2026-10-02T00:31:00Z", membership_ref: "sha256:manifest", budget_count: 1, calculated_budget_source_state: "formal_source_profile_missing", independent_invoice_source_state: "independent_source_not_bound" };
function item(): BudgetItem {
  return { ref: "00000000-0000-4000-8000-000000000001", budget_ref: "gateway-project:2", source_system_id: "source", source_instance: "synthetic", org_id: "1", project_id: "2", source_reading_id: "ur-reading-one", source_reading_cutoff: "2026-10-02T00:00:00Z", source_observed_at: "2026-10-02T00:00:01Z", received_at: "2026-10-02T00:00:02Z", definition_revision: "2", source_state: "active", runtime_effect: "report_only", detail_relationship: "request_membership_not_provided",
    definition: { state: "available", source_hash: "sha256:definition", body: { amount: amount("1000000000"), setting_state: "active", period_kind: "monthly" } },
    summary: { state: "available", body: { authority: "usage_finalization_journal.settled.charged_cost_micros", known_subtotal: amount("600000000"), period_start: "2026-10-01T00:00:00Z", period_end: "2026-11-01T00:00:00Z", amount_cutoff: "2026-10-02T00:00:00Z", coverage: { state: "complete_settled_as_of", reasons: [] }, comparison: { state: "comparable", remaining: amount("400000000"), utilization_numerator: "600000000", utilization_denominator: "1000000000", threshold_state: "below" }, unresolved_economics: { state: "none_observed" } } },
    alerts: { state: "available", body: { entries: [], has_more: false } }, alerts_page: "first_page" };
}
beforeEach(() => {
  window.history.replaceState({}, "", "/workbench/cost?view=budgets");
  vi.spyOn(budgetAPI, "createReading").mockResolvedValue(reading);
  vi.spyOn(budgetAPI, "reading").mockResolvedValue(reading);
  vi.spyOn(budgetAPI, "items").mockResolvedValue({ reading_id: reading.reading_id, items: [item()] });
  vi.spyOn(budgetAPI, "history").mockResolvedValue({ reading_id: reading.reading_id, items: [item()] });
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it("shows exact full-period budget and returns from source evidence with keyboard focus", async () => {
  const user = userEvent.setup();
  render(<BudgetPage session={session} navigate={vi.fn()} />);
  expect(await screen.findByText("1,000 CNY")).toBeTruthy();
  expect(screen.getByText("600 CNY")).toBeTruthy(); expect(screen.getByText("400 CNY")).toBeTruthy();
  expect(screen.getByText("60% · 未到预算")).toBeTruthy();
  expect(screen.getByText(/2026-10-01 00:00:00 UTC 至 2026-11-01/)).toBeTruthy();
  expect(new URLSearchParams(window.location.search).get("reading")).toBe(reading.reading_id);
  await user.click(screen.getByRole("button", { name: "查看依据" }));
  expect(await screen.findByRole("heading", { name: "预算依据：gateway-project:2" })).toBe(document.activeElement);
  expect(screen.getByText(/不能据两个总额推断缺失明细金额/)).toBeTruthy();
  expect(await screen.findByRole("heading", { name: "各次来源快照" })).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "← 返回预算比较" }));
  await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: "查看依据" })));
});

it("keeps large decimal amounts and sub-threshold comparisons exact", () => {
  expect(formatBudgetMoney(amount("9007199254740993123456789"))).toBe("9,007,199,254,740,993,123.456789 CNY");
  expect(formatBudgetMoney(amount("-200000000"))).toBe("−200 CNY");
  expect(formatBudgetMoney(amount("0"))).toBe("0 CNY");
  expect(formatBudgetMoney({ value_minor: "1", scale: "3", currency: "USD" })).toBe("1 / 3 USD");
  expect(formatBudgetMoney(undefined)).toBe("未提供");
  expect(budgetPercentage({ utilization_numerator: "999999999", utilization_denominator: "1000000000" })).toBe("约 99.99%");
});

it("does not reconstruct absent definition from a summary-only projection", async () => {
  const data = item(); data.definition = { state: "not_authorized" };
  data.summary.body!.comparison = { state: "definition_not_authorized" };
  vi.mocked(budgetAPI.items).mockResolvedValue({ reading_id: reading.reading_id, items: [data] });
  render(<BudgetPage session={session} navigate={vi.fn()} />);
  expect(await screen.findByText("600 CNY")).toBeTruthy();
  expect(screen.queryByText("1,000 CNY")).toBeNull(); expect(screen.queryByText("400 CNY")).toBeNull();
  expect(screen.getByText("没有预算定义查看权")).toBeTruthy();
});

it("clears shown amounts when history detects revoked access", async () => {
  const user = userEvent.setup();
  vi.mocked(budgetAPI.history).mockRejectedValue(new ApiRequestError(409));
  render(<BudgetPage session={session} navigate={vi.fn()} />);
  await screen.findByText("1,000 CNY");
  await user.click(screen.getByRole("button", { name: "查看依据" }));
  expect(await screen.findByRole("alert")).toBeTruthy();
  expect(screen.queryByText("1,000 CNY")).toBeNull();
  expect(screen.queryByRole("heading", { name: "预算依据：gateway-project:2" })).toBeNull();
});

it("labels continuation pages without treating their alerts as a complete reading", async () => {
  const data = item(); data.alerts_page = "continuation_page";
  vi.mocked(budgetAPI.items).mockResolvedValue({ reading_id: reading.reading_id, items: [data] });
  render(<BudgetPage session={session} navigate={vi.fn()} />);
  expect(await screen.findByText(/这是来源后续提醒页/)).toBeTruthy();
  expect(screen.getByText(/独立账单来源尚未绑定/)).toBeTruthy();
});

it("retries the existing reading after an item failure without creating another snapshot", async () => {
  vi.mocked(budgetAPI.items).mockRejectedValueOnce(new Error("temporary"));
  const user = userEvent.setup();
  render(<BudgetPage session={session} navigate={vi.fn()} />);
  await user.click(await screen.findByRole("button", { name: "重试" }));
  expect(await screen.findByText("1,000 CNY")).toBeTruthy();
  expect(budgetAPI.createReading).toHaveBeenCalledTimes(1);
  expect(budgetAPI.reading).not.toHaveBeenCalled();
});

it("rechecks current access when returning from evidence and clears revoked amounts", async () => {
  const user = userEvent.setup();
  render(<BudgetPage session={session} navigate={vi.fn()} />);
  await user.click(await screen.findByRole("button", { name: "查看依据" }));
  vi.mocked(budgetAPI.items).mockRejectedValueOnce(new ApiRequestError(409, { type: "about:blank", title: "Access changed", status: 409, code: "access_changed", retryable: false, correlation_id: "synthetic" }));
  await user.click(screen.getByRole("button", { name: "← 返回预算比较" }));
  expect(await screen.findByRole("alert")).toBeTruthy();
  expect(screen.queryByText("1,000 CNY")).toBeNull();
  expect(budgetAPI.createReading).toHaveBeenCalledTimes(1);
});

it("shows a current source outage separately from the saved whole-period snapshot", async () => {
  const saved = item(); saved.current_retrieval_state = "source_unavailable";
  vi.mocked(budgetAPI.items).mockResolvedValue({ reading_id: reading.reading_id, items: [saved] });
  render(<BudgetPage session={session} navigate={vi.fn()} />);
  expect(await screen.findByText(/来源目前不可达/)).toBeTruthy();
  expect(screen.getByText("600 CNY")).toBeTruthy();
  expect(screen.getByText(/不代表当前已用/)).toBeTruthy();
});
