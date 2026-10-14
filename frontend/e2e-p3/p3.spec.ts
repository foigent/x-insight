import { expect, test, type Page } from '@playwright/test';
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync, existsSync, openSync, writeSync, ftruncateSync, closeSync } from 'node:fs';
import { join } from 'node:path';
import en from '../src/i18n/en-US.json' with { type: 'json' };

const digest = (value: string) => createHash('sha256').update(value).digest('hex');
const money = (micros: string) => {
  if (!/^-?\d+$/.test(micros)) throw new Error('Invalid receipt amount');
  const value = BigInt(micros), absolute = value < 0n ? -value : value;
  return `${value < 0n ? '-' : ''}¥${absolute / 1000000n}.${(absolute % 1000000n).toString().padStart(6, '0')}`;
};

test('same P3 request, ordinary reader, money, runtime, resources and revocation', async ({ page }) => {
  const output = process.env.XINSIGHT_P3_RECEIPTS!;
  const result: Record<string, unknown> = { schema_version: 'xinsight.p3-browser.v1',
    passed: false, stage: 'input_receipts', wait: 'not_waiting', checks: [] };
  const completed: string[] = [];
  let receiptFD: number | undefined;
  const save = () => {
    if (receiptFD === undefined) return;
    const body = JSON.stringify(result) + '\n';
    writeSync(receiptFD, body, 0, 'utf8');
    ftruncateSync(receiptFD, Buffer.byteLength(body));
  };
  const stage = (name: string) => { result.stage = name; result.wait = 'not_waiting'; save(); };
  const wait = (name: string) => { result.wait = name; save(); };
  const done = (name: string) => { completed.push(name); result.checks = [...completed]; result.wait = 'not_waiting'; save(); };
  const readID = async (p: Page) => {
    wait('reading_ready');
    await expect(p.locator('.workbench-page')).toHaveAttribute('aria-busy', 'false');
    const details = p.locator('.reading-period details');
    wait('reading_identifier');
    if (!(await details.evaluate(el => (el as HTMLDetailsElement).open))) await details.locator('summary').click();
    const id = await p.locator('.reading-period code').innerText();
    expect(id).toMatch(/^xwr_[A-Za-z0-9_-]+$/);
    return id;
  };
  try {
    // Playwright can terminate a timed-out suspended body without reaching finally.
    // Retain only the existing closed partial receipt before each awaited stage.
    receiptFD = openSync(join(output, 'browser-result.json'), 'wx', 0o600);
    save();
    const { ready, chain, economics, runtime, resources, sourceReading, login } = await test.step('p3_input_receipts', () => {
      const load = (name: string) => JSON.parse(readFileSync(join(output, name), 'utf8'));
      return { ready: load('ready.json'), chain: load('p3-chain.json'),
        economics: load('ordinary-economic.json'), runtime: load('ordinary-runtime.json'),
        resources: load('ordinary-resources.json'), sourceReading: load('ordinary-money-reading.json'),
        login: JSON.parse(readFileSync(process.env.XINSIGHT_P3_LOGIN_FILE!, 'utf8')).reviewer };
    });
    await test.step('p3_test_body', async () => {
    stage('receipt_binding');
    const url = new URL(ready.base_url);
    expect(url.protocol).toBe('https:'); expect(url.hostname).toBe('127.0.0.1');
    expect(url.origin).toBe(process.env.XINSIGHT_P3_BROWSER_URL);
    expect(ready.source_system_id).toBe(chain.source_system_id);
    expect(ready.username).toBe(login.username);
    expect(economics.rows).toHaveLength(1); expect(runtime.items).toHaveLength(1);
    expect(resources.items).toHaveLength(1);
    const economic = economics.rows[0], request = runtime.items[0], resource = resources.items[0];
    for (const item of [economic, request, resource.record]) expect(item.request_id).toBe(chain.request_id);
    expect(economic.source_system_id).toBe(chain.source_system_id);
    expect(economic.source_project).toBe(`project-${chain.project_id}`);
    expect(sourceReading.request.filters.source_ids).toEqual([chain.source_system_id]);
    expect(sourceReading.request.filters.source_projects).toEqual([`project-${chain.project_id}`]);
    expect(economics.reading_id).toBe(sourceReading.reading.reading_id);
    for (const item of [request, resource.record.target]) {
      for (const key of ['source_system_id', 'source_instance', 'org_id', 'project_id']) expect(item[key]).toBe(chain[key]);
    }
    // UR writes its authoritative economic receipt during shutdown. The driver
    // binds these displayed amounts to that receipt after joining all owners.
    expect(economic.terminal_status).toBe('settled');
    result.request_sha256 = digest(chain.request_id);
    result.source_sha256 = digest(`${chain.source_system_id}/${chain.source_instance}/${chain.org_id}/${chain.project_id}`);
    done('exact_receipt_identity');

    stage('ordinary_login');
    wait('navigation');
    await page.goto('/login');
    wait('username_control');
    await page.getByLabel('Username', { exact: true }).fill(login.username);
    wait('password_control');
    await page.getByLabel('Password', { exact: true }).fill(login.password);
    wait('login_submit');
    await page.getByRole('button', { name: 'Sign in', exact: true }).click();
    wait('login_session');
    await expect(page.getByRole('link', { name: en.m0117, exact: true })).toBeVisible();
    done('ordinary_login');

    stage('cost_page');
    wait('navigation');
    await page.goto('/workbench/cost');
    await readID(page);
    wait('source_control');
    await page.getByRole('combobox', { name: en.m0472, exact: true }).selectOption(chain.source_system_id);
    wait('project_control');
    await page.getByLabel(en.m1180, { exact: true }).fill(`project-${chain.project_id}`);
    wait('reading_submit');
    await page.getByRole('button', { name: en.m2509, exact: true }).click();
    await readID(page);
    for (const [label, amount] of [[en.m2720, economic.gateway_charged_micros], [en.m2721, economic.calculated_reference_micros]]) {
      wait('money_control');
      await page.getByRole('button', { name: label, exact: true }).click();
      await readID(page);
      wait('money_display');
      await expect(page.locator('.metric-card.primary strong')).toHaveText(money(amount));
    }
    wait('group_control');
    await page.getByLabel(en.m2776).selectOption('source_project');
    wait('request_detail');
    await page.locator('.contribution-list button').filter({ hasText: `project-${chain.project_id}` }).click();
    // Open the product's existing full-identity disclosure rather than comparing truncated text.
    await page.locator('.workbench-detail-list summary').filter({ hasText: 'Request ID' }).click();
    await expect(page.locator('.workbench-detail-list code').filter({ hasText: chain.request_id })).toHaveText(chain.request_id);
    await expect(page.locator('.workbench-detail-list b').first()).toHaveText(money(economic.calculated_reference_micros));
    result.charged_micros = economic.gateway_charged_micros;
    result.calculated_micros = economic.calculated_reference_micros;
    done('displayed_money_and_exact_request');

    stage('runtime_and_resources');
    wait('navigation');
    await page.goto('/workbench/runtime');
    wait('runtime_controls');
    await page.getByLabel(en.m2086, { exact: true }).fill(chain.source_system_id);
    wait('project_control');
    await page.getByLabel(en.m1180, { exact: true }).fill(`project-${chain.project_id}`);
    wait('runtime_controls');
    await page.getByRole('button', { name: en.m2090, exact: true }).click();
    wait('runtime_controls');
    await page.locator('.runtime-entry select').selectOption('all');
    wait('runtime_detail');
    await page.getByTitle(chain.request_id, { exact: true }).and(page.locator('button.runtime-text-button')).click();
    const detail = page.locator('.runtime-detail');
    await expect(detail).toContainText(chain.source_instance);
    await expect(detail).toContainText(chain.project_id);
    await detail.locator('summary[title]').filter({ hasText: 'Request ID' }).click();
    await expect(detail.locator('.identifier-content code').first()).toHaveText(chain.request_id);
    wait('resource_detail');
    await detail.getByRole('button', { name: resource.record.resource.name, exact: true }).click();
    await expect(detail.locator('.runtime-resource-detail')).toContainText(resource.record.resource.ref);
    await expect(detail.locator('.runtime-resource-detail')).toContainText(resource.record.resource.version);
    done('displayed_same_request_runtime_and_resource');

    stage('foreign_scope_denied');
    wait('navigation');
    await page.goto('/workbench/cost'); await readID(page);
    wait('project_control');
    await page.getByLabel(en.m1180, { exact: true }).fill('project-p3-foreign-denied');
    const denied = page.waitForResponse(r => r.url().endsWith('/api/v1/workbench/readings') && r.request().method() === 'POST' && r.status() === 403);
    wait('reading_submit');
    await page.getByRole('button', { name: en.m2509, exact: true }).click();
    wait('foreign_response');
    expect((await (await denied).json()).code).toBe('workbench_scope_denied');
    wait('scope_cleared');
    await expect(page.getByRole('alert')).toBeVisible();
    await expect(page.locator('.metric-card.primary strong')).toHaveText('—');
    done('foreign_scope_denied_in_page');

    stage('revoked_reading');
    wait('navigation');
    await page.goto('/workbench/cost'); await readID(page);
    const old = await readID(page);
    writeFileSync(join(output, 'revoke'), '', { mode: 0o600, flag: 'wx' });
    wait('revocation_commit');
    await expect.poll(() => existsSync(join(output, 'revoked')), { timeout: 15000 }).toBe(true);
    // A real UI operation on the retained reading must observe the committed revoke.
    const invalidated = page.waitForResponse(r => r.url().includes(`/api/v1/workbench/readings/${old}/`) && [403, 409].includes(r.status()));
    wait('group_control');
    await page.getByLabel(en.m2776).selectOption('model');
    wait('revoked_response');
    await invalidated;
    wait('scope_cleared');
    // This fixture revokes every reviewer scope; the product closes the workbench.
    await expect(page.getByRole('heading', { name: en.m0112, exact: true })).toBeVisible();
    await expect(page.locator('.workbench-page')).toHaveCount(0);
    await expect(page.locator('.metric-card.primary strong')).toHaveCount(0);
    await expect(page.locator('.reading-period code')).toHaveCount(0);
    await expect(page.getByRole('navigation')).toHaveCount(0);
    done('revocation_removes_old_reading');
    result.passed = true; stage('complete');
    });
  } finally {
    // Closed fields only. Never retain passwords, session responses, DOM or error text.
    try { save(); } finally { if (receiptFD !== undefined) closeSync(receiptFD); }
  }
});
