import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock @fileupload/sdk
const mockLoginFn = vi.fn().mockResolvedValue({ access_token: 'tok-from-login' })
const mockMeFn = vi.fn().mockResolvedValue({ user_id: 'u1' })

vi.mock('@fileupload/sdk', () => ({
  FileuploadClient: vi.fn().mockImplementation(function (this: any, config: any) {
    this.config = config
    this.token = undefined
    this.setToken = vi.fn((t: string) => { this.token = t })
    this.login = mockLoginFn
    this.me = mockMeFn
  }),
}))

describe('sdkClient wrapper', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
  })

  it('does not construct FileuploadClient until first access', async () => {
    const { FileuploadClient } = await import('@fileupload/sdk')
    const { sdkClient } = await import('./sdk')

    expect(FileuploadClient).not.toHaveBeenCalled()
    void (sdkClient as any).me
    expect(FileuploadClient).toHaveBeenCalledTimes(1)
  })

  it('initializes FileuploadClient with token from localStorage', async () => {
    localStorage.setItem('fileupload_token', 'local-tok')
    localStorage.setItem('fileupload_namespace', 'my-ns')

    const { sdkClient } = await import('./sdk')

    // 验证 sdkClient 构造时 token 已自动 set（lazy-init 时从 localStorage 读）
    void (sdkClient as any).me
    // mock 实例的 setToken 被调用过一次
    expect((sdkClient as any).token).toBe('local-tok')
  })

  it('forwards method calls to underlying FileuploadClient', async () => {
    const { sdkClient } = await import('./sdk')

    const result = await (sdkClient as any).login('alice', 'secret')
    expect(result).toEqual({ access_token: 'tok-from-login' })
    expect(mockLoginFn).toHaveBeenCalledWith('alice', 'secret')
  })

  it('refreshSDKClient clears cache so next access reinitializes', async () => {
    const { FileuploadClient } = await import('@fileupload/sdk')
    const { sdkClient, refreshSDKClient } = await import('./sdk')

    void (sdkClient as any).me
    const initialCount = (FileuploadClient as any).mock.calls.length

    refreshSDKClient()

    void (sdkClient as any).me
    expect((FileuploadClient as any).mock.calls.length).toBe(initialCount + 1)
  })
})