import { Modal, Form, Select, Input, Button, Collapse } from 'antd'
import { SettingOutlined } from '@ant-design/icons'

interface SettingsModalProps {
  open: boolean
  onClose: () => void
  concurrency: number | 'auto'
  compression: 'none' | 'zstd'
  chunkSize: string
  onConcurrencyChange: (v: number | 'auto') => void
  onCompressionChange: (v: 'none' | 'zstd') => void
  onChunkSizeChange: (v: string) => void
}

export default function SettingsModal({
  open,
  onClose,
  concurrency,
  compression,
  chunkSize,
  onConcurrencyChange,
  onCompressionChange,
  onChunkSizeChange,
}: SettingsModalProps) {
  return (
    <Modal
      title={
        <span>
          <SettingOutlined className="mr-2 text-blue-500" />
          上传设置
        </span>
      }
      open={open}
      onCancel={onClose}
      width={420}
      footer={<Button onClick={onClose}>关闭</Button>}
    >
      <Form layout="vertical" className="mt-2">
        <Form.Item label="并发数" help="auto 根据文件大小自动选择">
          <Select
            value={concurrency}
            onChange={(v) => onConcurrencyChange(v)}
            options={[
              { value: 'auto', label: 'auto（推荐）' },
              { value: 1, label: '1' },
              { value: 2, label: '2' },
              { value: 4, label: '4' },
              { value: 8, label: '8' },
              { value: 16, label: '16' },
            ]}
          />
        </Form.Item>
        <Form.Item label="压缩">
          <Select
            value={compression}
            onChange={(v) => onCompressionChange(v)}
            options={[
              { value: 'none', label: '无' },
              { value: 'zstd', label: 'zstd（推荐）' },
            ]}
          />
        </Form.Item>
        <Collapse
          ghost
          items={[{
            key: 'advanced',
            label: <span className="text-xs text-gray-400">高级选项</span>,
            children: (
              <Form.Item label="分片大小" help="大文件分片上传时的每片大小，如 10m / 100m">
                <Input value={chunkSize} onChange={(e) => onChunkSizeChange(e.target.value)} />
              </Form.Item>
            ),
          }]}
        />
      </Form>
    </Modal>
  )
}
