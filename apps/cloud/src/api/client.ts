import { clearCsrfToken, getCsrfToken } from '../auth/session'
import { ApiError, parseErrorEnvelope } from './errors'

/**
 * HTTP client for the Axiom Engine API.
 *
 * - Base URL is configurable via `VITE_AXIOM_API_URL` (no trailing slash);
 *   default `http://localhost:8080`. The versioned prefix `/api/v1` is appended
 *   here (api-contract §1) so callers pass resource paths only.
 * - Every request carries `X-Request-ID` (generated client-side, matching
 *   `[A-Za-z0-9._-]{1,64}` per api-contract §18) and, when signed in, the
 *   session cookie (`credentials: 'include'`). Mutating requests also send the
 *   in-memory CSRF token in `X-CSRF-Token` (double-submit, api-contract §2).
 * - Errors are parsed into the typed {@link ApiError} from the
 *   `{ error: { code, message, requestId, details } }` envelope (§18).
 * - A 401 clears the interim token and notifies the registered handler so the
 *   auth boundary can redirect to /login with a return path (system §4).
 */

export const DEFAULT_API_BASE_URL = 'http://localhost:8080'
export const API_PREFIX = '/api/v1'

export function resolveBaseUrl(raw: string | undefined): string {
  const value = (raw ?? '').trim()
  if (value === '') return DEFAULT_API_BASE_URL
  return value.replace(/\/+$/, '')
}

export const API_BASE_URL = resolveBaseUrl(import.meta.env.VITE_AXIOM_API_URL)

export function generateRequestId(): string {
  const cryptoObj = typeof crypto !== 'undefined' ? crypto : undefined
  if (cryptoObj && typeof cryptoObj.randomUUID === 'function') {
    return cryptoObj.randomUUID()
  }
  return `req_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 10)}`
}

export type QueryValue = string | number | boolean | undefined | null

export function buildQuery(query?: Record<string, QueryValue>): string {
  if (!query) return ''
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === '') continue
    params.set(key, String(value))
  }
  const serialized = params.toString()
  return serialized ? `?${serialized}` : ''
}

export function apiUrl(path: string, query?: Record<string, QueryValue>): string {
  const normalized = path.startsWith('/') ? path : `/${path}`
  return `${API_BASE_URL}${API_PREFIX}${normalized}${buildQuery(query)}`
}

type UnauthorizedHandler = () => void

let unauthorizedHandler: UnauthorizedHandler | null = null

/** Registered once by AuthProvider. Called after a 401 clears the token. */
export function setUnauthorizedHandler(handler: UnauthorizedHandler | null): void {
  unauthorizedHandler = handler
}

export interface RequestOptions {
  method?: string
  query?: Record<string, QueryValue>
  body?: unknown
  headers?: Record<string, string>
  signal?: AbortSignal
  /** Explicit request id; otherwise generated. */
  requestId?: string
  /** Skip the CSRF header (login/register are unauthenticated). */
  skipCsrf?: boolean
}

export async function apiFetch<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const method = options.method ?? 'GET'
  const requestId = options.requestId ?? generateRequestId()

  const headers: Record<string, string> = {
    Accept: 'application/json',
    'X-Request-ID': requestId,
    ...options.headers,
  }

  const mutating = method !== 'GET' && method !== 'HEAD'
  if (mutating && !options.skipCsrf) {
    const csrf = getCsrfToken()
    if (csrf) headers['X-CSRF-Token'] = csrf
  }

  let body: BodyInit | undefined
  if (options.body !== undefined) {
    if (typeof options.body === 'string') {
      body = options.body
    } else {
      body = JSON.stringify(options.body)
      headers['Content-Type'] = 'application/json'
    }
  }

  let response: Response
  try {
    response = await fetch(apiUrl(path, options.query), {
      method,
      headers,
      body,
      signal: options.signal,
      credentials: 'include',
    })
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === 'AbortError') throw cause
    throw ApiError.network()
  }

  const echoedRequestId = response.headers.get('X-Request-ID') ?? requestId

  if (response.status === 204) {
    return undefined as T
  }

  let parsed: unknown = undefined
  const text = await response.text()
  if (text) {
    try {
      parsed = JSON.parse(text)
    } catch {
      parsed = undefined
    }
  }

  if (!response.ok) {
    const envelope = parseErrorEnvelope(parsed)
    const error = envelope
      ? ApiError.fromEnvelope(response.status, envelope, echoedRequestId)
      : new ApiError({
          code: response.status === 503 ? 'SERVICE_UNAVAILABLE' : 'INTERNAL_ERROR',
          message: `Request failed with status ${response.status}.`,
          status: response.status,
          requestId: echoedRequestId,
        })

    if (error.isUnauthorized) {
      clearCsrfToken()
      unauthorizedHandler?.()
    }
    throw error
  }

  return parsed as T
}

export const api = {
  get: <T>(path: string, options?: Omit<RequestOptions, 'method' | 'body'>) =>
    apiFetch<T>(path, { ...options, method: 'GET' }),
  post: <T>(path: string, body?: unknown, options?: Omit<RequestOptions, 'method' | 'body'>) =>
    apiFetch<T>(path, { ...options, method: 'POST', body }),
  patch: <T>(path: string, body?: unknown, options?: Omit<RequestOptions, 'method' | 'body'>) =>
    apiFetch<T>(path, { ...options, method: 'PATCH', body }),
  put: <T>(path: string, body?: unknown, options?: Omit<RequestOptions, 'method' | 'body'>) =>
    apiFetch<T>(path, { ...options, method: 'PUT', body }),
  delete: <T>(path: string, options?: Omit<RequestOptions, 'method' | 'body'>) =>
    apiFetch<T>(path, { ...options, method: 'DELETE' }),
}
