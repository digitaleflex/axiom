import { useState } from 'react'
import type { DeploymentStep } from '../api/types'
import { stepExplanation, stepLabel } from '../workflow/planSteps'
import { normalizeStepState, type StepState } from '../workflow/stepMapping'

/**
 * Step indicator (deployment-progress §1.3). Text + icon for every state;
 * completed steps use neutral, never `--status-live`.
 */
export function StepIndicator({ state }: { state: StepState }) {
  const icons: Record<StepState, string> = {
    queued: '○',
    running: '◐',
    completed: '✓',
    failed: '⬣',
    skipped: '—',
    cancelled: '⊘',
  }
  const labels: Record<StepState, string> = {
    queued: 'Queued',
    running: 'In progress',
    completed: 'Done',
    failed: 'Failed',
    skipped: 'Skipped',
    cancelled: 'Cancelled',
  }
  return (
    <span className={`step-indicator step-indicator--${state}`} data-state={state}>
      <span className="step-indicator__icon" aria-hidden="true">
        {icons[state]}
      </span>
      {labels[state]}
    </span>
  )
}

function formatDuration(startedAt?: string, completedAt?: string): string | null {
  if (!startedAt) return null
  const start = Date.parse(startedAt)
  const end = completedAt ? Date.parse(completedAt) : Date.now()
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) return null
  const seconds = Math.round((end - start) / 1000)
  const m = Math.floor(seconds / 60)
  const s = seconds % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

interface PlanSequenceProps {
  steps: string[]
  /** Live step states (deployment view). Omitted in the static plan view. */
  stepData?: DeploymentStep[]
  /** Precondition row (Analyze) — shown completed in plan view. */
  precondition?: { label: string; detail?: string }
  /** Auto-expand the failed step (failure view). */
  expandFailed?: boolean
}

/**
 * Plan sequence (deployment-plan §4–§5, handoff/deployment §2).
 *
 * Renders exactly `steps[]` in returned order — never adds, removes, renames
 * or reorders planner steps. Analyze appears only as a completed precondition
 * above the sequence, visually separated. Shared between the plan review and
 * the live deployment progress view.
 */
export function PlanSequence({ steps, stepData, precondition, expandFailed = false }: PlanSequenceProps) {
  const [expanded, setExpanded] = useState<Set<string>>(
    () => new Set(expandFailed ? (stepData ?? []).filter((s) => s.status === 'FAILED').map((s) => s.name) : []),
  )

  const toggle = (name: string) => {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      return next
    })
  }

  const stateFor = (name: string): StepState => {
    const match = (stepData ?? []).find((s) => s.name === name)
    return match ? normalizeStepState(match.status) : 'queued'
  }

  return (
    <div className="plan-sequence">
      {precondition && (
        <div className="plan-sequence__precondition">
          <span className="step-indicator__icon" aria-hidden="true">
            ✓
          </span>
          <span className="plan-sequence__precondition-label">{precondition.label}</span>
          {precondition.detail && <span className="muted">{precondition.detail}</span>}
        </div>
      )}
      <ol className="plan-sequence__list">
        {steps.map((code, index) => {
          const state = stateFor(code)
          const data = (stepData ?? []).find((s) => s.name === code)
          const isOpen = expanded.has(code)
          const duration = formatDuration(data?.startedAt, data?.completedAt)
          return (
            <li className="plan-step" key={code} data-state={state}>
              <button
                type="button"
                className="plan-step__header"
                aria-expanded={isOpen}
                aria-controls={`plan-step-${code}`}
                onClick={() => toggle(code)}
              >
                <span className="plan-step__number" aria-hidden="true">
                  {index + 1}
                </span>
                <span className="plan-step__name">{stepLabel(code)}</span>
                <span className="plan-step__state">
                  <StepIndicator state={state} />
                  {duration && <span className="mono muted"> · {duration}</span>}
                </span>
                <span className="plan-step__chevron" aria-hidden="true">
                  {isOpen ? '▾' : '▸'}
                </span>
              </button>
              <div className="plan-step__explanation">{stepExplanation(code)}</div>
              {isOpen && (
                <div className="plan-step__detail" id={`plan-step-${code}`}>
                  <div>
                    <span className="muted">Status</span> <StepIndicator state={state} />
                  </div>
                  {data?.startedAt && (
                    <div>
                      <span className="muted">Started</span> <span className="mono">{data.startedAt}</span>
                    </div>
                  )}
                  {data?.completedAt && (
                    <div>
                      <span className="muted">Completed</span> <span className="mono">{data.completedAt}</span>
                    </div>
                  )}
                  <div className="muted">Requires: planner order (no explicit dependencies in this plan).</div>
                </div>
              )}
            </li>
          )
        })}
      </ol>
    </div>
  )
}
