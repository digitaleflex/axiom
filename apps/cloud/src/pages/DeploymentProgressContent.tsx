import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { cancelDeployment, listLogs } from '../api/resources'
import type { LogEntry } from '../api/types'
import { ApiError } from '../api/errors'
import { LogViewer } from '../components/LogViewer'
import { PlanSequence } from '../components/PlanSequence'
import { StepIndicator } from '../components/PlanSequence'
import { TechnicalValue } from '../components/TechnicalValue'
import { ErrorPanel, InlineNotice, SkeletonLines } from '../components/system'
import { useDeploymentProgress } from '../hooks/useDeploymentProgress'
import { routes } from '../routes/builders'
import { isTerminalStatus, normalizeStepState, stepsActiveForStatus } from '../workflow/stepMapping'
import { stepLabel } from '../workflow/planSteps'

function formatElapsed(startedAt?: string, completedAt?: string): string | null {
  if (!startedAt) return null
  const start = Date.parse(startedAt)
  const end = completedAt ? Date.parse(completedAt) : Date.now()
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) return null
  const seconds = Math.round((end - start) / 1000)
  const m = Math.floor(seconds / 60)
  const s = seconds % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

/**
 * Deployment Progress tab content (deployment-progress §1). Rendered inside
 * the deployment tab chrome. Dominant element: the current step. Cancel where
 * the Engine permits. On terminal state the tab stays and shows the banner.
 */
export function DeploymentProgressContent() {
  const { applicationId, environment, deploymentId } = useParams<{
    applicationId: string
    environment: string
    deploymentId: string
  }>()
  const [cancelling, setCancelling] = useState(false)
  const [cancelError, setCancelError] = useState<ApiError | null>(null)
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [logsLoading, setLogsLoading] = useState(false)

  const progress = useDeploymentProgress(deploymentId)
  const { deployment, steps, status, connection } = progress

  const loadLogs = async () => {
    if (!deploymentId) return
    setLogsLoading(true)
    try {
      const result = await listLogs(deploymentId, { limit: 50 })
      setLogs(result.items)
    } catch {
      setLogs([])
    } finally {
      setLogsLoading(false)
    }
  }

  const onCancel = async () => {
    if (!deploymentId) return
    setCancelling(true)
    setCancelError(null)
    try {
      await cancelDeployment(deploymentId)
      progress.reload()
    } catch (cause) {
      const error = cause instanceof ApiError ? cause : ApiError.network()
      setCancelError(error)
    } finally {
      setCancelling(false)
    }
  }

  const terminal = isTerminalStatus(status)
  const activeSteps = stepsActiveForStatus(status)
  const currentStep = steps.find((s) => s.status === 'RUNNING') ?? steps.find((s) => activeSteps.includes(s.name))
  const elapsed = formatElapsed(deployment?.startedAt, deployment?.completedAt)
  const cancellable = !terminal && status !== 'VERIFYING'

  return (
    <div className="content__body stack">
      {connection === 'reconnecting' && (
        <InlineNotice variant="warning" title="Reconnecting…">
          Live updates paused — keeping the last known state.
        </InlineNotice>
      )}
      {connection === 'polling' && (
        <InlineNotice variant="warning" title="Live updates paused">
          Refreshing every 5s.
        </InlineNotice>
      )}

      {progress.error && (
        <ErrorPanel
          error={progress.error instanceof ApiError ? progress.error : ApiError.network()}
          objectName="this deployment"
          onRetry={progress.reload}
        />
      )}

      {cancelError && (
        <InlineNotice variant="failed" title="Couldn't cancel this deployment">
          {cancelError.message}
        </InlineNotice>
      )}

      {!progress.error && !deployment && <SkeletonLines lines={6} />}

      {deployment && (
        <>
          <section className="card" aria-label="Current step">
            <div className="row" style={{ justifyContent: 'space-between' }}>
              <h2 className="section-title">
                {currentStep ? stepLabel(currentStep.name) : status === 'PENDING' ? 'Queued' : 'Deployment'}
              </h2>
              {elapsed && <span className="mono muted">{elapsed}</span>}
            </div>
            {currentStep ? (
              <div>
                <StepIndicator state={normalizeStepState(currentStep.status)} />
                <p style={{ margin: '4px 0 0' }} className="muted">
                  Step {steps.findIndex((s) => s.name === currentStep.name) + 1} of {steps.length}
                </p>
              </div>
            ) : (
              <p className="muted">
                {status === 'PENDING' && 'Waiting to start.'}
                {status === 'ANALYZING' && 'Checking the repository.'}
                {status === 'PLANNING' && 'Preparing the execution plan.'}
                {status === 'LIVE' && 'This deployment is live.'}
                {status === 'FAILED' && 'This deployment failed.'}
                {status === 'CANCELLED' && 'This deployment was cancelled.'}
                {!status && 'Waiting for the first status event.'}
              </p>
            )}
          </section>

          <div className="grid grid--cards">
            <section className="card" aria-label="Sequence">
              <h2 className="section-title">Sequence</h2>
              <PlanSequence steps={steps.map((s) => s.name)} stepData={steps} />
            </section>

            <section className="card" aria-label="Live events">
              <div className="row" style={{ justifyContent: 'space-between' }}>
                <h2 className="section-title">Events / Logs</h2>
                <button type="button" className="btn btn--ghost btn--sm" onClick={() => void loadLogs()}>
                  Refresh logs
                </button>
              </div>
              <LogViewer lines={logs} loading={logsLoading} />
            </section>
          </div>

          {terminal && (
            <InlineNotice
              variant={status === 'LIVE' ? 'success' : status === 'FAILED' ? 'failed' : 'info'}
              title={
                status === 'LIVE'
                  ? 'This deployment is live.'
                  : status === 'FAILED'
                    ? 'This deployment failed.'
                    : 'This deployment was cancelled.'
              }
            >
              <Link
                className="btn btn--secondary"
                to={routes.deployment({
                  applicationId: applicationId ?? '',
                  environment: (environment as 'production') ?? 'production',
                  deploymentId: deployment.id,
                  tab: 'summary',
                })}
              >
                View summary
              </Link>
            </InlineNotice>
          )}

          {cancellable && (
            <button type="button" className="btn btn--destructive" onClick={() => void onCancel()} disabled={cancelling}>
              {cancelling ? 'Cancelling…' : 'Cancel'}
            </button>
          )}

          <p className="muted">
            <TechnicalValue value={deployment.id} />
            {deployment.serverId ? ` · server ${deployment.serverId}` : ''}
          </p>
        </>
      )}
    </div>
  )
}
