import { Card, Statistic, Row, Col } from 'antd'
import { FolderOutlined, FileOutlined, HddOutlined } from '@ant-design/icons'

export default function Admin() {
  return (
    <div className="p-6">
      <h2 className="text-lg font-bold mb-4">控制台</h2>
      <Row gutter={16}>
        <Col span={6}>
          <Card>
            <Statistic title="目录数" value={0} prefix={<FolderOutlined />} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="文件数" value={0} prefix={<FileOutlined />} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="存储空间" value="0 B" prefix={<HddOutlined />} />
          </Card>
        </Col>
        <Col span={6}>
          <Card>
            <Statistic title="进行中上传" value={0} suffix="个" />
          </Card>
        </Col>
      </Row>
      <Card className="mt-4">
        <p className="text-gray-400 text-sm">控制台功能开发中，后续将展示完整系统状态。</p>
      </Card>
    </div>
  )
}
