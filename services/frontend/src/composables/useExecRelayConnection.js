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
  let isUnmounting = false

  const windowResizeHandler = () => fitAddon.fit()

  function connect() {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${location.host}/relay/exec/${attemptId}/${assetName}/`
    document.cookie = `pb_auth=${pb.authStore.token}; SameSite=Strict; Secure; path=/`
    ws = new WebSocket(url)
    ws.binaryType = 'arraybuffer'

    ws.onopen = () => {
      ws.send(pb.authStore.token)

      terminal.onData((data) => {
        if (ws && ws.readyState === WebSocket.OPEN) {
          ws.send(data)
        }
      })

      terminal.onResize(({ cols, rows }) => {
        if (ws && ws.readyState === WebSocket.OPEN) {
          const buf = new ArrayBuffer(5)
          const view = new DataView(buf)
          view.setUint8(0, 0x01)
          view.setUint16(1, cols, true)
          view.setUint16(3, rows, true)
          ws.send(buf)
        }
      })

      fitAddon.fit()
      window.addEventListener('resize', windowResizeHandler)
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

  onMounted(() => {
    terminal.writeln('Connecting…')
    connect()
  })

  onUnmounted(() => {
    isUnmounting = true
    if (ws) {
      if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
        ws.close(1000, 'tab closed')
      }
    }
    window.removeEventListener('resize', windowResizeHandler)
    terminal.dispose()
  })

  return { terminal, fitAddon }
}
