import { defineConfig } from "@playwright/test";

const portText = process.env.XINSIGHT_E2E_PORT ?? "4173";
if (!/^[1-9][0-9]{0,4}$/.test(portText) || Number(portText) > 65535) {
  throw new Error("XINSIGHT_E2E_PORT must be a canonical TCP port");
}
const port = Number(portText);
const origin = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: "./e2e",
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: 0,
  workers: 1,
  reporter: process.env.CI ? "github" : "list",
  outputDir: "test-results",
  use: {
    baseURL: origin,
    locale: "zh-CN",
    timezoneId: "Asia/Shanghai",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    {
      name: "chromium-desktop",
      use: { browserName: "chromium", viewport: { width: 1440, height: 1000 } },
    },
    {
      name: "chromium-laptop",
      use: { browserName: "chromium", viewport: { width: 1280, height: 800 } },
    },
    {
      name: "chromium-narrow",
      use: { browserName: "chromium", viewport: { width: 390, height: 844 } },
    },
    {
      name: "chromium-320-reserved-scrollbar",
      use: { browserName: "chromium", viewport: { width: 320, height: 740 }, launchOptions: { ignoreDefaultArgs: ["--hide-scrollbars"] } },
    },
  ],
  webServer: {
    command: `./node_modules/.bin/vite --host 127.0.0.1 --port ${port} --strictPort`,
    url: `${origin}/executions`,
    reuseExistingServer: false,
    timeout: 30_000,
  },
});
