import { useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { listGithubConnections, listRepositories } from '../api/resources'
import type { Paginated, Repository } from '../api/types'
import { PageHeader } from '../components/shell/PageHeader'
import { EmptyState, ErrorPanel, SkeletonRows } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { hasNextPage, nextPage, previousPage } from '../api/pagination'
import { routes } from '../routes/builders'

export function RepositoriesPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const connectionParam = searchParams.get('connection') ?? undefined
  const query = searchParams.get('q') ?? ''
  const page = Number(searchParams.get('page') ?? '1') || 1
  const [searchDraft, setSearchDraft] = useState(query)

  const connectionsState = useAsync((signal) => listGithubConnections({ signal }), [])
  const connectionId = connectionParam ?? connectionsState.data?.[0]?.id

  const reposState = useAsync<Paginated<Repository> | null>(
    (signal) =>
      connectionId
        ? listRepositories(connectionId, { page, search: query || undefined }, { signal })
        : Promise.resolve(null),
    [connectionId, page, query],
  )

  const updateParams = (next: Record<string, string | undefined>) => {
    const params = new URLSearchParams(searchParams)
    for (const [key, value] of Object.entries(next)) {
      if (value === undefined || value === '') params.delete(key)
      else params.set(key, value)
    }
    setSearchParams(params, { replace: true })
  }

  const onSubmitSearch = (event: React.FormEvent) => {
    event.preventDefault()
    updateParams({ q: searchDraft.trim() || undefined, page: undefined })
  }

  const pageData = reposState.data

  return (
    <>
      <PageHeader
        breadcrumbs={[{ label: 'Workspace' }, { label: 'Repositories', current: true }]}
        title="Repositories"
      />
      <div className="content__body stack">
        {connectionsState.status === 'error' && connectionsState.error && (
          <ErrorPanel error={connectionsState.error} objectName="your GitHub connections" onRetry={connectionsState.reload} />
        )}

        {connectionsState.status === 'success' && (connectionsState.data?.length ?? 0) === 0 && (
          <EmptyState
            variant="first-use"
            title="Connect GitHub to see repositories"
            description="Repositories are listed from a connected GitHub account."
            primaryAction={
              <Link className="btn btn--primary" to={routes.github()}>
                Connect GitHub
              </Link>
            }
          />
        )}

        {connectionId && (
          <>
            <form className="row" onSubmit={onSubmitSearch} role="search">
              <input
                className="field__input"
                type="search"
                placeholder="Search repositories"
                aria-label="Search repositories"
                value={searchDraft}
                onChange={(event) => setSearchDraft(event.target.value)}
                style={{ flex: '1 1 auto' }}
              />
              <button className="btn btn--secondary" type="submit">
                Search
              </button>
            </form>

            {(reposState.status === 'loading' || connectionsState.status === 'loading') && <SkeletonRows count={5} />}

            {reposState.status === 'error' && reposState.error && (
              <ErrorPanel error={reposState.error} objectName="repositories" onRetry={reposState.reload} />
            )}

            {reposState.status === 'success' && pageData && pageData.items.length === 0 && (
              <EmptyState
                variant="no-results"
                title="No repositories match"
                description="Try a different search term or clear the filters."
                primaryAction={
                  <button
                    className="btn btn--secondary"
                    type="button"
                    onClick={() => {
                      setSearchDraft('')
                      updateParams({ q: undefined, page: undefined })
                    }}
                  >
                    Clear filters
                  </button>
                }
              />
            )}

            {reposState.status === 'success' && pageData && pageData.items.length > 0 && (
              <>
                <div className="data-list">
                  {pageData.items.map((repository) => (
                    <Link className="data-list__row" key={repository.id} to={routes.repository(repository.id)}>
                      <div className="data-list__main">
                        <div className="data-list__title mono">{repository.fullName}</div>
                        <div className="data-list__sub">
                          {repository.defaultBranch ? `default: ${repository.defaultBranch}` : '—'}
                          {repository.private ? ' · private' : ''}
                        </div>
                      </div>
                      <span className="muted" aria-hidden="true">
                        ›
                      </span>
                    </Link>
                  ))}
                </div>

                <div className="row">
                  <button
                    className="btn btn--secondary"
                    type="button"
                    disabled={previousPage(pageData) === null}
                    onClick={() => updateParams({ page: String(previousPage(pageData) ?? 1) })}
                  >
                    Previous
                  </button>
                  <span className="muted">
                    Page {pageData.page} · {pageData.total} repositories
                  </span>
                  <button
                    className="btn btn--secondary"
                    type="button"
                    disabled={!hasNextPage(pageData)}
                    onClick={() => updateParams({ page: String(nextPage(pageData) ?? pageData.page) })}
                  >
                    Next
                  </button>
                </div>
              </>
            )}
          </>
        )}
      </div>
    </>
  )
}
