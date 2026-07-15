import { describe, it, expect } from 'vitest'
import {
  createTask,
  patchTask,
  computeProgress,
  isActiveStatus,
  isDoneStatus,
  filterActiveTasks,
  progressStage,
} from './upload-task'
import type { UploadTask } from '../context/UploadContext'

const baseTask: UploadTask = {
  id: 't1', name: 'a.txt', progress: 0, speed: '', status: 'hashing',
}

describe('createTask', () => {
  it('creates initial task with hashing status', () => {
    const t = createTask('t1', 'a.txt', 1024)
    expect(t.id).toBe('t1')
    expect(t.name).toBe('a.txt')
    expect(t.status).toBe('hashing')
    expect(t.progress).toBe(0)
    expect(t.speed).toBe('')
  })
})

describe('patchTask', () => {
  it('returns new object with patch applied', () => {
    const t = patchTask(baseTask, { progress: 50, status: 'uploading' })
    expect(t).not.toBe(baseTask) // 不可变
    expect(t.progress).toBe(50)
    expect(t.status).toBe('uploading')
    expect(t.id).toBe('t1') // 未改字段保留
  })

  it('handles multiple patches', () => {
    const t = patchTask(baseTask, { progress: 30, speed: '1 MB/s', error: 'x' })
    expect(t.progress).toBe(30)
    expect(t.speed).toBe('1 MB/s')
    expect(t.error).toBe('x')
  })
})

describe('computeProgress', () => {
  it('returns 0 for zero total', () => {
    expect(computeProgress(50, 0)).toBe(0)
  })

  it('returns 0 for negative total', () => {
    expect(computeProgress(50, -1)).toBe(0)
  })

  it('returns 0 when loaded=0', () => {
    expect(computeProgress(0, 100)).toBe(0)
  })

  it('rounds percentage', () => {
    expect(computeProgress(50, 100)).toBe(50)
    expect(computeProgress(33, 100)).toBe(33)
    expect(computeProgress(99, 100)).toBe(99)
  })

  it('clamps to 100 even if loaded > total', () => {
    expect(computeProgress(150, 100)).toBe(100)
  })

  it('clamps to 0 for negative loaded', () => {
    expect(computeProgress(-5, 100)).toBe(0)
  })
})

describe('isActiveStatus', () => {
  it('returns true for non-final statuses', () => {
    expect(isActiveStatus('hashing')).toBe(true)
    expect(isActiveStatus('uploading')).toBe(true)
    expect(isActiveStatus('retrying')).toBe(true)
    expect(isActiveStatus('finalizing')).toBe(true)
  })

  it('returns false for final statuses', () => {
    expect(isActiveStatus('done')).toBe(false)
    expect(isActiveStatus('error')).toBe(false)
    expect(isActiveStatus('done')).toBe(false)
  })
})

describe('isDoneStatus', () => {
  it('returns true for done and error', () => {
    expect(isDoneStatus('done')).toBe(true)
    expect(isDoneStatus('error')).toBe(true)
  })

  it('returns false for others', () => {
    expect(isDoneStatus('hashing')).toBe(false)
    expect(isDoneStatus('uploading')).toBe(false)
    expect(isDoneStatus('retrying')).toBe(false)
  })
})

describe('filterActiveTasks', () => {
  it('keeps only active status tasks', () => {
    const tasks: UploadTask[] = [
      { id: '1', name: 'a', progress: 0, speed: '', status: 'uploading' },
      { id: '2', name: 'b', progress: 100, speed: '', status: 'done' },
      { id: '3', name: 'c', progress: 0, speed: '', status: 'hashing' },
      { id: '4', name: 'd', progress: 0, speed: '', status: 'error' },
    ]
    const active = filterActiveTasks(tasks)
    expect(active.map(t => t.id)).toEqual(['1', '3'])
  })

  it('returns empty array for all-done tasks', () => {
    const tasks: UploadTask[] = [
      { id: '1', name: 'a', progress: 100, speed: '', status: 'done' },
      { id: '2', name: 'b', progress: 0, speed: '', status: 'error' },
    ]
    expect(filterActiveTasks(tasks)).toEqual([])
  })
})

describe('progressStage', () => {
  it('hashing 阶段 max 5%', () => {
    expect(progressStage('hashing', 0)).toBe(0)
    expect(progressStage('hashing', 5)).toBe(5)
    expect(progressStage('hashing', 100)).toBe(5) // 截断到 5
  })

  it('uploading 阶段 5% → 90%', () => {
    expect(progressStage('uploading', 0)).toBe(5) // 5 + 0*0.85 = 5
    expect(progressStage('uploading', 50)).toBe(48) // 5 + 50*0.85 = 47.5 → 48
    expect(progressStage('uploading', 100)).toBe(90) // 5 + 100*0.85 = 90
  })

  it('finalizing 阶段 90% → 100%', () => {
    expect(progressStage('finalizing', 0)).toBe(90)
    expect(progressStage('finalizing', 50)).toBe(95)
    expect(progressStage('finalizing', 100)).toBe(100)
  })
})