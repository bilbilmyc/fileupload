import { describe, it, expect } from 'vitest'
import { AxiosError } from 'axios'
import {
  formatBytes,
  formatSpeed,
  parseChunkSize,
  sha256Hex,
  isRetryableError,
  retryDelay,
  MAX_RETRIES,
} from './upload-utils'

describe('formatBytes', () => {
  it('returns "0 B" for 0 or negative', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(-1)).toBe('0 B')
  })

  it('formats B / KB / MB / GB', () => {
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(1024)).toBe('1.0 KB')
    expect(formatBytes(1024 * 1024)).toBe('1.0 MB')
    expect(formatBytes(1024 * 1024 * 1024)).toBe('1.0 GB')
  })

  it('formats with decimals for non-power-of-2', () => {
    expect(formatBytes(1536)).toBe('1.5 KB')
    expect(formatBytes(1024 * 1024 * 2.5)).toBe('2.5 MB')
  })
})

describe('formatSpeed', () => {
  it('returns empty string for non-positive', () => {
    expect(formatSpeed(0)).toBe('')
    expect(formatSpeed(-1)).toBe('')
  })

  it('appends /s to formatBytes', () => {
    expect(formatSpeed(1024)).toBe('1.0 KB/s')
  })
})

describe('parseChunkSize', () => {
  it('returns default 10MB for empty or invalid input', () => {
    expect(parseChunkSize('')).toBe(10 * 1024 * 1024)
    expect(parseChunkSize('abc')).toBe(10 * 1024 * 1024)
  })

  it('parses bare numbers as bytes', () => {
    expect(parseChunkSize('1024')).toBe(1024)
    expect(parseChunkSize('0')).toBe(0)
  })

  it('parses K/M/G unit suffixes', () => {
    expect(parseChunkSize('10k')).toBe(10 * 1024)
    expect(parseChunkSize('10m')).toBe(10 * 1024 * 1024)
    expect(parseChunkSize('1g')).toBe(1024 * 1024 * 1024)
    expect(parseChunkSize('100K')).toBe(100 * 1024) // 大小写不敏感
  })
})

describe('sha256Hex', () => {
  it('produces correct hash for known input', async () => {
    // "abc" 的 SHA-256
    const encoder = new TextEncoder()
    const buf = encoder.encode('abc').buffer
    const hash = await sha256Hex(buf)
    expect(hash).toBe('ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad')
  })

  it('produces 64-char hex string', async () => {
    const buf = new TextEncoder().encode('hello').buffer
    const hash = await sha256Hex(buf)
    expect(hash).toHaveLength(64)
    expect(hash).toMatch(/^[0-9a-f]{64}$/)
  })
})

describe('isRetryableError', () => {
  it('returns true for network errors (no response)', () => {
    const err = new AxiosError('network error')
    // 不设 response → 模拟无响应
    expect(isRetryableError(err)).toBe(true)
  })

  it('returns true for 5xx', () => {
    const err = new AxiosError('server error')
    err.response = { status: 500 } as any
    expect(isRetryableError(err)).toBe(true)
    err.response = { status: 503 } as any
    expect(isRetryableError(err)).toBe(true)
  })

  it('returns true for 408 timeout', () => {
    const err = new AxiosError('timeout')
    err.response = { status: 408 } as any
    expect(isRetryableError(err)).toBe(true)
  })

  it('returns false for 4xx (except 408)', () => {
    const err = new AxiosError('client error')
    err.response = { status: 400 } as any
    expect(isRetryableError(err)).toBe(false)
    err.response = { status: 403 } as any
    expect(isRetryableError(err)).toBe(false)
    err.response = { status: 404 } as any
    expect(isRetryableError(err)).toBe(false)
  })

  it('returns true for non-axios errors', () => {
    expect(isRetryableError(new Error('random'))).toBe(true)
    expect(isRetryableError('string error')).toBe(true)
  })
})

describe('retryDelay', () => {
  it('returns 0 for negative attempt', () => {
    expect(retryDelay(-1)).toBe(0)
  })

  it('returns incremental delays', () => {
    expect(retryDelay(0)).toBe(500)
    expect(retryDelay(1)).toBe(1000)
    expect(retryDelay(2)).toBe(2000)
  })

  it('caps at last delay for attempt >= MAX_RETRIES', () => {
    expect(retryDelay(MAX_RETRIES)).toBe(2000)
    expect(retryDelay(MAX_RETRIES + 5)).toBe(2000)
  })
})