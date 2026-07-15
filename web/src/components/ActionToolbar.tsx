import { Button, Space, Tooltip, Upload } from 'antd'
import {
  UploadOutlined,
  FolderAddOutlined,
  DownloadOutlined,
  DeleteOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import type { UploadProps } from 'antd'

interface ActionToolbarProps {
  onUpload: (file: File) => void
  onNewFolder: () => void
  onDownload: () => void
  onDelete: () => void
  onRefresh: () => void
  hasSelection: boolean
  hasSingleSelection: boolean
}

export default function ActionToolbar({
  onUpload, onNewFolder, onDownload, onDelete, onRefresh,
  hasSelection, hasSingleSelection,
}: ActionToolbarProps) {
  const uploadProps: UploadProps = {
    beforeUpload: (file) => {
      onUpload(file as File)
      return false
    },
    showUploadList: false,
    multiple: true,
    directory: false,
  }

  return (
    <div className="flex items-center justify-between py-1">
      <Space size="small">
        <Upload {...uploadProps}>
          <Button type="primary" icon={<UploadOutlined />}>
            上传
          </Button>
        </Upload>
        <Tooltip title="新建文件夹">
          <Button size="small" icon={<FolderAddOutlined />} onClick={onNewFolder} />
        </Tooltip>
        <Tooltip title="下载">
          <Button size="small" icon={<DownloadOutlined />} disabled={!hasSingleSelection} onClick={onDownload} />
        </Tooltip>
        <Tooltip title="删除">
          <Button size="small" icon={<DeleteOutlined />} disabled={!hasSelection} danger onClick={onDelete} />
        </Tooltip>
        <Tooltip title="刷新">
          <Button size="small" icon={<ReloadOutlined />} onClick={onRefresh} />
        </Tooltip>
      </Space>
    </div>
  )
}
