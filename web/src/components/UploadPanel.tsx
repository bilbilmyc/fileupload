import { Upload, Progress, Button, Switch, Card, Space, Typography, Spin } from 'antd'
const { Text } = Typography
import { UploadOutlined, InboxOutlined, ClearOutlined } from '@ant-design/icons'
import type { UploadTask } from '../hooks/useUpload'

const { Dragger } = Upload

interface UploadPanelProps {
  uploadTasks: UploadTask[]
  dirMode: boolean
  showUpload: boolean
  hasActiveUploads: boolean
  onDirModeChange: (checked: boolean) => void
  onShowUploadChange: (show: boolean) => void
  onCustomRequest: (options: any) => void
  onClearDone: () => void
}

export default function UploadPanel({
  uploadTasks,
  dirMode,
  showUpload,
  hasActiveUploads,
  onDirModeChange,
  onShowUploadChange,
  onCustomRequest,
  onClearDone,
}: UploadPanelProps) {
  return (
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
          <Switch size="small" checked={dirMode} onChange={onDirModeChange} />
          {!showUpload && (
            <Button size="small" type="primary" ghost icon={<UploadOutlined />} onClick={() => onShowUploadChange(true)}>
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
            customRequest={onCustomRequest}
            showUploadList={false}
            className="bg-gray-50 border-dashed"
          >
            <p className="text-3xl text-blue-400 !mb-2"><InboxOutlined /></p>
            <p className="text-sm text-gray-500">点击或拖拽文件{dirMode ? '夹' : ''}到此处</p>
          </Dragger>
          {uploadTasks.length > 0 && (
            <div className="mt-3 space-y-1.5">
              <div className="flex justify-between items-center">
                <span className="text-xs text-gray-400">
                  {uploadTasks.filter(t => t.status === 'done').length}/{uploadTasks.length}
                  {hasActiveUploads && <Spin size="small" className="ml-2" />}
                </span>
                <Button type="link" size="small" onClick={onClearDone} className="text-xs">
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
          <Button size="small" type="text" onClick={() => onShowUploadChange(false)} className="mt-2 text-xs text-gray-400">
            收起
          </Button>
        </>
      ) : (
        <div className="flex items-center justify-center py-2">
          <Button type="dashed" icon={<UploadOutlined />} onClick={() => onShowUploadChange(true)} className="text-gray-400">
            点击展开上传区域
          </Button>
        </div>
      )}
    </Card>
  )
}
