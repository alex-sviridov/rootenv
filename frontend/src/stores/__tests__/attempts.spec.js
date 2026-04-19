import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

vi.mock('@/api/attempts', () => ({
  fetchLastAttempt: vi.fn(),
  fetchAttempts: vi.fn(),
  createAttempt: vi.fn(),
  subscribeToAttempt: vi.fn(),
  unsubscribeFromAttempt: vi.fn(),
  fetchActiveAttempt: vi.fn(),
  decommissionAttempt: vi.fn(),
}))

import {
  fetchLastAttempt,
  fetchAttempts,
  createAttempt,
  subscribeToAttempt,
  fetchActiveAttempt,
} from '@/api/attempts'
import { useAttemptsStore } from '../attempts'

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
})

describe('initial state', () => {
  it('starts with null lastAttempt, activeAttempt, empty history, false loading flags, null error', () => {
    const store = useAttemptsStore()
    expect(store.lastAttempt).toBeNull()
    expect(store.activeAttempt).toBeNull()
    expect(store.history).toEqual({ items: [], page: 1, totalPages: 1 })
    expect(store.loading).toBe(false)
    expect(store.historyLoading).toBe(false)
    expect(store.error).toBeNull()
  })
})

describe('canProvision', () => {
  it('returns true when lastAttempt is null', () => {
    const store = useAttemptsStore()
    expect(store.canProvision).toBe(true)
  })

  it('returns true when lastAttempt.status is "decommissioned"', () => {
    const store = useAttemptsStore()
    store.lastAttempt = { id: 'a1', state: 'decommissioned' }
    expect(store.canProvision).toBe(true)
  })

  it('returns false when lastAttempt status is "running"', () => {
    const store = useAttemptsStore()
    store.lastAttempt = { id: 'a1', state: 'running' }
    expect(store.canProvision).toBe(false)
  })

  it('returns false when lastAttempt status is "provisioning"', () => {
    const store = useAttemptsStore()
    store.lastAttempt = { id: 'a1', state: 'provisioning' }
    expect(store.canProvision).toBe(false)
  })
})

describe('loadLastAttempt', () => {
  it('fetches and sets lastAttempt on success', async () => {
    const attempt = { id: 'a1', state: 'running', lab: 'lab-1' }
    fetchLastAttempt.mockResolvedValue(attempt)

    const store = useAttemptsStore()
    await store.loadLastAttempt('lab-1')

    expect(fetchLastAttempt).toHaveBeenCalledWith('lab-1')
    expect(store.lastAttempt).toEqual(attempt)
    expect(store.error).toBeNull()
  })

  it('sets lastAttempt to null and no error on 404', async () => {
    const notFound = Object.assign(new Error('Not found'), { status: 404 })
    fetchLastAttempt.mockRejectedValue(notFound)

    const store = useAttemptsStore()
    await store.loadLastAttempt('lab-1')

    expect(store.lastAttempt).toBeNull()
    expect(store.error).toBeNull()
  })

  it('sets error for non-404 errors', async () => {
    fetchLastAttempt.mockRejectedValue(new Error('network error'))

    const store = useAttemptsStore()
    await store.loadLastAttempt('lab-1')

    expect(store.lastAttempt).toBeNull()
    expect(store.error).toBe('network error')
  })

  it('sets loading true while in-flight, false after', async () => {
    let resolve
    fetchLastAttempt.mockReturnValue(new Promise(r => { resolve = r }))

    const store = useAttemptsStore()
    const promise = store.loadLastAttempt('lab-1')
    expect(store.loading).toBe(true)
    resolve({ id: 'a1', state: 'running' })
    await promise
    expect(store.loading).toBe(false)
  })

  it('clears error before fetching', async () => {
    fetchLastAttempt.mockResolvedValue({ id: 'a1', state: 'running' })

    const store = useAttemptsStore()
    store.error = 'previous error'
    await store.loadLastAttempt('lab-1')

    expect(store.error).toBeNull()
  })
})

describe('loadHistory', () => {
  it('maps paginated result to history state', async () => {
    const result = { items: [{ id: 'a1' }], page: 2, totalPages: 5 }
    fetchAttempts.mockResolvedValue(result)

    const store = useAttemptsStore()
    await store.loadHistory('lab-1', 2, 5)

    expect(fetchAttempts).toHaveBeenCalledWith('lab-1', 2, 5)
    expect(store.history).toEqual({ items: [{ id: 'a1' }], page: 2, totalPages: 5 })
  })

  it('uses historyLoading, not loading, while in-flight', async () => {
    let resolve
    fetchAttempts.mockReturnValue(new Promise(r => { resolve = r }))

    const store = useAttemptsStore()
    const promise = store.loadHistory('lab-1')
    expect(store.historyLoading).toBe(true)
    expect(store.loading).toBe(false)
    resolve({ items: [], page: 1, totalPages: 1 })
    await promise
    expect(store.historyLoading).toBe(false)
  })

  it('defaults to page 1 and perPage 10 when not specified', async () => {
    fetchAttempts.mockResolvedValue({ items: [], page: 1, totalPages: 1 })

    const store = useAttemptsStore()
    await store.loadHistory('lab-1')

    expect(fetchAttempts).toHaveBeenCalledWith('lab-1', 1, 10)
  })

  it('sets error on failure', async () => {
    fetchAttempts.mockRejectedValue(new Error('history failed'))

    const store = useAttemptsStore()
    await store.loadHistory('lab-1')

    expect(store.error).toBe('history failed')
  })
})

describe('startWatching', () => {
  it('subscribes and updates lastAttempt via realtime callback', async () => {
    let capturedCallback
    subscribeToAttempt.mockImplementation(async (_labId, cb) => {
      capturedCallback = cb
      return vi.fn()
    })

    const store = useAttemptsStore()
    await store.startWatching('lab-1')

    const updated = { id: 'a1', state: 'running' }
    capturedCallback(updated)

    expect(store.lastAttempt).toEqual(updated)
  })

  it('calls stopWatching first if already subscribed', async () => {
    const firstUnsub = vi.fn()
    const secondUnsub = vi.fn()
    subscribeToAttempt
      .mockResolvedValueOnce(firstUnsub)
      .mockResolvedValueOnce(secondUnsub)

    const store = useAttemptsStore()
    await store.startWatching('lab-1')
    await store.startWatching('lab-1')

    expect(firstUnsub).toHaveBeenCalled()
    expect(subscribeToAttempt).toHaveBeenCalledTimes(2)
  })
})

describe('stopWatching', () => {
  it('calls and clears the stored unsubscribe function', async () => {
    const unsubFn = vi.fn()
    subscribeToAttempt.mockResolvedValue(unsubFn)

    const store = useAttemptsStore()
    await store.startWatching('lab-1')
    await store.stopWatching()

    expect(unsubFn).toHaveBeenCalled()
  })

  it('does nothing when not currently watching', async () => {
    const store = useAttemptsStore()
    await expect(store.stopWatching()).resolves.toBeUndefined()
  })
})

describe('addAttempt', () => {
  it('creates attempt and sets lastAttempt on success', async () => {
    const created = { id: 'a1', state: 'provisioning', lab: 'lab-1', lab_name: 'My Lab' }
    createAttempt.mockResolvedValue(created)

    const store = useAttemptsStore()
    await store.addAttempt('lab-1', 'My Lab')

    expect(createAttempt).toHaveBeenCalledWith('lab-1', 'My Lab')
    expect(store.lastAttempt).toEqual(created)
    expect(store.error).toBeNull()
  })

  it('sets error and skips API call when canProvision is false', async () => {
    const store = useAttemptsStore()
    store.lastAttempt = { id: 'a1', state: 'running' }

    await store.addAttempt('lab-1', 'My Lab')

    expect(createAttempt).not.toHaveBeenCalled()
    expect(store.error).toBeTruthy()
  })

  it('allows creation when lastAttempt.status is "decommissioned"', async () => {
    const created = { id: 'a2', state: 'provisioning', lab: 'lab-1', lab_name: 'My Lab' }
    createAttempt.mockResolvedValue(created)

    const store = useAttemptsStore()
    store.lastAttempt = { id: 'a1', state: 'decommissioned' }
    await store.addAttempt('lab-1', 'My Lab')

    expect(createAttempt).toHaveBeenCalledWith('lab-1', 'My Lab')
    expect(store.lastAttempt).toEqual(created)
  })

  it('sets loading true while in-flight, false after', async () => {
    let resolve
    createAttempt.mockReturnValue(new Promise(r => { resolve = r }))

    const store = useAttemptsStore()
    const promise = store.addAttempt('lab-1', 'My Lab')
    expect(store.loading).toBe(true)
    resolve({ id: 'a1', state: 'provisioning' })
    await promise
    expect(store.loading).toBe(false)
  })

  it('sets error on API failure', async () => {
    createAttempt.mockRejectedValue(new Error('create failed'))

    const store = useAttemptsStore()
    await store.addAttempt('lab-1', 'My Lab')

    expect(store.error).toBe('create failed')
  })
})

describe('loadActiveAttempt', () => {
  it('fetches and sets activeAttempt on success', async () => {
    const attempt = { id: 'a1', state: 'provisioned', lab: 'lab-1', lab_name: 'My Lab' }
    fetchActiveAttempt.mockResolvedValue(attempt)

    const store = useAttemptsStore()
    await store.loadActiveAttempt()

    expect(fetchActiveAttempt).toHaveBeenCalled()
    expect(store.activeAttempt).toEqual(attempt)
  })

  it('sets activeAttempt to null when no active attempt exists (null returned)', async () => {
    fetchActiveAttempt.mockResolvedValue(null)

    const store = useAttemptsStore()
    store.activeAttempt = { id: 'a1' }
    await store.loadActiveAttempt()

    expect(store.activeAttempt).toBeNull()
  })

  it('sets activeAttempt to null on error', async () => {
    fetchActiveAttempt.mockRejectedValue(new Error('network error'))

    const store = useAttemptsStore()
    store.activeAttempt = { id: 'a1' }
    await store.loadActiveAttempt()

    expect(store.activeAttempt).toBeNull()
  })
})
