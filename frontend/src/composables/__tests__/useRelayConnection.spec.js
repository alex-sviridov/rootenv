import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { withSetup } from './utils'

const { mockToken } = vi.hoisted(() => ({ mockToken: 'test-token' }))

vi.mock('@/lib/pb', () => ({
  pb: { authStore: { get token() { return mockToken } } },
}))

import { useRelayConnection } from '../useRelayConnection'

// Minimal WebSocket mock
class MockWebSocket {
  constructor(url) {
    this.url = url
    this.readyState = WebSocket.CONNECTING
    this.sent = []
    MockWebSocket.lastInstance = this
  }
  send(data) { this.sent.push(data) }
  close(code, reason) {
    this._closedWith = { code, reason }
  }
}
MockWebSocket.CONNECTING = 0
MockWebSocket.OPEN = 1
MockWebSocket.CLOSING = 2
MockWebSocket.CLOSED = 3

beforeEach(() => {
  MockWebSocket.lastInstance = null
  vi.stubGlobal('WebSocket', MockWebSocket)
  vi.stubGlobal('fetch', vi.fn())
  vi.stubGlobal('location', { protocol: 'http:', host: 'localhost:8080' })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('health check fails with non-ok response', () => {
  it('sets status to relay unavailable with status code and body', async () => {
    fetch.mockResolvedValue({ ok: false, status: 503, text: () => Promise.resolve('not ready') })

    const { result, unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    expect(result.status.value).toContain('503')
    expect(result.status.value).toContain('not ready')
    expect(MockWebSocket.lastInstance).toBeNull()
    unmount()
  })

  it('shows fallback message when body is empty', async () => {
    fetch.mockResolvedValue({ ok: false, status: 503, text: () => Promise.resolve('') })

    const { result, unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    expect(result.status.value).toContain('no details')
    unmount()
  })
})

describe('health check throws (network error)', () => {
  it('sets status to could not reach healthz', async () => {
    fetch.mockRejectedValue(new Error('network error'))

    const { result, unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    expect(result.status.value).toContain('/relay/healthz')
    expect(MockWebSocket.lastInstance).toBeNull()
    unmount()
  })
})

describe('health check passes', () => {
  beforeEach(() => {
    fetch.mockResolvedValue({ ok: true })
  })

  it('opens WebSocket without token in url (ws for http)', async () => {
    const { unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    expect(MockWebSocket.lastInstance).not.toBeNull()
    expect(MockWebSocket.lastInstance.url).toBe('ws://localhost:8080/relay/server1/')
    expect(MockWebSocket.lastInstance.url).not.toContain('token')
    unmount()
  })

  it('opens WebSocket with wss when protocol is https', async () => {
    vi.stubGlobal('location', { protocol: 'https:', host: 'example.com' })

    const { unmount } = withSetup(() => useRelayConnection('srv'))
    await flushPromises()

    expect(MockWebSocket.lastInstance.url).toContain('wss://')
    unmount()
  })

  it('sends token as first message on open', async () => {
    const { unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    MockWebSocket.lastInstance.onopen()

    expect(MockWebSocket.lastInstance.sent).toEqual([mockToken])
    unmount()
  })

  it('sets status to Connecting… before open', async () => {
    const { result, unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    expect(result.status.value).toBe('Connecting…')
    unmount()
  })

  it('sets status to Connected on ws.onopen', async () => {
    const { result, unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    MockWebSocket.lastInstance.onopen()

    expect(result.status.value).toBe('Connected')
    unmount()
  })

  it('sets status with code and reason on ws.onclose', async () => {
    const { result, unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    MockWebSocket.lastInstance.onclose({ code: 1008, reason: 'unauthorized' })

    expect(result.status.value).toContain('1008')
    expect(result.status.value).toContain('unauthorized')
    unmount()
  })

  it('sets status with code only when reason is empty', async () => {
    const { result, unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    MockWebSocket.lastInstance.onclose({ code: 1000, reason: '' })

    expect(result.status.value).toContain('1000')
    expect(result.status.value).not.toContain(':')
    unmount()
  })

  it('sets status to Connection error on ws.onerror', async () => {
    const { result, unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    MockWebSocket.lastInstance.onerror()

    expect(result.status.value).toBe('Connection error')
    unmount()
  })
})

describe('cleanup on unmount', () => {
  it('closes WebSocket with code 1000 when unmounted while open', async () => {
    fetch.mockResolvedValue({ ok: true })

    const { unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    MockWebSocket.lastInstance.readyState = MockWebSocket.OPEN
    unmount()

    expect(MockWebSocket.lastInstance._closedWith).toEqual({ code: 1000, reason: 'tab closed' })
  })

  it('does not close WebSocket when already closing', async () => {
    fetch.mockResolvedValue({ ok: true })

    const { unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    MockWebSocket.lastInstance.readyState = MockWebSocket.CLOSING
    unmount()

    expect(MockWebSocket.lastInstance._closedWith).toBeUndefined()
  })

  it('does not close WebSocket when health check failed (no ws created)', async () => {
    fetch.mockResolvedValue({ ok: false, status: 503, text: () => Promise.resolve('') })

    const { unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    // Should not throw
    expect(() => unmount()).not.toThrow()
  })
})
