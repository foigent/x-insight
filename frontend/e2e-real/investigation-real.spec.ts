import { expect, test } from "@playwright/test";

const requestID = process.env.XINSIGHT_E2E_REQUEST_ID;
if (!requestID) {
  throw new Error("XINSIGHT_E2E_REQUEST_ID is required for real-API browser verification");
}
const fromLocal = process.env.XINSIGHT_E2E_FROM_LOCAL;
const toLocal = process.env.XINSIGHT_E2E_TO_LOCAL;
const expectedEventCount = Number(process.env.XINSIGHT_E2E_EVENT_COUNT ?? "3");
const expectedTimelineLabel = process.env.XINSIGHT_E2E_TIMELINE_LABEL ?? "用量已记录";
const expectedGapType = process.env.XINSIGHT_E2E_GAP_TYPE ?? "not_available_from_source";
const expectedCandidate = process.env.XINSIGHT_E2E_GAP_CANDIDATE ?? "sampled_out";
const expectedProofLabel = process.env.XINSIGHT_E2E_PROOF_LABEL ?? "未分类证据";
const expectedSelectedEventType = process.env.XINSIGHT_E2E_SELECTED_EVENT_TYPE ?? "gap.capture.detected";
const expectedSelectedSemantic = process.env.XINSIGHT_E2E_SELECTED_SEMANTIC ?? "不适用";
const expectGapTimelineEvent = process.env.XINSIGHT_E2E_GAP_TIMELINE_EVENT !== "false";
const expectedMicroCNY = process.env.XINSIGHT_E2E_EXPECT_MICRO_CNY;

test("real API and PostgreSQL evidence complete search-to-gap investigation", async ({ page }) => {
  const consoleErrors: string[] = [];
  const apiRequests: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error") {
      consoleErrors.push(message.text());
    }
  });
  page.on("pageerror", (error) => consoleErrors.push(error.message));
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.pathname.startsWith("/api/v1/")) {
      apiRequests.push(`${url.pathname}${url.search}`);
    }
  });

  await page.goto("/executions");
  await expect(page.getByRole("heading", { name: "执行调查", level: 1 })).toBeVisible();
  await page.getByLabel("Request ID", { exact: true }).fill(requestID);
  if (fromLocal) {
    await page.getByLabel("开始时间（本地）").fill(fromLocal);
  }
  if (toLocal) {
    await page.getByLabel("结束时间（本地）").fill(toLocal);
  }
  await page.getByRole("button", { name: "查询" }).click();
  const result = page.getByRole("button", { name: requestID });
  await expect(result).toBeVisible();
  await result.click();

  await expect(page.getByRole("heading", { name: "执行路径调查", level: 1 })).toBeVisible();
  await expect(page).toHaveURL(/\/executions\/xe1_[^?]+\?as_of=/);
  await expect(page.getByText(expectedProofLabel).first()).toBeVisible();
  await expect(page.getByText("存在证据缺口").first()).toBeVisible();
  await expect(page.getByText(/终态快照|执行时间线|执行失败/).first()).toBeVisible();

  await page.getByRole("button", { name: "Chronology 原始时间顺序", exact: true }).click();
  const timeline = page.getByRole("list", { name: "按时间升序排列的 evidence events" });
  await expect(timeline.locator("li")).toHaveCount(expectedEventCount);
  await expect(timeline.getByText(expectedTimelineLabel)).toBeVisible();
  if (expectGapTimelineEvent) {
    await expect(timeline.getByText("检测到证据缺口")).toBeVisible();
  }
  await expect(page.getByText(expectedGapType).first()).toBeVisible();
  if (expectedCandidate) {
    await expect(page.getByText("候选原因（未确认）")).toBeVisible();
    await expect(page.getByText(expectedCandidate).first()).toBeVisible();
  }

  await page.getByRole("button", { name: "查看关联 evidence event" }).first().click();
  const nodePanel = page.getByRole("region", { name: "节点明细" });
  await expect(nodePanel.getByText(expectedSelectedEventType)).toBeVisible();
  await expect(nodePanel.getByText("Source ref", { exact: true })).toBeVisible();
  await expect(nodePanel.getByText("Event hash", { exact: true })).toBeVisible();
  await expect(nodePanel.getByText("Schema", { exact: true })).toBeVisible();
  await expect(nodePanel.getByText(expectedSelectedSemantic).first()).toBeVisible();
  await expect(nodePanel.getByText(/raw_source_excerpt/)).toBeVisible();
  await expect(page.getByText(/prompt|response/i).filter({ hasText: "TOP-SECRET" })).toHaveCount(0);

  if (expectedMicroCNY) {
    const usageNode = timeline.locator("li").filter({ hasText: "usage.tokens.recorded" });
    await usageNode.getByRole("button").click();
    await expect(nodePanel.getByText("精确成本（micro-CNY）")).toBeVisible();
    await expect(nodePanel.getByText(expectedMicroCNY, { exact: true })).toBeVisible();
  }

  expect(apiRequests.length).toBeGreaterThanOrEqual(6);
  expect(apiRequests.join("\n")).not.toContain("raw");
  expect(consoleErrors).toEqual([]);
});

// Existing Chinese scenarios select their UI language explicitly.
test.beforeEach(async ({ page }) => { await page.addInitScript(() => localStorage.setItem("xinsight.locale.v1", "zh-CN")); });
