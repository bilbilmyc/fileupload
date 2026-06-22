import { useEffect, useCallback, useState } from 'react'
import { Layout, message, Modal, Button } from 'antd'
import { FolderOpenOutlined, HistoryOutlined } from '@ant-design/icons'
import { useAuth } from '../context/AuthContext'
import * as api from '../api/client'
import { useFileOperations } from '../hooks/useFileOperations'
import { useUpload } from '../hooks/useUpload'
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

const { Content } = Layout

export default function Files() {
  const { namespace } = useAuth()
  const { items: batchHistory, addRecord } = useBatchHistory()

  // File operations hook
  const {
    loading,
    search,
    page,
    currentDir,
    selectedRowKeys,
    stats,
    breadcrumbItems,
    paginatedFiles,
    filteredFiles,
    setSearch,
    setPage,
    setSelectedRowKeys,
    loadFiles,
    navigateToDir,
    navigateUp,
    handleDownload,
    handleDelete,
  } = useFileOperations()

  // Upload hook
  const {
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
  } = useUpload(loadFiles)

  // Dialogs state
  const [dirPickerOpen, setDirPickerOpen] = useState(false)
  const [dirPickerMode, setDirPickerMode] = useState<'move' | 'copy'>('move')
  const [tagEditorOpen, setTagEditorOpen] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)

  // Initial load
  useEffect(() => {
    loadFiles()
  }, [loadFiles])

  // Refresh when namespace changes
  useEffect(() => {
    loadFiles()
  }, [namespace]) // eslint-disable-line react-hooks/exhaustive-deps

  const selectedFiles = filteredFiles.filter(f => selectedRowKeys.includes(f.file_id))

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

  const handleBatchDownload = useCallback(async (format: string) => {
    if (selectedRowKeys.length === 0) return
    try {
      const ids = selectedRowKeys as string[]
      const response = await fetch('/v1/batch/download', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ids, format }),
      })
      if (!response.ok) throw new Error('下载失败')
      const blob = await response.blob()
      const url = URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = `batch.${format}`
      a.click()
      URL.revokeObjectURL(url)
      addRecord({ type: 'download', fileCount: ids.length, status: 'success', detail: `格式: ${format}` })
      message.success('打包下载完成')
    } catch (e: any) {
      addRecord({ type: 'download', fileCount: selectedRowKeys.length, status: 'failed', detail: e.message })
      message.error(`批量下载失败: ${e.message}`)
    }
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
    <Layout className="min-h-screen bg-gray-50">
      <TopBar
        search={search}
        onSearchChange={setSearch}
        onRefresh={loadFiles}
        onOpenSettings={() => setConfigOpen(true)}
      />

      <Content className="px-6 py-4" style={{ maxWidth: 1200, margin: '0 auto' }}>
        <div className="flex flex-col gap-4">
          {/* Stats + Breadcrumb */}
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              {currentDir !== '/' && (
                <button
                  onClick={navigateUp}
                  className="text-xs text-blue-500 hover:text-blue-700 border border-blue-200 rounded px-2 py-0.5"
                  title="返回上一级"
                >
                  ↑ 上一级
                </button>
              )}
              <BreadcrumbNav items={breadcrumbItems} />
            </div>
            <StatsBar
              dirs={stats.dirs}
              files={stats.files}
              totalSize={stats.totalSize.toString()}
            />
          </div>

          {/* Hide Upload Area - click to show */}
          <UploadPanel
            uploadTasks={uploadTasks}
            dirMode={dirMode}
            showUpload={showUpload}
            hasActiveUploads={hasActiveUploads}
            onDirModeChange={setDirMode}
            onShowUploadChange={setShowUpload}
            onCustomRequest={customRequest}
            onClearDone={clearDoneTasks}
          />

          {/* File Table */}
          <div className="bg-white rounded-lg shadow-sm p-3">
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
                操作历史
              </Button>
            </div>
            <FileTable
              files={paginatedFiles}
              loading={loading}
              page={page}
              pageSize={50}
              total={filteredFiles.length}
              selectedRowKeys={selectedRowKeys}
              onPageChange={setPage}
              onSelectionChange={setSelectedRowKeys}
              onNavigateToDir={navigateToDir}
              onDownload={handleDownload}
              onDelete={handleDelete}
            />
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
    </Layout>
  )
}
