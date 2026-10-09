import { test, expect } from '@playwright/test';
import { readFileSync, mkdirSync } from 'node:fs';

test('本机控制台：登录、配置、阻止假启动、移动布局和会话撤销', async ({ page }) => {
  const credentials = JSON.parse(readFileSync('../.cache/e2e-auth.json', 'utf8'));
  const pageErrors: string[] = [];
  page.on('pageerror', (error) => pageErrors.push(error.message));
  await page.goto('/');
  await expect(page.getByRole('heading', { name: '欢迎回来' })).toBeVisible();
  mkdirSync('../artifacts', { recursive: true });
  await page.screenshot({ path: '../artifacts/login.png', fullPage: true });
  await page.getByLabel('用户名', { exact: true }).fill(credentials.username);
  await page.getByLabel('密码', { exact: true }).fill(credentials.password);
  await page.getByRole('button', { name: '登录控制台' }).click();
  await expect(page.getByRole('heading', { name: '连接的全貌，尽在这里。' })).toBeVisible();
  await expect(page.getByRole('heading', { name: '从第一个出口开始' })).toBeVisible();
  for (const [name, country] of [
    ['日本开发环境', 'JP'],
    ['美国浏览环境', 'US'],
  ]) {
    await page.getByRole('button', { name: '新建出口', exact: true }).click();
    await page.getByLabel('出口名称').fill(name);
    await page.getByRole('combobox', { name: '出口地区', exact: true }).selectOption(country);
    await page.getByRole('dialog').getByRole('button', { name: '创建出口', exact: true }).click();
    await expect(page.getByRole('dialog', { name: '保存你的代理凭据' })).toBeVisible();
    await page.getByRole('button', { name: '已保存', exact: true }).click();
    await expect(page.getByRole('button', { name: `查看 ${name}` })).toBeVisible();
  }
  await page.getByRole('button', { name: '查看 日本开发环境', exact: true }).click();
  const planResponse = page.waitForResponse(
    (response) => response.url().endsWith('/network-plan') && response.request().method() === 'GET',
  );
  await page.getByRole('button', { name: '预览隔离计划', exact: true }).click();
  const plan = await (await planResponse).json();
  expect(plan.stage).toBe('quarantine-only');
  expect(plan.proxy_enabled || plan.tunnel_enabled).toBe(false);
  await expect(page.getByText(plan.namespace, { exact: true })).toBeVisible();
  await page.getByText('查看拟安装的 nftables 规则', { exact: true }).click();
  await expect(page.getByLabel('nftables 隔离规则')).toContainText('policy drop');
  await expect(page.getByRole('button', { name: '关闭对话框', exact: true })).toBeInViewport();
  await page.screenshot({ path: '../artifacts/network-plan.png', fullPage: false });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(391);
  await expect(page.getByRole('button', { name: '关闭对话框', exact: true })).toBeInViewport();
  await page.screenshot({ path: '../artifacts/network-plan-mobile.png', fullPage: false });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.getByRole('button', { name: '关闭对话框', exact: true }).click();
  await page.getByRole('button', { name: '启动 日本开发环境', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('启动已阻止');
  await expect(page.locator('tbody')).toContainText('启动已阻止');
  await page.getByRole('button', { name: '关闭提示' }).click();
  await page.screenshot({ path: '../artifacts/overview.png', fullPage: true });
  await page.getByRole('button', { name: '切换深色主题' }).click();
  await page.screenshot({ path: '../artifacts/overview-dark.png', fullPage: true });
  await page.getByRole('button', { name: '切换浅色主题' }).click();
  await page.getByRole('button', { name: '查看 美国浏览环境' }).click();
  await page.getByRole('button', { name: '编辑配置' }).click();
  await page.getByLabel('出口名称').fill('美国开发环境');
  await page.getByRole('button', { name: '保存更改' }).click();
  await expect(page.getByRole('button', { name: '查看 美国开发环境' })).toBeVisible();
  await page.getByRole('button', { name: '查看 美国开发环境', exact: true }).click();
  await page.getByRole('button', { name: '重置凭据', exact: true }).click();
  await expect(page.getByRole('dialog', { name: '保存你的代理凭据' })).toBeVisible();
  await page.getByRole('button', { name: '已保存', exact: true }).click();
  await page.getByRole('button', { name: '查看 美国开发环境', exact: true }).click();
  await page.getByRole('button', { name: '删除配置', exact: true }).click();
  await expect(page.getByRole('dialog', { name: '删除出口配置？' })).toContainText('不会自动回收');
  await page.getByRole('button', { name: '确认删除', exact: true }).click();
  await expect(page.getByRole('button', { name: '查看 美国开发环境', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: '系统与诊断', exact: true }).click();
  await expect(page.getByText('特权网络执行器', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: '概览', exact: true }).click();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByRole('button', { name: '打开导航' })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(391);
  await page.screenshot({ path: '../artifacts/overview-mobile.png', fullPage: true });
  await page.getByRole('button', { name: '打开导航' }).click();
  await page.getByRole('button', { name: '事件日志', exact: true }).click();
  await expect(page.getByRole('heading', { name: '操作与系统事件' })).toBeVisible();
  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.getByRole('button', { name: /admin.*本机管理员/ }).click();
  await expect(page.getByRole('heading', { name: '欢迎回来' })).toBeVisible();
  const response = await page.request.get('/api/exits');
  expect(response.status()).toBe(401);
  expect(pageErrors).toEqual([]);
});
