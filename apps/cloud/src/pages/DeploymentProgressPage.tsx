import { Link, useParams } from 'react-router-dom'
import { getApplication } from '../api/resources'
import { PageHeader } from '../components/shell/PageHeader'
import { StatusPill } from '../components/StatusPill'
import { useAsync } from '../hooks/useAsync'
import { useDeploymentProgress } from '../hooks/useDeploymentProgress'
import { routes } from '../routes/builders'
import { isTerminalStatus } from '../workflow/stepMapping'
import { DeploymentProgressContent } from './DeploymentProgressContent'

/**
 * Deployment Progress permalink (navigation §5.2). Route `/deployments/:id`.
 * Renders the same progress content as the deployment Progress tab.
 */
export function DeploymentProgressPage() {
  const { deploymentId } = useParams<{ deploymentId: string }>()
  const progress = useDeploymentProgress(deploymentId)
  const { deployment, status } = progress

  const appState = useAsync(
    (signal) => (deployment?.applicationId ? getApplication(deployment.applicationId, { signal }) : Promise.reject(new Error('missing id'))),
    [deployment?.applicationId],
  )

  const terminal = isTerminalStatus(status)

  return (
    <>
      <PageHeader
        breadcrumbs={[
          { label: 'Workspace', to: routes.dashboard() },
          { label: appState.data?.name ?? deployment?.applicationId ?? 'Application' },
          { label: 'Deployments' },
          { label: deployment?.number ? `#${deployment.number}` : (deploymentId ?? 'Deployment'), current: true },
        ]}
        title={`Deployment ${deployment?.number ? `#${deployment.number}` : ''}`}
        meta={
          deployment
            ? `${deployment.id}${deployment.startedAt ? ` · started ${deployment.startedAt}` : ''}`
            : undefined
        }
        actions={
          <div className="row" style={{ gap: 8 }}>
            {status && <StatusPill status={status} />}
            {!terminal && (
              <Link
                className="btn btn--ghost"
                to={routes.deployment({
                  applicationId: deployment?.applicationId ?? '',
                  environment: (deployment?.environment as 'production') ?? 'production',
                  deploymentId: deployment?.id ?? '',
                  tab: 'summary',
                })}
              >
                View summary
              </Link>
            )}
          </div>
        }
      />
      <DeploymentProgressContent />
    </>
  )
}
