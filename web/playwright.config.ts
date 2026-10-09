import { defineConfig } from '@playwright/test';
export default defineConfig({
  testDir: './e2e',
  workers: 1,
  timeout: 30000,
  retries: 0,
  outputDir: '../.cache/playwright-results',
  reporter: 'list',
  use: {
    baseURL: 'http://127.0.0.1:3940',
    channel: process.env.MIKU_BROWSER_CHANNEL || (process.env.CI ? undefined : 'chrome'),
    headless: true,
    viewport: { width: 1440, height: 1000 },
    trace: 'retain-on-failure',
  },
  webServer: {
    command: 'node ../scripts/e2e-server.mjs',
    url: 'http://127.0.0.1:3940/api/health',
    reuseExistingServer: false,
    timeout: 30000,
  },
});
