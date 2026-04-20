<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { pb } from '@/lib/pb'

const props = defineProps({
  serverId: { type: String, required: true },
})

const status = ref('Checking relay…')
let ws = null

onMounted(async () => {
  try {
    const res = await fetch('/relay/healthz')
    if (!res.ok) {
      const body = await res.text()
      status.value = `Relay unavailable: ${body}`
      return
    }
  } catch {
    status.value = 'Relay unavailable: could not reach health endpoint'
    return
  }

  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  const url = `${proto}://${location.host}/relay/${props.serverId}/?token=${pb.authStore.token}`
  status.value = 'Connecting…'
  ws = new WebSocket(url)

  ws.onopen = () => {
    status.value = 'Connected'
  }

  ws.onclose = (e) => {
    const reason = e.reason ? `: ${e.reason}` : ''
    status.value = `Disconnected (code ${e.code}${reason})`
  }

  ws.onerror = () => {
    status.value = 'Connection error'
  }
})

onUnmounted(() => {
  if (ws && ws.readyState < WebSocket.CLOSING) {
    ws.close(1000, 'tab closed')
  }
})
</script>

<template>
  <div class="w-full h-full p-4 font-mono text-xs text-slate-400 overflow-auto">
    <pre>{{ status }}</pre>
  </div>
</template>
