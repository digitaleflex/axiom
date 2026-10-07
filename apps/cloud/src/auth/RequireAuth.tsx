import { useEffect, type ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { FullPageLoading } from '../components/system/FullPageLoading'
import { useAuth } from './AuthContext'
import { setReturnPath } from './session'

/**
 * Route guard. Unauthenticated users are redirected to /login carrying the
 * canonical return path; the login screen restores it after sign-in.
 */
export function RequireAuth({ children }: { children: ReactNode }) {
  const { status } = useAuth()
  const location = useLocation()
  const returnTo = `${location.pathname}${location.search}`

  useEffect(() => {
    if (status === 'anonymous' && returnTo && !returnTo.startsWith('/login')) {
      setReturnPath(returnTo)
    }
  }, [status, returnTo])

  if (status === 'loading') {
    return <FullPageLoading label="Checking your session" />
  }

  if (status === 'anonymous') {
    const target =
      returnTo && !returnTo.startsWith('/login')
        ? `/login?returnTo=${encodeURIComponent(returnTo)}`
        : '/login'
    return <Navigate to={target} replace />
  }

  return <>{children}</>
}
