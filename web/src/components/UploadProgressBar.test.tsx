import { describe, it, expect } from 'vitest'
import { render } from '@testing-library/react'
import UploadProgressBar from './UploadProgressBar'
import { UploadProvider } from '../context/UploadContext'

function renderProgressBar() {
  return render(
    <UploadProvider>
      <UploadProgressBar />
    </UploadProvider>
  )
}

describe('UploadProgressBar', () => {
  it('renders nothing when no tasks', () => {
    const { container } = renderProgressBar()
    expect(container.innerHTML).toBe('')
  })
})
