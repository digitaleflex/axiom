import { API_BASE_URL, API_PREFIX, buildQuery, type QueryValue } from './client'

/**
 * Server-Sent Events reader for the deployment event stream
 * (api-contract §15, `GET /api/v1/deployments/{id}/events/stream`).
 *
 * Requirements satisfied here:
 * - `EventSource` transport.
 * - Resume: on every (re)connect we send the last seen sequence both as the
 *   `Last-Event-ID` header (browsers do this automatically when they manage
 *   reconnection) and, because we recreate the EventSource ourselves, as the
 *   `?lastEventId=` query parameter fallback the contract defines.
 * - Reconnect with exponential backoff (capped), reset on a successful open.
 * - Ordered delivery: numeric sequence ids are emitted strictly in order,
 *   buffering gaps and dropping duplicates (via {@link OrderedDelivery}).
 */

export interface SseMessage {
  id: string | null
  type: string
  data: string
}

export interface BuildSseUrlOptions {
  lastEventId?: string | null
  query?: Record<string, QueryValue>
  baseUrl?: string
}

export function buildSseUrl(path: string, options: BuildSseUrlOptions = {}): string {
  const base = (options.baseUrl ?? API_BASE_URL).replace(/\/+$/, '')
  const normalized = path.startsWith('/') ? path : `/${path}`
  const query: Record<string, QueryValue> = { ...options.query }
  if (options.lastEventId) {
    query.lastEventId = options.lastEventId
  }
  return `${base}${API_PREFIX}${normalized}${buildQuery(query)}`
}

export interface BackoffOptions {
  baseDelayMs?: number
  maxDelayMs?: number
  /** Deterministic by default; pass a jitter fraction (0–1) for production randomness. */
  jitter?: number
}

/** Exponential backoff for reconnect attempt `attempt` (0-based). */
export function nextBackoff(attempt: number, options: BackoffOptions = {}): number {
  const base = options.baseDelayMs ?? 1000
  const max = options.maxDelayMs ?? 30000
  const jitter = options.jitter ?? 0
  const raw = Math.min(base * 2 ** Math.max(0, attempt), max)
  if (jitter <= 0) return raw
  const spread = raw * jitter
  return Math.round(raw - spread / 2 + Math.random() * spread)
}

function toSequence(id: string | null): number | null {
  if (id === null || id === '') return null
  const seq = Number(id)
  return Number.isInteger(seq) && seq >= 0 ? seq : null
}

/**
 * Emits events with numeric ids in ascending order, buffering out-of-order
 * frames until the gap is filled. Events without a numeric id pass through.
 */
export class OrderedDelivery {
  private nextSequence: number | null = null
  private readonly buffer = new Map<number, SseMessage>()

  constructor(private readonly emit: (message: SseMessage) => void) {}

  push(message: SseMessage): void {
    const sequence = toSequence(message.id)
    if (sequence === null) {
      this.emit(message)
      return
    }
    if (this.nextSequence === null) this.nextSequence = sequence
    if (sequence < this.nextSequence) return // duplicate / already delivered
    this.buffer.set(sequence, message)
    this.flush()
  }

  reset(): void {
    this.nextSequence = null
    this.buffer.clear()
  }

  private flush(): void {
    if (this.nextSequence === null) return
    let message = this.buffer.get(this.nextSequence)
    while (message) {
      this.buffer.delete(this.nextSequence)
      this.emit(message)
      this.nextSequence += 1
      message = this.buffer.get(this.nextSequence)
    }
  }
}

/**
 * Event names emitted by the Engine.
 *
 * Authoritative source: api-contract §14 ("Deployment events", the `Event
 * types:` sentence) — the list is reproduced verbatim there:
 * `deployment.created`, `deployment.status.changed`, `deployment.step.started`,
 * `deployment.step.completed`, `deployment.step.failed`,
 * `deployment.step.skipped`, `health.passed`, `health.failed`.
 *
 * Every entry is verified to be produced by the Engine:
 * - `deployment.*` step/status events are defined in
 *   `services/engine/internal/deployment/event.go:10-15` and persisted through
 *   the deployment event store (`internal/database/deployment/store.go`).
 *   `deployment.created` is emitted on create
 *   (`internal/database/deployment/store.go:166`), `deployment.step.skipped`
 *   is the default branch of `stepEventType` for any non-running /
 *   non-completed / non-failed step (`internal/deployment/memstore.go:276-287`).
 * - `health.passed` / `health.failed` are defined in
 *   `services/engine/internal/health/health.go:32-33` and appended by
 *   `RecordHealth` (`internal/deployment/service.go:114-116`), so they travel
 *   on the very same stream.
 *
 * NOT a member of this list: `deployment.log.appended`. It was previously
 * listed here but the Engine never emits it — build output goes to the
 * `deployment_logs` table via `internal/logs.Appender`, which has no event
 * counterpart. api-contract §14 "Deployment logs" defines logs as a separate
 * keyset-paginated resource (`GET /api/v1/deployments/{id}/logs`), and §15
 * "Realtime deployment updates" scopes SSE to deployment progress only: the
 * frame list it enumerates (`event: deployment.step.started`,
 * `deployment.step.completed`, `deployment.status.changed`) contains no log
 * event. Nothing in the contract requires live log streaming, so the entry was
 * removed here rather than adding an emission point to the Engine.
 *
 * Note the Engine's audit / authz action names (`deployment.cancel` in
 * `internal/api/handlers.go:468` and `ActionDeploymentCancel` in
 * `internal/authz/authz.go:29`) are *not* event types and are correctly absent.
 */
export const DEPLOYMENT_EVENT_TYPES = [
  'deployment.created',
  'deployment.status.changed',
  'deployment.step.started',
  'deployment.step.completed',
  'deployment.step.failed',
  'deployment.step.skipped',
  'health.passed',
  'health.failed',
] as const

/** Union of every event type the Engine can put on the deployment stream. */
export type DeploymentEventType = (typeof DEPLOYMENT_EVENT_TYPES)[number]

const DEPLOYMENT_EVENT_TYPE_SET: ReadonlySet<string> = new Set(DEPLOYMENT_EVENT_TYPES)

/**
 * Narrows a raw SSE frame name to the contract §14 union. Frames the Engine
 * does not know about are filtered out by the caller instead of being cast.
 */
export function isDeploymentEventType(type: string): type is DeploymentEventType {
  return DEPLOYMENT_EVENT_TYPE_SET.has(type)
}

export interface SseStreamOptions {
  path: string
  lastEventId?: string | null
  eventTypes?: readonly DeploymentEventType[]
  onMessage: (message: SseMessage) => void
  onOpen?: () => void
  onReconnect?: (attempt: number, delayMs: number) => void
  onError?: (error: unknown) => void
  maxAttempts?: number
  baseDelayMs?: number
  maxDelayMs?: number
}

export class SseStream {
  private source: EventSource | null = null
  private timer: ReturnType<typeof setTimeout> | null = null
  private attempt = 0
  private stopped = false
  private lastEventId: string | null
  private readonly delivery: OrderedDelivery

  constructor(private readonly options: SseStreamOptions) {
    this.lastEventId = options.lastEventId ?? null
    this.delivery = new OrderedDelivery((message) => this.options.onMessage(message))
  }

  get lastDeliveredEventId(): string | null {
    return this.lastEventId
  }

  start(): void {
    this.stopped = false
    this.connect()
  }

  stop(): void {
    this.stopped = true
    if (this.timer) {
      clearTimeout(this.timer)
      this.timer = null
    }
    this.source?.close()
    this.source = null
  }

  private connect(): void {
    if (typeof EventSource === 'undefined') {
      this.options.onError?.(new Error('EventSource is not available in this environment.'))
      return
    }

    const url = buildSseUrl(this.options.path, { lastEventId: this.lastEventId })
    const source = new EventSource(url)
    this.source = source

    source.onopen = () => {
      this.attempt = 0
      this.options.onOpen?.()
    }

    const handle = (event: MessageEvent) => {
      const message: SseMessage = {
        id: event.lastEventId || null,
        type: (event as MessageEvent & { type: string }).type,
        data: typeof event.data === 'string' ? event.data : String(event.data),
      }
      if (message.id) this.lastEventId = message.id
      this.delivery.push(message)
    }

    const eventTypes = this.options.eventTypes ?? DEPLOYMENT_EVENT_TYPES
    for (const type of eventTypes) {
      source.addEventListener(type, handle as EventListener)
    }
    source.onmessage = handle

    source.onerror = (event) => {
      this.options.onError?.(event)
      if (this.stopped) return
      source.close()
      this.scheduleReconnect()
    }
  }

  private scheduleReconnect(): void {
    const maxAttempts = this.options.maxAttempts ?? Number.POSITIVE_INFINITY
    if (this.attempt >= maxAttempts) {
      this.options.onError?.(new Error('Reconnect attempts exhausted.'))
      return
    }
    const delay = nextBackoff(this.attempt, {
      baseDelayMs: this.options.baseDelayMs,
      maxDelayMs: this.options.maxDelayMs,
    })
    this.attempt += 1
    this.options.onReconnect?.(this.attempt, delay)
    this.timer = setTimeout(() => this.connect(), delay)
  }
}
