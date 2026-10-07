import { Link } from 'react-router-dom'
import { listApplications } from '../api/resources'
import { EmptyState, ErrorPanel, SkeletonCards } from '../components/system'
import { PageHeader } from '../components/shell/PageHeader'
import { useAsync } from '../hooks/useAsync'
import { routes } from '../routes/builders'

export function DashboardPage() {
  const state = useAsync((signal) => listApplications({ signal }), [])

  return (
    <>
      <PageHeader
        breadcrumbs={[{ label: 'Workspace' }, { label: 'Dashboard', current: true }]}
        title="Dashboard"
        actions={
          <Link className="btn btn--primary" to={routes.repositories()}>
            New application
          </Link>
        }
      />
      <div className="content__body">
        {state.status === 'loading' && <SkeletonCards count={3} />}

        {state.status === 'error' && state.error && (
          <ErrorPanel error={state.error} objectName="your applications" onRetry={state.reload} />
        )}

        {state.status === 'success' && state.data && (
          state.data.items.length === 0 ? (
            <EmptyState
              variant="first-use"
              title="Deploy your first application"
              description="Connect a GitHub repository, let Axiom understand its stack, then deploy it to your infrastructure."
              primaryAction={
                <Link className="btn btn--primary" to={routes.github()}>
                  Connect GitHub
                </Link>
              }
              secondaryAction={
                <Link className="btn btn--secondary" to={routes.repositories()}>
                  Choose repository
                </Link>
              }
            />
          ) : (
            <div className="grid grid--cards">
              {state.data.items.map((application) => (
                <Link className="card" key={application.id} to={routes.application(application.id)}>
                  <div className="data-list__title">{application.name}</div>
                  <div className="data-list__sub mono">{application.id}</div>
                </Link>
              ))}
            </div>
          )
        )}
      </div>
    </>
  )
}
