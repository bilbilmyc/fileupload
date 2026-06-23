import { Collapse, Progress, Tag, Button } from 'antd'
import { CloseOutlined, CheckCircleOutlined, LoadingOutlined, CloseCircleOutlined } from '@ant-design/icons'
import { useUploadCtx } from '../context/UploadContext'

export default function UploadProgressBar() {
  const { uploadTasks, hasActiveUploads, clearDoneTasks } = useUploadCtx()

  if (uploadTasks.length === 0) return null

  const doneCount = uploadTasks.filter(t => t.status === 'done' || t.status === 'error').length

  const statusIcon = (status: string) => {
    switch (status) {
      case 'done': return <CheckCircleOutlined className="text-green-500" />
      case 'error': return <CloseCircleOutlined className="text-red-500" />
      case 'hashing':
      case 'uploading':
      case 'finalizing': return <LoadingOutlined className="text-blue-500" />
      default: return null
    }
  }

  const statusColor = (status: string) => {
    switch (status) {
      case 'done': return '#52c41a'
      case 'error': return '#ff4d4f'
      default: return '#1677ff'
    }
  }

  return (
    <div className="fixed bottom-0 left-0 right-0 z-50 bg-white dark:bg-gray-800 border-t border-gray-200 dark:border-gray-700 shadow-lg">
      <div className="max-w-full mx-auto px-4 py-2">
        <div className="flex items-center justify-between mb-1">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
              {hasActiveUploads ? '上传中...' : '上传完成'}
            </span>
            <Tag color={hasActiveUploads ? 'processing' : 'success'}>
              {doneCount}/{uploadTasks.length}
            </Tag>
          </div>
          {!hasActiveUploads && (
            <Button type="text" size="small" icon={<CloseOutlined />} onClick={clearDoneTasks} />
          )}
        </div>

        {uploadTasks.length <= 3 ? (
          <div className="space-y-1">
            {uploadTasks.map(task => (
              <div key={task.id} className="flex items-center gap-2 text-xs">
                <span className="w-4">{statusIcon(task.status)}</span>
                <span className="truncate flex-1 text-gray-600 dark:text-gray-400">{task.name}</span>
                <Progress
                  percent={task.progress}
                  size="small"
                  strokeColor={statusColor(task.status)}
                  className="!w-24 !mb-0"
                  format={() => ''}
                />
                {task.speed && <span className="text-gray-400 w-16 text-right">{task.speed}</span>}
                {task.status === 'error' && (
                  <span className="text-red-500 truncate max-w-[120px]">{task.error}</span>
                )}
              </div>
            ))}
          </div>
        ) : (
          <Collapse
            ghost
            size="small"
            items={[{
              key: 'tasks',
              label: (
                <span className="text-xs text-gray-500">
                  共 {uploadTasks.length} 个任务，{doneCount} 完成
                </span>
              ),
              children: (
                <div className="space-y-1 max-h-32 overflow-y-auto">
                  {uploadTasks.map(task => (
                    <div key={task.id} className="flex items-center gap-2 text-xs">
                      <span className="w-4">{statusIcon(task.status)}</span>
                      <span className="truncate flex-1 text-gray-600 dark:text-gray-400">{task.name}</span>
                      <Progress
                        percent={task.progress}
                        size="small"
                        strokeColor={statusColor(task.status)}
                        className="!w-24 !mb-0"
                        format={() => ''}
                      />
                      {task.speed && <span className="text-gray-400 w-16 text-right">{task.speed}</span>}
                    </div>
                  ))}
                </div>
              ),
            }]}
          />
        )}
      </div>
    </div>
  )
}
