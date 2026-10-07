import { useState } from 'react'
import { Link, Navigate, useNavigate, useParams } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { listSessions, revokeOtherSessions, revokeSession } from '../api/resources'
import { ApiError } from '../api/errors'
import { ConfirmButton } from '../components/ConfirmButton'
import { PageHeader } from '../components/shell/PageHeader'
import { ErrorPanel, InlineNotice, SkeletonLines, useToast } from '../components/system'
import { useAsync } from '../hooks/useAsync'
import { routes } from '../routes/builders'

/**
 * Settings (screens/settings §3). Only sections whose backend contract exists
 * are rendered (§3: unsupported contracts are not shown, not disabled).
 * V0.1 renders Account (`GET /auth/me`) and Sessions (#125).
 */
const SECTIONS = [
  { id: 'account', label: 'Account' },
  { id: 'sessions', label: 'Sessions' },
] as const

type SectionId = (typeof SECTIONS)[number]['id']

function isSectionId(value: string | undefined): value is SectionId {
  return SECTIONS.some((s) => s.id === value)
}

/** Relative "last active" rendering; the Engine reports absolute timestamps. */
function relativeTime(iso: string | undefined): string {
  if (!iso) return '—'
  const then = Date.parse(iso)
  if (Number.isNaN(then)) return iso
  const seconds = Math.max(0, Math.round((Date.now() - then) / 1000))
  if (seconds < 60) return 'just now'
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes} min ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours} h ago`
  return `${Math.floor(hours / 24)} d ago`
}

function AccountSection() {
  const { user, signOut } = useAuth()
  return (
    <section className="card" aria-label="Account">
      <h2 className="section-title">Account</h2>
      <div className="data-list">
        <div className="data-list__row">
          <div className="data-list__main">
            <div className="data-list__title">Name</div>
            <div className="data-list__sub">{user?.name ?? '—'}</div>
          </div>
        </div>
        <div className="data-list__row">
          <div className="data-list__main">
            <div className="data-list__title">Signed in with</div>
            <div className="data-list__sub">Session cookie (HttpOnly)</div>
          </div>
        </div>
      </div>
      <div style={{ marginTop: 16 }}>
        <button type="button" className="btn btn--secondary" onClick={() => void signOut()}>
          Sign out
        </button>
      </div>
    </section>
  )
}

/**
 * Active sessions (#125, api-contract §2). The API never returns tokens or
 * CSRF secrets; the list renders device, location and last-active metadata
 * only. Revoking — one session or all others — is destructive and gated
 * behind a two-step confirmation.
 */
function SessionsSection() {
  const { push } = useToast()
  const sessionsState = useAsync((signal) => listSessions({ signal }), [])
  const [busy, setBusy] = useState(false)

  const sessions = sessionsState.data?.items ?? []
  const current = sessions.find((s) => s.current)
  const others = sessions.filter((s) => !s.current)

  const run = async (action: () => Promise<void>, successTitle: string, successBody: string) => {
    setBusy(true)
    try {
      await action()
      push({ variant: 'success', title: successTitle, body: successBody })
      sessionsState.reload()
    } catch (cause) {
      const error = cause instanceof ApiError ? cause : ApiError.network()
      push({ variant: 'failure', title: "Couldn't complete action", body: error.message })
    } finally {
      setBusy(false)
    }
  }

  return (
    <section className="card" aria-label="Sessions">
      <h2 className="section-title">Authentication &amp; sessions</h2>

      {sessionsState.status === 'error' && sessionsState.error && (
        <ErrorPanel error={sessionsState.error} objectName="sessions" onRetry={sessionsState.reload} />
      )}

      {sessionsState.status === 'loading' && <SkeletonLines lines={3} />}

      {sessionsState.status === 'success' && (
        <div className="stack" style={{ gap: 16 }}>
          {current && (
            <div className="data-list">
              <div className="data-list__row">
                <div className="data-list__main">
                  <div className="data-list__title">
                    This device <span className="provenance-tag">current session</span>
                  </div>
                  <div className="data-list__sub">
                    {current.userAgent ?? 'Unknown device'}
                    {current.ip ? ` · ${current.ip}` : ''} · last active {relativeTime(current.lastSeenAt)}
                  </div>
                </div>
              </div>
            </div>
          )}

          <div>
            <div className="muted" style={{ marginBottom: 8 }}>
              Other active sessions
            </div>
            {others.length === 0 ? (
              <p className="muted">No other active sessions.</p>
            ) : (
              <div className="data-list">
                {others.map((session) => (
                  <div className="data-list__row" key={session.id}>
                    <div className="data-list__main">
                      <div className="data-list__title">{session.userAgent ?? 'Unknown device'}</div>
                      <div className="data-list__sub">
                        {session.ip ? `${session.ip} · ` : ''}last active {relativeTime(session.lastSeenAt)}
                      </div>
                    </div>
                    <ConfirmButton
                      label="Revoke"
                      confirmLabel="Confirm revoke"
                      className="btn btn--destructive btn--sm"
                      disabled={busy}
                      onConfirm={() => void run(() => revokeSession(session.id), 'Session revoked.', `The session on ${session.userAgent ?? 'that device'} was revoked.`)}
                    />
                  </div>
                ))}
              </div>
            )}
          </div>

          <div>
            <ConfirmButton
              label="Sign out other sessions"
              confirmLabel="Confirm sign out"
              disabled={busy || others.length === 0}
              onConfirm={() => void run(() => revokeOtherSessions(), 'Signed out', 'All other sessions were revoked.')}
            />
            <p className="muted" style={{ marginTop: 8 }}>
              Revoking a session signs that device out immediately. This does not affect your current session.
            </p>
          </div>
        </div>
      )}

      <InlineNotice variant="info" title="Sessions are token-free in the UI">
        Axiom never returns session tokens or CSRF secrets to the console; they stay in HttpOnly cookies.
      </InlineNotice>
    </section>
  )
}

export function SettingsPage() {
  const { section } = useParams<{ section: string }>()
  const navigate = useNavigate()

  if (!isSectionId(section)) {
    return <Navigate to={routes.settings('account')} replace />
  }

  return (
    <>
      <PageHeader
        breadcrumbs={[{ label: 'Workspace' }, { label: 'Settings', to: routes.settings() }, { label: section, current: true }]}
        title="Settings"
      />
      <div className="content__body">
        <div className="settings-layout">
          <nav className="settings-nav" aria-label="Settings sections">
            <ul className="settings-nav__list">
              {SECTIONS.map((s) => (
                <li key={s.id}>
                  <Link
                    to={routes.settings(s.id)}
                    className="settings-nav__link"
                    aria-current={section === s.id ? 'page' : undefined}
                  >
                    {s.label}
                  </Link>
                </li>
              ))}
            </ul>
            <select
              className="settings-nav__select"
              value={section}
              onChange={(event) => navigate(routes.settings(event.target.value))}
              aria-label="Settings sections"
            >
              {SECTIONS.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.label}
                </option>
              ))}
            </select>
          </nav>
          <div className="settings-content stack">
            {section === 'account' ? <AccountSection /> : <SessionsSection />}
          </div>
        </div>
      </div>
    </>
  )
}
