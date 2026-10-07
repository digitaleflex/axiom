import { describe, expect, it } from 'vitest'
import { buildSseUrl, nextBackoff, OrderedDelivery, type SseMessage } from './sse'

describe('buildSseUrl', () => {
  it('includes the ?lastEventId= resume fallback when a sequence is known', () => {
    const url = buildSseUrl('/deployments/dep_1/events/stream', {
      lastEventId: '42',
      baseUrl: 'http://api.test/',
    })
    expect(url).toBe('http://api.test/api/v1/deployments/dep_1/events/stream?lastEventId=42')
  })

  it('omits the parameter on a fresh connection', () => {
    const url = buildSseUrl('/deployments/dep_1/events/stream', { baseUrl: 'http://api.test' })
    expect(url).toBe('http://api.test/api/v1/deployments/dep_1/events/stream')
  })
})

describe('nextBackoff', () => {
  it('grows exponentially and caps at the maximum', () => {
    expect(nextBackoff(0)).toBe(1000)
    expect(nextBackoff(1)).toBe(2000)
    expect(nextBackoff(2)).toBe(4000)
    expect(nextBackoff(10)).toBe(30000)
  })
})

describe('OrderedDelivery', () => {
  it('buffers gaps, emits in sequence order and drops duplicates', () => {
    const received: string[] = []
    const delivery = new OrderedDelivery((message: SseMessage) => received.push(message.id ?? 'none'))

    delivery.push({ id: '1', type: 'x', data: '{}' })
    expect(received).toEqual(['1'])

    // Gap: 3 arrives before 2 and must be buffered.
    delivery.push({ id: '3', type: 'x', data: '{}' })
    expect(received).toEqual(['1'])

    delivery.push({ id: '2', type: 'x', data: '{}' })
    expect(received).toEqual(['1', '2', '3'])

    // Duplicate is dropped.
    delivery.push({ id: '2', type: 'x', data: '{}' })
    expect(received).toEqual(['1', '2', '3'])

    delivery.push({ id: '4', type: 'x', data: '{}' })
    expect(received).toEqual(['1', '2', '3', '4'])
  })
})
