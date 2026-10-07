import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { AuthProvider } from '../auth/AuthContext'
import { ToastProvider } from '../components/system'
import { SettingsPage } from './SettingsPage'

const CSRF = 'csrf_test_token'

const ME = { id: 'usr_1', name: 'Jane Doe', csrfToken: CSRF }

/** Sessions payload includes `token` fields the real API never returns. */
const SESSIONS = {
  items: [
    {
      id: 'ses_1',
      userAgent: 'Chrome · macOS',
      ip: '203.0.113.10',
      createdAt: '2026-10-01T08:00:00Z',
      lastSeenAt: '2026-10-07T09:00:00Z',
      expiresAt: '2026-10-08T08:00:00Z',
      current: true,
      token: 'current-session-secret',
    },
    {
      id: 'ses_2',
      userAgent: 'Firefox · Linux',
      ip: '198.51.100.7',
      createdAt: '2026-10-05T12:00:00Z',
      lastSeenAt: '2026-10-06T18:00:00Z',
      expiresAt: '2026-10-07T12:00:00Z',
      token: 'other-session-secret',
    },
  ],
}

interface RecordedCall {
  path: string
  method: string
  headers: Record<string, string>
  body: unknown
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}

function noContent(): Response {
  return new Response(null, { status: 204 })
}

function mockApi(handler: (path: string, method: string, body: unknown) => Response | Promise<Response>) {
  const calls: RecordedCall[] = []
  const fn = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input).replace(/^https?:\/\/[^/]+/, '')
    const method = init?.method ?? 'GET'
    const headers: Record<string, string> = {}
    if (init?.headers) {
      for (const [key, value] of Object.entries(init.headers)) headers[key.toLowerCase()] = String(value)
    }
    let body: unknown
    if (typeof init?.body === 'string') {
      try {
        body = JSON.parse(init.body)
      } catch {
        body = undefined
      }
    }
    calls.push({ path, method, headers, body })
    return Promise.resolve(handler(path, method, body))
  })
  vi.stubGlobal('fetch', fn)
  return { fn, calls }
}

function routeSettings(path: string, method: string): Response {
  if (path === '/api/v1/auth/me' && method === 'GET') return json(ME)
  if (path === '/api/v1/auth/sessions' && method === 'GET') return json(SESSIONS)
  if (path === '/api/v1/auth/sessions' && method === 'DELETE') return noContent()
  if (path.startsWith('/api/v1/auth/sessions/') && method === 'DELETE') return noContent()
  return json({ error: { code: 'INTERNAL_ERROR', message: `unmocked ${method} ${path}` } }, 500)
}

function renderSettings(section: string) {
  return render(
    <MemoryRouter initialEntries={[`/settings/${section}`]}>
      <ToastProvider>
        <AuthProvider>
          <Routes>
            <Route path="/settings/:section" element={<SettingsPage />} />
          </Routes>
        </AuthProvider>
      </ToastProvider>
    </MemoryRouter>,
  )
}

describe('SettingsPage — sessions (#125)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('lists active sessions, marks the current one and never renders tokens', async () => {
    mockApi(routeSettings)
    renderSettings('sessions')

    expect(await screen.findByText('Authentication & sessions')).toBeInTheDocument()
    expect(screen.getByText('This device')).toBeInTheDocument()
    expect(screen.getByText('current session')).toBeInTheDocument()
    expect(screen.getByText('Chrome · macOS')).toBeInTheDocument()
    expect(screen.getByText('Firefox · Linux')).toBeInTheDocument()
    expect(screen.getByText('203.0.113.10')).toBeInTheDocument()
    // Token material must never reach the UI, even if a response carried it.
    expect(screen.queryByText('current-session-secret')).not.toBeInTheDocument()
    expect(screen.queryByText('other-session-secret')).not.toBeInTheDocument()
  })

  it('gates revoke behind confirmation, then calls DELETE /auth/sessions/{id} with CSRF', async () => {
    const { calls } = mockApi(routeSettings)
    renderSettings('sessions')

    fireEvent.click(await screen.findByRole('button', { name: 'Revoke' }))
    // Gating: arming must not revoke yet.
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: 'Confirm revoke' }))
    await waitFor(() => {
      const del = calls.find((c) => c.method === 'DELETE')
      expect(del).toBeDefined()
      expect(del?.path).toBe('/api/v1/auth/sessions/ses_2')
      expect(del?.headers['x-csrf-token']).toBe(CSRF)
    })
  })

  it('gates "sign out other sessions" behind confirmation, then calls DELETE /auth/sessions with CSRF', async () => {
    const { calls } = mockApi(routeSettings)
    renderSettings('sessions')

    fireEvent.click(await screen.findByRole('button', { name: 'Sign out other sessions' }))
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: 'Confirm sign out' }))
    await waitFor(() => {
      const del = calls.find((c) => c.method === 'DELETE')
      expect(del).toBeDefined()
      expect(del?.path).toBe('/api/v1/auth/sessions')
      expect(del?.headers['x-csrf-token']).toBe(CSRF)
    })
  })

  it('cancelling the confirmation disarms without calling the API', async () => {
    const { calls } = mockApi(routeSettings)
    renderSettings('sessions')

    fireEvent.click(await screen.findByRole('button', { name: 'Revoke' }))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    // The action is disarmed; the plain button is back and nothing was called.
    expect(screen.getByRole('button', { name: 'Revoke' })).toBeInTheDocument()
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)
  })

  it('shows the account section from GET /auth/me', async () => {
    mockApi(routeSettings)
    renderSettings('account')

    expect(await screen.findByText('Account')).toBeInTheDocument()
    expect(screen.getByText('Jane Doe')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Sign out' })).toBeInTheDocument()
  })
})
