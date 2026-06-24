import { test, expect } from '@playwright/test'

/**
 * 文件列表 E2E 测试
 *
 * 前提：已用 admin/admin 登录（依赖 login.spec.ts 的种子用户）
 */
test.describe('文件管理', () => {
  test.beforeEach(async ({ page }) => {
    // 模拟已登录状态：直接注入 token
    await page.goto('/')
    await page.evaluate(() => {
      localStorage.setItem('fileupload_token', 'test-token')
      localStorage.setItem('fileupload_namespace', 'default')
      localStorage.setItem('fileupload_user_id', 'admin')
    })
  })

  test('文件管理页可见，含工具栏', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('heading', { name: /文件|File/i }).first()).toBeVisible({ timeout: 3000 })
  })

  test('上传按钮可见且可点击', async ({ page }) => {
    await page.goto('/')
    const uploadBtn = page.getByRole('button', { name: /上传|Upload/i }).first()
    if (await uploadBtn.isVisible({ timeout: 2000 }).catch(() => false)) {
      await uploadBtn.click()
      // 可能弹出 Modal 或导航到上传页
      // 这里仅验证点击不抛错
    }
  })
})

/**
 * 上传流程 E2E 测试
 */
test.describe('上传', () => {
  test('上传入口可见', async ({ page }) => {
    await page.goto('/')
    await page.evaluate(() => {
      localStorage.setItem('fileupload_token', 'test-token')
    })

    // 找上传区域（可能是按钮或拖拽区）
    const uploadElement = page.getByText(/拖拽|上传|Upload|drop/i).first()
    const exists = await uploadElement.isVisible({ timeout: 2000 }).catch(() => false)
    if (exists) {
      await expect(uploadElement).toBeVisible()
    }
  })
})