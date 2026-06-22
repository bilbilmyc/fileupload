import { Input, Button, Space, Tooltip, Typography } from 'antd'
import {
  ReloadOutlined,
  SettingOutlined,
  SearchOutlined,
  MoonOutlined,
  SunOutlined,
} from '@ant-design/icons'
import { useAuth } from '../context/AuthContext'
import { useTheme } from '../context/ThemeContext'

const { Title, Text } = Typography

interface TopBarProps {
  search: string
  onSearchChange: (value: string) => void
  onRefresh: () => void
  onOpenSettings: () => void
}

export default function TopBar({
  search,
  onSearchChange,
  onRefresh,
  onOpenSettings,
}: TopBarProps) {
  const { namespace, setNamespace, isAuthenticated, logout } = useAuth()
  const { mode, toggle } = useTheme()

  return (
    <header
      className="bg-white dark:bg-gray-800 px-6 flex items-center justify-between shadow-sm border-b border-gray-200 dark:border-gray-700"
      style={{ height: 56, lineHeight: '56px' }}
    >
      <div className="flex items-center gap-3">
        <Title level={5} className="!mb-0 text-gray-800 dark:text-gray-100">
          📦 fileupload
        </Title>
        <Text className="text-xs text-gray-400 bg-gray-100 dark:bg-gray-700 dark:text-gray-400 px-2 py-0.5 rounded">
          dev
        </Text>
      </div>
      <Space size="small" wrap>
        <Input
          size="small"
          prefix={<SearchOutlined className="text-gray-400" />}
          placeholder="搜索..."
          value={search}
          onChange={(e) => onSearchChange(e.target.value)}
          className="!w-[120px] sm:!w-[200px]"
          allowClear
        />
        <Input
          size="small"
          placeholder="NS"
          value={namespace}
          onChange={(e) => setNamespace(e.target.value)}
          className="!w-[80px] sm:!w-[120px]"
        />
        <Tooltip title={mode === 'dark' ? '切换亮色模式' : '切换暗色模式'}>
          <Button
            type="text"
            icon={mode === 'dark' ? <SunOutlined /> : <MoonOutlined />}
            onClick={toggle}
          />
        </Tooltip>
        <Tooltip title="刷新">
          <Button type="text" icon={<ReloadOutlined />} onClick={onRefresh} />
        </Tooltip>
        <Tooltip title="上传设置">
          <Button type="text" icon={<SettingOutlined />} onClick={onOpenSettings} />
        </Tooltip>
        {isAuthenticated && (
          <Button size="small" onClick={logout}>退出</Button>
        )}
      </Space>
    </header>
  )
}
