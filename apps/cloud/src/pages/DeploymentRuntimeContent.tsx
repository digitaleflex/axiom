import { useParams } from 'react-router-dom'
import { getDeployment } from '../api/resources'
import { InlineNotice, SkeletonLines } from '../components/system'
import { useAsync } from '../hooks/useAsync'

/**
 * Deployment Runtime tab content. Runtime details (container state) arrive
 * with the Runtime Agent (#85) — until then this shows the deployment's
 * runtime-affecting fields from the deployment resource.
 */
export function DeploymentRuntimeContent() {
  const { deploymentId } = useParams<{ deploymentId: string }>()

  const deploymentState = useAsync(
    (signal) => (deploymentId ? getDeployment(deploymentId, { signal }) : Promise.reject(new Error('missing id'))),
    [deploymentId],
  )

  const deployment = deploymentState.data

  return (
    <div className="content__body stack">
      {deploymentState.status === 'loading' && <SkeletonLines lines={4} />}

      {deploymentState.status === 'success' && deployment && (
        <section className="card" aria-label="Runtime">
          <h2 className="section-title">Runtime</h2>
          <div className="data-list">
            <div className="data-list__row">
              <div className="data-list__main">
                <div className="data-list__title">Server</div>
              </div>
              <span className="mono">{deployment.serverId ?? '—'}</span>
            </div>
            <div className="data-list__row">
              <div className="data-list__main">
                <div className="data-list__title">Status</div>
              </div>
              <span className="mono">{deployment.status ?? '—'}</span>
            </div>
          </div>
          <p className="muted" style={{ marginTop: 8 }}>
            Container runtime details arrive with the Runtime Agent (#85).
          </p>
        </section>
      )}

      {deploymentState.status === 'success' && !deployment && (
        <InlineNotice variant="info" title="No deployment data">
          The Engine has not recorded runtime data for this deployment.
        </InlineNotice>
      )}
    </div>
  )
}
