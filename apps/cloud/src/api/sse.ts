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

/** Event names emitted by the Engine (api-contract §15, §23). */
export const DEPLOYMENT_EVENT_TYPES = [
  'deployment.status.changed',
  'deployment.step.started',
  'deployment.step.completed',
  'deployment.step.failed',
  'deployment.log.appended',
] as const

export interface SseStreamOptions {
  path: string
  lastEventId?: string | null
  eventTypes?: readonly string[]
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
