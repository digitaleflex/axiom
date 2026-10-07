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
import { getMe, logout as apiLogout } from '../api/resources'
import type { User } from '../api/types'
import { clearReturnPath, clearToken, getToken, setToken } from './session'

/**
 * INTERIM auth boundary (see auth/session.ts). Validates the stored token
 * against `GET /auth/me` when possible; treats transport failures optimistically
 * so an unreachable Engine does not sign the user out. A 401 anywhere clears
 * the session and flips to `anonymous`, which lets RequireAuth redirect.
 */
export type AuthStatus = 'loading' | 'authenticated' | 'anonymous'

export interface AuthContextValue {
  status: AuthStatus
  user: User | null
  token: string | null
  signIn: (token: string) => Promise<void>
  signOut: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>(() => (getToken() ? 'loading' : 'anonymous'))
  const [user, setUser] = useState<User | null>(null)
  const [token, setTokenState] = useState<string | null>(() => getToken())

  const becomeAnonymous = useCallback(() => {
    clearToken()
    clearReturnPath()
    setTokenState(null)
    setUser(null)
    setStatus('anonymous')
  }, [])

  // Any 401 from the API client signs the user out (system §4).
  useEffect(() => {
    setUnauthorizedHandler(becomeAnonymous)
    return () => setUnauthorizedHandler(null)
  }, [becomeAnonymous])

  // Validate an existing token on boot.
  useEffect(() => {
    const existing = getToken()
    if (!existing) {
      setStatus('anonymous')
      return
    }
    let active = true
    getMe()
      .then((me) => {
        if (!active) return
        setUser(me)
        setStatus('authenticated')
      })
      .catch((error: unknown) => {
        if (!active) return
        if (error instanceof ApiError && error.isUnauthorized) {
          becomeAnonymous()
          return
        }
        // Network / 503: keep the interim token, proceed optimistically.
        setStatus('authenticated')
      })
    return () => {
      active = false
    }
  }, [becomeAnonymous])

  const signIn = useCallback(async (rawToken: string) => {
    const next = rawToken.trim()
    if (!next) throw new ApiError({ code: 'INVALID_REQUEST', message: 'Enter a token.', status: 400 })
    setToken(next)
    setTokenState(next)
    try {
      const me = await getMe()
      setUser(me)
      setStatus('authenticated')
    } catch (error) {
      if (error instanceof ApiError && error.isUnauthorized) {
        clearToken()
        setTokenState(null)
        setStatus('anonymous')
        throw error
      }
      // Could not validate (Engine unreachable) — accept the interim token.
      setStatus('authenticated')
    }
  }, [])

  const signOut = useCallback(async () => {
    try {
      await apiLogout()
    } catch {
      // best-effort: always clear locally
    }
    becomeAnonymous()
  }, [becomeAnonymous])

  const value = useMemo<AuthContextValue>(
    () => ({ status, user, token, signIn, signOut }),
    [status, user, token, signIn, signOut],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext)
  if (!context) throw new Error('useAuth must be used within an AuthProvider')
  return context
}
