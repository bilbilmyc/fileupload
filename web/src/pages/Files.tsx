import { useEffect, useCallback, useState } from 'react'
import { Layout, message, Modal, Button } from 'antd'
import { FolderOpenOutlined, HistoryOutlined } from '@ant-design/icons'
import { useAuth } from '../context/AuthContext'
import * as api from '../api/client'
import { useFileOperations } from '../hooks/useFileOperations'
import { useUpload } from '../hooks/useUpload'
import ErrorBoundary from '../components/ErrorBoundary'
import TopBar from '../components/TopBar'
import BreadcrumbNav from '../components/BreadcrumbNav'
import StatsBar from '../components/StatsBar'
import UploadPanel from '../components/UploadPanel'
import FileTable from '../components/FileTable'
import BatchToolbar from '../components/BatchToolbar'
import SettingsModal from '../components/SettingsModal'
import DirectoryPicker from '../components/DirectoryPicker'
import BatchTagEditor from '../components/BatchTagEditor'
import BatchHistoryPanel, { useBatchHistory } from '../components/BatchHistoryPanel'
import FilePreview from '../components/FilePreview'

const { Content } = Layout

export default function Files() {
  const { namespace } = useAuth()
  const { items: batchHistory, addRecord } = useBatchHistory()

  // File operations hook
  const {
    loading,
    search,
    page,
    total,
    parentID,
    selectedRowKeys,
    stats,
    breadcrumbItems,
    files,
    typeFilter,
    setSearch,
    setPage,
    setTypeFilter,
    setSelectedRowKeys,
    loadFiles,
    navigateToDir,
    navigateUp,
    handleDownload,
    handleDelete,
    handleRename,
    handleSortChange,
  } = useFileOperations()

  // Upload hook
  const {
    uploadTasks,
    dirMode,
    chunkSize,
    concurrency,
    compression,
    configOpen,
    hasActiveUploads,
    setDirMode,
    setChunkSize,
    setConcurrency,
    setCompression,
    setConfigOpen,
    customRequest,
    clearDoneTasks,
  } = useUpload(loadFiles)

  // Dialogs state
  const [dirPickerOpen, setDirPickerOpen] = useState(false)
  const [dirPickerMode, setDirPickerMode] = useState<'move' | 'copy'>('move')
  const [tagEditorOpen, setTagEditorOpen] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [previewFile, setPreviewFile] = useState<{ id: string; name: string; size: number } | null>(null)

  // Initial load
  useEffect(() => {
    loadFiles()
  }, [loadFiles])

  // Refresh when namespace changes
  useEffect(() => {
    loadFiles()
  }, [namespace]) // eslint-disable-line react-hooks/exhaustive-deps

  const handlePreview = useCallback((record: { file_id: string; name: string; size: number }) => {
    setPreviewFile({ id: record.file_id, name: record.name, size: record.size })
  }, [])

  const selectedFiles = files.filter(f => selectedRowKeys.includes(f.file_id))

  // ---- Batch operation handlers ----

  const handleBatchDelete = useCallback(async () => {
    if (selectedRowKeys.length === 0) return
    Modal.confirm({
      title: `确认删除 ${selectedRowKeys.length} 个项目？`,
      content: '删除后不可恢复。目录删除仅移除目录记录，不删除子文件。',
      onOk: async () => {
        try {
          const result = await api.batchDelete(selectedRowKeys as string[])
          const status = result.failed === 0 ? 'success' : result.success > 0 ? 'partial' : 'failed'
          addRecord({ type: 'delete', fileCount: selectedRowKeys.length, status, detail: `${result.success} 成功, ${result.failed} 失败` })
          setSelectedRowKeys([])
          message.success(`删除完成: ${result.success} 成功${result.failed ? `, ${result.failed} 失败` : ''}`)
          loadFiles()
        } catch {
          // Fallback: sequential delete
          let ok = 0, fail = 0
          for (const item of selectedFiles) {
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
          const status = fail === 0 ? 'success' : ok > 0 ? 'partial' : 'failed'
          addRecord({ type: 'delete', fileCount: selectedRowKeys.length, status, detail: `${ok} 成功, ${fail} 失败` })
          setSelectedRowKeys([])
          message.success(`删除完成: ${ok} 成功${fail ? `, ${fail} 失败` : ''}`)
          loadFiles()
        }
      },
    })
  }, [selectedRowKeys, selectedFiles, loadFiles, addRecord])

  const handleBatchDownload = useCallback((format: string) => {
    if (selectedRowKeys.length === 0) return
    const ids = selectedRowKeys as string[]
    const ns = localStorage.getItem('fileupload_namespace') || 'default'
    // 使用 GET 流式下载，浏览器原生处理文件流，不占用前端内存
    const url = `/v1/batch/download?ids=${ids.join(',')}&format=${format}&namespace=${encodeURIComponent(ns)}`
    window.open(url, '_blank')
    addRecord({ type: 'download', fileCount: ids.length, status: 'success', detail: `格式: ${format}` })
    message.success('打包下载已开始')
  }, [selectedRowKeys, addRecord])

  const handleBatchMove = useCallback(() => {
    if (selectedRowKeys.length === 0) return
    setDirPickerMode('move')
    setDirPickerOpen(true)
  }, [selectedRowKeys])

  const handleBatchCopy = useCallback(() => {
    if (selectedRowKeys.length === 0) return
    setDirPickerMode('copy')
    setDirPickerOpen(true)
  }, [selectedRowKeys])

  const handleDirPickerConfirm = useCallback(async (dirId: string, dirName: string) => {
    setDirPickerOpen(false)
    const ids = selectedRowKeys as string[]
    try {
      if (dirPickerMode === 'move') {
        await api.batchMove(ids, dirId)
        addRecord({ type: 'move', fileCount: ids.length, status: 'success', detail: `目标: ${dirName || '根目录'}` })
        message.success(`已移动 ${ids.length} 个项目到 ${dirName || '根目录'}`)
      } else {
        await api.batchCopy(ids, dirId)
        addRecord({ type: 'copy', fileCount: ids.length, status: 'success', detail: `目标: ${dirName || '根目录'}` })
        message.success(`已复制 ${ids.length} 个项目到 ${dirName || '根目录'}`)
      }
      setSelectedRowKeys([])
      loadFiles()
    } catch (e: any) {
      const op = dirPickerMode === 'move' ? '移动' : '复制'
      addRecord({ type: dirPickerMode, fileCount: ids.length, status: 'failed', detail: e.message })
      message.error(`批量${op}失败: ${e.message}`)
    }
  }, [selectedRowKeys, dirPickerMode, loadFiles, addRecord])

  const handleBatchTag = useCallback(() => {
    if (selectedRowKeys.length === 0) return
    setTagEditorOpen(true)
  }, [selectedRowKeys])

  const handleTagConfirm = useCallback(async (tags: string[]) => {
    setTagEditorOpen(false)
    const ids = selectedRowKeys as string[]
    try {
      await api.batchSetTags(ids, tags)
      addRecord({ type: 'tag', fileCount: ids.length, status: 'success', detail: `标签: ${tags.join(', ')}` })
      setSelectedRowKeys([])
      message.success(`标记完成: ${tags.join(', ')}`)
      loadFiles()
    } catch (e: any) {
      addRecord({ type: 'tag', fileCount: ids.length, status: 'failed', detail: e.message })
      message.error(`批量标记失败: ${e.message}`)
    }
  }, [selectedRowKeys, loadFiles, addRecord])

  return (
    <Layout className="min-h-screen bg-gray-50 dark:bg-gray-900">
      <TopBar
        search={search}
        typeFilter={typeFilter}
        onSearchChange={setSearch}
        onTypeFilterChange={setTypeFilter}
        onRefresh={loadFiles}
        onOpenSettings={() => setConfigOpen(true)}
      />

      <Content className="px-3 sm:px-6 py-3 sm:py-4" style={{ maxWidth: 1200, margin: '0 auto' }}>
        <div className="flex flex-col gap-3 sm:gap-4">
          {/* Stats + Breadcrumb */}
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2">
            <BreadcrumbNav items={breadcrumbItems} />
            <StatsBar
              dirs={stats.dirs}
              files={stats.files}
              totalSize={stats.totalSize}
            />
          </div>

          <UploadPanel
            uploadTasks={uploadTasks}
            dirMode={dirMode}
            hasActiveUploads={hasActiveUploads}
            onDirModeChange={setDirMode}
            onCustomRequest={customRequest}
            onClearDone={clearDoneTasks}
          />

          {/* File Table */}
          <div className="bg-white dark:bg-gray-800 rounded-lg shadow-sm p-3">
            <div className="flex items-center justify-between mb-3 px-1">
              <div className="flex items-center gap-2">
                <FolderOpenOutlined className="text-amber-500" />
                <span className="text-sm font-medium">文件列表</span>
              </div>
              <Button
                type="text"
                size="small"
                icon={<HistoryOutlined />}
                onClick={() => setHistoryOpen(true)}
                className="text-gray-400"
              >
                <span className="hidden sm:inline">操作历史</span>
              </Button>
            </div>
            <ErrorBoundary title="文件列表异常">
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
            </ErrorBoundary>
            <BatchToolbar
              selectedCount={selectedRowKeys.length}
              onCancel={() => setSelectedRowKeys([])}
              onBatchDelete={handleBatchDelete}
              onBatchDownload={handleBatchDownload}
              onBatchMove={handleBatchMove}
              onBatchCopy={handleBatchCopy}
              onBatchTag={handleBatchTag}
            />
          </div>
        </div>
      </Content>

      {/* Settings Modal */}
      <SettingsModal
        open={configOpen}
        onClose={() => setConfigOpen(false)}
        concurrency={concurrency}
        compression={compression}
        chunkSize={chunkSize}
        onConcurrencyChange={setConcurrency}
        onCompressionChange={setCompression}
        onChunkSizeChange={setChunkSize}
      />

      {/* Directory Picker for Move/Copy */}
      <DirectoryPicker
        open={dirPickerOpen}
        title={dirPickerMode === 'move' ? '选择目标目录 - 批量移动' : '选择目标目录 - 批量复制'}
        onCancel={() => setDirPickerOpen(false)}
        onConfirm={handleDirPickerConfirm}
      />

      {/* Tag Editor */}
      <BatchTagEditor
        open={tagEditorOpen}
        fileCount={selectedRowKeys.length}
        onCancel={() => setTagEditorOpen(false)}
        onConfirm={handleTagConfirm}
      />

      {/* Batch History */}
      <BatchHistoryPanel
        open={historyOpen}
        onClose={() => setHistoryOpen(false)}
        history={batchHistory}
        onClear={() => { /* history auto-managed via hook */ }}
      />

      {/* File Preview */}
      {previewFile && (
        <FilePreview
          fileId={previewFile.id}
          fileName={previewFile.name}
          fileSize={previewFile.size}
          open={!!previewFile}
          onClose={() => setPreviewFile(null)}
        />
      )}
    </Layout>
  )
}
