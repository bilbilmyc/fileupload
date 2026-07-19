import axios, { AxiosInstance, AxiosProgressEvent } from 'axios'
import type {
  ClientConfig,
  FileInfo,
  FileItem,
  ListResult,
  UploadInitResult,
  UploadStatusResult,
  FinalizeResult,
  BatchCopyResult,
  BatchDeleteResult,
  UploadOptions,
  TokenPair,
  UserInfo,
  DirManifest,
  ShareEntry,
  CreateShareRequest,
  SystemStatus,
  AuditLogEntry,
  ScanReport,
} from './types.js'

/**
 * Fileupload SDK 客户端
 *
 * @example
 * ```typescript
 * import { FileuploadClient } from '@fileupload/sdk'
 *
 * const client = new FileuploadClient({ endpoint: 'http://localhost:8080' })
 *
 * // 上传文件
 * const file = await client.upload(fileBlob, 'photo.jpg')
 * console.log('上传完成:', file.file_id)
 *
 * // 列目录
 * const list = await client.list('/')
 * console.log('文件列表:', list.children)
 * ```
 */
export class FileuploadClient {
  private http: AxiosInstance
  private endpoint: string
  private namespace: string
  private token?: string

  constructor(config: ClientConfig = {}) {
    this.endpoint = (config.endpoint || 'http://localhost:8080').replace(/\/+$/, '')
    this.namespace = config.namespace || 'default'

    this.http = axios.create({
      baseURL: this.endpoint,
      timeout: config.timeout || 60000,
      headers: {
        'X-Namespace': this.namespace,
        ...(config.token ? { Authorization: `Bearer ${config.token}`, 'X-Auth-Token': config.token } : {}),
      },
    })

    // 自动注入 token 和 namespace
    this.http.interceptors.request.use((cfg) => {
      cfg.headers['X-Namespace'] = this.namespace
      const token = localStorage?.getItem?.('fileupload_token')
      if (token) {
        cfg.headers['Authorization'] = `Bearer ${token}`
        cfg.headers['X-Auth-Token'] = token
      }
      return cfg
    })
  }

  /** 设置认证令牌 */
  setToken(token: string): void {
    this.token = token
    this.http.defaults.headers['Authorization'] = `Bearer ${token}`
    this.http.defaults.headers['X-Auth-Token'] = token
  }

  /** 获取当前 token */
  getToken(): string | undefined {
    return this.token
  }

  /** 清除认证令牌 */
  clearToken(): void {
    this.token = undefined
    delete this.http.defaults.headers['Authorization']
    delete this.http.defaults.headers['X-Auth-Token']
  }

  // ===== 鉴权 =====

  /** 用户名密码登录，登录成功后自动 setToken */
  async login(username: string, password: string): Promise<TokenPair> {
    const response = await this.http.post('/v1/auth/login', { username, password })
    const pair = response.data as TokenPair
    this.setToken(pair.access_token)
    return pair
  }

  /** 用 refresh token 刷新 access token */
  async refreshToken(refreshToken: string): Promise<TokenPair> {
    const response = await this.http.post('/v1/auth/refresh', { refresh_token: refreshToken })
    const pair = response.data as TokenPair
    this.setToken(pair.access_token)
    return pair
  }

  /** 获取当前登录用户信息 */
  async me(): Promise<UserInfo> {
    const response = await this.http.get('/v1/auth/me')
    return response.data as UserInfo
  }

  // ===== 上传生命周期 =====

  /** 取消 tus 上传（DELETE /uploads/{sessionID}） */
  async cancelUpload(sessionID: string): Promise<void> {
    await this.http.delete(`/uploads/${encodeURIComponent(sessionID)}`)
  }

  /** 查询上传分片状态（GET /v1/uploads/{sessionID}/status） */
  async getUploadStatus(sessionID: string): Promise<UploadStatusResult> {
    const response = await this.http.get(`/v1/uploads/${encodeURIComponent(sessionID)}/status`)
    return response.data as UploadStatusResult
  }

  // ===== 目录 =====

  /** 提交目录 Manifest（POST /v1/dirs） */
  async submitDir(manifest: DirManifest): Promise<{ file_id: string }> {
    const response = await this.http.post('/v1/dirs', manifest)
    return response.data
  }

  // ===== 预览 =====

  /** 获取文件预览 URL（GET /v1/preview/{id}） */
  previewUrl(id: string): string {
    return `${this.endpoint}/v1/preview/${encodeURIComponent(id)}?namespace=${encodeURIComponent(this.namespace)}`
  }

  /** 下载预览（返回 Blob） */
  async preview(id: string): Promise<Blob> {
    const response = await this.http.get(`/v1/preview/${encodeURIComponent(id)}`, {
      responseType: 'blob',
    })
    return response.data as Blob
  }

  // ===== 文件管理 =====

  /** 重命名文件或目录（PATCH /v1/files/{id}） */
  async rename(id: string, newName: string): Promise<void> {
    await this.http.patch(`/v1/files/${encodeURIComponent(id)}`, { name: newName })
  }

  // ===== 分享 =====

  /** 创建分享链接（POST /v1/share） */
  async createShare(req: CreateShareRequest): Promise<ShareEntry> {
    const response = await this.http.post('/v1/share', req)
    return response.data as ShareEntry
  }

  /** 通过 token 访问分享（GET /s/{token}） */
  async accessShare(token: string): Promise<ShareEntry> {
    const response = await this.http.get(`/s/${encodeURIComponent(token)}`)
    return response.data as ShareEntry
  }

  /** 构造分享访问 URL */
  shareUrl(token: string): string {
    return `${this.endpoint}/s/${encodeURIComponent(token)}?namespace=${encodeURIComponent(this.namespace)}`
  }

  // ===== 后台管理 =====

  /** 系统状态（GET /v1/admin/status） */
  async systemStatus(): Promise<SystemStatus> {
    const response = await this.http.get('/v1/admin/status')
    return response.data as SystemStatus
  }

  /** 审计日志（GET /v1/admin/audit） */
  async listAuditLogs(page: number = 1, perPage: number = 50): Promise<{ entries: AuditLogEntry[]; total: number }> {
    const response = await this.http.get('/v1/admin/audit', {
      params: { page, per_page: perPage },
    })
    return response.data
  }

  /** 触发一致性巡检（POST /v1/admin/scan） */
  async triggerScan(): Promise<ScanReport> {
    const response = await this.http.post('/v1/admin/scan')
    return response.data as ScanReport
  }

  /** 设置命名空间 */
  setNamespace(ns: string): void {
    this.namespace = ns
  }

  // ========== 文件操作 ==========

  /** 上传文件 */
  async upload(
    file: Blob | File,
    fileName?: string,
    options: UploadOptions = {},
  ): Promise<FileInfo> {
    const name = fileName || (file instanceof File ? file.name : 'file')
    const size = file.size
    const concurrency = options.concurrency || 4
    const chunkSize = (options.chunkSize || 10 * 1024 * 1024)
    const compression = options.compression || 'none'

    // 计算 SHA-256
    const sha256 = await this.computeSHA256(file)

    // 秒传预检
    const exists = await this.checkExists(sha256, name)
    if (exists) return exists

    // 创建会话
    const init = await this.initUpload(size, sha256, name, compression)

    // 分片上传
    const totalChunks = Math.ceil(size / chunkSize)
    let uploaded = 0

    // 分批并发
    for (let batchStart = 0; batchStart < totalChunks; batchStart += concurrency) {
      const batch = Array.from(
        { length: Math.min(concurrency, totalChunks - batchStart) },
        (_, i) => batchStart + i,
      )
      await Promise.all(
        batch.map(async (idx) => {
          const start = idx * chunkSize
          const end = Math.min(start + chunkSize, size)
          const chunk = file.slice(start, end)
          const sliceSha = await this.chunkSHA256(chunk)
          await this.uploadChunk(init.session_id, idx, chunk, sliceSha)
          uploaded += (end - start)
          options.onProgress?.(uploaded, size)
        }),
      )
    }

    // 完成上传
    return this.finalize(init.session_id)
  }

  /** 下载文件，返回 Blob */
  async download(fileId: string): Promise<Blob> {
    const response = await this.http.get(`/v1/files/${fileId}`, {
      responseType: 'blob',
    })
    return response.data
  }

  /** 获取文件下载 URL */
  downloadUrl(fileId: string): string {
    return `${this.endpoint}/v1/files/${fileId}?namespace=${encodeURIComponent(this.namespace)}`
  }

  /** 获取目录下载 URL */
  downloadDirUrl(dirId: string, format: string = 'tar.gz'): string {
    return `${this.endpoint}/v1/dirs/${dirId}?format=${format}&namespace=${encodeURIComponent(this.namespace)}`
  }

  /** 删除文件 */
  async delete(id: string): Promise<void> {
    await this.http.delete(`/v1/files/${id}`)
  }

  /** 删除目录 */
  async deleteDir(id: string): Promise<void> {
    await this.http.delete(`/v1/dirs/${id}`)
  }

  // ========== 目录操作 ==========

  /** 列目录 */
  async list(parent: string = '/'): Promise<ListResult> {
    const response = await this.http.get('/v1/ls', { params: { parent } })
    return response.data
  }

  /** 获取文件/目录信息 */
  async stat(id: string): Promise<{ file: FileItem; blob?: any }> {
    const response = await this.http.get(`/v1/stat/${id}`)
    return response.data
  }

  // ========== 秒传 ==========

  /** 检查文件是否存在（秒传预检） */
  async checkExists(sha256: string, name?: string): Promise<FileInfo | null> {
    try {
      const response = await this.http.head('/v1/files', {
        params: { sha256, name },
      })
      if (response.status !== 200) return null

      // HEAD 响应体不会传给客户端；优先读取服务端返回的秒传元数据头。
      const fileId = response.headers['x-file-id']
      const fileSizeHeader = response.headers['x-file-size']
      const fileSize = typeof fileSizeHeader === 'string' && fileSizeHeader.trim() !== ''
        ? Number(fileSizeHeader)
        : Number.NaN
      if (
        typeof fileId === 'string' &&
        fileId !== '' &&
        Number.isSafeInteger(fileSize) &&
        fileSize >= 0
      ) {
        return {
          file_id: fileId,
          sha256: String(response.headers['x-file-sha256'] || sha256),
          size: fileSize,
          name: name || '',
        }
      }

      // 兼容自定义 transport 中仍可提供响应体的旧实现。
      const body = response.data as Partial<FileInfo> | undefined
      if (body?.file_id && typeof body.size === 'number') {
        return {
          file_id: body.file_id,
          sha256: body.sha256 || sha256,
          size: body.size,
          name: body.name || name || '',
        }
      }
      throw new Error('check exists response missing X-File-ID or X-File-Size')
    } catch (error) {
      if (axios.isAxiosError(error) && error.response?.status === 404) return null
      throw error
    }
  }

  // ========== 批量操作 ==========

  /** 批量删除 */
  async batchDelete(ids: string[]): Promise<BatchDeleteResult> {
    const response = await this.http.post('/v1/batch/delete', { ids })
    return response.data
  }

  /** 批量移动 */
  async batchMove(ids: string[], targetDirId: string): Promise<void> {
    await this.http.post('/v1/batch/move', { ids, target_dir_id: targetDirId })
  }

  /** 批量复制（v0.1.0+：返回 success/failed 计数） */
  async batchCopy(ids: string[], targetDirId: string): Promise<BatchCopyResult> {
    const response = await this.http.post('/v1/batch/copy', { ids, target_dir_id: targetDirId })
    return response.data as BatchCopyResult
  }

  /** 批量标记 */
  async batchSetTags(ids: string[], tags: string[]): Promise<void> {
    await this.http.post('/v1/batch/tags', { ids, tags })
  }

  /** 批量下载 URL */
  batchDownloadUrl(ids: string[], format: string = 'zip'): string {
    const idStr = ids.join(',')
    return `${this.endpoint}/v1/batch/download?ids=${idStr}&format=${format}&namespace=${encodeURIComponent(this.namespace)}`
  }

  // ========== 上传 API（内部） ==========

  private async initUpload(
    size: number, sha256: string, fileName: string, compression: string,
  ): Promise<UploadInitResult> {
    const response = await this.http.post('/v1/uploads/init', null, {
      params: { size },
      headers: {
        'X-SHA256': sha256,
        'X-Compression': compression,
        'X-File-Name': encodeURIComponent(fileName),
      },
    })
    return response.data
  }

  private async uploadChunk(
    sessionId: string, index: number, chunk: Blob, sliceSha256: string,
  ): Promise<void> {
    await this.http.put(`/v1/uploads/${sessionId}/chunks/${index}`, chunk, {
      headers: {
        'X-Slice-SHA256': sliceSha256,
        'Content-Type': 'application/octet-stream',
      },
    })
  }

  private async finalize(sessionId: string): Promise<FileInfo> {
    const response = await this.http.post(`/v1/uploads/${sessionId}/finalize`)
    return response.data
  }

  private async uploadStatus(sessionId: string): Promise<UploadStatusResult> {
    const response = await this.http.get(`/v1/uploads/${sessionId}/status`)
    return response.data
  }

  // ========== 工具 ==========

  private async computeSHA256(file: Blob): Promise<string> {
    const buf = await file.arrayBuffer()
    const hash = await crypto.subtle.digest('SHA-256', buf)
    return this.hex(hash)
  }

  private async chunkSHA256(chunk: Blob): Promise<string> {
    const buf = await chunk.arrayBuffer()
    const hash = await crypto.subtle.digest('SHA-256', buf)
    return this.hex(hash)
  }

  private hex(buf: ArrayBuffer): string {
    return Array.from(new Uint8Array(buf))
      .map((b) => b.toString(16).padStart(2, '0'))
      .join('')
  }
}
