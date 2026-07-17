import { useState, useMemo, useCallback, useRef, useEffect } from 'react'
import { useSearchParams } from 'react-router-dom'
import { message } from 'antd'
import * as api from '../api/client'
import type { FileItem } from '../api/client'
import {
  createFileListSearchParams,
  parseFileListQuery,
  toFileSortBy,
  toFileTypeFilter,
  type FileListQuery,
} from '../lib/file-list-query'

export interface FileStats {
  dirs: number
  files: number
  totalSize: number
}

function useDebouncedValue<T>(value: T, delay: number) {
  const [debouncedValue, setDebouncedValue] = useState(value)

  useEffect(() => {
    const timeout = window.setTimeout(() => setDebouncedValue(value), delay)
    return () => window.clearTimeout(timeout)
  }, [delay, value])

  return debouncedValue
}

export function useFileOperations(_namespace?: string) {
  const [files, setFiles] = useState<FileItem[]>([])
  const [loading, setLoading] = useState(false)
  const requestIdRef = useRef(0)
  const [parentName, setParentName] = useState<string>('')
  const [parentID, setParentID] = useState<string | null>(null)
  const [ancestors, setAncestors] = useState<FileItem[]>([])
  const [total, setTotal] = useState(0)
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([])
  const [searchParams, setSearchParams] = useSearchParams()

  const query = useMemo(() => parseFileListQuery(searchParams), [searchParams])
  const { directory: currentDir, search, typeFilter, page, sortBy, sortOrder } = query
  const debouncedSearch = useDebouncedValue(search, 300)
  const searchPending = search !== debouncedSearch

  const updateQuery = useCallback((patch: Partial<FileListQuery>, replace = false) => {
    setSelectedRowKeys([])
    setSearchParams(createFileListSearchParams({ ...query, ...patch }), { replace })
  }, [query, setSearchParams])

  const fetchFiles = useCallback(async (searchTerm: string) => {
    const requestId = ++requestIdRef.current
    const perPage = 50
    setLoading(true)

    try {
      const res = await api.listFiles({
        parent: currentDir,
        search: searchTerm.trim() || undefined,
        page,
        per_page: perPage,
        sort_by: sortBy,
        sort_order: sortOrder,
        type: typeFilter || undefined,
      })
      if (requestId !== requestIdRef.current) return

      const items = res.children || []
      setFiles(items)
      setTotal(res.total || items.length)
      if (res.dir && typeof res.dir === 'object' && 'name' in res.dir) {
        const dir = res.dir as { name: string; parent_id?: string | null }
        setParentName(dir.name)
        setParentID(dir.parent_id || (currentDir !== '/' ? '/' : null))
      } else {
        setParentName('')
        setParentID(null)
      }
      setAncestors(res.ancestors || [])
      setSelectedRowKeys([])
    } catch (error: any) {
      if (requestId === requestIdRef.current) {
        message.error(`加载失败: ${error.message || '请稍后重试'}`)
      }
    } finally {
      if (requestId === requestIdRef.current) {
        setLoading(false)
      }
    }
  }, [currentDir, page, sortBy, sortOrder, typeFilter])

  // The normal query path waits for the current text to settle, so sorting or paging
  // while typing cannot issue a request with a stale search term.
  const loadFiles = useCallback(() => {
    if (searchPending) return
    return fetchFiles(debouncedSearch)
  }, [debouncedSearch, fetchFiles, searchPending])
  // Explicit user actions always refresh the exact text currently shown in the search field.
  const refreshFiles = useCallback(() => fetchFiles(search), [fetchFiles, search])

  const setSearch = useCallback((value: string) => {
    updateQuery({ search: value, page: 1 }, true)
  }, [updateQuery])

  const setPage = useCallback((value: number) => {
    updateQuery({ page: Number.isSafeInteger(value) && value > 0 ? value : 1 })
  }, [updateQuery])

  const setTypeFilter = useCallback((value: string) => {
    updateQuery({ typeFilter: toFileTypeFilter(value), page: 1 })
  }, [updateQuery])

  const navigateToDir = useCallback((dirId: string) => {
    updateQuery({ directory: dirId || '/', page: 1 })
  }, [updateQuery])

  const navigateUp = useCallback(() => {
    updateQuery({ directory: parentID || '/', page: 1 })
  }, [parentID, updateQuery])

  const navigateToRoot = useCallback(() => {
    updateQuery({ directory: '/', page: 1 })
  }, [updateQuery])

  const stats = useMemo<FileStats>(() => {
    let dirs = 0
    let fileCount = 0
    let totalSize = 0

    for (const item of files) {
      if (item.is_dir) dirs += 1
      else {
        fileCount += 1
        totalSize += item.size
      }
    }

    return { dirs, files: fileCount, totalSize }
  }, [files])

  const breadcrumbItems = useMemo(() => {
    const items: any[] = []
    if (ancestors && ancestors.length > 0) {
      for (let i = 0; i < ancestors.length; i += 1) {
        const ancestor = ancestors[i]
        const isLast = i === ancestors.length - 1
        items.push({
          title: isLast ? (
            <span className="text-gray-700 font-medium">
              <span role="img" aria-label="folder">📁</span> {ancestor.name}
            </span>
          ) : (
            <button
              type="button"
              onClick={() => navigateToDir(ancestor.file_id)}
              className="text-gray-500 hover:text-blue-500"
            >
              <span role="img" aria-label="folder">📁</span> {ancestor.name}
            </button>
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

  const handleDownload = useCallback(async (record: FileItem) => {
    try {
      const blob = record.is_dir
        ? await api.downloadDir(record.file_id)
        : await api.downloadFile(record.file_id)
      api.saveBlob(blob, record.is_dir ? `${record.name}.tar.gz` : record.name)
    } catch (error: any) {
      message.error(`下载失败: ${error.message || '请稍后重试'}`)
    }
  }, [])

  const handleDelete = useCallback(async (record: FileItem) => {
    try {
      if (record.is_dir) await api.deleteDir(record.file_id)
      else await api.deleteFile(record.file_id)
      message.success('已删除')
      void refreshFiles()
    } catch (error: any) {
      message.error(`删除失败: ${error.message || '请稍后重试'}`)
    }
  }, [refreshFiles])

  const handleBatchDelete = useCallback(async () => {
    const items = files.filter(file => selectedRowKeys.includes(file.file_id))
    if (items.length === 0) return

    let ok = 0
    let fail = 0
    for (const item of items) {
      try {
        if (item.is_dir) await api.deleteDir(item.file_id)
        else await api.deleteFile(item.file_id)
        ok += 1
      } catch {
        fail += 1
      }
    }
    setSelectedRowKeys([])
    message.success(`删除完成: ${ok} 成功${fail ? `, ${fail} 失败` : ''}`)
    void refreshFiles()
  }, [files, refreshFiles, selectedRowKeys])

  const handleRename = useCallback(async (record: FileItem, newName: string) => {
    try {
      await api.renameFile(record.file_id, newName)
      message.success('已重命名')
      void refreshFiles()
    } catch (error: any) {
      message.error(`重命名失败: ${error.message || '请稍后重试'}`)
    }
  }, [refreshFiles])

  const handleSortChange = useCallback((field: string, order: string) => {
    updateQuery({
      sortBy: toFileSortBy(field),
      sortOrder: order === 'descend' ? 'desc' : 'asc',
      page: 1,
    })
  }, [updateQuery])

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
    searchPending,
    setSearch,
    setPage,
    setTypeFilter,
    setSelectedRowKeys,
    loadFiles,
    refreshFiles,
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
