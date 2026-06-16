import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { fetchLastAttempt, fetchAttempts, createAttempt, fetchActiveAttempt, decommissionAttempt, subscribeToAttempt } from '@/api/attempts'

export const useAttemptsStore = defineStore('attempts', () => {
  const lastAttempt = ref(null)
  const activeAttempt = ref(null)
  const history = ref({ items: [], page: 1, totalPages: 1 })
  const loading = ref(false)
  const historyLoading = ref(false)
  const error = ref(null)

  const servers = computed(() => lastAttempt.value?.assets ?? [])

  let _unsubscribe = null

  async function withLoading(fn, loadingRef = loading) {
    loadingRef.value = true
    error.value = null
    try {
      await fn()
    } catch (e) {
      if (!e?.isAbort) error.value = e.message
    } finally {
      loadingRef.value = false
    }
  }

  function _handleAttemptEvent(event) {
    if (lastAttempt.value?.id === event.record.id) lastAttempt.value = event.record
    if (activeAttempt.value?.id === event.record.id) activeAttempt.value = event.record
  }

  async function startWatching(attemptId) {
    if (_unsubscribe) await stopWatching()
    _unsubscribe = await subscribeToAttempt(attemptId, _handleAttemptEvent)
  }

  async function stopWatching() {
    if (_unsubscribe) { await _unsubscribe(); _unsubscribe = null }
  }

  const loadLastAttempt = (labId) =>
    withLoading(async () => {
      try {
        lastAttempt.value = await fetchLastAttempt(labId)
      } catch (e) {
        if (e?.status === 404) lastAttempt.value = null
        else throw e
      }
    })

  const loadHistory = (labId, page = 1, perPage = 10) =>
    withLoading(async () => {
      const result = await fetchAttempts(labId, page, perPage)
      history.value = { items: result.items, page: result.page, totalPages: result.totalPages }
    }, historyLoading)

  const loadActiveAttempt = async () => {
    try {
      activeAttempt.value = await fetchActiveAttempt()
    } catch {
      activeAttempt.value = null
    }
  }

  const addAttempt = (labId, labName) =>
    withLoading(async () => {
      const attempt = await createAttempt(labId, labName)
      lastAttempt.value = attempt
      activeAttempt.value = attempt
    })

  const removeAttempt = () => {
    if (!lastAttempt.value) return
    return withLoading(async () => {
      await decommissionAttempt(lastAttempt.value.id)
      if (lastAttempt.value) lastAttempt.value = { ...lastAttempt.value, current_state: 'decommissioning' }
      if (activeAttempt.value?.id === lastAttempt.value?.id) activeAttempt.value = { ...activeAttempt.value, current_state: 'decommissioning' }
    })
  }

  return {
    lastAttempt, activeAttempt, servers, history, loading, historyLoading, error,
    loadLastAttempt, loadActiveAttempt, loadHistory, addAttempt, removeAttempt,
    startWatching, stopWatching,
  }
})
