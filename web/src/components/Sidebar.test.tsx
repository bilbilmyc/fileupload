import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { AuthProvider } from '../context/AuthContext'
import { ThemeProvider } from '../context/ThemeContext'
import Sidebar from './Sidebar'

function renderSidebar() {
  return render(
    <MemoryRouter>
      <AuthProvider>
        <ThemeProvider>
          <Sidebar />
        </ThemeProvider>
      </AuthProvider>
    </MemoryRouter>
  )
}

describe('Sidebar', () => {
  it('renders logo', () => {
    renderSidebar()
    expect(screen.getByText('fileupload')).toBeInTheDocument()
  })

  it('renders navigation items', () => {
    renderSidebar()
    expect(screen.getByText('文件管理')).toBeInTheDocument()
    expect(screen.getByText('控制台')).toBeInTheDocument()
    expect(screen.getByText('操作日志')).toBeInTheDocument()
    expect(screen.getByText('设置')).toBeInTheDocument()
  })

  it('renders namespace input', () => {
    renderSidebar()
    expect(screen.getByPlaceholderText('命名空间')).toBeInTheDocument()
  })
})
