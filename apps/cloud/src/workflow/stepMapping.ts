import type { DeploymentStep } from '../api/types'
import type { DeploymentEventType } from '../api/sse'

/**
 * SSE event → step/status mapping (deployment-progress §1, §8).
 *
 * Events are applied idempotently: a step never moves backwards, and a
 * terminal status is final. Step data, when present, always wins over
 * status-derived emphasis.
 *
 * The table below is keyed by {@link DeploymentEventType} and typed with
 * `satisfies Record<DeploymentEventType, …>`, so TypeScript fails to compile if
 * the Engine ever gains an event type that this console has no case for. The
 * exhaustive list comes from api-contract §14.
 */
export type StepState = 'queued' | 'running' | 'completed' | 'failed' | 'skipped' | 'cancelled'

export const TERMINAL_DEPLOYMENT_STATUSES: readonly string[] = ['LIVE', 'FAILED', 'CANCELLED']

export function isTerminalStatus(status: string | undefined | null): boolean {
  return !!status && TERMINAL_DEPLOYMENT_STATUSES.includes(status)
}

export interface StepEventMapping {
  step?: string
  state?: StepState
  status?: string
}

function str(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined
}

/**
 * One entry per event type of api-contract §14.
 *
 * - `deployment.created` carries `{ status, planId, number }`; only the status
 *   is meaningful here (always `PENDING` at that point) and it is already
 *   known from the snapshot, so mapping it is harmless and keeps the stream
 *   self-sufficient.
 * - `deployment.step.skipped` carries `{ step, status: "SKIPPED" }` and is
 *   rendered as the "Skipped" state of deployment-progress §1.3 (INACTIVE,
 *   dash). `applyStepEvent` writes the uppercased state, which is exactly the
 *   `SKIPPED` value `normalizeStepState` reads back.
 * - `health.passed` / `health.failed` carry a probe report
 *   (`{ status, statusCode, latencyMs, path, checkedAt, attempt, … }`), not a
 *   step or a deployment status. The Progress screen has no health line —
 *   deployment-progress §1.3 has no health step state, and the health result is
 *   displayed by the Health tab — so these are explicitly no-ops rather than
 *   silently falling through. TODO: render the probe outcome in the "Live
 *   events" feed as described by deployment-progress §5 line 127
 *   ("health-check results"). Needs a product decision on the exact wording,
 *   hence not invented here.
 */
const EVENT_MAPPING = {
  'deployment.created': (data: Record<string, unknown>): StepEventMapping => ({
    status: str(data.status),
  }),
  'deployment.status.changed': (data: Record<string, unknown>): StepEventMapping => ({
    status: str(data.status),
  }),
  'deployment.step.started': (data: Record<string, unknown>): StepEventMapping => ({
    step: str(data.step),
    state: 'running',
  }),
  'deployment.step.completed': (data: Record<string, unknown>): StepEventMapping => ({
    step: str(data.step),
    state: 'completed',
  }),
  'deployment.step.failed': (data: Record<string, unknown>): StepEventMapping => ({
    step: str(data.step),
    state: 'failed',
  }),
  'deployment.step.skipped': (data: Record<string, unknown>): StepEventMapping => ({
    step: str(data.step),
    state: 'skipped',
  }),
  'health.passed': (): StepEventMapping | null => null,
  'health.failed': (): StepEventMapping | null => null,
} satisfies Record<DeploymentEventType, (data: Record<string, unknown>) => StepEventMapping | null>

/** Maps an SSE event type + payload to a step state and/or deployment status. */
export function mapDeploymentEvent(
  type: DeploymentEventType | undefined,
  data: Record<string, unknown> | undefined,
): StepEventMapping | null {
  if (type === undefined) return null
  const handler = EVENT_MAPPING[type]
  if (!handler) return null
  return handler(data ?? {})
}

/**
 * Applies a step event to the step list, ignoring events that would move a
 * step backwards (idempotent application, deployment-progress §8.3).
 */
export function applyStepEvent(steps: DeploymentStep[], mapping: StepEventMapping): DeploymentStep[] {
  const stepName = mapping.step
  const newState = mapping.state
  if (!stepName || !newState) return steps
  const order: Record<StepState, number> = {
    queued: 0,
    running: 1,
    completed: 2,
    failed: 2,
    skipped: 2,
    cancelled: 2,
  }
  return steps.map((step) => {
    if (step.name !== stepName) return step
    const current = normalizeStepState(step.status)
    // Never move a step backwards (e.g. completed → running on replay).
    if (order[newState] < order[current]) return step
    return { ...step, status: newState.toUpperCase() }
  })
}

export function normalizeStepState(status: string | undefined | null): StepState {
  switch ((status ?? '').toUpperCase()) {
    case 'RUNNING':
      return 'running'
    case 'COMPLETED':
      return 'completed'
    case 'FAILED':
      return 'failed'
    case 'SKIPPED':
      return 'skipped'
    case 'CANCELLED':
      return 'cancelled'
    default:
      return 'queued'
  }
}

/** Status → step emphasis (presentation only; step data wins when present). */
export function stepsActiveForStatus(status: string | undefined): string[] {
  switch ((status ?? '').toUpperCase()) {
    case 'BUILDING':
      return ['BUILD']
    case 'DEPLOYING':
      return ['CREATE_RUNTIME', 'NETWORK', 'START']
    case 'VERIFYING':
      return ['VERIFY']
    default:
      return []
  }
}
