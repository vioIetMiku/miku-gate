import { test, expect } from '@playwright/test';
import { readFileSync, mkdirSync } from 'node:fs';

test('新增情报弹窗：真实会话和同意门槛、未知结果及移动布局', async ({ page }) => {
  const account = JSON.parse(readFileSync('../.cache/e2e-auth.json', 'utf8'));
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill(account.username);
  await page.getByLabel('密码', { exact: true }).fill(account.password);
  await page.getByRole('button', { name: '登录控制台' }).click();
  await expect(page.getByRole('heading', { name: '连接的全貌，尽在这里。' })).toBeVisible();
  await page.getByRole('button', { name: '节点库', exact: true }).click();
  let requests = 0;
  await page.route('**/api/nodes/profile-check-fixture/intelligence', async (route) => {
    requests += 1;
    expect(route.request().postDataJSON()).toEqual({ confirm: true });
    // Browser UI fixture only. Provider truth mapping and real API consent are
    // separately tested; this route must never be reported as a live lookup.
    await route.fulfill({
      json: {
        ip: '8.8.8.8',
        checked_at: new Date().toISOString(),
        cached: false,
        network_path: 'UI fixture, not a live provider query',
        asns: [],
        geography: { state: 'unknown', source: 'fixture unavailable' },
        reputation: {
          state: 'pending',
          source: 'fixture pending',
          pending: true,
          listed_count: 0,
          checked_count: 0,
          results: [],
        },
        residential: 'unknown',
        warnings: ['夹具未知结果不是纯净IP证明。'],
      },
    });
  });
  await page
    .getByRole('button', { name: '归属与风险 fixture-static-profile', exact: true })
    .click();
  const dialog = page.getByRole('dialog', {
    name: '归属与风险辅助 · fixture-static-profile',
    exact: true,
  });
  const lookup = dialog.getByRole('button', { name: '查询候选 8.8.8.8', exact: true });
  await expect(lookup).toBeDisabled();
  expect(requests).toBe(0);
  await dialog.getByRole('checkbox', { name: /我同意向固定第三方/ }).check();
  await lookup.click();
  await expect(dialog).toContainText('第三方结果未出齐 · 未知');
  await expect(dialog).toContainText('未知（不凭运营商名称或国家猜测）');
  await expect(dialog).toContainText('UI fixture, not a live provider query');
  expect(requests).toBe(1);
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(dialog.getByRole('button', { name: '关闭对话框', exact: true })).toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  mkdirSync('../artifacts', { recursive: true });
  await page.screenshot({ path: '../artifacts/intelligence-mobile.png', fullPage: true });
  await dialog.getByRole('button', { name: '关闭对话框', exact: true }).click();
  expect(errors).toEqual([]);
});
