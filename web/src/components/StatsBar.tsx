import { Space } from 'antd'

function formatBytes(bytes: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)} ${units[i]}`
}

interface StatsBarProps {
  dirs: number
  files: number
  totalSize: number
}

export default function StatsBar({ dirs, files, totalSize }: StatsBarProps) {
  return (
    <Space size="large" className="text-xs text-gray-400">
      <span>{dirs} 个目录</span>
      <span>{files} 个文件</span>
      <span>共 {formatBytes(totalSize)}</span>
    </Space>
  )
}
