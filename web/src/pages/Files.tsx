import { useEffect, useMemo, useState, useCallback, useRef } from 'react'
import {
  Layout,
  Space,
  Input,
  Button,
  Table,
  Tag,
  Upload,
  Progress,
  Modal,
  Form,
  Select,
  message,
  Typography,
  Breadcrumb,
  Spin,
  Popconfirm,
  Switch,
  Card,
  Tooltip,
  Badge,
} from 'antd'
import {
  UploadOutlined,
  ReloadOutlined,
  DeleteOutlined,
  DownloadOutlined,
  FolderOutlined,
  FileOutlined,
  InboxOutlined,
  SettingOutlined,
  ClearOutlined,
  SearchOutlined,
  FolderOpenOutlined,
} from '@ant-design/icons'
import { useAuth } from '../context/AuthContext'
import * as api from '../api/client'
import type { FileItem, UploadInitResult } from '../api/client'
import type { RcFile } from 'antd/es/upload'

const { Header, Content } = Layout
const { Title, Text } = Typography
const { Dragger } = Upload
const PAGE_SIZE = 50

interface UploadTask {
  id: string
  name: string
  size: number
  status: 'pending' | 'hashing' | 'uploading' | 'finalizing' | 'done' | 'error'
  progress: number
  error?: string
  speed?: string
}

function formatBytes(bytes: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)} ${units[i]}`
}

function sha256Hex(buffer: ArrayBuffer): string {
  const arr = new Uint8Array(buffer)
  return Array.from(arr)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
}

async function fileSHA256(file: Blob): Promise<string> {
  const buf = await file.arrayBuffer()
  const hash = await crypto.subtle.digest('SHA-256', buf)
  return sha256Hex(hash)
}

async function chunkSHA256(chunk: ArrayBuffer): Promise<string> {
  const hash = await crypto.subtle.digest('SHA-256', chunk)
  return sha256Hex(hash)
}

export default function Files() {
  const { namespace, setNamespace, isAuthenticated, logout } = useAuth()
  const [files, setFiles] = useState<FileItem[]>([])
  const [loading, setLoading] = useState(false)
  const [parent, setParent] = useState<string>('/')
  const [parentName, setParentName] = useState<string>('')
  const [search, setSearch] = useState('')
  const [uploadTasks, setUploadTasks] = useState<UploadTask[]>([])
  const [configOpen, setConfigOpen] = useState(false)
  const [chunkSize, setChunkSize] = useState('10m')
  const [concurrency, setConcurrency] = useState<number | 'auto'>(4)
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([])
  const [compression, setCompression] = useState<'none' | 'zstd'>('none')
  const [dirMode, setDirMode] = useState(false)
  const [showUpload, setShowUpload] = useState(false)
  const [page, setPage] = useState(1)

  const loadFiles = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.listFiles(parent)
      setFiles(res.children || [])
      if (res.dir && typeof res.dir === 'object' && 'name' in res.dir) {
        setParentName((res.dir as any).name as string)
      } else {
        setParentName('')
      }
    } catch (e: any) {
      message.error(`加载失败: ${e.message}`)
    } finally {
      setLoading(false)
    }
    setPage(1)
    setSelectedRowKeys([])
  }, [parent])

  useEffect(() => {
    loadFiles()
  }, [loadFiles])

  const filteredFiles = useMemo(() => {
    if (!search) return files
    return files.filter((f) => f.name.toLowerCase().includes(search.toLowerCase()))
  }, [files, search])

  // Paginated files
  const paginatedFiles = useMemo(() => {
    const start = (page - 1) * PAGE_SIZE
    return filteredFiles.slice(start, start + PAGE_SIZE)
  }, [filteredFiles, page])

  const columns = [
    {
      title: '名称',
      dataIndex: 'name',
      ellipsis: true,
      render: (_: any, record: FileItem) => (
        <Space>
          {record.is_dir
            ? <FolderOutlined className="text-amber-500" />
            : <FileOutlined className="text-blue-400" />}
          <a
            className="text-sm font-medium"
            onClick={() => {
              if (record.is_dir) setParent(record.file_id)
            }}
          >
            {record.name}
          </a>
        </Space>
      ),
    },
    {
      title: '大小',
      dataIndex: 'size',
      width: 100,
      align: 'right' as const,
      render: (size: number) => <Text className="text-xs text-gray-500">{formatBytes(size)}</Text>,
    },
    {
      title: '类型',
      width: 80,
      render: (_: any, record: FileItem) => (
        record.is_dir
          ? <Tag color="orange" className="text-xs">目录</Tag>
          : <Tag className="text-xs">文件</Tag>
      ),
    },
    {
      title: 'SHA256',
      dataIndex: 'sha256',
      width: 110,
      render: (sha?: string) => (
        sha
          ? <Tooltip title={sha}><Tag className="text-xs font-mono">{sha.slice(0, 8)}</Tag></Tooltip>
          : <Text className="text-xs text-gray-300">-</Text>
      ),
    },
    {
      title: '操作',
      width: 120,
      render: (_: any, record: FileItem) => (
        <Space size="small">
          <Tooltip title="下载">
            <Button
              type="text"
              icon={<DownloadOutlined />}
              size="small"
              onClick={() => handleDownload(record)}
            />
          </Tooltip>
          <Popconfirm title="确认删除？" onConfirm={() => handleDelete(record)}>
            <Tooltip title="删除">
              <Button type="text" icon={<DeleteOutlined />} size="small" danger />
            </Tooltip>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  // Stats
  const stats = useMemo(() => {
    let d = 0, f = 0, totalSize = 0
    for (const item of files) {
      if (item.is_dir) d++
      else { f++; totalSize += item.size }
    }
    return { dirs: d, files: f, totalSize }
  }, [files])

  const breadcrumbItems = useMemo(() => {
    const items: any[] = [{ title: <a onClick={() => setParent('/')}><FolderOpenOutlined /> 根目录</a> }]
    if (parent !== '/' && parentName) {
      items.push({ title: <span className="text-gray-500"><FolderOutlined /> {parentName.slice(0, 16)}</span> })
    }
    return items
  }, [parent, parentName])

  const handleDownload = (record: FileItem) => {
    const url = record.is_dir
      ? api.downloadDirUrl(record.file_id)
      : api.downloadFileUrl(record.file_id)
    const a = document.createElement('a')
    a.href = url
    a.download = record.name
    a.click()
  }

  const handleDelete = async (record: FileItem) => {
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
  }

  const handleBatchDelete = async () => {
    const items = files.filter(f => selectedRowKeys.includes(f.file_id))
    if (items.length === 0) return
    Modal.confirm({
      title: `确认删除 ${items.length} 个项目？`,
      content: '删除后不可恢复。目录删除仅移除目录记录，不删除子文件。',
      onOk: async () => {
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
      },
    })
  }

  // 目录上传批次跟踪
  const dirBatches = useRef<Map<string, {
    dirName: string
    entries: { path: string; file_id: string }[]
    total: number
    done: number
    taskId: string
  }>>(new Map())

  const uploadSingleFile = async (file: RcFile, taskId: string, dirRelPath?: string, dirName?: string) => {
    const size = file.size
    updateTask(taskId, { status: 'hashing', progress: 0 })

    let sha256 = ''
    try {
      sha256 = await fileSHA256(file)
    } catch (e) {
      console.warn('SHA-256 计算失败', e)
    }

    if (sha256) {
      try {
        const exists = await api.checkExists(sha256, file.name)
        if (exists) {
          updateTask(taskId, { status: 'done', progress: 100 })
          if (dirRelPath && dirName) recordDirFile(dirName, dirRelPath, exists.file_id)
          else loadFiles()
          return
        }
      } catch {
        // continue
      }
    }

    const chunkBytes = parseChunkSize(chunkSize)
    let init: UploadInitResult
    try {
      init = await api.initUpload(size, sha256, file.name, compression)
    } catch (e: any) {
      updateTask(taskId, { status: 'error', error: e.message })
      throw e
    }

    const totalChunks = Math.ceil(size / chunkBytes)
    const startTime = Date.now()
    let uploaded = 0

    for (let i = 0; i < totalChunks; i++) {
      const start = i * chunkBytes
      const end = Math.min(start + chunkBytes, size)
      const chunk = file.slice(start, end)
      const sliceSha = await chunkSHA256(await chunk.arrayBuffer())

      await api.uploadChunk(init.session_id, i, chunk, sliceSha, (ev) => {
        if (ev.total) {
          const pct = Math.min(100, Math.round(((uploaded + ev.loaded) / size) * 100))
          const elapsed = (Date.now() - startTime) / 1000
          const speed = elapsed > 0 ? formatBytes((uploaded + ev.loaded) / elapsed) + '/s' : ''
          updateTask(taskId, { status: 'uploading', progress: pct, speed })
        }
      })
      uploaded += end - start
    }

    updateTask(taskId, { status: 'finalizing', progress: 99 })
    try {
      const result = await api.finalizeUpload(init.session_id)
      updateTask(taskId, { status: 'done', progress: 100 })
      if (dirRelPath && dirName) {
        recordDirFile(dirName, dirRelPath, result.file_id)
      } else {
        if (!showUpload) setShowUpload(true)
        loadFiles()
      }
    } catch (e: any) {
      updateTask(taskId, { status: 'error', error: e.message })
      throw e
    }
  }

  const recordDirFile = (dirName: string, relPath: string, fileId: string) => {
    const batch = dirBatches.current.get(dirName)
    if (!batch) return
    batch.entries.push({ path: relPath, file_id: fileId })
    batch.done++
    updateTask(batch.taskId, { progress: Math.round((batch.done / batch.total) * 100) })
    if (batch.done >= batch.total) {
      updateTask(batch.taskId, { status: 'finalizing', progress: 99 })
      api.submitDir(batch.dirName, batch.entries).then(() => {
        updateTask(batch.taskId, { status: 'done', progress: 100, name: `📁 ${batch.dirName}` })
        dirBatches.current.delete(dirName)
        dirBatches.current.delete(batch.taskId)
        loadFiles()
      }).catch((e: any) => {
        updateTask(batch.taskId, { status: 'error', error: e.message })
      })
    }
  }

  const updateTask = (id: string, patch: Partial<UploadTask>) => {
    setUploadTasks((prev) =>
      prev.map((t) => (t.id === id ? { ...t, ...patch } : t))
    )
  }

  const addTask = (file: RcFile, dirName?: string): string => {
    const id = `${file.name}-${Date.now()}-${Math.random()}`
    setUploadTasks((prev) => [...prev, {
      id,
      name: dirName ? `📁 ${dirName} — ${file.name}` : file.name,
      size: file.size,
      status: 'pending',
      progress: 0,
    }])
    return id
  }

  const customRequest = async (options: any) => {
    const { file, onSuccess, onError } = options
    if (dirMode && file.webkitRelativePath) {
      const parts = file.webkitRelativePath.split('/')
      const dirName = parts[0]
      const relPath = parts.slice(1).join('/')
      let batch = dirBatches.current.get(dirName)
      if (!batch) {
        const placeholderId = `dir-${dirName}-${Date.now()}`
        setUploadTasks((prev) => [...prev, {
          id: placeholderId,
          name: `📁 ${dirName}`,
          size: 0,
          status: 'pending',
          progress: 0,
        }])
        batch = { dirName, entries: [], total: 0, done: 0, taskId: placeholderId }
        dirBatches.current.set(dirName, batch)
        dirBatches.current.set(placeholderId, batch)
      }
      batch.total++
      const taskId = addTask(file, dirName)
      try {
        await uploadSingleFile(file, taskId, relPath, dirName)
        onSuccess?.()
      } catch (e: any) {
        updateTask(taskId, { status: 'error', error: (e as Error).message })
        onError?.(e)
      }
    } else {
      const taskId = addTask(file)
      try {
        await uploadSingleFile(file, taskId)
        onSuccess?.()
      } catch (e: any) {
        updateTask(taskId, { status: 'error', error: (e as Error).message })
        onError?.(e)
      }
    }
  }

  const clearDoneTasks = () => {
    setUploadTasks((prev) => prev.filter(t => t.status !== 'done'))
  }

  const hasActiveUploads = uploadTasks.some(t => t.status === 'uploading' || t.status === 'hashing' || t.status === 'finalizing')

  return (
    <Layout className="min-h-screen bg-gray-50">
      {/* ===== Top Bar ===== */}
      <Header className="bg-white px-6 flex items-center justify-between shadow-sm border-b border-gray-200" style={{ height: 56, lineHeight: '56px' }}>
        <div className="flex items-center gap-3">
          <Title level={5} className="!mb-0 text-gray-800">
            📦 fileupload
          </Title>
          <Text className="text-xs text-gray-400 bg-gray-100 px-2 py-0.5 rounded">dev</Text>
        </div>
        <Space size="middle">
          <Input
            size="small"
            prefix={<SearchOutlined className="text-gray-400" />}
            placeholder="搜索文件..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            style={{ width: 200 }}
            allowClear
          />
          <Input
            size="small"
            placeholder="Namespace"
            value={namespace}
            onChange={(e) => setNamespace(e.target.value)}
            style={{ width: 120 }}
          />
          <Tooltip title="刷新">
            <Button type="text" icon={<ReloadOutlined />} onClick={loadFiles} />
          </Tooltip>
          <Tooltip title="上传设置">
            <Button type="text" icon={<SettingOutlined />} onClick={() => setConfigOpen(true)} />
          </Tooltip>
          {isAuthenticated && (
            <Button size="small" onClick={logout}>退出</Button>
          )}
        </Space>
      </Header>

      <Content className="px-6 py-4" style={{ maxWidth: 1200, margin: '0 auto' }}>
        <Space direction="vertical" className="w-full" size="middle">
          {/* ===== Stats Bar ===== */}
          <div className="flex items-center justify-between">
            <Breadcrumb items={breadcrumbItems} className="text-sm" />
            <Space size="large" className="text-xs text-gray-400">
              <span>{stats.dirs} 个目录</span>
              <span>{stats.files} 个文件</span>
              <span>共 {formatBytes(stats.totalSize)}</span>
            </Space>
          </div>

          {/* ===== Upload Area ===== */}
          <Card
            size="small"
            className="shadow-sm"
            title={
              <Space>
                <UploadOutlined className="text-blue-500" />
                <span className="text-sm font-medium">上传</span>
              </Space>
            }
            extra={
              <Space size="small">
                <Text className="text-xs text-gray-400">目录模式</Text>
                <Switch size="small" checked={dirMode} onChange={setDirMode} />
                {!showUpload && (
                  <Button size="small" type="primary" ghost icon={<UploadOutlined />} onClick={() => setShowUpload(true)}>
                    展开
                  </Button>
                )}
              </Space>
            }
          >
            {showUpload ? (
              <>
                <Dragger
                  multiple={!dirMode}
                  directory={dirMode}
                  customRequest={customRequest}
                  showUploadList={false}
                  className="bg-gray-50 border-dashed"
                >
                  <p className="text-3xl text-blue-400 !mb-2"><InboxOutlined /></p>
                  <p className="text-sm text-gray-500">点击或拖拽文件{dirMode ? '夹' : ''}到此处</p>
                </Dragger>
                {uploadTasks.length > 0 && (
                  <div className="mt-3 space-y-1.5">
                    <div className="flex justify-between items-center">
                      <Text className="text-xs text-gray-400">
                        {uploadTasks.filter(t => t.status === 'done').length}/{uploadTasks.length}
                        {hasActiveUploads && <Spin size="small" className="ml-2" />}
                      </Text>
                      <Button type="link" size="small" onClick={clearDoneTasks} className="text-xs">
                        <ClearOutlined /> 清除已完成
                      </Button>
                    </div>
                    {uploadTasks.map((t) => (
                      <div key={t.id} className="flex items-center gap-3 py-1">
                        <div className="flex-1 min-w-0">
                          <div className="flex justify-between text-xs">
                            <span className="truncate">{t.name}</span>
                            <span className="text-gray-400 shrink-0 ml-2">
                              {t.status === 'done' ? '✓'
                                : t.status === 'error' ? `✗ ${t.error || ''}`
                                : `${t.speed || ''}`}
                            </span>
                          </div>
                          <Progress
                            percent={t.progress}
                            size="small"
                            status={t.status === 'error' ? 'exception' : undefined}
                            showInfo={false}
                            strokeColor={t.status === 'done' ? '#52c41a' : undefined}
                          />
                        </div>
                      </div>
                    ))}
                  </div>
                )}
                <Button size="small" type="text" onClick={() => setShowUpload(false)} className="mt-2 text-xs text-gray-400">
                  收起
                </Button>
              </>
            ) : (
              <div className="flex items-center justify-center py-2">
                <Button type="dashed" icon={<UploadOutlined />} onClick={() => setShowUpload(true)} className="text-gray-400">
                  点击展开上传区域
                </Button>
              </div>
            )}
          </Card>

          {/* ===== File Table ===== */}
          <Card
            size="small"
            className="shadow-sm"
            title={
              <Space>
                <FolderOpenOutlined className="text-amber-500" />
                <span className="text-sm font-medium">文件列表</span>
              </Space>
            }
          >
            <Spin spinning={loading}>
              <Table
                rowKey="file_id"
                dataSource={paginatedFiles}
                columns={columns}
                pagination={{
                  current: page,
                  pageSize: PAGE_SIZE,
                  total: filteredFiles.length,
                  onChange: (p) => setPage(p),
                  showSizeChanger: false,
                  showTotal: (total) => `共 ${total} 项`,
                  size: 'small',
                }}
                size="small"
                locale={{ emptyText: '暂无文件' }}
                rowSelection={{
                  selectedRowKeys,
                  onChange: (keys) => setSelectedRowKeys(keys),
                  columnWidth: 40,
                }}
                className="[&_.ant-table-row]:cursor-default"
              />
              {selectedRowKeys.length > 0 && (
                <div className="mt-3 flex items-center gap-3 px-1">
                  <Badge count={selectedRowKeys.length} style={{ backgroundColor: '#1677ff' }}>
                    <Text className="text-xs">已选</Text>
                  </Badge>
                  <Popconfirm title={`确认删除选中的 ${selectedRowKeys.length} 个项目？`} onConfirm={handleBatchDelete}>
                    <Button icon={<DeleteOutlined />} danger size="small">批量删除</Button>
                  </Popconfirm>
                  <Button size="small" onClick={() => setSelectedRowKeys([])}>取消选择</Button>
                </div>
              )}
            </Spin>
          </Card>
        </Space>
      </Content>

      {/* ===== Settings Modal ===== */}
      <Modal
        title="上传设置"
        open={configOpen}
        onOk={() => setConfigOpen(false)}
        onCancel={() => setConfigOpen(false)}
        width={400}
        footer={<Button type="primary" onClick={() => setConfigOpen(false)}>关闭</Button>}
      >
        <Form layout="vertical">
          <Form.Item label="并发数" help="auto 根据文件大小自动选择">
            <Select
              value={concurrency}
              onChange={(v) => setConcurrency(v)}
              options={[
                { value: 'auto', label: 'auto（推荐）' },
                { value: 1, label: '1' },
                { value: 2, label: '2' },
                { value: 4, label: '4' },
                { value: 8, label: '8' },
                { value: 16, label: '16' },
              ]}
            />
          </Form.Item>
          <Form.Item label="压缩">
            <Select
              value={compression}
              onChange={(v) => setCompression(v)}
              options={[
                { value: 'none', label: '无' },
                { value: 'zstd', label: 'zstd' },
              ]}
            />
          </Form.Item>
          <details className="mt-2 text-gray-400 text-xs cursor-pointer">
            <summary className="select-none">高级选项</summary>
            <div className="mt-2 pl-2 border-l-2 border-gray-200">
              <Form.Item label="分片大小（如 10m / 100m）" help="大文件分片上传时的每片大小">
                <Input value={chunkSize} onChange={(e) => setChunkSize(e.target.value)} />
              </Form.Item>
            </div>
          </details>
        </Form>
      </Modal>
    </Layout>
  )
}

function parseChunkSize(v: string): number {
  if (!v) return 10 * 1024 * 1024
  const match = v.match(/^(\d+)([kmg]?)$/i)
  if (!match) return 10 * 1024 * 1024
  const n = parseInt(match[1], 10)
  const unit = match[2].toLowerCase()
  switch (unit) {
    case 'k': return n * 1024
    case 'm': return n * 1024 * 1024
    case 'g': return n * 1024 * 1024 * 1024
    default: return n
  }
}
