import { Placeholder } from '../components/Placeholder'
import { PageHeader } from '../components/shell/PageHeader'

export function DomainsPage() {
  return (
    <>
      <PageHeader breadcrumbs={[{ label: 'Workspace' }, { label: 'Domains', current: true }]} title="Domains" />
      <div className="content__body">
        <Placeholder
          params={{}}
          resources={['GET /api/v1/domains (workspace-wide domain list)']}
          note="Workspace-wide domains land with the Domains screens; domain status is a contract gap (#64)."
        />
      </div>
    </>
  )
}
