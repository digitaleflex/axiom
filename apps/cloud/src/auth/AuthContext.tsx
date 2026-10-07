import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { setUnauthorizedHandler } from '../api/client'
import { ApiError } from '../api/errors'
import {
  getMe,
  login as apiLogin,
  logout as apiLogout,
  register as apiRegister,
} from '../api/resources'
import type { User } from '../api/types'
import { clearCsrfToken, clearReturnPath, setCsrfToken } from './session'

/**
 * Session-based auth boundary (#125). The Engine sets an HttpOnly session
 * cookie; the console keeps only the CSRF token in memory and re-hydrates it
 * via `GET /auth/me` on boot (the cookie persists across refreshes).
 * Transport failures are treated optimistically so an unreachable Engine does
 * not sign the user out; a 401 anywhere clears the session.
 */
export type AuthStatus = 'loading' | 'authenticated' | 'anonymous'

export interface AuthContextValue {
  status: AuthStatus
  user: User | null
  signIn: (email: string, password: string) => Promise<void>
  signUp: (email: string, password: string, name?: string) => Promise<void>
  signOut: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>('loading')
  const [user, setUser] = useState<User | null>(null)

  const becomeAnonymous = useCallback(() => {
    clearCsrfToken()
    clearReturnPath()
    setUser(null)
    setStatus('anonymous')
  }, [])

  // Any 401 from the API client signs the user out (system §4).
  useEffect(() => {
    setUnauthorizedHandler(becomeAnonymous)
    return () => setUnauthorizedHandler(null)
  }, [becomeAnonymous])

  // Re-hydrate the session (cookie + CSRF token) on boot.
  useEffect(() => {
    let active = true
    getMe()
      .then((me) => {
        if (!active) return
        setCsrfToken(me.csrfToken)
        setUser({ id: me.id, name: me.name })
        setStatus('authenticated')
      })
      .catch((error: unknown) => {
        if (!active) return
        if (error instanceof ApiError && error.isUnauthorized) {
          becomeAnonymous()
          return
        }
        // Network / 503 with no known session: not authenticated.
        becomeAnonymous()
      })
    return () => {
      active = false
    }
  }, [becomeAnonymous])

  const startSession = useCallback(async (result: { user: User; csrfToken: string }) => {
    setCsrfToken(result.csrfToken)
    setUser(result.user)
    setStatus('authenticated')
  }, [])

  const signIn = useCallback(
    async (email: string, password: string) => {
      await startSession(await apiLogin(email, password))
    },
    [startSession],
  )

  const signUp = useCallback(
    async (email: string, password: string, name?: string) => {
      await startSession(await apiRegister(email, password, name))
    },
    [startSession],
  )

  const signOut = useCallback(async () => {
    try {
      await apiLogout()
    } catch {
      // best-effort: always clear locally
    }
    becomeAnonymous()
  }, [becomeAnonymous])

  const value = useMemo<AuthContextValue>(
    () => ({ status, user, signIn, signUp, signOut }),
    [status, user, signIn, signUp, signOut],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext)
  if (!context) throw new Error('useAuth must be used within an AuthProvider')
  return context
}
