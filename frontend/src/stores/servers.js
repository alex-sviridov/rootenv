import { defineStore } from 'pinia'
import { ref } from 'vue'
import { fetchServers, subscribeToServers } from '@/api/servers'

export const useServersStore = defineStore('servers', () => {
  const servers = ref([])
  const loading = ref(false)
  let _unsubscribe = null

  async function loadServers(attemptId) {
    loading.value = true
    servers.value = []
    try {
      servers.value = await fetchServers(attemptId)
    } catch {
      // leave servers empty on error
    } finally {
      loading.value = false
    }
  }

  function handleServerEvent(event) {
    if (event.action === 'delete') {
      servers.value = servers.value.filter(s => s.id !== event.record.id)
    } else {
      const idx = servers.value.findIndex(s => s.id === event.record.id)
      if (idx !== -1) servers.value[idx] = event.record
      else servers.value.push(event.record)
    }
  }

  async function startWatching(attemptId) {
    if (_unsubscribe) await stopWatching()
    _unsubscribe = await subscribeToServers(attemptId, handleServerEvent)
  }

  async function stopWatching() {
    if (_unsubscribe) { await _unsubscribe(); _unsubscribe = null }
    servers.value = []
  }

  return { servers, loading, loadServers, startWatching, stopWatching }
})
