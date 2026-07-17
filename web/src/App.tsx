import { lazy, Suspense } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import { Layout, Spin } from 'antd'
import Sidebar from './components/Sidebar'
import UploadProgressBar from './components/UploadProgressBar'
import { UploadProvider } from './context/UploadContext'
import ErrorBoundary from './components/ErrorBoundary'

// 所有页面均按路由加载：登录页不会预加载文件管理，管理功能也不阻塞工作台首屏。
const Login = lazy(() => import('./pages/Login'))
const Files = lazy(() => import('./pages/Files'))
const Admin = lazy(() => import('./pages/Admin'))
const Logs = lazy(() => import('./pages/Logs'))
const Settings = lazy(() => import('./pages/Settings'))
const Trash = lazy(() => import('./pages/Trash'))

const { Content } = Layout

function LoadingFallback() {
  return (
    <div className="route-loading" role="status" aria-live="polite">
      <Spin size="large" tip="正在加载工作区…" />
    </div>
  )
}

function RequireAuth({ children }: { children: React.ReactNode }) {
  return <>{children}</>
}

function AppLayout({ children }: { children: React.ReactNode }) {
  return (
    <Layout className="app-shell min-h-screen">
      <Sidebar />
      <Layout>
        <Content className="app-content flex flex-col">
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
      <Suspense fallback={<LoadingFallback />}>
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
                      <Route path="/admin" element={<Admin />} />
                      <Route path="/logs" element={<Logs />} />
                      <Route path="/settings" element={<Settings />} />
                      <Route path="/trash" element={<Trash />} />
                      <Route path="*" element={<Navigate to="/" />} />
                    </Routes>
                  </AppLayout>
                </UploadProvider>
              </RequireAuth>
            }
          />
        </Routes>
      </Suspense>
    </ErrorBoundary>
  )
}
