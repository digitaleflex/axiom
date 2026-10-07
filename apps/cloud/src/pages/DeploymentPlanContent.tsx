import { useParams } from 'react-router-dom'
import { getDeployment, getPlan } from '../api/resources'
import { PlanSequence } from '../components/PlanSequence'
import { ErrorPanel, InlineNotice, SkeletonLines } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { stepLabel } from '../workflow/planSteps'

/**
 * Deployment Plan tab content (deployment-plan §2). Read-only reuse of the
 * plan view — no Deploy action. The plan is immutable: the URL always
 * identifies one plan.
 */
export function DeploymentPlanContent() {
  const { deploymentId } = useParams<{ deploymentId: string }>()

  const deploymentState = useAsync(
    (signal) => (deploymentId ? getDeployment(deploymentId, { signal }) : Promise.reject(new Error('missing id'))),
    [deploymentId],
  )

  const planId = deploymentState.data?.planId
  const planState = useAsync(
    (signal) => (planId ? getPlan(planId, { signal }) : Promise.reject(new Error('missing plan'))),
    [planId],
  )

  const plan = planState.data

  return (
    <div className="content__body stack">
      {deploymentState.status === 'loading' && <SkeletonLines lines={4} />}

      {deploymentState.status === 'error' && deploymentState.error && (
        <ErrorPanel error={deploymentState.error} objectName="this deployment" onRetry={deploymentState.reload} />
      )}

      {deploymentState.status === 'success' && !planId && (
        <InlineNotice variant="info" title="No plan for this deployment">
          This deployment was not created from a reviewable plan.
        </InlineNotice>
      )}

      {planId && planState.status === 'loading' && <SkeletonLines lines={4} />}

      {planId && planState.status === 'error' && planState.error && (
        <ErrorPanel error={planState.error} objectName="this plan" onRetry={planState.reload} />
      )}

      {plan && (
        <>
          <section className="card" aria-label="Plan summary">
            <h2 className="section-title">Plan</h2>
            <div className="data-list">
              <div className="data-list__row">
                <div className="data-list__main">
                  <div className="data-list__title">Plan ID</div>
                </div>
                <span className="mono">{plan.id}</span>
              </div>
              <div className="data-list__row">
                <div className="data-list__main">
                  <div className="data-list__title">Status</div>
                </div>
                <span className="mono">{plan.status ?? '—'}</span>
              </div>
              <div className="data-list__row">
                <div className="data-list__main">
                  <div className="data-list__title">Profile version</div>
                </div>
                <span className="mono">{plan.applicationProfileVersion ?? '—'}</span>
              </div>
              <div className="data-list__row">
                <div className="data-list__main">
                  <div className="data-list__title">Server</div>
                </div>
                <span className="mono">{plan.serverId ?? '—'}</span>
              </div>
              <div className="data-list__row">
                <div className="data-list__main">
                  <div className="data-list__title">Health check</div>
                </div>
                <span className="mono">
                  {plan.healthCheck ? `${plan.healthCheck.type ?? 'http'} ${plan.healthCheck.path ?? '/'}` : '—'}
                </span>
              </div>
            </div>
          </section>

          <section className="card" aria-label="Execution sequence">
            <h2 className="section-title">Execution sequence</h2>
            <PlanSequence steps={plan.steps ?? []} />
          </section>

          {plan.steps && plan.steps.length > 0 && (
            <section className="card" aria-label="Step explanations">
              <h2 className="section-title">What each step does</h2>
              <div className="data-list">
                {plan.steps.map((code) => (
                  <div className="data-list__row" key={code}>
                    <div className="data-list__main">
                      <div className="data-list__title">{stepLabel(code)}</div>
                      <div className="data-list__sub">{stepExplanation(code)}</div>
                    </div>
                  </div>
                ))}
              </div>
            </section>
          )}
        </>
      )}
    </div>
  )
}

function stepExplanation(code: string): string {
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
