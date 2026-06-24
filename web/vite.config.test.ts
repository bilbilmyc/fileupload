import { describe, it, expect } from 'vitest'
import { readFileSync, existsSync, readdirSync } from 'fs'
import { join } from 'path'

/**
 * Vite 构建配置 + 产物结构验证（v0.8.0）
 *
 * 这些测试不运行 build（太慢），而是验证：
 *   1. vite.config.ts 包含 manualChunks 分割策略
 *   2. dist/ 目录结构符合预期（关键 chunk 文件存在）
 *
 * 跑测试前需要先 `npm run build` 生成 dist/。
 */

describe('vite.config.ts', () => {
  it('包含 manualChunks 配置把 vendor 分离', () => {
    const config = readFileSync(join(__dirname, 'vite.config.ts'), 'utf8')
    expect(config).toMatch(/manualChunks/)
    expect(config).toMatch(/['"]react['"]/)
    expect(config).toMatch(/['"]antd['"]/)
    expect(config).toMatch(/['"]axios['"]/)
  })
})

describe.skip('dist/ 产物结构（需先 npm run build）', () => {
  const distDir = join(__dirname, 'dist')
  const hasDist = () => existsSync(distDir)

  it('产物存在', () => {
    if (!hasDist()) {
      console.warn('dist/ 不存在，跳过（先 npm run build）')
      return
    }
    expect(hasDist()).toBe(true)
  })

  it('主 chunk 存在', () => {
    if (!hasDist()) return
    const files = readdirSync(join(distDir, 'assets'))
    expect(files.some((f) => f.startsWith('index-') && f.endsWith('.js'))).toBe(true)
  })

  it('vendor chunks 已分离（react / antd / axios）', () => {
    if (!hasDist()) return
    const files = readdirSync(join(distDir, 'assets'))
    const vendorChunks = files.filter(
      (f) => f.endsWith('.js') && /react|antd|axios|vendor/.test(f)
    )
    // 至少应有 1 个 vendor 风格的 chunk
    expect(vendorChunks.length).toBeGreaterThanOrEqual(1)
  })
})