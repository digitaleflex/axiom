import { render, screen } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
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
    vi.restoreAllMocks()
  })

  it('redirects unauthenticated users to /login', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 401 })))
    renderGuarded('/dashboard')
    expect(await screen.findByText('login page')).toBeInTheDocument()
    expect(screen.queryByText('secret dashboard')).not.toBeInTheDocument()
  })

  it('stores the canonical return path for post-login navigation', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 401 })))
    renderGuarded('/dashboard')
    await screen.findByText('login page')
    expect(window.sessionStorage.getItem(RETURN_PATH_STORAGE_KEY)).toBe('/dashboard')
  })

  it('renders children when the session cookie is valid', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ id: 'usr_1', name: 'Jane', csrfToken: 'csrf_abc' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      ),
    )
    renderGuarded('/dashboard')
    expect(await screen.findByText('secret dashboard')).toBeInTheDocument()
  })
})
