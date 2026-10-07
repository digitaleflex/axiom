import { NavLink, useParams } from 'react-router-dom'
import { Placeholder } from '../components/Placeholder'
import { PageHeader } from '../components/shell/PageHeader'
import { DEPLOYMENT_TABS, isEnvironment, routes, type DeploymentTab } from '../routes/builders'

/** Canonical deployment area placeholder with header tabs (shell §6). */
export function DeploymentTabPage() {
  const { applicationId, environment, deploymentId, tab } = useParams<{
    applicationId: string
    environment: string
    deploymentId: string
    tab: string
  }>()
  const env = isEnvironment(environment) ? environment : undefined
  const activeTab = (tab ?? 'progress') as DeploymentTab

  const tabs =
    applicationId && env && deploymentId ? (
      <nav aria-label="Deployment sections" className="breadcrumbs" style={{ marginTop: 12, marginBottom: 0 }}>
        {DEPLOYMENT_TABS.map((value, index) => (
          <span key={value} className="row" style={{ gap: 8 }}>
            {index > 0 && (
              <span className="breadcrumbs__sep" aria-hidden="true">
                ·
              </span>
            )}
            <NavLink
              to={routes.deployment({ applicationId, environment: env, deploymentId, tab: value })}
              aria-current={activeTab === value ? 'page' : undefined}
            >
              {value[0].toUpperCase() + value.slice(1)}
            </NavLink>
          </span>
        ))}
      </nav>
    ) : undefined

  return (
    <>
      <PageHeader
        breadcrumbs={[
          { label: 'Workspace', to: routes.dashboard() },
          { label: applicationId ?? 'Application' },
          ...(env ? [{ label: '', environment: env }] : []),
          { label: 'Deployments' },
          { label: deploymentId ?? 'Deployment', current: true },
        ]}
        title={`Deployment ${deploymentId ?? ''}`}
        environment={env}
        tabs={tabs}
      />
      <div className="content__body">
        <Placeholder
          params={{ applicationId, environment, deploymentId, tab: activeTab }}
          resources={[
            'GET /api/v1/deployments/{deploymentId}',
            'GET /api/v1/deployments/{deploymentId}/events/stream (SSE)',
          ]}
          note="Deployment tabs render in the page header; each tab's content lands in #120."
        />
      </div>
    </>
  )
}
