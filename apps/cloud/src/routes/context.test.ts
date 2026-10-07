import { describe, expect, it } from 'vitest'
import { parseRoute } from './context'
import { sanitizeReturnPath } from '../pages/LoginPage'

describe('parseRoute', () => {
  it('derives variant, active item and context from canonical routes', () => {
    const ctx = parseRoute('/apps/app_1/production/logs')
    expect(ctx.variant).toBe('application')
    expect(ctx.sidebarActive).toBe('logs')
    expect(ctx.applicationId).toBe('app_1')
    expect(ctx.environment).toBe('production')
    expect(ctx.unknown).toBe(false)
  })

  it('marks an unknown environment as Not Found instead of defaulting', () => {
    const ctx = parseRoute('/apps/app_1/qa/logs')
    expect(ctx.unknown).toBe(true)
  })

  it('recognizes the task-lane routes', () => {
    expect(parseRoute('/applications/app_1').applicationId).toBe('app_1')
    expect(parseRoute('/deployments/dep_1').deploymentId).toBe('dep_1')
    expect(parseRoute('/servers/srv_1').sidebarActive).toBe('servers')
  })
})

describe('sanitizeReturnPath', () => {
  it('only allows same-origin relative paths', () => {
    expect(sanitizeReturnPath('/repositories?q=web')).toBe('/repositories?q=web')
    expect(sanitizeReturnPath('//evil.example.com')).toBe('/dashboard')
    expect(sanitizeReturnPath('https://evil.example.com')).toBe('/dashboard')
    expect(sanitizeReturnPath(null)).toBe('/dashboard')
  })
})
