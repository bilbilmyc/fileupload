import { createContext, useCallback, useContext, useMemo, useRef, useState } from 'react'
import * as api from '../api/client'
import { MAX_RETRIES, formatSpeed, isRetryableError, retryDelay } from '../lib/upload-utils'
import { createTask, filterActiveTasks, patchTask, progressStage } from '../lib/upload-task'

export type UploadStatus = 'hashing' | 'uploading' | 'retrying' | 'finalizing' | 'done' | 'error'

export interface UploadTask {
  id: string
  name: string
  progress: number
  speed: string
  status: UploadStatus
  size?: number
  error?: string
  retryCount?: number
}

interface UploadContextType {
  uploadTasks: UploadTask[]
  hasActiveUploads: boolean
  customRequest: (file: File) => Promise<void>
  clearDoneTasks: () => void
  setUploadTasks: (tasks: UploadTask[]) => void
}

const UploadContext = createContext<UploadContextType>(null!)

/** Dispatched after an upload commits successfully so a mounted file list can refresh. */
export const UPLOAD_COMPLETED_EVENT = 'fileupload:upload-completed'
const MAX_CONCURRENT_FILES = 3
const FALLBACK_CHUNK_SIZE = 10 * 1024 * 1024

export function useUploadCtx() {
  return useContext(UploadContext)
}

export function UploadProvider({ children, onComplete }: { children: React.ReactNode; onComplete?: () => void }) {
  const [uploadTasks, setUploadTasks_] = useState<UploadTask[]>([])
  const taskIdRef = useRef(0)
  const activeCountRef = useRef(0)
  const queuedJobsRef = useRef<Array<() => void>>([])
  const doneRef = useRef(onComplete)
  doneRef.current = onComplete

  const setUploadTasks = useCallback((tasks: UploadTask[]) => setUploadTasks_(tasks), [])

  const clearDoneTasks = useCallback(() => {
    setUploadTasks_(prev => prev.filter(t => t.status !== 'done' && t.status !== 'error'))
  }, [])

  const runWithRetries = useCallback(async <T,>(operation: () => Promise<T>, onRetry: (attempt: number, error: unknown) => void): Promise<T> => {
    let lastError: unknown
    for (let attempt = 0; attempt <= MAX_RETRIES; attempt += 1) {
      try {
        return await operation()
      } catch (error) {
        lastError = error
        if (!isRetryableError(error) || attempt === MAX_RETRIES) break
        onRetry(attempt + 1, error)
        await new Promise(resolve => window.setTimeout(resolve, retryDelay(attempt)))
      }
    }
    throw lastError
  }, [])

  const startUpload = useCallback(async (file: File, taskId: string) => {
    let task = createTask(taskId, file.name, file.size)
    const commit = (patch: Partial<UploadTask>) => {
      task = patchTask(task, patch)
      setUploadTasks_(prev => {
        const index = prev.findIndex(item => item.id === taskId)
        if (index === -1) return [...prev, task]
        return prev.map(item => item.id === taskId ? task : item)
      })
    }

    let sessionID: string | undefined
    try {
      // Do not load the whole file into an ArrayBuffer merely to calculate a pre-upload hash.
      // Large files stay chunk-streamed and the server computes the authoritative SHA-256 on finalize.
      commit({ status: 'uploading', progress: progressStage('hashing', 100), speed: '' })
      const init = await runWithRetries(
        () => api.initUpload(file.size, undefined, file.name, 'none'),
        (attempt) => commit({ status: 'retrying', retryCount: attempt, error: `正在重试初始化 (${attempt}/${MAX_RETRIES})` }),
      )
      sessionID = init.session_id
      const chunkSize = Number(init.chunk_size) > 0 ? Number(init.chunk_size) : FALLBACK_CHUNK_SIZE
      const totalChunks = Math.max(1, Math.ceil(file.size / chunkSize))

      for (let index = 0; index < totalChunks; index += 1) {
        const start = index * chunkSize
        const chunk = file.slice(start, Math.min(start + chunkSize, file.size))
        const checksum = await crypto.subtle.digest('SHA-256', await chunk.arrayBuffer())
        const chunkSHA256 = Array.from(new Uint8Array(checksum), byte => byte.toString(16).padStart(2, '0')).join('')
        let lastProgressAt = 0
        const chunkStartedAt = Date.now()

        await runWithRetries(
          () => api.uploadChunk(sessionID!, index, chunk, chunkSHA256, event => {
            const now = Date.now()
            if (now - lastProgressAt < 120 && event.loaded !== event.total) return
            lastProgressAt = now
            const loaded = Math.min(file.size, start + (event.loaded || 0))
            const elapsedSeconds = Math.max((now - chunkStartedAt) / 1000, 0.001)
            const speed = formatSpeed((event.loaded || 0) / elapsedSeconds)
            commit({
              status: 'uploading',
              retryCount: undefined,
              error: undefined,
              progress: progressStage('uploading', (loaded / Math.max(file.size, 1)) * 100),
              speed,
            })
          }),
          (attempt) => commit({ status: 'retrying', retryCount: attempt, error: `网络波动，正在重试分片 ${index + 1}/${totalChunks}` }),
        )
      }

      commit({ status: 'finalizing', progress: progressStage('finalizing', 50), speed: '' })
      await runWithRetries(
        () => api.finalizeUpload(sessionID!),
        (attempt) => commit({ status: 'retrying', retryCount: attempt, error: `正在重试完成校验 (${attempt}/${MAX_RETRIES})` }),
      )
      commit({ status: 'done', progress: 100, speed: '', error: undefined })
      window.dispatchEvent(new Event(UPLOAD_COMPLETED_EVENT))
      doneRef.current?.()
    } catch (error: any) {
      if (sessionID) void api.cancelUpload(sessionID).catch(() => undefined)
      commit({ status: 'error', error: error?.response?.data?.error || error?.message || '上传失败', speed: '' })
      throw error
    }
  }, [runWithRetries])

  const schedule = useCallback((job: () => Promise<void>) => new Promise<void>((resolve, reject) => {
    const run = () => {
      activeCountRef.current += 1
      void job().then(resolve, reject).finally(() => {
        activeCountRef.current -= 1
        const next = queuedJobsRef.current.shift()
        if (next) next()
      })
    }
    if (activeCountRef.current < MAX_CONCURRENT_FILES) run()
    else queuedJobsRef.current.push(run)
  }), [])

  const customRequest = useCallback((file: File) => {
    const taskId = `upload-${Date.now()}-${++taskIdRef.current}`
    setUploadTasks_(prev => [...prev, { ...createTask(taskId, file.name, file.size), size: file.size }])
    return schedule(() => startUpload(file, taskId))
  }, [schedule, startUpload])

  const hasActiveUploads = useMemo(() => filterActiveTasks(uploadTasks).length > 0, [uploadTasks])

  return (
    <UploadContext.Provider value={{ uploadTasks, hasActiveUploads, customRequest, clearDoneTasks, setUploadTasks }}>
      {children}
    </UploadContext.Provider>
  )
}
