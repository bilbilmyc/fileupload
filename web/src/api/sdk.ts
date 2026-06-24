// web/src/api/sdk.ts — Frontend SDK 集成层
//
// 这是 web/ 接入 @fileupload/sdk 的最小可工作示例。
// 当前 web/src/api/client.ts 仍用直接的 axios 调用 — 那是 legacy 路径。
// 本文件演示如何用 SDK 替代它，模式可逐步迁移。
//
// 用法：
//   import { sdkClient } from './api/sdk'
//   const me = await sdkClient.me()
//   const status = await sdkClient.systemStatus()

import { FileuploadClient } from '@fileupload/sdk'

/**
 * sdkClient 是带 web 端 auth 拦截器的 FileuploadClient 单例。
 *
 * 为什么需要单独包一层：
 * - web 端 token 存在 localStorage（key: fileupload_token）
 * - FileuploadClient.setToken() 已实现，但需要从 localStorage 读
 * - 这里封装一个 getClient() lazy-init 模式，避免每次都构造
 */
let _client: FileuploadClient | null = null

function getClient(): FileuploadClient {
  if (_client) return _client

  // 推断 endpoint：当前页面 location.origin + '' (同源代理)
  // 生产可改为 import.meta.env.VITE_API_BASE
  const endpoint = (import.meta as any).env?.VITE_API_BASE || window.location.origin

  const token = localStorage.getItem('fileupload_token') || undefined
  const namespace = localStorage.getItem('fileupload_namespace') || 'default'

  _client = new FileuploadClient({ endpoint, namespace })
  if (token) {
    _client.setToken(token)
  }
  return _client
}

/** 暴露一个 Proxy 让 sdkClient 的访问总是走 getClient() lazy-init */
export const sdkClient: FileuploadClient = new Proxy({} as FileuploadClient, {
  get(_target, prop: string | symbol) {
    const client = getClient()
    const value = (client as any)[prop]
    return typeof value === 'function' ? value.bind(client) : value
  },
})

/** token 变化时（登录/登出）调用此函数清缓存 */
export function refreshSDKClient(): void {
  _client = null
}