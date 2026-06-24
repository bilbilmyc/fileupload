import { test, expect } from '@playwright/test'

/**
 * 登录流程 E2E 测试
 *
 * 前提：dev server 已启动（npm run dev），后端服务可用（admin 用户已 seed）。
 */
test.describe('登录流程', () => {
  test.beforeEach(async ({ page }) => {
    // 清理 localStorage 确保每次都是干净状态
    await page.goto('/')
    await page.evaluate(() => localStorage.clear())
  })

  test('登录页可见，含用户名/密码字段', async ({ page }) => {
    await page.goto('/login')
    await expect(page.getByLabel(/用户名/i)).toBeVisible()
    await expect(page.getByLabel(/密码/i)).toBeVisible()
    await expect(page.getByRole('button', { name: /登录|Login/i })).toBeVisible()
  })

  test('空字段提交显示验证错误', async ({ page }) => {
    await page.goto('/login')
    await page.getByRole('button', { name: /登录|Login/i }).click()
    // antd Form 校验：表单字段应显示错误状态
    await expect(page.getByText(/请输入|required/i).first()).toBeVisible({ timeout: 3000 })
  })

  test('错误密码显示错误提示', async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel(/用户名/i).fill('admin')
    await page.getByLabel(/密码/i).fill('wrong-password')
    await page.getByRole('button', { name: /登录|Login/i }).click()
    // 错误提示可能是 message 组件 / form error / 401 提示
    await expect(page.locator('.ant-message, .ant-notification, [role="alert"]').first()).toBeVisible({ timeout: 5000 })
  })

  test('登录成功后跳转到文件管理', async ({ page }) => {
    await page.goto('/login')
    // 假设 admin/admin 是默认 seed 用户（生产前需通过 ADMIN_USERNAME/ADMIN_PASSWORD env 配置）
    await page.getByLabel(/用户名/i).fill('admin')
    await page.getByLabel(/密码/i).fill('admin')
    await page.getByRole('button', { name: /登录|Login/i }).click()

    // 等待跳转
    await page.waitForURL((url) => !url.pathname.includes('/login'), { timeout: 5000 })

    // token 已设置到 localStorage
    const token = await page.evaluate(() => localStorage.getItem('fileupload_token'))
    expect(token).toBeTruthy()
  })
})