import { useParams } from 'react-router-dom'
import { getDeploymentHealth } from '../api/resources'
import { ErrorPanel, InlineNotice, SkeletonLines } from '../components/system'
import { useAsync } from '../hooks/useAsync'

/**
 * Deployment Health tab content (health §1). Shows the verification outcome
 * known to the Engine — persisted probe details when verification ran,
 * otherwise the authoritative state. UNKNOWN is never an implied pass.
 */
export function DeploymentHealthContent() {
  const { deploymentId } = useParams<{ deploymentId: string }>()

  const healthState = useAsync(
    (signal) => (deploymentId ? getDeploymentHealth(deploymentId, { signal }) : Promise.reject(new Error('missing id'))),
    [deploymentId],
  )

  const health = healthState.data

  return (
    <div className="content__body stack">
      {healthState.status === 'loading' && <SkeletonLines lines={4} />}

      {healthState.status === 'error' && healthState.error && (
        <ErrorPanel error={healthState.error} objectName="deployment health" onRetry={healthState.reload} />
      )}

      {healthState.status === 'success' && !health && (
        <InlineNotice variant="info" title="No health data">
          The Engine has not recorded a health result for this deployment.
        </InlineNotice>
      )}

      {health && (
        <section className="card" aria-label="Health result">
          <h2 className="section-title">Health</h2>
          <div className="data-list">
            <div className="data-list__row">
              <div className="data-list__main">
                <div className="data-list__title">Status</div>
              </div>
              <span className="mono">{health.status ?? 'UNKNOWN'}</span>
            </div>
            {health.deploymentStatus && (
              <div className="data-list__row">
                <div className="data-list__main">
                  <div className="data-list__title">Deployment status</div>
                </div>
                <span className="mono">{health.deploymentStatus}</span>
              </div>
            )}
            {health.http?.statusCode !== undefined && (
              <div className="data-list__row">
                <div className="data-list__main">
                  <div className="data-list__title">HTTP status</div>
                </div>
                <span className="mono">{health.http.statusCode}</span>
              </div>
            )}
            {health.http?.latencyMs !== undefined && (
              <div className="data-list__row">
                <div className="data-list__main">
                  <div className="data-list__title">Latency</div>
                </div>
                <span className="mono">{health.http.latencyMs} ms</span>
              </div>
            )}
            {health.attempt !== undefined && (
              <div className="data-list__row">
                <div className="data-list__main">
                  <div className="data-list__title">Attempt</div>
                </div>
                <span className="mono">{health.attempt}</span>
              </div>
            )}
            {health.checkedAt && (
              <div className="data-list__row">
                <div className="data-list__main">
                  <div className="data-list__title">Checked at</div>
                </div>
                <span className="mono">{health.checkedAt}</span>
              </div>
            )}
          </div>
          {health.status === 'UNKNOWN' && (
            <p className="muted" style={{ marginTop: 8 }}>
              No probe data — this is not an implied pass.
            </p>
          )}
        </section>
      )}
    </div>
  )
}
