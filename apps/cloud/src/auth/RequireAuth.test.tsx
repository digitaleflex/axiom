import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it } from 'vitest'
import { AuthProvider } from './AuthContext'
import { RequireAuth } from './RequireAuth'
import { RETURN_PATH_STORAGE_KEY } from './session'

function renderGuarded(initialPath: string) {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <AuthProvider>
        <Routes>
          <Route
            path="/dashboard"
            element={
              <RequireAuth>
                <div>secret dashboard</div>
              </RequireAuth>
            }
          />
          <Route path="/login" element={<div>login page</div>} />
        </Routes>
      </AuthProvider>
    </MemoryRouter>,
  )
}

describe('RequireAuth', () => {
  afterEach(() => {
    window.sessionStorage.clear()
  })

  it('redirects unauthenticated users to /login', () => {
    renderGuarded('/dashboard')
    expect(screen.queryByText('secret dashboard')).not.toBeInTheDocument()
    expect(screen.getByText('login page')).toBeInTheDocument()
  })

  it('stores the canonical return path for post-login navigation', () => {
    renderGuarded('/dashboard')
    expect(window.sessionStorage.getItem(RETURN_PATH_STORAGE_KEY)).toBe('/dashboard')
  })

  it('renders children when a token is present', async () => {
    window.sessionStorage.setItem('axiom.token', 'test-token')
    renderGuarded('/dashboard')
    // No API is available in jsdom, so /auth/me fails as a network error and the
    // interim token is trusted optimistically.
    expect(await screen.findByText('secret dashboard')).toBeInTheDocument()
  })
})
