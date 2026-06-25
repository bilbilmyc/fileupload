// web/src/lib/upload-utils.ts — v0.10.0+ 纯函数抽取
//
// 把 useUpload.ts 中的纯函数（无 React 依赖）提到这里，
// 便于 vitest 单测覆盖。

import axios from 'axios'

/** 字节格式化：1024 → "1.0 KB" */
export function formatBytes(bytes: number): string {
  if (!bytes || bytes < 0 || !Number.isFinite(bytes)) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)} ${units[i]}`
}

/** 速度格式化："1.0 KB/s" */
export function formatSpeed(bytesPerSec: number): string {
  if (bytesPerSec <= 0) return ''
  return formatBytes(bytesPerSec) + '/s'
}

/** 解析 chunk size 字符串："10m" → 10485760；非法 → 默认 10MB */
export function parseChunkSize(v: string): number {
  if (!v) return 10 * 1024 * 1024
  const match = v.match(/^(\d+)([kmg]?)$/i)
  if (!match) return 10 * 1024 * 1024
  const n = parseInt(match[1], 10)
  const unit = match[2].toLowerCase()
  switch (unit) {
    case 'k': return n * 1024
    case 'm': return n * 1024 * 1024
    case 'g': return n * 1024 * 1024 * 1024
    default: return n
  }
}

/** ArrayBuffer → 64 字符 hex SHA-256 */
export function sha256Hex(buffer: ArrayBuffer): Promise<string> {
  return crypto.subtle.digest('SHA-256', buffer).then((hash) => {
    const arr = new Uint8Array(hash)
    return Array.from(arr).map((b) => b.toString(16).padStart(2, '0')).join('')
  })
}

/** 判断错误是否可重试：网络错误 / 5xx / 408 → 可重试；4xx → 不可重试 */
export function isRetryableError(error: unknown): boolean {
  if (axios.isAxiosError(error)) {
    if (!error.response) return true // 网络错误
    const status = error.response.status
    return status >= 500 || status === 408
  }
  return true // 非 axios 错误也尝试重试
}

export const MAX_RETRIES = 3
export const RETRY_DELAYS_MS = [500, 1000, 2000]

/** 退避延迟：第 N 次重试前等待 ms（封顶 RETRY_DELAYS_MS 最后一个值） */
export function retryDelay(attempt: number): number {
  if (attempt < 0) return 0
  if (attempt >= RETRY_DELAYS_MS.length) return RETRY_DELAYS_MS[RETRY_DELAYS_MS.length - 1]
  return RETRY_DELAYS_MS[attempt]
}