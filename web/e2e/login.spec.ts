import { test, expect, type Page } from '@playwright/test'

const loginPath = '**/v1/auth/login'

async function mockLoginAPI(page: Page) {
  await page.route('**/v1/ls?**', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ children: [], ancestors: [], total: 0 }),
  }))

  await page.route(loginPath, async (route) => {
    const credentials = route.request().postDataJSON() as {
      username?: string
      password?: string
    }

    if (credentials.username === 'admin' && credentials.password === 'admin123') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          access_token: 'e2e-access-token',
          namespace: 'default',
          user_id: 'e2e-admin',
        }),
      })
      return
    }

    await route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: JSON.stringify({ error: '用户名或密码错误' }),
    })
  })
}

test.describe('登录流程', () => {
  test.beforeEach(async ({ page }) => {
    await mockLoginAPI(page)
    await page.goto('/login')
    await page.evaluate(() => localStorage.clear())
    await page.reload()
  })

  test('登录页可见，含用户名/密码字段', async ({ page }) => {
    await expect(page.getByPlaceholder(/用户名/)).toBeVisible()
    await expect(page.getByPlaceholder(/密码/)).toBeVisible()
    await expect(page.getByRole('button', { name: /^login 登录$/i })).toBeVisible()
  })

  test('空字段提交显示验证错误', async ({ page }) => {
    await page.getByRole('button', { name: /^login 登录$/i }).click()
    await expect(page.getByText('请输入用户名和密码')).toBeVisible()
  })

  test('错误密码显示错误提示', async ({ page }) => {
    await page.getByPlaceholder(/用户名/).fill('admin')
    await page.getByPlaceholder(/密码/).fill('wrong-password')
    await page.getByRole('button', { name: /^login 登录$/i }).click()

    await expect(page.getByText('用户名或密码错误')).toBeVisible()
  })

  test('登录成功后跳转到文件管理', async ({ page }) => {
    await page.getByPlaceholder(/用户名/).fill('admin')
    await page.getByPlaceholder(/密码/).fill('admin123')
    await page.getByRole('button', { name: /^login 登录$/i }).click()

    await page.waitForURL((url) => !url.pathname.includes('/login'))
    await expect.poll(
      () => page.evaluate(() => localStorage.getItem('fileupload_token')),
    ).toBe('e2e-access-token')
  })
})
