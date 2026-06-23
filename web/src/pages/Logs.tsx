import { Card, Empty } from 'antd'

export default function Logs() {
  return (
    <div className="p-6">
      <h2 className="text-lg font-bold mb-4">操作日志</h2>
      <Card>
        <Empty description="操作日志功能开发中" />
      </Card>
    </div>
  )
}
