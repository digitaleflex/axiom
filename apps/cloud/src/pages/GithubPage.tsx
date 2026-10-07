import { useState } from 'react'
import { ApiError } from '../api/errors'
import { disconnectGithubConnection, listGithubConnections, startGithubConnect } from '../api/resources'
import { PageHeader } from '../components/shell/PageHeader'
import { StatusPill } from '../components/StatusPill'
import { EmptyState, ErrorPanel, InlineNotice, SkeletonRows, useToast } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { routes } from '../routes/builders'

export function GithubPage() {
  const { push } = useToast()
  const state = useAsync((signal) => listGithubConnections({ signal }), [])
  const [connecting, setConnecting] = useState(false)
  const [connectError, setConnectError] = useState<ApiError | null>(null)

  const connect = async () => {
    setConnecting(true)
    setConnectError(null)
    try {
      const { authorizeUrl } = await startGithubConnect()
      window.location.assign(authorizeUrl)
    } catch (cause) {
      setConnectError(cause instanceof ApiError ? cause : ApiError.network())
      setConnecting(false)
    }
  }

  const disconnect = async (connectionId: string) => {
    try {
      await disconnectGithubConnection(connectionId)
      push({ variant: 'success', title: 'GitHub disconnected' })
      state.reload()
    } catch (cause) {
      const error = cause instanceof ApiError ? cause : ApiError.network()
      push({ variant: 'failure', title: "Couldn't disconnect GitHub", body: error.message })
    }
  }

  const connections = state.data ?? []

  return (
    <>
      <PageHeader
        breadcrumbs={[{ label: 'Workspace' }, { label: 'GitHub', current: true }]}
        title="GitHub"
        actions={
          <button className="btn btn--primary" type="button" onClick={() => void connect()} disabled={connecting}>
            {connecting ? 'Redirecting…' : 'Connect GitHub'}
          </button>
        }
      />
      <div className="content__body stack">
        {connectError?.isUnavailable && (
          <InlineNotice variant="warning" title="GitHub is not configured on this Engine">
            The Engine returned <span className="mono">SERVICE_UNAVAILABLE</span> (503). Ask an operator to configure the
            GitHub App before connecting.
          </InlineNotice>
        )}
        {connectError && !connectError.isUnavailable && (
          <InlineNotice variant="failed" title="Couldn't start the GitHub connection">
            {connectError.message}
          </InlineNotice>
        )}

        {state.status === 'loading' && <SkeletonRows count={2} />}

        {state.status === 'error' && state.error && (
          <ErrorPanel error={state.error} objectName="your GitHub connections" onRetry={state.reload} />
        )}

        {state.status === 'success' && connections.length === 0 && (
          <EmptyState
            variant="first-use"
            title="No GitHub account connected"
            description="Connect a GitHub account to browse repositories and deploy from them."
            primaryAction={
              <button className="btn btn--primary" type="button" onClick={() => void connect()} disabled={connecting}>
                Connect GitHub
              </button>
            }
          />
        )}

        {state.status === 'success' && connections.length > 0 && (
          <div className="data-list">
            {connections.map((connection) => (
              <div className="data-list__row" key={connection.id}>
                <div className="data-list__main">
                  <div className="data-list__title">{connection.accountLogin}</div>
                  <div className="data-list__sub">
                    {connection.accountType} · <span className="mono">{connection.id}</span>
                  </div>
                </div>
                <StatusPill status={connection.status} />
                <button className="btn btn--ghost" type="button" onClick={() => void disconnect(connection.id)}>
                  Disconnect
                </button>
              </div>
            ))}
          </div>
        )}

        <p className="muted">
          After connecting, browse repositories in <a href={routes.repositories()}>Repositories</a>.
        </p>
      </div>
    </>
  )
}
