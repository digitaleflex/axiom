import { useParams } from 'react-router-dom'
import { PageHeader } from '../components/shell/PageHeader'
import { isEnvironment, routes, type ApplicationSection } from '../routes/builders'
import { ApplicationSectionContent } from './ApplicationSectionContent'
import { NotFoundPage } from './NotFoundPage'

const SECTION_TITLES: Record<string, string> = {
  overview: 'Overview',
  deployments: 'Deployments',
  logs: 'Logs',
  metrics: 'Metrics',
  domains: 'Domains',
}

/**
 * Application section (application §2, logs §1, metrics §1, domains §1).
 * Route `/apps/:id/:env/:section`. Each section renders its real content.
 */
export function ApplicationSectionPage() {
  const { applicationId, environment, section } = useParams<{
    applicationId: string
    environment: string
    section: string
  }>()
  const env = isEnvironment(environment) ? environment : undefined
  const sectionKey = (section ?? 'overview') as ApplicationSection

  if (!env || !SECTION_TITLES[sectionKey]) {
    return <NotFoundPage />
  }

  return (
    <>
      <PageHeader
        breadcrumbs={[
          { label: 'Workspace', to: routes.dashboard() },
          { label: applicationId ?? 'Application' },
          { label: '', environment: env },
          { label: SECTION_TITLES[sectionKey], current: true },
        ]}
        title={SECTION_TITLES[sectionKey]}
        environment={env}
      />
      <ApplicationSectionContent section={sectionKey} />
    </>
  )
}
