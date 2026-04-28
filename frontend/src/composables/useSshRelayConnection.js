import { onMounted, onUnmounted, ref } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { WebLinksAddon } from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'
import { pb } from '@/lib/pb'

const POLICY_VIOLATION = 1002

export function useSshRelayConnection(serverId, secret) {
  const terminal = new Terminal({
    scrollback: 10000,
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

  async function connect() {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${location.host}/relay/ssh/${serverId}/`
    ws = new WebSocket(url)
    ws.binaryType = 'arraybuffer'

    ws.onopen = () => {
      ws.send(pb.authStore.token + '\n' + secret)
      onDataHandler = (data) => {
        if (ws && ws.readyState === WebSocket.OPEN) {
          ws.send(data)
        }
      }
      terminal.onData(onDataHandler)

      // Send resize frame when terminal size changes.
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

      // Fit terminal to container and set up browser resize listener.
      fitAddon.fit()
      window.addEventListener('resize', () => fitAddon.fit())
    }

    ws.onmessage = (e) => {
      terminal.write(new Uint8Array(e.data))
    }

    ws.onclose = async (e) => {
      if (isUnmounting) return

      // Code 1002 with "session expired" = token expired; try refresh
      if (e.code === POLICY_VIOLATION && e.reason === 'session expired') {
        terminal.writeln('\r\nSession expired, reconnecting…')
        try {
          // Refresh token from PocketBase (doesn't require logout/login)
          await pb.collection('users').authRefresh()
          terminal.writeln('Token refreshed.')
          // Reconnect after a short delay to allow server state to catch up
          await new Promise(r => setTimeout(r, 500))
          connect()
        } catch (err) {
          terminal.writeln(`\r\nFailed to refresh session: ${err.message}`)
          terminal.writeln('Please reload the page to reconnect.')
        }
        return
      }

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
      const res = await fetch('/relay/healthz')
      if (!res.ok) {
        const body = await res.text()
        terminal.writeln(`\r\nRelay unavailable (HTTP ${res.status}): ${body || 'no details'}`)
        return
      }
    } catch {
      terminal.writeln('\r\nRelay unavailable: could not reach /relay/healthz')
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
