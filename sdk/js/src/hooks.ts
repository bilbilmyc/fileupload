import { useState, useCallback, useEffect, useRef } from 'react'
import { FileuploadClient } from './client'
import type { FileItem, ListResult, ClientConfig } from './types'

export { FileuploadClient } from './client'
export type * from './types'

/**
 * React Hook — 文件列表
 *
 * @example
 * ```tsx
 * function FileBrowser() {
 *   const { files, loading, navigateToDir } = useFileList()
 *   return <div>...</div>
 * }
 * ```
 */
export function useFileList(config?: ClientConfig) {
  const clientRef = useRef(new FileuploadClient(config))
  const [files, setFiles] = useState<FileItem[]>([])
  const [loading, setLoading] = useState(false)
  const [currentDir, setCurrentDir] = useState('/')
  const [ancestors, setAncestors] = useState<FileItem[]>([])

  const load = useCallback(async (dir?: string) => {
    setLoading(true)
    try {
      const parent = dir ?? currentDir
      const res: ListResult = await clientRef.current.list(parent)
      setFiles(res.children || [])
      setAncestors(res.ancestors || [])
    } catch (e: any) {
      console.error('加载文件列表失败:', e)
    } finally {
      setLoading(false)
    }
  }, [currentDir])

  useEffect(() => { load() }, [load])

  const navigateToDir = useCallback((dirId: string) => {
    setCurrentDir(dirId)
  }, [])

  return { files, loading, currentDir, ancestors, navigateToDir, reload: () => load() }
}

/**
 * React Hook — 文件上传
 *
 * @example
 * ```tsx
 * function Uploader() {
 *   const { upload, uploading, progress } = useFileUpload()
 *   return <div>...</div>
 * }
 * ```
 */
export function useFileUpload(config?: ClientConfig) {
  const clientRef = useRef(new FileuploadClient(config))
  const [uploading, setUploading] = useState(false)
  const [progress, setProgress] = useState(0)
  const [error, setError] = useState<string | null>(null)

  const upload = useCallback(async (file: File) => {
    setUploading(true)
    setProgress(0)
    setError(null)
    try {
      const result = await clientRef.current.upload(file, file.name, {
        onProgress: (uploaded, total) => {
          setProgress(Math.round((uploaded / total) * 100))
        },
      })
      return result
    } catch (e: any) {
      setError(e.message)
      throw e
    } finally {
      setUploading(false)
    }
  }, [])

  return { upload, uploading, progress, error }
}
