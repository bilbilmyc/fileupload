// web/src/api/sdkAdapter.ts — v0.6.0+ SDK 迁移适配层
//
// 把 web/ 现有的 axios 调用迁移到 @fileupload/sdk。
// 模式：每个 web 风格函数薄包装到 sdkClient 对应方法。
//
// 收益：调用栈统一、可被 e2e mock、所有 API 行为集中到 SDK 维护。

import { sdkClient } from './sdk'

/** 列目录 */
export async function listFilesSDK(parent: string = '/'): Promise<{
  dir: any
  children: any[]
  total: number
}> {
  return sdkClient.list(parent) as any
}

/** 系统状态 */
export async function systemStatusSDK() {
  return sdkClient.systemStatus()
}

/** 触发一致性巡检 */
export async function triggerScanSDK() {
  return sdkClient.triggerScan()
}

/** 查询审计日志 */
export async function listAuditLogsSDK(page: number = 1, perPage: number = 50) {
  return sdkClient.listAuditLogs(page, perPage)
}

/** 当前用户信息 */
export async function meSDK() {
  return sdkClient.me()
}

/**
 * 批量下载（返回 Blob）
 *
 * v0.8.0 新增：sdkClient.batchDownloadUrl 返回 URL，要拿 Blob 仍需 fetch。
 * 这里包装 fetch + blob 让旧调用方无感升级。
 */
export async function batchDownloadBlobSDK(ids: string[], format: string = 'zip'): Promise<Blob> {
  const url = sdkClient.batchDownloadUrl(ids, format)
  const res = await fetch(url, {
    headers: {
      Authorization: `Bearer ${localStorage.getItem('fileupload_token') || ''}`,
      'X-Auth-Token': localStorage.getItem('fileupload_token') || '',
    },
  })
  if (!res.ok) {
    throw new Error(`batch download failed: HTTP ${res.status}`)
  }
  return res.blob()
}