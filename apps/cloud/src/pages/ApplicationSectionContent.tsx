import { useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { getDeploymentHealth, listDeployments, listDomains, listLogs } from '../api/resources'
import { LogViewer } from '../components/LogViewer'
import { StatusPill } from '../components/StatusPill'
import { EmptyState, ErrorPanel, InlineNotice, SkeletonLines } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { routes, type ApplicationSection } from '../routes/builders'

/**
 * Application section content (application §2, logs §1, metrics §1, domains §1).
 * Renders the real content for each application section tab.
 */
export function ApplicationSectionContent({ section }: { section: ApplicationSection }) {
  const { applicationId, environment } = useParams<{ applicationId: string; environment: string }>()

  const deploymentsState = useAsync(
    (signal) =>
      applicationId
        ? listDeployments(applicationId, { environment, limit: 20 }, { signal })
        : Promise.reject(new Error('missing id')),
    [applicationId, environment],
  )

  const domainsState = useAsync(
    (signal) => (applicationId ? listDomains(applicationId, environment, { signal }) : Promise.reject(new Error('missing id'))),
    [applicationId, environment],
  )

  const [statusFilter, setStatusFilter] = useState('')

  const deployments = deploymentsState.data?.items ?? []
  const filteredDeployments = statusFilter
    ? deployments.filter((d) => d.status === statusFilter)
    : deployments
  const domains = domainsState.data ?? []
  const currentDeployment = deployments.find((d) => d.status === 'LIVE') ?? deployments[0]

  if (section === 'deployments') {
    return (
      <div className="content__body stack">
        {deploymentsState.status === 'loading' && <SkeletonLines lines={6} />}

        {deploymentsState.status === 'error' && deploymentsState.error && (
          <ErrorPanel error={deploymentsState.error} objectName="deployments" onRetry={deploymentsState.reload} />
        )}

        {deploymentsState.status === 'success' && deployments.length === 0 && (
          <EmptyState
            variant="no-results"
            title="No deployments yet"
            description="Deploy this application to see its deployment history."
            primaryAction={
              <Link className="btn btn--primary" to={routes.setup({ applicationId: applicationId ?? '', step: 'configure' })}>
                Deploy
              </Link>
            }
          />
        )}

        {deploymentsState.status === 'success' && deployments.length > 0 && (
          <>
            <div className="row" style={{ gap: 8, alignItems: 'center' }}>
              <label htmlFor="status-filter" className="muted">
                Status
              </label>
              <select
                id="status-filter"
                className="field__input"
                style={{ width: 'auto' }}
                value={statusFilter}
                onChange={(event) => setStatusFilter(event.target.value)}
              >
                <option value="">All</option>
                <option value="LIVE">Live</option>
                <option value="FAILED">Failed</option>
                <option value="BUILDING">Building</option>
                <option value="DEPLOYING">Deploying</option>
                <option value="VERIFYING">Verifying</option>
                <option value="CANCELLED">Cancelled</option>
              </select>
            </div>

            {filteredDeployments.length === 0 ? (
              <InlineNotice variant="info" title="No deployments match this filter">
                Try a different status filter.
              </InlineNotice>
            ) : (
              <div className="data-list">
                {filteredDeployments.map((dep) => (
                  <Link
                    className="data-list__row"
                    key={dep.id}
                    to={routes.deployment({
                      applicationId: applicationId ?? '',
                      environment: environment as 'production',
                      deploymentId: dep.id,
                      tab: 'summary',
                    })}
                  >
                    <div className="data-list__main">
                      <div className="data-list__title">
                        #{dep.number ?? dep.id} <StatusPill status={dep.status} />
                      </div>
                      <div className="data-list__sub mono">
                        {dep.createdAt ?? ''}
                        {dep.createdBy ? ` · by ${dep.createdBy}` : ''}
                        {dep.url ? ` · ${dep.url}` : ''}
                      </div>
                    </div>
                  </Link>
                ))}
              </div>
            )}
          </>
        )}
      </div>
    )
  }

  if (section === 'logs') {
    return <ApplicationLogsContent deploymentId={currentDeployment?.id} />
  }

  if (section === 'metrics') {
    return <ApplicationMetricsContent deploymentId={currentDeployment?.id} />
  }

  if (section === 'domains') {
    return (
      <div className="content__body stack">
        {domainsState.status === 'loading' && <SkeletonLines lines={4} />}

        {domainsState.status === 'error' && domainsState.error && (
          <ErrorPanel error={domainsState.error} objectName="domains" onRetry={domainsState.reload} />
        )}

        {domainsState.status === 'success' && domains.length === 0 && (
          <EmptyState
            variant="no-results"
            title="No domains configured"
            description="Add a domain to route traffic to this application."
          />
        )}

        {domainsState.status === 'success' && domains.length > 0 && (
          <div className="data-list">
            {domains.map((domain) => (
              <div className="data-list__row" key={domain.id}>
                <div className="data-list__main">
                  <div className="data-list__title mono">{domain.hostname ?? '—'}</div>
                  <div className="data-list__sub">
                    {domain.isPrimary ? 'Primary' : 'Secondary'}
                    {domain.tlsStatus ? ` · TLS ${domain.tlsStatus}` : ''}
                    {domain.routingStatus ? ` · Routing ${domain.routingStatus}` : ''}
                    {domain.dnsStatus ? ` · DNS ${domain.dnsStatus}` : ''}
                  </div>
                </div>
                <StatusPill status={domain.tlsStatus === 'valid' ? 'LIVE' : domain.dnsStatus === 'ok' ? 'LIVE' : 'unknown'} />
              </div>
            ))}
          </div>
        )}
      </div>
    )
  }

  return null
}

function ApplicationLogsContent({ deploymentId }: { deploymentId?: string }) {
  const [severity, setSeverity] = useState('')

  const logsState = useAsync(
    (signal) =>
      deploymentId
        ? listLogs(deploymentId, { level: severity || undefined, limit: 200 }, { signal })
        : Promise.reject(new Error('no deployment')),
    [deploymentId, severity],
  )

  if (!deploymentId) {
    return (
      <div className="content__body stack">
        <EmptyState
          variant="no-results"
          title="No deployment to show logs for"
          description="Deploy this application to see its logs."
        />
      </div>
    )
  }

  const logs = logsState.data?.items ?? []

  return (
    <div className="content__body stack">
      <div className="row" style={{ gap: 8, alignItems: 'center' }}>
        <label htmlFor="app-log-severity" className="muted">
          Severity
        </label>
        <select
          id="app-log-severity"
          className="field__input"
          style={{ width: 'auto' }}
          value={severity}
          onChange={(event) => setSeverity(event.target.value)}
        >
          <option value="">All</option>
          <option value="debug">Debug and above</option>
          <option value="info">Info and above</option>
          <option value="warn">Warn and above</option>
          <option value="error">Error only</option>
        </select>
        <button type="button" className="btn btn--ghost btn--sm" onClick={logsState.reload}>
          Refresh
        </button>
      </div>

      {logsState.status === 'loading' && <SkeletonLines lines={6} />}

      {logsState.status === 'error' && logsState.error && (
        <ErrorPanel error={logsState.error} objectName="logs" onRetry={logsState.reload} />
      )}

      {logsState.status === 'success' && logs.length === 0 && (
        <InlineNotice variant="info" title="No log lines">
          No log lines match the current filters.
        </InlineNotice>
      )}

      {logsState.status === 'success' && logs.length > 0 && <LogViewer lines={logs} />}
    </div>
  )
}

function ApplicationMetricsContent({ deploymentId }: { deploymentId?: string }) {
  const healthState = useAsync(
    (signal) => (deploymentId ? getDeploymentHealth(deploymentId, { signal }) : Promise.reject(new Error('no deployment'))),
    [deploymentId],
  )

  if (!deploymentId) {
    return (
      <div className="content__body stack">
        <EmptyState
          variant="no-results"
          title="No metrics yet"
          description="Deploy this application to see its metrics."
        />
      </div>
    )
  }

  const health = healthState.data

  return (
    <div className="content__body stack">
      <section className="card" aria-label="Application health">
        <h2 className="section-title">Application</h2>
        {healthState.status === 'loading' && <SkeletonLines lines={2} />}
        {healthState.status === 'success' && health && (
          <div className="data-list">
            <div className="data-list__row">
              <div className="data-list__main">
                <div className="data-list__title">Health status</div>
              </div>
              <span className="mono">{health.status ?? 'UNKNOWN'}</span>
            </div>
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
                  <div className="data-list__title">Check latency</div>
                </div>
                <span className="mono">{health.http.latencyMs} ms</span>
              </div>
            )}
          </div>
        )}
        {healthState.status === 'success' && !health && (
          <p className="muted">No health data available.</p>
        )}
      </section>

      <section className="card" aria-label="Runtime metrics">
        <h2 className="section-title">Runtime</h2>
        <p className="muted">
          Runtime metrics (CPU, memory, restarts) arrive with the Runtime Agent (#85).
        </p>
      </section>

      <section className="card" aria-label="Network metrics">
        <h2 className="section-title">Network</h2>
        <p className="muted">
          Network metrics (requests, response codes, throughput) arrive with the Runtime Agent (#85).
        </p>
      </section>
    </div>
  )
}
