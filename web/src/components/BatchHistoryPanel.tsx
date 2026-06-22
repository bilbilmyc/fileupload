import { useState, useCallback } from 'react'
import { Modal, Timeline, Typography, Button, Empty, Space, Tag } from 'antd'
import { ClockCircleOutlined, ClearOutlined } from '@ant-design/icons'

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

const TYPE_LABELS: Record<string, string> = {
  delete: '批量删除',
  download: '批量下载',
  move: '批量移动',
  copy: '批量复制',
  tag: '批量标记',
}

const TYPE_COLORS: Record<string, string> = {
  delete: 'red',
  download: 'blue',
  move: 'orange',
  copy: 'green',
  tag: 'purple',
}

const STATUS_DOTS: Record<string, string> = {
  success: 'green',
  partial: 'orange',
  failed: 'red',
}

export default function BatchHistoryPanel({ open, onClose, history, onClear }: BatchHistoryPanelProps) {

  return (
    <Modal
      title={
        <Space>
          <ClockCircleOutlined />
          <span>批量操作历史</span>
        </Space>
      }
      open={open}
      onCancel={onClose}
      footer={
        history.length > 0 ? (
          <Button size="small" icon={<ClearOutlined />} onClick={onClear}>
            清除历史
          </Button>
        ) : null
      }
      width={500}
    >
      {history.length === 0 ? (
        <Empty description="暂无操作历史" className="py-8" />
      ) : (
        <Timeline
          items={history.slice().reverse().map((item) => ({
            color: STATUS_DOTS[item.status],
            children: (
              <div key={item.id}>
                <Space>
                  <Tag color={TYPE_COLORS[item.type]}>
                    {TYPE_LABELS[item.type] || item.type}
                  </Tag>
                  <Text className="text-sm">{item.fileCount} 个文件</Text>
                  {item.status === 'success' && <Tag color="success">全部成功</Tag>}
                  {item.status === 'partial' && <Tag color="warning">部分成功</Tag>}
                  {item.status === 'failed' && <Tag color="error">失败</Tag>}
                </Space>
                {item.detail && (
                  <div className="text-xs text-gray-400 mt-1">{item.detail}</div>
                )}
                <div className="text-xs text-gray-300 mt-0.5">
                  {item.time.toLocaleString()}
                </div>
              </div>
            ),
          }))}
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
    // Keep max 100 records
    setItems((prev: BatchHistoryItem[]) => prev.length > 100 ? prev.slice(-100) : prev)
  }, [])

  return { items, addRecord }
}
