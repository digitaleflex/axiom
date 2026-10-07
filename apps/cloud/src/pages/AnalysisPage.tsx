import { useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { getAnalysis, getApplication } from '../api/resources'
import type { Analysis, Finding } from '../api/types'
import { ConfidenceBadge } from '../components/ConfidenceBadge'
import { EvidenceDrawer } from '../components/EvidenceDrawer'
import { PageHeader } from '../components/shell/PageHeader'
import { SetupStepper } from '../components/SetupStepper'
import { EmptyState, ErrorPanel, InlineNotice, SkeletonLines } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { routes } from '../routes/builders'
import { analysisErrorCopy } from '../workflow/errorCopy'

const STAGES = [
  'Fetch repository snapshot',
  'Inspect manifests',
  'Detect runtime & framework',
  'Detect build & start commands',
  'Detect services & configuration',
  'Resolve deployment strategy',
] as const

function findingStateLabel(finding: Finding): string {
  switch (finding.state) {
    case 'detected':
      return 'Detected'
    case 'ambiguous':
      return 'Ambiguous'
    case 'not_detected':
      return 'Not detected'
    case 'unsupported':
      return 'Not supported in V0.1'
    case 'not_applicable':
      return 'Not applicable'
    default:
      return 'Unknown'
  }
}

function findingValue(finding: Finding): string {
  if (finding.state === 'ambiguous') {
    return (finding.candidates ?? []).join(' or ')
  }
  if (finding.values && finding.values.length > 0) return finding.values.join(', ')
  return finding.value ?? '—'
}

/**
 * Repository Analysis screen (analysis §3). Setup step 1.
 *
 * Shows the analysis stages, findings with confidence bands and evidence
 * counts, and the overall outcome. Continue is disabled until the analysis
 * completes; blocking facts are resolved in the Profile step.
 */
export function AnalysisPage() {
  const { applicationId } = useParams<{ applicationId: string }>()
  const [searchParams] = useSearchParams()
  const analysisId = searchParams.get('analysis') ?? undefined
  const [evidenceFor, setEvidenceFor] = useState<Finding | null>(null)

  const appState = useAsync(
    (signal) => (applicationId ? getApplication(applicationId, { signal }) : Promise.reject(new Error('missing id'))),
    [applicationId],
  )

  const state = useAsync<Analysis | null>(
    (signal) =>
      applicationId
        ? analysisId
          ? getAnalysis(applicationId, analysisId, { signal })
          : // No analysisId: the latest analysis is what the Engine returns from
            // the start-analysis flow; without one we show the empty state.
            Promise.resolve(null)
        : Promise.reject(new Error('missing id')),
    [applicationId, analysisId],
  )

  const analysis = state.data
  const result = analysis?.result
  const findings = result?.findings ?? []
  const running = analysis?.status === undefined || analysis?.status === 'RUNNING' || analysis?.status === 'PENDING'
  const failed = analysis?.status === 'FAILED'
  const completed = analysis?.status === 'COMPLETED'

  const blockingCount = findings.filter(
    (f) => f.state === 'ambiguous' || f.state === 'not_detected' || f.state === 'unsupported',
  ).length

  const unsupported = findings.find((f) => f.state === 'unsupported')

  return (
    <>
      <PageHeader
        breadcrumbs={[
          { label: 'Workspace', to: routes.dashboard() },
          { label: appState.data?.name ?? applicationId ?? 'Application' },
          { label: 'Setup', current: true },
        ]}
        title={`Analyzing ${appState.data?.name ?? 'application'}`}
        meta={
          analysis?.ref
            ? `ref ${analysis.ref}${analysis.commit ? ` · ${analysis.commit.slice(0, 7)}` : ''} · read-only analysis — no code is executed`
            : undefined
        }
        actions={
          <Link
            className="btn btn--primary"
            to={routes.setup({ applicationId: applicationId ?? '', step: 'profile' })}
            aria-disabled={!completed}
            style={{ opacity: completed ? 1 : 0.5, pointerEvents: completed ? 'auto' : 'none' }}
          >
            {running ? 'Analyzing…' : 'Continue ▸'}
          </Link>
        }
        tabs={applicationId ? <SetupStepper applicationId={applicationId} current="analysis" /> : undefined}
      />
      <div className="content__body stack">
        {state.status === 'loading' && <SkeletonLines lines={6} />}

        {state.status === 'error' && state.error && (
          <ErrorPanel error={state.error} objectName="this analysis" onRetry={state.reload} />
        )}

        {state.status === 'success' && !analysis && (
          <EmptyState
            variant="no-results"
            title="No analysis yet"
            description="Start an analysis from the repository detail screen to let Axiom understand this application."
            primaryAction={
              <Link className="btn btn--secondary" to={routes.dashboard()}>
                Back to dashboard
              </Link>
            }
          />
        )}

        {analysis && failed && (
          <InlineNotice variant="failed" title="Analysis couldn't complete">
            {analysisErrorCopy(analysis.errorCode)}
            <div style={{ marginTop: 8 }}>
              <button type="button" className="btn btn--secondary" onClick={state.reload}>
                Retry analysis
              </button>
            </div>
          </InlineNotice>
        )}

        {analysis && unsupported && (
          <InlineNotice variant="warning" title="This repository can't be deployed by Axiom V0.1 yet">
            {unsupported.value ? `Detected: ${unsupported.value}. ` : ''}
            Supported stacks: Node.js (Next.js, Vite), Go, Dockerfile.
          </InlineNotice>
        )}

        {analysis && completed && !unsupported && (
          <InlineNotice
            variant={blockingCount > 0 ? 'warning' : 'success'}
            title={blockingCount > 0 ? `Axiom needs you to confirm ${blockingCount} detail${blockingCount > 1 ? 's' : ''}.` : 'Axiom understood this repository.'}
          >
            {blockingCount > 0
              ? 'Blocking facts are resolved in the Profile step before continuing.'
              : 'Review the profile, then continue to server selection.'}
          </InlineNotice>
        )}

        {analysis && (
          <div className="grid grid--cards">
            <section className="card" aria-label="Analysis stages">
              <h2 className="section-title">Stages</h2>
              <ol className="stage-list">
                {STAGES.map((stage, index) => {
                  const stageState = !completed && !failed
                    ? index === 0
                      ? 'current'
                      : 'queued'
                    : completed
                      ? 'completed'
                      : index === 0
                        ? 'failed'
                        : 'queued'
                  return (
                    <li key={stage} data-state={stageState} aria-current={stageState === 'current' ? 'step' : undefined}>
                      <span className="stage-list__icon" aria-hidden="true">
                        {stageState === 'completed' ? '✓' : stageState === 'current' ? '◐' : stageState === 'failed' ? '⬣' : '○'}
                      </span>
                      {stage}
                    </li>
                  )
                })}
              </ol>
            </section>

            <section className="card" aria-label="Findings">
              <h2 className="section-title">Findings</h2>
              {findings.length === 0 ? (
                <p className="muted">No findings recorded for this analysis.</p>
              ) : (
                <div className="data-list">
                  {findings.map((finding, index) => (
                    <div className="data-list__row" key={`${finding.kind ?? 'finding'}-${index}`}>
                      <div className="data-list__main">
                        <div className="data-list__title">{finding.kind ?? 'Unknown'}</div>
                        <div className="data-list__sub">
                          {findingValue(finding)} · {findingStateLabel(finding)}
                        </div>
                      </div>
                      {finding.confidence !== undefined && <ConfidenceBadge confidence={finding.confidence} />}
                      {finding.evidence && finding.evidence.length > 0 && (
                        <button
                          type="button"
                          className="btn btn--ghost btn--sm"
                          onClick={() => setEvidenceFor(finding)}
                          aria-label={`View ${finding.evidence.length} evidence records for ${finding.kind}`}
                        >
                          ⓘ {finding.evidence.length}
                        </button>
                      )}
                    </div>
                  ))}
                </div>
              )}
              {result && result.warnings && result.warnings.length > 0 && (
                <div style={{ marginTop: 12 }}>
                  {result.warnings.map((warning) => (
                    <div key={warning} className="muted">
                      {warning}
                    </div>
                  ))}
                </div>
              )}
            </section>
          </div>
        )}

        <EvidenceDrawer
          open={evidenceFor !== null}
          title={evidenceFor ? `Evidence — ${evidenceFor.kind ?? 'finding'}` : ''}
          evidence={evidenceFor?.evidence}
          onClose={() => setEvidenceFor(null)}
        />

        {analysis?.commit && (
          <p className="muted">
            Analysis {analysis.analysisId} · commit <span className="mono">{analysis.commit.slice(0, 7)}</span>
            {analysis.analyzerVersion ? ` · analyzer ${analysis.analyzerVersion}` : ''}
          </p>
        )}
      </div>
    </>
  )
}
