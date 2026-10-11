import { test, expect } from '@playwright/test';
import { readFileSync, mkdirSync } from 'node:fs';
test('面板持久网页：真实本机API/CSRF、脱敏预览与未健康出口阻止', async ({ page }) => {
  const account = JSON.parse(readFileSync('../.cache/e2e-auth.json', 'utf8'));
  const errors: string[] = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await page.goto('/');
  await page.getByLabel('用户名', { exact: true }).fill(account.username);
  await page.getByLabel('密码', { exact: true }).fill(account.password);
  await page.getByRole('button', { name: '登录控制台' }).click();
  await expect(page.getByRole('heading', { name: '连接的全貌，尽在这里。' })).toBeVisible();
  await page.getByRole('button', { name: '系统与诊断', exact: true }).click();
  const section = page.getByRole('region', { name: '复用本机 3x-ui 已有节点' });
  await section.getByLabel('面板本机端口').fill(String(account.panelFixturePort));
  await section.getByLabel('面板基础路径').fill('/fixture/');
  await section.getByLabel('面板用户名').fill('fixture-admin');
  await section.getByLabel('面板密码', { exact: true }).fill('fixture-private-password');
  const login = section.getByRole('button', { name: '登录并检测持久入口' });
  await expect(login).toBeDisabled();
  await section.getByRole('checkbox', { name: /确认读取本机面板/ }).check();
  const pending = page.waitForResponse((r) => r.url().endsWith('/host/panel/inspect'));
  await login.click();
  const inspected = await (await pending).json();
  expect(inspected.applied).toBe(false);
  expect(inspected.persistent_api).toBe(true);
  expect(JSON.stringify(inspected)).not.toContain('fixture-private-password');
  expect(JSON.stringify(inspected)).not.toContain('private-panel-fixture-client');
  await section.getByLabel('选择已有入口').selectOption(inspected.report.inbounds[0].id);
  await expect(section.getByRole('button', { name: '备份并应用持久路由' })).toBeDisabled();
  await expect(section).toContainText('双摘要变化时拒绝写入');
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  mkdirSync('../artifacts', { recursive: true });
  await page.screenshot({ path: '../artifacts/panel-reuse-mobile.png', fullPage: true });
  expect(errors).toEqual([]);
});
