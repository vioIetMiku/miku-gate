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
  await page.getByRole('button', { name: '节点库', exact: true }).click();
  const profileResponse = page.waitForResponse((r) =>
    r.url().endsWith('/nodes/profile-check-fixture/profile'),
  );
  await page.getByRole('button', { name: '检查配置 fixture-static-profile', exact: true }).click();
  const profileResult = await (await profileResponse).json();
  expect(profileResult.mode).toBe('unsupported');
  expect(profileResult.network_tested).toBe(false);
  expect(profileResult.server_identity_verified).toBe(false);
  expect(JSON.stringify(profileResult)).not.toContain('UNTRUSTED_TEST_CONFIG_DO_NOT_ECHO');
  const profileDialog = page.getByRole('dialog', {
    name: '配置检查 · fixture-static-profile',
    exact: true,
  });
  await expect(
    profileDialog.getByRole('heading', { name: '配置不受支持', exact: true }),
  ).toBeVisible();
  await expect(profileDialog).toContainText('未测试网络');
  await page.screenshot({ path: '../artifacts/node-profile-check.png', fullPage: false });
  await profileDialog.getByRole('button', { name: '关闭', exact: true }).click();
  await page.getByRole('button', { name: '概览', exact: true }).click();

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
  await page.getByRole('button', { name: '选择VPN 日本开发环境', exact: true }).click();
  const vpnDialog = page.getByRole('dialog', { name: 'OpenVPN 节点 · 日本开发环境', exact: true });
  await expect(
    vpnDialog.getByRole('combobox', { name: '官方候选节点', exact: true }),
  ).toBeEnabled();
  await vpnDialog
    .getByRole('combobox', { name: '官方候选节点', exact: true })
    .selectOption('profile-check-fixture');
  await expect(
    vpnDialog.getByRole('button', { name: '验证官方TLS身份', exact: true }),
  ).toBeDisabled();
  await vpnDialog.getByRole('checkbox', { name: /高级：手动指定/ }).check();
  await expect(
    vpnDialog.getByRole('textbox', { name: '预期服务器证书名称', exact: true }),
  ).toHaveValue('fixture-static-profile');
  await expect(vpnDialog).toContainText('配置未通过受限格式');
  await expect(vpnDialog.getByRole('button', { name: '保存VPN绑定', exact: true })).toBeDisabled();
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(vpnDialog.getByRole('button', { name: '关闭对话框', exact: true })).toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(391);
  await page.screenshot({ path: '../artifacts/vpn-selection-mobile.png', fullPage: false });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await vpnDialog.getByRole('button', { name: '关闭对话框', exact: true }).click();
  await page.getByRole('button', { name: '查看 日本开发环境', exact: true }).click();
  const planResponse = page.waitForResponse(
    (response) => response.url().endsWith('/network-plan') && response.request().method() === 'GET',
  );
  await page.getByRole('button', { name: '预览隔离计划', exact: true }).click();
  const plan = await (await planResponse).json();
  expect(plan.stage).toBe('quarantine-only');
  expect(plan.proxy_enabled || plan.tunnel_enabled).toBe(false);
  await expect(page.getByText(plan.namespace, { exact: true })).toBeVisible();
  await page.getByRole('button', { name: '检查执行器', exact: true }).click();
  await expect(page.getByText(/未启用隔离 IPC/)).toBeVisible();
  await expect(page.getByRole('button', { name: '创建隔离沙箱', exact: true })).toHaveCount(0);
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
  await expect(page.getByRole('status')).toContainText('请先选择出口');
  await expect(page.locator('tbody')).toContainText('启动已阻止');
  await page.getByRole('button', { name: '关闭提示' }).click();
  await page.getByRole('button', { name: '设置上游 日本开发环境', exact: true }).click();
  const upstreamDialog = page.getByRole('dialog', { name: '代理上游 · 日本开发环境' });
  await expect(upstreamDialog.getByLabel('上游协议')).toBeEnabled();
  await upstreamDialog.getByLabel('上游地址', { exact: true }).fill('127.0.0.1');
  await upstreamDialog
    .getByLabel('上游端口', { exact: true })
    .fill(String(credentials.refusingProxyPort));
  await upstreamDialog.getByLabel('上游用户名（可选）', { exact: true }).fill('lab-upstream');
  await upstreamDialog
    .getByLabel('上游密码（可选）', { exact: true })
    .fill('not-a-real-upstream-password');
  await upstreamDialog.getByRole('checkbox', { name: /允许我明确指定的本机/ }).check();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(391);
  await expect(
    upstreamDialog.getByRole('button', { name: '关闭对话框', exact: true }),
  ).toBeInViewport();
  await page.screenshot({ path: '../artifacts/upstream-mobile.png', fullPage: false });
  await page.setViewportSize({ width: 1440, height: 1000 });
  await upstreamDialog.getByRole('button', { name: '保存上游', exact: true }).click();
  await expect(upstreamDialog).toHaveCount(0);
  await expect(page.locator('tbody')).toContainText('SOCKS5 上游');
  await page.getByRole('button', { name: '设置上游 日本开发环境', exact: true }).click();
  await expect(upstreamDialog.getByLabel('上游密码（可选）', { exact: true })).toHaveValue('');
  await expect(upstreamDialog.getByText(/不会回传旧密码/)).toBeVisible();
  await page.screenshot({ path: '../artifacts/upstream-settings.png', fullPage: false });
  await upstreamDialog.getByRole('button', { name: '关闭对话框', exact: true }).click();
  const rejected = page.waitForResponse(
    (r) => r.url().endsWith('/start') && r.request().method() === 'POST',
  );
  await page.getByRole('button', { name: '启动 日本开发环境', exact: true }).click();
  expect((await rejected).status()).toBe(502);
  await expect(page.getByRole('status')).toContainText('代理上游认证失败');
  const exitsResponse = await page.request.get('/api/exits');
  const savedExits = await exitsResponse.json();
  const savedExit = savedExits.find((item: { name: string }) => item.name === '日本开发环境');
  const savedUpstream = await page.request.get('/api/exits/' + savedExit.id + '/upstream');
  const safeView = await savedUpstream.json();
  expect(safeView.has_password).toBe(true);
  expect(safeView.password).toBeUndefined();
  expect(JSON.stringify(safeView)).not.toContain('not-a-real-upstream-password');
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
  await expect(page.getByRole('heading', { name: '主机与已有服务' })).toBeVisible();
  const inventoryResponse = page.waitForResponse((r) => r.url().endsWith('/api/host/inventory'));
  await page.getByRole('button', { name: '检测已有服务' }).click();
  const inventory = await inventoryResponse;
  expect(inventory.status()).toBe(200);
  const metadata = await inventory.json();
  expect(metadata.read_only).toBe(true);
  await expect(page.getByText('普通用户只读视图', { exact: false })).toBeVisible();
  if (metadata.supported) {
    await page.getByText('3x-ui / x-ui（同路径家族，尚未识别分支）', { exact: true }).click();
    await expect(page.getByText('/etc/x-ui/x-ui.db', { exact: true })).toBeVisible();
  } else {
    await expect(page.getByText('主机安装检测仅支持 Linux；当前结果不是 VPS 清单。')).toBeVisible();
  }
  await page.screenshot({
    path: '../artifacts/host-inventory-desktop.png',
    fullPage: true,
    animations: 'disabled',
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByRole('button', { name: '检测已有服务' })).toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  await page.screenshot({
    path: '../artifacts/host-inventory-mobile.png',
    fullPage: true,
    animations: 'disabled',
  });
  await page.setViewportSize({ width: 1440, height: 1000 });

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
