import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Card, Input, Button, Typography, Space, message, Spin } from 'antd'
import { UserOutlined, LockOutlined, LoginOutlined } from '@ant-design/icons'
import { useAuth } from '../context/AuthContext'

const { Title, Text } = Typography

export default function Login() {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [loading, setLoading] = useState(false)
  const { loginWithCredentials } = useAuth()
  const navigate = useNavigate()

  const handleLogin = async () => {
    if (!username.trim() || !password.trim()) {
      message.warning('请输入用户名和密码')
      return
    }
    setLoading(true)
    try {
      await loginWithCredentials(username, password)
      message.success('登录成功')
      navigate('/')
    } catch (e: any) {
      const msg = e.response?.data?.error || e.message || '登录失败'
      message.error(msg)
    } finally {
      setLoading(false)
    }
  }

  const skip = () => {
    navigate('/')
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-100 dark:bg-gray-900">
      <Card className="w-96 shadow-lg dark:bg-gray-800">
        <Space direction="vertical" className="w-full" size="middle">
          <div className="text-center">
            <Title level={3} className="!mb-1">📦 fileupload</Title>
            <Text type="secondary" className="text-xs">文件上传下载管理面板</Text>
          </div>

          <div>
            <Input
              prefix={<UserOutlined className="text-gray-400" />}
              placeholder="用户名（默认 admin）"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              onPressEnter={handleLogin}
              size="large"
            />
          </div>
          <div>
            <Input.Password
              prefix={<LockOutlined className="text-gray-400" />}
              placeholder="密码（默认 admin123）"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              onPressEnter={handleLogin}
              size="large"
            />
          </div>

          <Button
            type="primary"
            block
            size="large"
            icon={loading ? <Spin /> : <LoginOutlined />}
            onClick={handleLogin}
            disabled={loading}
          >
            {loading ? '登录中...' : '登录'}
          </Button>

          <div className="text-center">
            <Button type="link" size="small" onClick={skip}>
              跳过登录（演示模式）
            </Button>
          </div>
        </Space>
      </Card>
    </div>
  )
}
