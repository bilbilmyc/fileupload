// web/src/api/sdkAdapter.ts — v0.6.0 SDK 迁移模式示例
//
// 本文件演示如何把 web/ 现有的 axios 调用（api/client.ts）迁移到 @fileupload/sdk。
// 模式：每个 web 风格函数薄包装到 sdkClient 对应方法。
// 收益：调用栈统一、可被 e2e 测试 mock、所有 API 行为集中到 SDK 维护。
//
// 迁移步骤（其他函数可按此模板逐个迁移）：
//   1. 把 web 风格的"opts 参数"映射到 SDK 风格的"具名参数"
//   2. 调 sdkClient 对应方法
//   3. 把 SDK 返回值映射回 web 风格的返回类型
//
// 注意：api/client.ts 的旧函数仍可用 — 这是渐进迁移，不是破坏式替换。

import { sdkClient } from './sdk'

/**
 * 列目录（SDK 风格 + web 风格参数适配）
 *
 * @param parent 父目录 ID（'' 或 '/' 表示根）
 */
export async function listFilesSDK(parent: string = '/'): Promise<{
  dir: any
  children: any[]
  total: number
}> {
  return sdkClient.list(parent) as any
}

/** 系统状态（直传 SDK） */
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