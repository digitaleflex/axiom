import { Link } from 'react-router-dom'
import { routes, SETUP_STEPS, type SetupStep } from '../routes/builders'

const STEP_TITLES: Record<SetupStep, string> = {
  analysis: 'Analyze',
  profile: 'Profile',
  server: 'Server',
  configure: 'Configure',
  plan: 'Plan',
}

export const SETUP_STEP_TITLES = STEP_TITLES

/**
 * Setup stepper (screens/shell §8). Completed steps link back to their
 * screen; the current step is `aria-current="step"`; future steps are
 * inert. The Plan step links to the plan URL when a planId is known.
 */
export function SetupStepper({
  applicationId,
  current,
  planId,
  maxReached,
}: {
  applicationId: string
  current: SetupStep
  planId?: string
  /** Highest step the user may navigate back to (default: current). */
  maxReached?: SetupStep
}) {
  const currentIndex = SETUP_STEPS.indexOf(current)
  const maxIndex = SETUP_STEPS.indexOf(maxReached ?? current)

  return (
    <nav aria-label="Setup steps" className="stepper">
      <ol className="stepper__list">
        {SETUP_STEPS.map((step, index) => {
          const state = index < currentIndex ? 'completed' : index === currentIndex ? 'current' : 'upcoming'
          const reachable = index <= maxIndex
          const label = STEP_TITLES[step]
          const target =
            step === 'plan' && planId
              ? routes.setupPlan({ applicationId, planId })
              : routes.setup({ applicationId, step })

          return (
            <li key={step} className="stepper__item" data-state={state}>
              {index > 0 && <span className="stepper__connector" aria-hidden="true" />}
              {reachable ? (
                <Link to={target} aria-current={state === 'current' ? 'step' : undefined} className="stepper__link">
                  <span className="stepper__marker" aria-hidden="true">
                    {state === 'completed' ? '✓' : index + 1}
                  </span>
                  <span className="stepper__label">{label}</span>
                </Link>
              ) : (
                <span className="stepper__link" aria-disabled="true">
                  <span className="stepper__marker" aria-hidden="true">
                    {index + 1}
                  </span>
                  <span className="stepper__label">{label}</span>
                </span>
              )}
            </li>
          )
        })}
      </ol>
    </nav>
  )
}
