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

/** 批量复制结果（v0.1.0+） */
export interface BatchCopyResult {
  success: number
  failed: number
}

/** 登录/刷新令牌对 */
export interface TokenPair {
  access_token: string
  refresh_token: string
  expires_in: number
}

/** 当前登录用户信息 */
export interface UserInfo {
  user_id: string
  namespace: string
  roles: string[]
}

/** 上传分片状态 */
export interface UploadStatusResult {
  session_id: string
  chunks: Array<{ index: number; sha256: string; size: number }>
  total: number
}

/** 目录 Manifest */
export interface DirManifest {
  name?: string
  entries: Array<{ path: string; file_id: string; size?: number; sha256?: string }>
}

/** 分享链接 */
export interface ShareEntry {
  token: string
  file_id: string
  expires_at?: string
  max_downloads: number
  cur_downloads: number
  namespace: string
}

/** 创建分享请求 */
export interface CreateShareRequest {
  file_id: string
  password?: string
  expires_in: number
  max_downloads: number
}

/** 系统状态 */
export interface SystemStatus {
  version: string
  start_time: string
  uptime: string
  storage?: Record<string, any>
  metadata?: Record<string, any>
  counts?: Record<string, number>
}

/** 审计日志条目 */
export interface AuditLogEntry {
  id: string
  target_type: string
  target_id: string
  namespace: string
  detail: string
  created_at: string
}

/** 一致性巡检报告 */
export interface ScanReport {
  orphan_parts: number
  orphan_files: string[]
  metadata_orphans: number
  ref_count_fixes: number
  corrupted_files: string[]
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
