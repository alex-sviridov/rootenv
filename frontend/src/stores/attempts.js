import { defineStore } from 'pinia'
import { ref } from 'vue'
import { fetchLastAttempt, fetchAttempts, createAttempt, fetchActiveAttempt, decommissionAttempt } from '@/api/attempts'

export const useAttemptsStore = defineStore('attempts', () => {
  const lastAttempt = ref(null)
  const activeAttempt = ref(null)
  const history = ref({ items: [], page: 1, totalPages: 1 })
  const loading = ref(false)
  const historyLoading = ref(false)
  const error = ref(null)

  async function withLoading(fn) {
    loading.value = true
    error.value = null
    try {
      await fn()
    } catch (e) {
      if (!e?.isAbort) error.value = e.message
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
      if (!e?.isAbort) error.value = e.message
    } finally {
      historyLoading.value = false
    }
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
    withHistoryLoading(async () => {
      const result = await fetchAttempts(labId, page, perPage)
      history.value = { items: result.items, page: result.page, totalPages: result.totalPages }
    })

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
    })

  const removeAttempt = (serverIds) => {
    if (!lastAttempt.value) return
    return withLoading(async () => {
      await decommissionAttempt(serverIds)
    })
  }

  return {
    lastAttempt, activeAttempt, history, loading, historyLoading, error,
    loadLastAttempt, loadActiveAttempt, loadHistory, addAttempt, removeAttempt,
  }
})
