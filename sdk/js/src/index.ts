/**
 * @fileupload/sdk — Fileupload 服务 TypeScript SDK
 *
 * 浏览器 + Node.js 双环境支持。
 *
 * ## 快速开始
 *
 * ```typescript
 * import { FileuploadClient } from '@fileupload/sdk'
 *
 * const client = new FileuploadClient({ endpoint: 'http://localhost:8080' })
 *
 * // 上传
 * const file = await client.upload(fileBlob, 'photo.jpg')
 *
 * // 列目录
 * const list = await client.list('/')
 * ```
 */

export { FileuploadClient } from './client'
export { useFileList, useFileUpload } from './hooks'
export type * from './types'
