import { defineConfig, devices } from '@playwright/test'

/**
 * Playwright E2E 测试配置（v0.7.0）
 *
 * 启动方式：先启后端 + web dev server，再跑测试。
 *   终端 A: cd server && ./bin/fileupload-server
 *   终端 B: cd web && npm run dev
 *   终端 C: cd web && npx playwright test
 */
export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: 'list',
  use: {
    baseURL: 'http://localhost:5173',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  // 不自动启 dev server — 用户手动启（避免与已有服务冲突）
  // webServer: { command: 'npm run dev', port: 5173, reuseExistingServer: true },
})