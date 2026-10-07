import { useParams } from 'react-router-dom'
import { getDeployment } from '../api/resources'
import { Placeholder } from '../components/Placeholder'
import { PageHeader } from '../components/shell/PageHeader'
import { StatusPill } from '../components/StatusPill'
import { ErrorPanel, SkeletonLines } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { routes } from '../routes/builders'
import { ForbiddenPage } from './ForbiddenPage'
import { NotFoundPage } from './NotFoundPage'

/**
 * Deployment Progress placeholder (task lane route `/deployments/:id`).
 * Canonical form is `/apps/:applicationId/:environment/deployments/:deploymentId/progress`.
 */
export function DeploymentProgressPage() {
  const { deploymentId } = useParams<{ deploymentId: string }>()
  const state = useAsync(
    (signal) => (deploymentId ? getDeployment(deploymentId, { signal }) : Promise.reject(new Error('missing id'))),
    [deploymentId],
  )

  if (state.status === 'error' && state.error?.isForbidden) return <ForbiddenPage />
  if (state.status === 'error' && state.error?.isNotFound) return <NotFoundPage />

  return (
    <>
      <PageHeader
        breadcrumbs={[
          { label: 'Workspace', to: routes.dashboard() },
          { label: 'Deployment', to: routes.dashboard() },
          { label: deploymentId ?? 'Deployment', current: true },
        ]}
        title="Deployment progress"
        actions={state.data ? <StatusPill status={state.data.status} /> : undefined}
      />
      <div className="content__body stack">
        {state.status === 'loading' && <SkeletonLines lines={4} />}
        {state.status === 'error' && state.error && (
          <ErrorPanel error={state.error} objectName="this deployment" onRetry={state.reload} />
        )}
        {state.status === 'success' && state.data && (
          <div className="card">
            <div className="muted">Current status</div>
            <div className="row">
              <StatusPill status={state.data.status} />
              <span className="mono muted">{state.data.id}</span>
            </div>
          </div>
        )}

        <Placeholder
          params={{ deploymentId }}
          resources={[
            'GET /api/v1/deployments/{deploymentId}',
            'GET /api/v1/deployments/{deploymentId}/events/stream (SSE, Last-Event-ID resume)',
          ]}
          note="The step timeline, cancel action and live stream rendering are wired in #120. The SSE reader (with Last-Event-ID and ?lastEventId= resume) is ready in src/api/sse.ts."
        />
      </div>
    </>
  )
}
