import { ref, onMounted, onUnmounted } from 'vue'
import { pb } from '@/lib/pb'

export function useRelayConnection(serverId) {
  const status = ref('Checking relay…')
  let ws = null

  onMounted(async () => {
    try {
      const res = await fetch('/relay/healthz')
      if (!res.ok) {
        const body = await res.text()
        status.value = `Relay unavailable (HTTP ${res.status}): ${body || 'no details'}`
        return
      }
    } catch {
      status.value = 'Relay unavailable: could not reach /relay/healthz'
      return
    }

    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${location.host}/relay/${serverId}/`
    status.value = 'Connecting…'
    ws = new WebSocket(url)

    ws.onopen = () => {
      ws.send(pb.authStore.token)
      status.value = 'Connected'
    }
    ws.onclose = (e) => {
      const reason = e.reason ? `: ${e.reason}` : ''
      status.value = `Disconnected (code ${e.code}${reason})`
    }
    ws.onerror = () => { status.value = 'Connection error' }
  })

  onUnmounted(() => {
    if (ws && ws.readyState < WebSocket.CLOSING) ws.close(1000, 'tab closed')
  })

  return { status, ws: () => ws }
}
