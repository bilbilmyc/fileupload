import { Routes, Route, Navigate } from 'react-router-dom'
import Login from './pages/Login'
import Files from './pages/Files'

function RequireAuth({ children }: { children: React.ReactNode }) {
  // 当前版本允许未登录访问（登录为预留），后续通过 useAuth() 检查 isAuthenticated
  return <>{children}</>
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route
        path="/"
        element={
          <RequireAuth>
            <Files />
          </RequireAuth>
        }
      />
      <Route path="*" element={<Navigate to="/" />} />
    </Routes>
  )
}
