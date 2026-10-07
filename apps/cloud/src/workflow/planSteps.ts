/**
 * Plan step label mapping (handoff/deployment §3).
 *
 * Single source of truth for plan steps (`steps[]`) and deployment steps
 * (`GET /deployments/{id}/steps`). Unknown codes render raw in mono — never
 * dropped, renamed or reordered.
 */
export const PLAN_STEP_LABELS: Record<string, string> = {
  BUILD: 'Build',
  CREATE_RUNTIME: 'Create Runtime',
  NETWORK: 'Configure Network',
  START: 'Start',
  VERIFY: 'Verify',
}

export function stepLabel(code: string | undefined | null): string {
  if (!code) return '—'
  return PLAN_STEP_LABELS[code] ?? code
}

/** Short explanation template per step (deployment-plan §4). */
export function stepExplanation(code: string): string {
  switch (code) {
    case 'BUILD':
      return 'Build a container image from the analyzed ref'
    case 'CREATE_RUNTIME':
      return 'Prepare the runtime on the target server'
    case 'NETWORK':
      return 'Route the domain with HTTPS'
    case 'START':
      return 'Start the application on its port'
    case 'VERIFY':
      return 'Check the application responds before going LIVE'
    default:
      return 'Step defined by the planner'
  }
}
