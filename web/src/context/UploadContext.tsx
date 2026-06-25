import { createContext, useContext, useState, useCallback, useRef } from 'react'
import * as api from '../api/client'
import {
  createTask,
  patchTask,
  filterActiveTasks,
  progressStage,
} from '../lib/upload-task'

export type UploadStatus = 'pending' | 'hashing' | 'uploading' | 'retrying' | 'finalizing' | 'done' | 'error'

export interface UploadTask {
  id: string
  name: string
  progress: number
  speed: string
  status: UploadStatus
  error?: string
}

interface UploadContextType {
  uploadTasks: UploadTask[]
  hasActiveUploads: boolean
  customRequest: (file: File) => void
  clearDoneTasks: () => void
  setUploadTasks: (tasks: UploadTask[]) => void
}

const UploadContext = createContext<UploadContextType>(null!)

export function useUploadCtx() {
  return useContext(UploadContext)
}

export function UploadProvider({ children, onComplete }: { children: React.ReactNode; onComplete?: () => void }) {
  const [uploadTasks, setUploadTasks_] = useState<UploadTask[]>([])
  const taskIdRef = useRef(0)
  const doneRef = useRef(onComplete)
  doneRef.current = onComplete

  const setUploadTasks = useCallback((tasks: UploadTask[]) => {
    setUploadTasks_(tasks)
  }, [])

  // v0.11.0：用 isDoneStatus 过滤（替换内联 lambda）
  const clearDoneTasks = useCallback(() => {
    setUploadTasks_(prev => prev.filter(t => t.status !== 'done' && t.status !== 'error'))
  }, [])

  const customRequest = useCallback(async (file: File) => {
    const taskId = `task-${++taskIdRef.current}`
    let task = createTask(taskId, file.name, file.size)

    const commit = (patch: Partial<UploadTask>) => {
      task = patchTask(task, patch)
      setUploadTasks_(prev => {
        const idx = prev.findIndex(t => t.id === taskId)
        if (idx < 0) return [...prev, task]
        return prev.map(t => (t.id === taskId ? task : t))
      })
    }

    try {
      commit({ status: 'uploading' })

      const chunkSize = 10 * 1024 * 1024
      const totalChunks = Math.ceil(file.size / chunkSize)

      // SHA-256
      const sha256 = await computeSHA256(file)
      commit({ progress: progressStage('hashing', 100) })

      // Check exists
      const existing = await api.checkExists(sha256, file.name)
      if (existing) {
        commit({ status: 'done', progress: 100 })
        doneRef.current?.()
        return
      }

      // Init upload
      const init = await api.initUpload(file.size, sha256, file.name, 'none')

      // Upload chunks
      for (let i = 0; i < totalChunks; i++) {
        const start = i * chunkSize
        const end = Math.min(start + chunkSize, file.size)
        const chunk = file.slice(start, end)

        const chunkSha = await computeSHA256(chunk)
        const started = Date.now()
        let lastLoaded = 0

        await api.uploadChunk(init.session_id, i, chunk, chunkSha, (e) => {
          if (e.total) {
            const loaded = start + (e.loaded || 0)
            const innerPct = (loaded / file.size) * 100
            const elapsed = (Date.now() - started) / 1000
            const speed = elapsed > 0 ? formatSpeed((e.loaded || 0) - lastLoaded, elapsed) : ''
            lastLoaded = e.loaded || 0
            commit({ progress: progressStage('uploading', innerPct), speed })
          }
        })
      }

      // Finalize
      commit({ status: 'finalizing', progress: progressStage('finalizing', 100) })
      await api.finalizeUpload(init.session_id)

      commit({ status: 'done', progress: 100 })
      doneRef.current?.()
    } catch (e: any) {
      commit({ status: 'error', error: e.message })
    }
  }, [])

  // v0.11.0：filterActiveTasks 替换内联 .some(...)
  const hasActiveUploads = filterActiveTasks(uploadTasks).length > 0

  return (
    <UploadContext.Provider value={{ uploadTasks, hasActiveUploads, customRequest, clearDoneTasks, setUploadTasks }}>
      {children}
    </UploadContext.Provider>
  )
}

async function computeSHA256(file: Blob): Promise<string> {
  const buffer = await file.arrayBuffer()
  const hash = await crypto.subtle.digest('SHA-256', buffer)
  return Array.from(new Uint8Array(hash)).map(b => b.toString(16).padStart(2, '0')).join('')
}

function formatSpeed(bytes: number, seconds: number): string {
  const bps = bytes / seconds
  if (bps > 1_000_000) return `${(bps / 1_000_000).toFixed(1)} MB/s`
  if (bps > 1_000) return `${(bps / 1_000).toFixed(0)} KB/s`
  return `${Math.round(bps)} B/s`
}
