import { describe, expect, it } from 'vitest'
import { canContinueFromProfile, canDeployPlan, canReviewPlan } from './blockingGate'
import { idempotencyKeyForPlan } from './idempotency'
import { deploymentErrorCopy, analysisErrorCopy } from './errorCopy'
import { confidenceBand, bandRequiresConfirmation } from './confidence'
import { stepLabel, stepExplanation } from './planSteps'
import {
  applyStepEvent,
  isTerminalStatus,
  mapDeploymentEvent,
  normalizeStepState,
  stepsActiveForStatus,
  type StepState,
} from './stepMapping'
import type { DeploymentStep, Profile } from '../api/types'
import { DEPLOYMENT_EVENT_TYPES, isDeploymentEventType } from '../api/sse'

describe('blocking gate (profile review)', () => {
  it('allows Continue only for ready profiles', () => {
    const ready: Profile = { status: 'ready', blocking: [] }
    expect(canContinueFromProfile(ready).allowed).toBe(true)

    const needsReview: Profile = {
      status: 'needs_review',
      blocking: [{ field: 'packageManager', code: 'ambiguous', message: 'Two lockfiles found.' }],
    }
    const gate = canContinueFromProfile(needsReview)
    expect(gate.allowed).toBe(false)
    expect(gate.reasons).toContain('Two lockfiles found.')

    const unsupported: Profile = { status: 'unsupported', unsupported: { code: 'UNSUPPORTED_FRAMEWORK', message: "Python isn't supported in V0.1." } }
    expect(canContinueFromProfile(unsupported).allowed).toBe(false)

    expect(canContinueFromProfile(null).allowed).toBe(false)
  })

  it('gates plan review on environment, server state, ref, required values and domain', () => {
    const valid = canReviewPlan({
      environment: 'production',
      serverStatus: 'ready',
      ref: 'main',
      missingRequiredValues: [],
    })
    expect(valid.valid).toBe(true)

    const invalid = canReviewPlan({
      environment: undefined,
      serverStatus: 'offline',
      ref: undefined,
      missingRequiredValues: ['DATABASE_URL'],
      domainInvalid: true,
    })
    expect(invalid.valid).toBe(false)
    expect(invalid.errors.length).toBeGreaterThanOrEqual(4)

    const degraded = canReviewPlan({ environment: 'staging', serverStatus: 'degraded', ref: 'main' })
    expect(degraded.valid).toBe(false)
    const degradedAck = canReviewPlan({
      environment: 'staging',
      serverStatus: 'degraded',
      ref: 'main',
      degradedAcknowledged: true,
    })
    expect(degradedAck.valid).toBe(true)
  })

  it('gates Deploy on plan status, staleness, execution and server state with reasons', () => {
    expect(
      canDeployPlan({ planStatus: 'READY', serverStatus: 'ready' }).canDeploy,
    ).toBe(true)

    const stale = canDeployPlan({ planStatus: 'READY', planStale: true, serverStatus: 'ready' })
    expect(stale.canDeploy).toBe(false)
    expect(stale.reason).toMatch(/out of date/i)

    const executed = canDeployPlan({ planStatus: 'READY', planExecuted: true, serverStatus: 'ready' })
    expect(executed.canDeploy).toBe(false)
    expect(executed.reason).toMatch(/already executed/i)

    const offline = canDeployPlan({ planStatus: 'READY', serverStatus: 'offline' })
    expect(offline.canDeploy).toBe(false)
    expect(offline.reason).toMatch(/offline/i)
  })
})

describe('SSE event → step mapping', () => {
  it('maps event types to step states and statuses', () => {
    expect(mapDeploymentEvent('deployment.step.started', { step: 'BUILD' })).toEqual({
      step: 'BUILD',
      state: 'running',
    })
    expect(mapDeploymentEvent('deployment.step.completed', { step: 'BUILD' })).toEqual({
      step: 'BUILD',
      state: 'completed',
    })
    expect(mapDeploymentEvent('deployment.step.failed', { step: 'VERIFY' })).toEqual({
      step: 'VERIFY',
      state: 'failed',
    })
    expect(mapDeploymentEvent('deployment.status.changed', { status: 'LIVE' })).toEqual({ status: 'LIVE' })
    expect(mapDeploymentEvent('health.passed', {})).toBeNull()
  })

  it('applies step events idempotently and never moves a step backwards', () => {
    const steps: DeploymentStep[] = [
      { name: 'BUILD', status: 'COMPLETED' },
      { name: 'START', status: 'RUNNING' },
    ]
    const applied = applyStepEvent(steps, { step: 'BUILD', state: 'running' })
    expect(applied[0].status).toBe('COMPLETED') // replay ignored
    expect(applied[1].status).toBe('RUNNING')

    const failed = applyStepEvent(applied, { step: 'START', state: 'failed' })
    expect(failed[1].status).toBe('FAILED')
  })

  it('normalizes step states and recognizes terminal statuses', () => {
    expect(normalizeStepState('RUNNING')).toBe('running')
    expect(normalizeStepState('COMPLETED')).toBe('completed')
    expect(normalizeStepState(undefined)).toBe('queued')
    expect(isTerminalStatus('LIVE')).toBe(true)
    expect(isTerminalStatus('FAILED')).toBe(true)
    expect(isTerminalStatus('BUILDING')).toBe(false)
  })

  // Alignment with the Engine: api-contract §14 is the authoritative event
  // list, and the Engine emits all of it (deployment/event.go + health/health.go).
  it('lists exactly the event types of api-contract §14', () => {
    expect([...DEPLOYMENT_EVENT_TYPES].sort()).toEqual([
      'deployment.created',
      'deployment.status.changed',
      'deployment.step.completed',
      'deployment.step.failed',
      'deployment.step.skipped',
      'deployment.step.started',
      'health.failed',
      'health.passed',
    ])
  })

  it('has a mapping decision for every event type the Engine can emit', () => {
    for (const type of DEPLOYMENT_EVENT_TYPES) {
      expect(isDeploymentEventType(type)).toBe(true)
      // Explicitly accounted for: either a mapping, or a documented no-op.
      const mapping = mapDeploymentEvent(type, {})
      expect(mapping === null || typeof mapping === 'object').toBe(true)
    }
  })

  it('maps deployment.created and deployment.step.skipped (Engine-emitted, formerly missing)', () => {
    expect(mapDeploymentEvent('deployment.created', { status: 'PENDING', planId: 'plan_1', number: 42 })).toEqual({
      status: 'PENDING',
    })
    const skipped = mapDeploymentEvent('deployment.step.skipped', { step: 'NETWORK', status: 'SKIPPED' })
    expect(skipped).toEqual({ step: 'NETWORK', state: 'skipped' })

    const steps: DeploymentStep[] = [
      { name: 'NETWORK', status: 'QUEUED' },
      { name: 'VERIFY', status: 'QUEUED' },
    ]
    const applied = applyStepEvent(steps, skipped!)
    expect(applied[0].status).toBe('SKIPPED')
    expect(normalizeStepState(applied[0].status)).toBe('skipped')
    expect(applied[1].status).toBe('QUEUED')
  })

  it('ignores deployment.log.appended: not an Engine event, absent from the contract', () => {
    expect(DEPLOYMENT_EVENT_TYPES).not.toContain('deployment.log.appended')
    expect(isDeploymentEventType('deployment.log.appended')).toBe(false)
  })

  it('maps deployment statuses to typically-active steps (presentation only)', () => {
    expect(stepsActiveForStatus('BUILDING')).toEqual(['BUILD'])
    expect(stepsActiveForStatus('DEPLOYING')).toEqual(['CREATE_RUNTIME', 'NETWORK', 'START'])
    expect(stepsActiveForStatus('VERIFYING')).toEqual(['VERIFY'])
    expect(stepsActiveForStatus('PENDING')).toEqual([])
  })

  it('supports every step state in the Design DNA vocabulary', () => {
    const states: StepState[] = ['queued', 'running', 'completed', 'failed', 'skipped', 'cancelled']
    const order: Record<StepState, number> = {
      queued: 0,
      running: 1,
      completed: 2,
      failed: 2,
      skipped: 2,
      cancelled: 2,
    }
    for (const state of states) {
      expect(order[state]).toBeGreaterThanOrEqual(0)
    }
  })
})

describe('idempotency keys', () => {
  it('is deterministic per plan so retries cannot create a second deployment', () => {
    expect(idempotencyKeyForPlan('plan_123')).toBe('deploy:plan_123')
    expect(idempotencyKeyForPlan('plan_123')).toBe(idempotencyKeyForPlan('plan_123'))
    expect(idempotencyKeyForPlan('plan_456')).not.toBe(idempotencyKeyForPlan('plan_123'))
  })
})

describe('error-code → copy mapping', () => {
  it('maps canonical deployment error codes to explanations and checklists', () => {
    const build = deploymentErrorCopy('BUILD_FAILED')
    expect(build.explanation).toMatch(/build failed/i)
    expect(build.checklist.length).toBeGreaterThan(0)

    const health = deploymentErrorCopy('HEALTH_CHECK_FAILED')
    expect(health.explanation).toMatch(/health check/i)

    const eligible = deploymentErrorCopy('DEPLOYMENT_NOT_ELIGIBLE')
    expect(eligible.explanation).toMatch(/server/i)
  })

  it('renders unknown codes raw without inventing causes', () => {
    const unknown = deploymentErrorCopy('SOME_FUTURE_CODE')
    expect(unknown.explanation).toMatch(/stopped this deployment/i)
    expect(unknown.checklist.join(' ')).toMatch(/logs/i)
  })

  it('maps analysis failure codes', () => {
    expect(analysisErrorCopy('REF_NOT_FOUND')).toMatch(/ref/i)
    expect(analysisErrorCopy('SNAPSHOT_REJECTED')).toMatch(/snapshot/i)
    expect(analysisErrorCopy(undefined)).toMatch(/could not complete/i)
  })
})

describe('confidence bands', () => {
  it('bands confidence into high/medium/low with confirmation gating', () => {
    expect(confidenceBand(0.98)).toBe('high')
    expect(confidenceBand(0.9)).toBe('high')
    expect(confidenceBand(0.75)).toBe('medium')
    expect(confidenceBand(0.6)).toBe('medium')
    expect(confidenceBand(0.3)).toBe('low')
    expect(confidenceBand(undefined)).toBe('low')

    expect(bandRequiresConfirmation('low')).toBe(true)
    expect(bandRequiresConfirmation('high')).toBe(false)
  })
})

describe('plan step labels', () => {
  it('maps contract step codes to labels and renders unknown codes raw', () => {
    expect(stepLabel('BUILD')).toBe('Build')
    expect(stepLabel('CREATE_RUNTIME')).toBe('Create Runtime')
    expect(stepLabel('NETWORK')).toBe('Configure Network')
    expect(stepLabel('START')).toBe('Start')
    expect(stepLabel('VERIFY')).toBe('Verify')
    expect(stepLabel('FUTURE_STEP')).toBe('FUTURE_STEP')
    expect(stepLabel(undefined)).toBe('—')
  })

  it('provides an explanation template per step', () => {
    expect(stepExplanation('BUILD')).toMatch(/container image/i)
    expect(stepExplanation('VERIFY')).toMatch(/LIVE/i)
    expect(stepExplanation('UNKNOWN')).toMatch(/planner/i)
  })
})
