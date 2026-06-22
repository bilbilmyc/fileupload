import axios, { AxiosProgressEvent } from 'axios'

const axiosInstance = axios.create({
  timeout: 60000,
})

axiosInstance.interceptors.request.use((config) => {
  const token = localStorage.getItem('fileupload_token')
  const ns = localStorage.getItem('fileupload_namespace') || 'default'
  if (token) {
    // 同时发送 JWT Bearer 和旧版 X-Auth-Token 头，兼容两种认证方式
    config.headers['Authorization'] = `Bearer ${token}`
    config.headers['X-Auth-Token'] = token
  }
  config.headers['X-Namespace'] = ns
  return config
})

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

export interface ListResult {
  dir: FileItem | null
  children: FileItem[]
  ancestors?: FileItem[]
}

export interface UploadInitResult {
  session_id: string
  chunk_size: number
}

export interface UploadStatusResult {
  chunks: { index: number; sha256: string; size: number }[]
  total: number
}

export interface FinalizeResult {
  file_id: string
  sha256: string
  size: number
  name: string
}

export async function checkHealth(): Promise<boolean> {
  try {
    const r = await axiosInstance.get('/health')
    return r.status === 200
  } catch {
    return false
  }
}

export async function listFiles(parent: string = '/', search?: string): Promise<ListResult> {
  const params: Record<string, string> = { parent }
  if (search) params.search = search
  const r = await axiosInstance.get('/v1/ls', { params })
  return r.data
}

export async function statFile(id: string): Promise<{ file: FileItem; blob?: any }> {
  const r = await axiosInstance.get(`/v1/stat/${id}`)
  return r.data
}

export async function deleteFile(id: string): Promise<void> {
  await axiosInstance.delete(`/v1/files/${id}`)
}

export async function deleteDir(id: string): Promise<void> {
  await axiosInstance.delete(`/v1/dirs/${id}`)
}

export async function checkExists(sha256: string, name?: string): Promise<FileItem | null> {
  try {
    const r = await axiosInstance.head('/v1/files', {
      params: { sha256, name },
    })
    if (r.status === 200) return r.data as FileItem
  } catch {
    // 404 means not found
  }
  return null
}

export async function initUpload(
  size: number,
  sha256: string,
  fileName: string,
  compression: 'none' | 'zstd' = 'none'
): Promise<UploadInitResult> {
  const r = await axiosInstance.post('/v1/uploads/init', null, {
    params: { size },
    headers: {
      'X-SHA256': sha256,
      'X-Compression': compression,
      'X-File-Name': encodeURIComponent(fileName),
    },
  })
  return r.data
}

export async function uploadChunk(
  sessionId: string,
  index: number,
  chunk: Blob,
  sliceSha256: string,
  onProgress?: (progressEvent: AxiosProgressEvent) => void
): Promise<void> {
  await axiosInstance.put(`/v1/uploads/${sessionId}/chunks/${index}`, chunk, {
    headers: {
      'X-Slice-SHA256': sliceSha256,
      'Content-Type': 'application/octet-stream',
    },
    onUploadProgress: onProgress,
  })
}

export async function finalizeUpload(sessionId: string): Promise<FinalizeResult> {
  const r = await axiosInstance.post(`/v1/uploads/${sessionId}/finalize`)
  return r.data
}

export async function uploadStatus(sessionId: string): Promise<UploadStatusResult> {
  const r = await axiosInstance.get(`/v1/uploads/${sessionId}/status`)
  return r.data
}

export function downloadFileUrl(id: string): string {
  return `/v1/files/${id}`
}

export function downloadDirUrl(id: string, format: string = 'tar.gz'): string {
  return `/v1/dirs/${id}?format=${format}`
}

export function previewFileUrl(id: string): string {
  return `/v1/preview/${id}`
}

export async function submitDir(name: string, entries: { path: string; file_id: string }[]): Promise<{ file_id: string }> {
  const r = await axiosInstance.post('/v1/dirs', { name, entries })
  return r.data
}

// ========== 批量操作 ==========

export interface BatchDeleteResult {
  success: number
  failed: number
}

export async function batchDelete(ids: string[]): Promise<BatchDeleteResult> {
  const r = await axiosInstance.post('/v1/batch/delete', { ids })
  return r.data
}

export async function batchDownload(ids: string[], format: string = 'zip'): Promise<Blob> {
  const r = await axiosInstance.post('/v1/batch/download', { ids, format }, {
    responseType: 'blob',
  })
  return r.data
}

export function batchDownloadUrl(): string {
  return '/v1/batch/download'
}

export async function batchMove(ids: string[], targetDirId: string): Promise<void> {
  await axiosInstance.post('/v1/batch/move', { ids, target_dir_id: targetDirId })
}

export async function batchCopy(ids: string[], targetDirId: string): Promise<void> {
  await axiosInstance.post('/v1/batch/copy', { ids, target_dir_id: targetDirId })
}

export async function batchSetTags(ids: string[], tags: string[]): Promise<void> {
  await axiosInstance.post('/v1/batch/tags', { ids, tags })
}
