import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

const { mockToken } = vi.hoisted(() => ({ mockToken: 'test-token' }))

vi.mock('@/lib/pb', () => ({
  pb: { authStore: { get token() { return mockToken } } },
}))

import { useGraderConnection } from '../useGraderConnection'

class MockWebSocket {
  constructor(url) {
    this.url = url
    this.readyState = WebSocket.CONNECTING
    this.sent = []
    MockWebSocket.lastInstance = this
  }
  send(data) { this.sent.push(data) }
  close(code, reason) { this._closedWith = { code, reason } }
}
MockWebSocket.CONNECTING = 0
MockWebSocket.OPEN = 1
MockWebSocket.CLOSING = 2
MockWebSocket.CLOSED = 3

beforeEach(() => {
  MockWebSocket.lastInstance = null
  vi.stubGlobal('WebSocket', MockWebSocket)
  vi.stubGlobal('location', { protocol: 'http:', host: 'localhost:8080' })
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('useGraderConnection', () => {
  it('opens WebSocket at /relay/grade/<attemptId>/ on connect()', () => {
    const { connect } = useGraderConnection('atm_123')
    connect()

    expect(MockWebSocket.lastInstance.url).toBe('ws://localhost:8080/relay/grade/atm_123/')
  })

  it('opens wss when protocol is https', () => {
    vi.stubGlobal('location', { protocol: 'https:', host: 'example.com' })
    const { connect } = useGraderConnection('atm_123')
    connect()

    expect(MockWebSocket.lastInstance.url).toBe('wss://example.com/relay/grade/atm_123/')
  })

  it('sets pb_auth cookie before connecting', () => {
    let setCookieValue = null
    const originalDescriptor = Object.getOwnPropertyDescriptor(document, 'cookie')
    Object.defineProperty(document, 'cookie', {
      set(value) { setCookieValue = value },
      configurable: true,
    })

    try {
      const { connect } = useGraderConnection('atm_123')
      connect()
      expect(setCookieValue).toContain('pb_auth=test-token')
    } finally {
      if (originalDescriptor) Object.defineProperty(document, 'cookie', originalDescriptor)
    }
  })

  it('sends the token as the first message on open', () => {
    const { connect } = useGraderConnection('atm_123')
    connect()
    MockWebSocket.lastInstance.onopen()

    expect(MockWebSocket.lastInstance.sent).toEqual([mockToken])
  })

  it('populates grades from a JSON message', () => {
    const { connect, grades } = useGraderConnection('atm_123')
    connect()
    MockWebSocket.lastInstance.onmessage({ data: JSON.stringify({ '1.1': true, '1.2': false }) })

    expect(grades.value).toEqual({ '1.1': true, '1.2': false })
  })

  it('replaces the whole grades map on each message', () => {
    const { connect, grades } = useGraderConnection('atm_123')
    connect()
    MockWebSocket.lastInstance.onmessage({ data: JSON.stringify({ '1.1': false }) })
    MockWebSocket.lastInstance.onmessage({ data: JSON.stringify({ '1.1': true, '2.1': true }) })

    expect(grades.value).toEqual({ '1.1': true, '2.1': true })
  })

  it('does not throw and leaves grades unchanged on close', () => {
    const { connect, grades } = useGraderConnection('atm_123')
    connect()
    MockWebSocket.lastInstance.onmessage({ data: JSON.stringify({ '1.1': true }) })
    expect(() => MockWebSocket.lastInstance.onclose({ code: 1000, reason: '' })).not.toThrow()
    expect(grades.value).toEqual({ '1.1': true })
  })

  it('does not throw and leaves grades unchanged on error', () => {
    const { connect, grades } = useGraderConnection('atm_123')
    connect()
    MockWebSocket.lastInstance.onmessage({ data: JSON.stringify({ '1.1': true }) })
    expect(() => MockWebSocket.lastInstance.onerror()).not.toThrow()
    expect(grades.value).toEqual({ '1.1': true })
  })

  it('does not throw and leaves grades unchanged on malformed JSON message', () => {
    const { connect, grades } = useGraderConnection('atm_123')
    connect()
    MockWebSocket.lastInstance.onmessage({ data: JSON.stringify({ '1.1': true }) })
    expect(() => MockWebSocket.lastInstance.onmessage({ data: 'not valid json{' })).not.toThrow()
    expect(grades.value).toEqual({ '1.1': true })
  })

  it('close() closes the socket with code 1000 when open', () => {
    const { connect, close } = useGraderConnection('atm_123')
    connect()
    MockWebSocket.lastInstance.readyState = MockWebSocket.OPEN
    close()

    expect(MockWebSocket.lastInstance._closedWith).toEqual({ code: 1000, reason: 'session ended' })
  })

  it('close() is a no-op if never connected', () => {
    const { close } = useGraderConnection('atm_123')
    expect(() => close()).not.toThrow()
  })

  it('close() is safe to call twice', () => {
    const { connect, close } = useGraderConnection('atm_123')
    connect()
    MockWebSocket.lastInstance.readyState = MockWebSocket.OPEN
    close()
    expect(() => close()).not.toThrow()
  })
})
