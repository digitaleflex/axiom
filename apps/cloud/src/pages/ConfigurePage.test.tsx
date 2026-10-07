import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { clearCsrfToken, setCsrfToken } from '../auth/session'
import { ToastProvider } from '../components/system'
import { ConfigurePage } from './ConfigurePage'

const CSRF = 'csrf_test_token'

const PROFILE = {
  schemaVersion: 1,
  version: 3,
  status: 'ready',
  source: { ref: 'main', commit: 'abc1234' },
  configuration: [{ name: 'DATABASE_URL', required: true, secret: true }],
  port: { value: 3000 },
  healthCheck: { value: { type: 'http', path: '/' } },
  startCommand: { value: 'pnpm start' },
}

/** A stored value; includes a `value` field the real API never returns. */
const STORED_CONFIG = {
  items: [
    {
      name: 'DATABASE_URL',
      secret: true,
      isSet: true,
      updatedAt: '2026-10-07T10:00:00Z',
      value: 'postgres://leaked-value',
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

/**
 * Stubs global fetch with a path-routing handler and records every call so
 * tests can assert on method, URL, headers (e.g. X-CSRF-Token) and body.
 */
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

/** Routes every endpoint ConfigurePage consumes; tests override per case. */
function routeConfigure(path: string, method: string): Response {
  if (path === '/api/v1/applications/app_1' && method === 'GET') {
    return json({ id: 'app_1', name: 'My App' })
  }
  if (path === '/api/v1/applications/app_1/profile' && method === 'GET') return json(PROFILE)
  if (path === '/api/v1/servers' && method === 'GET') {
    return json({ items: [{ id: 'srv_1', name: 'srv-eu-1', status: 'ready' }] })
  }
  if (path === '/api/v1/applications/app_1/configuration' && method === 'GET') return json(STORED_CONFIG)
  if (path.startsWith('/api/v1/applications/app_1/configuration/') && method === 'PUT') return noContent()
  if (path.startsWith('/api/v1/applications/app_1/configuration/') && method === 'DELETE') return noContent()
  return json({ error: { code: 'INTERNAL_ERROR', message: `unmocked ${method} ${path}` } }, 500)
}

function renderConfigurePage() {
  return render(
    <MemoryRouter initialEntries={['/apps/app_1/setup/configure']}>
      <ToastProvider>
        <Routes>
          <Route path="/apps/:applicationId/setup/configure" element={<ConfigurePage />} />
        </Routes>
      </ToastProvider>
    </MemoryRouter>,
  )
}

describe('ConfigurePage — configuration values (#126)', () => {
  beforeEach(() => {
    setCsrfToken(CSRF)
  })

  afterEach(() => {
    clearCsrfToken()
    vi.unstubAllGlobals()
  })

  it('renders stored values as masked metadata only — never the value', async () => {
    mockApi(routeConfigure)
    renderConfigurePage()

    expect(await screen.findByText('DATABASE_URL')).toBeInTheDocument()
    expect(await screen.findByText('•••••••• set')).toBeInTheDocument()
    expect(screen.getByText(/required/)).toBeInTheDocument()
    // The API response carries a `value` field (which the real endpoint never
    // returns); the UI must never render it anywhere.
    expect(screen.queryByText('postgres://leaked-value')).not.toBeInTheDocument()
    // Deploy-time injection notice is visible.
    expect(screen.getByText('Values are injected at deploy time')).toBeInTheDocument()
  })

  it('saves a replaced value via PUT with the CSRF header and never echoes it', async () => {
    const { calls } = mockApi(routeConfigure)
    renderConfigurePage()

    fireEvent.click(await screen.findByRole('button', { name: 'Replace' }))
    const input = await screen.findByLabelText('DATABASE_URL')
    fireEvent.change(input, { target: { value: 'postgres://new-value' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      const put = calls.find((c) => c.method === 'PUT')
      expect(put).toBeDefined()
      expect(put?.path).toBe('/api/v1/applications/app_1/configuration/DATABASE_URL')
      expect(put?.headers['x-csrf-token']).toBe(CSRF)
      expect(put?.body).toEqual({ value: 'postgres://new-value', secret: true })
    })
    // After save the row returns to the masked "set" state; the typed value
    // is never rendered.
    expect(await screen.findByText('•••••••• set')).toBeInTheDocument()
    expect(screen.queryByText('postgres://new-value')).not.toBeInTheDocument()
  })

  it('gates delete behind confirmation, then calls DELETE with the CSRF header', async () => {
    const { calls } = mockApi(routeConfigure)
    renderConfigurePage()

    fireEvent.click(await screen.findByRole('button', { name: 'Remove' }))
    // Gating: arming the button must not call the API yet.
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false)

    fireEvent.click(screen.getByRole('button', { name: 'Confirm remove' }))
    await waitFor(() => {
      const del = calls.find((c) => c.method === 'DELETE')
      expect(del).toBeDefined()
      expect(del?.path).toBe('/api/v1/applications/app_1/configuration/DATABASE_URL')
      expect(del?.headers['x-csrf-token']).toBe(CSRF)
    })
  })

  it('adds a new variable via PUT with the CSRF header', async () => {
    const { calls } = mockApi(routeConfigure)
    renderConfigurePage()

    fireEvent.change(await screen.findByLabelText('New variable name'), { target: { value: 'API_KEY' } })
    const input = await screen.findByLabelText('API_KEY')
    fireEvent.change(input, { target: { value: 'secret-key-123' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => {
      const put = calls.find((c) => c.method === 'PUT')
      expect(put).toBeDefined()
      expect(put?.path).toBe('/api/v1/applications/app_1/configuration/API_KEY')
      expect(put?.headers['x-csrf-token']).toBe(CSRF)
      expect(put?.body).toEqual({ value: 'secret-key-123', secret: true })
    })
    expect(screen.queryByText('secret-key-123')).not.toBeInTheDocument()
  })

  it('rejects invalid variable names without calling the API', async () => {
    const { calls } = mockApi(routeConfigure)
    renderConfigurePage()

    fireEvent.change(await screen.findByLabelText('New variable name'), { target: { value: 'not-valid' } })
    expect(await screen.findByText('Names match ^[A-Z_][A-Z0-9_]*$')).toBeInTheDocument()
    expect(calls.some((c) => c.method === 'PUT')).toBe(false)
  })
})
