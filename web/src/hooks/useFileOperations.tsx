import { useState, useMemo, useCallback, useRef } from 'react'
import { message } from 'antd'
import * as api from '../api/client'
import type { FileItem } from '../api/client'


export interface FileStats {
  dirs: number
  files: number
  totalSize: number
}

export function useFileOperations(_namespace?: string) {
  const [files, setFiles] = useState<FileItem[]>([])
  const [loading, setLoading] = useState(false)
  const [currentDir, setCurrentDir] = useState<string>('/')
  const requestIdRef = useRef(0)
  const [parentName, setParentName] = useState<string>('')
  const [parentID, setParentID] = useState<string | null>(null)
  const [ancestors, setAncestors] = useState<FileItem[]>([])
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const [sortBy, setSortBy] = useState('name')
  const [sortOrder, setSortOrder] = useState('asc')
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([])
  const [typeFilter, setTypeFilter] = useState<string>('')

  const loadFiles = useCallback(async () => {
    const requestId = ++requestIdRef.current
    const perPage = 50
    setLoading(true)
    try {
      const res = await api.listFiles({
        parent: currentDir,
        search: search || undefined,
        page,
        per_page: perPage,
        sort_by: sortBy,
        sort_order: sortOrder,
      })
      if (requestId !== requestIdRef.current) return

      let items = res.children || []
      // 客户端类型筛选（可选）
      if (typeFilter === 'dir') items = items.filter(f => f.is_dir)
      else if (typeFilter === 'file') items = items.filter(f => !f.is_dir)

      setFiles(items)
      setTotal(res.total || items.length)
      if (res.dir && typeof res.dir === 'object' && 'name' in res.dir) {
        const d = res.dir as any
        setParentName(d.name as string)
        setParentID(d.parent_id || (currentDir !== '/' ? '/' : null))
      } else {
        setParentName('')
        setParentID(null)
      }
      setAncestors(res.ancestors || [])
    } catch (e: any) {
      message.error(`加载失败: ${e.message}`)
    } finally {
      setLoading(false)
    }
    setSelectedRowKeys([])
  }, [currentDir, search, page, sortBy, sortOrder, typeFilter])

  const navigateToDir = useCallback((dirId: string) => {
    setCurrentDir(dirId)
    setPage(1)
  }, [])

  const navigateUp = useCallback(() => {
    if (parentID) {
      setCurrentDir(parentID)
    } else {
      setCurrentDir('/')
    }
    setPage(1)
  }, [parentID])

  const navigateToRoot = useCallback(() => {
    setCurrentDir('/')
    setPage(1)
  }, [])

  const stats = useMemo<FileStats>(() => {
    let d = 0, f = 0, totalSize = 0
    const allFiles = files
    for (const item of allFiles) {
      if (item.is_dir) d++
      else { f++; totalSize += item.size }
    }
    return { dirs: d, files: f, totalSize }
  }, [files])

  const breadcrumbItems = useMemo(() => {
    const items: any[] = []
    if (ancestors && ancestors.length > 0) {
      for (let i = 0; i < ancestors.length; i++) {
        const a = ancestors[i]
        const isLast = i === ancestors.length - 1
        items.push({
          title: isLast ? (
            <span className="text-gray-700 font-medium">
              <span role="img" aria-label="folder">📁</span> {a.name}
            </span>
          ) : (
            <a onClick={() => navigateToDir(a.file_id)} className="text-gray-500 hover:text-blue-500">
              <span role="img" aria-label="folder">📁</span> {a.name}
            </a>
          ),
        })
      }
    } else {
      items.push({
        title: (
          <span className="text-gray-700 font-medium">
            <span role="img" aria-label="root">📂</span> 根目录
          </span>
        ),
      })
    }
    return items
  }, [ancestors, navigateToDir])

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

  const handleRename = useCallback(async (record: FileItem, newName: string) => {
    try {
      await api.renameFile(record.file_id, newName)
      message.success('已重命名')
      loadFiles()
    } catch (e: any) {
      message.error(`重命名失败: ${e.message}`)
    }
  }, [loadFiles])

  const handleSortChange = useCallback((field: string, order: string) => {
    setSortBy(field)
    setSortOrder(order === 'ascend' ? 'asc' : order === 'descend' ? 'desc' : 'asc')
    setPage(1)
  }, [])

  return {
    files,
    loading,
    currentDir,
    parentName,
    parentID,
    search,
    page,
    total,
    sortBy,
    sortOrder,
    typeFilter,
    selectedRowKeys,
    stats,
    breadcrumbItems,
    setSearch,
    setPage,
    setSortBy,
    setSortOrder,
    setTypeFilter,
    setSelectedRowKeys,
    loadFiles,
    navigateToDir,
    navigateUp,
    navigateToRoot,
    handleDownload,
    handleDelete,
    handleBatchDelete,
    handleRename,
    handleSortChange,
  }
}
