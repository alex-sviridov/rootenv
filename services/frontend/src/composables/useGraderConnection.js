import { ref } from 'vue'
import { pb } from '@/lib/pb'

const RECONNECT_DELAY_MS = 5000

export function useGraderConnection(attemptId) {
  const grades = ref({})
  let ws = null
  let reconnectTimer = null
  let closedExplicitly = false

  function connect() {
    closedExplicitly = false
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${location.host}/relay/grade/${attemptId}/`
    document.cookie = `pb_auth=${pb.authStore.token}; SameSite=Strict; Secure; path=/`
    ws = new WebSocket(url)

    ws.onopen = () => {
      ws.send(pb.authStore.token)
    }

    ws.onmessage = (e) => {
      try {
        grades.value = JSON.parse(e.data)
      } catch {
        // malformed frame — freeze at last known grades, same as onclose/onerror
      }
    }

    ws.onclose = () => {
      // grades frozen at last known value; no error UI
      scheduleReconnect()
    }

    ws.onerror = () => {
      // grades frozen at last known value; no error UI
      // onclose always follows onerror for a WebSocket, so reconnect is scheduled there
    }
  }

  function scheduleReconnect() {
    if (closedExplicitly || reconnectTimer) return
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null
      connect()
    }, RECONNECT_DELAY_MS)
  }

  function close() {
    closedExplicitly = true
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
      ws.close(1000, 'session ended')
    }
    ws = null
  }

  return { grades, connect, close }
}
