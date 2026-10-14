import { defineConfig } from '@playwright/test';

// Listing tests is offline. Running them requires the admitted fixture URL.
if (!process.argv.includes('--list') && !process.env.XINSIGHT_P3_BROWSER_URL) {
  throw new Error('Admitted P3 browser URL required');
}
export default defineConfig({
  testDir: './e2e-p3', fullyParallel: false, workers: 1, retries: 0,
  timeout: 120000, reporter: './e2e-p3/safe-reporter.ts',
  outputDir: process.env.XINSIGHT_P3_BROWSER_PRIVATE_OUTPUT || '../output/p3-browser-private',
  use: {
    baseURL: process.env.XINSIGHT_P3_BROWSER_URL,
    browserName: 'chromium', launchOptions: { slowMo: 100 },
    ignoreHTTPSErrors: true, locale: 'en-US', timezoneId: 'UTC',
    viewport: { width: 1440, height: 1000 },
    screenshot: 'off', trace: 'off', video: 'off',
  },
});
