// web/src/lib/upload-task.ts — v0.11.0 UploadTask 状态机纯函数
//
// 把 UploadContext 中分散的状态转换逻辑集中到纯函数，
// 便于 vitest 单测覆盖。

import type { UploadTask, UploadStatus } from '../context/UploadContext'

/** 创建初始 pending 任务 */
export function createTask(id: string, name: string, _fileSize?: number): UploadTask {
  return {
    id,
    name,
    progress: 0,
    speed: '',
    status: 'hashing',
  }
}

/** 不可变更新：应用 patch 到 task，返回新对象 */
export function patchTask(task: UploadTask, patch: Partial<UploadTask>): UploadTask {
  return { ...task, ...patch }
}

/** 计算进度百分比：loaded/total，clamp 到 [0, 100] */
export function computeProgress(loaded: number, total: number): number {
  if (total <= 0) return 0
  return Math.min(100, Math.max(0, Math.round((loaded / total) * 100)))
}

/** 判断 task 是否为"活跃"状态（未完成） */
export function isActiveStatus(status: UploadStatus): boolean {
  return ['hashing', 'uploading', 'retrying', 'finalizing'].includes(status)
}

/** 判断 task 是否为"已完成"（done 或 error） */
export function isDoneStatus(status: UploadStatus): boolean {
  return status === 'done' || status === 'error'
}

/** 过滤出活跃任务 */
export function filterActiveTasks(tasks: UploadTask[]): UploadTask[] {
  return tasks.filter(t => isActiveStatus(t.status))
}

/** 计算任务进度段（hashing=0-5, uploading=5-90, finalizing=90-100, done=100） */
export function progressStage(stage: 'hashing' | 'uploading' | 'finalizing', innerPct: number): number {
  if (stage === 'hashing') return Math.min(5, innerPct)
  if (stage === 'uploading') return 5 + Math.round(innerPct * 0.85) // 5-90
  if (stage === 'finalizing') return 90 + Math.round(innerPct * 0.10) // 90-100
  return 100
}