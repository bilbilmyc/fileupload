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
} from './types'

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
    this.http.defaults.headers['Authorization'] = `Bearer ${token}`
    this.http.defaults.headers['X-Auth-Token'] = token
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
      if (response.status === 200) return response.data as FileInfo
    } catch {
      // 404 = not found
    }
    return null
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
