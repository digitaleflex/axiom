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

type Mode = 'signin' | 'signup'

export function LoginPage() {
  const { status, signIn, signUp } = useAuth()
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const returnTo = sanitizeReturnPath(searchParams.get('returnTo') ?? getReturnPath())

  const [mode, setMode] = useState<Mode>('signin')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [name, setName] = useState('')
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
      if (mode === 'signin') {
        await signIn(email, password)
      } else {
        await signUp(email, password, name || undefined)
      }
      clearReturnPath()
      navigate(returnTo, { replace: true })
    } catch (cause) {
      setError(cause instanceof ApiError ? cause : ApiError.network())
    } finally {
      setSubmitting(false)
    }
  }

  const cannotSubmit = submitting || email.trim() === '' || password === ''

  return (
    <div className="login">
      <form className="login__card" onSubmit={onSubmit}>
        <div className="login__brand">
          <span className="sidebar__mark" aria-hidden="true">
            A
          </span>
          AXIOM
        </div>
        <h1 className="login__title">
          {mode === 'signin' ? 'Sign in to Cloud Console' : 'Create your Axiom account'}
        </h1>
        <p className="login__body">
          Your session is a secure HttpOnly cookie; the only credential kept in this tab is the
          CSRF token, held in memory.
        </p>

        {error && (
          <InlineNotice variant="failed" title={mode === 'signin' ? 'Sign-in failed' : 'Sign-up failed'}>
            {error.isUnauthorized ? 'That email or password was rejected.' : error.message}
          </InlineNotice>
        )}

        {mode === 'signup' && (
          <div className="field">
            <label className="field__label" htmlFor="name">
              Name (optional)
            </label>
            <input
              id="name"
              className="field__input"
              type="text"
              autoComplete="name"
              value={name}
              onChange={(event) => setName(event.target.value)}
            />
          </div>
        )}

        <div className="field">
          <label className="field__label" htmlFor="email">
            Email
          </label>
          <input
            id="email"
            className="field__input"
            type="email"
            autoComplete="email"
            required
            value={email}
            onChange={(event) => setEmail(event.target.value)}
          />
        </div>

        <div className="field">
          <label className="field__label" htmlFor="password">
            Password
          </label>
          <input
            id="password"
            className="field__input"
            type="password"
            autoComplete={mode === 'signin' ? 'current-password' : 'new-password'}
            required
            minLength={8}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </div>

        <button className="btn btn--primary" type="submit" disabled={cannotSubmit}>
          {submitting ? 'Please wait…' : mode === 'signin' ? 'Sign in' : 'Create account'}
        </button>

        <button
          type="button"
          className="btn btn--ghost"
          onClick={() => {
            setMode(mode === 'signin' ? 'signup' : 'signin')
            setError(null)
          }}
        >
          {mode === 'signin' ? 'Need an account? Sign up' : 'Already have an account? Sign in'}
        </button>
      </form>
    </div>
  )
}
