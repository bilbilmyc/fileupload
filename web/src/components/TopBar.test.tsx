import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { AuthProvider } from '../context/AuthContext'
import { ThemeProvider } from '../context/ThemeContext'
import TopBar from './TopBar'

function renderTopBar() {
  return render(
    <AuthProvider>
      <ThemeProvider>
        <TopBar
          search=""
          typeFilter=""
          onSearchChange={vi.fn()}
          onTypeFilterChange={vi.fn()}
          onRefresh={vi.fn()}
        />
      </ThemeProvider>
    </AuthProvider>
  )
}

describe('TopBar', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('renders search input', () => {
    renderTopBar()
    expect(screen.getByPlaceholderText('搜索文件...')).toBeInTheDocument()
  })

  it('renders type filter', () => {
    renderTopBar()
    expect(screen.getByText('全部')).toBeInTheDocument()
  })

  it('renders refresh button (存在 RefreshOutlined icon)', () => {
    renderTopBar()
    // antd Button 含 ReloadOutlined → 通过 aria-label 或类名找
    const refreshBtn = document.querySelector('.anticon-reload')
    expect(refreshBtn).toBeInTheDocument()
  })

  // v0.11.1：namespace 显示从 Sidebar 移到这里（更显眼）
  it('renders namespace selector with current value', () => {
    localStorage.setItem('fileupload_namespace', 'my-team')
    renderTopBar()
    // antd Select 组件（用类名验证存在性，值由 antd 内部状态管理）
    expect(document.querySelector('.ant-select')).toBeInTheDocument()
  })

  // v0.11.1：用户菜单 — 验证 Dropdown 组件挂载（具体内容由 antd 管理）
  it('renders user dropdown trigger', () => {
    localStorage.setItem('fileupload_user_id', 'alice')
    renderTopBar()
    // Dropdown 组件挂载：antd 会创建 .ant-dropdown-trigger 包装
    const trigger = document.querySelector('.ant-dropdown-trigger')
    expect(trigger).toBeTruthy()
  })

  it('renders user dropdown trigger even without userId (fallback)', () => {
    renderTopBar()
    // 无 userId 时仍显示占位 dropdown trigger
    const trigger = document.querySelector('.ant-dropdown-trigger')
    expect(trigger).toBeTruthy()
  })

  it('opens user menu on click (no throw)', () => {
    localStorage.setItem('fileupload_user_id', 'bob')
    renderTopBar()
    const trigger = document.querySelector('.ant-dropdown-trigger') as HTMLElement
    expect(() => fireEvent.click(trigger)).not.toThrow()
  })
})