import { Button, Badge, Typography, Popconfirm, Dropdown } from 'antd'
import {
  DeleteOutlined,
  DownloadOutlined,
  ScissorOutlined,
  CopyOutlined,
  TagOutlined,
  DownOutlined,
} from '@ant-design/icons'

const { Text } = Typography

interface BatchToolbarProps {
  selectedCount: number
  onCancel: () => void
  onBatchDelete: () => void
  onBatchDownload: (format: string) => void
  onBatchMove: () => void
  onBatchCopy: () => void
  onBatchTag: () => void
}

export default function BatchToolbar({
  selectedCount,
  onCancel,
  onBatchDelete,
  onBatchDownload,
  onBatchMove,
  onBatchCopy,
  onBatchTag,
}: BatchToolbarProps) {
  if (selectedCount === 0) return null

  const downloadItems = [
    { key: 'zip', label: '打包为 ZIP', onClick: () => onBatchDownload('zip') },
    { key: 'tar.gz', label: '打包为 tar.gz', onClick: () => onBatchDownload('tar.gz') },
  ]

  return (
    <div className="mt-3 flex items-center gap-3 px-1 py-2 bg-blue-50 rounded-md">
      <Badge count={selectedCount} style={{ backgroundColor: '#1677ff' }}>
        <Text className="text-xs">已选</Text>
      </Badge>

      <Popconfirm title={`确认删除选中的 ${selectedCount} 个项目？`} onConfirm={onBatchDelete}>
        <Button icon={<DeleteOutlined />} danger size="small">
          批量删除
        </Button>
      </Popconfirm>

      <Dropdown menu={{ items: downloadItems }} trigger={['click']}>
        <Button icon={<DownloadOutlined />} size="small">
          批量下载 <DownOutlined />
        </Button>
      </Dropdown>

      <Button icon={<ScissorOutlined />} size="small" onClick={onBatchMove}>
        批量移动
      </Button>

      <Button icon={<CopyOutlined />} size="small" onClick={onBatchCopy}>
        批量复制
      </Button>

      <Button icon={<TagOutlined />} size="small" onClick={onBatchTag}>
        批量标记
      </Button>

      <Button size="small" onClick={onCancel}>
        取消选择
      </Button>
    </div>
  )
}
