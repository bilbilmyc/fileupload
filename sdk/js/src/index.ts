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
// React hooks（useFileList, useFileUpload）从 './hooks/react' 单独导出，
// 不在此 barrel — 避免非 React 消费者引入 react peer dep。
export type * from './types'
