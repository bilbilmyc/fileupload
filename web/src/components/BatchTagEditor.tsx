import { useState, useEffect } from 'react'
import { Modal, Input, Tag, Space, Typography, message } from 'antd'

const { Text } = Typography

interface BatchTagEditorProps {
  open: boolean
  fileCount: number
  onCancel: () => void
  onConfirm: (tags: string[]) => void
}

// Common tag suggestions
const SUGGESTED_TAGS = [
  '重要', '归档', '待处理', '已完成',
  'work', 'personal', 'archive', 'temp', 'backup',
]

export default function BatchTagEditor({
  open,
  fileCount,
  onCancel,
  onConfirm,
}: BatchTagEditorProps) {
  const [inputValue, setInputValue] = useState('')
  const [tags, setTags] = useState<string[]>([])

  useEffect(() => {
    if (open) {
      setInputValue('')
      setTags([])
    }
  }, [open])

  const addTag = (tag: string) => {
    const t = tag.trim()
    if (!t) return
    if (tags.includes(t)) return
    setTags([...tags, t])
  }

  const removeTag = (tag: string) => {
    setTags(tags.filter(t => t !== tag))
  }

  const handleInputConfirm = () => {
    if (inputValue) {
      // Split by comma for batch input
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
      okText="确认标记"
      cancelText="取消"
    >
      <div className="space-y-3">
        <Text className="text-sm">
          为选中的 <strong>{fileCount}</strong> 个文件设置标签：
        </Text>

        <div>
          <Input
            placeholder="输入标签后按 Enter 确认，支持逗号分隔"
            value={inputValue}
            onChange={(e) => setInputValue(e.target.value)}
            onKeyDown={handleKeyDown}
            onBlur={handleInputConfirm}
          />
        </div>

        {tags.length > 0 && (
          <div className="p-2 bg-gray-50 rounded">
            <Text className="text-xs text-gray-500 mb-1 block">已添加的标签：</Text>
            <Space wrap>
              {tags.map(tag => (
                <Tag
                  key={tag}
                  closable
                  onClose={() => removeTag(tag)}
                  color="blue"
                >
                  {tag}
                </Tag>
              ))}
            </Space>
          </div>
        )}

        <div>
          <Text className="text-xs text-gray-400 mb-1 block">快速添加：</Text>
          <Space wrap>
            {SUGGESTED_TAGS.filter(t => !tags.includes(t)).map(tag => (
              <Tag
                key={tag}
                className="cursor-pointer hover:border-blue-400"
                onClick={() => addTag(tag)}
              >
                + {tag}
              </Tag>
            ))}
          </Space>
        </div>
      </div>
    </Modal>
  )
}
