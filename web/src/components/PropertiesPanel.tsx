import { Drawer, Descriptions, Tag, Space, Button, Tooltip } from 'antd'
import { DownloadOutlined, CopyOutlined, LinkOutlined } from '@ant-design/icons'
import type { FileItem } from '../api/client'
import { useState } from 'react'
import { downloadFileUrl } from '../api/client'
import ShareLinkManager from './ShareLinkManager'

function formatBytes(bytes: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)} ${units[i]}`
}

interface PropertiesPanelProps {
  file: FileItem | null
  onClose: () => void
}

export default function PropertiesPanel({ file, onClose }: PropertiesPanelProps) {
  const [copied, setCopied] = useState(false)
  const [shareOpen, setShareOpen] = useState(false)

  if (!file) return null

  const handleCopyHash = async () => {
    if (file.sha256) {
      await navigator.clipboard.writeText(file.sha256)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }

  const fileTypeLabel = file.is_dir ? '目录' : '文件'
  const fileTypeColor = file.is_dir ? 'orange' : 'blue'

  return (
    <>
    <Drawer
      title={<span>📄 {file.name}</span>}
      placement="right"
      width={380}
      open={!!file}
      onClose={onClose}
    >
      <Descriptions column={1} size="small" bordered>
        <Descriptions.Item label="名称">{file.name}</Descriptions.Item>
        <Descriptions.Item label="类型">
          <Tag color={fileTypeColor}>{fileTypeLabel}</Tag>
        </Descriptions.Item>
        <Descriptions.Item label="大小">{formatBytes(file.size)}</Descriptions.Item>
        {file.is_dir && (
          <Descriptions.Item label="路径">{file.path || '/'}</Descriptions.Item>
        )}
        {file.sha256 && (
          <Descriptions.Item label="SHA-256">
            <Space size={4}>
              <code className="text-xs break-all">{file.sha256}</code>
              <Tooltip title={copied ? '已复制' : '复制'}>
                <Button type="text" size="small" icon={<CopyOutlined />} onClick={handleCopyHash} />
              </Tooltip>
            </Space>
          </Descriptions.Item>
        )}
        <Descriptions.Item label="创建时间">
          {file.created_at ? new Date(file.created_at).toLocaleString() : '-'}
        </Descriptions.Item>
        {file.tags && file.tags.length > 0 && (
          <Descriptions.Item label="标签">
            <Space size={4} wrap>
              {file.tags.map(tag => <Tag key={tag} color="blue">{tag}</Tag>)}
            </Space>
          </Descriptions.Item>
        )}
      </Descriptions>

      {!file.is_dir && (
        <div className="properties-actions">
          <Button type="primary" icon={<DownloadOutlined />} block href={downloadFileUrl(file.file_id)}>
            下载文件
          </Button>
          <Button icon={<LinkOutlined />} block onClick={() => setShareOpen(true)}>
            管理分享链接
          </Button>
        </div>
      )}
    </Drawer>
    <ShareLinkManager file={file} open={shareOpen} onClose={() => setShareOpen(false)} />
    </>
  )
}
