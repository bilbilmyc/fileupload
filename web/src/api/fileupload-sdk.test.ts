import { describe, expect, it, vi } from 'vitest'
import { FileuploadClient } from '@fileupload/sdk'

type HeadOnlyHTTP = {
  head: (path: string, config?: unknown) => Promise<unknown>
}

function clientWithHead(head: HeadOnlyHTTP['head']): FileuploadClient {
  const client = new FileuploadClient({ endpoint: 'http://example.test' })
  ;(client as unknown as { http: HeadOnlyHTTP }).http = { head }
  return client
}

describe('FileuploadClient.checkExists', () => {
  it('reads instant-upload metadata from HEAD response headers', async () => {
    const head = vi.fn().mockResolvedValue({
      status: 200,
      headers: {
        'x-file-id': 'instant-file',
        'x-file-sha256': 'server-sha',
        'x-file-size': '42',
      },
    })
    const client = clientWithHead(head)

    await expect(client.checkExists('request-sha', 'photo.jpg')).resolves.toEqual({
      file_id: 'instant-file',
      sha256: 'server-sha',
      size: 42,
      name: 'photo.jpg',
    })
    expect(head).toHaveBeenCalledWith('/v1/files', {
      params: { sha256: 'request-sha', name: 'photo.jpg' },
    })
  })

  it('returns null only for a 404 response', async () => {
    const client = clientWithHead(vi.fn().mockRejectedValue({
      isAxiosError: true,
      response: { status: 404 },
    }))

    await expect(client.checkExists('missing-sha')).resolves.toBeNull()
  })

  it('does not hide network failures', async () => {
    const networkError = Object.assign(new Error('connection reset'), { isAxiosError: true })
    const client = clientWithHead(vi.fn().mockRejectedValue(networkError))

    await expect(client.checkExists('sha')).rejects.toBe(networkError)
  })

  it('rejects missing or invalid metadata headers', async () => {
    const client = clientWithHead(vi.fn().mockResolvedValue({
      status: 200,
      headers: { 'x-file-id': 'instant-file', 'x-file-size': '' },
    }))

    await expect(client.checkExists('sha')).rejects.toThrow(
      'missing X-File-ID or X-File-Size',
    )
  })
})
