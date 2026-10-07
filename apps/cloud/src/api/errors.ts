import type { ApiErrorEnvelope } from './types'

/**
 * Typed API error (api-contract §18).
 *
 * Clients MUST branch on `code`, never on `message` (the message is not the
 * stable machine contract). `details` is always an object.
 */
export class ApiError extends Error {
  readonly code: string
  readonly status: number
  readonly requestId?: string
  readonly details: Record<string, unknown>

  constructor(params: {
    code: string
    message: string
    status: number
    requestId?: string
    details?: Record<string, unknown>
  }) {
    super(params.message)
    this.name = 'ApiError'
    this.code = params.code
    this.status = params.status
    this.requestId = params.requestId
    this.details = params.details ?? {}
  }

  static fromEnvelope(status: number, envelope: ApiErrorEnvelope, fallbackRequestId?: string): ApiError {
    return new ApiError({
      code: envelope.error.code,
      message: envelope.error.message,
      status,
      requestId: envelope.error.requestId ?? fallbackRequestId,
      details: envelope.error.details,
    })
  }

  /** Transport failure: no HTTP response (offline, DNS, CORS, aborted-not-really). */
  static network(message = 'Could not reach Axiom.'): ApiError {
    return new ApiError({ code: 'NETWORK_ERROR', message, status: 0 })
  }

  get isUnauthorized(): boolean {
    return this.status === 401 || this.code === 'UNAUTHORIZED'
  }

  get isForbidden(): boolean {
    return this.status === 403 || this.code === 'FORBIDDEN' || this.code === 'POLICY_DENIED'
  }

  get isNotFound(): boolean {
    return this.status === 404 || this.code === 'NOT_FOUND'
  }

  get isValidation(): boolean {
    return this.code === 'VALIDATION_FAILED' || this.code === 'INVALID_REQUEST'
  }

  get isRateLimited(): boolean {
    return this.status === 429 || this.code === 'RATE_LIMITED'
  }

  get isUnavailable(): boolean {
    return this.status === 503 || this.code === 'SERVICE_UNAVAILABLE'
  }

  /** Client could not reach the Engine at all. */
  get isNetwork(): boolean {
    return this.code === 'NETWORK_ERROR'
  }
}

/** Structural parse of the error envelope. Returns null when the shape is wrong. */
export function parseErrorEnvelope(body: unknown): ApiErrorEnvelope | null {
  if (typeof body !== 'object' || body === null) return null
  const error = (body as { error?: unknown }).error
  if (typeof error !== 'object' || error === null) return null
  const candidate = error as { code?: unknown; message?: unknown }
  if (typeof candidate.code !== 'string' || typeof candidate.message !== 'string') return null
  return body as ApiErrorEnvelope
}

/**
 * Best-effort human copy for an error code. Screens may override with object
 * names; this is the default per docs/design/components/system §3.3.
 */
export function errorCopy(error: ApiError, objectName = 'this'): string {
  switch (error.code) {
    case 'FORBIDDEN':
    case 'POLICY_DENIED':
      return `You don't have access to ${objectName}.`
    case 'NOT_FOUND':
      return `${capitalize(objectName)} doesn't exist or was removed.`
    case 'RATE_LIMITED':
      return 'Too many requests. Retrying shortly.'
    case 'CONFLICT':
    case 'DEPLOYMENT_INVALID_STATE':
      return 'This changed while you were viewing it.'
    case 'VALIDATION_FAILED':
    case 'INVALID_REQUEST':
      return 'Some values are not valid.'
    case 'SERVICE_UNAVAILABLE':
      return 'Axiom is temporarily unavailable.'
    case 'NETWORK_ERROR':
      return "Axiom couldn't be reached."
    case 'INTERNAL_ERROR':
    default:
      return "Axiom couldn't complete this request."
  }
}

function capitalize(value: string): string {
  return value.length === 0 ? value : value[0].toUpperCase() + value.slice(1)
}
