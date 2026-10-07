/**
 * Client-side session state for the Axiom Cloud Console.
 *
 * The Engine authenticates the console with an HttpOnly session cookie (#125);
 * JavaScript never sees the session token. The only credential kept in memory
 * is the CSRF token returned by `POST /auth/login` (and re-hydrated by
 * `GET /auth/me`), which must accompany every mutating request in the
 * `X-CSRF-Token` header (double-submit, api-contract §2).
 *
 * It is deliberately memory-only: refreshing the page re-hydrates the CSRF
 * token from `GET /auth/me` because the cookie persists. Nothing here is
 * written to localStorage/sessionStorage. Return-path handling is the only
 * persisted piece and it is not a secret.
 */

export const RETURN_PATH_STORAGE_KEY = 'axiom.returnPath'

function storage(): Storage | null {
  try {
    return typeof window !== 'undefined' ? window.sessionStorage : null
  } catch {
    return null
  }
}

let csrfToken: string | null = null

/** Set (or clear) the CSRF token for the current session. */
export function setCsrfToken(token: string | null): void {
  csrfToken = token && token.trim() !== '' ? token : null
}

/** Current CSRF token, or null when signed out. */
export function getCsrfToken(): string | null {
  return csrfToken
}

export function clearCsrfToken(): void {
  csrfToken = null
}

/** Canonical return path captured when the guard redirects to /login. */
export function getReturnPath(): string | null {
  return storage()?.getItem(RETURN_PATH_STORAGE_KEY) ?? null
}

export function setReturnPath(path: string): void {
  storage()?.setItem(RETURN_PATH_STORAGE_KEY, path)
}

export function clearReturnPath(): void {
  storage()?.removeItem(RETURN_PATH_STORAGE_KEY)
}
