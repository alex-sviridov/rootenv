import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { fetchLastAttempt, fetchAttempts, createAttempt, subscribeToAttempt, fetchActiveAttempt, decommissionAttempt } from '@/api/attempts'

export const useAttemptsStore = defineStore('attempts', () => {
  const lastAttempt = ref(null)
  const activeAttempt = ref(null)
  const history = ref({ items: [], page: 1, totalPages: 1 })
  const loading = ref(false)
  const historyLoading = ref(false)
  const error = ref(null)
  let _unsubscribe = null

  const canProvision = computed(() =>
    lastAttempt.value === null ||
    lastAttempt.value.state === 'decommissioned' ||
    lastAttempt.value.state === 'decommissioning'
  )

  async function withLoading(fn) {
    loading.value = true
    error.value = null
    try {
      await fn()
    } catch (e) {
      error.value = e.message
    } finally {
      loading.value = false
    }
  }

  async function withHistoryLoading(fn) {
    historyLoading.value = true
    error.value = null
    try {
      await fn()
    } catch (e) {
      error.value = e.message
    } finally {
      historyLoading.value = false
    }
  }

  const loadLastAttempt = (labId) =>
    withLoading(async () => {
      try {
        lastAttempt.value = await fetchLastAttempt(labId)
      } catch (e) {
        if (e?.status === 404) {
          lastAttempt.value = null
        } else {
          throw e
        }
      }
    })

  const loadHistory = (labId, page = 1, perPage = 10) =>
    withHistoryLoading(async () => {
      const result = await fetchAttempts(labId, page, perPage)
      history.value = { items: result.items, page: result.page, totalPages: result.totalPages }
    })

  const stopWatching = async () => {
    if (_unsubscribe) {
      await _unsubscribe()
      _unsubscribe = null
    }
  }

  const startWatching = async (labId) => {
    if (_unsubscribe) await stopWatching()
    _unsubscribe = await subscribeToAttempt(labId, (record) => {
      lastAttempt.value = record
      if (record.state !== 'decommissioned' && record.state !== 'decommissioning') {
        activeAttempt.value = record
      } else if (record.state === 'decommissioned') {
        activeAttempt.value = null
      }
    })
  }

  const loadActiveAttempt = async () => {
    try {
      activeAttempt.value = await fetchActiveAttempt()
    } catch (e) {
      activeAttempt.value = null
    }
  }

  const addAttempt = (labId, labName) => {
    if (!canProvision.value) {
      error.value = 'An active attempt already exists'
      return
    }
    return withLoading(async () => {
      lastAttempt.value = await createAttempt(labId, labName)
    })
  }

  const removeAttempt = (serverIds) => {
    if (!lastAttempt.value) return
    return withLoading(async () => {
      await decommissionAttempt(serverIds)
      lastAttempt.value = { ...lastAttempt.value, state: 'decommissioning' }
      activeAttempt.value = null
    })
  }

  return {
    lastAttempt, activeAttempt, history, loading, historyLoading, error,
    canProvision,
    loadLastAttempt, loadActiveAttempt, loadHistory, startWatching, stopWatching, addAttempt, removeAttempt,
  }
})
