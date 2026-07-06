import { ref } from 'vue'
import { pb } from '@/lib/pb'

export function useGraderConnection(attemptId) {
  const grades = ref({})
  let ws = null

  function connect() {
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const url = `${proto}://${location.host}/relay/grade/${attemptId}/`
    document.cookie = `pb_auth=${pb.authStore.token}; SameSite=Strict; Secure; path=/`
    ws = new WebSocket(url)

    ws.onopen = () => {
      ws.send(pb.authStore.token)
    }

    ws.onmessage = (e) => {
      grades.value = JSON.parse(e.data)
    }

    ws.onclose = () => {
      // grades frozen at last known value; no error UI
    }

    ws.onerror = () => {
      // grades frozen at last known value; no error UI
    }
  }

  function close() {
    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
      ws.close(1000, 'session ended')
    }
    ws = null
  }

  return { grades, connect, close }
}
