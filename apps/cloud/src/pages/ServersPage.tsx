import { Link } from 'react-router-dom'
import { listServers } from '../api/resources'
import { PageHeader } from '../components/shell/PageHeader'
import { StatusPill } from '../components/StatusPill'
import { EmptyState, ErrorPanel, SkeletonRows } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { routes } from '../routes/builders'

export function ServersPage() {
  const state = useAsync((signal) => listServers({}, { signal }), [])
  const servers = state.data ?? []

  return (
    <>
      <PageHeader breadcrumbs={[{ label: 'Workspace' }, { label: 'Servers', current: true }]} title="Servers" />
      <div className="content__body stack">
        {state.status === 'loading' && <SkeletonRows count={3} />}

        {state.status === 'error' && state.error && (
          <ErrorPanel error={state.error} objectName="your servers" onRetry={state.reload} />
        )}

        {state.status === 'success' && servers.length === 0 && (
          <EmptyState
            variant="first-use"
            title="No servers registered"
            description="Register a server so the Runtime Agent can bind to it and receive deployments."
          />
        )}

        {state.status === 'success' && servers.length > 0 && (
          <div className="data-list">
            {servers.map((server) => (
              <Link className="data-list__row" key={server.id} to={routes.server(server.id)}>
                <div className="data-list__main">
                  <div className="data-list__title">{server.name}</div>
                  <div className="data-list__sub mono">{server.address ?? server.id}</div>
                </div>
                <StatusPill status={server.status} />
              </Link>
            ))}
          </div>
        )}
      </div>
    </>
  )
}
