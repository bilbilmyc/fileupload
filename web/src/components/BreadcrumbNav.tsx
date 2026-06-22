import { Breadcrumb } from 'antd'
import { RightOutlined } from '@ant-design/icons'

interface BreadcrumbNavProps {
  items: any[]
}

export default function BreadcrumbNav({ items }: BreadcrumbNavProps) {
  return (
    <Breadcrumb
      items={items}
      separator={<RightOutlined className="text-xs text-gray-300" />}
      className="text-xs sm:text-sm"
    />
  )
}
