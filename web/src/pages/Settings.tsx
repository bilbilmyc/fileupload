import { Card, Form, Input, Switch } from 'antd'

export default function Settings() {
  return (
    <div className="p-6">
      <h2 className="text-lg font-bold mb-4">设置</h2>
      <Card className="max-w-lg">
        <Form layout="vertical">
          <Form.Item label="默认命名空间">
            <Input placeholder="default" />
          </Form.Item>
          <Form.Item label="上传并发数">
            <Input type="number" placeholder="16" />
          </Form.Item>
          <Form.Item label="分片大小 (MB)">
            <Input type="number" placeholder="10" />
          </Form.Item>
          <Form.Item label="自动主题">
            <Switch />
          </Form.Item>
        </Form>
        <p className="text-gray-400 text-xs mt-4">设置功能开发中，当前配置请在右上角上传设置中调整。</p>
      </Card>
    </div>
  )
}
