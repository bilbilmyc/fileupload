import { Table, Space, Tag, Button, Tooltip, Typography, Popconfirm, Spin } from 'antd'
import type React from 'react'
import {
  DownloadOutlined,
  DeleteOutlined,
  FolderOutlined,
  FileOutlined,
  EyeOutlined,
  FileImageOutlined,
  FilePdfOutlined,
  FileWordOutlined,
  FileExcelOutlined,
  FileZipOutlined,
  FileMarkdownOutlined,
  FileTextOutlined,
  PlayCircleOutlined,
  SoundOutlined,
  CodeOutlined,
} from '@ant-design/icons'
import type { FileItem } from '../api/client'
const { Text } = Typography

/** 根据文件名返回文件类型图标 + 颜色 */
function getFileIcon(name: string, isDir: boolean): { icon: React.ReactNode; color: string } {
  if (isDir) return { icon: <FolderOutlined />, color: '#d48806' }

  const ext = name.split('.').pop()?.toLowerCase() || ''

  // 图片
  if (['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg', 'bmp', 'ico'].includes(ext)) {
    return { icon: <FileImageOutlined />, color: '#52c41a' }
  }
  // PDF
  if (ext === 'pdf') return { icon: <FilePdfOutlined />, color: '#f5222d' }
  // 文档
  if (['doc', 'docx'].includes(ext)) return { icon: <FileWordOutlined />, color: '#1677ff' }
  if (['xls', 'xlsx', 'csv'].includes(ext)) return { icon: <FileExcelOutlined />, color: '#52c41a' }
  if (['ppt', 'pptx'].includes(ext)) return { icon: <FileTextOutlined />, color: '#fa8c16' }
  // Markdown / 文本
  if (['md', 'markdown'].includes(ext)) return { icon: <FileMarkdownOutlined />, color: '#1677ff' }
  if (['txt', 'log'].includes(ext)) return { icon: <FileTextOutlined />, color: '#8c8c8c' }
  // 压缩包
  if (['zip', 'tar', 'gz', 'bz2', 'xz', '7z', 'rar'].includes(ext)) {
    return { icon: <FileZipOutlined />, color: '#fa8c16' }
  }
  // 代码
  if (['js', 'jsx', 'ts', 'tsx', 'go', 'py', 'rb', 'rs', 'java', 'c', 'cpp', 'h', 'hpp', 'cs', 'swift', 'kt', 'sh', 'bash', 'css', 'scss', 'less', 'sql', 'html', 'htm', 'json', 'xml', 'yaml', 'yml', 'toml'].includes(ext)) {
    return { icon: <CodeOutlined />, color: '#722ed1' }
  }
  // 视频
  if (['mp4', 'webm', 'avi', 'mov', 'mkv'].includes(ext)) {
    return { icon: <PlayCircleOutlined />, color: '#eb2f96' }
  }
  // 音频
  if (['mp3', 'wav', 'ogg', 'flac', 'aac', 'm4a'].includes(ext)) {
    return { icon: <SoundOutlined />, color: '#1677ff' }
  }

  return { icon: <FileOutlined />, color: '#8c8c8c' }
}

function formatBytes(bytes: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)} ${units[i]}`
}

interface FileTableProps {
  files: FileItem[]
  loading: boolean
  page: number
  pageSize: number
  total: number
  selectedRowKeys: React.Key[]
  parentFileId?: string | null
  onPageChange: (page: number) => void
  onSelectionChange: (keys: React.Key[]) => void
  onNavigateToDir: (dirId: string) => void
  onNavigateUp?: () => void
  onDownload: (record: FileItem) => void
  onDelete: (record: FileItem) => void
  onPreview?: (record: FileItem) => void
}

export default function FileTable({
  files,
  loading,
  page,
  pageSize,
  total,
  selectedRowKeys,
  parentFileId,
  onPageChange,
  onSelectionChange,
  onNavigateToDir,
  onNavigateUp,
  onDownload,
  onDelete,
  onPreview,
}: FileTableProps) {
  const columns = [
    {
      title: '名称',
      dataIndex: 'name',
      ellipsis: true,
      render: (_: any, record: any) => {
        const fi = record.file_id === '__parent__' ? { icon: <FolderOutlined />, color: '#bfbfbf' } : getFileIcon(record.name, record.is_dir)
        const nameEl = <span
          className={`text-sm font-medium ${record.file_id === '__parent__' ? 'text-gray-500' : ''}`}
          onClick={() => {
            if (record.file_id === '__parent__' && onNavigateUp) onNavigateUp()
            else if (record.is_dir) onNavigateToDir(record.file_id)
          }}
        >
          {record.file_id === '__parent__' ? '返回上级目录' : record.name}
        </span>
        return <Space>
          <span style={{ color: fi.color, fontSize: 16 }}>{fi.icon}</span>
          {nameEl}
          {record.tags && record.tags.length > 0 && (
            <Space size={2}>
              {record.tags.map((tag: string) => (
                <Tag key={tag} className="text-xs" color="blue">{tag}</Tag>
              ))}
            </Space>
          )}
        </Space>
      },
    },
    {
      title: '大小',
      dataIndex: 'size',
      width: 100,
      align: 'right' as const,
      render: (size: number) => (
        <Text className="text-xs text-gray-500">{formatBytes(size)}</Text>
      ),
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
      render: (sha: string, record: any) => {
        if (record.file_id === '__parent__') return <Text className="text-xs text-gray-300">-</Text>
        return sha
          ? <Tooltip title={sha}><Tag className="text-xs font-mono">{sha.slice(0, 8)}</Tag></Tooltip>
          : <Text className="text-xs text-gray-300">-</Text>
      },
    },
    {
      title: '操作',
      width: 120,
      render: (_: any, record: any) => {
        if (record.file_id === '__parent__') return null
        const showPreview = onPreview && !record.is_dir
        return <Space size="small">
          {showPreview && (
            <Tooltip title="预览">
              <Button type="text" icon={<EyeOutlined />} size="small" onClick={() => onPreview!(record)} />
            </Tooltip>
          )}
          <Tooltip title="下载">
            <Button type="text" icon={<DownloadOutlined />} size="small" onClick={() => onDownload(record)} />
          </Tooltip>
          <Popconfirm title="确认删除？" onConfirm={() => onDelete(record)}>
            <Tooltip title="删除">
              <Button type="text" icon={<DeleteOutlined />} size="small" danger />
            </Tooltip>
          </Popconfirm>
        </Space>
      },
    },
  ]

  // 在非根目录时，添加"返回上级"条目到列表头部
  const parentDirEntry = parentFileId !== null && parentFileId !== undefined ? [{
    file_id: '__parent__',
    name: '..',
    is_dir: false,
    size: 0,
  } as any] : []

  const dataSource = [...parentDirEntry, ...files]

  return (
    <Spin spinning={loading}>
      <Table
        rowKey="file_id"
        dataSource={dataSource}
        columns={columns}
        pagination={{
          current: page,
          pageSize,
          total: total + (parentDirEntry.length > 0 ? 1 : 0),
          onChange: (p) => onPageChange(p),
          showSizeChanger: false,
          showTotal: (t) => `共 ${t - (parentDirEntry.length > 0 ? 1 : 0)} 项`,
          size: 'small',
        }}
        size="small"
        locale={{ emptyText: '暂无文件' }}
        onRow={(record) => ({
          onClick: () => {
            if (record.file_id === '__parent__' && onNavigateUp) {
              onNavigateUp()
            } else if (record.is_dir) {
              onNavigateToDir(record.file_id)
            }
          },
          className: record.file_id === '__parent__' ? 'cursor-pointer hover:bg-gray-50' : 'cursor-pointer hover:bg-gray-50',
        })}
        rowSelection={{
          selectedRowKeys: selectedRowKeys.filter(k => k !== '__parent__'),
          onChange: (keys) => onSelectionChange(keys),
          columnWidth: 40,
        }}
        className="[&_.ant-table-row]:cursor-default"
      />
    </Spin>
  )
}
