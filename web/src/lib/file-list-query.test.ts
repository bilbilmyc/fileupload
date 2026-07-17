import { describe, expect, it } from 'vitest'
import { createFileListSearchParams, parseFileListQuery, toFileSortBy, toFileTypeFilter } from './file-list-query'

describe('file list query state', () => {
  it('uses safe defaults for missing or invalid URL values', () => {
    const query = parseFileListQuery(new URLSearchParams('dir=%20&q=%20&type=archive&page=-3&sort=owner&order=up'))

    expect(query).toEqual({
      directory: '/',
      search: '',
      typeFilter: '',
      page: 1,
      sortBy: 'name',
      sortOrder: 'asc',
    })
  })

  it('round-trips a complete, shareable file view', () => {
    const params = createFileListSearchParams({
      directory: 'dir-2026',
      search: '年度 报告',
      typeFilter: 'file',
      page: 3,
      sortBy: 'size',
      sortOrder: 'desc',
    })

    expect(params.toString()).toBe('dir=dir-2026&q=%E5%B9%B4%E5%BA%A6+%E6%8A%A5%E5%91%8A&type=file&page=3&sort=size&order=desc')
    expect(parseFileListQuery(params)).toEqual({
      directory: 'dir-2026',
      search: '年度 报告',
      typeFilter: 'file',
      page: 3,
      sortBy: 'size',
      sortOrder: 'desc',
    })
  })

  it('keeps URLs compact when all view options are at defaults', () => {
    const params = createFileListSearchParams({
      directory: '/',
      search: '',
      typeFilter: '',
      page: 1,
      sortBy: 'name',
      sortOrder: 'asc',
    })

    expect(params.toString()).toBe('')
  })

  it('only accepts supported type and sort values from UI callbacks', () => {
    expect(toFileTypeFilter('dir')).toBe('dir')
    expect(toFileTypeFilter('anything-else')).toBe('')
    expect(toFileSortBy('created_at')).toBe('created_at')
    expect(toFileSortBy('owner')).toBe('name')
  })
})
