import React from 'react'
import { Button, Result } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'

interface Props {
  children: React.ReactNode
  title?: string
}

interface State {
  hasError: boolean
  error: Error | null
}

export default class ErrorBoundary extends React.Component<Props, State> {
  constructor(props: Props) {
    super(props)
    this.state = { hasError: false, error: null }
  }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, errorInfo: React.ErrorInfo) {
    console.error('[ErrorBoundary]', error, errorInfo)
  }

  handleRetry = () => {
    this.setState({ hasError: false, error: null })
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="p-8">
          <Result
            status="error"
            title={this.props.title || '页面渲染异常'}
            subTitle={this.state.error?.message || '发生了意外错误，请尝试刷新页面'}
            extra={[
              <Button
                key="retry"
                type="primary"
                icon={<ReloadOutlined />}
                onClick={this.handleRetry}
              >
                重试
              </Button>,
              <Button key="reload" onClick={() => window.location.reload()}>
                刷新页面
              </Button>,
            ]}
          />
        </div>
      )
    }

    return this.props.children
  }
}
