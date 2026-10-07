/**
 * INTERIM authentication session store.
 *
 * Until #125 (identity provider), the Engine accepts a single bearer token
 * configured as `AXIOM_API_TOKEN` (api-contract §2). The console stores that
 * token in `sessionStorage` under the key `axiom.token` — NOT localStorage, so
 * it dies with the tab, and never in the URL.
 *
 * This is deliberately a small, isolated module: when #125 lands, replace this
 * implementation (and AuthContext) with the real provider. Nothing else should
 * read the storage key directly.
 *
 * Prohibited by docs/design/handoff/README.md §6.8: secrets/tokens in URLs,
 * logs or analytics. We only ever put the token in the Authorization header.
 */

export const TOKEN_STORAGE_KEY = 'axiom.token'
export const RETURN_PATH_STORAGE_KEY = 'axiom.returnPath'

function storage(): Storage | null {
  try {
    return typeof window !== 'undefined' ? window.sessionStorage : null
  } catch {
    return null
  }
}

export function getToken(): string | null {
  return storage()?.getItem(TOKEN_STORAGE_KEY) ?? null
}

export function setToken(token: string): void {
  storage()?.setItem(TOKEN_STORAGE_KEY, token)
}

export function clearToken(): void {
  storage()?.removeItem(TOKEN_STORAGE_KEY)
}

export function hasToken(): boolean {
  return getToken() !== null
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
