import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { withSetup } from './utils'

const { mockToken } = vi.hoisted(() => ({ mockToken: 'test-token' }))

vi.mock('@/lib/pb', () => ({
  pb: { authStore: { get token() { return mockToken } } },
}))

vi.mock('@xterm/xterm', () => {
  class MockTerminal {
    constructor() {
      this.buffer = []
      this.dataHandler = null
    }
    writeln(text) {
      this.buffer.push(text)
    }
    write(data) {
      this.buffer.push(data)
    }
    onData(handler) {
      this.dataHandler = handler
    }
    onResize(handler) {
      this.resizeHandler = handler
    }
    loadAddon() {}
    dispose() {}
    open() {}
  }
  return { Terminal: MockTerminal }
})

vi.mock('@xterm/addon-fit', () => {
  class MockFitAddon {
    fit() {}
  }
  return { FitAddon: MockFitAddon }
})

vi.mock('@xterm/addon-web-links', () => {
  class MockWebLinksAddon {}
  return { WebLinksAddon: MockWebLinksAddon }
})

import { useSshRelayConnection as useRelayConnection } from '../useSshRelayConnection'

// Minimal WebSocket mock
class MockWebSocket {
  constructor(url) {
    this.url = url
    this.readyState = WebSocket.CONNECTING
    this.sent = []
    this.binaryType = null
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
  it('writes error to terminal with status code and body', async () => {
    fetch.mockResolvedValue({ ok: false, status: 503, text: () => Promise.resolve('not ready') })

    const { result, unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    const output = result.terminal.buffer.join('\n')
    expect(output).toContain('503')
    expect(output).toContain('not ready')
    expect(MockWebSocket.lastInstance).toBeNull()
    unmount()
  })

  it('shows fallback message when body is empty', async () => {
    fetch.mockResolvedValue({ ok: false, status: 503, text: () => Promise.resolve('') })

    const { result, unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    expect(result.terminal.buffer.some(msg => msg.includes('no details'))).toBe(true)
    unmount()
  })
})

describe('health check throws (network error)', () => {
  it('writes error to terminal when healthz unreachable', async () => {
    fetch.mockRejectedValue(new Error('network error'))

    const { result, unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    expect(result.terminal.buffer.some(msg => msg.includes('/relay/ssh/healthz'))).toBe(true)
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
    expect(MockWebSocket.lastInstance.url).toBe('ws://localhost:8080/relay/ssh/server1/')
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

  it('sets binaryType to arraybuffer', async () => {
    const { unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    expect(MockWebSocket.lastInstance.binaryType).toBe('arraybuffer')
    unmount()
  })

  it('sends token\\nsecret as first message on open', async () => {
    const { unmount } = withSetup(() => useRelayConnection('server1', 'mysecret'))
    await flushPromises()

    MockWebSocket.lastInstance.onopen()

    expect(MockWebSocket.lastInstance.sent).toEqual([`${mockToken}\nmysecret`])
    unmount()
  })

  it('writes binary data from ws messages to terminal', async () => {
    const { result, unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    const testData = new ArrayBuffer(5)
    MockWebSocket.lastInstance.onmessage({ data: testData })

    expect(result.terminal.buffer.some(item => item instanceof Uint8Array)).toBe(true)
    unmount()
  })

  it('registers onData handler to forward terminal input to ws', async () => {
    const { result, unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    MockWebSocket.lastInstance.onopen()

    expect(result.terminal.dataHandler).not.toBeNull()
    expect(typeof result.terminal.dataHandler).toBe('function')
    unmount()
  })

  it('sends terminal input through ws when onData fires', async () => {
    const { result, unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    MockWebSocket.lastInstance.readyState = MockWebSocket.OPEN
    MockWebSocket.lastInstance.onopen()

    result.terminal.dataHandler('echo hello')

    expect(MockWebSocket.lastInstance.sent).toContain('echo hello')
    unmount()
  })

  it('writes disconnect message to terminal on close with code and reason', async () => {
    const { result, unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    MockWebSocket.lastInstance.onclose({ code: 1008, reason: 'unauthorized' })

    const output = result.terminal.buffer.join('\n')
    expect(output).toContain('1008')
    expect(output).toContain('unauthorized')
    unmount()
  })

  it('writes disconnect message with code only when reason is empty', async () => {
    const { result, unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    MockWebSocket.lastInstance.onclose({ code: 1000, reason: '' })

    const output = result.terminal.buffer.join('\n')
    expect(output).toContain('1000')
    expect(output).not.toMatch(/1000.*:/)
    unmount()
  })

  it('writes connection error to terminal on ws error', async () => {
    const { result, unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    MockWebSocket.lastInstance.onerror()

    expect(result.terminal.buffer.some(msg => msg.includes('Connection error'))).toBe(true)
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

  it('disposes terminal on unmount', async () => {
    fetch.mockResolvedValue({ ok: true })

    const { result, unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    const disposeSpy = vi.spyOn(result.terminal, 'dispose')
    unmount()

    expect(disposeSpy).toHaveBeenCalled()
  })

  it('does not throw when health check failed (no ws created)', async () => {
    fetch.mockResolvedValue({ ok: false, status: 503, text: () => Promise.resolve('') })

    const { unmount } = withSetup(() => useRelayConnection('server1'))
    await flushPromises()

    expect(() => unmount()).not.toThrow()
  })
})
