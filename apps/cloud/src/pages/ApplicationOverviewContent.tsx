import { Link, useParams } from 'react-router-dom'
import {
  getApplication,
  getDeploymentHealth,
  listDeployments,
  listDomains,
} from '../api/resources'
import { StatusPill } from '../components/StatusPill'
import { EmptyState, ErrorPanel, SkeletonLines } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { routes } from '../routes/builders'

/**
 * Application Overview content (application §1). Operational home for one
 * application in one environment: is it live, where, which version, is it
 * healthy.
 */
export function ApplicationOverviewContent() {
  const { applicationId, environment } = useParams<{ applicationId: string; environment: string }>()

  const appState = useAsync(
    (signal) => (applicationId ? getApplication(applicationId, { signal }) : Promise.reject(new Error('missing id'))),
    [applicationId],
  )

  const deploymentsState = useAsync(
    (signal) =>
      applicationId
        ? listDeployments(applicationId, { environment, limit: 5 }, { signal })
        : Promise.reject(new Error('missing id')),
    [applicationId, environment],
  )

  const domainsState = useAsync(
    (signal) => (applicationId ? listDomains(applicationId, environment, { signal }) : Promise.reject(new Error('missing id'))),
    [applicationId, environment],
  )

  const deployments = deploymentsState.data?.items ?? []
  const currentDeployment = deployments.find((d) => d.status === 'LIVE') ?? deployments[0]
  const domains = domainsState.data ?? []

  const healthState = useAsync(
    (signal) =>
      currentDeployment?.id
        ? getDeploymentHealth(currentDeployment.id, { signal })
        : Promise.reject(new Error('no deployment')),
    [currentDeployment?.id],
  )

  const health = healthState.data
  const failedDeployment = deployments.find((d) => d.status === 'FAILED')

  return (
    <div className="content__body stack">
      {appState.status === 'loading' && <SkeletonLines lines={4} />}

      {appState.status === 'error' && appState.error && (
        <ErrorPanel error={appState.error} objectName="this application" onRetry={appState.reload} />
      )}

      {appState.status === 'success' && appState.data && (
        <>
          {!currentDeployment && (
            <EmptyState
              variant="no-results"
              title={`${appState.data.name} isn't deployed to ${environment} yet`}
              description="Deploy this application to start serving traffic."
              primaryAction={
                <Link className="btn btn--primary" to={routes.setup({ applicationId: applicationId ?? '', step: 'configure' })}>
                  Deploy to {environment}
                </Link>
              }
            />
          )}

          {currentDeployment && (
            <section className="card" aria-label="Status">
              <div className="row" style={{ justifyContent: 'space-between' }}>
                <h2 className="section-title">
                  <StatusPill status={currentDeployment.status} />
                </h2>
                <Link
                  className="btn btn--ghost btn--sm"
                  to={routes.deployment({
                    applicationId: applicationId ?? '',
                    environment: environment as 'production',
                    deploymentId: currentDeployment.id,
                    tab: 'summary',
                  })}
                >
                  View deployment
                </Link>
              </div>
              {currentDeployment.url && (
                <p style={{ margin: '4px 0 0' }}>
                  <a href={currentDeployment.url} target="_blank" rel="noreferrer" className="mono">
                    {currentDeployment.url}
                  </a>
                </p>
              )}
              {health?.status && (
                <p className="muted" style={{ marginTop: 4 }}>
                  Health{' '}
                  {health.status === 'HEALTHY' ? '● Healthy' : health.status === 'UNHEALTHY' ? '⬣ Failing' : '○ Unknown'}
                  {health.http?.statusCode ? ` · GET ${health.http.statusCode}` : ''}
                  {health.http?.latencyMs !== undefined ? ` in ${health.http.latencyMs} ms` : ''}
                  {health.checkedAt ? ` · checked ${health.checkedAt}` : ''}
                </p>
              )}
              {failedDeployment && (
                <p className="muted" style={{ marginTop: 4 }}>
                  Last deployment <strong>failed</strong> —{' '}
                  <Link
                    to={routes.deployment({
                      applicationId: applicationId ?? '',
                      environment: environment as 'production',
                      deploymentId: failedDeployment.id,
                      tab: 'summary',
                    })}
                  >
                    View failure
                  </Link>
                </p>
              )}
            </section>
          )}

          <div className="grid grid--cards">
            <section className="card" aria-label="Runtime">
              <h2 className="section-title">Runtime</h2>
              {currentDeployment ? (
                <div className="data-list">
                  <div className="data-list__row">
                    <div className="data-list__main">
                      <div className="data-list__title">Server</div>
                    </div>
                    <span className="mono">{currentDeployment.serverId ?? '—'}</span>
                  </div>
                  <div className="data-list__row">
                    <div className="data-list__main">
                      <div className="data-list__title">Deployment</div>
                    </div>
                    <span className="mono">#{currentDeployment.number ?? currentDeployment.id}</span>
                  </div>
                </div>
              ) : (
                <p className="muted">Not deployed.</p>
              )}
            </section>

            <section className="card" aria-label="Domains">
              <h2 className="section-title">Domains</h2>
              {domains.length === 0 ? (
                <p className="muted">No domains configured.</p>
              ) : (
                <div className="data-list">
                  {domains.map((domain) => (
                    <div className="data-list__row" key={domain.id}>
                      <div className="data-list__main">
                        <div className="data-list__title mono">{domain.hostname ?? '—'}</div>
                        <div className="data-list__sub">
                          {domain.isPrimary ? 'Primary' : 'Secondary'}
                          {domain.tlsStatus ? ` · TLS ${domain.tlsStatus}` : ''}
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
              <Link
                className="btn btn--ghost btn--sm"
                to={routes.appSection({ applicationId: applicationId ?? '', environment: environment as 'production', section: 'domains' })}
              >
                Manage domains
              </Link>
            </section>
          </div>

          {deployments.length > 0 && (
            <section className="card" aria-label="Recent deployments">
              <h2 className="section-title">Recent deployments</h2>
              <div className="data-list">
                {deployments.slice(0, 5).map((dep) => (
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
                      </div>
                    </div>
                  </Link>
                ))}
              </div>
              <Link
                className="btn btn--ghost btn--sm"
                to={routes.appSection({ applicationId: applicationId ?? '', environment: environment as 'production', section: 'deployments' })}
              >
                All deployments
              </Link>
            </section>
          )}
        </>
      )}
    </div>
  )
}
