import { Link, useParams } from 'react-router-dom'
import { getRepository } from '../api/resources'
import { PageHeader } from '../components/shell/PageHeader'
import { ErrorPanel, SkeletonLines } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { routes } from '../routes/builders'

export function RepositoryDetailPage() {
  const { repositoryId } = useParams<{ repositoryId: string }>()
  const state = useAsync(
    (signal) => (repositoryId ? getRepository(repositoryId, { signal }) : Promise.reject(new Error('missing id'))),
    [repositoryId],
  )

  return (
    <>
      <PageHeader
        breadcrumbs={[
          { label: 'Workspace' },
          { label: 'Repositories', to: routes.repositories() },
          { label: state.data?.fullName ?? repositoryId ?? 'Repository', current: true },
        ]}
        title={state.data?.fullName ?? 'Repository'}
        actions={
          <Link className="btn btn--ghost" to={routes.repositories()}>
            Back to repositories
          </Link>
        }
      />
      <div className="content__body">
        {state.status === 'loading' && <SkeletonLines lines={4} />}
        {state.status === 'error' && state.error && (
          <ErrorPanel error={state.error} objectName="this repository" onRetry={state.reload} />
        )}
        {state.status === 'success' && state.data && (
          <div className="card stack">
            <div>
              <div className="muted">Full name</div>
              <div className="mono">{state.data.fullName}</div>
            </div>
            <div>
              <div className="muted">Default branch</div>
              <div className="mono">{state.data.defaultBranch ?? '—'}</div>
            </div>
            <div>
              <div className="muted">Repository ID</div>
              <div className="mono">{state.data.id}</div>
            </div>
            <p className="muted">Analysis and application setup are wired in #120.</p>
          </div>
        )}
      </div>
    </>
  )
}
