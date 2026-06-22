import { Input, Button, Space, Tooltip, Typography } from 'antd'
import {
  ReloadOutlined,
  SettingOutlined,
  SearchOutlined,
} from '@ant-design/icons'
import { useAuth } from '../context/AuthContext'

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

  return (
    <header
      className="bg-white px-6 flex items-center justify-between shadow-sm border-b border-gray-200"
      style={{ height: 56, lineHeight: '56px' }}
    >
      <div className="flex items-center gap-3">
        <Title level={5} className="!mb-0 text-gray-800">
          📦 fileupload
        </Title>
        <Text className="text-xs text-gray-400 bg-gray-100 px-2 py-0.5 rounded">
          dev
        </Text>
      </div>
      <Space size="middle">
        <Input
          size="small"
          prefix={<SearchOutlined className="text-gray-400" />}
          placeholder="搜索文件..."
          value={search}
          onChange={(e) => onSearchChange(e.target.value)}
          style={{ width: 200 }}
          allowClear
        />
        <Input
          size="small"
          placeholder="Namespace"
          value={namespace}
          onChange={(e) => setNamespace(e.target.value)}
          style={{ width: 120 }}
        />
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
