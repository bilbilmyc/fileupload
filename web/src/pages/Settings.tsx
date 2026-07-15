import { useEffect, useState } from 'react'
import { Button, Card, Input, Segmented, Tag, Typography, message } from 'antd'
import {
  CheckCircleFilled,
  CloudServerOutlined,
  DatabaseOutlined,
  GlobalOutlined,
  MoonOutlined,
  SaveOutlined,
  SafetyCertificateOutlined,
  SunOutlined,
} from '@ant-design/icons'
import { useAuth } from '../context/AuthContext'
import { useTheme } from '../context/ThemeContext'
import * as api from '../api/client'

const { Text } = Typography

function formatBytes(bytes: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / Math.pow(1024, index)).toFixed(index ? 1 : 0)} ${units[index]}`
}

export default function Settings() {
  const { namespace, setNamespace } = useAuth()
  const { mode, setMode } = useTheme()
  const [namespaceDraft, setNamespaceDraft] = useState(namespace)
  const [usage, setUsage] = useState<api.NamespaceUsage | null>(null)
  const [usageLoading, setUsageLoading] = useState(false)

  useEffect(() => setNamespaceDraft(namespace), [namespace])
  useEffect(() => {
    let active = true
    setUsageLoading(true)
    api.getNamespaceUsage()
      .then(value => { if (active) setUsage(value) })
      .catch(() => { if (active) setUsage(null) })
      .finally(() => { if (active) setUsageLoading(false) })
    return () => { active = false }
  }, [namespace])

  const saveNamespace = () => {
    const nextNamespace = namespaceDraft.trim()
    if (!nextNamespace) {
      message.warning('命名空间不能为空')
      return
    }
    setNamespace(nextNamespace)
    message.success(`已切换到空间「${nextNamespace}」`)
  }

  return (
    <main className="workspace-page settings-page">
      <section className="workspace-hero settings-hero">
        <div>
          <span className="workspace-eyebrow">WORKSPACE PREFERENCES</span>
          <h1>设置</h1>
          <p>管理当前文件空间和界面偏好。传输策略由服务端统一协商，确保所有客户端遵循相同的安全边界。</p>
        </div>
        <div className="workspace-hero__meta">
          <span>当前空间</span>
          <strong>{namespace}</strong>
        </div>
      </section>

      <section className="settings-layout">
        <div className="settings-main">
          <Card className="surface-card settings-card" bordered={false}>
            <div className="settings-card__header">
              <span className="settings-card__icon"><DatabaseOutlined /></span>
              <div>
                <h2>文件空间</h2>
                <p>切换后，文件列表、上传和下载都会在新的命名空间中执行。</p>
              </div>
            </div>
            <div className="settings-form-row">
              <div className="settings-field">
                <label htmlFor="namespace">默认命名空间</label>
                <Input
                  id="namespace"
                  size="large"
                  value={namespaceDraft}
                  onChange={event => setNamespaceDraft(event.target.value)}
                  onPressEnter={saveNamespace}
                  prefix={<GlobalOutlined />}
                  aria-describedby="namespace-help"
                />
                <Text id="namespace-help" type="secondary">适用于当前浏览器；服务器会继续校验每次访问的空间归属。</Text>
              </div>
              <Button type="primary" size="large" icon={<SaveOutlined />} onClick={saveNamespace}>
                保存空间
              </Button>
            </div>
          </Card>

          <Card className="surface-card settings-card" bordered={false}>
            <div className="settings-card__header">
              <span className="settings-card__icon settings-card__icon--violet"><SunOutlined /></span>
              <div>
                <h2>显示偏好</h2>
                <p>主题设置会立即应用，并保存在当前浏览器中。</p>
              </div>
            </div>
            <div className="settings-theme-row">
              <div>
                <div className="settings-field__label">界面主题</div>
                <Text type="secondary">选择最适合当前工作环境的显示模式。</Text>
              </div>
              <Segmented
                size="large"
                value={mode}
                onChange={value => setMode(value as 'light' | 'dark')}
                options={[
                  { value: 'light', label: '亮色', icon: <SunOutlined /> },
                  { value: 'dark', label: '暗色', icon: <MoonOutlined /> },
                ]}
              />
            </div>
          </Card>
        </div>

        <aside className="settings-aside" aria-label="服务策略说明">
          <Card className="surface-card settings-card settings-usage-card" bordered={false}>
            <div className="settings-card__header">
              <span className="settings-card__icon settings-card__icon--blue"><CloudServerOutlined /></span>
              <div>
                <h2>空间用量</h2>
                <p>按逻辑文件大小统计，不含目录本身。</p>
              </div>
            </div>
            <div className="settings-usage-grid" aria-busy={usageLoading}>
              <div><span>已用容量</span><strong>{usageLoading ? '加载中…' : formatBytes(usage?.total_size || 0)}</strong></div>
              <div><span>容量配额</span><strong>{usageLoading ? '—' : usage?.quota_bytes ? formatBytes(usage.quota_bytes) : '未限制'}</strong></div>
            </div>
            <Text type="secondary" className="settings-usage-note">{usage?.quota_bytes ? `已启用服务端容量限制；剩余 ${formatBytes(Math.max(0, usage.quota_bytes - (usage.total_size || 0)))}` : '当前实例未设置容量上限；管理员可通过 FILEUPLOAD_NAMESPACE_QUOTA_BYTES 启用配额。'}</Text>
          </Card>
          <Card className="surface-card settings-card settings-policy-card" bordered={false}>
            <div className="settings-card__header">
              <span className="settings-card__icon settings-card__icon--green"><SafetyCertificateOutlined /></span>
              <div>
                <h2>上传策略</h2>
                <p>由服务端统一执行</p>
              </div>
            </div>
            <ul className="settings-policy-list">
              <li><CheckCircleFilled /> 分片校验与完整性验证</li>
              <li><CheckCircleFilled /> 断点续传 offset 冲突保护</li>
              <li><CheckCircleFilled /> 上传会话 namespace 隔离</li>
              <li><CheckCircleFilled /> 下载范围与响应头安全处理</li>
            </ul>
            <div className="settings-policy-note">
              <CloudServerOutlined />
              <span>分片大小、并发上限和压缩方式由服务端按会话协商，避免客户端配置造成上传失败。</span>
            </div>
          </Card>
          <Card className="surface-card settings-tip-card" bordered={false}>
            <Tag color="blue">提示</Tag>
            <h3>切换空间后会发生什么？</h3>
            <p>文件表会自动加载该空间的内容。正在进行的上传会话仍保持在它们创建时的空间中，不会被切换操作影响。</p>
          </Card>
        </aside>
      </section>
    </main>
  )
}
