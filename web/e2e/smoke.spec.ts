import { test, expect } from '@playwright/test'

/**
 * 冒烟测试（v0.8.0+）：不依赖后端 admin 用户
 *
 * 验证 web dev server 正常运行，React 应用挂载成功。
 * 后续 Playwright e2e 应在 docker-compose 或 dev 环境下跑（自动启动后端）。
 */
test.describe('冒烟测试', () => {
  test('首页加载无 JS 错误', async ({ page }) => {
    const errors: string[] = []
    page.on('pageerror', (err) => errors.push(err.message))

    await page.goto('/')
    await page.waitForLoadState('networkidle', { timeout: 5000 }).catch(() => {})

    // 至少 root 元素已渲染
    const root = page.locator('#root')
    await expect(root).toBeVisible()

    // 没有 JS 错误（允许 1-2 个 401 因为没登录 token）
    const fatalErrors = errors.filter(
      (e) => !e.includes('401') && !e.includes('Network')
    )
    expect(fatalErrors).toEqual([])
  })

  test('根路径可访问（返回 HTML）', async ({ page }) => {
    const response = await page.goto('/')
    expect(response?.status()).toBe(200)
    expect(response?.headers()['content-type']).toContain('text/html')
  })

  test('SDK 文件可加载（@fileupload/sdk 通过 file: dep）', async ({ page }) => {
    await page.goto('/')
    // 简单验证：页面挂载未抛"模块未找到"错误
    const consoleErrors: string[] = []
    page.on('console', (msg) => {
      if (msg.type() === 'error') consoleErrors.push(msg.text())
    })

    await page.waitForTimeout(500)
    const moduleErrors = consoleErrors.filter((e) => /module|cannot find|404/i.test(e))
    expect(moduleErrors).toEqual([])
  })
})