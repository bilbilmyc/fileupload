import { Routes, Route, Navigate } from 'react-router-dom'
import Login from './pages/Login'
import Files from './pages/Files'
import ErrorBoundary from './components/ErrorBoundary'

function RequireAuth({ children }: { children: React.ReactNode }) {
  // 当前版本允许未登录访问（登录为预留），后续通过 useAuth() 检查 isAuthenticated
  return <>{children}</>
}

export default function App() {
  return (
    <ErrorBoundary title="应用崩溃">
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route
          path="/"
          element={
            <RequireAuth>
              <ErrorBoundary title="文件管理异常">
                <Files />
              </ErrorBoundary>
            </RequireAuth>
          }
        />
        <Route path="*" element={<Navigate to="/" />} />
      </Routes>
    </ErrorBoundary>
  )
}
