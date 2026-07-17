import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Button, Modal, Result, Spin, Tag, Typography } from 'antd'
import {
  DownloadOutlined,
  FileImageOutlined,
  FilePdfOutlined,
  FileTextOutlined,
  PlayCircleOutlined,
  SoundOutlined,
} from '@ant-design/icons'
import { downloadFile, fetchPreviewBlob, saveBlob } from '../api/client'

const { Text } = Typography
const MAX_INLINE_PREVIEW_BYTES = 25 * 1024 * 1024
const MAX_TEXT_PREVIEW_BYTES = 1024 * 1024

type FileCategory = 'image' | 'text' | 'pdf' | 'video' | 'audio' | 'binary'

function getFileCategory(name: string): FileCategory {
  const ext = name.split('.').pop()?.toLowerCase() || ''
  if (['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg', 'bmp', 'ico', 'avif'].includes(ext)) return 'image'
  if (['txt', 'log', 'md', 'json', 'xml', 'csv', 'yaml', 'yml', 'toml', 'ini'].includes(ext)) return 'text'
  if (['js', 'jsx', 'ts', 'tsx', 'go', 'py', 'rb', 'rs', 'java', 'c', 'cpp', 'h', 'hpp', 'cs', 'swift', 'kt', 'sh', 'bash', 'css', 'scss', 'less', 'sql'].includes(ext)) return 'text'
  if (ext === 'pdf') return 'pdf'
  if (['mp4', 'webm', 'mov', 'm4v'].includes(ext)) return 'video'
  if (['mp3', 'wav', 'ogg', 'flac', 'aac', 'm4a'].includes(ext)) return 'audio'
  return 'binary'
}

function formatFileSize(bytes: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / Math.pow(1024, index)).toFixed(index > 0 ? 1 : 0)} ${units[index]}`
}

interface FilePreviewProps {
  fileId: string
  fileName: string
  fileSize: number
  open: boolean
  onClose: () => void
}

/**
 * 统一的认证预览窗口。
 *
 * 预览内容统一通过 axios 获取 Blob，因此静态 Token/JWT 不会因 img、iframe、video
 * 这类原生标签无法附带自定义请求头而失效。为避免占用过多浏览器内存，二进制预览
 * 限制为 25 MB；文本则使用 Range 仅读取前 1 MB。
 */
export default function FilePreview({ fileId, fileName, fileSize, open, onClose }: FilePreviewProps) {
  const category = useMemo(() => getFileCategory(fileName), [fileName])
  const [loading, setLoading] = useState(false)
  const [contentURL, setContentURL] = useState<string | null>(null)
  const [textContent, setTextContent] = useState('')
  const [error, setError] = useState<string | null>(null)

  const canPreview = category !== 'binary' && (category === 'text' || fileSize <= MAX_INLINE_PREVIEW_BYTES)
  const sizeBlocked = category !== 'text' && fileSize > MAX_INLINE_PREVIEW_BYTES

  const releaseContentURL = useCallback(() => {
    setContentURL(current => {
      if (current) URL.revokeObjectURL(current)
      return null
    })
  }, [])

  useEffect(() => () => releaseContentURL(), [releaseContentURL])

  useEffect(() => {
    if (!open) return

    releaseContentURL()
    setTextContent('')
    setError(null)

    if (!canPreview) {
      setLoading(false)
      return
    }

    let cancelled = false
    setLoading(true)
    void fetchPreviewBlob(fileId, category === 'text' ? MAX_TEXT_PREVIEW_BYTES : undefined)
      .then(async blob => {
        if (cancelled) return
        if (category === 'text') {
          const text = await blob.text()
          if (!cancelled) setTextContent(text)
        } else {
          setContentURL(URL.createObjectURL(blob))
        }
      })
      .catch((requestError: unknown) => {
        if (!cancelled) {
          setError(requestError instanceof Error ? requestError.message : '预览请求失败，请稍后重试')
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => { cancelled = true }
  }, [open, fileId, category, canPreview, releaseContentURL])

  const handleDownload = useCallback(async () => {
    try {
      const blob = await downloadFile(fileId)
      saveBlob(blob, fileName)
    } catch (downloadError) {
      setError(downloadError instanceof Error ? `下载失败：${downloadError.message}` : '下载失败，请稍后重试')
    }
  }, [fileId, fileName])

  const typeIcon = () => {
    switch (category) {
      case 'image': return <FileImageOutlined className="preview-dialog__type-icon preview-dialog__type-icon--image" />
      case 'pdf': return <FilePdfOutlined className="preview-dialog__type-icon preview-dialog__type-icon--pdf" />
      case 'video': return <PlayCircleOutlined className="preview-dialog__type-icon preview-dialog__type-icon--video" />
      case 'audio': return <SoundOutlined className="preview-dialog__type-icon preview-dialog__type-icon--audio" />
      default: return <FileTextOutlined className="preview-dialog__type-icon" />
    }
  }

  const renderUnsupported = () => (
    <Result
      status="info"
      icon={<FileTextOutlined />}
      title={sizeBlocked ? '文件超过在线预览大小限制' : '暂不支持此文件类型的在线预览'}
      subTitle={sizeBlocked
        ? `为避免浏览器占用过高，本服务仅在线预览不超过 ${formatFileSize(MAX_INLINE_PREVIEW_BYTES)} 的媒体和 PDF 文件。`
        : '文件仍可安全下载到本地后查看。'}
      extra={<Button type="primary" icon={<DownloadOutlined />} onClick={() => void handleDownload()}>下载文件</Button>}
    />
  )

  const renderPreview = () => {
    if (!canPreview) return renderUnsupported()
    if (loading) return <div className="preview-dialog__loading"><Spin size="large" tip="正在加载预览" /></div>
    if (error) {
      return <Result status="error" title="无法加载预览" subTitle={error} extra={<Button onClick={() => void handleDownload()}>改为下载</Button>} />
    }

    switch (category) {
      case 'text':
        return (
          <>
            {fileSize > MAX_TEXT_PREVIEW_BYTES && <Alert className="preview-dialog__notice" type="info" showIcon message={`仅展示前 ${formatFileSize(MAX_TEXT_PREVIEW_BYTES)}；请下载完整文件以查看全部内容。`} />}
            <pre className="preview-dialog__text">{textContent}</pre>
          </>
        )
      case 'image':
        return <div className="preview-dialog__media"><img src={contentURL || ''} alt={fileName} /></div>
      case 'pdf':
        return <iframe className="preview-dialog__document" src={contentURL || ''} title={`${fileName} 预览`} />
      case 'video':
        return <div className="preview-dialog__media"><video controls preload="metadata" src={contentURL || ''}>您的浏览器不支持视频播放</video></div>
      case 'audio':
        return <div className="preview-dialog__audio"><SoundOutlined /><audio controls preload="metadata" src={contentURL || ''}>您的浏览器不支持音频播放</audio></div>
      default:
        return renderUnsupported()
    }
  }

  return (
    <Modal
      className="preview-dialog"
      title={<div className="preview-dialog__title">{typeIcon()}<span className="preview-dialog__name" title={fileName}>{fileName}</span><Tag>{formatFileSize(fileSize)}</Tag></div>}
      open={open}
      onCancel={onClose}
      footer={<div className="preview-dialog__footer"><Text type="secondary">预览内容不会写入本地缓存</Text><Button icon={<DownloadOutlined />} onClick={() => void handleDownload()}>下载</Button></div>}
      width={Math.min(1040, window.innerWidth - 32)}
      destroyOnClose
    >
      {renderPreview()}
    </Modal>
  )
}
