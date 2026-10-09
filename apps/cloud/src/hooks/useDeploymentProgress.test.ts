import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useDeploymentProgress } from './useDeploymentProgress'
import { DEPLOYMENT_EVENT_TYPES } from '../api/sse'
import type { Deployment, DeploymentStep } from '../api/types'

/**
 * End-to-end proof that the console is aligned with the Engine event stream
 * (api-contract §14 / §15): an event the Engine really emits
 * (`deployment.step.skipped`, defined in
 * services/engine/internal/deployment/event.go:15) updates the step list, and
 * the never-emitted `deployment.log.appended` does not break the flow.
 */

const getDeployment = vi.fn<() => Promise<Deployment>>()
const listSteps = vi.fn<() => Promise<DeploymentStep[]>>()

vi.mock('../api/resources', () => ({
  getDeployment: (...args: unknown[]) => getDeployment(...(args as [])),
  listSteps: (...args: unknown[]) => listSteps(...(args as [])),
}))

type Listener = (event: { type: string; data: string; lastEventId: string }) => void

interface FakeSource {
  url: string
  listeners: Map<string, Listener>
  onopen: (() => void) | null
  onmessage: Listener | null
  onerror: ((event: unknown) => void) | null
  close: () => void
  addEventListener: (type: string, listener: Listener) => void
}

const sources: FakeSource[] = []

/** Fire the transport-level `open` event, as a real EventSource would. */
function open() {
  const source = sources[sources.length - 1]
  if (!source.onopen) throw new Error('no onopen handler registered')
  act(() => source.onopen?.())
}

/** Frame the Engine would send: `id: <seq>`, `event: <type>`, `data: <json>`. */
function frame(type: string, data: Record<string, unknown>, seq: number) {
  const source = sources[sources.length - 1]
  const event = { type, data: JSON.stringify(data), lastEventId: String(seq) }
  const listener = source.listeners.get(type) ?? source.onmessage
  if (!listener) throw new Error(`no listener registered for ${type}`)
  act(() => listener(event))
}

class FakeEventSource {
  url: string
  listeners = new Map<string, Listener>()
  onopen: (() => void) | null = null
  onmessage: Listener | null = null
  onerror: ((event: unknown) => void) | null = null
  closed = false

  constructor(url: string) {
    this.url = url
    sources.push(this as unknown as FakeSource)
  }

  addEventListener(type: string, listener: Listener) {
    this.listeners.set(type, listener)
  }

  close() {
    this.closed = true
  }
}

const snapshot: Deployment = {
  id: 'dep_1',
  number: 42,
  status: 'DEPLOYING',
} as Deployment

const initialSteps: DeploymentStep[] = [
  { name: 'BUILD', status: 'COMPLETED' },
  { name: 'NETWORK', status: 'QUEUED' },
  { name: 'VERIFY', status: 'QUEUED' },
]

beforeEach(() => {
  sources.length = 0
  getDeployment.mockReset().mockResolvedValue(snapshot)
  listSteps.mockReset().mockResolvedValue(initialSteps)
  vi.stubGlobal('EventSource', FakeEventSource as unknown as typeof EventSource)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useDeploymentProgress — Engine event alignment', () => {
  it('subscribes to every event type of api-contract §14', async () => {
    const { result } = renderHook(() => useDeploymentProgress('dep_1'))
    await waitFor(() => expect(sources.length).toBeGreaterThan(0))
    open()
    await waitFor(() => expect(result.current.connection).toBe('live'))

    const registered = [...sources[0].listeners.keys()].sort()
    expect(registered).toEqual([...DEPLOYMENT_EVENT_TYPES].sort())
    // The regression that opened this work: the Engine emits a skipped step.
    expect(registered).toContain('deployment.step.skipped')
    expect(registered).toContain('deployment.created')
  })

  it('applies a deployment.step.skipped frame emitted by the Engine', async () => {
    const { result } = renderHook(() => useDeploymentProgress('dep_1'))
    await waitFor(() => expect(sources.length).toBeGreaterThan(0))
    open()
    await waitFor(() => expect(result.current.connection).toBe('live'))

    // Engine payload shape: deployment.stepEventData → { step, status }.
    frame('deployment.step.skipped', { step: 'NETWORK', status: 'SKIPPED' }, 7)

    await waitFor(() => {
      expect(result.current.steps.find((s) => s.name === 'NETWORK')?.status).toBe('SKIPPED')
    })
    // Untouched steps stay untouched.
    expect(result.current.steps.find((s) => s.name === 'VERIFY')?.status).toBe('QUEUED')
  })

  it('ignores a deployment.log.appended frame without breaking the stream', async () => {
    const { result } = renderHook(() => useDeploymentProgress('dep_1'))
    await waitFor(() => expect(sources.length).toBeGreaterThan(0))
    open()
    await waitFor(() => expect(result.current.connection).toBe('live'))

    // No listener exists for this type: it reaches the unnamed `onmessage`
    // handler and must be discarded rather than break sequencing.
    frame('deployment.log.appended', { line: 'compiling…' }, 8)

    // The stream keeps working for the events that follow.
    frame('deployment.step.started', { step: 'VERIFY', status: 'RUNNING' }, 9)
    frame('deployment.status.changed', { from: 'DEPLOYING', status: 'LIVE' }, 10)

    await waitFor(() => expect(result.current.status).toBe('LIVE'))
    expect(result.current.steps.find((s) => s.name === 'VERIFY')?.status).toBe('RUNNING')
    expect(result.current.connection).toBe('live')
    expect(result.current.error).toBeNull()
  })

  it('applies a deployment.created frame', async () => {
    const { result } = renderHook(() => useDeploymentProgress('dep_1'))
    await waitFor(() => expect(sources.length).toBeGreaterThan(0))
    open()
    await waitFor(() => expect(result.current.connection).toBe('live'))

    // Engine payload shape: deployment create → { status, planId, number }.
    frame('deployment.created', { status: 'PENDING', planId: 'plan_1', number: 42 }, 1)
    await waitFor(() => expect(result.current.status).toBe('PENDING'))
  })
})