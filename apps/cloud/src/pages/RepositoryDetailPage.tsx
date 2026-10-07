import { useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import {
  createApplication,
  getRepository,
  listApplications,
  listRefs,
  startAnalysis,
} from '../api/resources'
import { ApiError } from '../api/errors'
import { PageHeader } from '../components/shell/PageHeader'
import { TechnicalValue } from '../components/TechnicalValue'
import { ErrorPanel, InlineNotice, SkeletonLines, useToast } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { routes } from '../routes/builders'

/**
 * Repository Detail screen (repository §2). Choose the ref to deploy and
 * start analysis. Primary action: **Analyze**.
 *
 * If no application exists for this repository, one is created. If one
 * exists, the user chooses which application to analyze for.
 */
export function RepositoryDetailPage() {
  const { repositoryId } = useParams<{ repositoryId: string }>()
  const [searchParams, setSearchParams] = useSearchParams()
  const navigate = useNavigate()
  const { push } = useToast()

  const [selectedRef, setSelectedRef] = useState<string | null>(searchParams.get('ref'))
  const [analyzing, setAnalyzing] = useState(false)
  const [analyzeError, setAnalyzeError] = useState<ApiError | null>(null)
  const [showAppDialog, setShowAppDialog] = useState(false)

  const repoState = useAsync(
    (signal) => (repositoryId ? getRepository(repositoryId, { signal }) : Promise.reject(new Error('missing id'))),
    [repositoryId],
  )

  const refsState = useAsync(
    (signal) => (repositoryId ? listRefs(repositoryId, { signal }) : Promise.reject(new Error('missing id'))),
    [repositoryId],
  )

  const appsState = useAsync((signal) => listApplications({ signal }), [])

  const repository = repoState.data
  const refs = refsState.data ?? []
  const applications = appsState.data?.items ?? []
  const repoApps = applications.filter((a) => a.repositoryId === repositoryId)

  const refParam = searchParams.get('ref')
  const effectiveRef = selectedRef ?? refParam ?? repository?.defaultBranch ?? refs.find((r) => r.default)?.name ?? null

  const selectRef = (ref: string) => {
    setSelectedRef(ref)
    const params = new URLSearchParams(searchParams)
    params.set('ref', ref)
    setSearchParams(params, { replace: true })
  }

  const analyze = async (applicationId?: string) => {
    if (!repositoryId || !effectiveRef) return
    setAnalyzing(true)
    setAnalyzeError(null)
    try {
      let appId = applicationId
      if (!appId) {
        if (repoApps.length === 0) {
          const name = repository?.fullName?.split('/').pop()?.replace(/[^a-zA-Z0-9-]/g, '-').toLowerCase() ?? 'app'
          const app = await createApplication({ repositoryId, name })
          appId = app.id
        } else {
          setShowAppDialog(true)
          setAnalyzing(false)
          return
        }
      }
      const analysis = await startAnalysis(appId, { ref: effectiveRef })
      push({ variant: 'success', title: 'Analysis complete', body: `Analysis ${analysis.analysisId} finished.` })
      navigate(routes.setup({ applicationId: appId, step: 'analysis' }) + `?analysis=${analysis.analysisId}`)
    } catch (cause) {
      const error = cause instanceof ApiError ? cause : ApiError.network()
      setAnalyzeError(error)
      push({ variant: 'failure', title: "Couldn't start analysis", body: error.message })
    } finally {
      setAnalyzing(false)
    }
  }

  const branches = refs.filter((r) => r.type === 'branch' || r.type === undefined)
  const tags = refs.filter((r) => r.type === 'tag')

  return (
    <>
      <PageHeader
        breadcrumbs={[
          { label: 'Workspace' },
          { label: 'Repositories', to: routes.repositories() },
          { label: repository?.fullName ?? repositoryId ?? 'Repository', current: true },
        ]}
        title={repository?.fullName ?? 'Repository'}
        meta={
          repository
            ? `${repository.htmlUrl ?? repository.cloneUrl ?? ''}${repository.defaultBranch ? ` · default ${repository.defaultBranch}` : ''}`
            : undefined
        }
        actions={
          <button
            type="button"
            className="btn btn--primary"
            onClick={() => void analyze()}
            disabled={analyzing || !effectiveRef}
          >
            {analyzing ? 'Analyzing…' : 'Analyze ▸'}
          </button>
        }
      />
      <div className="content__body stack">
        {repoState.status === 'loading' && <SkeletonLines lines={4} />}

        {repoState.status === 'error' && repoState.error && (
          <ErrorPanel error={repoState.error} objectName="this repository" onRetry={repoState.reload} />
        )}

        {analyzeError && (
          <InlineNotice variant="failed" title="Analysis couldn't start">
            {analyzeError.message}
          </InlineNotice>
        )}

        {repository && (
          <>
            <section className="card" aria-label="Ref">
              <h2 className="section-title">Ref</h2>
              <div className="field">
                <label className="field__label" htmlFor="ref-select">
                  Branch or tag
                </label>
                <select
                  id="ref-select"
                  className="field__input"
                  value={effectiveRef ?? ''}
                  onChange={(event) => selectRef(event.target.value)}
                >
                  {refs.length === 0 && <option value="">No refs available</option>}
                  {branches.length > 0 && (
                    <optgroup label="Branches">
                      {branches.map((ref) => (
                        <option key={ref.name} value={ref.name}>
                          {ref.name}
                          {ref.default ? ' (default)' : ''}
                          {ref.commitSha ? ` · ${ref.commitSha.slice(0, 7)}` : ''}
                        </option>
                      ))}
                    </optgroup>
                  )}
                  {tags.length > 0 && (
                    <optgroup label="Tags">
                      {tags.map((ref) => (
                        <option key={ref.name} value={ref.name}>
                          {ref.name}
                          {ref.commitSha ? ` · ${ref.commitSha.slice(0, 7)}` : ''}
                        </option>
                      ))}
                    </optgroup>
                  )}
                </select>
                {effectiveRef && (
                  <p className="muted" style={{ marginTop: 4 }}>
                    Analysis will run against <span className="mono">{effectiveRef}</span>
                  </p>
                )}
              </div>
            </section>

            <div className="grid grid--cards">
              <section className="card" aria-label="How analysis works">
                <h2 className="section-title">How analysis works</h2>
                <p className="muted">
                  Axiom reads repository files to detect the runtime, framework and commands. No code is executed.
                </p>
              </section>

              <section className="card" aria-label="Repository details">
                <h2 className="section-title">Details</h2>
                <div className="data-list">
                  <div className="data-list__row">
                    <div className="data-list__main">
                      <div className="data-list__title">Repository ID</div>
                    </div>
                    <TechnicalValue value={repository.id} />
                  </div>
                  {repository.language && (
                    <div className="data-list__row">
                      <div className="data-list__main">
                        <div className="data-list__title">GitHub language</div>
                      </div>
                      <span>{repository.language}</span>
                    </div>
                  )}
                  {repository.pushedAt && (
                    <div className="data-list__row">
                      <div className="data-list__main">
                        <div className="data-list__title">Last push</div>
                      </div>
                      <span className="mono">{repository.pushedAt}</span>
                    </div>
                  )}
                </div>
              </section>
            </div>

            {repoApps.length > 0 && (
              <section className="card" aria-label="Axiom applications from this repository">
                <h2 className="section-title">Axiom applications</h2>
                <div className="data-list">
                  {repoApps.map((app) => (
                    <div className="data-list__row" key={app.id}>
                      <div className="data-list__main">
                        <div className="data-list__title">{app.name}</div>
                        <div className="data-list__sub mono">{app.id}</div>
                      </div>
                      <Link className="btn btn--ghost btn--sm" to={routes.application(app.id)}>
                        Open
                      </Link>
                    </div>
                  ))}
                </div>
              </section>
            )}
          </>
        )}

        {showAppDialog && repoApps.length > 0 && (
          <div className="drawer-scrim" onClick={() => setShowAppDialog(false)} aria-hidden="true">
            <div
              className="confirm-dialog"
              role="alertdialog"
              aria-modal="true"
              aria-labelledby="app-dialog-title"
              onClick={(event) => event.stopPropagation()}
            >
              <h2 className="confirm-dialog__title" id="app-dialog-title">
                Analyze for which application?
              </h2>
              <div className="stack" style={{ gap: 8 }}>
                {repoApps.map((app) => (
                  <button
                    key={app.id}
                    type="button"
                    className="btn btn--secondary"
                    onClick={() => {
                      setShowAppDialog(false)
                      void analyze(app.id)
                    }}
                  >
                    {app.name}
                  </button>
                ))}
              </div>
              <div className="confirm-dialog__actions">
                <button type="button" className="btn btn--ghost" onClick={() => setShowAppDialog(false)}>
                  Cancel
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </>
  )
}
