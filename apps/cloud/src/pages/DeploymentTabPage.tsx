import { NavLink, useParams } from 'react-router-dom'
import { PageHeader } from '../components/shell/PageHeader'
import { StatusPill } from '../components/StatusPill'
import { DEPLOYMENT_TABS, isEnvironment, routes, type DeploymentTab } from '../routes/builders'
import { DeploymentHealthContent } from './DeploymentHealthContent'
import { DeploymentLogsContent } from './DeploymentLogsContent'
import { DeploymentPlanContent } from './DeploymentPlanContent'
import { DeploymentProgressContent } from './DeploymentProgressContent'
import { DeploymentRuntimeContent } from './DeploymentRuntimeContent'
import { DeploymentSummaryContent } from './DeploymentSummaryContent'
import { useAsync } from '../hooks/useAsync'
import { getDeployment } from '../api/resources'
import { SkeletonLines } from '../components/system'

/**
 * Deployment area with tab chrome (shell §6, deployment-progress §3).
 * Each tab renders its real content; the header shows the deployment status.
 */
export function DeploymentTabPage() {
  const { applicationId, environment, deploymentId, tab } = useParams<{
    applicationId: string
    environment: string
    deploymentId: string
    tab: string
  }>()
  const env = isEnvironment(environment) ? environment : undefined
  const activeTab = (tab ?? 'progress') as DeploymentTab

  const deploymentState = useAsync(
    (signal) => (deploymentId ? getDeployment(deploymentId, { signal }) : Promise.reject(new Error('missing id'))),
    [deploymentId],
  )

  const deployment = deploymentState.data

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
          { label: deployment?.number ? `#${deployment.number}` : (deploymentId ?? 'Deployment'), current: true },
        ]}
        title={`Deployment ${deployment?.number ? `#${deployment.number}` : ''}`}
        environment={env}
        meta={
          deployment
            ? `${deployment.id}${deployment.startedAt ? ` · started ${deployment.startedAt}` : ''}`
            : undefined
        }
        actions={
          <div className="row" style={{ gap: 8 }}>
            {deployment?.status && <StatusPill status={deployment.status} />}
          </div>
        }
        tabs={tabs}
      />
      {deploymentState.status === 'loading' && <SkeletonLines lines={4} />}
      {activeTab === 'summary' && <DeploymentSummaryContent />}
      {activeTab === 'plan' && <DeploymentPlanContent />}
      {activeTab === 'progress' && <DeploymentProgressContent />}
      {activeTab === 'logs' && <DeploymentLogsContent />}
      {activeTab === 'health' && <DeploymentHealthContent />}
      {activeTab === 'runtime' && <DeploymentRuntimeContent />}
    </>
  )
}
