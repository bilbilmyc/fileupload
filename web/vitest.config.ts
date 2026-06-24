import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test-setup.ts'],
    // v0.7.0：排除 Playwright e2e 文件（由 `npm run e2e` 单独跑）
    exclude: ['**/node_modules/**', '**/dist/**', 'e2e/**'],
  },
})
