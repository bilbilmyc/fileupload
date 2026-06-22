/** 文件项 */
export interface FileItem {
  file_id: string
  name: string
  path?: string
  size: number
  sha256?: string
  is_dir: boolean
  parent_id?: string
  tags?: string[]
  created_at: string
}

/** 目录列表结果 */
export interface ListResult {
  dir: FileItem | null
  children: FileItem[]
  ancestors?: FileItem[]
}

/** 上传初始化结果 */
export interface UploadInitResult {
  session_id: string
  chunk_size: number
}

/** 上传状态 */
export interface UploadStatusResult {
  chunks: { index: number; sha256: string; size: number }[]
  total: number
}

/** 完成上传结果 */
export interface FinalizeResult {
  file_id: string
  sha256: string
  size: number
  name: string
}

/** 批量删除结果 */
export interface BatchDeleteResult {
  success: number
  failed: number
}

/** 文件信息 */
export interface FileInfo {
  file_id: string
  sha256: string
  size: number
  name: string
}

/** 上传选项 */
export interface UploadOptions {
  /** 并发数 (默认 4) */
  concurrency?: number
  /** 压缩方式: "none" | "zstd" */
  compression?: 'none' | 'zstd'
  /** 分片大小 (默认 10MB) */
  chunkSize?: number
  /** 进度回调 */
  onProgress?: (bytesUploaded: number, totalBytes: number) => void
}

/** 客户端配置 */
export interface ClientConfig {
  /** 服务端地址 (默认 http://localhost:8080) */
  endpoint?: string
  /** JWT 令牌 */
  token?: string
  /** 命名空间 (默认 default) */
  namespace?: string
  /** 请求超时毫秒 (默认 60000) */
  timeout?: number
}
