import type { DeploymentStep } from '../api/types'

/**
 * SSE event → step/status mapping (deployment-progress §1, §8).
 *
 * Events are applied idempotently: a step never moves backwards, and a
 * terminal status is final. Step data, when present, always wins over
 * status-derived emphasis.
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

/** Maps an SSE event type + payload to a step state and/or deployment status. */
export function mapDeploymentEvent(
  type: string | undefined,
  data: Record<string, unknown> | undefined,
): StepEventMapping | null {
  const payload = data ?? {}
  switch (type) {
    case 'deployment.step.started':
      return { step: payload.step as string, state: 'running' }
    case 'deployment.step.completed':
      return { step: payload.step as string, state: 'completed' }
    case 'deployment.step.failed':
      return { step: payload.step as string, state: 'failed' }
    case 'deployment.step.skipped':
      return { step: payload.step as string, state: 'skipped' }
    case 'deployment.status.changed':
      return { status: payload.status as string }
    default:
      return null
  }
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
