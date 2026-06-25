import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { AuthProvider } from '../context/AuthContext'
import Admin from './Admin'

// RED：Admin.tsx 应该是空壳（占位 0）。本测试 RED。
describe('Admin (RED — should fetch real data)', () => {
  let origFetch: typeof global.fetch

  beforeEach(() => {
    origFetch = global.fetch
    localStorage.setItem('fileupload_token', 'test-token')
    localStorage.setItem('fileupload_namespace', 'default')
  })

  afterEach(() => {
    global.fetch = origFetch
  })

  it('fetches /v1/admin/status and displays file/dir counts', async () => {
    // mock fetch 返回系统状态
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        version: 'v0.11.2',
        storage: { total_files: 42, total_blobs: 17, total_size: 1048576 },
        database: { type: 'sqlite' },
        worker_pool: { capacity: 8, available: 7 },
      }),
    } as any)

    render(
      <AuthProvider>
        <Admin />
      </AuthProvider>
    )

    // 等待 fetch 完成 → 数字 42 / 17 / 1.0 MB 应出现
    await waitFor(() => {
      expect(screen.getByText('42')).toBeInTheDocument()
    })
  })
})
