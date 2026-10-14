import { defineConfig } from '@playwright/test';
if (!process.env.XINSIGHT_E2E_BASE_URL) throw new Error('Isolated production harness URL required');
export default defineConfig({
 testDir:'./e2e-workbench',fullyParallel:false,workers:1,retries:0,timeout:60000,
 reporter:'list',outputDir:`${process.env.XINSIGHT_A0_BROWSER_OUTPUT||'../output/playwright/xi-a0-delivery'}/results`,
 // Keep real UI actions within the production handler's rate limit (20/s,
 // burst 40). Mocked API suites use their own unpaced configuration.
 use:{baseURL:process.env.XINSIGHT_E2E_BASE_URL,browserName:'chromium',launchOptions:{slowMo:100},ignoreHTTPSErrors:true,
 locale:'zh-CN',timezoneId:'Asia/Shanghai',viewport:{width:1440,height:1000},screenshot:'only-on-failure'}
});
