import { useState, useEffect, useCallback } from 'react'
import { Modal, Spin, Image, Result, Button } from 'antd'
import {
  FileTextOutlined,
  FileImageOutlined,
  FilePdfOutlined,
  PlayCircleOutlined,
  SoundOutlined,
} from '@ant-design/icons'
import axios from 'axios'
import { previewFileUrl } from '../api/client'

/** 判断文件类型类别 */
type FileCategory = 'image' | 'text' | 'pdf' | 'video' | 'audio' | 'binary'

function getFileCategory(name: string): FileCategory {
  const ext = name.split('.').pop()?.toLowerCase() || ''
  if (['jpg', 'jpeg', 'png', 'gif', 'webp', 'svg', 'bmp', 'ico'].includes(ext)) return 'image'
  if (['txt', 'log', 'md', 'json', 'xml', 'csv', 'yaml', 'yml'].includes(ext)) return 'text'
  if (['js', 'jsx', 'ts', 'tsx', 'go', 'py', 'rb', 'rs', 'java', 'c', 'cpp', 'h', 'hpp', 'cs', 'swift', 'kt', 'sh', 'bash', 'css', 'scss', 'less', 'sql'].includes(ext)) return 'text'
  if (['pdf'].includes(ext)) return 'pdf'
  if (['mp4', 'webm', 'avi', 'mov', 'mkv'].includes(ext)) return 'video'
  if (['mp3', 'wav', 'ogg', 'flac', 'aac', 'm4a'].includes(ext)) return 'audio'
  return 'binary'
}

function formatFileSize(bytes: number): string {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)} ${units[i]}`
}

interface FilePreviewProps {
  fileId: string
  fileName: string
  fileSize: number
  open: boolean
  onClose: () => void
}

export default function FilePreview({ fileId, fileName, fileSize, open, onClose }: FilePreviewProps) {
  const [loading, setLoading] = useState(true)
  const [textContent, setTextContent] = useState('')
  const [error, setError] = useState('')
  const category = getFileCategory(fileName)
  const previewUrl = previewFileUrl(fileId)

  const loadTextContent = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const ns = localStorage.getItem('fileupload_namespace') || 'default'
      const response = await axios.get(previewUrl, {
        headers: { 'X-Namespace': ns },
        responseType: 'text',
        timeout: 30000,
      })
      setTextContent(response.data)
    } catch (e: any) {
      setError(e.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }, [previewUrl])

  useEffect(() => {
    if (!open) return
    if (category === 'text') {
      loadTextContent()
    } else {
      setLoading(false)
    }
  }, [open, category, loadTextContent])

  /** 获取用于 iframe/pdf/视频的完整 URL（含 namespace） */
  const getFullUrl = () => {
    const ns = localStorage.getItem('fileupload_namespace') || 'default'
    return `${window.location.origin}${previewUrl}?namespace=${encodeURIComponent(ns)}`
  }

  const renderPreview = () => {
    switch (category) {
      case 'image':
        return (
          <div className="flex justify-center items-center min-h-[200px]">
            <Image
              src={getFullUrl()}
              alt={fileName}
              style={{ maxWidth: '100%', maxHeight: '70vh' }}
              preview={false}
              placeholder={<Spin />}
            />
          </div>
        )

      case 'text':
        if (loading) return <div className="flex justify-center py-12"><Spin size="large" /></div>
        if (error) return <Result status="error" subTitle={error} />
        return (
          <pre className="bg-gray-50 rounded p-4 overflow-auto max-h-[65vh] text-sm font-mono leading-relaxed whitespace-pre-wrap break-all">
            {textContent.slice(0, 100_000)}
            {textContent.length > 100_000 && (
              <div className="mt-2 text-gray-400 text-xs border-t pt-2">
                文件过大，仅显示前 100,000 字符
              </div>
            )}
          </pre>
        )

      case 'pdf':
        return (
          <iframe
            src={getFullUrl()}
            title={fileName}
            className="w-full border-0 rounded"
            style={{ height: '70vh' }}
          />
        )

      case 'video':
        return (
          <div className="flex justify-center">
            <video controls className="max-w-full rounded" style={{ maxHeight: '70vh' }}>
              <source src={getFullUrl()} />
              您的浏览器不支持视频播放
            </video>
          </div>
        )

      case 'audio':
        return (
          <div className="flex flex-col items-center justify-center py-12 gap-4">
            <SoundOutlined className="text-5xl text-blue-400" />
            <audio controls className="w-full max-w-md">
              <source src={getFullUrl()} />
              您的浏览器不支持音频播放
            </audio>
          </div>
        )

      default:
        return (
          <Result
            icon={<FileTextOutlined />}
            title="暂不支持在线预览"
            subTitle="该文件类型无法在此处预览，请下载后查看"
            extra={
              <Button type="primary" href={getFullUrl()} target="_blank">
                下载文件
              </Button>
            }
          />
        )
    }
  }

  /** 文件类型图标 */
  const typeIcon = () => {
    switch (category) {
      case 'image': return <FileImageOutlined className="text-green-500" />
      case 'pdf': return <FilePdfOutlined className="text-red-500" />
      case 'video': return <PlayCircleOutlined className="text-purple-500" />
      case 'audio': return <SoundOutlined className="text-blue-500" />
      default: return <FileTextOutlined className="text-gray-500" />
    }
  }

  return (
    <Modal
      title={
        <div className="flex items-center gap-2">
          {typeIcon()}
          <span className="text-sm font-medium truncate max-w-[400px]">{fileName}</span>
          <span className="text-xs text-gray-400">({formatFileSize(fileSize)})</span>
        </div>
      }
      open={open}
      onCancel={onClose}
      footer={null}
      width="80%"
      style={{ maxWidth: 1000 }}
      destroyOnClose
    >
      {loading && category !== 'text' ? (
        <div className="flex justify-center py-12"><Spin size="large" /></div>
      ) : (
        renderPreview()
      )}
    </Modal>
  )
}
