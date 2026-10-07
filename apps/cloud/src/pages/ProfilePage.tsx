import { useState, type ReactNode } from 'react'
import { Link, useParams } from 'react-router-dom'
import { getApplication, getProfile, putProfileOverrides } from '../api/resources'
import type { Profile, ProfileValue } from '../api/types'
import { ApiError } from '../api/errors'
import { ConfidenceBadge } from '../components/ConfidenceBadge'
import { PageHeader } from '../components/shell/PageHeader'
import { ProvenanceTag } from '../components/ProvenanceTag'
import { SetupStepper } from '../components/SetupStepper'
import { TechnicalValue } from '../components/TechnicalValue'
import { EmptyState, ErrorPanel, InlineNotice, SkeletonLines, useToast } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { routes } from '../routes/builders'
import { canContinueFromProfile } from '../workflow/blockingGate'

function FieldRow({
  label,
  value,
  provenance,
  confidence,
  mono = true,
  children,
}: {
  label: string
  value: ReactNode
  provenance?: ProfileValue<unknown>['provenance']
  confidence?: number
  mono?: boolean
  children?: ReactNode
}) {
  return (
    <div className="profile-field">
      <dt className="profile-field__label">{label}</dt>
      <dd className="profile-field__value">
        <span className={mono ? 'mono' : ''}>{value}</span>
        {provenance && <ProvenanceTag provenance={provenance} />}
        {confidence !== undefined && <ConfidenceBadge confidence={confidence} />}
        {children}
      </dd>
    </div>
  )
}

function valueText(value: unknown): string {
  if (value === undefined || value === null) return '—'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

/**
 * Application Profile screen (application-profile §1). Setup step 2.
 *
 * Shows Axiom's complete interpretation with provenance per field, lets the
 * user correct values via overrides (PUT), and gates Continue on unresolved
 * uncertainty. Only `ready` profiles can continue.
 */
export function ProfilePage() {
  const { applicationId } = useParams<{ applicationId: string }>()
  const { push } = useToast()
  const [overrides, setOverrides] = useState<Record<string, string>>({})
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<ApiError | null>(null)

  const appState = useAsync(
    (signal) => (applicationId ? getApplication(applicationId, { signal }) : Promise.reject(new Error('missing id'))),
    [applicationId],
  )

  const state = useAsync<Profile | null>(
    (signal) => (applicationId ? getProfile(applicationId, { signal }) : Promise.reject(new Error('missing id'))),
    [applicationId],
  )

  const profile = state.data
  const gate = canContinueFromProfile(profile)

  const setOverride = (field: string, value: string) => {
    setOverrides((prev) => {
      const next = { ...prev }
      if (value === '') delete next[field]
      else next[field] = value
      return next
    })
  }

  const saveOverrides = async () => {
    if (!applicationId) return
    setSaving(true)
    setSaveError(null)
    try {
      const body: Record<string, unknown> = {}
      for (const [field, value] of Object.entries(overrides)) {
        if (field === 'port') {
          const port = Number(value)
          if (Number.isInteger(port) && port > 0 && port <= 65535) body.port = port
        } else {
          body[field] = value
        }
      }
      await putProfileOverrides(applicationId, body)
      push({ variant: 'success', title: 'Profile updated', body: 'Overrides saved as profile overrides.' })
      setOverrides({})
      state.reload()
    } catch (cause) {
      const error = cause instanceof ApiError ? cause : ApiError.network()
      setSaveError(error)
      push({ variant: 'failure', title: "Couldn't save overrides", body: error.message })
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <PageHeader
        breadcrumbs={[
          { label: 'Workspace', to: routes.dashboard() },
          { label: appState.data?.name ?? applicationId ?? 'Application' },
          { label: 'Setup', current: true },
        ]}
        title="Application profile"
        meta={
          profile?.source?.ref
            ? `${profile.source.ref}${profile.source.commit ? ` · ${profile.source.commit.slice(0, 7)}` : ''} · profile v${profile.version ?? '—'}`
            : undefined
        }
        actions={
          <Link
            className="btn btn--primary"
            to={routes.setup({ applicationId: applicationId ?? '', step: 'server' })}
            aria-disabled={!gate.allowed}
            style={{ opacity: gate.allowed ? 1 : 0.5, pointerEvents: gate.allowed ? 'auto' : 'none' }}
          >
            Continue ▸
          </Link>
        }
        tabs={applicationId ? <SetupStepper applicationId={applicationId} current="profile" /> : undefined}
      />
      <div className="content__body stack">
        {state.status === 'loading' && <SkeletonLines lines={6} />}

        {state.status === 'error' && state.error && (
          <ErrorPanel error={state.error} objectName="this profile" onRetry={state.reload} />
        )}

        {state.status === 'success' && !profile && (
          <EmptyState
            variant="no-results"
            title="No profile yet"
            description="Run an analysis first — the profile is built from its findings."
            primaryAction={
              <Link className="btn btn--secondary" to={routes.setup({ applicationId: applicationId ?? '', step: 'analysis' })}>
                Go to analysis
              </Link>
            }
          />
        )}

        {profile && profile.status === 'unsupported' && (
          <InlineNotice variant="failed" title="This repository can't be deployed by Axiom V0.1 yet">
            {profile.unsupported?.message}
            {profile.unsupported?.alternatives && profile.unsupported.alternatives.length > 0 && (
              <div style={{ marginTop: 4 }}>Supported: {profile.unsupported.alternatives.join(', ')}</div>
            )}
          </InlineNotice>
        )}

        {profile && profile.status === 'needs_review' && (
          <InlineNotice variant="warning" title="Axiom needs you to confirm details before continuing">
            <ul style={{ margin: '4px 0 0', paddingLeft: 20 }}>
              {(profile.blocking ?? []).map((issue, index) => (
                <li key={`${issue.field ?? 'issue'}-${index}`}>{issue.message}</li>
              ))}
            </ul>
          </InlineNotice>
        )}

        {profile && profile.summary && (
          <section className="card">
            <div className="muted">Summary</div>
            <p style={{ margin: '4px 0 0' }}>{profile.summary}</p>
          </section>
        )}

        {profile && (
          <div className="grid grid--cards">
            <section className="card" aria-label="Runtime">
              <h2 className="section-title">Runtime</h2>
              <dl className="profile-fields">
                <FieldRow label="Language" value={valueText(profile.language?.value)} provenance={profile.language?.provenance} confidence={profile.language?.confidence} />
                <FieldRow label="Runtime version" value={valueText(profile.runtimeVersion?.value)} provenance={profile.runtimeVersion?.provenance} confidence={profile.runtimeVersion?.confidence} />
                <FieldRow label="Framework" value={valueText(profile.framework?.value)} provenance={profile.framework?.provenance} confidence={profile.framework?.confidence} />
              </dl>
            </section>

            <section className="card" aria-label="Build and start">
              <h2 className="section-title">Build &amp; start</h2>
              <dl className="profile-fields">
                <FieldRow label="Package manager" value={valueText(profile.packageManager?.value)} provenance={profile.packageManager?.provenance} confidence={profile.packageManager?.confidence} />
                <FieldRow label="Build command" value={valueText(profile.buildCommand?.value)} provenance={profile.buildCommand?.provenance} confidence={profile.buildCommand?.confidence} />
                <FieldRow label="Start command" value={valueText(profile.startCommand?.value)} provenance={profile.startCommand?.provenance} confidence={profile.startCommand?.confidence} />
              </dl>
            </section>

            <section className="card" aria-label="Networking and health">
              <h2 className="section-title">Networking &amp; health</h2>
              <dl className="profile-fields">
                <FieldRow
                  label="Port"
                  value={valueText(profile.port?.value)}
                  provenance={profile.port?.provenance}
                  confidence={profile.port?.confidence}
                >
                  <span className="profile-field__edit">
                    <input
                      className="field__input field__input--inline"
                      type="number"
                      min={1}
                      max={65535}
                      placeholder="Override"
                      value={overrides.port ?? ''}
                      onChange={(event) => setOverride('port', event.target.value)}
                      aria-label="Override port"
                    />
                  </span>
                </FieldRow>
                <FieldRow
                  label="Health check"
                  value={profile.healthCheck?.value ? `${profile.healthCheck.value.type ?? 'http'} ${profile.healthCheck.value.path ?? '/'}` : '—'}
                  provenance={profile.healthCheck?.provenance}
                  confidence={profile.healthCheck?.confidence}
                />
                <FieldRow label="Container strategy" value={valueText(profile.containerStrategy?.value)} provenance={profile.containerStrategy?.provenance} />
              </dl>
            </section>
          </div>
        )}

        {profile && profile.configuration && profile.configuration.length > 0 && (
          <section className="card" aria-label="Configuration requirements">
            <h2 className="section-title">Configuration requirements</h2>
            <div className="data-list">
              {profile.configuration.map((req) => (
                <div className="data-list__row" key={req.name}>
                  <div className="data-list__main">
                    <div className="data-list__title mono">{req.name}</div>
                    <div className="data-list__sub">
                      {req.required ? 'Required' : 'Optional'}
                      {req.secret ? ' · Secret' : ''}
                    </div>
                  </div>
                </div>
              ))}
            </div>
            <p className="muted">Values are set at the Configure step — names only are shown here, never values.</p>
          </section>
        )}

        {profile && Object.keys(overrides).length > 0 && (
          <div className="row">
            <button type="button" className="btn btn--primary" onClick={() => void saveOverrides()} disabled={saving}>
              {saving ? 'Saving…' : 'Save overrides'}
            </button>
            <button type="button" className="btn btn--ghost" onClick={() => setOverrides({})} disabled={saving}>
              Discard
            </button>
          </div>
        )}

        {saveError && (
          <InlineNotice variant="failed" title="Couldn't save overrides">
            {saveError.message}
          </InlineNotice>
        )}

        {profile && !gate.allowed && (
          <p className="muted" role="note">
            Continue is blocked: {gate.reasons.join(' ')}
          </p>
        )}

        {profile?.source?.commit && (
          <p className="muted">
            <TechnicalValue value={profile.source.commit} /> · analysis {profile.analysisId ?? '—'}
          </p>
        )}
      </div>
    </>
  )
}
