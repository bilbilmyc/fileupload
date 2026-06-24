import { lazy, Suspense } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { Layout, Spin } from 'antd'
import Login from './pages/Login'
import Files from './pages/Files'
import Sidebar from './components/Sidebar'
import UploadProgressBar from './components/UploadProgressBar'
import { UploadProvider } from './context/UploadContext'
import ErrorBoundary from './components/ErrorBoundary'

// v0.6.0：路由级代码分割 — 非首屏页面按需加载
const Admin = lazy(() => import('./pages/Admin'))
const Logs = lazy(() => import('./pages/Logs'))
const Settings = lazy(() => import('./pages/Settings'))

const { Content } = Layout

function LoadingFallback() {
  return (
    <div className="flex items-center justify-center h-64">
      <Spin size="large" tip="加载中..." />
    </div>
  )
}

function RequireAuth({ children }: { children: React.ReactNode }) {
  return <>{children}</>
}

function AppLayout({ children }: { children: React.ReactNode }) {
  return (
    <Layout className="min-h-screen">
      <Sidebar />
      <Layout>
        <Content className="bg-gray-50 dark:bg-gray-900 flex flex-col">
          {children}
          <UploadProgressBar />
        </Content>
      </Layout>
    </Layout>
  )
}

export default function App() {
  return (
    <ErrorBoundary title="应用崩溃">
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route
          path="/*"
          element={
            <RequireAuth>
              <UploadProvider>
                <AppLayout>
                  <Routes>
                    <Route path="/" element={
                      <ErrorBoundary title="文件管理异常">
                        <Files />
                      </ErrorBoundary>
                    } />
                    <Route path="/admin" element={
                      <Suspense fallback={<LoadingFallback />}><Admin /></Suspense>
                    } />
                    <Route path="/logs" element={
                      <Suspense fallback={<LoadingFallback />}><Logs /></Suspense>
                    } />
                    <Route path="/settings" element={
                      <Suspense fallback={<LoadingFallback />}><Settings /></Suspense>
                    } />
                    <Route path="*" element={<Navigate to="/" />} />
                  </Routes>
                </AppLayout>
              </UploadProvider>
            </RequireAuth>
          }
        />
      </Routes>
    </ErrorBoundary>
  )
}
