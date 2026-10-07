import { Link } from 'react-router-dom'
import { listApplications, listDomains } from '../api/resources'
import type { Domain } from '../api/types'
import { PageHeader } from '../components/shell/PageHeader'
import { StatusPill } from '../components/StatusPill'
import { EmptyState, ErrorPanel, SkeletonRows } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { routes } from '../routes/builders'

function overallStatus(domain: Domain): string {
  if (domain.tlsStatus === 'failed' || domain.routingStatus === 'failed') return 'FAILED'
  if (domain.dnsStatus === 'mismatch' || domain.tlsStatus === 'expiring') return 'warning'
  if (domain.dnsStatus === 'ok' && domain.tlsStatus === 'valid' && domain.routingStatus === 'active') return 'LIVE'
  return 'unknown'
}

/**
 * Workspace-wide Domains list (domains §2). Lists domains across all
 * applications by fetching each application's domains.
 */
export function DomainsPage() {
  const appsState = useAsync((signal) => listApplications({ signal }), [])

  const applications = appsState.data?.items ?? []

  const domainsState = useAsync(
    async (signal) => {
      const results = await Promise.all(
        applications.map(async (app) => {
          try {
            const domains = await listDomains(app.id, undefined, { signal })
            return domains.map((domain) => ({
              domain,
              applicationId: app.id,
              applicationName: app.name,
            }))
          } catch {
            return []
          }
        }),
      )
      return results.flat()
    },
    [applications],
  )

  const domains = domainsState.data ?? []

  return (
    <>
      <PageHeader breadcrumbs={[{ label: 'Workspace' }, { label: 'Domains', current: true }]} title="Domains" />
      <div className="content__body stack">
        {appsState.status === 'loading' && <SkeletonRows count={4} />}

        {appsState.status === 'error' && appsState.error && (
          <ErrorPanel error={appsState.error} objectName="your applications" onRetry={appsState.reload} />
        )}

        {appsState.status === 'success' && applications.length === 0 && (
          <EmptyState
            variant="first-use"
            title="No applications yet"
            description="Deploy an application to see its domains here."
          />
        )}

        {appsState.status === 'success' && applications.length > 0 && domainsState.status === 'loading' && (
          <SkeletonRows count={4} />
        )}

        {appsState.status === 'success' && applications.length > 0 && domainsState.status === 'success' && domains.length === 0 && (
          <EmptyState
            variant="no-results"
            title="No domains configured"
            description="Add a domain to an application to see it here."
          />
        )}

        {appsState.status === 'success' && applications.length > 0 && domainsState.status === 'success' && domains.length > 0 && (
          <div className="data-list">
            {domains.map(({ domain, applicationId, applicationName }) => (
              <Link
                className="data-list__row"
                key={domain.id}
                to={routes.appSection({ applicationId, environment: (domain.environment ?? 'production') as 'production', section: 'domains' })}
              >
                <div className="data-list__main">
                  <div className="data-list__title mono">{domain.hostname ?? '—'}</div>
                  <div className="data-list__sub">
                    {applicationName} · {domain.environment ?? '—'}
                    {domain.isPrimary ? ' · Primary' : ''}
                    {domain.dnsStatus ? ` · DNS ${domain.dnsStatus}` : ''}
                    {domain.tlsStatus ? ` · TLS ${domain.tlsStatus}` : ''}
                    {domain.routingStatus ? ` · Routing ${domain.routingStatus}` : ''}
                  </div>
                </div>
                <StatusPill status={overallStatus(domain)} />
              </Link>
            ))}
          </div>
        )}
      </div>
    </>
  )
}
