import { useParams } from 'react-router-dom'
import { Placeholder } from '../components/Placeholder'
import { PageHeader } from '../components/shell/PageHeader'
import { isEnvironment, routes, type ApplicationSection } from '../routes/builders'

const SECTION_TITLES: Record<string, string> = {
  overview: 'Overview',
  deployments: 'Deployments',
  logs: 'Logs',
  metrics: 'Metrics',
  domains: 'Domains',
}

/** Canonical application area placeholder (navigation §5.1). */
export function ApplicationSectionPage() {
  const { applicationId, environment, section } = useParams<{
    applicationId: string
    environment: string
    section: string
  }>()
  const env = isEnvironment(environment) ? environment : undefined
  const sectionKey = (section ?? 'overview') as ApplicationSection

  return (
    <>
      <PageHeader
        breadcrumbs={[
          { label: 'Workspace', to: routes.dashboard() },
          { label: applicationId ?? 'Application' },
          ...(env ? [{ label: '', environment: env }] : []),
          { label: SECTION_TITLES[sectionKey] ?? sectionKey, current: true },
        ]}
        title={SECTION_TITLES[sectionKey] ?? 'Application'}
        environment={env}
      />
      <div className="content__body">
        <Placeholder
          params={{ applicationId, environment, section }}
          resources={[
            'GET /api/v1/applications/{applicationId}',
            'GET /api/v1/applications/{applicationId}/deployments',
            sectionKey === 'logs' ? 'GET /api/v1/deployments/{deploymentId}/logs' : '',
            sectionKey === 'metrics' ? 'GET /api/v1/servers/{serverId}/metrics' : '',
            sectionKey === 'domains' ? 'GET /api/v1/domains' : '',
          ].filter(Boolean)}
          note="Environment-aware screens land in #120; the environment chip and context bar already follow the URL."
        />
      </div>
    </>
  )
}
