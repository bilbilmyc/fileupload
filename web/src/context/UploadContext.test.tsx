import { describe, it, expect } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import { UploadProvider, useUploadCtx } from './UploadContext'

describe('UploadContext', () => {
  it('provides initial state', () => {
    const { result } = renderHook(() => useUploadCtx(), {
      wrapper: ({ children }) => <UploadProvider>{children}</UploadProvider>,
    })
    expect(result.current.uploadTasks).toEqual([])
    expect(result.current.hasActiveUploads).toBe(false)
  })

  it('clearDoneTasks removes done and error tasks', () => {
    const { result } = renderHook(() => useUploadCtx(), {
      wrapper: ({ children }) => <UploadProvider>{children}</UploadProvider>,
    })

    act(() => {
      result.current.setUploadTasks([
        { id: '1', name: 'done.txt', progress: 100, speed: '', status: 'done' },
        { id: '2', name: 'error.txt', progress: 50, speed: '', status: 'error', error: 'fail' },
        { id: '3', name: 'active.txt', progress: 30, speed: '', status: 'uploading' },
      ])
    })

    expect(result.current.uploadTasks.length).toBe(3)

    act(() => {
      result.current.clearDoneTasks()
    })

    expect(result.current.uploadTasks.length).toBe(1)
    expect(result.current.uploadTasks[0].id).toBe('3')
  })

  it('hasActiveUploads returns true when tasks are in progress', () => {
    const { result } = renderHook(() => useUploadCtx(), {
      wrapper: ({ children }) => <UploadProvider>{children}</UploadProvider>,
    })

    expect(result.current.hasActiveUploads).toBe(false)

    act(() => {
      result.current.setUploadTasks([
        { id: '1', name: 'test.txt', progress: 50, speed: '', status: 'uploading' },
      ])
    })

    expect(result.current.hasActiveUploads).toBe(true)
  })
})
