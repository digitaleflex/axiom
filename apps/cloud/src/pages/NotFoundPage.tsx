import { Link } from 'react-router-dom'
import { PageHeader } from '../components/shell/PageHeader'
import { SystemStatePage } from '../components/system'
import { routes } from '../routes/builders'

export function NotFoundPage() {
  return (
    <>
      <PageHeader breadcrumbs={[{ label: 'Workspace' }, { label: "We can't find that page", current: true }]} title="Not found" />
      <div className="content__body">
        <SystemStatePage
          code="404"
          title="We can't find that page."
          description="That page doesn't exist, or you may not have access to it."
          actions={
            <Link className="btn btn--primary" to={routes.dashboard()}>
              Go to Dashboard
            </Link>
          }
        />
      </div>
    </>
  )
}
