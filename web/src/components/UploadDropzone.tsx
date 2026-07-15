import { Upload } from 'antd'
import { CloudUploadOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import type { UploadProps } from 'antd'

interface UploadDropzoneProps {
  onUpload: (file: File) => void
}

/** A lightweight drop target; task state and retries are rendered by UploadProgressBar. */
export default function UploadDropzone({ onUpload }: UploadDropzoneProps) {
  const props: UploadProps = {
    multiple: true,
    showUploadList: false,
    beforeUpload: (file) => {
      onUpload(file as unknown as File)
      return Upload.LIST_IGNORE
    },
  }

  return (
    <Upload.Dragger {...props} className="upload-dropzone">
      <div className="upload-dropzone__icon"><CloudUploadOutlined /></div>
      <p className="upload-dropzone__title">拖拽文件到这里，或点击选择文件</p>
      <p className="upload-dropzone__hint">大文件分片上传、网络异常自动重试、上传完成后自动校验</p>
      <span className="upload-dropzone__badge"><SafetyCertificateOutlined /> 传输过程受完整性校验保护</span>
    </Upload.Dragger>
  )
}
