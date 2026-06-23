import { createContext, useContext, useState, useCallback, useRef } from 'react'
import * as api from '../api/client'

export interface UploadTask {
  id: string
  name: string
  progress: number
  speed: string
  status: 'pending' | 'hashing' | 'uploading' | 'retrying' | 'finalizing' | 'done' | 'error'
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

  const clearDoneTasks = useCallback(() => {
    setUploadTasks_(prev => prev.filter(t => t.status !== 'done' && t.status !== 'error'))
  }, [])

  const customRequest = useCallback(async (file: File) => {
    const taskId = `task-${++taskIdRef.current}`
    setUploadTasks_(prev => [...prev, {
      id: taskId, name: file.name, progress: 0, speed: '',
      status: 'hashing',
    }])

    try {
      setUploadTasks_(prev => prev.map(t =>
        t.id === taskId ? { ...t, status: 'uploading' as const } : t
      ))

      const chunkSize = 10 * 1024 * 1024
      const totalChunks = Math.ceil(file.size / chunkSize)

      // SHA-256
      const sha256 = await computeSHA256(file)

      // Check exists
      setUploadTasks_(prev => prev.map(t =>
        t.id === taskId ? { ...t, status: 'uploading' as const, progress: 5 } : t
      ))

      const existing = await api.checkExists(sha256, file.name)
      if (existing) {
        setUploadTasks_(prev => prev.map(t =>
          t.id === taskId ? { ...t, status: 'done' as const, progress: 100 } : t
        ))
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
            const pct = Math.round((loaded / file.size) * 90)
            const elapsed = (Date.now() - started) / 1000
            const speed = elapsed > 0 ? formatSpeed((e.loaded || 0) - lastLoaded, elapsed) : ''
            lastLoaded = e.loaded || 0
            setUploadTasks_(prev => prev.map(t =>
              t.id === taskId ? { ...t, progress: pct, speed } : t
            ))
          }
        })
      }

      // Finalize
      setUploadTasks_(prev => prev.map(t =>
        t.id === taskId ? { ...t, status: 'finalizing' as const, progress: 95 } : t
      ))
      await api.finalizeUpload(init.session_id)

      setUploadTasks_(prev => prev.map(t =>
        t.id === taskId ? { ...t, status: 'done' as const, progress: 100 } : t
      ))
      doneRef.current?.()
    } catch (e: any) {
      setUploadTasks_(prev => prev.map(t =>
        t.id === taskId ? { ...t, status: 'error' as const, error: e.message } : t
      ))
    }
  }, [])

  const hasActiveUploads = uploadTasks.some(t =>
    ['hashing', 'uploading', 'retrying', 'finalizing'].includes(t.status)
  )

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
