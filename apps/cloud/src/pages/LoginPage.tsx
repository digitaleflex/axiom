import { useState } from 'react'
import { Navigate, useNavigate, useSearchParams } from 'react-router-dom'
import { ApiError } from '../api/errors'
import { useAuth } from '../auth/AuthContext'
import { clearReturnPath, getReturnPath } from '../auth/session'
import { FullPageLoading, InlineNotice } from '../components/system'

/** Only same-origin relative paths are honored (handoff §5.5). */
export function sanitizeReturnPath(value: string | null): string {
  if (!value) return '/dashboard'
  if (!value.startsWith('/') || value.startsWith('//')) return '/dashboard'
  if (value.startsWith('/login')) return '/dashboard'
  return value
}

export function LoginPage() {
  const { status, signIn } = useAuth()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const returnTo = sanitizeReturnPath(searchParams.get('returnTo') ?? getReturnPath())

  const [token, setToken] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)

  if (status === 'authenticated') {
    return <Navigate to={returnTo} replace />
  }

  if (status === 'loading') {
    return <FullPageLoading label="Checking your session" />
  }

  const onSubmit = async (event: React.FormEvent) => {
    event.preventDefault()
    setSubmitting(true)
    setError(null)
    try {
      await signIn(token)
      clearReturnPath()
      navigate(returnTo, { replace: true })
    } catch (cause) {
      setError(cause instanceof ApiError ? cause : ApiError.network())
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="login">
      <form className="login__card" onSubmit={onSubmit}>
        <div className="login__brand">
          <span className="sidebar__mark" aria-hidden="true">
            A
          </span>
          AXIOM
        </div>
        <h1 className="login__title">Sign in to Cloud Console</h1>
        <p className="login__body">
          Authentication is interim until #125. Paste the Engine bearer token (<span className="mono">AXIOM_API_TOKEN</span>
          ). It is kept in this tab&rsquo;s session storage and sent as{' '}
          <span className="mono">Authorization: Bearer</span>.
        </p>

        {error && (
          <InlineNotice variant="failed" title="Sign-in failed">
            {error.isUnauthorized ? 'That token was rejected by the Engine.' : error.message}
          </InlineNotice>
        )}

        <div className="field">
          <label className="field__label" htmlFor="token">
            Bearer token
          </label>
          <input
            id="token"
            className="field__input"
            type="password"
            autoComplete="off"
            spellCheck={false}
            value={token}
            onChange={(event) => setToken(event.target.value)}
            required
          />
        </div>

        <button className="btn btn--primary" type="submit" disabled={submitting || token.trim() === ''}>
          {submitting ? 'Signing in…' : 'Sign in'}
        </button>
      </form>
    </div>
  )
}
