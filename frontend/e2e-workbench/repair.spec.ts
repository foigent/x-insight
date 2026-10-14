import { readingID } from './reading';
import { expect, test, type Page } from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

const output=process.env.XINSIGHT_A0_BROWSER_OUTPUT!;
function fixtureSQL(query:string){
 const ready=JSON.parse(readFileSync(join(output,'ready.json'),'utf8'));
 if(!/^xia0_http_[a-f0-9]+$/.test(ready.database_name)||process.env.PGHOST!=='127.0.0.1')throw new Error('Dedicated synthetic harness database required');
 return execFileSync('psql',['-X','-At','-v','ON_ERROR_STOP=1','-d',ready.database_name,'-c',query],{encoding:'utf8'}).trim();
}
async function login(page:Page){await page.goto('/login');await page.getByLabel('用户名').fill('workbench.one');await page.getByLabel('密码',{exact:true}).fill('workbench password');await page.getByRole('button',{name:'登录',exact:true}).click();await expect(page.locator('.metric-card.primary strong')).toHaveText('¥0.000270');await page.getByText('阅读标识与版本',{exact:true}).click();await expect(page.locator('.metric-card code')).toBeVisible()}
async function apply(page:Page,hours:number){await page.getByLabel('期间',{exact:true}).selectOption(String(hours));const before=await readingID(page);await page.getByRole('button',{name:'应用范围',exact:true}).click();await expect(page.locator('.metric-card code')).not.toHaveText(before);await expect(page.locator('.metric-card.primary strong')).toHaveText('¥0.000270');await expect(page.locator('.workbench-page')).toHaveAttribute('aria-busy','false')}
async function openCoverage(page:Page){const panel=page.locator('.coverage-disclosure');if(!(await panel.evaluate(el=>(el as HTMLDetailsElement).open)))await panel.locator('summary').click()}
async function currentReading(page:Page){const id=await readingID(page);const response=await page.request.get(`/api/v1/workbench/readings/${id}`);expect(response.ok()).toBe(true);return response.json()}

test('all periods, widths, themes and hourly details preserve complete reading totals',async({page})=>{
 const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message));
 await login(page);const evidence:unknown[]=[];
 for(const hours of [24,72,168,720]){
  await apply(page,hours);const reading=await currentReading(page);
  const buckets=page.locator('.series-bars>div');await expect(buckets).toHaveCount(hours===168?28:hours===720?30:24);
  for(const width of [1024,1440,1920])for(const colorScheme of ['light','dark'] as const){
   await page.setViewportSize({width,height:1000});await page.emulateMedia({colorScheme});
   const measurements=await page.locator('.series-bars').evaluate(el=>({documentWidth:document.documentElement.scrollWidth,viewport:window.innerWidth,minBucketWidth:Math.min(...[...el.children].map(x=>x.getBoundingClientRect().width)),total:[...el.children].reduce((sum,x)=>sum+BigInt((x as HTMLElement).dataset.value!),0n).toString(),labels:[...el.querySelectorAll('span')].map(x=>({left:x.getBoundingClientRect().left,right:x.getBoundingClientRect().right}))}));
   expect(measurements.documentWidth).toBeLessThanOrEqual(width);expect(measurements.minBucketWidth).toBeGreaterThan(0);expect(measurements.total).toBe('270');
   for(let i=1;i<measurements.labels.length;i++)expect(measurements.labels[i].left).toBeGreaterThanOrEqual(measurements.labels[i-1].right);
   await expect(buckets.first()).toHaveAttribute('data-start',reading.period.start_inclusive);await expect(buckets.last()).toHaveAttribute('data-end',reading.period.end_exclusive);
   await expect(page.locator('.metric-card code')).toHaveText(reading.reading_id);
   await page.screenshot({path:join(output,`${hours}h-${width}-${colorScheme}.png`),fullPage:true});evidence.push({hours,width,colorScheme,...measurements,reading_id:reading.reading_id});
  }
  await page.getByLabel('趋势粒度',{exact:true}).selectOption('hourly');await expect(buckets).toHaveCount(hours);
  await expect(page.locator('.series-bars')).toHaveAttribute('data-reading-id',reading.reading_id);
  expect(await buckets.evaluateAll(items=>items.reduce((sum,x)=>sum+BigInt((x as HTMLElement).dataset.value!),0n).toString())).toBe('270');
  await expect(buckets.first()).toHaveAttribute('data-start',reading.period.start_inclusive);await expect(buckets.last()).toHaveAttribute('data-end',reading.period.end_exclusive);
  for(const width of [1024,1440,1920]){
   await page.setViewportSize({width,height:1000});
   expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
   if(hours>30){
    expect(await page.locator('.series-scroll').evaluate(el=>el.scrollWidth>el.clientWidth)).toBe(true);
    await buckets.last().focus();await expect(page.locator('.series-value')).toContainText('¥');
    expect(await buckets.last().evaluate(el=>{const a=el.getBoundingClientRect(),b=el.closest('.series-scroll')!.getBoundingClientRect();return a.left>=b.left&&a.right<=b.right+1})).toBe(true);
    await buckets.first().focus();
   }
  }
  const after=await currentReading(page);expect(after.cutoff).toBe(reading.cutoff);expect(after.membership_ref).toBe(reading.membership_ref);
  await page.screenshot({path:join(output,`${hours}h-hourly.png`),fullPage:true});
 }
 expect(errors).toEqual([]);writeFileSync(join(output,'period-matrix.json'),JSON.stringify(evidence,null,2));
});

test('real coverage, expiry, revocation and transport recovery never mislabel stale data',async({page})=>{
 const source=JSON.parse(readFileSync(join(output,'ready.json'),'utf8')).source_system_id;
 if(!/^[a-f0-9-]{36}$/.test(source))throw new Error('invalid synthetic source');
 fixtureSQL(`INSERT INTO adapter_import_states(source_system_id,adapter_manifest_id,cursor_key,activation_source_instance,activation_cursor,activation_high_watermark,activated_at,authorization_ref,cursor_value,high_watermark,worker_status,last_import_completed_at) VALUES('${source}','22222222-2222-4222-8222-222222222224','unirouter.completed-execution.v1','synthetic','opaque','opaque',now()-interval '12 hours','synthetic-test','opaque','opaque','idle',now())`);
 await login(page);
 await openCoverage(page);
 await expect(page.getByText(/该部分历史未确认覆盖/)).toBeVisible();await expect(page.getByText('已知采集起点',{exact:true})).toBeVisible();
 await expect(page.getByText(/来源字段缺项/)).toBeVisible();
 const reading=await currentReading(page);
 expect(reading.coverage.period_completeness).toBe('unknown');expect(reading.coverage.gaps).toContain('period_precedes_capture_start');
 fixtureSQL(`UPDATE adapter_import_states SET status='error',worker_status='error' WHERE source_system_id='${source}'`);
 const live=await page.request.get(`/api/v1/workbench/readings/${reading.reading_id}/sources`);expect((await live.json()).items[0].status).toBe('disconnected');
 const fixed=await currentReading(page);expect(fixed.coverage).toEqual(reading.coverage);
 await page.screenshot({path:join(output,'coverage-fixed.png'),fullPage:true});
 // Actual production 410, never an intercepted replacement response.
 fixtureSQL(`UPDATE workbench_readings SET expires_at=cutoff+interval '1 second' WHERE id='${reading.reading_id}'`);
 await expect.poll(()=>Date.now()).toBeGreaterThan(Date.parse(reading.cutoff)+1000);
 const gone=page.waitForResponse(r=>r.url().includes('/contributions')&&r.status()===410);
 await page.getByLabel('贡献维度').selectOption('model');await gone;
 await expect(page.getByRole('alert')).toContainText('过期');await expect(page.locator('.contribution-list button')).toHaveCount(0);await expect(page.locator('.metric-card.primary strong')).toHaveText('—');
 await page.getByRole('button',{name:'按当前权限重新建立阅读'}).click();await expect(page.locator('.metric-card.primary strong')).toHaveText('¥0.000270');
 // Ordinary network failure is explicitly injected at the browser transport.
 await page.route('**/contributions?**',route=>route.abort('failed'));
 await page.getByLabel('贡献维度').selectOption('model');await expect(page.getByRole('alert')).toBeVisible();await expect(page.getByLabel('贡献维度')).toHaveValue('source_project');await expect(page.locator('.contribution-list')).toContainText('project-');
 await page.unroute('**/contributions?**');await page.getByRole('button',{name:'重试更新'}).click();await expect(page.getByLabel('贡献维度')).toHaveValue('model');await expect(page.locator('.contribution-list')).toContainText('browser-model');
 // Actual 52-member detail page, then transport failure/recovery and real expiry on page two.
 await page.getByLabel('贡献维度').selectOption('source_project');
 await page.getByLabel('贡献模式').selectOption('all');await expect(page.locator('.contribution-list button')).toHaveCount(5);
 await page.getByRole('button',{name:'加载更多贡献'}).click();await expect(page.locator('.contribution-list button')).toHaveCount(10);
 await page.getByRole('button',{name:'加载更多贡献'}).click();await expect(page.locator('.contribution-list button')).toHaveCount(12);
 await page.locator('.contribution-list button').filter({hasText:'project-9000'}).click();
 await expect(page.locator('.workbench-detail-list article')).toHaveCount(50);
 await page.route('**/details?**',route=>route.abort('failed'));
 await page.getByRole('button',{name:'加载更多依据'}).click();await expect(page.getByRole('alert')).toBeVisible();
 await expect(page.locator('.workbench-detail-list article')).toHaveCount(50);
 await page.unroute('**/details?**');await page.getByRole('button',{name:'重试更新'}).click();await expect(page.locator('.workbench-detail-list article')).toHaveCount(52);
 await page.getByRole('button',{name:'← 返回费用贡献'}).click();
 const detailReading=await currentReading(page);
 await page.locator('.contribution-list button').filter({hasText:'project-9000'}).click();await expect(page.locator('.workbench-detail-list article')).toHaveCount(50);
 fixtureSQL(`UPDATE workbench_readings SET expires_at=cutoff+interval '1 second' WHERE id='${detailReading.reading_id}'`);
 await expect.poll(()=>Date.now()).toBeGreaterThan(Date.parse(detailReading.cutoff)+1000);
 const detailGone=page.waitForResponse(r=>r.url().includes('/details')&&r.status()===410);
 await page.getByRole('button',{name:'加载更多依据'}).click();await detailGone;
 await expect(page.locator('.workbench-detail-list')).toHaveCount(0);await expect(page.locator('.metric-card.primary strong')).toHaveText('—');
 await page.getByRole('button',{name:'按当前权限重新建立阅读'}).click();await expect(page.locator('.metric-card.primary strong')).toHaveText('¥0.000270');
 fixtureSQL(`UPDATE workbench_access_grants SET active=false,revision=revision+1 WHERE subject_id='xs_8899aabbccddeeff'`);
 const revoked=page.waitForResponse(r=>r.url().includes('/contributions')&&r.status()===409);await page.getByLabel('贡献维度').selectOption('cost_project');await revoked;
 await expect(page.getByRole('alert')).toContainText('权限已变化');await expect(page.locator('.metric-card.primary strong')).toHaveText('—');
 fixtureSQL(`UPDATE workbench_access_grants SET active=true,revision=revision+1,source_project_ref='project-9000' WHERE subject_id='xs_8899aabbccddeeff'`);
 await page.getByRole('button',{name:'按当前权限重新建立阅读'}).click();await expect(page.locator('.metric-card.primary strong')).toHaveText('¥0.000017');
 const restricted=await currentReading(page);expect(restricted.coverage.status).toBe('scope_limited_unknown');expect(restricted.coverage.capture_started_at).toBeUndefined();expect(restricted.coverage.received_watermark||'').toBe('');
 await openCoverage(page);await expect(page.getByText('当前授权不足以判断整源覆盖。')).toBeVisible();await expect(page.getByText(/该部分历史未确认覆盖/)).toHaveCount(0);await expect(page.getByText('已知采集起点',{exact:true})).toHaveCount(0);
 await page.screenshot({path:join(output,'restricted-coverage.png'),fullPage:true});
 fixtureSQL(`UPDATE workbench_access_grants SET source_project_ref=NULL,revision=revision+1 WHERE subject_id='xs_8899aabbccddeeff'; UPDATE adapter_import_states SET status='idle',worker_status='idle' WHERE source_system_id='${source}'`);
 const second='99999999-9999-4999-8999-999999999999';
 fixtureSQL(`INSERT INTO source_systems(id,source_key,source_type,display_name,source_version) SELECT '${second}','ui-second-source',source_type,'Second synthetic source',source_version FROM source_systems WHERE id='${source}'; INSERT INTO workbench_access_grants(subject_id,source_system_id,allow_summary,allow_details,allow_coverage,allow_preferences) VALUES('xs_8899aabbccddeeff','${second}',true,true,true,true); INSERT INTO adapter_import_states(source_system_id,adapter_manifest_id,cursor_key,activation_source_instance,activation_cursor,activation_high_watermark,activated_at,authorization_ref,cursor_value,high_watermark,worker_status,last_import_completed_at) VALUES('${second}','22222222-2222-4222-8222-222222222224','unirouter.completed-execution.v1','synthetic-second','opaque','opaque',now()-interval '48 hours','synthetic-test','opaque','opaque','idle',now())`);
 await apply(page,24);await openCoverage(page);await expect(page.getByText('最早已知采集起点（不代表所有来源）')).toBeVisible();await expect(page.getByText(/该部分历史未确认覆盖/)).toBeVisible();
 const mixed=await currentReading(page);expect(Date.parse(mixed.coverage.capture_started_at)).toBeLessThan(Date.parse(mixed.period.start_inclusive));expect(mixed.coverage.gaps).toContain('period_precedes_capture_start');
 await page.screenshot({path:join(output,'mixed-source-coverage.png'),fullPage:true});
 writeFileSync(join(output,'recovery-evidence.json'),JSON.stringify({reading_id:reading.reading_id,fixed_coverage:reading.coverage,restricted_coverage:restricted.coverage,expiry_http:410,revocation_http:409,ordinary_failure:'browser transport abort; real production success after removal'},null,2));
});

// Existing Chinese scenarios select their UI language explicitly.
test.beforeEach(async ({ page }) => { await page.addInitScript(() => localStorage.setItem("xinsight.locale.v1", "zh-CN")); });
