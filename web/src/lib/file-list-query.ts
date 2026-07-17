export type FileTypeFilter = '' | 'dir' | 'file'
export type FileSortBy = 'name' | 'size' | 'created_at'
export type FileSortOrder = 'asc' | 'desc'

export interface FileListQuery {
  directory: string
  search: string
  typeFilter: FileTypeFilter
  page: number
  sortBy: FileSortBy
  sortOrder: FileSortOrder
}

const defaultQuery: FileListQuery = {
  directory: '/',
  search: '',
  typeFilter: '',
  page: 1,
  sortBy: 'name',
  sortOrder: 'asc',
}

function readTypeFilter(value: string | null): FileTypeFilter {
  return value === 'dir' || value === 'file' ? value : ''
}

function readSortBy(value: string | null): FileSortBy {
  return value === 'size' || value === 'created_at' ? value : 'name'
}

function readSortOrder(value: string | null): FileSortOrder {
  return value === 'desc' ? 'desc' : 'asc'
}

function readPage(value: string | null): number {
  const page = Number(value)
  return Number.isSafeInteger(page) && page > 0 ? page : 1
}

/**
 * Converts the file-list URL into safe, fully-defined view state.
 * Unknown values intentionally fall back to defaults so copied links remain resilient.
 */
export function parseFileListQuery(params: URLSearchParams): FileListQuery {
  const directory = params.get('dir')?.trim() || defaultQuery.directory

  return {
    directory,
    search: params.get('q')?.trim() || defaultQuery.search,
    typeFilter: readTypeFilter(params.get('type')),
    page: readPage(params.get('page')),
    sortBy: readSortBy(params.get('sort')),
    sortOrder: readSortOrder(params.get('order')),
  }
}

/** Creates a compact, shareable query string by omitting default values. */
export function createFileListSearchParams(query: FileListQuery): URLSearchParams {
  const params = new URLSearchParams()

  if (query.directory !== defaultQuery.directory) params.set('dir', query.directory)
  if (query.search) params.set('q', query.search)
  if (query.typeFilter) params.set('type', query.typeFilter)
  if (query.page !== defaultQuery.page) params.set('page', String(query.page))
  if (query.sortBy !== defaultQuery.sortBy) params.set('sort', query.sortBy)
  if (query.sortOrder !== defaultQuery.sortOrder) params.set('order', query.sortOrder)

  return params
}

export function toFileTypeFilter(value: string): FileTypeFilter {
  return readTypeFilter(value)
}

export function toFileSortBy(value: string): FileSortBy {
  return readSortBy(value)
}
