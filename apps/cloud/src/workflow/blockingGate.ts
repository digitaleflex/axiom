import type { Profile } from '../api/types'

/**
 * Profile review gating (application-profile §1, handoff/deployment §5).
 *
 * Continue is allowed only for `ready` profiles. `needs_review` blocks with
 * each blocking issue as a reason; `unsupported` blocks with the reason
 * from the Engine. Reasons are always shown as visible text — never a
 * silently disabled button.
 */
export interface GateResult {
  allowed: boolean
  reasons: string[]
}

export function canContinueFromProfile(profile: Profile | null | undefined): GateResult {
  if (!profile) {
    return { allowed: false, reasons: ['No profile yet — run an analysis first.'] }
  }
  if (profile.status === 'unsupported') {
    return {
      allowed: false,
      reasons: [profile.unsupported?.message ?? "This repository can't be deployed by Axiom V0.1 yet."],
    }
  }
  if (profile.status === 'needs_review') {
    const reasons = (profile.blocking ?? []).map((issue) => issue.message || issue.field || 'Unresolved detail')
    return { allowed: false, reasons }
  }
  return { allowed: true, reasons: [] }
}

/**
 * Configure → Plan gating (handoff/deployment §5).
 *
 * `Review plan` is never disabled when invalid — it validates and shows the
 * error summary. This returns the validation result the form displays.
 */
export interface PlanReviewGate {
  valid: boolean
  errors: string[]
}

export function canReviewPlan(input: {
  environment?: string
  serverStatus?: string
  degradedAcknowledged?: boolean
  ref?: string
  missingRequiredValues?: string[]
  domainInvalid?: boolean
}): PlanReviewGate {
  const errors: string[] = []
  if (!input.environment) errors.push('Choose an environment.')
  if (!input.ref) errors.push('Choose a ref to deploy.')
  if (input.serverStatus === 'offline') errors.push('The selected server is offline — choose another server.')
  if (input.serverStatus === 'degraded' && !input.degradedAcknowledged) {
    errors.push('Acknowledge that the selected server is degraded.')
  }
  for (const name of input.missingRequiredValues ?? []) {
    errors.push(`Set a value for ${name} (required).`)
  }
  if (input.domainInvalid) errors.push('Enter a valid domain name.')
  return { valid: errors.length === 0, errors }
}

/**
 * Deploy gating (handoff/deployment §5, deployment-plan §7).
 *
 * Deploy is disabled with a visible reason when the plan is not READY, is
 * stale, was already executed, or the target server is offline.
 */
export interface DeployGate {
  canDeploy: boolean
  reason: string | null
}

export function canDeployPlan(input: {
  planStatus?: string
  planStale?: boolean
  planExecuted?: boolean
  serverStatus?: string
}): DeployGate {
  if (input.planExecuted) {
    return { canDeploy: false, reason: 'This plan was already executed — generate a new plan to deploy again.' }
  }
  if (input.planStale) {
    return { canDeploy: false, reason: 'This plan is out of date — regenerate it before deploying.' }
  }
  if (input.planStatus && input.planStatus !== 'READY') {
    return { canDeploy: false, reason: `This plan is ${input.planStatus} and cannot be deployed.` }
  }
  if (input.serverStatus === 'offline') {
    return { canDeploy: false, reason: 'The target server is offline — choose another server.' }
  }
  if (!input.planStatus) {
    return { canDeploy: false, reason: 'The plan is still generating.' }
  }
  return { canDeploy: true, reason: null }
}
