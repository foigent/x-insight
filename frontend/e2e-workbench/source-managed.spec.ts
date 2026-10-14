import { expect, test } from '@playwright/test';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { openRetainedSourceFacts, readingID } from './reading';

test('managed source attention is visible and current field revocation hides its status', async ({ page }) => {
 const output=process.env.XINSIGHT_A0_BROWSER_OUTPUT!;
 const ready=JSON.parse(readFileSync(join(output,'ready.json'),'utf8'));
 const errors:string[]=[];page.on('pageerror',e=>errors.push(e.message));
 await page.goto('/login');await page.getByLabel('用户名').fill('workbench.one');
 await page.getByLabel('密码',{exact:true}).fill('workbench password');
 await page.getByRole('button',{name:'登录',exact:true}).click();
 await page.getByRole('link',{name:'费用与使用',exact:true}).click();
 await expect(page.getByRole('heading',{name:'费用与使用',exact:true})).toBeVisible();
 await readingID(page);
 await openRetainedSourceFacts(page,ready);
 const original=await readingID(page);
 await page.getByText('来源观察与覆盖',{exact:true}).click();
 const profiles=page.getByRole('region',{name:'来源字段采集状态'});
 await expect(profiles).toContainText('执行上下文');
 await expect(profiles).toContainText('账户与授权来源');
 await expect(profiles).toContainText('计费依据');
 await expect(profiles).toContainText('最近成功接收页：');
 await expect(profiles).not.toContainText('尚未接收');
 await expect(page.locator('.source-facts-panel')).toContainText('26 份已授权记录');
 const originalResponse=await page.request.get(`/api/v1/workbench/readings/${original}`);
 expect(originalResponse.ok()).toBe(true);const originalReading=await originalResponse.json();
 expect(originalReading.source_coverage.requires_attention??false).toBe(false);
 writeFileSync(join(output,'source-attention'),'record a synthetic expired cursor through the importer store\n');
 await expect.poll(()=>existsSync(join(output,'source-attention-applied'))).toBe(true);
 const frozenResponse=await page.request.get(`/api/v1/workbench/readings/${original}`);
 expect((await frozenResponse.json()).source_coverage.requires_attention??false).toBe(false);
 const currentResponse=await page.request.get(`/api/v1/workbench/readings/${original}/sources`);
 expect(currentResponse.ok()).toBe(true);const current=await currentResponse.json();
 expect(current.items.flatMap((s:{source_facts?:{requires_attention:boolean}[]})=>s.source_facts??[]).some((s:{requires_attention:boolean})=>s.requires_attention)).toBe(true);
 await page.getByRole('button',{name:'应用范围',exact:true}).click();
 const affected=await readingID(page);expect(affected).not.toBe(original);
 await expect(profiles).toContainText('需管理员处理');
 await expect(profiles).toContainText('消费位置已过期');
 await expect(page.locator('.source-facts-panel')).toContainText('建立本阅读时，存在需管理员处理的采集注册');
 await expect(page.locator('.source-facts-panel')).toContainText('26 份已授权记录');
 const geometry=[];
 for(const [width,colorScheme] of [[1440,'light'],[736,'dark'],[360,'light']] as const){
  await page.setViewportSize({width,height:1000});await page.emulateMedia({colorScheme});
  await profiles.scrollIntoViewIfNeeded();
  const measured=await profiles.evaluate(el=>({viewport:innerWidth,scroll:document.documentElement.scrollWidth,left:el.getBoundingClientRect().left,right:el.getBoundingClientRect().right}));
  expect(measured.scroll).toBeLessThanOrEqual(width);expect(measured.left).toBeGreaterThanOrEqual(0);expect(measured.right).toBeLessThanOrEqual(width);
  geometry.push({colorScheme,...measured});await page.screenshot({path:join(output,`managed-${width}-${colorScheme}.png`),fullPage:true});
 }
 writeFileSync(join(output,'revoke-identity'),'revoke current identity reading permission\n');
 await expect.poll(async()=>{
  const r=await page.request.get(`/api/v1/workbench/readings/${affected}/sources`);return r.status();
 }).toBe(409);
 // A reload must reject the retained old reading. Use the product's explicit
 // rebuild action with the refreshed scope; never restore the revoked grant.
 await page.reload();
 await page.getByRole('button',{name:'按当前权限重新建立阅读',exact:true}).click();
 await readingID(page);
 await page.getByText('来源观察与覆盖',{exact:true}).click();
 await expect(profiles).not.toContainText('账户与授权来源');
 await expect(profiles).not.toContainText('需管理员处理');
 await expect(profiles).toContainText('执行上下文');
 await expect(page.locator('.source-facts-panel')).not.toContainText('建立本阅读时，存在需管理员处理的采集注册');
 if(ready.bounded_source_id){
  await expect.poll(()=>existsSync(join(output,'bounded-ingestion-complete')),{timeout:45000}).toBe(true);
  await page.getByRole('combobox',{name:'来源',exact:true}).selectOption('');
  await page.getByRole('button',{name:'应用范围',exact:true}).click();await readingID(page);
  await page.getByRole('combobox',{name:'来源',exact:true}).selectOption(ready.bounded_source_id);
  await page.getByRole('button',{name:'应用范围',exact:true}).click();const bounded=await readingID(page);
  await expect(page.locator('.source-facts-panel')).toContainText('600 份已授权记录');
  const response=await page.request.get(`/api/v1/workbench/readings/${bounded}/source-facts?limit=50`);
  expect(response.ok()).toBe(true);const facts=await response.json();
  expect(facts.items.length).toBe(50);expect(facts.items.every((f:{revision:string,chain_state:string})=>f.revision==='3'&&f.chain_state!=='missing_predecessor')).toBe(true);
  await expect(profiles).toContainText('已启用');await expect(profiles).not.toContainText('需管理员处理');
 }
 expect(errors).toEqual([]);
 writeFileSync(join(output,'managed-observation.json'),JSON.stringify({original,affected,retained_documents:26,bounded_received_documents:ready.bounded_source_id?1800:undefined,bounded_latest_documents:ready.bounded_source_id?600:undefined,frozen_attention_unchanged:true,current_attention:true,field_revocation_hides_status:true,geometry,errors,boundary:'production HTTPS handler, native restricted PostgreSQL roles, retained synthetic Source Facts'},null,2));
});

// Existing Chinese scenarios select their UI language explicitly.
test.beforeEach(async ({ page }) => { await page.addInitScript(() => localStorage.setItem("xinsight.locale.v1", "zh-CN")); });
