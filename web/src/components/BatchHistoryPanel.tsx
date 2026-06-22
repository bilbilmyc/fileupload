import { useState, useCallback } from 'react'
import { Modal, Table, Tag, Button, Empty, Space, Typography } from 'antd'
import { ClockCircleOutlined, ClearOutlined, DeleteOutlined, DownloadOutlined, ScissorOutlined, CopyOutlined, TagOutlined } from '@ant-design/icons'

const { Text } = Typography

export interface BatchHistoryItem {
  id: string
  type: 'delete' | 'download' | 'move' | 'copy' | 'tag'
  time: Date
  fileCount: number
  status: 'success' | 'partial' | 'failed'
  detail?: string
}

interface BatchHistoryPanelProps {
  open: boolean
  onClose: () => void
  history: BatchHistoryItem[]
  onClear: () => void
}

const TYPE_CONFIG: Record<string, { label: string; color: string; icon: React.ReactNode }> = {
  delete: { label: '删除', color: 'red', icon: <DeleteOutlined /> },
  download: { label: '下载', color: 'blue', icon: <DownloadOutlined /> },
  move: { label: '移动', color: 'orange', icon: <ScissorOutlined /> },
  copy: { label: '复制', color: 'green', icon: <CopyOutlined /> },
  tag: { label: '标记', color: 'purple', icon: <TagOutlined /> },
}

export default function BatchHistoryPanel({ open, onClose, history, onClear }: BatchHistoryPanelProps) {
  const columns = [
    {
      title: '操作',
      dataIndex: 'type',
      width: 80,
      render: (type: string) => {
        const cfg = TYPE_CONFIG[type] || {}
        return <Tag color={cfg.color}>{cfg.label || type}</Tag>
      },
    },
    {
      title: '数量',
      dataIndex: 'fileCount',
      width: 60,
      align: 'right' as const,
      render: (n: number) => <Text className="text-sm">{n}</Text>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 80,
      render: (status: string) => {
        const colors: Record<string, string> = { success: 'green', partial: 'orange', failed: 'red' }
        const labels: Record<string, string> = { success: '成功', partial: '部分', failed: '失败' }
        return <Tag color={colors[status]}>{labels[status] || status}</Tag>
      },
    },
    {
      title: '详情',
      dataIndex: 'detail',
      ellipsis: true,
      render: (d: string) => <Text className="text-xs text-gray-500">{d || '-'}</Text>,
    },
    {
      title: '时间',
      dataIndex: 'time',
      width: 160,
      render: (t: Date) => <Text className="text-xs text-gray-400">{t.toLocaleString()}</Text>,
    },
  ]

  return (
    <Modal
      title={
        <Space>
          <ClockCircleOutlined className="text-blue-500" />
          <span>批量操作历史</span>
        </Space>
      }
      open={open}
      onCancel={onClose}
      footer={
        history.length > 0 ? (
          <Space>
            <Text className="text-xs text-gray-400">共 {history.length} 条记录</Text>
            <Button size="small" icon={<ClearOutlined />} onClick={onClear}>
              清除历史
            </Button>
          </Space>
        ) : null
      }
      width={600}
    >
      {history.length === 0 ? (
        <Empty description="暂无操作历史" className="py-8" />
      ) : (
        <Table
          dataSource={[...history].reverse()}
          columns={columns}
          rowKey="id"
          size="small"
          pagination={{ pageSize: 10, size: 'small', showTotal: (t) => `共 ${t} 条` }}
          locale={{ emptyText: '暂无记录' }}
        />
      )}
    </Modal>
  )
}

export function useBatchHistory() {
  const [items, setItems] = useState<BatchHistoryItem[]>([])

  const addRecord = useCallback((record: Omit<BatchHistoryItem, 'id' | 'time'>) => {
    const newItem: BatchHistoryItem = {
      ...record,
      id: `batch-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`,
      time: new Date(),
    }
    setItems((prev: BatchHistoryItem[]) => [...prev, newItem])
    if (items.length >= 100) {
      setItems((prev: BatchHistoryItem[]) => prev.slice(-100))
    }
  }, [])

  return { items, addRecord }
}
