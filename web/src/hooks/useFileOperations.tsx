import { useState, useMemo, useCallback } from 'react'
import { message } from 'antd'
import * as api from '../api/client'
import type { FileItem } from '../api/client'

const PAGE_SIZE = 50

export interface FileStats {
  dirs: number
  files: number
  totalSize: number
}

export function useFileOperations(_namespace?: string) {
  const [files, setFiles] = useState<FileItem[]>([])
  const [loading, setLoading] = useState(false)
  const [currentDir, setCurrentDir] = useState<string>('/')
  const [parentName, setParentName] = useState<string>('')
  const [parentID, setParentID] = useState<string | null>(null) // 上级目录 ID，用于"上一级"
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([])

  const loadFiles = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.listFiles(currentDir)
      setFiles(res.children || [])
      if (res.dir && typeof res.dir === 'object' && 'name' in res.dir) {
        const d = res.dir as any
        setParentName(d.name as string)
        setParentID(d.parent_id || null)
      } else {
        setParentName('')
        setParentID(null)
      }
    } catch (e: any) {
      message.error(`加载失败: ${e.message}`)
    } finally {
      setLoading(false)
    }
    setPage(1)
    setSelectedRowKeys([])
  }, [currentDir])

  const navigateToDir = useCallback((dirId: string) => {
    setCurrentDir(dirId)
  }, [])

  const navigateUp = useCallback(() => {
    if (parentID) {
      setCurrentDir(parentID)
    } else {
      setCurrentDir('/')
    }
  }, [parentID])

  const navigateToRoot = useCallback(() => {
    setCurrentDir('/')
  }, [])

  const filteredFiles = useMemo(() => {
    if (!search) return files
    return files.filter((f) =>
      f.name.toLowerCase().includes(search.toLowerCase())
    )
  }, [files, search])

  const paginatedFiles = useMemo(() => {
    const start = (page - 1) * PAGE_SIZE
    return filteredFiles.slice(start, start + PAGE_SIZE)
  }, [filteredFiles, page])

  const stats = useMemo<FileStats>(() => {
    let d = 0, f = 0, totalSize = 0
    for (const item of files) {
      if (item.is_dir) d++
      else { f++; totalSize += item.size }
    }
    return { dirs: d, files: f, totalSize }
  }, [files])

  const breadcrumbItems = useMemo(() => {
    const items: any[] = [
      {
        title: (
          <a onClick={navigateToRoot}>
            <span role="img" aria-label="root">📂</span> 根目录
          </a>
        ),
      },
    ]
    if (currentDir !== '/' && parentName) {
      items.push({
        title: (
          <a onClick={navigateUp} className="text-gray-500 hover:text-blue-500 cursor-pointer">
            <span role="img" aria-label="folder">📁</span> {parentName.slice(0, 16)}
          </a>
        ),
      })
    }
    return items
  }, [currentDir, parentName, navigateToRoot, navigateUp])

  const handleDownload = useCallback((record: FileItem) => {
    const url = record.is_dir
      ? api.downloadDirUrl(record.file_id)
      : api.downloadFileUrl(record.file_id)
    const a = document.createElement('a')
    a.href = url
    a.download = record.name
    a.click()
  }, [])

  const handleDelete = useCallback(async (record: FileItem) => {
    try {
      if (record.is_dir) {
        await api.deleteDir(record.file_id)
      } else {
        await api.deleteFile(record.file_id)
      }
      message.success('已删除')
      loadFiles()
    } catch (e: any) {
      message.error(`删除失败: ${e.message}`)
    }
  }, [loadFiles])

  const handleBatchDelete = useCallback(async () => {
    const items = files.filter(f => selectedRowKeys.includes(f.file_id))
    if (items.length === 0) return
    let ok = 0, fail = 0
    for (const item of items) {
      try {
        if (item.is_dir) {
          await api.deleteDir(item.file_id)
        } else {
          await api.deleteFile(item.file_id)
        }
        ok++
      } catch {
        fail++
      }
    }
    setSelectedRowKeys([])
    message.success(`删除完成: ${ok} 成功${fail ? `, ${fail} 失败` : ''}`)
    loadFiles()
  }, [files, selectedRowKeys, loadFiles])

  return {
    files,
    loading,
    currentDir,
    parentName,
    parentID,
    search,
    page,
    selectedRowKeys,
    stats,
    breadcrumbItems,
    filteredFiles,
    paginatedFiles,
    setSearch,
    setPage,
    setSelectedRowKeys,
    loadFiles,
    navigateToDir,
    navigateUp,
    navigateToRoot,
    handleDownload,
    handleDelete,
    handleBatchDelete,
  }
}
