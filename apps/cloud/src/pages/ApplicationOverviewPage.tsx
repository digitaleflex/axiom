import { useParams } from 'react-router-dom'
import { PageHeader } from '../components/shell/PageHeader'
import { isEnvironment, routes } from '../routes/builders'
import { ApplicationOverviewContent } from './ApplicationOverviewContent'
import { NotFoundPage } from './NotFoundPage'

/**
 * Application Overview (application §1). Route `/apps/:id/:env/overview`
 * and task-lane `/applications/:id`. Operational home for one application
 * in one environment.
 */
export function ApplicationOverviewPage() {
  const { applicationId, environment } = useParams<{ applicationId: string; environment?: string }>()
  const env = isEnvironment(environment) ? environment : undefined

  if (!env) {
    return <NotFoundPage />
  }

  return (
    <>
      <PageHeader
        breadcrumbs={[
          { label: 'Workspace', to: routes.dashboard() },
          { label: applicationId ?? 'Application' },
          { label: '', environment: env },
          { label: 'Overview', current: true },
        ]}
        title="Overview"
        environment={env}
      />
      <ApplicationOverviewContent />
    </>
  )
}
