import { onMounted, onUnmounted } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'
import { pb } from '@/lib/pb'

const SCROLLBACK_LINES = 10000

export function useExecRelayConnection(attemptId, assetName) {
  const terminal = new Terminal({
    scrollback: SCROLLBACK_LINES,
    cursorBlink: true,
    cursorStyle: 'block',
    fontFamily: 'monospace',
    fontSize: 12,
    theme: {
      background: '#0f172a',
      foreground: '#cbd5e1',
      cursor: '#cbd5e1',
    },
  })

  const fitAddon = new FitAddon()
  terminal.loadAddon(fitAddon)
  terminal.loadAddon(new WebLinksAddon())

  let ws = null
  let onDataHandler = null
  let isUnmounting = false

  function connect() {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${location.host}/relay/${attemptId}/exec/${assetName}/`
    ws = new WebSocket(url)
    ws.binaryType = 'arraybuffer'

    ws.onopen = () => {
      // Send token as first message — ingress-authenticator already validated it,
      // relay-exec discards this frame (protocol compatibility).
      ws.send(pb.authStore.token)

      onDataHandler = (data) => {
        if (ws && ws.readyState === WebSocket.OPEN) {
          ws.send(data)
        }
      }
      terminal.onData(onDataHandler)

      terminal.onResize(({ cols, rows }) => {
        if (ws && ws.readyState === WebSocket.OPEN) {
          const buf = new ArrayBuffer(5)
          const view = new DataView(buf)
          view.setUint8(0, 0x01)
          view.setUint16(1, cols, true) // LE
          view.setUint16(3, rows, true) // LE
          ws.send(buf)
        }
      })

      fitAddon.fit()
      window.addEventListener('resize', () => fitAddon.fit())
    }

    ws.onmessage = (e) => {
      terminal.write(new Uint8Array(e.data))
    }

    ws.onclose = (e) => {
      if (isUnmounting) return
      const reason = e.reason ? `: ${e.reason}` : ''
      terminal.writeln(`\r\nDisconnected (code ${e.code}${reason})`)
    }

    ws.onerror = () => {
      terminal.writeln('\r\nConnection error')
    }
  }

  onMounted(async () => {
    terminal.writeln('Checking relay…')

    try {
      const res = await fetch(`/relay/${attemptId}/exec/healthz`)
      if (!res.ok) {
        const body = await res.text()
        terminal.writeln(`\r\nRelay unavailable (HTTP ${res.status}): ${body || 'no details'}`)
        return
      }
    } catch {
      terminal.writeln(`\r\nRelay unavailable: could not reach /relay/${attemptId}/exec/healthz`)
      return
    }

    terminal.writeln('Connecting…')
    connect()
  })

  onUnmounted(() => {
    isUnmounting = true
    if (onDataHandler) {
      terminal.onData(() => {}, true)
    }
    if (ws) {
      if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
        ws.close(1000, 'tab closed')
      }
    }
    terminal.dispose()
  })

  return { terminal, fitAddon }
}
