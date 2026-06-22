import { Modal, Form, Select, Input, Button } from 'antd'

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
      title="上传设置"
      open={open}
      onOk={onClose}
      onCancel={onClose}
      width={400}
      footer={<Button type="primary" onClick={onClose}>关闭</Button>}
    >
      <Form layout="vertical">
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
              { value: 'zstd', label: 'zstd' },
            ]}
          />
        </Form.Item>
        <details className="mt-2 text-gray-400 text-xs cursor-pointer">
          <summary className="select-none">高级选项</summary>
          <div className="mt-2 pl-2 border-l-2 border-gray-200">
            <Form.Item label="分片大小（如 10m / 100m）" help="大文件分片上传时的每片大小">
              <Input value={chunkSize} onChange={(e) => onChunkSizeChange(e.target.value)} />
            </Form.Item>
          </div>
        </details>
      </Form>
    </Modal>
  )
}
