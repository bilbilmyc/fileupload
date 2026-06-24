import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock factory 每次调用重新创建（用 vi.hoisted 让 mock 在模块加载前可用）
const { mockLoginFn, mockMeFn, mockFileuploadClient } = vi.hoisted(() => ({
  mockLoginFn: vi.fn().mockResolvedValue({ access_token: 'tok-from-login' }),
  mockMeFn: vi.fn().mockResolvedValue({ user_id: 'u1' }),
  mockFileuploadClient: vi.fn().mockImplementation(function (this: any, config: any) {
    this.config = config
    this.token = undefined
    this.setToken = vi.fn((t: string) => { this.token = t })
    this.login = mockLoginFn
    this.me = mockMeFn
  }),
}))

vi.mock('@fileupload/sdk', () => ({
  FileuploadClient: mockFileuploadClient,
}))

describe('sdkClient wrapper', () => {
  beforeEach(async () => {
    localStorage.clear()
    mockFileuploadClient.mockClear()
    mockLoginFn.mockClear()
    mockMeFn.mockClear()
    // 关键：清模块缓存，否则 sdk.ts 中的 _client 单例 cache 跨测试复用
    vi.resetModules()
  })

  it('does not construct FileuploadClient until first access', async () => {
    const { sdkClient } = await import('./sdk')

    expect(mockFileuploadClient).not.toHaveBeenCalled()
    void (sdkClient as any).me
    expect(mockFileuploadClient).toHaveBeenCalledTimes(1)
  })

  it('initializes FileuploadClient with token from localStorage', async () => {
    localStorage.setItem('fileupload_token', 'local-tok')
    localStorage.setItem('fileupload_namespace', 'my-ns')

    const { sdkClient } = await import('./sdk')

    void (sdkClient as any).me

    expect(mockFileuploadClient).toHaveBeenCalledWith(
      expect.objectContaining({ namespace: 'my-ns' })
    )
  })

  it('forwards method calls to underlying FileuploadClient', async () => {
    const { sdkClient } = await import('./sdk')

    const result = await (sdkClient as any).login('alice', 'secret')
    expect(result).toEqual({ access_token: 'tok-from-login' })
    expect(mockLoginFn).toHaveBeenCalledWith('alice', 'secret')
  })

  it('refreshSDKClient clears cache so next access reinitializes', async () => {
    const { sdkClient, refreshSDKClient } = await import('./sdk')

    void (sdkClient as any).me
    expect(mockFileuploadClient).toHaveBeenCalledTimes(1)

    refreshSDKClient()

    void (sdkClient as any).me
    expect(mockFileuploadClient).toHaveBeenCalledTimes(2)
  })
})