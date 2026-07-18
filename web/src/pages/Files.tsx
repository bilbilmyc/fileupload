import { useEffect, useCallback, useState } from 'react'
import { message, Modal, Button, Tooltip } from 'antd'
import { CloudUploadOutlined, HistoryOutlined } from '@ant-design/icons'
import { useAuth } from '../context/AuthContext'
import { UPLOAD_COMPLETED_EVENT, useUploadCtx } from '../context/UploadContext'
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
import PropertiesPanel from '../components/PropertiesPanel'
import UploadDropzone from '../components/UploadDropzone'

export default function Files() {
  const { namespace } = useAuth()
  const { items: batchHistory, addRecord } = useBatchHistory()
  const uploadCtx = useUploadCtx()

  const {
    loading, search, page, total, parentID, selectedRowKeys, stats,
    breadcrumbItems, files, typeFilter, searchPending,
    setSearch, setPage, setTypeFilter, setSelectedRowKeys,
    loadFiles, refreshFiles, navigateToDir, navigateUp, handleDownload, handleDelete,
    handleRename, handleSortChange,
  } = useFileOperations()

  const [dirPickerOpen, setDirPickerOpen] = useState(false)
  const [dirPickerMode, setDirPickerMode] = useState<'move' | 'copy'>('move')
  const [tagEditorOpen, setTagEditorOpen] = useState(false)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [previewFile, setPreviewFile] = useState<{ id: string; name: string; size: number } | null>(null)
  const [propertiesFile, setPropertiesFile] = useState<any | null>(null)

  useEffect(() => { void loadFiles() }, [loadFiles, namespace])
  useEffect(() => {
    const refresh = () => { void refreshFiles() }
    window.addEventListener(UPLOAD_COMPLETED_EVENT, refresh)
    return () => window.removeEventListener(UPLOAD_COMPLETED_EVENT, refresh)
  }, [refreshFiles])

  const handlePreview = useCallback((record: { file_id: string; name: string; size: number }) => {
    setPreviewFile({ id: record.file_id, name: record.name, size: record.size })
  }, [])

  const selectedFiles = files.filter(f => selectedRowKeys.includes(f.file_id))

  // ---- Batch operation handlers ----

  const handleBatchDelete = useCallback(async () => {
    if (selectedRowKeys.length === 0) return
    Modal.confirm({
      title: `移入回收站 ${selectedRowKeys.length} 个项目？`,
      content: '可在回收站恢复；彻底删除才会释放存储空间。',
      onOk: async () => {
        const results = await Promise.allSettled(selectedFiles.map(item => item.is_dir ? api.deleteDir(item.file_id) : api.deleteFile(item.file_id)))
        const ok = results.filter(result => result.status === 'fulfilled').length
        const fail = results.length - ok
        addRecord({ type: 'delete', fileCount: selectedRowKeys.length, status: fail === 0 ? 'success' : 'partial', detail: `已移入回收站：${ok} 成功, ${fail} 失败` })
        setSelectedRowKeys([])
        if (fail) message.warning(`已移入回收站: ${ok} 成功, ${fail} 失败`)
        else message.success(`已移入回收站: ${ok} 项`)
        refreshFiles()
      },
    })
  }, [selectedRowKeys, selectedFiles, refreshFiles, addRecord, setSelectedRowKeys])

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
      refreshFiles()
    } catch (e: any) {
      message.error(`${dirPickerMode === 'move' ? '移动' : '复制'}失败: ${e.message}`)
    }
    setDirPickerOpen(false)
  }, [dirPickerMode, selectedRowKeys, refreshFiles, setSelectedRowKeys])

  const handleTagEditorConfirm = useCallback(async (tags: string[]) => {
    try {
      await api.batchSetTags(selectedRowKeys as string[], tags)
      message.success('标记完成')
      setSelectedRowKeys([])
      refreshFiles()
    } catch (e: any) {
      message.error(`标记失败: ${e.message}`)
    }
    setTagEditorOpen(false)
  }, [selectedRowKeys, refreshFiles, setSelectedRowKeys])

  // ---- New folder ----
  const handleNewFolder = useCallback(() => {
    const name = prompt('请输入目录名:')
    if (!name?.trim()) return
    // Create via upload an empty dir manifest
    const manifest = { entries: [] }
    api.submitDir(name.trim(), manifest.entries).then(() => {
      message.success('目录创建成功')
      refreshFiles()
    }).catch((e: any) => {
      message.error(`创建目录失败: ${e.message}`)
    })
  }, [refreshFiles])

  // ---- Upload ----
  const handleUploadFile = useCallback((file: File) => {
    void uploadCtx.customRequest(file).catch(() => undefined)
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
    <div className="workspace-shell flex flex-col flex-1">
      <TopBar
        search={search}
        typeFilter={typeFilter}
        searching={searchPending}
        onSearchChange={setSearch}
        onTypeFilterChange={setTypeFilter}
        onRefresh={refreshFiles}
      />

      <main className="workspace-page">
        <section className="workspace-hero">
          <div>
            <span className="workspace-eyebrow">SECURE FILE WORKSPACE</span>
            <h1>文件空间</h1>
            <p>上传、管理和分发团队文件。支持断点续传、分片校验与安全下载。</p>
          </div>
          <div className="workspace-hero__meta">
            <span>当前空间</span>
            <strong>{namespace}</strong>
          </div>
        </section>

        <section className="surface-card workspace-controls">
          <div className="workspace-controls__header">
            <BreadcrumbNav items={breadcrumbItems} />
            <Tooltip title="操作历史">
              <Button type="text" size="small" icon={<HistoryOutlined />} onClick={() => setHistoryOpen(!historyOpen)} />
            </Tooltip>
          </div>
          <div className="workspace-controls__actions">
            <ActionToolbar
              onUpload={handleUploadFile}
              onNewFolder={handleNewFolder}
              onDownload={handleSingleDownload}
              onDelete={handleSingleDelete}
              onRefresh={refreshFiles}
              hasSelection={selectedRowKeys.length > 0}
              hasSingleSelection={selectedFiles.length === 1}
            />
          </div>
          <UploadDropzone onUpload={handleUploadFile} />
        </section>

        {historyOpen && (
          <section className="mb-4">
            <BatchHistoryPanel open={true} onClose={() => setHistoryOpen(false)} history={batchHistory} onClear={() => {}} />
          </section>
        )}

        <section className="surface-card file-list-card">
          <div className="file-list-card__header">
            <div>
              <div className="file-list-card__title"><CloudUploadOutlined /> 文件与目录</div>
              <div className="file-list-card__sub">所有变更实时反映在当前文件空间</div>
            </div>
            <StatsBar dirs={stats.dirs} files={stats.files} totalSize={stats.totalSize} />
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
              onShowProperties={setPropertiesFile}
            />
          </ErrorBoundary>
        </section>

        {selectedRowKeys.length > 0 && (
          <BatchToolbar
            selectedCount={selectedRowKeys.length}
            onCancel={() => setSelectedRowKeys([])}
            onBatchDelete={handleBatchDelete}
            onBatchDownload={(fmt) => {
              void api.batchDownload(selectedRowKeys as string[], fmt)
                .then(blob => {
                  api.saveBlob(blob, `fileupload-${new Date().toISOString().slice(0, 10)}.${fmt === 'tar.gz' ? 'tar.gz' : fmt}`)
                  addRecord({ type: 'download', fileCount: selectedRowKeys.length, status: 'success', detail: `已打包为 ${fmt}` })
                })
                .catch((error: any) => message.error(`打包下载失败：${error.message || '请稍后重试'}`))
            }}
            onBatchMove={handleBatchMove}
            onBatchCopy={handleBatchCopy}
            onBatchTag={handleBatchTag}
          />
        )}
      </main>

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
      <PropertiesPanel file={propertiesFile} onClose={() => setPropertiesFile(null)} />
    </div>
  )}
