import { useEffect, useCallback, useState } from 'react'
import { message, Modal, Button, Tooltip } from 'antd'
import { HistoryOutlined } from '@ant-design/icons'
import { useAuth } from '../context/AuthContext'
import { useUploadCtx } from '../context/UploadContext'
import * as api from '../api/client'
import { useFileOperations } from '../hooks/useFileOperations'
import ErrorBoundary from '../components/ErrorBoundary'
import TopBar from '../components/TopBar'
import ActionToolbar from '../components/ActionToolbar'
import BreadcrumbNav from '../components/BreadcrumbNav'
import StatsBar from '../components/StatsBar'
import FileTable from '../components/FileTable'
import BatchToolbar from '../components/BatchToolbar'
import DirectoryPicker from '../components/DirectoryPicker'
import BatchTagEditor from '../components/BatchTagEditor'
import BatchHistoryPanel, { useBatchHistory } from '../components/BatchHistoryPanel'
import FilePreview from '../components/FilePreview'

export default function Files() {
  const { namespace } = useAuth()
  const { items: batchHistory, addRecord } = useBatchHistory()
  const uploadCtx = useUploadCtx()

  const {
    loading, search, page, total, parentID, selectedRowKeys, stats,
    breadcrumbItems, files, typeFilter,
    setSearch, setPage, setTypeFilter, setSelectedRowKeys,
    loadFiles, navigateToDir, navigateUp, handleDownload, handleDelete,
    handleRename, handleSortChange,
  } = useFileOperations()

  const [dirPickerOpen, setDirPickerOpen] = useState(false)
  const [dirPickerMode, setDirPickerMode] = useState<'move' | 'copy'>('move')
  const [tagEditorOpen, setTagEditorOpen] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [previewFile, setPreviewFile] = useState<{ id: string; name: string; size: number } | null>(null)

  useEffect(() => { loadFiles() }, [loadFiles])
  useEffect(() => { loadFiles() }, [namespace]) // eslint-disable-line react-hooks/exhaustive-deps

  const handlePreview = useCallback((record: { file_id: string; name: string; size: number }) => {
    setPreviewFile({ id: record.file_id, name: record.name, size: record.size })
  }, [])

  const selectedFiles = files.filter(f => selectedRowKeys.includes(f.file_id))

  // ---- Batch operation handlers ----

  const handleBatchDelete = useCallback(async () => {
    if (selectedRowKeys.length === 0) return
    Modal.confirm({
      title: `确认删除 ${selectedRowKeys.length} 个项目？`,
      content: '删除后不可恢复。',
      onOk: async () => {
        try {
          const result = await api.batchDelete(selectedRowKeys as string[])
          addRecord({ type: 'delete', fileCount: selectedRowKeys.length, status: result.failed === 0 ? 'success' : 'partial', detail: `${result.success} 成功, ${result.failed} 失败` })
          setSelectedRowKeys([])
          message.success(`删除完成: ${result.success} 成功${result.failed ? `, ${result.failed} 失败` : ''}`)
          loadFiles()
        } catch {
          let ok = 0, fail = 0
          for (const item of selectedFiles) {
            try {
              if (item.is_dir) await api.deleteDir(item.file_id)
              else await api.deleteFile(item.file_id)
              ok++
            } catch { fail++ }
          }
          addRecord({ type: 'delete', fileCount: selectedRowKeys.length, status: fail === 0 ? 'success' : 'partial', detail: `${ok} 成功, ${fail} 失败` })
          setSelectedRowKeys([])
          message.success(`删除完成: ${ok} 成功${fail ? `, ${fail} 失败` : ''}`)
          loadFiles()
        }
      },
    })
  }, [selectedRowKeys, selectedFiles, loadFiles, addRecord])

  const handleBatchMove = useCallback(() => {
    setDirPickerMode('move')
    setDirPickerOpen(true)
  }, [])

  const handleBatchCopy = useCallback(() => {
    setDirPickerMode('copy')
    setDirPickerOpen(true)
  }, [])

  const handleBatchTag = useCallback(() => {
    setTagEditorOpen(true)
  }, [])

  const handleDirPickerConfirm = useCallback(async (targetDirId: string) => {
    try {
      if (dirPickerMode === 'move') {
        await api.batchMove(selectedRowKeys as string[], targetDirId)
      } else {
        await api.batchCopy(selectedRowKeys as string[], targetDirId)
      }
      message.success(`${dirPickerMode === 'move' ? '移动' : '复制'}完成`)
      setSelectedRowKeys([])
      loadFiles()
    } catch (e: any) {
      message.error(`${dirPickerMode === 'move' ? '移动' : '复制'}失败: ${e.message}`)
    }
    setDirPickerOpen(false)
  }, [dirPickerMode, selectedRowKeys, loadFiles])

  const handleTagEditorConfirm = useCallback(async (tags: string[]) => {
    try {
      await api.batchSetTags(selectedRowKeys as string[], tags)
      message.success('标记完成')
      setSelectedRowKeys([])
      loadFiles()
    } catch (e: any) {
      message.error(`标记失败: ${e.message}`)
    }
    setTagEditorOpen(false)
  }, [selectedRowKeys, loadFiles])

  // ---- New folder ----
  const handleNewFolder = useCallback(() => {
    const name = prompt('请输入目录名:')
    if (!name?.trim()) return
    // Create via upload an empty dir manifest
    const manifest = { entries: [] }
    api.submitDir(name.trim(), manifest.entries).then(() => {
      message.success('目录创建成功')
      loadFiles()
    }).catch((e: any) => {
      message.error(`创建目录失败: ${e.message}`)
    })
  }, [loadFiles])

  // ---- Upload ----
  const handleUploadFile = useCallback((file: File) => {
    uploadCtx.customRequest(file)
  }, [uploadCtx])

  // ---- Single item actions ----
  const handleSingleDownload = useCallback(() => {
    if (selectedFiles.length === 1) {
      handleDownload(selectedFiles[0])
    }
  }, [selectedFiles, handleDownload])

  const handleSingleDelete = useCallback(() => {
    if (selectedRowKeys.length === 0) return
    if (selectedFiles.length === 1) {
      Modal.confirm({
        title: `确认删除 ${selectedFiles[0].name}？`,
        onOk: () => handleDelete(selectedFiles[0]),
      })
    } else {
      handleBatchDelete()
    }
  }, [selectedFiles, selectedRowKeys, handleDelete, handleBatchDelete])

  return (
    <div className="flex flex-col flex-1">
      <TopBar
        search={search}
        typeFilter={typeFilter}
        onSearchChange={setSearch}
        onTypeFilterChange={setTypeFilter}
        onRefresh={loadFiles}
      />

      <div className="flex-1 p-4 overflow-auto">
        <BreadcrumbNav items={breadcrumbItems} />

        <div className="flex items-center justify-between mt-1">
          <StatsBar dirs={stats.dirs} files={stats.files} totalSize={stats.totalSize} />
          <Tooltip title="操作历史">
            <Button type="text" size="small" icon={<HistoryOutlined />} onClick={() => setHistoryOpen(!historyOpen)} />
          </Tooltip>
        </div>

        <ActionToolbar
          onUpload={handleUploadFile}
          onNewFolder={handleNewFolder}
          onDownload={handleSingleDownload}
          onDelete={handleSingleDelete}
          onRefresh={loadFiles}
          hasSelection={selectedRowKeys.length > 0}
          hasSingleSelection={selectedFiles.length === 1}
        />

        {/* History panel */}
        {historyOpen && (
          <div className="mb-3">
            <BatchHistoryPanel open={true} onClose={() => setHistoryOpen(false)} history={batchHistory} onClear={() => {}} />
          </div>
        )}

        {/* File table */}
        <ErrorBoundary title="文件列表异常">
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-sm border border-gray-200 dark:border-gray-700">
            <FileTable
              files={files}
              loading={loading}
              page={page}
              pageSize={50}
              total={total}
              selectedRowKeys={selectedRowKeys}
              parentFileId={parentID}
              onPageChange={setPage}
              onSelectionChange={setSelectedRowKeys}
              onNavigateToDir={navigateToDir}
              onNavigateUp={navigateUp}
              onDownload={handleDownload}
              onDelete={handleDelete}
              onPreview={handlePreview}
              onRename={handleRename}
              onSortChange={handleSortChange}
            />
          </div>
        </ErrorBoundary>

        {/* Batch toolbar */}
        {selectedRowKeys.length > 0 && (
          <BatchToolbar
            selectedCount={selectedRowKeys.length}
            onCancel={() => setSelectedRowKeys([])}
            onBatchDelete={handleBatchDelete}
            onBatchDownload={(fmt) => {
              const ids = selectedRowKeys.join(',')
              const a = document.createElement('a')
              a.href = `${api.batchDownloadUrl()}?ids=${ids}&format=${fmt}`
              a.click()
            }}
            onBatchMove={handleBatchMove}
            onBatchCopy={handleBatchCopy}
            onBatchTag={handleBatchTag}
          />
        )}
      </div>

      {/* Modals */}
      <DirectoryPicker
        open={dirPickerOpen}
        title="选择目标目录"
        onCancel={() => setDirPickerOpen(false)}
        onConfirm={handleDirPickerConfirm}
      />
      <BatchTagEditor
        open={tagEditorOpen}
        fileCount={selectedRowKeys.length}
        onConfirm={handleTagEditorConfirm}
        onCancel={() => setTagEditorOpen(false)}
      />
      {previewFile && (
        <FilePreview
          fileId={previewFile.id}
          fileName={previewFile.name}
          fileSize={previewFile.size}
          open={true}
          onClose={() => setPreviewFile(null)}
        />
      )}
    </div>
  )
}
