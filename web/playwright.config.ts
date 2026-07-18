import { defineConfig, devices } from '@playwright/test'

/**
 * Playwright 浏览器烟雾测试。
 *
 * `pnpm e2e` 会自动启动 Vite；登录接口由测试用例按场景 mock，
 * 因此本地和 CI 都不依赖一套长期运行的后端或固定管理员密码。
 */
export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: [
    ['list'],
    ['html', { outputFolder: 'playwright-report', open: 'never' }],
  ],
  use: {
    baseURL: 'http://127.0.0.1:41783',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: {
    command: 'node node_modules/vite/bin/vite.js --host 127.0.0.1 --port 41783',
    url: 'http://127.0.0.1:41783',
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
})
