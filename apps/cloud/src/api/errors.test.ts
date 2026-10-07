import { describe, expect, it } from 'vitest'
import { ApiError, parseErrorEnvelope } from './errors'

describe('parseErrorEnvelope', () => {
  it('parses the api-contract §18 error envelope', () => {
    const envelope = parseErrorEnvelope({
      error: {
        code: 'NOT_FOUND',
        message: 'Deployment not found.',
        requestId: 'req_123',
        details: { deploymentId: 'dep_1' },
      },
    })
    expect(envelope).not.toBeNull()
    expect(envelope?.error.code).toBe('NOT_FOUND')
    expect(envelope?.error.details).toEqual({ deploymentId: 'dep_1' })
  })

  it('rejects malformed bodies', () => {
    expect(parseErrorEnvelope(null)).toBeNull()
    expect(parseErrorEnvelope({ message: 'no error object' })).toBeNull()
    expect(parseErrorEnvelope({ error: { code: 500, message: 'bad types' } })).toBeNull()
  })
})

describe('ApiError', () => {
  it('maps status/code to typed predicates and keeps requestId', () => {
    const envelope = parseErrorEnvelope({
      error: { code: 'UNAUTHORIZED', message: 'Missing token.' },
    })!
    const error = ApiError.fromEnvelope(401, envelope, 'req_fallback')
    expect(error.isUnauthorized).toBe(true)
    expect(error.requestId).toBe('req_fallback')
    expect(error.details).toEqual({})
  })

  it('classifies service unavailable and network failures', () => {
    expect(new ApiError({ code: 'SERVICE_UNAVAILABLE', message: 'db down', status: 503 }).isUnavailable).toBe(true)
    expect(ApiError.network().isNetwork).toBe(true)
  })
})
