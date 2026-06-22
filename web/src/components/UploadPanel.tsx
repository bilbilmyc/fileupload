import { Upload, Progress, Button, Switch, Typography, Spin } from 'antd'
import { UploadOutlined, InboxOutlined, ClearOutlined, CheckCircleOutlined, CloseCircleOutlined } from '@ant-design/icons'
import type { UploadTask } from '../hooks/useUpload'

const { Text } = Typography
const { Dragger } = Upload

interface UploadPanelProps {
  uploadTasks: UploadTask[]
  dirMode: boolean
  hasActiveUploads: boolean
  onDirModeChange: (checked: boolean) => void
  onCustomRequest: (options: any) => void
  onClearDone: () => void
}

export default function UploadPanel({
  uploadTasks,
  dirMode,
  hasActiveUploads,
  onDirModeChange,
  onCustomRequest,
  onClearDone,
}: UploadPanelProps) {
  const doneCount = uploadTasks.filter(t => t.status === 'done' || t.status === 'error').length
  const allDone = doneCount === uploadTasks.length && uploadTasks.length > 0

  const renderTaskIcon = (status: string) => {
    switch (status) {
      case 'done': return <CheckCircleOutlined className="text-green-500 text-xs" />
      case 'error': return <CloseCircleOutlined className="text-red-500 text-xs" />
      default: return <Spin size="small" className="text-blue-500" />
    }
  }

  return (
    <div className="bg-white rounded-lg shadow-sm p-4">
      {/* Upload Bar */}
      <div className="flex items-center gap-3 mb-3">
        <div className="flex items-center gap-1.5 text-sm font-medium text-gray-700 min-w-[60px]">
          <UploadOutlined className="text-blue-500" />
          <span>上传</span>
        </div>
        <div className="flex items-center gap-1.5">
          <Text className="text-xs text-gray-400">目录模式</Text>
          <Switch size="small" checked={dirMode} onChange={onDirModeChange} />
        </div>
        <div className="flex-1">
          <Dragger
            multiple={!dirMode}
            directory={dirMode}
            customRequest={onCustomRequest}
            showUploadList={false}
            className="!bg-gray-50 !border-dashed !border-gray-300 hover:!border-blue-400 !rounded-md"
            style={{ padding: '6px 16px' }}
          >
            <div className="flex items-center justify-center gap-2 text-xs text-gray-400">
              <InboxOutlined className="text-base" />
              <span>点击或拖拽文件{dirMode ? '夹' : ''}到此处</span>
            </div>
          </Dragger>
        </div>
        {allDone && (
          <Button size="small" type="text" onClick={onClearDone} className="text-xs text-gray-400">
            <ClearOutlined /> 清除
          </Button>
        )}
      </div>

      {/* Upload Task List */}
      {uploadTasks.length > 0 && (
        <div className="space-y-1.5 border-t border-gray-100 pt-3">
          <div className="flex justify-between items-center mb-1">
            <Text className="text-xs text-gray-400">
              {doneCount}/{uploadTasks.length}
              {hasActiveUploads && <Spin size="small" className="ml-1" />}
            </Text>
          </div>
          {uploadTasks.map((t) => (
            <div key={t.id} className="flex items-center gap-2 py-0.5">
              {renderTaskIcon(t.status)}
              <div className="flex-1 min-w-0">
                <div className="flex justify-between items-center text-xs">
                  <span className="truncate text-gray-600">{t.name}</span>
                  <span className="text-gray-400 shrink-0 ml-2">
                    {t.status === 'error' ? t.error || '失败'
                      : t.status === 'done' ? ''
                      : t.speed || ''}
                  </span>
                </div>
                <Progress
                  percent={t.progress}
                  size="small"
                  status={t.status === 'error' ? 'exception' : undefined}
                  showInfo={true}
                  strokeColor={t.status === 'done' ? '#52c41a' : undefined}
                  format={(pct) => t.status === 'error' ? '' : t.status === 'done' ? '' : `${pct}%`}
                  style={{ margin: 0 }}
                />
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
