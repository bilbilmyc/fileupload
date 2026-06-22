import { Breadcrumb } from 'antd'
interface BreadcrumbNavProps {
  items: any[]
}

export default function BreadcrumbNav({ items }: BreadcrumbNavProps) {
  return (
    <Breadcrumb
      items={items}
      className="text-sm"
    />
  )
}
