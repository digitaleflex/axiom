import { Link, useParams } from 'react-router-dom'
import { getDeployment, getDeploymentHealth, listLogs, listSteps } from '../api/resources'
import type { LogEntry } from '../api/types'
import { LogViewer } from '../components/LogViewer'
import { PlanSequence } from '../components/PlanSequence'
import { TechnicalValue } from '../components/TechnicalValue'
import { ErrorPanel, SkeletonLines } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { routes } from '../routes/builders'
import { deploymentErrorCopy } from '../workflow/errorCopy'
import { stepLabel } from '../workflow/planSteps'

function formatDuration(startedAt?: string, completedAt?: string): string | null {
  if (!startedAt || !completedAt) return null
  const start = Date.parse(startedAt)
  const end = Date.parse(completedAt)
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) return null
  const seconds = Math.round((end - start) / 1000)
  const m = Math.floor(seconds / 60)
  const s = seconds % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

/**
 * Deployment Summary tab content (deployment-success §1, deployment-failure §1).
 *
 * LIVE: proves health and exposes the live URL. FAILED: names the failed
 * step, a concise cause from the Engine error code, the impact line, and
 * actionable next steps (retry / edit configuration / logs).
 */
export function DeploymentSummaryContent() {
  const { applicationId, environment, deploymentId } = useParams<{
    applicationId: string
    environment: string
    deploymentId: string
  }>()
  const deploymentState = useAsync(
    (signal) => (deploymentId ? getDeployment(deploymentId, { signal }) : Promise.reject(new Error('missing id'))),
    [deploymentId],
  )

  const healthState = useAsync(
    (signal) => (deploymentId ? getDeploymentHealth(deploymentId, { signal }) : Promise.reject(new Error('missing id'))),
    [deploymentId],
  )

  const stepsState = useAsync(
    (signal) => (deploymentId ? listSteps(deploymentId, { signal }) : Promise.reject(new Error('missing id'))),
    [deploymentId],
  )

  const logsState = useAsync<{ items: LogEntry[] } | null>(
    (signal) =>
      deploymentId
        ? listLogs(deploymentId, { level: 'error', limit: 50 }, { signal }).then((r) => ({ items: r.items }))
        : Promise.reject(new Error('missing id')),
    [deploymentId],
  )

  const deployment = deploymentState.data
  const status = deployment?.status
  const live = status === 'LIVE'
  const failed = status === 'FAILED'
  const health = healthState.data
  const steps = stepsState.data ?? []
  const failedStep = steps.find((s) => s.status === 'FAILED')
  const errorCopy = deployment?.errorCode ? deploymentErrorCopy(deployment.errorCode) : null
  const duration = formatDuration(deployment?.startedAt, deployment?.completedAt)

  return (
    <div className="content__body stack">
      {deploymentState.status === 'loading' && <SkeletonLines lines={6} />}

      {deploymentState.status === 'error' && deploymentState.error && (
        <ErrorPanel error={deploymentState.error} objectName="this deployment" onRetry={deploymentState.reload} />
      )}

      {deployment && live && (
        <section className="card" aria-label="Deployment result">
          <h2 className="section-title">
            <span aria-hidden="true">● </span>Live
          </h2>
          {deployment?.url ? (
            <p style={{ margin: '4px 0 0' }}>
              <a href={deployment.url} target="_blank" rel="noreferrer" className="mono" style={{ fontSize: 'var(--text-xl)' }}>
                {deployment.url}
              </a>
            </p>
          ) : (
            <p className="muted">This deployment is live.</p>
          )}
          {health?.status && (
            <p className="muted" style={{ marginTop: 8 }}>
              Health check {health.status === 'HEALTHY' ? 'passed' : health.status === 'UNHEALTHY' ? 'failing' : 'unknown'}
              {health.http?.statusCode ? ` · GET ${health.http.statusCode}` : ''}
              {health.http?.latencyMs !== undefined ? ` in ${health.http.latencyMs} ms` : ''}
              {health.checkedAt ? ` · checked ${health.checkedAt}` : ''}
            </p>
          )}
          {healthState.status === 'error' && (
            <p className="muted">Health result unavailable — never an implied pass.</p>
          )}
        </section>
      )}

      {deployment && failed && (
        <section className="card" aria-label="Failure">
          <h2 className="section-title">
            <span aria-hidden="true">⬣ </span>Failed
          </h2>
          <p style={{ margin: '4px 0 0' }}>
            Stopped at <strong>{failedStep ? stepLabel(failedStep.name) : 'unknown step'}</strong>
          </p>
          {errorCopy && <p style={{ margin: '4px 0 0' }}>{errorCopy.explanation}</p>}
          {deployment.errorCode && (
            <p className="muted" style={{ marginTop: 4 }}>
              Error code: <span className="mono">{deployment.errorCode}</span>
            </p>
          )}
          {failedStep?.completedAt && (
            <p className="muted">
              <span className="mono">{failedStep.completedAt}</span>
            </p>
          )}
          <p className="muted" style={{ marginTop: 8 }}>
            The current deployment keeps serving if one was live before this attempt.
          </p>
        </section>
      )}

      {deployment && failed && errorCopy && errorCopy.checklist.length > 0 && (
        <section className="card" aria-label="What to check">
          <h2 className="section-title">What to check</h2>
          <ul style={{ margin: 0, paddingLeft: 20 }}>
            {errorCopy.checklist.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
          <div className="row" style={{ marginTop: 12 }}>
            <Link
              className="btn btn--secondary"
              to={routes.setup({ applicationId: applicationId ?? '', step: 'configure' })}
            >
              Edit configuration
            </Link>
            <Link className="btn btn--ghost" to={routes.setup({ applicationId: applicationId ?? '', step: 'profile' })}>
              Edit profile
            </Link>
          </div>
        </section>
      )}

      {deployment && steps.length > 0 && (
        <section className="card" aria-label="Sequence">
          <h2 className="section-title">Sequence</h2>
          <PlanSequence steps={steps.map((s) => s.name)} stepData={steps} expandFailed />
        </section>
      )}

      {deployment && failed && logsState.data && logsState.data.items.length > 0 && (
        <section className="card" aria-label="Relevant logs">
          <h2 className="section-title">Relevant logs</h2>
          <LogViewer lines={logsState.data.items} />
          <Link
            className="btn btn--ghost"
            to={routes.deployment({
              applicationId: applicationId ?? '',
              environment: (environment as 'production') ?? 'production',
              deploymentId: deployment.id,
              tab: 'logs',
              query: { severity: 'error' },
            })}
          >
            Open full logs
          </Link>
        </section>
      )}

      {deployment && (
        <section className="card" aria-label="Deployment details">
          <h2 className="section-title">Details</h2>
          <div className="data-list">
            <div className="data-list__row">
              <div className="data-list__main">
                <div className="data-list__title">Deployment ID</div>
              </div>
              <TechnicalValue value={deployment.id} />
            </div>
            {duration && (
              <div className="data-list__row">
                <div className="data-list__main">
                  <div className="data-list__title">Duration</div>
                </div>
                <span className="mono">{duration}</span>
              </div>
            )}
            {deployment.serverId && (
              <div className="data-list__row">
                <div className="data-list__main">
                  <div className="data-list__title">Server</div>
                </div>
                <span className="mono">{deployment.serverId}</span>
              </div>
            )}
          </div>
        </section>
      )}
    </div>
  )
}
