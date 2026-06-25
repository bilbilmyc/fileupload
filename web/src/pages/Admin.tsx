import { Card, Statistic, Row, Col, Spin, Alert } from 'antd'
import { FolderOutlined, FileOutlined, HddOutlined, CloudSyncOutlined } from '@ant-design/icons'
import { useEffect, useState } from 'react'
import { useAuth } from '../context/AuthContext'

interface AdminStatus {
  version: string
  storage: {
    data_dir: string
    total_files: number
    total_blobs: number
    total_size: number
  }
  database: { type: string; path: string }
  worker_pool: { capacity: number; available: number }
}

function formatBytes(n: number): string {
  if (!n) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1)
  return `${(n / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)} ${units[i]}`
}

export default function Admin() {
  const { namespace } = useAuth()
  const [status, setStatus] = useState<AdminStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    const token = localStorage.getItem('fileupload_token') || ''
    fetch(`/v1/admin/status?namespace=${encodeURIComponent(namespace)}`, {
      headers: {
        Authorization: `Bearer ${token}`,
        'X-Auth-Token': token,
        'X-Namespace': namespace,
      },
    })
      .then((r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`)
        return r.json()
      })
      .then((data: AdminStatus) => {
        if (!cancelled) setStatus(data)
      })
      .catch((e: Error) => {
        if (!cancelled) setError(e.message)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => { cancelled = true }
  }, [namespace])

  if (error) {
    return (
      <div className="p-6">
        <h2 className="text-lg font-bold mb-4">控制台</h2>
        <Alert type="error" message="加载失败" description={error} showIcon />
      </div>
    )
  }

  if (loading || !status) {
    return (
      <div className="p-6 flex items-center justify-center min-h-[300px]">
        <Spin size="large" tip="加载中..." />
      </div>
    )
  }

  return (
    <div className="p-6">
      <h2 className="text-lg font-bold mb-4 flex items-center gap-2">
        <CloudSyncOutlined className="text-blue-500" /> 控制台
        <span className="text-xs text-gray-400 font-normal">
          （namespace: {namespace} · {status.database.type}）
        </span>
      </h2>

      <Row gutter={16}>
        <Col span={6}>
          <Card>
            <Statistic
              title="目录数"
              value={status.storage.total_files}
              prefix={<FolderOutlined />}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="blob 数"
              value={status.storage.total_blobs}
              prefix={<FileOutlined />}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="存储空间"
              value={formatBytes(status.storage.total_size)}
              prefix={<HddOutlined />}
            />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic
              title="worker 池"
              value={`${status.worker_pool.available}/${status.worker_pool.capacity}`}
              suffix="可用"
            />
          </Card>
        </Col>
      </Row>

      <Card className="mt-4" title="系统信息">
        <Row gutter={[16, 8]}>
          <Col span={12}>
            <div className="text-xs text-gray-500">数据目录</div>
            <div className="font-mono text-sm break-all">{status.storage.data_dir}</div>
          </Col>
          <Col span={6}>
            <div className="text-xs text-gray-500">数据库</div>
            <div className="font-mono text-sm">{status.database.type}</div>
          </Col>
          <Col span={6}>
            <div className="text-xs text-gray-500">服务版本</div>
            <div className="font-mono text-sm">{status.version}</div>
          </Col>
        </Row>
      </Card>
    </div>
  )
}
