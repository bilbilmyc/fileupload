import { Input, Button, Space, Tooltip, Select } from 'antd'
import { ReloadOutlined, SearchOutlined } from '@ant-design/icons'

interface TopBarProps {
  search: string
  typeFilter: string
  onSearchChange: (value: string) => void
  onTypeFilterChange: (value: string) => void
  onRefresh: () => void
}

export default function TopBar({
  search, typeFilter, onSearchChange, onTypeFilterChange, onRefresh,
}: TopBarProps) {
  return (
    <header
      className="bg-white dark:bg-gray-800 px-4 flex items-center justify-between border-b border-gray-200 dark:border-gray-700"
      style={{ height: 48, lineHeight: '48px' }}
    >
      <Space size="small" wrap>
        <Input
          size="small"
          prefix={<SearchOutlined className="text-gray-400" />}
          placeholder="搜索文件..."
          value={search}
          onChange={(e) => onSearchChange(e.target.value)}
          className="!w-[160px] sm:!w-[240px]"
          allowClear
        />
        <Select
          size="small"
          value={typeFilter}
          onChange={onTypeFilterChange}
          className="!w-[80px]"
          options={[
            { value: '', label: '全部' },
            { value: 'dir', label: '目录' },
            { value: 'file', label: '文件' },
          ]}
        />
      </Space>
      <Tooltip title="刷新">
        <Button type="text" size="small" icon={<ReloadOutlined />} onClick={onRefresh} />
      </Tooltip>
    </header>
  )
}
