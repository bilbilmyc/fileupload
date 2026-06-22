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
} from 'antd'
import {
  UploadOutlined,
  ReloadOutlined,
  DeleteOutlined,
  DownloadOutlined,
  FolderOutlined,
  FileOutlined,
} from '@ant-design/icons'
import { useAuth } from '../context/AuthContext'
import * as api from '../api/client'
import type { FileItem, UploadInitResult } from '../api/client'
import type { RcFile } from 'antd/es/upload'

const { Header, Content } = Layout
const { Title, Text } = Typography

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
  const [search, setSearch] = useState('')
  const [uploadTasks, setUploadTasks] = useState<UploadTask[]>([])
  const [configOpen, setConfigOpen] = useState(false)
  const [chunkSize, setChunkSize] = useState('10m')
  const [concurrency, setConcurrency] = useState(4)
  const [compression, setCompression] = useState<'none' | 'zstd'>('none')
  const [dirMode, setDirMode] = useState(false)

  const loadFiles = useCallback(async () => {
    setLoading(true)
    try {
      const res = await api.listFiles(parent)
      setFiles(res.children || [])
    } catch (e: any) {
      message.error(`加载失败: ${e.message}`)
    } finally {
      setLoading(false)
    }
  }, [parent])

  useEffect(() => {
    loadFiles()
  }, [loadFiles])

  const filteredFiles = useMemo(() => {
    if (!search) return files
    return files.filter((f) => f.name.toLowerCase().includes(search.toLowerCase()))
  }, [files, search])

  const columns = [
    {
      title: '名称',
      dataIndex: 'name',
      render: (_: any, record: FileItem) => (
        <Space>
          {record.is_dir ? <FolderOutlined /> : <FileOutlined />}
          <a
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
      width: 120,
      render: (size: number) => formatBytes(size),
    },
    {
      title: 'SHA256',
      dataIndex: 'sha256',
      width: 140,
      render: (sha?: string) => (sha ? <Tag>{sha.slice(0, 8)}</Tag> : '-'),
    },
    {
      title: '操作',
      width: 180,
      render: (_: any, record: FileItem) => (
        <Space>
          <Button
            icon={<DownloadOutlined />}
            size="small"
            onClick={() => handleDownload(record)}
          />
          <Popconfirm
            title="确认删除？"
            onConfirm={() => handleDelete(record)}
          >
            <Button icon={<DeleteOutlined />} size="small" danger />
          </Popconfirm>
        </Space>
      ),
    },
  ]

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

  // 目录上传批次跟踪（taskId → 目录信息）
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

    // 秒传预检
    if (sha256) {
      try {
        const exists = await api.checkExists(sha256, file.name)
        if (exists) {
          updateTask(taskId, { status: 'done', progress: 100 })
          message.success(`秒传: ${file.name}`)
          if (dirRelPath && dirName) recordDirFile(dirName, dirRelPath, exists.file_id)
          else loadFiles()
          return
        }
      } catch {
        // 继续上传
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
          const pct = Math.min(
            100,
            Math.round(((uploaded + ev.loaded) / size) * 100)
          )
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
        // 目录模式：记录文件路径，提交 manifest
        recordDirFile(dirName, dirRelPath, result.file_id)
      } else {
        message.success(`上传完成: ${file.name}`)
        loadFiles()
      }
    } catch (e: any) {
      updateTask(taskId, { status: 'error', error: e.message })
      throw e
    }
  }

  // 记录目录文件并检测是否完成
  const recordDirFile = (dirName: string, relPath: string, fileId: string) => {
    const batch = dirBatches.current.get(dirName)
    if (!batch) return
    batch.entries.push({ path: relPath, file_id: fileId })
    batch.done++
    updateTask(batch.taskId, { progress: Math.round((batch.done / batch.total) * 100) })

    if (batch.done >= batch.total) {
      // 所有文件上传完成，提交目录 manifest
      updateTask(batch.taskId, { status: 'finalizing', progress: 99 })
      api.submitDir(batch.dirName, batch.entries).then(() => {
        updateTask(batch.taskId, { status: 'done', progress: 100, name: `📁 ${batch.dirName}` })
        message.success(`目录上传完成: ${batch.dirName} (${batch.entries.length} 个文件)`)
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
    const task: UploadTask = {
      id,
      name: dirName ? `📁 ${dirName} — ${file.name}` : file.name,
      size: file.size,
      status: 'pending',
      progress: 0,
    }
    setUploadTasks((prev) => [...prev, task])
    return id
  }

  const customRequest = async (options: any) => {
    const { file, onSuccess, onError } = options

    if (dirMode && file.webkitRelativePath) {
      // ===== 目录模式 =====
      const parts = file.webkitRelativePath.split('/')
      const dirName = parts[0]
      const relPath = parts.slice(1).join('/')

      // 查找或创建批次
      let batch = dirBatches.current.get(dirName)
      if (!batch) {
        // 首次遇到此目录：先估算总文件数（创建一个占位任务）
        const placeholderId = `dir-${dirName}-${Date.now()}`
        const task: UploadTask = {
          id: placeholderId,
          name: `📁 ${dirName}`,
          size: 0,
          status: 'pending',
          progress: 0,
        }
        setUploadTasks((prev) => [...prev, task])
        // 暂存：稍后在收集完所有文件后更新 total
        batch = { dirName, entries: [], total: 0, done: 0, taskId: placeholderId }
        dirBatches.current.set(dirName, batch)
        dirBatches.current.set(placeholderId, batch)
      }

      batch.total++
      // 实际 total 在文件逐个到达时递增，用于进度计算

      // 每个文件单独一个 taskId 用于进度显示
      const taskId = addTask(file, dirName)
      try {
        await uploadSingleFile(file, taskId, relPath, dirName)
        onSuccess?.()
      } catch (e: any) {
        updateTask(taskId, { status: 'error', error: (e as Error).message })
        onError?.(e)
      }
    } else {
      // ===== 单文件/多文件模式 =====
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

  const handleUploadChange = () => {
    // 由 customRequest 处理
  }

  return (
    <Layout className="min-h-screen">
      <Header className="bg-white px-6 flex items-center justify-between shadow">
        <Title level={4} className="!mb-0">
          📦 fileupload 管理面板
        </Title>
        <Space>
          <Input
            placeholder="Namespace"
            value={namespace}
            onChange={(e) => setNamespace(e.target.value)}
            style={{ width: 120 }}
          />
          <Button icon={<ReloadOutlined />} onClick={loadFiles}>
            刷新
          </Button>
          <Button onClick={() => setConfigOpen(true)}>设置</Button>
          {isAuthenticated && <Button onClick={logout}>退出</Button>}
        </Space>
      </Header>

      <Content className="p-6">
        <Space direction="vertical" className="w-full" size="large">
          <div className="bg-white p-6 rounded shadow">
            <div className="flex justify-between mb-4">
              <div>
                <Title level={5}>上传</Title>
                <Text type="secondary">
                  拖拽文件或目录到下方{dirMode ? '（目录模式）' : '（单文件/多文件）'}
                </Text>
              </div>
              <Space>
                <Text>目录模式</Text>
                <Switch checked={dirMode} onChange={setDirMode} />
              </Space>
            </div>
            <Upload.Dragger
              multiple={!dirMode}
              directory={dirMode}
              customRequest={customRequest}
              onChange={handleUploadChange}
              showUploadList={false}
            >
              <p className="ant-upload-drag-icon">
                <UploadOutlined />
              </p>
              <p>点击或拖拽文件{dirMode ? '夹' : ''}到此处上传</p>
            </Upload.Dragger>

            {uploadTasks.length > 0 && (
              <div className="mt-4 space-y-2">
                {uploadTasks.map((t) => (
                  <div key={t.id} className="bg-gray-50 p-3 rounded">
                    <div className="flex justify-between text-sm">
                      <span className="truncate max-w-md">{t.name}</span>
                      <span>
                        {t.status === 'done'
                          ? '完成'
                          : t.status === 'error'
                          ? `失败: ${t.error || ''}`
                          : `${t.status} ${t.speed || ''}`}
                      </span>
                    </div>
                    <Progress percent={t.progress} status={t.status === 'error' ? 'exception' : undefined} />
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className="bg-white p-6 rounded shadow">
            <div className="flex justify-between mb-4">
              <Breadcrumb>
                <Breadcrumb.Item>
                  <a onClick={() => setParent('/')}>根目录</a>
                </Breadcrumb.Item>
                {parent !== '/' && (
                  <Breadcrumb.Item>
                    <span className="text-gray-500">{parent.slice(0, 12)}...</span>
                  </Breadcrumb.Item>
                )}
              </Breadcrumb>
              <Input
                placeholder="搜索文件"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                style={{ width: 200 }}
              />
            </div>
            <Spin spinning={loading}>
              <Table
                rowKey="file_id"
                dataSource={filteredFiles}
                columns={columns}
                pagination={false}
                locale={{ emptyText: '暂无文件' }}
              />
            </Spin>
          </div>
        </Space>
      </Content>

      <Modal
        title="上传设置"
        open={configOpen}
        onOk={() => setConfigOpen(false)}
        onCancel={() => setConfigOpen(false)}
      >
        <Form layout="vertical">
          <Form.Item label="分片大小">
            <Input value={chunkSize} onChange={(e) => setChunkSize(e.target.value)} />
          </Form.Item>
          <Form.Item label="并发数">
            <Select
              value={concurrency}
              onChange={(v) => setConcurrency(v)}
              options={[1, 2, 4, 8, 16].map((n) => ({ value: n, label: n }))}
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
    case 'k':
      return n * 1024
    case 'm':
      return n * 1024 * 1024
    case 'g':
      return n * 1024 * 1024 * 1024
    default:
      return n
  }
}
