import { Routes, Route, Navigate } from 'react-router-dom'
import { Layout } from 'antd'
import Login from './pages/Login'
import Files from './pages/Files'
import Admin from './pages/Admin'
import Logs from './pages/Logs'
import Settings from './pages/Settings'
import Sidebar from './components/Sidebar'
import UploadProgressBar from './components/UploadProgressBar'
import { UploadProvider } from './context/UploadContext'
import ErrorBoundary from './components/ErrorBoundary'

const { Content } = Layout

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
                    <Route path="/admin" element={<Admin />} />
                    <Route path="/logs" element={<Logs />} />
                    <Route path="/settings" element={<Settings />} />
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
