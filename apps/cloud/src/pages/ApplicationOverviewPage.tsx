import { useParams } from 'react-router-dom'
import { getApplication } from '../api/resources'
import { Placeholder } from '../components/Placeholder'
import { PageHeader } from '../components/shell/PageHeader'
import { ErrorPanel, SkeletonLines } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { routes } from '../routes/builders'
import { ForbiddenPage } from './ForbiddenPage'
import { NotFoundPage } from './NotFoundPage'

/**
 * Application Overview placeholder (task lane route `/applications/:id`).
 * The canonical navigation route is `/apps/:applicationId/:environment/overview`;
 * both resolve here until #120 wires the environment-aware screen.
 */
export function ApplicationOverviewPage() {
  const { applicationId } = useParams<{ applicationId: string }>()
  const state = useAsync(
    (signal) => (applicationId ? getApplication(applicationId, { signal }) : Promise.reject(new Error('missing id'))),
    [applicationId],
  )

  // Page-primary resource 403/404 render as full-page system states.
  if (state.status === 'error' && state.error?.isForbidden) return <ForbiddenPage />
  if (state.status === 'error' && state.error?.isNotFound) return <NotFoundPage />

  return (
    <>
      <PageHeader
        breadcrumbs={[
          { label: 'Workspace', to: routes.dashboard() },
          { label: state.data?.name ?? applicationId ?? 'Application', current: true },
        ]}
        title={state.data?.name ?? 'Application overview'}
      />
      <div className="content__body stack">
        {state.status === 'loading' && <SkeletonLines lines={3} />}
        {state.status === 'error' && state.error && (
          <ErrorPanel error={state.error} objectName="this application" onRetry={state.reload} />
        )}
        {state.status === 'success' && state.data && (
          <div className="card">
            <div className="muted">Application</div>
            <div className="data-list__title">{state.data.name}</div>
            <div className="mono muted">{state.data.id}</div>
          </div>
        )}

        <Placeholder
          params={{ applicationId }}
          resources={[
            'GET /api/v1/applications/{applicationId}',
            'GET /api/v1/applications/{applicationId}/deployments',
            'GET /api/v1/servers',
          ]}
          note="Environment context (Production/Staging/Preview) is a contract gap tracked by #71 / #117; the environment chip and per-environment views render once the API exposes a per-deployment environment field."
        />
      </div>
    </>
  )
}
