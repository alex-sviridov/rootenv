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
      this.resizeHandler = null
      this.selectionHandler = null
    }
    writeln(text) { this.buffer.push(text) }
    write(data) { this.buffer.push(data) }
    onData(handler) { this.dataHandler = handler }
    onResize(handler) { this.resizeHandler = handler }
    onSelectionChange(handler) { this.selectionHandler = handler }
    getSelection() { return '' }
    input(text) { this.buffer.push(text) }
    loadAddon() {}
    dispose() {}
    open() {}
    get textarea() { return { addEventListener: vi.fn() } }
  }
  return { Terminal: MockTerminal }
})

vi.mock('@xterm/addon-fit', () => {
  class MockFitAddon { fit() {} }
  return { FitAddon: MockFitAddon }
})

vi.mock('@xterm/addon-web-links', () => {
  class MockWebLinksAddon {}
  return { WebLinksAddon: MockWebLinksAddon }
})

import { useExecRelayConnection } from '../useExecRelayConnection'

class MockWebSocket {
  constructor(url) {
    this.url = url
    this.readyState = WebSocket.CONNECTING
    this.sent = []
    this.binaryType = null
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

describe('useExecRelayConnection', () => {
  it('opens WebSocket at /relay/exec/<attemptId>/<assetName>/ (ws for http)', async () => {
    const { unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    expect(MockWebSocket.lastInstance.url).toBe('ws://localhost:8080/relay/exec/atm_123/workstation/')
    unmount()
  })

  it('sets pb_auth cookie before WebSocket connect', async () => {
    let setCookieValue = null
    const originalDescriptor = Object.getOwnPropertyDescriptor(document, 'cookie')
    Object.defineProperty(document, 'cookie', {
      set(value) { setCookieValue = value },
      configurable: true,
    })

    try {
      const { unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
      await flushPromises()

      expect(setCookieValue).toContain('pb_auth=test-token')
      expect(setCookieValue).toContain('SameSite=Strict')
      expect(setCookieValue).toContain('Secure')
      expect(setCookieValue).toContain('path=/')
      unmount()
    } finally {
      if (originalDescriptor) Object.defineProperty(document, 'cookie', originalDescriptor)
    }
  })

  it('opens WebSocket at wss when protocol is https', async () => {
    vi.stubGlobal('location', { protocol: 'https:', host: 'example.com' })

    const { unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    expect(MockWebSocket.lastInstance.url).toContain('wss://')
    unmount()
  })

  it('sets binaryType to arraybuffer', async () => {
    const { unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    expect(MockWebSocket.lastInstance.binaryType).toBe('arraybuffer')
    unmount()
  })

  it('sends only the token as first message on open (no secret)', async () => {
    const { unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.onopen()

    expect(MockWebSocket.lastInstance.sent).toEqual([mockToken])
    unmount()
  })

  it('registers onData handler to forward terminal input to ws', async () => {
    const { result, unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.onopen()

    expect(result.terminal.dataHandler).not.toBeNull()
    unmount()
  })

  it('forwards terminal input through ws when onData fires', async () => {
    const { result, unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.readyState = MockWebSocket.OPEN
    MockWebSocket.lastInstance.onopen()
    result.terminal.dataHandler('ls')

    expect(MockWebSocket.lastInstance.sent).toContain('ls')
    unmount()
  })

  it('writes binary data from ws messages to terminal', async () => {
    const { result, unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.onmessage({ data: new ArrayBuffer(4) })

    expect(result.terminal.buffer.some(item => item instanceof Uint8Array)).toBe(true)
    unmount()
  })

  it('writes disconnect message on close with code and reason', async () => {
    const { result, unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.onclose({ code: 1008, reason: 'unauthorized' })

    const output = result.terminal.buffer.join('\n')
    expect(output).toContain('1008')
    expect(output).toContain('unauthorized')
    unmount()
  })

  it('writes disconnect message with code only when reason is empty', async () => {
    const { result, unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.onclose({ code: 1000, reason: '' })

    const output = result.terminal.buffer.join('\n')
    expect(output).toContain('1000')
    expect(output).not.toMatch(/1000.*:/)
    unmount()
  })

  it('writes connection error on ws error', async () => {
    const { result, unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.onerror()

    expect(result.terminal.buffer.some(m => m.includes('Connection error'))).toBe(true)
    unmount()
  })

  it('closes ws with code 1000 on unmount when open', async () => {
    const { unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.readyState = MockWebSocket.OPEN
    unmount()

    expect(MockWebSocket.lastInstance._closedWith).toEqual({ code: 1000, reason: 'tab closed' })
  })

  it('does not close ws when already closing on unmount', async () => {
    const { unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.readyState = MockWebSocket.CLOSING
    unmount()

    expect(MockWebSocket.lastInstance._closedWith).toBeUndefined()
  })

  it('sends a 5-byte resize frame (0x01 + cols LE + rows LE) when terminal resizes', async () => {
    const { result, unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    MockWebSocket.lastInstance.readyState = MockWebSocket.OPEN
    MockWebSocket.lastInstance.onopen()
    result.terminal.resizeHandler({ cols: 80, rows: 24 })

    const frame = MockWebSocket.lastInstance.sent.find(s => s instanceof ArrayBuffer)
    expect(frame).toBeDefined()
    expect(frame.byteLength).toBe(5)
    const view = new DataView(frame)
    expect(view.getUint8(0)).toBe(0x01)
    expect(view.getUint16(1, true)).toBe(80)
    expect(view.getUint16(3, true)).toBe(24)
    unmount()
  })

  it('disposes terminal on unmount', async () => {
    const { result, unmount } = withSetup(() => useExecRelayConnection('atm_123', 'workstation'))
    await flushPromises()

    const spy = vi.spyOn(result.terminal, 'dispose')
    unmount()

    expect(spy).toHaveBeenCalled()
  })
})
