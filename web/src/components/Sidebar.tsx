import { useState } from 'react'
import { useNavigate, useLocation } from 'react-router-dom'
import { Layout, Menu, Button, Tooltip } from 'antd'
import {
  FolderOutlined,
  DashboardOutlined,
  HistoryOutlined,
  SettingOutlined,
  SunOutlined,
  MoonOutlined,
} from '@ant-design/icons'
import { useTheme } from '../context/ThemeContext'

const { Sider } = Layout

export default function Sidebar() {
  const navigate = useNavigate()
  const location = useLocation()
  const { mode, toggle } = useTheme()
  const [collapsed, setCollapsed] = useState(false)

  const menuItems = [
    { key: '/', icon: <FolderOutlined />, label: '文件管理' },
    { key: '/admin', icon: <DashboardOutlined />, label: '控制台' },
    { key: '/logs', icon: <HistoryOutlined />, label: '操作日志' },
    { key: '/settings', icon: <SettingOutlined />, label: '设置' },
  ]

  return (
    <Sider
      collapsible
      collapsed={collapsed}
      onCollapse={setCollapsed}
      width={200}
      className="border-r border-gray-200 dark:border-gray-700"
      style={{ background: '#fff' }}
      theme="light"
    >
      {/* Logo */}
      <div className="flex items-center gap-2 px-4 py-4 border-b border-gray-100 dark:border-gray-700">
        <span className="text-xl">📦</span>
        {!collapsed && (
          <span className="font-bold text-gray-800 dark:text-gray-100 whitespace-nowrap">fileupload</span>
        )}
      </div>

      {/* Nav menu */}
      <Menu
        mode="inline"
        selectedKeys={[location.pathname]}
        items={menuItems}
        onClick={({ key }) => navigate(key)}
        className="border-0"
      />

      {/* Bottom area — v0.11.1+：移除 namespace 输入（移到 TopBar 显眼位置） */}
      <div className="absolute bottom-0 left-0 right-0 p-3 border-t border-gray-100 dark:border-gray-700">
        <div className="flex justify-center">
          <Tooltip title={mode === 'dark' ? '亮色模式' : '暗色模式'}>
            <Button
              type="text"
              size="small"
              icon={mode === 'dark' ? <SunOutlined /> : <MoonOutlined />}
              onClick={toggle}
            />
          </Tooltip>
        </div>
      </div>
    </Sider>
  )
}
