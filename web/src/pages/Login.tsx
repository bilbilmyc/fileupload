import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Card, Input, Button, Typography, Space, message } from 'antd'
import { useAuth } from '../context/AuthContext'

const { Title, Text } = Typography

export default function Login() {
  const [token, setToken] = useState('')
  const { login } = useAuth()
  const navigate = useNavigate()

  const handleLogin = () => {
    if (!token.trim()) {
      message.warning('请输入 token')
      return
    }
    login(token)
    message.success('已登录')
    navigate('/')
  }

  const skip = () => {
    navigate('/')
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-100">
      <Card className="w-96 shadow-lg">
        <Space direction="vertical" className="w-full">
          <Title level={3} className="text-center">fileupload 登录</Title>
          <Text type="secondary">
            当前为占位登录页。输入任意 token 或点击「跳过」继续。
          </Text>
          <Input.Password
            placeholder="Access Token"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            onPressEnter={handleLogin}
          />
          <Button type="primary" block onClick={handleLogin}>
            登录
          </Button>
          <Button block onClick={skip}>跳过</Button>
        </Space>
      </Card>
    </div>
  )
}
