import { defineConfig } from "@playwright/test";
import { isAbsolute } from "node:path";

const baseURL = process.env.XINSIGHT_E2E_BASE_URL;
if (!baseURL) {
  throw new Error("XINSIGHT_E2E_BASE_URL is required for real-API browser verification");
}
const storageState = process.env.XINSIGHT_E2E_STORAGE_STATE;
if (!storageState || !isAbsolute(storageState)) {
  throw new Error(
    "XINSIGHT_E2E_STORAGE_STATE must be an absolute path to a protected authenticated Playwright storage-state file",
  );
}
const ignoreHTTPSErrorsText = process.env.XINSIGHT_E2E_IGNORE_HTTPS_ERRORS ?? "0";
if (ignoreHTTPSErrorsText !== "0" && ignoreHTTPSErrorsText !== "1") {
  throw new Error("XINSIGHT_E2E_IGNORE_HTTPS_ERRORS must be 0 or 1");
}

export default defineConfig({
  testDir: "./e2e-real",
  fullyParallel: false,
  retries: 0,
  workers: 1,
  reporter: "list",
  outputDir: "test-results-real",
  use: {
    baseURL,
    browserName: "chromium",
    storageState,
    ignoreHTTPSErrors: ignoreHTTPSErrorsText === "1",
    viewport: { width: 1440, height: 1000 },
    locale: "zh-CN",
    timezoneId: "Asia/Shanghai",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
});
