/**
 * API resource types.
 *
 * These mirror docs/architecture/api-contract.md. They are intentionally
 * permissive (optional fields, index signatures) because #120 will refine them
 * as endpoints are wired. Unknown fields are never rendered.
 */

export interface ApiErrorEnvelope {
  error: {
    code: string
    message: string
    requestId?: string
    details?: Record<string, unknown>
  }
}

/** Pagination envelope — api-contract §21. */
export interface Paginated<T> {
  items: T[]
  page: number
  limit: number
  total: number
  /** Some collection endpoints (repositories) add this flag — api-contract §5. */
  truncated?: boolean
}

/** Non-paginated collection envelope, e.g. GET /github/connections. */
export interface Collection<T> {
  items: T[]
}

/** api-contract §2. */
export interface User {
  id: string
  name: string
}

/** api-contract §4. */
export interface GithubConnection {
  id: string
  accountLogin: string
  accountType: string
  status: 'active' | 'needs_attention' | 'disconnected'
  scopes?: string
  connectedAt?: string
  updatedAt?: string
}

/** api-contract §5. */
export interface Repository {
  id: string
  connectionId?: string
  externalId?: string
  fullName: string
  cloneUrl?: string
  htmlUrl?: string
  defaultBranch?: string
  private?: boolean
  language?: string
  pushedAt?: string
}

/** api-contract §6. Fields beyond `id`/`name` are not guaranteed in V0.1. */
export interface Application {
  id: string
  name: string
  repositoryId?: string
  createdAt?: string
  updatedAt?: string
}

/** api-contract §9. */
export type ServerStatus = 'ready' | 'degraded' | 'offline' | 'pending' | 'revoked' | 'unknown'

export interface Server {
  id: string
  name: string
  address?: string
  status: ServerStatus
  capabilities?: string[]
  lastSeenAt?: string
  createdAt?: string
}

/** api-contract §11. Status vocabulary is Engine-owned; render unknown raw. */
export interface Deployment {
  id: string
  status: string
  applicationId?: string
  environment?: 'production' | 'staging' | 'preview'
  createdAt?: string
  updatedAt?: string
  url?: string
}
