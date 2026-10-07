import { Link } from 'react-router-dom'
import { PageHeader } from '../components/shell/PageHeader'
import { SystemStatePage } from '../components/system'
import { routes } from '../routes/builders'

export function ForbiddenPage() {
  return (
    <>
      <PageHeader breadcrumbs={[{ label: 'Workspace' }, { label: 'Forbidden', current: true }]} title="Forbidden" />
      <div className="content__body">
        <SystemStatePage
          code="403"
          title="You don't have access to this page."
          description="An owner of this workspace can grant access."
          actions={
            <>
              <Link className="btn btn--secondary" to={routes.dashboard()}>
                Back to Dashboard
              </Link>
            </>
          }
        />
      </div>
    </>
  )
}
