import { useState, useEffect, useRef } from 'react'
import { Modal, Input, Tag, Space, Typography, message } from 'antd'
import { PlusOutlined } from '@ant-design/icons'

const { Text } = Typography

interface BatchTagEditorProps {
  open: boolean
  fileCount: number
  onCancel: () => void
  onConfirm: (tags: string[]) => void
}

const SUGGESTED_TAGS = [
  '重要', '归档', '待处理', '已完成',
  'work', 'personal', 'archive', 'backup',
]

const TAG_COLORS = ['blue', 'green', 'orange', 'purple', 'cyan', 'magenta']

export default function BatchTagEditor({
  open,
  fileCount,
  onCancel,
  onConfirm,
}: BatchTagEditorProps) {
  const [inputValue, setInputValue] = useState('')
  const [tags, setTags] = useState<string[]>([])
  const inputRef = useRef<any>(null)

  useEffect(() => {
    if (open) {
      setInputValue('')
      setTags([])
      // Auto-focus input
      setTimeout(() => inputRef.current?.focus(), 100)
    }
  }, [open])

  const addTag = (tag: string) => {
    const t = tag.trim()
    if (!t || tags.includes(t)) return
    setTags([...tags, t])
  }

  const removeTag = (tag: string) => {
    setTags(tags.filter(t => t !== tag))
  }

  const handleInputConfirm = () => {
    if (inputValue) {
      const parts = inputValue.split(/[,，]/).map(s => s.trim()).filter(Boolean)
      const newTags = [...tags]
      for (const p of parts) {
        if (!newTags.includes(p)) newTags.push(p)
      }
      setTags(newTags)
      setInputValue('')
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      handleInputConfirm()
    }
  }

  return (
    <Modal
      title="批量标记"
      open={open}
      onOk={() => {
        if (tags.length === 0) {
          message.warning('请添加至少一个标签')
          return
        }
        onConfirm(tags)
      }}
      onCancel={onCancel}
      okText={`标记 ${tags.length > 0 ? `(${tags.length})` : ''}`}
      cancelText="取消"
      width={480}
    >
      <div className="space-y-3">
        <Text className="text-sm">
          为选中的 <strong>{fileCount}</strong> 个文件添加标签：
        </Text>

        <Input
          ref={inputRef}
          placeholder="输入标签后按 Enter，支持英文逗号分隔"
          value={inputValue}
          onChange={(e) => setInputValue(e.target.value)}
          onKeyDown={handleKeyDown}
          onBlur={handleInputConfirm}
          size="middle"
        />

        {tags.length > 0 && (
          <div className="p-3 bg-gray-50 rounded-md">
            <Text className="text-xs text-gray-500 mb-1.5 block">已添加：</Text>
            <Space wrap size={[4, 4]}>
              {tags.map((tag, i) => (
                <Tag
                  key={tag}
                  closable
                  onClose={() => removeTag(tag)}
                  color={TAG_COLORS[i % TAG_COLORS.length]}
                  className="rounded-md !px-2 !py-0.5"
                >
                  {tag}
                </Tag>
              ))}
            </Space>
          </div>
        )}

        <div>
          <Text className="text-xs text-gray-400 mb-1.5 block">快速添加：</Text>
          <Space wrap size={[4, 4]}>
            {SUGGESTED_TAGS.filter(t => !tags.includes(t)).map(tag => (
              <Tag
                key={tag}
                className="cursor-pointer hover:border-blue-400 hover:text-blue-500 transition-colors rounded-md !px-2 !py-0.5"
                onClick={() => addTag(tag)}
                style={{ borderStyle: 'dashed' }}
              >
                <PlusOutlined className="text-xs" /> {tag}
              </Tag>
            ))}
          </Space>
        </div>
      </div>
    </Modal>
  )
}
