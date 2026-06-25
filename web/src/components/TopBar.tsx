import { Input, Button, Space, Tooltip, Select, Dropdown } from 'antd'
import { ReloadOutlined, SearchOutlined, UserOutlined, GlobalOutlined, LogoutOutlined } from '@ant-design/icons'
import { useAuth } from '../context/AuthContext'

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
  const { namespace, setNamespace, logout } = useAuth()

  return (
    <header
      className="bg-white dark:bg-gray-800 px-4 flex items-center justify-between border-b border-gray-200 dark:border-gray-700"
      style={{ height: 48, lineHeight: '48px' }}
    >
      {/* 左：搜索 + 类型筛选 */}
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

      {/* 右：namespace 选择 + 刷新 + 用户菜单（v0.11.2+） */}
      <Space size="middle">
        <Tooltip title="命名空间（不同用户/空间数据隔离）">
          <Select
            size="small"
            value={namespace}
            onChange={setNamespace}
            suffixIcon={<GlobalOutlined />}
            className="!w-[100px] sm:!w-[140px]"
            showSearch
            options={[
              { value: 'default', label: '默认 (default)' },
              { value: 'demo', label: '演示 (demo)' },
              { value: namespace, label: `当前 (${namespace})` },
            ]}
          />
        </Tooltip>
        <Tooltip title="刷新">
          <Button type="text" size="small" icon={<ReloadOutlined />} onClick={onRefresh} />
        </Tooltip>
        <Dropdown
          menu={{
            items: [
              {
                key: 'user',
                label: (
                  <div className="px-2 py-1">
                    <div className="text-xs text-gray-400">当前 namespace</div>
                    <div className="font-medium">{namespace}</div>
                  </div>
                ),
                disabled: true,
              },
              { type: 'divider' },
              {
                key: 'logout',
                icon: <LogoutOutlined />,
                label: '登出',
                onClick: logout,
              },
            ],
          }}
          placement="bottomRight"
        >
          <Tooltip title="账号">
            <Button type="text" size="small" icon={<UserOutlined />}>
              <span className="ml-1 hidden sm:inline">{namespace}</span>
            </Button>
          </Tooltip>
        </Dropdown>
      </Space>
    </header>
  )
}
