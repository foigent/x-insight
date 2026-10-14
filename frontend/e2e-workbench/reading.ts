import { expect, type Page } from '@playwright/test';
import { randomUUID } from 'node:crypto';

// Retained producer bytes have fixed event times. Use the ordinary personal
// filter API and reopen in the UI; never rewrite facts or widen authorization.
export async function openRetainedSourceFacts(page: Page, ready: {
 source_facts_id: string;
 source_facts_period: { start_inclusive: string; end_exclusive: string; display_timezone: string };
}): Promise<void> {
 const response = await page.request.get('/api/v1/access/session');
 expect(response.ok()).toBe(true);
 const session = await response.json();
 expect(ready.source_facts_period).toBeDefined();
 const name = `retained-source-${randomUUID()}`;
 const saved = await page.request.post('/api/v1/workbench/saved-filters', {
  headers: { Origin: new URL(page.url()).origin, 'X-CSRF-Token': session.csrf_token,
   ...(session.scope_revision ? { 'X-XInsight-Scope-Revision': session.scope_revision } : {}) },
  data: { name, expected_revision: 0, idempotency_key: randomUUID(), config: {
   contract_version: 'xinsight.workbench.v1', page: 'cost', metric_version: 'xinsight.workbench.cost.v1',
   period: ready.source_facts_period, money_basis: 'gateway_charged', currency: 'CNY',
   filters: { source_ids: [ready.source_facts_id], source_projects: [], cost_projects: [] },
  } },
 });
 expect(saved.ok()).toBe(true);
 await page.goto('/workbench/cost');
 await readingID(page);
 await page.locator('.saved-filter-panel').locator('..').locator('summary').click();
 await page.locator('.saved-filter-panel li').filter({ hasText: name }).getByRole('button', { name: '重开', exact: true }).click();
 await readingID(page);
}

// Read the displayed identity only after the replacement reading has finished.
export async function readingID(page: Page): Promise<string> {
 await expect(page.locator('.workbench-page')).toHaveAttribute('aria-busy', 'false');
 const identity = page.locator('.reading-period details');
 if (!(await identity.evaluate(el => (el as HTMLDetailsElement).open))) {
  await identity.locator('summary').click();
 }
 const code = page.locator('.metric-card code');
 await expect(code).toBeVisible();
 await expect(code).toHaveText(/^xwr_[A-Za-z0-9_-]+$/);
 return code.innerText();
}
