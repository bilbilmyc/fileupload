import { useCallback, useEffect, useState } from 'react'
import { Button, Empty, Table, Tag, Tooltip, Typography, message } from 'antd'
import { HistoryOutlined, ReloadOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import type { ColumnsType } from 'antd/es/table'
import type { AuditLogEntry } from '../api/client'
import * as api from '../api/client'

const { Text } = Typography
const PAGE_SIZE = 25

const actionMeta: Record<string, { label: string; color: string }> = {
  download: { label: '下载', color: 'blue' },
  preview: { label: '预览', color: 'cyan' },
  share_create: { label: '创建分享', color: 'green' },
  share_revoke: { label: '撤销分享', color: 'orange' },
  share_download: { label: '分享下载', color: 'purple' },
}

function formatTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.valueOf()) ? value || '-' : date.toLocaleString()
}

export default function Logs() {
  const [entries, setEntries] = useState<AuditLogEntry[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const result = await api.listAuditLogs(page, PAGE_SIZE)
      setEntries(result.entries || [])
      setTotal(result.total || 0)
    } catch (error: any) {
      message.error(`加载操作日志失败：${error.message || '请确认当前账号具有管理权限'}`)
    } finally {
      setLoading(false)
    }
  }, [page])

  useEffect(() => { void load() }, [load])

  const columns: ColumnsType<AuditLogEntry> = [
    {
      title: '时间', dataIndex: 'created_at', width: 184,
      render: (value) => <Text type="secondary">{formatTime(value)}</Text>,
    },
    {
      title: '操作', dataIndex: 'action', width: 126,
      render: (action: string) => {
        const meta = actionMeta[action] || { label: action || '未知操作', color: 'default' }
        return <Tag color={meta.color}>{meta.label}</Tag>
      },
    },
    { title: '操作者', dataIndex: 'user_id', width: 148, render: (value) => value || '系统' },
    { title: '空间', dataIndex: 'namespace', width: 132, ellipsis: true },
    {
      title: '对象', key: 'target', width: 180,
      render: (_, item) => <Text ellipsis title={item.target_id || item.target_type}>{item.target_id || item.target_type || '-'}</Text>,
    },
    {
      title: '详情', dataIndex: 'detail', ellipsis: true,
      render: (value: string) => <Tooltip title={value}><Text type="secondary">{value || '-'}</Text></Tooltip>,
    },
  ]

  return (
    <main className="workspace-page logs-page">
      <section className="workspace-hero">
        <div>
          <span className="workspace-eyebrow">AUDIT TRAIL</span>
          <h1>操作日志</h1>
          <p>追踪关键下载、预览与文件分享操作，帮助定位异常访问并满足审计要求。</p>
        </div>
        <div className="workspace-hero__meta">
          <span>已记录事件</span>
          <strong>{total.toLocaleString()} 条</strong>
        </div>
      </section>

      <section className="surface-card file-list-card">
        <div className="file-list-card__header">
          <div>
            <div className="file-list-card__title"><SafetyCertificateOutlined /> 安全审计记录</div>
            <div className="file-list-card__sub">系统记录成功发起的关键文件访问和分享管理操作。</div>
          </div>
          <Button icon={<ReloadOutlined />} onClick={() => void load()} loading={loading}>刷新</Button>
        </div>
        <Table
          aria-label="操作日志表"
          rowKey={(entry) => `${entry.id}-${entry.created_at}`}
          columns={columns}
          dataSource={entries}
          loading={loading}
          locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂时没有可显示的操作日志" /> }}
          pagination={{
            current: page,
            pageSize: PAGE_SIZE,
            total,
            showSizeChanger: false,
            showTotal: count => `共 ${count} 条`,
            onChange: setPage,
          }}
          scroll={{ x: 900 }}
        />
      </section>

      {!loading && entries.length === 0 && total === 0 && (
        <div className="logs-page__tip"><HistoryOutlined /> 执行一次文件下载、在线预览或分享操作后，审计记录会显示在这里。</div>
      )}
    </main>
  )
}
