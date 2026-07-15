import { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, Card, Empty, message, Popconfirm, Space, Spin, Table, Tag, Tooltip, Typography } from 'antd'
import { DeleteOutlined, FolderOutlined, ReloadOutlined, UndoOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import { useAuth } from '../context/AuthContext'
import * as api from '../api/client'

const { Text, Title } = Typography

function formatBytes(size: number) {
  if (!size) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(Math.floor(Math.log(size) / Math.log(1024)), units.length - 1)
  return `${(size / (1024 ** index)).toFixed(index ? 1 : 0)} ${units[index]}`
}

export default function Trash() {
  const { namespace } = useAuth()
  const [items, setItems] = useState<api.FileItem[]>([])
  const [loading, setLoading] = useState(true)
  const [busyID, setBusyID] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setItems(await api.listTrash())
    } catch {
      message.error('无法加载回收站，请稍后重试')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void load() }, [load, namespace])

  const summary = useMemo(() => ({
    files: items.filter(item => !item.is_dir).length,
    folders: items.filter(item => item.is_dir).length,
    size: items.filter(item => !item.is_dir).reduce((total, item) => total + item.size, 0),
  }), [items])

  const restore = async (id: string) => {
    setBusyID(id)
    try {
      await api.restoreTrash(id)
      message.success('已恢复到原位置')
      await load()
    } catch {
      message.error('恢复失败：原目录可能已被彻底删除或服务暂不可用')
    } finally {
      setBusyID(null)
    }
  }

  const purge = async (id: string) => {
    setBusyID(id)
    try {
      await api.purgeTrash(id)
      message.success('已彻底删除')
      await load()
    } catch {
      message.error('彻底删除失败，请稍后重试')
    } finally {
      setBusyID(null)
    }
  }

  return (
    <main className="workspace-page trash-page">
      <section className="workspace-hero">
        <div>
          <div className="eyebrow"><SafetyCertificateOutlined /> 可恢复删除</div>
          <Title level={2}>回收站</Title>
          <p>删除的项目会保留原始内容与目录关系。恢复不会重新上传；彻底删除才会释放存储空间。</p>
        </div>
        <Tooltip title="刷新回收站"><Button icon={<ReloadOutlined />} onClick={() => void load()} loading={loading}>刷新</Button></Tooltip>
      </section>

      <section className="trash-summary" aria-label="回收站统计">
        <Card bordered={false}><span>已删除文件</span><strong>{summary.files}</strong></Card>
        <Card bordered={false}><span>已删除目录</span><strong>{summary.folders}</strong></Card>
        <Card bordered={false}><span>待释放空间</span><strong>{formatBytes(summary.size)}</strong></Card>
      </section>

      <Card className="surface-card trash-table-card" bordered={false}>
        <div className="trash-table-card__head">
          <div><h2>待处理项目</h2><Text type="secondary">恢复后将回到删除前的位置；彻底删除不可撤销。</Text></div>
          <Tag color="gold">{items.length} 项</Tag>
        </div>
        <Spin spinning={loading}>
          {items.length === 0 && !loading ? <Empty description="回收站为空" image={Empty.PRESENTED_IMAGE_SIMPLE} /> : (
            <Table
              rowKey="file_id"
              size="middle"
              pagination={{ pageSize: 20, showSizeChanger: false, showTotal: total => `共 ${total} 项` }}
              dataSource={items}
              columns={[
                {
                  title: '项目', dataIndex: 'name',
                  render: (name: string, record: api.FileItem) => <Space><span className="trash-file-icon">{record.is_dir ? <FolderOutlined /> : '◧'}</span><strong>{name}</strong></Space>,
                },
                { title: '原始路径', dataIndex: 'path', render: (path: string) => <code>{path || '/'}</code> },
                { title: '大小', dataIndex: 'size', width: 120, align: 'right' as const, render: (size: number, record: api.FileItem) => record.is_dir ? '—' : formatBytes(size) },
                { title: '删除时间', dataIndex: 'deleted_at', width: 180, render: (value: string) => value ? new Date(value).toLocaleString() : '—' },
                {
                  title: '操作', width: 190,
                  render: (_: unknown, record: api.FileItem) => <Space>
                    <Button size="small" type="primary" icon={<UndoOutlined />} loading={busyID === record.file_id} onClick={() => void restore(record.file_id)}>恢复</Button>
                    <Popconfirm title="彻底删除这个项目？" description="此操作不可恢复，且可能释放底层存储内容。" okText="彻底删除" okButtonProps={{ danger: true }} cancelText="取消" onConfirm={() => void purge(record.file_id)}>
                      <Button size="small" danger icon={<DeleteOutlined />} loading={busyID === record.file_id}>彻底删除</Button>
                    </Popconfirm>
                  </Space>,
                },
              ]}
            />
          )}
        </Spin>
      </Card>
    </main>
  )
}

