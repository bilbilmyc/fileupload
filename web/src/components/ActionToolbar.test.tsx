import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import ActionToolbar from './ActionToolbar'

describe('ActionToolbar', () => {
  const defaultProps = {
    onUpload: vi.fn(),
    onNewFolder: vi.fn(),
    onDownload: vi.fn(),
    onDelete: vi.fn(),
    onRefresh: vi.fn(),
    hasSelection: false,
    hasSingleSelection: false,
  }

  it('renders upload button', () => {
    render(<ActionToolbar {...defaultProps} />)
    expect(screen.getByText('上传')).toBeInTheDocument()
  })

  it('has refresh button that is clickable', () => {
    const onRefresh = vi.fn()
    render(<ActionToolbar {...defaultProps} onRefresh={onRefresh} />)
    const buttons = screen.getAllByRole('button')
    // Find refresh by filtering - the last Tooltip icon button
    fireEvent.click(buttons[buttons.length - 1])
    expect(onRefresh).toHaveBeenCalledTimes(1)
  })

  it('calls onNewFolder when new folder button clicked', () => {
    const onNewFolder = vi.fn()
    render(<ActionToolbar {...defaultProps} onNewFolder={onNewFolder} />)
    const buttons = screen.getAllByRole('button')
    // Click the second button (new folder is after upload)
    fireEvent.click(buttons[1])
    expect(onNewFolder).toHaveBeenCalledTimes(1)
  })
})
