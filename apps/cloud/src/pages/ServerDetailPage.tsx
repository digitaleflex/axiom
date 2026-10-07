import { Link, useParams } from 'react-router-dom'
import { getServer } from '../api/resources'
import { PageHeader } from '../components/shell/PageHeader'
import { StatusPill } from '../components/StatusPill'
import { ErrorPanel, SkeletonLines } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { routes } from '../routes/builders'

export function ServerDetailPage() {
  const { serverId } = useParams<{ serverId: string }>()
  const state = useAsync(
    (signal) => (serverId ? getServer(serverId, { signal }) : Promise.reject(new Error('missing id'))),
    [serverId],
  )

  return (
    <>
      <PageHeader
        breadcrumbs={[
          { label: 'Workspace' },
          { label: 'Servers', to: routes.servers() },
          { label: serverId ?? 'Server', current: true },
        ]}
        title={state.data?.name ?? 'Server'}
        actions={
          <Link className="btn btn--ghost" to={routes.servers()}>
            Back to servers
          </Link>
        }
      />
      <div className="content__body">
        {state.status === 'loading' && <SkeletonLines lines={4} />}
        {state.status === 'error' && state.error && (
          <ErrorPanel error={state.error} objectName="this server" onRetry={state.reload} />
        )}
        {state.status === 'success' && state.data && (
          <div className="card stack">
            <div className="row">
              <span className="muted">Status</span>
              <StatusPill status={state.data.status} />
            </div>
            <div>
              <div className="muted">Address</div>
              <div className="mono">{state.data.address ?? '—'}</div>
            </div>
            <div>
              <div className="muted">Server ID</div>
              <div className="mono">{state.data.id}</div>
            </div>
            {state.data.lastSeenAt && (
              <div>
                <div className="muted">Last seen</div>
                <div className="mono">{state.data.lastSeenAt}</div>
              </div>
            )}
          </div>
        )}
      </div>
    </>
  )
}
