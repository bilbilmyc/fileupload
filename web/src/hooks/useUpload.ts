import { useState, useCallback, useRef } from 'react'
import type { RcFile } from 'antd/es/upload'
import * as api from '../api/client'
import type { UploadInitResult } from '../api/client'

export interface UploadTask {
  id: string
  name: string
  size: number
  status: 'pending' | 'hashing' | 'uploading' | 'finalizing' | 'done' | 'error'
  progress: number
  error?: string
  speed?: string
}

function formatBytes(bytes: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)} ${units[i]}`
}

function sha256Hex(buffer: ArrayBuffer): string {
  const arr = new Uint8Array(buffer)
  return Array.from(arr)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}

async function fileSHA256(file: Blob): Promise<string> {
  const buf = await file.arrayBuffer()
  const hash = await crypto.subtle.digest('SHA-256', buf)
  return sha256Hex(hash)
}

async function chunkSHA256(chunk: ArrayBuffer): Promise<string> {
  const hash = await crypto.subtle.digest('SHA-256', chunk)
  return sha256Hex(hash)
}

function parseChunkSize(v: string): number {
  if (!v) return 10 * 1024 * 1024
  const match = v.match(/^(\d+)([kmg]?)$/i)
  if (!match) return 10 * 1024 * 1024
  const n = parseInt(match[1], 10)
  const unit = match[2].toLowerCase()
  switch (unit) {
    case 'k': return n * 1024
    case 'm': return n * 1024 * 1024
    case 'g': return n * 1024 * 1024 * 1024
    default: return n
  }
}

export function useUpload(onUploadComplete: () => void) {
  const [uploadTasks, setUploadTasks] = useState<UploadTask[]>([])
  const [dirMode, setDirMode] = useState(false)
  const [showUpload, setShowUpload] = useState(false)
  const [chunkSize, setChunkSize] = useState('10m')
  const [concurrency, setConcurrency] = useState<number | 'auto'>(4)
  const [compression, setCompression] = useState<'none' | 'zstd'>('none')
  const [configOpen, setConfigOpen] = useState(false)

  const dirBatches = useRef<Map<string, {
    dirName: string
    entries: { path: string; file_id: string }[]
    total: number
    done: number
    taskId: string
  }>>(new Map())

  const updateTask = useCallback((id: string, patch: Partial<UploadTask>) => {
    setUploadTasks((prev) =>
      prev.map((t) => (t.id === id ? { ...t, ...patch } : t))
    )
  }, [])

  const addTask = useCallback((file: RcFile, dirName?: string): string => {
    const id = `${file.name}-${Date.now()}-${Math.random()}`
    setUploadTasks((prev) => [...prev, {
      id,
      name: dirName ? `📁 ${dirName} — ${file.name}` : file.name,
      size: file.size,
      status: 'pending' as const,
      progress: 0,
    }])
    return id
  }, [])

  const clearDoneTasks = useCallback(() => {
    setUploadTasks((prev) => prev.filter(t => t.status !== 'done'))
  }, [])

  const hasActiveUploads = uploadTasks.some(
    t => t.status === 'uploading' || t.status === 'hashing' || t.status === 'finalizing'
  )

  const recordDirFile = useCallback((dirName: string, relPath: string, fileId: string) => {
    const batch = dirBatches.current.get(dirName)
    if (!batch) return
    batch.entries.push({ path: relPath, file_id: fileId })
    batch.done++
    updateTask(batch.taskId, { progress: Math.round((batch.done / batch.total) * 100) })
    if (batch.done >= batch.total) {
      updateTask(batch.taskId, { status: 'finalizing', progress: 99 })
      api.submitDir(batch.dirName, batch.entries).then(() => {
        updateTask(batch.taskId, { status: 'done', progress: 100, name: `📁 ${batch.dirName}` })
        dirBatches.current.delete(dirName)
        dirBatches.current.delete(batch.taskId)
        onUploadComplete()
      }).catch((e: any) => {
        updateTask(batch.taskId, { status: 'error', error: e.message })
      })
    }
  }, [updateTask, onUploadComplete])

  const uploadSingleFile = useCallback(async (
    file: RcFile,
    taskId: string,
    dirRelPath?: string,
    dirName?: string
  ) => {
    const size = file.size
    updateTask(taskId, { status: 'hashing', progress: 0 })

    let sha256 = ''
    try {
      sha256 = await fileSHA256(file)
    } catch (e) {
      console.warn('SHA-256 计算失败', e)
    }

    if (sha256) {
      try {
        const exists = await api.checkExists(sha256, file.name)
        if (exists) {
          updateTask(taskId, { status: 'done', progress: 100 })
          if (dirRelPath && dirName) recordDirFile(dirName, dirRelPath, exists.file_id)
          else onUploadComplete()
          return
        }
      } catch {
        // continue if check fails
      }
    }

    const chunkBytes = parseChunkSize(chunkSize)
    let init: UploadInitResult
    try {
      init = await api.initUpload(size, sha256, file.name, compression)
    } catch (e: any) {
      updateTask(taskId, { status: 'error', error: e.message })
      throw e
    }

    const totalChunks = Math.ceil(size / chunkBytes)
    const startTime = Date.now()
    let uploaded = 0

    for (let i = 0; i < totalChunks; i++) {
      const start = i * chunkBytes
      const end = Math.min(start + chunkBytes, size)
      const chunk = file.slice(start, end)
      const sliceSha = await chunkSHA256(await chunk.arrayBuffer())

      await api.uploadChunk(init.session_id, i, chunk, sliceSha, (ev) => {
        if (ev.total) {
          const pct = Math.min(100, Math.round(((uploaded + ev.loaded) / size) * 100))
          const elapsed = (Date.now() - startTime) / 1000
          const speed = elapsed > 0 ? formatBytes((uploaded + ev.loaded) / elapsed) + '/s' : ''
          updateTask(taskId, { status: 'uploading', progress: pct, speed })
        }
      })
      uploaded += end - start
    }

    updateTask(taskId, { status: 'finalizing', progress: 99 })
    try {
      const result = await api.finalizeUpload(init.session_id)
      updateTask(taskId, { status: 'done', progress: 100 })
      if (dirRelPath && dirName) {
        recordDirFile(dirName, dirRelPath, result.file_id)
      } else {
        if (!showUpload) setShowUpload(true)
        onUploadComplete()
      }
    } catch (e: any) {
      updateTask(taskId, { status: 'error', error: e.message })
      throw e
    }
  }, [chunkSize, compression, updateTask, recordDirFile, onUploadComplete, showUpload])

  const customRequest = useCallback(async (options: any) => {
    const { file, onSuccess, onError } = options
    if (dirMode && (file as RcFile).webkitRelativePath) {
      const parts = (file as RcFile).webkitRelativePath.split('/')
      const dirName = parts[0]
      const relPath = parts.slice(1).join('/')
      let batch = dirBatches.current.get(dirName)
      if (!batch) {
        const placeholderId = `dir-${dirName}-${Date.now()}`
        setUploadTasks((prev) => [...prev, {
          id: placeholderId,
          name: `📁 ${dirName}`,
          size: 0,
          status: 'pending' as const,
          progress: 0,
        }])
        batch = { dirName, entries: [], total: 0, done: 0, taskId: placeholderId }
        dirBatches.current.set(dirName, batch)
        dirBatches.current.set(placeholderId, batch)
      }
      batch.total++
      const taskId = addTask(file, dirName)
      try {
        await uploadSingleFile(file, taskId, relPath, dirName)
        onSuccess?.()
      } catch (e: any) {
        updateTask(taskId, { status: 'error', error: (e as Error).message })
        onError?.(e)
      }
    } else {
      const taskId = addTask(file as RcFile)
      try {
        await uploadSingleFile(file as RcFile, taskId)
        onSuccess?.()
      } catch (e: any) {
        updateTask(taskId, { status: 'error', error: (e as Error).message })
        onError?.(e)
      }
    }
  }, [dirMode, addTask, uploadSingleFile, updateTask])

  return {
    uploadTasks,
    dirMode,
    showUpload,
    chunkSize,
    concurrency,
    compression,
    configOpen,
    hasActiveUploads,
    setDirMode,
    setShowUpload,
    setChunkSize,
    setConcurrency,
    setCompression,
    setConfigOpen,
    customRequest,
    clearDoneTasks,
  }
}
