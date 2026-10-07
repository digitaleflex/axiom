import { useParams } from 'react-router-dom'
import { Placeholder } from '../components/Placeholder'
import { PageHeader } from '../components/shell/PageHeader'
import { routes } from '../routes/builders'

export function SettingsPage() {
  const { section } = useParams<{ section: string }>()
  return (
    <>
      <PageHeader
        breadcrumbs={[{ label: 'Workspace' }, { label: 'Settings', to: routes.settings() }, ...(section ? [{ label: section, current: true }] : [])]}
        title="Settings"
      />
      <div className="content__body">
        <Placeholder
          params={{ section }}
          resources={[
            'GET /api/v1/auth/me',
            'Sessions / API tokens / audit (contract gaps #100 / #125 / #126 / #128 — not rendered yet)',
          ]}
          note="Settings sections depend on contracts that do not exist yet; they are intentionally not rendered."
        />
      </div>
    </>
  )
}
