import type { Paginated } from './types'

/** Pagination query — api-contract §21 (`?page=1&limit=20`). */
export interface PageQuery {
  page?: number
  limit?: number
}

export function pageQuery({ page, limit }: PageQuery): Record<string, string> {
  const query: Record<string, string> = {}
  if (page !== undefined) query.page = String(page)
  if (limit !== undefined) query.limit = String(limit)
  return query
}

/**
 * Tolerates the two shapes the Engine uses for collections: a pagination
 * envelope (§21) or a bare `{ items }` object. Screens should not have to care.
 */
export function normalizePage<T>(value: unknown): Paginated<T> {
  if (Array.isArray(value)) {
    return { items: value as T[], page: 1, limit: value.length, total: value.length }
  }
  const record = (value ?? {}) as Partial<Paginated<T>>
  const items = Array.isArray(record.items) ? record.items : []
  return {
    items,
    page: typeof record.page === 'number' ? record.page : 1,
    limit: typeof record.limit === 'number' ? record.limit : items.length,
    total: typeof record.total === 'number' ? record.total : items.length,
    truncated: record.truncated,
  }
}

export function hasNextPage<T>(page: Paginated<T>): boolean {
  if (page.limit <= 0) return false
  return page.page * page.limit < page.total
}

export function nextPage<T>(page: Paginated<T>): number | null {
  return hasNextPage(page) ? page.page + 1 : null
}

export function previousPage<T>(page: Paginated<T>): number | null {
  return page.page > 1 ? page.page - 1 : null
}
