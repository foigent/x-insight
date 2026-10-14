import { readingID } from './reading';
import { expect, test } from '@playwright/test';
import { mkdir } from 'node:fs/promises';

test('official V1 → PG → production HTTP → embedded workbench', async ({page})=>{
 const errors:string[]=[];page.on('pageerror',error=>errors.push(error.message));
 let authenticated=false;
 const failed:string[]=[];const deniedOptionalPanels:string[]=[];
 page.on('response',async response=>{
  const path=new URL(response.url()).pathname;
  if(!path.startsWith('/api/')||response.status()<400)return;
  if(response.status()===401&&path==='/api/v1/access/session'&&!authenticated)return;
  // The retained workbench-only grants do not qualify optional activity and
  // runtime panels on the current home page. Require their exact scope denial.
  if(response.status()===403&&['/api/v1/work-activity/summary','/api/v1/runtime/readings'].includes(path)){
   const problem=await response.json();
   if(problem.code==='workbench_scope_denied'){deniedOptionalPanels.push(path);return;}
  }
  failed.push(`${response.status()} ${path}`);
 });
 await page.goto('/login');
 await page.getByLabel('用户名').fill('workbench.one');
 await page.getByLabel('密码',{exact:true}).fill('workbench password');
 await page.getByRole('button',{name:'登录',exact:true}).click();
 await page.getByRole('link',{name:'费用与使用',exact:true}).click();
 await expect(page.getByRole('heading',{name:'费用与使用',exact:true})).toBeVisible();
 await page.getByRole('button',{name:'网关扣费',exact:true}).click();
 await expect(page.locator('.metric-card.primary strong')).toHaveText('¥0.000270');
 authenticated=true;
 await page.getByText('阅读标识与版本',{exact:true}).click();
 await expect(page.locator('.metric-card code')).toBeVisible();
 const id=await readingID(page);
 await page.locator('.contribution-controls select').first().selectOption('model');
 await expect(page.locator('.contribution-list button')).toHaveCount(10);
 await expect(page.locator('.metric-card code')).toHaveText(id);
 await page.locator('.contribution-controls select').last().selectOption('all');
 await expect(page.locator('.contribution-list button')).toHaveCount(5);
 await page.getByRole('button',{name:'加载更多贡献'}).click();
 await expect(page.locator('.contribution-list button')).toHaveCount(10);
 await page.getByRole('button',{name:'加载更多贡献'}).click();
 await expect(page.locator('.contribution-list button')).toHaveCount(12);
 await expect(page.locator('.contributions-panel footer strong')).toHaveText('当前范围已知金额 ¥0.000270');
 await page.locator('.contribution-list button').last().click();
 await expect(page.locator('.workbench-detail-list')).toContainText('¥0.000017');
 await expect(page.getByRole('button',{name:'查看调查证据'})).toHaveCount(0);
 await page.getByRole('button',{name:'← 返回费用贡献'}).click();
 await expect(page.locator('.metric-card code')).toHaveText(id);
 await page.getByRole('button',{name:'计算参考值',exact:true}).click();
 await expect(page.locator('.metric-card.primary strong')).toHaveText('¥0.000342');
 await page.getByLabel('来源项目',{exact:true}).fill('project-9000');
 await page.getByRole('button',{name:'应用范围',exact:true}).click();
 await expect(page.locator('.metric-card.primary strong')).toHaveText('¥0.000023');
 const savedName=`browser-${Date.now()}`;
 await page.getByText('个人筛选 · 保存与重开',{exact:true}).click();
 await page.locator('.saved-filter-panel input').fill(savedName);
 await page.getByRole('button',{name:'保存当前筛选'}).click();
 const savedRow=page.locator('.saved-filter-panel li').filter({hasText:savedName});
 await expect(savedRow).toBeVisible();
 await page.getByRole('button',{name:'网关扣费',exact:true}).click();
 await expect(page.locator('.metric-card.primary strong')).toHaveText('¥0.000017');
 await savedRow.getByRole('button',{name:'重开',exact:true}).click();
 await expect(page.locator('.metric-card.primary strong')).toHaveText('¥0.000023');
 await expect(page.getByLabel('来源项目',{exact:true})).toHaveValue('project-9000');
 await expect(page.getByRole('button',{name:'计算参考值',exact:true})).toHaveClass('active');
 await savedRow.getByRole('button',{name:'更新',exact:true}).click();
 await expect(savedRow).toContainText('修订 2');
 const output=process.env.XINSIGHT_A0_BROWSER_OUTPUT||'../output/playwright/xi-a0-delivery';await mkdir(output,{recursive:true});
 for(const width of [1440,1920,1024])for(const colorScheme of ['light','dark'] as const){
  await page.setViewportSize({width,height:1000});await page.emulateMedia({colorScheme});
  await expect(page.locator('.metric-card.primary strong')).toHaveText('¥0.000023');
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);
  await page.evaluate(()=>window.scrollTo(0,0));
  if(colorScheme==='dark')expect(await page.locator('.workbench-hero h1').evaluate(el=>getComputedStyle(el).color)).toBe('rgb(236, 233, 242)');
  await page.screenshot({path:`${output}/${width}-${colorScheme}.png`,fullPage:true});
 }
 expect(deniedOptionalPanels).toContain('/api/v1/work-activity/summary');
 expect(errors).toEqual([]);expect(failed).toEqual([]);
});

// Existing Chinese scenarios select their UI language explicitly.
test.beforeEach(async ({ page }) => { await page.addInitScript(() => localStorage.setItem("xinsight.locale.v1", "zh-CN")); });
