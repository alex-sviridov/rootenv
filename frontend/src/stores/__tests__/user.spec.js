import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

vi.mock('@/api/auth', () => ({
  login: vi.fn(),
  register: vi.fn(),
  logout: vi.fn(),
  getAuthStore: vi.fn(),
  changePassword: vi.fn(),
  deleteAccount: vi.fn(),
}))

import { login, register, logout, getAuthStore, changePassword, deleteAccount as deleteAccountApi } from '@/api/auth'
import { useUserStore } from '../user'

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
})

describe('initial state', () => {
  it('starts unauthenticated with no loading or error', () => {
    const store = useUserStore()
    expect(store.user).toBeNull()
    expect(store.isAuthenticated).toBe(false)
    expect(store.loading).toBe(false)
    expect(store.error).toBeNull()
  })
})

describe('isAuthenticated', () => {
  it('returns true when user is set', () => {
    const store = useUserStore()
    store.user = { id: 'u1' }
    expect(store.isAuthenticated).toBe(true)
  })
})

describe('signIn', () => {
  it('sets error and skips API call when fields are empty', async () => {
    const store = useUserStore()
    await store.signIn('', 'pass')
    expect(store.error).toBeTruthy()
    expect(login).not.toHaveBeenCalled()

    await store.signIn('a@b.com', '')
    expect(store.error).toBeTruthy()
    expect(login).not.toHaveBeenCalled()
  })

  it('sets error and skips API call for invalid email', async () => {
    const store = useUserStore()
    await store.signIn('not-an-email', 'pass123')
    expect(store.error).toBeTruthy()
    expect(login).not.toHaveBeenCalled()
  })

  it('sets user on success', async () => {
    const record = { id: 'u1', email: 'a@b.com' }
    login.mockResolvedValue({ token: 'tok', record })
    const store = useUserStore()
    await store.signIn('a@b.com', 'pass123')
    expect(store.user).toEqual(record)
    expect(store.error).toBeNull()
  })

  it('captures error on failure', async () => {
    login.mockRejectedValue(new Error('wrong credentials'))
    const store = useUserStore()
    await store.signIn('a@b.com', 'pass123')
    expect(store.user).toBeNull()
    expect(store.error).toBe('wrong credentials')
  })

  it('sets loading true while in-flight', async () => {
    let resolve
    login.mockReturnValue(new Promise(r => { resolve = r }))
    const store = useUserStore()
    const promise = store.signIn('a@b.com', 'pass123')
    expect(store.loading).toBe(true)
    resolve({ token: 'tok', record: { id: 'u1' } })
    await promise
    expect(store.loading).toBe(false)
  })
})

describe('signUp', () => {
  it('sets error and skips API when email or password is empty', async () => {
    const store = useUserStore()
    await store.signUp('', 'pass123', 'pass123')
    expect(store.error).toBeTruthy()
    expect(register).not.toHaveBeenCalled()
  })

  it('sets error for invalid email', async () => {
    const store = useUserStore()
    await store.signUp('bad', 'pass123', 'pass123')
    expect(store.error).toBeTruthy()
    expect(register).not.toHaveBeenCalled()
  })

  it('sets error when password is too short', async () => {
    const store = useUserStore()
    await store.signUp('a@b.com', 'short', 'short')
    expect(store.error).toBeTruthy()
    expect(register).not.toHaveBeenCalled()
  })

  it('sets error when passwords do not match', async () => {
    const store = useUserStore()
    await store.signUp('a@b.com', 'pass123', 'different')
    expect(store.error).toBeTruthy()
    expect(register).not.toHaveBeenCalled()
  })

  it('registers then auto-logs in and sets user on success', async () => {
    const record = { id: 'u1', email: 'a@b.com' }
    register.mockResolvedValue(record)
    login.mockResolvedValue({ token: 'tok', record })
    const store = useUserStore()
    await store.signUp('a@b.com', 'password1', 'password1')
    expect(register).toHaveBeenCalledWith('a@b.com', 'password1', 'password1')
    expect(login).toHaveBeenCalledWith('a@b.com', 'password1')
    expect(store.user).toEqual(record)
  })

  it('captures error and skips login if register fails', async () => {
    register.mockRejectedValue(new Error('email taken'))
    const store = useUserStore()
    await store.signUp('a@b.com', 'password1', 'password1')
    expect(store.error).toBe('email taken')
    expect(login).not.toHaveBeenCalled()
  })

  it('captures error if auto-login after register fails', async () => {
    register.mockResolvedValue({})
    login.mockRejectedValue(new Error('login failed'))
    const store = useUserStore()
    await store.signUp('a@b.com', 'password1', 'password1')
    expect(store.error).toBe('login failed')
    expect(store.user).toBeNull()
  })
})

describe('signOut', () => {
  it('clears user and error, calls logout', () => {
    const store = useUserStore()
    store.user = { id: 'u1' }
    store.error = 'some error'
    store.signOut()
    expect(store.user).toBeNull()
    expect(store.error).toBeNull()
    expect(logout).toHaveBeenCalled()
  })
})

describe('updatePassword', () => {
  it('sets error and skips API when any field is empty', async () => {
    const store = useUserStore()
    store.user = { id: 'u1' }
    await store.updatePassword('', 'new12345', 'new12345')
    expect(store.error).toBeTruthy()
    expect(changePassword).not.toHaveBeenCalled()
  })

  it('sets error when new password is too short', async () => {
    const store = useUserStore()
    store.user = { id: 'u1' }
    await store.updatePassword('old123', 'short', 'short')
    expect(store.error).toBeTruthy()
    expect(changePassword).not.toHaveBeenCalled()
  })

  it('sets error when new passwords do not match', async () => {
    const store = useUserStore()
    store.user = { id: 'u1' }
    await store.updatePassword('old123', 'new12345', 'different')
    expect(store.error).toBeTruthy()
    expect(changePassword).not.toHaveBeenCalled()
  })

  it('calls changePassword with user id and signs out on success', async () => {
    changePassword.mockResolvedValue({})
    const store = useUserStore()
    store.user = { id: 'u1' }
    await store.updatePassword('old12345', 'new12345', 'new12345')
    expect(changePassword).toHaveBeenCalledWith('u1', 'old12345', 'new12345', 'new12345')
    expect(store.user).toBeNull()
    expect(logout).toHaveBeenCalled()
  })

  it('captures error if changePassword fails', async () => {
    changePassword.mockRejectedValue(new Error('wrong old password'))
    const store = useUserStore()
    store.user = { id: 'u1' }
    await store.updatePassword('bad', 'new12345', 'new12345')
    expect(store.error).toBe('wrong old password')
    expect(store.user).not.toBeNull()
  })

  it('extracts the first field-level message from a PocketBase error', async () => {
    const pbError = Object.assign(new Error('Failed to update record.'), {
      data: { oldPassword: { code: 'validation_invalid_credentials', message: 'Missing or invalid credentials.' } },
    })
    changePassword.mockRejectedValue(pbError)
    const store = useUserStore()
    store.user = { id: 'u1' }
    await store.updatePassword('wrong', 'new12345', 'new12345')
    expect(store.error).toBe('Missing or invalid credentials.')
  })
})

describe('deleteAccount', () => {
  it('sets error and skips API when password is empty', async () => {
    const store = useUserStore()
    store.user = { id: 'u1', email: 'a@b.com' }
    await store.deleteAccount('')
    expect(store.error).toBeTruthy()
    expect(login).not.toHaveBeenCalled()
    expect(deleteAccountApi).not.toHaveBeenCalled()
  })

  it('verifies password via login then deletes account and signs out', async () => {
    const record = { id: 'u1', email: 'a@b.com' }
    login.mockResolvedValue({ token: 'tok', record })
    deleteAccountApi.mockResolvedValue(undefined)
    const store = useUserStore()
    store.user = record
    await store.deleteAccount('pass123')
    expect(login).toHaveBeenCalledWith('a@b.com', 'pass123')
    expect(deleteAccountApi).toHaveBeenCalledWith('u1')
    expect(store.user).toBeNull()
    expect(logout).toHaveBeenCalled()
  })

  it('captures error and preserves session when password is wrong', async () => {
    login.mockRejectedValue(new Error('invalid credentials'))
    const store = useUserStore()
    store.user = { id: 'u1', email: 'a@b.com' }
    await store.deleteAccount('wrongpass')
    expect(store.error).toBe('invalid credentials')
    expect(deleteAccountApi).not.toHaveBeenCalled()
    expect(store.user).not.toBeNull()
  })

  it('captures error and preserves session when delete API fails', async () => {
    login.mockResolvedValue({ token: 'tok', record: { id: 'u1', email: 'a@b.com' } })
    deleteAccountApi.mockRejectedValue(new Error('delete failed'))
    const store = useUserStore()
    store.user = { id: 'u1', email: 'a@b.com' }
    await store.deleteAccount('pass123')
    expect(store.error).toBe('delete failed')
    expect(store.user).not.toBeNull()
  })
})

describe('init', () => {
  it('restores user from a valid auth store', () => {
    const model = { id: 'u1', email: 'a@b.com' }
    getAuthStore.mockReturnValue({ isValid: true, model })
    const store = useUserStore()
    store.init()
    expect(store.user).toEqual(model)
  })

  it('leaves user null when auth store is invalid', () => {
    getAuthStore.mockReturnValue({ isValid: false, model: null })
    const store = useUserStore()
    store.init()
    expect(store.user).toBeNull()
  })
})
