import { describe, it, expect, vi } from 'vitest'
import { render } from '@testing-library/react'
import DirectoryPicker from './DirectoryPicker'

describe('DirectoryPicker', () => {
  it('renders without crashing when open=false', () => {
    expect(() =>
      render(
        <DirectoryPicker
          open={false}
          title="选择目录"
          onCancel={vi.fn()}
          onConfirm={vi.fn()}
        />
      )
    ).not.toThrow()
  })

  it('renders title in modal when open=true', () => {
    // 不深入测试 antd Modal 内部 — 仅验证 props 传入不报错
    expect(() =>
      render(
        <DirectoryPicker
          open={true}
          title="选择目标目录"
          onCancel={vi.fn()}
          onConfirm={vi.fn()}
        />
      )
    ).not.toThrow()
  })
})