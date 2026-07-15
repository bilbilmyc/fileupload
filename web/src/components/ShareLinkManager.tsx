import { useCallback, useEffect, useState } from 'react'
import { Button, Drawer, Empty, InputNumber, List, Popconfirm, Select, Space, Tag, Tooltip, Typography, message } from 'antd'
import { CopyOutlined, DeleteOutlined, LinkOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import type { FileItem, ShareEntry } from '../api/client'
import * as api from '../api/client'

const { Text } = Typography

interface ShareLinkManagerProps {
  file: FileItem | null
  open: boolean
  onClose: () => void
}

function formatExpiry(entry: ShareEntry): string {
  if (!entry.expires_at) return '永不过期'
  const date = new Date(entry.expires_at)
  return Number.isNaN(date.valueOf()) ? entry.expires_at : `至 ${date.toLocaleString()}`
}

export default function ShareLinkManager({ file, open, onClose }: ShareLinkManagerProps) {
  const [entries, setEntries] = useState<ShareEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [creating, setCreating] = useState(false)
  const [expiresIn, setExpiresIn] = useState(168)
  const [maxDownloads, setMaxDownloads] = useState<number | null>(0)

  const load = useCallback(async () => {
    if (!file || file.is_dir) return
    setLoading(true)
    try {
      setEntries(await api.listShares(file.file_id))
    } catch (error: any) {
      message.error(`读取分享链接失败：${error.message || '请稍后重试'}`)
    } finally {
      setLoading(false)
    }
  }, [file])

  useEffect(() => {
    if (open) void load()
  }, [open, load])

  const copy = async (token: string) => {
    try {
      await navigator.clipboard.writeText(api.shareUrl(token))
      message.success('分享链接已复制')
    } catch {
      message.error('复制失败，请检查浏览器剪贴板权限')
    }
  }

  const create = async () => {
    if (!file) return
    setCreating(true)
    try {
      const entry = await api.createShare(file.file_id, {
        expires_in: expiresIn,
        max_downloads: maxDownloads || 0,
      })
      setEntries(current => [entry, ...current])
      await copy(entry.token)
    } catch (error: any) {
      message.error(`创建分享失败：${error.message || '请稍后重试'}`)
    } finally {
      setCreating(false)
    }
  }

  const revoke = async (token: string) => {
    try {
      await api.revokeShare(token)
      setEntries(current => current.filter(entry => entry.token !== token))
      message.success('分享链接已撤销')
    } catch (error: any) {
      message.error(`撤销失败：${error.message || '请稍后重试'}`)
    }
  }

  return (
    <Drawer
      title={<Space><LinkOutlined /><span>分享链接</span></Space>}
      placement="right"
      width={460}
      open={open}
      onClose={onClose}
      extra={<Tooltip title="刷新"><Button type="text" icon={<ReloadOutlined />} onClick={() => void load()} /></Tooltip>}
    >
      <section className="share-create-card" aria-label="创建分享链接">
        <Text strong>{file?.name || '文件'}</Text>
        <Text type="secondary" className="share-create-card__hint">链接创建后可随时撤销；访问次数在下载开始时计数。</Text>
        <div className="share-create-card__fields">
          <label>
            <span>有效期</span>
            <Select value={expiresIn} onChange={setExpiresIn} options={[
              { value: 1, label: '1 小时' },
              { value: 24, label: '24 小时' },
              { value: 168, label: '7 天' },
              { value: 720, label: '30 天' },
              { value: 0, label: '永不过期' },
            ]} />
          </label>
          <label>
            <span>最大下载次数</span>
            <InputNumber min={0} max={1000000} value={maxDownloads} onChange={setMaxDownloads} placeholder="0 = 不限" />
          </label>
        </div>
        <Button type="primary" icon={<PlusOutlined />} loading={creating} onClick={() => void create()} block>
          创建并复制链接
        </Button>
      </section>

      <div className="share-list-heading">
        <div>
          <Text strong>已创建的链接</Text>
          <Text type="secondary"> {entries.length ? `(${entries.length})` : ''}</Text>
        </div>
      </div>
      <List
        loading={loading}
        locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="尚未创建分享链接" /> }}
        dataSource={entries}
        renderItem={(entry) => (
          <List.Item className="share-list-item" actions={[
            <Tooltip title="复制链接" key="copy"><Button type="text" icon={<CopyOutlined />} onClick={() => void copy(entry.token)} /></Tooltip>,
            <Popconfirm key="revoke" title="撤销后此链接将立即失效，确定继续？" okText="撤销" cancelText="取消" onConfirm={() => void revoke(entry.token)}>
              <Tooltip title="撤销链接"><Button type="text" danger icon={<DeleteOutlined />} /></Tooltip>
            </Popconfirm>,
          ]}>
            <List.Item.Meta
              title={<Space size={6}><code>{entry.token.slice(0, 16)}…</code>{entry.password_protected && <Tag color="orange">密码保护</Tag>}</Space>}
              description={<Space size={[6, 5]} wrap><span>{formatExpiry(entry)}</span><span>下载 {entry.cur_downloads}{entry.max_downloads ? ` / ${entry.max_downloads}` : ' 次'}</span></Space>}
            />
          </List.Item>
        )}
      />
    </Drawer>
  )
}