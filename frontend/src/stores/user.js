import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { login, register, logout, getAuthStore, changePassword, deleteAccount as deleteAccountApi, authRefresh } from '@/api/auth'

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/

export const useUserStore = defineStore('user', () => {
  const user = ref(null)
  const loading = ref(false)
  const error = ref(null)

  const isAuthenticated = computed(() => !!user.value)

  async function withLoading(fn) {
    loading.value = true
    error.value = null
    try {
      await fn()
    } catch (e) {
      const fieldMsg = e?.data && Object.values(e.data)[0]?.message
      error.value = fieldMsg || e.message
    } finally {
      loading.value = false
    }
  }

  async function signIn(email, password) {
    if (!email || !password) { error.value = 'Email and password are required'; return }
    if (!EMAIL_RE.test(email)) { error.value = 'Invalid email'; return }
    await withLoading(async () => {
      const auth = await login(email, password)
      user.value = auth.record
    })
  }

  async function signUp(email, password, passwordConfirm) {
    if (!email || !password) { error.value = 'Email and password are required'; return }
    if (!EMAIL_RE.test(email)) { error.value = 'Invalid email'; return }
    if (password.length < 8) { error.value = 'Password must be at least 8 characters'; return }
    if (password !== passwordConfirm) { error.value = 'Passwords do not match'; return }
    await withLoading(async () => {
      await register(email, password, passwordConfirm)
      const auth = await login(email, password)
      user.value = auth.record
    })
  }

  async function updatePassword(oldPassword, password, passwordConfirm) {
    if (!oldPassword || !password) { error.value = 'All fields are required'; return }
    if (password.length < 8) { error.value = 'Password must be at least 8 characters'; return }
    if (password !== passwordConfirm) { error.value = 'Passwords do not match'; return }
    await withLoading(async () => {
      await changePassword(user.value.id, oldPassword, password, passwordConfirm)
      signOut()
    })
  }

  async function deleteAccount(password) {
    if (!password) { error.value = 'Password is required'; return }
    await withLoading(async () => {
      await login(user.value.email, password)
      await deleteAccountApi(user.value.id)
      signOut()
    })
  }

  function signOut() {
    logout()
    user.value = null
    error.value = null
  }

  let _authReadyResolve
  const authReady = new Promise(resolve => { _authReadyResolve = resolve })

  function init() {
    const authStore = getAuthStore()
    if (!authStore.isValid) {
      _authReadyResolve()
      return
    }
    user.value = authStore.model
    authRefresh()
      .then(auth => { user.value = auth.record })
      .catch(() => { signOut() })
      .finally(() => { _authReadyResolve() })
  }

  return { user, loading, error, isAuthenticated, signIn, signUp, signOut, updatePassword, deleteAccount, init, authReady }
})
