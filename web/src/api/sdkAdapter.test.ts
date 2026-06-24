import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock @fileupload/sdk
const { mockList, mockBatchDownloadUrl, mockMockClient } = vi.hoisted(() => {
  const mockList = vi.fn().mockResolvedValue({ dir: null, children: [], total: 0 })
  const mockBatchDownloadUrl = vi.fn().mockImplementation(
    (ids: string[], format: string) => `http://example.com/v1/batch/download?ids=${ids.join(',')}&format=${format}`
  )
  const mockMockClient = vi.fn().mockImplementation(function (this: any) {
    this.list = mockList
    this.batchDownloadUrl = mockBatchDownloadUrl
  })
  return { mockList, mockBatchDownloadUrl, mockMockClient }
})

vi.mock('@fileupload/sdk', () => ({
  FileuploadClient: mockMockClient,
}))

// Mock fetch for blob download
const mockFetch = vi.fn()
vi.stubGlobal('fetch', mockFetch)

import { listFilesSDK, batchDownloadBlobSDK } from './sdkAdapter'

describe('sdkAdapter', () => {
  beforeEach(() => {
    localStorage.clear()
    mockList.mockClear()
    mockBatchDownloadUrl.mockClear()
    mockMockClient.mockClear()
    mockFetch.mockClear()
    vi.resetModules()
  })

  describe('listFilesSDK', () => {
    it('calls sdkClient.list with parent parameter', async () => {
      await listFilesSDK('/root')
      expect(mockList).toHaveBeenCalledWith('/root')
    })

    it('defaults parent to "/"', async () => {
      await listFilesSDK()
      expect(mockList).toHaveBeenCalledWith('/')
    })
  })

  describe('batchDownloadBlobSDK', () => {
    it('builds URL via sdkClient.batchDownloadUrl', async () => {
      mockFetch.mockResolvedValue({ ok: true, status: 200, blob: async () => new Blob() })
      await batchDownloadBlobSDK(['id1', 'id2'], 'zip')
      expect(mockBatchDownloadUrl).toHaveBeenCalledWith(['id1', 'id2'], 'zip')
    })

    it('sends Authorization header with token from localStorage', async () => {
      localStorage.setItem('fileupload_token', 'test-token')
      mockFetch.mockResolvedValue({ ok: true, status: 200, blob: async () => new Blob() })
      await batchDownloadBlobSDK(['id1'], 'zip')
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining('/v1/batch/download'),
        expect.objectContaining({
          headers: expect.objectContaining({
            Authorization: 'Bearer test-token',
          }),
        })
      )
    })

    it('returns Blob on success', async () => {
      // 用简单对象 mock fetch — jsdom 的 Response blob() 不可靠
      const fakeBlob = new Blob(['zipdata'])
      mockFetch.mockResolvedValue({ ok: true, status: 200, blob: async () => fakeBlob })
      const blob = await batchDownloadBlobSDK(['id1'], 'zip')
      expect(blob).toBeInstanceOf(Blob)
    })

    it('throws on HTTP error', async () => {
      mockFetch.mockResolvedValue({ ok: false, status: 500, blob: async () => new Blob() })
      await expect(batchDownloadBlobSDK(['id1'], 'zip')).rejects.toThrow(/500/)
    })
  })
})