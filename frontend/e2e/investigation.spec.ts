import { expect, test, type Page } from "@playwright/test";
import { readFileSync } from "node:fs";

const releaseConfig = JSON.parse(readFileSync(new URL("../../release/d-validation-config.json", import.meta.url), "utf8"));

// Exercise a scrollbar that consumes layout width without changing OS settings.
test.beforeEach(async ({ page }, testInfo) => {
  if (testInfo.project.name.endsWith("reserved-scrollbar")) {
    await page.addInitScript(() => {
      document.addEventListener("DOMContentLoaded", () => {
        const style = document.createElement("style");
        style.textContent = "html { overflow-y: scroll; } ::-webkit-scrollbar { width: 15px; height: 15px; }";
        document.head.append(style);
      }, { once: true });
    });
  }
});

async function expectNoPageHorizontalScroll(page: Page) {
  const geometry = await page.evaluate(() => {
    const root = document.documentElement;
    const previous = { left: window.scrollX, top: window.scrollY };
    window.scrollTo({ left: 1_000_000, top: previous.top, behavior: "instant" });
    const result = {
      innerWidth: window.innerWidth,
      clientWidth: root.clientWidth,
      scrollWidth: root.scrollWidth,
      maxPageScrollLeft: document.scrollingElement?.scrollLeft ?? window.scrollX,
      overflowing: [...document.querySelectorAll("main *")].filter(element => element.getBoundingClientRect().right > root.clientWidth + 1).slice(0, 12).map(element => ({ tag: element.tagName, class: element.className, width: element.getBoundingClientRect().width })),
    };
    window.scrollTo({ ...previous, behavior: "instant" });
    return result;
  });
  if (test.info().project.name.endsWith("reserved-scrollbar")) {
    expect(geometry.innerWidth - geometry.clientWidth, JSON.stringify(geometry)).toBe(15);
  }
  expect(geometry.scrollWidth, JSON.stringify(geometry)).toBeLessThanOrEqual(geometry.clientWidth);
  expect(geometry.maxPageScrollLeft, JSON.stringify(geometry)).toBe(0);
}

const asOf = "2026-07-30T06:05:00.000Z";
const executionID = "xe1_demo_6d636f6c6c6973696f6e";
const requestID = "req-v2-fallback-demo";
const e2eOrigin = `http://127.0.0.1:${process.env.XINSIGHT_E2E_PORT ?? "4173"}`;

const freshness = {
  integration_mode: "synthetic_fixture",
  sync_state: "current",
  timeline_complete: true,
  source_terminal_at: "2026-07-30T06:00:01.000Z",
  source_generated_at: "2026-07-30T06:00:01.000Z",
  imported_at: "2026-07-30T06:00:02.000Z",
  search_visible_at: "2026-07-30T06:00:03.000Z",
  source_revision: "unirouter.execution-evidence.v2",
  imported_revision: "xinsight.import.v2",
  source_watermark: "closed:2026-07-30T06:00:01Z",
  import_watermark: "event:6",
};

const summary = {
  execution_id: executionID,
  request_id: requestID,
  source_system_id: "src-fixture-01",
  source_type: "unirouter",
  source_display_name: "UniRouter Execution Evidence V2 Demo",
  outcome: "success",
  first_effective_at: "2026-07-30T06:00:00.000Z",
  last_effective_at: "2026-07-30T06:00:01.000Z",
  event_count: 6,
  gap_count: 0,
  has_gaps: false,
  payload_capture_modes: ["metadata_only"],
  proof_class: "synthetic_fixture",
  assurance_mode: "synthetic_fixture",
  freshness,
};

const posture = {
  schema_version: "xinsight.posture.v1",
  as_of: asOf,
  from: "2026-07-29T06:05:00.000Z",
  to: asOf,
  risk: {
    open_total: 2,
    critical: 1,
    error: 0,
    warning: 1,
    info: 0,
    affected_executions: 1,
    unlinked: 1,
    items: [
      {
        id: `xal_${"1".repeat(64)}`,
        source_system_id: "src-fixture-01",
        source_alert_ref: "fixture:provider-timeout",
        title: "Provider fallback frequency crossed the review threshold",
        severity: "critical",
        state: "open",
        execution_id: executionID,
        observed_at: "2026-07-30T06:01:00.000Z",
      },
      {
        id: `xal_${"2".repeat(64)}`,
        source_system_id: "src-fixture-01",
        source_alert_ref: "fixture:coverage-drop",
        title: "Identity coverage is incomplete for the current window",
        severity: "warning",
        state: "acknowledged",
        observed_at: "2026-07-30T05:40:00.000Z",
      },
    ],
  },
  executions: {
    total: 3820,
    success: 3572,
    failure: 86,
    partial: 120,
    unknown: 42,
    with_gaps: 184,
    total_gaps: 219,
  },
  coverage: [
    { dimension: "temporal", label: "Direct time", available: true, numerator: 3740, denominator: 3820, ratio_decimal: "0.9791", unit: "executions", basis: "direct Event time", limitation: "ingestion time excluded" },
    { dimension: "identity", label: "Identity", available: true, numerator: 3418, denominator: 3820, ratio_decimal: "0.8948", unit: "executions", basis: "identity group observed", limitation: "presence is not completeness" },
    { dimension: "outcome", label: "Outcome", available: true, numerator: 3778, denominator: 3820, ratio_decimal: "0.9890", unit: "executions", basis: "deterministic outcome rollup", limitation: "terminal completeness remains separate" },
    { dimension: "causal", label: "Causal links", available: true, numerator: 2900, denominator: 3472, ratio_decimal: "0.8353", unit: "declared_parent_links", unresolved_count: 560, ambiguous_count: 12, basis: "source-declared parent links", limitation: "chronology never creates an edge" },
  ],
  sources: [
    { source_system_id: "src-fixture-01", source_type: "unirouter", source_display_name: "UniRouter", execution_total: 3820, failure_total: 86, executions_with_gaps: 184, last_effective_at: asOf },
  ],
  trend: {
    bucket_unit: "hour",
    items: Array.from({ length: 12 }, (_, index) => ({
      bucket_start: `2026-07-30T${String(index).padStart(2, "0")}:00:00.000Z`,
      execution_total: 240 + index * 8,
      failure_total: 2 + (index > 7 ? index - 6 : 0),
      partial_total: 4 + index,
      executions_with_gaps: 8 + index * 2,
      open_alerts: index > 8 ? index - 8 : 0,
    })),
    boundary: "fixed-window counts; absence is zero, not proof of health",
  },
  boundaries: [
    "aggregates_use_fixed_as_of_and_effective_time_window",
    "coverage_ratios_measure_observed_fields_or_declared_links_not_ground_truth_completeness",
  ],
};

test("posture homepage ranks evidence-backed risk and drills into the declared runtime path", async ({ page }) => {
  const requests: string[] = [];
  await installMockAPI(page, requests);
  await page.goto("/inbox");

  await expect(page.getByRole("heading", { name: "运行态势", level: 1 })).toBeVisible();
  await expect(page.getByText("2", { exact: true }).first()).toBeVisible();
  await expect(page.getByText("Provider fallback frequency crossed the review threshold")).toBeVisible();
  await expect(page.getByRole("heading", { name: "AI Runtime Path" })).toBeVisible();
  const runtimePath = page.getByRole("list", { name: "来源声明的执行路径" });
  await expect(runtimePath.getByRole("button")).toHaveCount(3);
  await expect(runtimePath.getByText("agent-orchestrator")).toBeVisible();
  await expect(runtimePath.getByText("policy-router")).toBeVisible();
  await expect(runtimePath.getByText("model-provider")).toBeVisible();
  await expect(page.getByText("LiteLLM（已配置）")).toBeVisible();
  await expect(page.getByText("四个口径不做无意义平均；每个比率都保留自己的分子、分母与限制。")).toBeVisible();
  await page.getByRole("tab", { name: "有 Gap 执行" }).click();
  await expect(page.getByText("当前窗口合计")).toBeVisible();
  expect(requests.some((request) => request.startsWith("/api/v1/posture?"))).toBe(true);
  await expectNoPageHorizontalScroll(page);

  await page.getByRole("button", { name: /从最高优先级证据开始/ }).click();
  await expect(page.getByRole("heading", { name: "执行路径调查", level: 1 })).toBeVisible();
});

test("customer demo reads recovered fallback as a source-backed trace without exposing raw content", async ({ page }) => {
  const requests: string[] = [];
  await installMockAPI(page, requests);
  await page.goto("/executions");

  const skipLink = page.getByRole("link", { name: "跳到主要内容" });
  await skipLink.focus();
  await expect(skipLink).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(page.locator("#main-content")).toBeFocused();
  await expect(page.getByRole("heading", { name: "执行调查", level: 1 })).toBeVisible();
  await expect(page.getByLabel("访问状态")).toContainText("本机开发入口");
  await expect(page.getByRole("button", { name: requestID })).toBeVisible();
  const captureCell = page.getByRole("cell", { name: /仅元数据/ });
  await expect(captureCell).toHaveCount(1);
  await captureCell.getByText("采集代码").click();
  await expect(captureCell.getByText("metadata_only", { exact: true })).toBeVisible();
  await expectNoPageHorizontalScroll(page);

  await page.getByLabel("Request ID 匹配方式").selectOption("prefix");
  await page.getByLabel("Request ID", { exact: true }).fill("req");
  await page.getByLabel("来源", { exact: true }).selectOption("src-fixture-01");
  await page.getByLabel("结果", { exact: true }).selectOption("success");
  await page.getByLabel("Capture gaps", { exact: true }).selectOption("false");
  const searchRequest = page.waitForRequest((request) => {
    const url = new URL(request.url());
    return url.pathname === "/api/v1/executions" && url.searchParams.get("request_id_prefix") === "req";
  });
  await page.getByRole("button", { name: "查询" }).click();
  const boundedURL = new URL((await searchRequest).url());
  expect(boundedURL.searchParams.get("source_system_id")).toBe("src-fixture-01");
  expect(boundedURL.searchParams.get("outcome")).toBe("success");
  expect(boundedURL.searchParams.get("has_gaps")).toBe("false");
  expect(boundedURL.searchParams.get("limit")).toBe("50");

  await page.getByRole("button", { name: requestID }).click();
  await expect(page.getByRole("complementary", { name: "所选运行摘要" })).toBeVisible();
  await page.getByRole("button", { name: "进入完整执行详情" }).click();
  await expect(page.getByRole("heading", { name: "执行路径调查", level: 1 })).toBeVisible();
  await expect(page.getByRole("heading", { name: "最终成功：来源声明路径显示备用路由完成恢复" })).toBeVisible();
  await expect(page.getByText("演示数据 · 合成资料")).toBeVisible();
  await expect(page.getByText("provider-a HTTP 502 → 重试 → provider-b 成功")).toBeVisible();
  const journey = page.getByRole("list", { name: "执行路径摘要" });
  await expect(journey.getByText("发起者")).toBeVisible();
  await expect(journey.getByText("发起 AI 执行请求")).toBeVisible();
  await expect(journey.getByText("经过系统")).toBeVisible();
  await expect(journey.getByText("只使用来源声明父子边")).toBeVisible();
  const crossExecutionJourney = page.getByRole("list", { name: "跨执行数据流路径" });
  await expect(page.getByRole("heading", { name: "跨系统执行路径" })).toBeVisible();
  await expect(crossExecutionJourney.getByRole("button")).toHaveCount(3);
  await expect(page.getByText("agent-orchestrator → policy-router → model-provider")).toBeVisible();
  await expect(page.getByText("3 次执行")).toBeVisible();
  await expect(
    page.locator(".investigation-metrics").getByText("CNY 0.000017 (17 micro-CNY)"),
  ).toBeVisible();
  await page.locator(".coverage-summary").click();
  await expect(page.locator(".coverage-strip").getByText("有证据", { exact: true })).toHaveCount(7);
  await expect(page.locator(".completeness-grid").getByText("有证据", { exact: true })).toHaveCount(4);

  const trace = page.getByRole("list", { name: "执行证据路径" });
  await expect(trace.locator("li")).toHaveCount(6);
  await expect(trace.locator("li").nth(0)).toContainText("选择主路由");
  await expect(trace.locator("li").nth(1)).toContainText("主提供方调用失败");
  await expect(trace.locator("li").nth(2)).toContainText("重试已安排");
  await expect(trace.locator("li").nth(3)).toContainText("切换备用路由");
  await expect(trace.locator("li").nth(4)).toContainText("计费结果已记录");
  await expect(trace.locator("li").nth(5)).toContainText("执行完成并记录用量");
  await expect(trace.getByText(/来源声明的父节点/)).toHaveCount(5);
  await expect(trace.getByText("HTTP 502")).toBeVisible();

  await trace.getByRole("button").nth(3).click();
  await page.getByRole("tab", { name: "证据" }).click();
  await expect(page.getByText("unirouter:trace:fallback")).toBeVisible();
  await expect(page.getByText("sha256:event-fallback")).toBeVisible();
  await expect(page.getByText("TOP-SECRET-HOSTILE-PROMPT")).toHaveCount(0);
  expect(requests.join("\n")).not.toContain("raw");

  const detailViews = page.getByRole("navigation", { name: "执行详情视图" });
  await detailViews.getByRole("button", { name: /^因果图/ }).click();
  if ((page.viewportSize()?.width ?? 1440) <= 760) {
    const relationshipNodes = page.getByRole("list", { name: "因果图节点与来源声明关系" }).getByRole("button");
    await expect(relationshipNodes).toHaveCount(6);
    await relationshipNodes.nth(1).focus();
    await expect(relationshipNodes.nth(1)).toBeFocused();
    await page.keyboard.press("Enter");
  } else {
    const graphNodes = page.getByLabel("来源声明的执行因果图").locator(".graph-node");
    await expect(graphNodes).toHaveCount(6);
    await graphNodes.nth(1).focus();
    await expect(graphNodes.nth(1)).toBeFocused();
    await page.keyboard.press("Enter");
  }
  await page.getByRole("tab", { name: "证据" }).click();
  await expect(page.getByText("sha256:event-provider-failed")).toBeVisible();

  await detailViews.getByRole("button", { name: /^时间线/ }).click();
  const timeline = page.getByRole("list", { name: "按时间升序排列的证据事件" });
  await expect(timeline.locator("li")).toHaveCount(6);
  await expect(timeline.getByText("声明父节点")).toHaveCount(5);

  await detailViews.getByRole("button", { name: /^证据依据/ }).click();
  await expect(page.getByRole("heading", { name: "证据依据" })).toBeVisible();
  await expect(
    page.getByText("unirouter.execution-evidence.bundle.v2", { exact: true }),
  ).toHaveCount(2);
  await expect(page.getByText("F2", { exact: true })).toBeVisible();
  await expect(page.getByText("sha256:event-provider-failed")).toBeVisible();
  await expectNoPageHorizontalScroll(page);
});

test("mobile investigation keeps navigation, journey, and causal relationships usable", async ({ page }) => {
  const failedAPIResponses: string[] = [];
  page.on("response", (response) => {
    const url = new URL(response.url());
    if (url.pathname.startsWith("/api/") && response.status() >= 400) {
      failedAPIResponses.push(`${response.status()} ${url.pathname}`);
    }
  });

  await page.setViewportSize({ width: Math.min(page.viewportSize()?.width ?? 390, 390), height: 844 });
  await installMockAPI(page, []);
  await page.goto("/executions");

  const productNavigation = page.getByRole("navigation", { name: "产品功能" });
  await expect(productNavigation).toBeHidden();
  await page.getByRole("button", { name: "菜单" }).click();
  await expect(productNavigation).toBeVisible();
  await expect(page.getByRole("link", { name: /来源与覆盖/ })).toBeVisible();

  await page.getByRole("link", { name: /运行与执行/ }).click();
  await expect(productNavigation).toBeHidden();
  await page.getByRole("button", { name: requestID }).click();
  await expect(page.getByRole("complementary", { name: "所选运行摘要" })).toBeVisible();
  await page.getByRole("button", { name: "进入完整执行详情" }).click();
  await expect(page.getByRole("heading", { name: "执行路径调查", level: 1 })).toBeVisible();
  await expect(page.getByRole("list", { name: "执行路径摘要" })).toBeVisible();
  await expectNoPageHorizontalScroll(page);

  const detailViews = page.getByRole("navigation", { name: "执行详情视图" });
  await detailViews.getByRole("button", { name: /^因果图/ }).click();
  await expect(page.getByLabel("来源声明的执行因果图")).toBeHidden();
  const relationshipList = page.getByRole("list", { name: "因果图节点与来源声明关系" });
  await expect(relationshipList).toBeVisible();
  await expect(relationshipList.locator("li")).toHaveCount(6);
  await expectNoPageHorizontalScroll(page);
  expect(failedAPIResponses).toEqual([]);
});

test("independent product login is visible and refuses credentials on the HTTP demo origin", async ({ page }) => {
  const requestedPaths: string[] = [];
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    requestedPaths.push(url.pathname);
    if (url.pathname === "/api/v1/access/session") {
      return route.fulfill({ status: 401, json: { code: "unauthenticated" } });
    }
    return route.fulfill({ status: 404, json: { code: "test_route_not_found" } });
  });

  await page.goto("/login");

  await expect(page.getByRole("heading", { name: "登录工作台", level: 1 })).toBeVisible();
  await expect(page.getByText("X-Insight 工作台")).toBeVisible();
  await expect(page.getByText("使用管理员提供的账号，查看获准访问的资料。")).toBeVisible();
  await expect(page.getByRole("alert")).toContainText("非 HTTPS 入口已禁用登录");
  await expect(page.getByLabel("用户名")).toBeDisabled();
  await expect(page.getByLabel("密码")).toBeDisabled();
  await expect(page.getByRole("button", { name: "登录" })).toBeDisabled();
  expect(requestedPaths).toContain("/api/v1/access/session");
  expect(requestedPaths).not.toContain("/api/v1/access/login");
  await expectNoPageHorizontalScroll(page);
});

test("product help opens the release-matched offline manual on the same origin", async ({ page }) => {
  const offOriginRequests: string[] = [];
  await installMockAPI(page, []);
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.origin !== e2eOrigin) {
      offOriginRequests.push(request.url());
    }
  });

  await page.goto("/executions");
  await expect(page.getByRole("complementary", { name: "主导航" })).toBeVisible();
  const mobileMenu = page.getByRole("button", { name: "菜单" });
  if (await mobileMenu.isVisible()) {
    await mobileMenu.click();
  }
  await page.getByRole("link", { name: /产品手册/ }).click();
  await expect(page).toHaveURL(`${e2eOrigin}/manual/zh-CN/`);
  await expect(page.getByRole("heading", { name: "简体中文产品手册", level: 1 })).toBeVisible();
  await expect(page.getByText(releaseConfig.manual.version, { exact: true }).first()).toBeVisible();
  expect(await page.locator('meta[name="xinsight-product-version"]').getAttribute("content")).toBe(releaseConfig.product_version);
  expect(await page.locator('meta[name="xinsight-manual-version"]').getAttribute("content")).toBe(releaseConfig.manual.version);
  await expectNoPageHorizontalScroll(page);
  expect(offOriginRequests).toEqual([]);
});

test("compare keeps Partial and Missing distinct without narrow-screen overflow", async ({ page }) => {
  await installMockAPI(page, []);
  await page.goto("/compare");

  const selectors = page.locator("fieldset");
  await selectors.nth(0).getByLabel("Execution ID").fill(executionID);
  await selectors.nth(0).getByLabel("as_of (UTC RFC3339)").fill(asOf);
  await selectors.nth(1).getByLabel("Execution ID").fill("xe1_demo_right");
  await selectors.nth(1).getByLabel("as_of (UTC RFC3339)").fill(asOf);
  const compareRequest = page.waitForRequest((request) => new URL(request.url()).pathname === "/api/v1/compare");
  await page.getByRole("button", { name: "比较固定快照" }).click();

  const requested = new URL((await compareRequest).url());
  expect(requested.searchParams.get("left")).toBe(executionID);
  expect(requested.searchParams.get("right")).toBe("xe1_demo_right");
  const table = page.getByRole("table", { name: "Left 与 Right 的同维度值" });
  await expect(table.getByText("Partial")).toBeVisible();
  await expect(table.getByText("Missing")).toBeVisible();
  await expect(table.getByText("unavailable")).toHaveCount(2);
  await expectNoPageHorizontalScroll(page);
});


test("running reading preserves page, preview, related request and copied-link recovery", async ({ page }) => {
  const requests: string[] = [];
  await installRunningAPI(page, requests);
  await page.goto(`/executions?request_id_prefix=req&as_of=${encodeURIComponent(asOf)}`);
  await page.getByRole("button", { name: "下一页", exact: true }).click();
  await page.getByRole("button", { name: requestID, exact: true }).click();
  const preview = page.getByRole("complementary", { name: "所选运行摘要" });
  await expect(preview.getByRole("heading", { name: requestID })).toBeVisible();
  const origin = page.url();
  await preview.getByRole("button", { name: "进入完整执行详情" }).click();
  await page.getByRole("button", { name: /^因果图/ }).click();
  const graphURL = page.url();
  await page.getByRole("button", { name: /^时间线/ }).click();
  await page.goBack();
  await expect(page.getByRole("button", { name: /^因果图/ })).toHaveAttribute("aria-current", "page");
  expect(page.url()).toBe(graphURL);
  const child = traceJourney().nodes.find(node => node.execution_id !== executionID)!;
  await page.getByRole("list", { name: "跨执行数据流路径" }).getByRole("button").nth(1).click();
  await expect(page).toHaveURL(new RegExp(`/executions/${child.execution_id}`));
  expect(new URL(page.url()).searchParams.get("view")).toBe("graph");
  expect(new URL(page.url()).searchParams.get("evidence")).toBe(child.event_id);
  await expect(page.getByRole("region", { name: "链接指定的证据事件" })).toBeVisible();
  await expect(page.getByText("过程投影尚未可用，通过现有事件接口独立定位；不补造过程节点或因果边。")).toBeVisible();
  await page.getByRole("button", { name: "← 返回运行与执行", exact: true }).click();
  await expect(preview.getByRole("heading", { name: requestID })).toBeVisible();
  expect(page.url()).toBe(origin);
  await expect(page.getByText("第 2 页", { exact: true })).toBeVisible();
  await expectNoPageHorizontalScroll(page);
  const copy = await page.context().newPage();
  try {
    await installRunningAPI(copy, []); await copy.goto(origin);
    await expect(copy.getByRole("button", { name: "上一页", exact: true })).toBeDisabled();
    await expect(copy.getByText("此链接没有可用的上一页游标记录；无法恢复页号或推算上一页。")).toBeVisible();
    await copy.getByRole("button", { name: "从此截止第一页重新读取" }).click();
    await expect(copy.getByRole("button", { name: "req-first-page" })).toBeVisible();
    expect(new URL(copy.url()).searchParams.get("as_of")).toBe(asOf);
    expect(new URL(copy.url()).searchParams.has("cursor")).toBe(false);
  } finally { await copy.close(); }
  await page.getByRole("button", { name: "关闭摘要" }).click();
  await expect(preview).toBeHidden();
  await page.goBack();
  await expect(preview).toBeVisible();
  await page.goForward();
  await expect(preview).toBeHidden();
  expect(requests.filter(path => path.includes("/executions?")).every(path => new URL(path, e2eOrigin).searchParams.get("as_of") === asOf)).toBe(true);
});

test("running reading layouts remain usable across desktop tablet and narrow themes", async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== "chromium-desktop", "One representative visual pass; functional coverage uses all projects.");
  await installRunningAPI(page, []);
  await page.goto(`/executions?as_of=${encodeURIComponent(asOf)}&cursor=page-two&selected=${executionID}`);
  const preview = page.getByRole("complementary", { name: "所选运行摘要" });
  await expect(preview.getByRole("heading", { name: requestID })).toBeVisible();
  const geometry: object[] = [];
  for (const [width, colorScheme] of [[1440, "light"], [1024, "light"], [360, "dark"], [320, "dark"]] as const) {
    await page.setViewportSize({ width, height: 1000 }); await page.emulateMedia({ colorScheme });
    await preview.scrollIntoViewIfNeeded(); await expectNoPageHorizontalScroll(page);
    const contrast = await expectReadingContrast(page, [[".search-panel label", ".search-panel"], [".execution-table td", ".execution-row-selected"]]);
    const boxes = { width, colorScheme, contrast, preview: await preview.boundingBox(), list: await page.locator(".execution-table").boundingBox() };
    geometry.push(boxes);
    if (width === 1440) expect(boxes.preview!.x).toBeGreaterThan(boxes.list!.x);
    else expect(boxes.preview!.y).toBeGreaterThan(boxes.list!.y);
    await page.screenshot({ path: testInfo.outputPath(`preview-${width}-${colorScheme}.png`), fullPage: true });
  }
  await preview.getByRole("button", { name: "进入完整执行详情" }).click();
  await page.getByRole("button", { name: /^因果图/ }).click();
  for (const [width, colorScheme] of [[1920, "dark"], [736, "light"]] as const) {
    await page.setViewportSize({ width, height: 1000 }); await page.emulateMedia({ colorScheme });
    await expectNoPageHorizontalScroll(page);
    await page.screenshot({ path: testInfo.outputPath(`detail-${width}-${colorScheme}.png`), fullPage: true });
    const contrast = await expectReadingContrast(page, [[".execution-facts dd", ".execution-facts>div"], ["#investigation-headline", ".investigation-overview"], [".unprovable-section h2", ".unprovable-section"], ...(width === 1920 ? [[".graph-node:not(.graph-node-selected) .graph-node-title", ".graph-node:not(.graph-node-selected) rect", "fill"]] : [])]);
    geometry.push({ width, colorScheme, detail: true, contrast });
  }
  await testInfo.attach("reading-layout-geometry", { body: JSON.stringify({ boundary: "frontend synthetic API contract; not native account integration", geometry }, null, 2), contentType: "application/json" });
});

async function expectReadingContrast(page: Page, pairs: string[][]) {
  const ratios = await page.evaluate(pairs => {
    const luminance = (color: string) => {
      const channels = color.match(/[\d.]+/g)!.slice(0, 3).map(Number).map(v => { const c = v / 255; return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4; });
      return channels[0] * 0.2126 + channels[1] * 0.7152 + channels[2] * 0.0722;
    };
    return pairs.map(([foreground, background, fill]) => {
      const a = luminance(getComputedStyle(document.querySelector(foreground)!).getPropertyValue(fill || "color"));
      const b = luminance(getComputedStyle(document.querySelector(background)!).getPropertyValue(fill || "background-color"));
      return { foreground, background, ratio: (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05) };
    });
  }, pairs);
  for (const value of ratios) expect(value.ratio, JSON.stringify(value)).toBeGreaterThanOrEqual(4.5);
  return ratios;
}

async function installRunningAPI(page: Page, requests: string[]) {
  await installMockAPI(page, requests);
  const child = traceJourney().nodes.find(node => node.execution_id !== executionID)!;
  await page.route("**/api/v1/executions**", async route => {
    const url = new URL(route.request().url()), path = url.pathname;
    if (path === "/api/v1/executions") {
      requests.push(path + url.search);
      const second = url.searchParams.has("cursor");
      return route.fulfill({ json: { items: [second ? summary : { ...summary, execution_id: "xe1_first_page", request_id: "req-first-page" }], page: { as_of: asOf, count: 1, has_more: !second, next_cursor: second ? undefined : "page-two" } } });
    }
    if (path === `/api/v1/executions/${child.execution_id}`) return route.fulfill({ json: { summary: { ...summary, execution_id: child.execution_id, request_id: child.request_id }, as_of: asOf } });
    if (path === `/api/v1/executions/${child.execution_id}/investigation`) return route.fulfill({ status: 503, json: { status: 503, code: "database_unavailable", title: "Synthetic partial projection failure" } });
    if (path === `/api/v1/executions/${child.execution_id}/events/${child.event_id}`) return route.fulfill({ json: { ...eventDetail(child.event_id), execution_id: child.execution_id } });
    if (path === `/api/v1/executions/${child.execution_id}/timeline`) return route.fulfill({ json: { execution_id: child.execution_id, items: [], total_event_count: 0, freshness, page: { as_of: asOf, count: 0, has_more: false } } });
    if (path === `/api/v1/executions/${child.execution_id}/gaps`) return route.fulfill({ json: { execution_id: child.execution_id, items: [], total_gap_count: 0, freshness, page: { as_of: asOf, count: 0, has_more: false } } });
    return route.fallback();
  });
}

test("read-only case list uses the full available width and renders all three records", async ({ page }, testInfo) => {
  const cases = ["Alpha", "Beta", "case-very-long-unbroken-identifier-0123456789abcdefghijklmnopqrstuvwxyz"].map((title, index) => ({ id: `xc_case_${index}`, title: `调查 ${title}`, description: `说明 ${title}`, status: "open", created_by_subject: "xs_0123456789abcdef", created_at: "2026-09-01T00:00:00Z", updated_at: "2026-09-02T00:00:00Z", version: 1 }));
  await installMockAPI(page, [], { readOnlyCases: cases });
  await page.goto("/cases");
  const list = page.getByRole("region", { name: "最近案件" });
  await expect(list.locator(".case-card")).toHaveCount(3);
  for (const item of cases) await expect(list.getByText(item.title)).toBeVisible();
  await expect(page.getByRole("heading", { name: "建立调查范围" })).toHaveCount(0);
  const geometry = await page.evaluate(() => {
    const layout = document.querySelector(".split-layout")!.getBoundingClientRect();
    const list = document.querySelector(".split-layout .panel")!.getBoundingClientRect();
    return { layout: layout.width, list: list.width };
  });
  expect(geometry.list).toBeGreaterThanOrEqual(geometry.layout - 2);
  const cardTextFits = await list.locator(".case-card").evaluateAll(cards => cards.every(card => {
    const cardRight = card.getBoundingClientRect().right;
    return [...card.querySelectorAll<HTMLElement>("strong, p")].every(node => node.scrollWidth <= node.clientWidth + 1 && node.getBoundingClientRect().right <= cardRight - 12);
  }));
  expect(cardTextFits).toBe(true);
  await expectNoPageHorizontalScroll(page);
  await page.screenshot({ path: testInfo.outputPath("read-only-cases.png"), fullPage: true });
});

for (const [name, amounts] of [["mixed", ["-900000", "0", "500000"]], ["all-negative", ["-900000", "-500000", "-100000"]]] as const) {
  test(`cost trend displays ${name} signed amounts from the mock contract`, async ({ page }, testInfo) => {
    await installMockAPI(page, [], { trendAmounts: [...amounts] });
    await installTrendMockAPI(page, [...amounts]);
    await page.goto("/workbench/cost");
    const plot = page.getByRole("region", { name: "趋势时间桶" });
    await expect(plot.locator("[data-value]")).toHaveCount(3);
    await expect(plot.locator("[data-direction=negative]")).toHaveCount(name === "mixed" ? 1 : 3);
    await expect(plot.locator(".series-trend-segment")).toHaveCount(2);
    await expect(plot.locator(".series-bars--signed")).toBeVisible();
    await expect(plot.locator(".series-trend-dot")).toHaveCount(3);
    await expect(plot.locator("i")).toHaveCount(0);
    await expect(page.getByText(name === "mixed" ? /1 个负金额时段，1 个正金额时段/ : /3 个负金额时段，0 个正金额时段/)).toBeVisible();
    if (name === "mixed") await expect(plot.locator("[data-direction=positive]")).toHaveCount(1);
    else await expect(plot.locator("[data-direction=positive]")).toHaveCount(0);
    const baseline = (await plot.locator(".series-zero-line").boundingBox())!.y;
    const negativeDots = await plot.locator("[data-direction=negative] .series-trend-dot").all();
    for (const dot of negativeDots) expect((await dot.boundingBox())!.y).toBeGreaterThan(baseline);
    if (name === "mixed") expect((await plot.locator("[data-direction=positive] .series-trend-dot").boundingBox())!.y).toBeLessThan(baseline);
    const endpointError = await plot.locator(".series-bars").evaluate(bars => {
      const columns = [...bars.querySelectorAll<HTMLElement>("[data-value]")];
      return [...bars.querySelectorAll<SVGLineElement>(".series-trend-segment")].flatMap(line => {
        const matrix = line.ownerSVGElement!.getScreenCTM()!;
        return [[line.x1.baseVal.value,line.y1.baseVal.value,Number(line.dataset.from)], [line.x2.baseVal.value,line.y2.baseVal.value,Number(line.dataset.to)]].map(([x,y,index]) => {
          const endpoint = new DOMPoint(x,y).matrixTransform(matrix);
          const dot = columns[index].querySelector(".series-trend-dot")!.getBoundingClientRect();
          return Math.hypot(endpoint.x-(dot.left+dot.width/2),endpoint.y-(dot.top+dot.height/2));
        });
      });
    });
    expect(Math.max(...endpointError)).toBeLessThan(1.5);
    await expectNoPageHorizontalScroll(page);
    await plot.scrollIntoViewIfNeeded();
    await page.screenshot({ path: testInfo.outputPath(`cost-trend-${name}.png`), fullPage: false });
  });
}

async function installTrendMockAPI(page: Page, amounts: string[], requests: string[] = []) {
  const id = "xwr_0123456789abcdefghijkl";
  const coverage = { status: "period_completeness_unknown", period_completeness: "unknown", projection_complete_for_members: true, known_items: 3, gaps: ["source_period_completeness_not_asserted"] };
  const period = { start_inclusive: "2026-09-01T00:00:00Z", end_exclusive: "2026-09-08T00:00:00Z", timezone: "Asia/Shanghai" };
  await page.route("**/api/v1/workbench/**", async route => {
    requests.push(new URL(route.request().url()).pathname);
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/v1/workbench/readings") return route.fulfill({ json: { reading_id: id, contract_version: "xinsight.workbench.v1", metric_version: "xinsight.workbench.cost.v1", period, cutoff: period.end_exclusive, expires_at: new Date(Date.now()+3600000).toISOString(), money_basis: "gateway_charged", money_title: "网关扣费", currency: "CNY", membership_ref: "members-mock", interpretation_revision: "interpret-mock", authorization_revision: "scope-mock", dimensions: ["model", "source_project", "cost_project"], coverage } });
    if (path.endsWith("/summary")) return route.fulfill({ json: { reading_id: id, money_basis: "gateway_charged", currency: "CNY", request_count: 3, request_total_complete: false, selected_amount_micros: "0", gateway_charged_micros: "0", calculated_reference_micros: "0", amount_complete: false, purpose_known_requests: 0, actor_known_requests: 0, coverage } });
    if (path.endsWith("/contributions")) return route.fulfill({ json: { reading_id: id, dimension: "source_project", mode: "top_n", rows: [], other_known_micros: "0", unassigned_micros: "0", unassigned_scope: "reading_constant", denominator_micros: "0", is_last_page: true, authorized_group_count: 0 } });
    if (path.endsWith("/series")) return route.fulfill({ json: { reading_id: id, metric: "amount", unit: "micros", bucket_seconds: 21600, points: amounts.map((known_value,index) => ({ start_inclusive: new Date(Date.parse(period.start_inclusive)+index*21600000).toISOString(), end_exclusive: new Date(Date.parse(period.start_inclusive)+(index+1)*21600000).toISOString(), known_value, complete: false })), coverage } });
    if (path.endsWith("/sources")) return route.fulfill({ json: { reading_id: id, items: [] } });
    if (path === "/api/v1/workbench/saved-filters") return route.fulfill({ json: { items: [] } });
    return route.fulfill({ status: 404, json: { code: "mock_route_not_found" } });
  });
}

async function installMockAPI(page: Page, requests: string[], options?: { readOnlyCases?: { id:string; title:string; description:string; status:string; created_by_subject:string; created_at:string; updated_at:string; version:number }[]; trendAmounts?: string[] }) {
  await page.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    requests.push(`${url.pathname}${url.search}`);
    const path = url.pathname;
    if (path === "/api/v1/access/session") {
      return route.fulfill({
        json: {
          session: {
            session_ref: "xr_0123456789abcdefghijkl",
            subject_id: "xs_0123456789abcdef",
            role: "investigator",
            permissions: options?.trendAmounts ? ["workbench.summary.read", "workbench.coverage.read", "workbench.details.read", "workbench.preferences.write"] : options?.readOnlyCases ? ["evidence.read", "investigation.scoped.read"] : ["evidence.read", "evidence.cite", "investigation.write"],
            created_at: "2026-07-29T08:00:00Z",
            last_seen_at: "2026-07-29T08:10:00Z",
            idle_expires_at: "2026-07-29T08:40:00Z",
            absolute_expires_at: "2026-07-29T16:00:00Z",
          },
          csrf_token: "csrf-e2e-memory-only",
        },
      });
    }
    if (path === "/api/v1/sources") {
      return route.fulfill({
        json: {
          items: [
            {
              source_system_id: "src-fixture-01",
              source_key: "fixture/unirouter",
              source_type: "unirouter",
              display_name: "UniRouter Execution Evidence V2 Demo",
              source_version: "unirouter.execution-evidence.bundle.v2",
            },
          ],
        },
      });
    }
    if (path === "/api/v1/posture") {
      return route.fulfill({ json: posture });
    }
    if (path === "/api/v1/connectors") {
      return route.fulfill({
        json: {
          items: [{
            connector_key: "fixture:connector:primary",
            source_system_id: "src-fixture-01",
            source_key: "fixture/unirouter",
            source_type: "unirouter",
            source_display_name: "UniRouter",
            source_status: "active",
            adapter_manifest_id: "manifest-fixture",
            adapter_id: "foigent.unirouter.completed-execution",
            adapter_version: "2.0.2",
            manifest_active: true,
            fidelity_level: "F2",
            ingestion_modes: ["contracted_pull"],
            payload_behavior: "metadata_only",
            idempotency_strategy: "source_event_identity",
            trust_state: "reported_current",
            checkpoint: {
              epoch: 2,
              cursor_key: "completed-execution",
              cursor_present: false,
              high_watermark_present: false,
              worker_status: "active",
              import_status: "idle",
              consecutive_failures: 0,
              pages_committed: 1,
              items_received: 3820,
              items_committed: 3820,
              duplicate_noop: 0,
              error_present: false,
            },
            delivery_state_boundary: "mutable_checkpoint_status_not_event_evidence",
          }, {
            connector_key: "fixture:connector:litellm",
            source_system_id: "src-fixture-02",
            source_key: "fixture/litellm",
            source_type: "litellm",
            source_display_name: "LiteLLM（已配置）",
            source_status: "inactive",
            adapter_manifest_id: "manifest-litellm-fixture",
            adapter_id: "foigent.litellm.openinference",
            adapter_version: "2.0.2",
            manifest_active: true,
            fidelity_level: "F1",
            ingestion_modes: ["contracted_pull"],
            payload_behavior: "metadata_only",
            idempotency_strategy: "source_event_identity",
            trust_state: "inactive",
            checkpoint: {
              epoch: 1,
              cursor_key: "openinference",
              cursor_present: false,
              high_watermark_present: false,
              worker_status: "inactive",
              import_status: "idle",
              consecutive_failures: 0,
              pages_committed: 0,
              items_received: 0,
              items_committed: 0,
              duplicate_noop: 0,
              error_present: false,
            },
            delivery_state_boundary: "mutable_checkpoint_status_not_event_evidence",
          }],
          installations: [],
          installation_control: {
            enabled: false,
            connector_type: "unirouter",
            event_scope: ["model.request.completed"],
            ordinary_sync_requires_assurance: false,
          },
          delivery_state_boundary: "checkpoint status is mutable and excluded from Evidence identity",
        },
      });
    }
    if (path === "/api/v1/saved-views") {
      return route.fulfill({ json: { items: [] } });
    }
    if (path === "/api/v1/cases") {
      return route.fulfill({ json: { items: options?.readOnlyCases ?? [] } });
    }
    if (path === "/api/v1/executions") {
      return route.fulfill({ json: { items: [summary], page: { as_of: asOf, count: 1, has_more: false } } });
    }
    if (path === "/api/v1/compare") {
      return route.fulfill({
        json: {
          left: { execution_id: executionID },
          right: { execution_id: "xe1_demo_right" },
          left_as_of: asOf,
          right_as_of: asOf,
          dimensions: [
            {
              dimension: "provider_path",
              left: { state: "partial", value: "router → provider-a", unit: "ordered_path" },
              right: { state: "observed", value: "router → provider-b", unit: "ordered_path" },
              relation: "unavailable",
            },
            {
              dimension: "cost_micros",
              left: { state: "missing" },
              right: { state: "observed", value: "17", unit: "micro-CNY" },
              relation: "unavailable",
            },
          ],
        },
      });
    }
    if (path.endsWith("/investigation")) {
      return route.fulfill({ json: investigationProjection() });
    }
    if (path.endsWith("/trace-journey")) {
      return route.fulfill({ json: traceJourney() });
    }
    if (path.endsWith("/timeline")) {
      return route.fulfill({
        json: {
          execution_id: executionID,
          total_event_count: 6,
          page: { as_of: asOf, count: 6, has_more: false },
          freshness,
          items: [
            timelineNode("event-route", "routing.route_selected", "routing", "2026-07-30T06:00:00.000Z", {
              field_provenance: semanticProvenance(),
            }),
            timelineNode("event-provider-failed", "provider.request_failed", "provider", "2026-07-30T06:00:00.100Z", {
              severity: "error",
              declared_parent_event_id: "event-route",
              declared_parent_resolved: true,
            }),
            timelineNode("event-retry", "retry.attempt.scheduled", "routing", "2026-07-30T06:00:00.200Z", {
              declared_parent_event_id: "event-provider-failed",
              declared_parent_resolved: true,
            }),
            timelineNode("event-fallback", "fallback.route.selected", "routing", "2026-07-30T06:00:00.300Z", {
              declared_parent_event_id: "event-retry",
              declared_parent_resolved: true,
            }),
            timelineNode("event-billing", "billing.settle.succeeded", "billing", "2026-07-30T06:00:00.900Z", {
              declared_parent_event_id: "event-fallback",
              declared_parent_resolved: true,
            }),
            timelineNode("event-complete", "gateway.call.completed", "logging", "2026-07-30T06:00:01.000Z", {
              declared_parent_event_id: "event-billing",
              declared_parent_resolved: true,
            }),
          ],
        },
      });
    }
    if (path.endsWith("/gaps")) {
      return route.fulfill({
        json: {
          execution_id: executionID,
          total_gap_count: 0,
          page: { as_of: asOf, count: 0, has_more: false },
          freshness,
          items: [],
        },
      });
    }
    if (path.includes("/events/")) {
      const eventID = decodeURIComponent(path.split("/events/")[1] ?? "event-route");
      return route.fulfill({ json: eventDetail(eventID) });
    }
    if (path === `/api/v1/executions/${executionID}`) {
      return route.fulfill({ json: { summary, as_of: asOf, trace_ids: ["trace-demo-001"] } });
    }
    return route.fulfill({ status: 404, json: { code: "test_route_not_found" } });
  });
}

function investigationProjection() {
  const stages = ["identity", "routing", "provider", "retry_fallback", "billing", "usage", "logging"];
  return {
    schema_version: "xinsight.investigation-projection.v1",
    execution: summary,
    as_of: asOf,
    presentation_mode: "causal_trace",
    summary_claims: [
      {
        code: "provider_failed_then_retry_scheduled",
        evidence_event_ids: ["event-provider-failed", "event-retry"],
        derivation_basis: "source_declared_parent_path",
      },
      {
        code: "primary_provider_failed_then_fallback_succeeded",
        evidence_event_ids: ["event-provider-failed", "event-retry", "event-fallback", "event-billing", "event-complete"],
        derivation_basis: "source_declared_parent_path",
      },
      {
        code: "execution_completed_successfully",
        evidence_event_ids: ["event-complete"],
        derivation_basis: "direct_terminal_outcome",
      },
    ],
    stage_coverage: stages.map((stage) => ({
      stage,
      status: "present",
      evidence_event_ids: [
        stage === "provider"
          ? "event-provider-failed"
          : stage === "retry_fallback"
            ? "event-retry"
            : stage === "billing"
              ? "event-billing"
              : stage === "usage" || stage === "logging"
              ? "event-complete"
              : "event-route",
      ],
    })),
    nodes: [
      projectionNode("event-route", "routing.route_selected", "route_selected", "routing", "2026-07-30T06:00:00.000Z", {
        provider: "provider-a",
        model: "model-a",
        status_code: 200,
        latency_ms: 2,
      }),
      projectionNode("event-provider-failed", "provider.request_failed", "provider_failed", "provider", "2026-07-30T06:00:00.100Z", {
        severity: "warning",
        declared_parent_event_id: "event-route",
        provider: "provider-a",
        model: "model-a",
        status_code: 502,
        latency_ms: 80,
        decision_result: "failed",
        outcome_status: "failure",
      }),
      projectionNode("event-retry", "retry.attempt.scheduled", "retry_scheduled", "routing", "2026-07-30T06:00:00.200Z", {
        declared_parent_event_id: "event-provider-failed",
        provider: "provider-b",
        model: "model-a",
        decision_result: "retry_scheduled",
      }),
      projectionNode("event-fallback", "fallback.route.selected", "fallback_selected", "routing", "2026-07-30T06:00:00.300Z", {
        declared_parent_event_id: "event-retry",
        provider: "provider-b",
        model: "model-a",
        decision_result: "fallback_selected",
      }),
      projectionNode("event-billing", "billing.settle.succeeded", "billing_recorded", "billing", "2026-07-30T06:00:00.900Z", {
        declared_parent_event_id: "event-fallback",
        provider: "provider-b",
        model: "model-a",
        status_code: 200,
        latency_ms: 140,
        decision_result: "allowed",
        cost_micros: "17",
      }),
      projectionNode("event-complete", "gateway.call.completed", "execution_completed", "logging", "2026-07-30T06:00:01.000Z", {
        declared_parent_event_id: "event-billing",
        provider: "provider-b",
        model: "model-a",
        status_code: 200,
        latency_ms: 140,
        total_tokens: 11,
        cost_micros: "17",
        outcome_status: "success",
      }),
    ],
    edges: [
      { from: "event-route", to: "event-provider-failed", edge_basis: "source_declared_parent_span" },
      { from: "event-provider-failed", to: "event-retry", edge_basis: "source_declared_parent_span" },
      { from: "event-retry", to: "event-fallback", edge_basis: "source_declared_parent_span" },
      { from: "event-fallback", to: "event-billing", edge_basis: "source_declared_parent_span" },
      { from: "event-billing", to: "event-complete", edge_basis: "source_declared_parent_span" },
    ],
    gaps: [],
    freshness,
    integrity: {
      source_profile_complete: true,
      import_complete: true,
      projection_complete: true,
      node_count: 6,
      total_event_count: 6,
      resolved_parent_count: 5,
      parent_eligible_count: 5,
      unresolved_parent_count: 0,
      causal_complete: true,
      truncated: false,
    },
  };
}

function traceJourney() {
  const traceID = "0123456789abcdef0123456789abcdef";
  const nodes = [
    traceJourneyNode(executionID, "11111111-1111-4111-8111-111111111111", "1111111111111111", "", "agent-orchestrator", "agent.plan", "2026-07-30T06:00:00.000Z"),
    traceJourneyNode("xe1_demo_policy_router", "22222222-2222-4222-8222-222222222222", "2222222222222222", "1111111111111111", "policy-router", "route.model_call", "2026-07-30T06:00:00.200Z"),
    traceJourneyNode("xe1_demo_model_provider", "33333333-3333-4333-8333-333333333333", "3333333333333333", "2222222222222222", "model-provider", "chat.completions", "2026-07-30T06:00:00.500Z"),
  ];
  return {
    schema_version: "xinsight.trace-journey.v1",
    available: true,
    anchor_execution_id: executionID,
    source_system_id: "11111111-1111-4111-8111-111111111111",
    trace_id: traceID,
    as_of: asOf,
    nodes,
    edges: [
      {
        from_event_id: nodes[0].event_id,
        to_event_id: nodes[1].event_id,
        edge_basis: "source_declared_parent_span_across_execution",
      },
      {
        from_event_id: nodes[1].event_id,
        to_event_id: nodes[2].event_id,
        edge_basis: "source_declared_parent_span_across_execution",
      },
    ],
    integrity: {
      node_count: 3,
      execution_count: 3,
      declared_parent_count: 2,
      resolved_parent_count: 2,
      unresolved_parent_count: 0,
      ambiguous_parent_count: 0,
      cycle_detected: false,
      truncated: false,
      source_timeline_complete: false,
      visible_parent_resolution_complete: true,
    },
    evidence_boundary: "source-declared trace/span correlation across the same source, organization and project",
  };
}

function traceJourneyNode(
  nodeExecutionID: string,
  eventID: string,
  spanID: string,
  parentSpanID: string,
  serviceName: string,
  action: string,
  effectiveAt: string,
) {
  return {
    execution_id: nodeExecutionID,
    event_id: eventID,
    request_id: `${traceIDForRequest()}-${spanID}`,
    source_type: "openinference",
    source_ref: `openinference:trace:${spanID}`,
    event_hash: `sha256:${spanID.repeat(4)}`,
    trace_id: "0123456789abcdef0123456789abcdef",
    span_id: spanID,
    ...(parentSpanID ? { parent_span_id: parentSpanID } : { actor_type: "human", actor_ref: "study-user" }),
    action,
    service_name: serviceName,
    provider: "provider-b",
    model: "model-b",
    outcome: "success",
    effective_at: effectiveAt,
  };
}

function traceIDForRequest() {
  return "trace-study";
}

function projectionNode(
  eventID: string,
  eventType: string,
  kind: string,
  stage: string,
  effectiveAt: string,
  overrides: Record<string, unknown> = {},
) {
  return {
    event_id: eventID,
    event_type: eventType,
    kind,
    stage,
    severity: "info",
    effective_at: effectiveAt,
    actor_type: "api_key",
    actor_ref: "api_key:1",
    org_id: "org:1",
    project_id: "project:1",
    requested_model: "model-a",
    citation: {
      evidence_event_id: eventID,
      source_ref: sourceRef(eventID),
      event_hash: `sha256:${eventID}`,
    },
    ...overrides,
  };
}

function timelineNode(
  eventID: string,
  eventType: string,
  stage: string,
  effectiveAt: string,
  overrides: Record<string, unknown> = {},
) {
  return {
    event_id: eventID,
    event_type: eventType,
    stage,
    severity: "info",
    effective_at: effectiveAt,
    ingested_at: "2026-07-30T06:00:02.000Z",
    source_ref: sourceRef(eventID),
    source_type: "unirouter",
    field_provenance: { correlation: "direct" },
    ...overrides,
  };
}

function semanticProvenance() {
  return {
    correlation: "direct",
    identity: "derived",
    capability: "declared",
    decision: "suppressed",
    usage: "unknown",
    outcome: "not_applicable",
    payload_policy: "gap",
  };
}

function sourceRef(eventID: string) {
  const refs: Record<string, string> = {
    "event-route": "unirouter:trace:route",
    "event-provider-failed": "unirouter:trace:failed",
    "event-retry": "unirouter:trace:retry",
    "event-fallback": "unirouter:trace:fallback",
    "event-billing": "unirouter:trace:billing",
    "event-complete": "unirouter:call:req-v2-fallback-demo",
  };
  return refs[eventID] ?? `unirouter:unknown:${eventID}`;
}

function eventDetail(eventID: string) {
  const facts: Record<string, {
    eventType: string;
    stage: string;
    provider: string;
    model: string;
    status: number;
    latency: number;
    outcome: "success" | "failure";
    sourceEventType: string;
  }> = {
    "event-route": {
      eventType: "routing.route_selected",
      stage: "routing",
      provider: "provider-a",
      model: "model-a",
      status: 200,
      latency: 2,
      outcome: "success",
      sourceEventType: "request_trace_event",
    },
    "event-provider-failed": {
      eventType: "provider.request_failed",
      stage: "provider",
      provider: "provider-a",
      model: "model-a",
      status: 502,
      latency: 80,
      outcome: "failure",
      sourceEventType: "request_trace_event",
    },
    "event-retry": {
      eventType: "retry.attempt.scheduled",
      stage: "routing",
      provider: "provider-b",
      model: "model-a",
      status: 200,
      latency: 0,
      outcome: "success",
      sourceEventType: "request_trace_event",
    },
    "event-fallback": {
      eventType: "fallback.route.selected",
      stage: "routing",
      provider: "provider-b",
      model: "model-a",
      status: 200,
      latency: 0,
      outcome: "success",
      sourceEventType: "request_trace_event",
    },
    "event-billing": {
      eventType: "billing.settle.succeeded",
      stage: "billing",
      provider: "provider-b",
      model: "model-a",
      status: 200,
      latency: 140,
      outcome: "success",
      sourceEventType: "request_trace_event",
    },
    "event-complete": {
      eventType: "gateway.call.completed",
      stage: "logging",
      provider: "provider-b",
      model: "model-a",
      status: 200,
      latency: 140,
      outcome: "success",
      sourceEventType: "call_log",
    },
  };
  const fact = facts[eventID] ?? facts["event-route"];
  return {
    execution_id: executionID,
    event_id: eventID,
    event_type: fact.eventType,
    stage: fact.stage,
    severity: fact.outcome === "failure" ? "error" : "info",
    capture_mode: "metadata_only",
    occurred_at: "2026-07-30T06:00:00.300Z",
    ingested_at: "2026-07-30T06:00:02.000Z",
    correlation: { request_id: requestID, trace_id: "trace-demo-001" },
    identity: {
      actor_type: "api_key",
      actor_ref: "api_key:1",
      org_id: "org:1",
      project_id: "project:1",
    },
    capability: {
      capability_type: "model_call",
      requested_model: "model-a",
      model: fact.model,
      provider: fact.provider,
      channel: fact.provider === "provider-a" ? "primary" : "fallback",
    },
    decision: {
      decision_kind: fact.stage === "provider" ? "provider" : "routing",
      decision_result: fact.outcome === "failure" ? "failed" : "selected",
      decision_reason: fact.outcome === "failure" ? "provider.unavailable" : "fixture.declared",
    },
    usage: {
      total_tokens: eventID === "event-complete" ? 11 : undefined,
      cost_micros: eventID === "event-complete" || eventID === "event-billing" ? "17" : undefined,
      currency: "CNY",
      amount_scale: 1000000,
      money_schema: "micro-cny.v1",
      latency_ms: fact.latency,
      usage_source: "source_declared",
    },
    outcome: {
      outcome_status: fact.outcome,
      status_code: fact.status,
      error_code: fact.outcome === "failure" ? "provider_unavailable" : undefined,
    },
    payload_policy: { payload_capture_mode: "metadata_only", redaction_state: "not_captured" },
    field_provenance: semanticProvenance(),
    source: {
      source_system_id: "src-fixture-01",
      adapter_manifest_id: "manifest-fixture-v1",
      schema_version: "unirouter.execution-evidence.bundle.v2",
      source_type: "unirouter",
      source_version: "unirouter.execution-evidence.bundle.v2",
      source_event_id: `source-${eventID}`,
      source_event_type: fact.sourceEventType,
      source_ref: sourceRef(eventID),
      event_hash: `sha256:${eventID}`,
      source_event_hash: `sha256:source-${eventID}`,
      fidelity_level: "F2",
    },
  };
}

// Existing Chinese scenarios select their UI language explicitly.
test.beforeEach(async ({ page }, info) => {
  if (!info.title.startsWith("English UI:")) await page.addInitScript(() => localStorage.setItem("xinsight.locale.v1", "zh-CN"));
});

test("English UI: manual follows the selected locale and is readable offline", async ({ page }, testInfo) => {
  await page.route("**/manual/en-US/manual.css", async route => {
    await new Promise(resolve => setTimeout(resolve, 300));
    await route.continue();
  });
  await installMockAPI(page, []);
  const offOrigin: string[] = [];
  page.on("request", request => { if (new URL(request.url()).origin !== e2eOrigin) offOrigin.push(request.url()); });
  await page.goto("/executions");
  const mobileMenu = page.getByRole("button", { name: "Menu", exact: true });
  if (await mobileMenu.isVisible()) await mobileMenu.click();
  await page.getByRole("link", { name: /Product manual/ }).click();
  await expect(page).toHaveURL(`${e2eOrigin}/manual/en-US/`);
  await page.waitForLoadState("load");
  await expect(page.getByRole("heading", { name: "Product manual", exact: true, level: 1 })).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("lang", "en-US");
  expect(await page.locator('meta[name="xinsight-product-version"]').getAttribute("content")).toBe(releaseConfig.product_version);
  await expect(page.getByRole("heading", { name: "Connect the first source", exact: true })).toBeVisible();
  await expectNoPageHorizontalScroll(page);
  expect(offOrigin).toEqual([]);
  if (testInfo.project.name === "chromium-320-reserved-scrollbar") {
    await page.screenshot({ path: testInfo.outputPath("english-manual.png") });
  }
});

test("English UI: defaults to English and keeps the search draft, selection, URL and requests while switching", async ({ page }) => {
  const requests: string[] = [];
  await installMockAPI(page, requests);
  await page.goto("/executions");
  await expect(page.locator("html")).toHaveAttribute("lang", "en-US");
  await expect(page.getByRole("combobox", { name: "Language" })).toHaveValue("en-US");
  await expect(page.getByRole("button", { name: requestID, exact: true })).toBeVisible();
  await page.locator("#request-search").fill("user 原文 draft");
  await page.getByRole("button", { name: requestID, exact: true }).click();
  await expect(page.getByRole("button", { name: requestID, exact: true })).toHaveAttribute("aria-pressed", "true");
  const url = page.url();
  const apiCount = requests.length;
  await page.getByRole("combobox", { name: "Language" }).selectOption("zh-CN");
  await expect(page.locator("#request-search")).toHaveValue("user 原文 draft");
  await expect(page.getByRole("button", { name: requestID, exact: true })).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("combobox", { name: "语言" }).selectOption("en-US");
  await expect(page.locator("#request-search")).toHaveValue("user 原文 draft");
  expect(page.url()).toBe(url);
  expect(requests.length).toBe(apiCount);
  await expectNoPageHorizontalScroll(page);
  await page.getByRole("combobox", { name: "Language" }).selectOption("zh-CN");
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("lang", "zh-CN");
  await page.getByRole("combobox", { name: "语言" }).selectOption("en-US");
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("lang", "en-US");
});

test("English UI: invalid or blocked preferences fall back to English and show an English HTTP login boundary", async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem("xinsight.locale.v1", "invalid"));
  await page.route("**/api/v1/**", route => route.fulfill({ status: 401, json: { code: "authentication_required", title: "Authentication required" } }));
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "Sign in to your workspace" })).toBeVisible();
  await expect(page.getByLabel("Username", { exact: true })).toBeDisabled();
  await expect(page.getByRole("alert")).toContainText("Sign-in is disabled over HTTP");
  await page.addInitScript(() => { Object.defineProperty(window, "localStorage", { get() { throw new DOMException("blocked", "SecurityError"); } }); });
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("lang", "en-US");
  await page.getByRole("combobox", { name: "Language" }).selectOption("zh-CN");
  await expect(page.getByRole("heading", { name: "登录工作台" })).toBeVisible();
  await expectNoPageHorizontalScroll(page);
});

test("English UI: known cost, zero and gaps remain the same frozen reading after a language switch", async ({ page }) => {
  const requests: string[] = [];
  await installMockAPI(page, requests, { trendAmounts: ["1000000", "0", "-500000"] });
  await installTrendMockAPI(page, ["1000000", "0", "-500000"], requests);
  await page.goto("/workbench/cost");
  await expect(page.locator(".workbench-page")).toHaveAttribute("aria-busy", "false");
  await expect(page.locator(".metric-card code")).toHaveText(/^xwr_[A-Za-z0-9_-]+$/);
  await expect(page.locator("[data-value]")).toHaveCount(3);
  await expect(page.locator('[data-value="0"]')).toHaveCount(1);
  const before = await page.locator(".metric-card").allTextContents();
  const identity = await page.locator(".metric-card code").textContent();
  const calls = requests.filter(path => path.includes("/readings")).length;
  await page.getByRole("combobox", { name: "Language" }).selectOption("zh-CN");
  await expect(page.locator(".metric-card code")).toHaveText(identity!);
  await page.getByRole("combobox", { name: "语言" }).selectOption("en-US");
  expect(await page.locator(".metric-card").allTextContents()).toEqual(before);
  expect(requests.filter(path => path.includes("/readings")).length).toBe(calls);
  await expectNoPageHorizontalScroll(page);
});
