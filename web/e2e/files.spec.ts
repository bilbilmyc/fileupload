import { test, expect, type Page } from '@playwright/test'

async function mockFileList(page: Page) {
  await page.route('**/v1/ls?**', (route) => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ children: [], ancestors: [], total: 0 }),
  }))
}

test.describe('文件管理', () => {
  test.beforeEach(async ({ page }) => {
    await mockFileList(page)
    await page.goto('/')
    await page.evaluate(() => {
      localStorage.setItem('fileupload_token', 'test-token')
      localStorage.setItem('fileupload_namespace', 'default')
      localStorage.setItem('fileupload_user', 'admin')
    })
  })

  test('文件管理页可见，含工具栏', async ({ page }) => {
    await page.goto('/')
    await expect(page.getByRole('heading', { name: /文件|File/i }).first()).toBeVisible()
  })

  test('上传按钮可见且可点击', async ({ page }) => {
    await page.goto('/')
    const uploadButton = page.getByRole('button', { name: /上传|Upload/i }).first()
    await expect(uploadButton).toBeVisible()
    await uploadButton.click()
  })
})

test.describe('上传', () => {
  test('上传入口可见', async ({ page }) => {
    await mockFileList(page)
    await page.goto('/')

    const uploadElement = page.getByText(/拖拽|上传|Upload|drop/i).first()
    await expect(uploadElement).toBeVisible()
  })
})
