import { onMounted, onUnmounted } from 'vue'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'
import { pb } from '@/lib/pb'

export function useRelayConnection(serverId) {
  const terminal = new Terminal({
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

  let ws = null

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

    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${location.host}/relay/${serverId}/`
    ws = new WebSocket(url)

    ws.binaryType = 'arraybuffer'

    ws.onopen = () => {
      ws.send(pb.authStore.token)
    }

    ws.onmessage = (e) => {
      terminal.write(new Uint8Array(e.data))
    }

    ws.onclose = (e) => {
      const reason = e.reason ? `: ${e.reason}` : ''
      terminal.writeln(`\r\nDisconnected (code ${e.code}${reason})`)
    }

    ws.onerror = () => {
      terminal.writeln('\r\nConnection error')
    }
  })

  onUnmounted(() => {
    if (ws && ws.readyState < WebSocket.CLOSING) {
      ws.close(1000, 'tab closed')
    }
    terminal.dispose()
  })

  return { terminal, fitAddon }
}
