import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

vi.mock('@/api/attempts', () => ({
  fetchLastAttempt: vi.fn(),
  fetchAttempts: vi.fn(),
  createAttempt: vi.fn(),
  fetchActiveAttempt: vi.fn(),
  decommissionAttempt: vi.fn(),
  subscribeToAttempt: vi.fn(),
}))

import {
  fetchLastAttempt,
  fetchAttempts,
  createAttempt,
  fetchActiveAttempt,
  decommissionAttempt,
  subscribeToAttempt,
} from '@/api/attempts'
import { useAttemptsStore } from '../attempts'

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
})

describe('initial state', () => {
  it('starts with null lastAttempt, null activeAttempt, empty servers, empty history, false loading flags, null error', () => {
    const store = useAttemptsStore()
    expect(store.lastAttempt).toBeNull()
    expect(store.activeAttempt).toBeNull()
    expect(store.servers).toEqual([])
    expect(store.history).toEqual({ items: [], page: 1, totalPages: 1 })
    expect(store.loading).toBe(false)
    expect(store.historyLoading).toBe(false)
    expect(store.error).toBeNull()
  })
})

describe('servers', () => {
  it('returns empty array when lastAttempt is null', () => {
    const store = useAttemptsStore()
    expect(store.servers).toEqual([])
  })

  it('returns empty array when lastAttempt has no assets field', () => {
    const store = useAttemptsStore()
    store.lastAttempt = { id: 'a1', current_state: 'provisioning' }
    expect(store.servers).toEqual([])
  })

  it('returns the assets array from lastAttempt', () => {
    const assets = [
      { name: 'server-0', state: 'provisioned', status: 'poweredon', protocols: ['exec'] },
      { name: 'server-1', state: 'provisioning', status: 'poweredoff', protocols: [] },
    ]
    const store = useAttemptsStore()
    store.lastAttempt = { id: 'a1', current_state: 'provisioned', assets }
    expect(store.servers).toEqual(assets)
  })

  it('updates synchronously when lastAttempt.assets changes', () => {
    const store = useAttemptsStore()
    store.lastAttempt = { id: 'a1', assets: [{ name: 'server-0' }] }
    expect(store.servers).toHaveLength(1)
    store.lastAttempt = { id: 'a1', assets: [{ name: 'server-0' }, { name: 'server-1' }] }
    expect(store.servers).toHaveLength(2)
  })

  it('reflects assets from realtime event when watching', async () => {
    let capturedCallback
    subscribeToAttempt.mockImplementation(async (_id, cb) => { capturedCallback = cb; return vi.fn() })

    const store = useAttemptsStore()
    store.lastAttempt = { id: 'a1', current_state: 'provisioning', assets: [] }
    await store.startWatching('a1')

    const updated = { id: 'a1', current_state: 'provisioned', assets: [{ name: 'server-0', state: 'provisioned', status: 'poweredon', protocols: ['exec'] }] }
    capturedCallback({ action: 'update', record: updated })

    expect(store.servers).toEqual(updated.assets)
  })
})

describe('loadLastAttempt', () => {
  it('fetches and sets lastAttempt on success', async () => {
    const attempt = { id: 'a1', current_state: 'provisioned', lab: 'lab-1', assets: [] }
    fetchLastAttempt.mockResolvedValue(attempt)

    const store = useAttemptsStore()
    await store.loadLastAttempt('lab-1')

    expect(fetchLastAttempt).toHaveBeenCalledWith('lab-1')
    expect(store.lastAttempt).toEqual(attempt)
    expect(store.error).toBeNull()
  })

  it('sets lastAttempt to null on 404 without setting error', async () => {
    fetchLastAttempt.mockRejectedValue(Object.assign(new Error('Not found'), { status: 404 }))

    const store = useAttemptsStore()
    await store.loadLastAttempt('lab-1')

    expect(store.lastAttempt).toBeNull()
    expect(store.error).toBeNull()
  })

  it('sets error on non-404 failure', async () => {
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
    resolve({ id: 'a1' })
    await promise
    expect(store.loading).toBe(false)
  })

  it('clears a previous error before fetching', async () => {
    fetchLastAttempt.mockResolvedValue({ id: 'a1' })

    const store = useAttemptsStore()
    store.error = 'previous error'
    await store.loadLastAttempt('lab-1')

    expect(store.error).toBeNull()
  })

  it('suppresses abort errors without setting error state', async () => {
    fetchLastAttempt.mockRejectedValue(Object.assign(new Error('aborted'), { isAbort: true }))

    const store = useAttemptsStore()
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

  it('defaults to page 1, perPage 10', async () => {
    fetchAttempts.mockResolvedValue({ items: [], page: 1, totalPages: 1 })

    const store = useAttemptsStore()
    await store.loadHistory('lab-1')

    expect(fetchAttempts).toHaveBeenCalledWith('lab-1', 1, 10)
  })

  it('uses historyLoading flag, not loading', async () => {
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

  it('sets error on failure', async () => {
    fetchAttempts.mockRejectedValue(new Error('history failed'))

    const store = useAttemptsStore()
    await store.loadHistory('lab-1')

    expect(store.error).toBe('history failed')
  })
})

describe('loadActiveAttempt', () => {
  it('fetches and sets activeAttempt on success', async () => {
    const attempt = { id: 'a1', current_state: 'provisioned', lab: 'lab-1' }
    fetchActiveAttempt.mockResolvedValue(attempt)

    const store = useAttemptsStore()
    await store.loadActiveAttempt()

    expect(store.activeAttempt).toEqual(attempt)
  })

  it('sets activeAttempt to null when API returns null (no active attempt)', async () => {
    fetchActiveAttempt.mockResolvedValue(null)

    const store = useAttemptsStore()
    store.activeAttempt = { id: 'a1' }
    await store.loadActiveAttempt()

    expect(store.activeAttempt).toBeNull()
  })

  it('sets activeAttempt to null on error without propagating', async () => {
    fetchActiveAttempt.mockRejectedValue(new Error('network error'))

    const store = useAttemptsStore()
    store.activeAttempt = { id: 'a1' }
    await store.loadActiveAttempt()

    expect(store.activeAttempt).toBeNull()
  })
})

describe('addAttempt', () => {
  it('creates attempt and sets both lastAttempt and activeAttempt', async () => {
    const created = { id: 'a1', current_state: 'new', lab: 'lab-1', lab_name: 'My Lab' }
    createAttempt.mockResolvedValue(created)

    const store = useAttemptsStore()
    await store.addAttempt('lab-1', 'My Lab')

    expect(createAttempt).toHaveBeenCalledWith('lab-1', 'My Lab')
    expect(store.lastAttempt).toEqual(created)
    expect(store.activeAttempt).toEqual(created)
    expect(store.error).toBeNull()
  })

  it('sets loading true while in-flight, false after', async () => {
    let resolve
    createAttempt.mockReturnValue(new Promise(r => { resolve = r }))

    const store = useAttemptsStore()
    const promise = store.addAttempt('lab-1', 'My Lab')
    expect(store.loading).toBe(true)
    resolve({ id: 'a1' })
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

describe('removeAttempt', () => {
  it('does nothing when there is no lastAttempt', async () => {
    const store = useAttemptsStore()
    await store.removeAttempt()
    expect(decommissionAttempt).not.toHaveBeenCalled()
  })

  it('calls decommissionAttempt with the current attempt id', async () => {
    decommissionAttempt.mockResolvedValue({})
    const store = useAttemptsStore()
    store.lastAttempt = { id: 'a1', current_state: 'provisioned' }

    await store.removeAttempt()

    expect(decommissionAttempt).toHaveBeenCalledWith('a1')
  })

  it('optimistically sets lastAttempt.current_state to decommissioning', async () => {
    decommissionAttempt.mockResolvedValue({})
    const store = useAttemptsStore()
    store.lastAttempt = { id: 'a1', current_state: 'provisioned' }

    await store.removeAttempt()

    expect(store.lastAttempt.current_state).toBe('decommissioning')
  })

  it('also updates activeAttempt when it is the same attempt', async () => {
    decommissionAttempt.mockResolvedValue({})
    const store = useAttemptsStore()
    store.lastAttempt = { id: 'a1', current_state: 'provisioned' }
    store.activeAttempt = { id: 'a1', current_state: 'provisioned' }

    await store.removeAttempt()

    expect(store.activeAttempt.current_state).toBe('decommissioning')
  })

  it('does not touch activeAttempt when it is a different attempt', async () => {
    decommissionAttempt.mockResolvedValue({})
    const store = useAttemptsStore()
    store.lastAttempt = { id: 'a1', current_state: 'provisioned' }
    store.activeAttempt = { id: 'a2', current_state: 'provisioned' }

    await store.removeAttempt()

    expect(store.activeAttempt.current_state).toBe('provisioned')
  })

  it('sets loading true while in-flight, false after', async () => {
    let resolve
    decommissionAttempt.mockReturnValue(new Promise(r => { resolve = r }))
    const store = useAttemptsStore()
    store.lastAttempt = { id: 'a1', current_state: 'provisioned' }

    const promise = store.removeAttempt()
    expect(store.loading).toBe(true)
    resolve({})
    await promise
    expect(store.loading).toBe(false)
  })

  it('sets error on API failure', async () => {
    decommissionAttempt.mockRejectedValue(new Error('decommission failed'))
    const store = useAttemptsStore()
    store.lastAttempt = { id: 'a1', current_state: 'provisioned' }

    await store.removeAttempt()

    expect(store.error).toBe('decommission failed')
  })
})

describe('startWatching / stopWatching', () => {
  it('subscribes to the given attempt id', async () => {
    subscribeToAttempt.mockResolvedValue(vi.fn())
    const store = useAttemptsStore()

    await store.startWatching('a1')

    expect(subscribeToAttempt).toHaveBeenCalledWith('a1', expect.any(Function))
  })

  it('routes update event to lastAttempt when ids match', async () => {
    let capturedCallback
    subscribeToAttempt.mockImplementation(async (_id, cb) => { capturedCallback = cb; return vi.fn() })

    const store = useAttemptsStore()
    store.lastAttempt = { id: 'a1', current_state: 'provisioning', assets: [] }
    await store.startWatching('a1')

    const updated = { id: 'a1', current_state: 'provisioned', assets: [{ name: 'server-0' }] }
    capturedCallback({ action: 'update', record: updated })

    expect(store.lastAttempt).toEqual(updated)
  })

  it('routes update event to activeAttempt when ids match', async () => {
    let capturedCallback
    subscribeToAttempt.mockImplementation(async (_id, cb) => { capturedCallback = cb; return vi.fn() })

    const store = useAttemptsStore()
    store.activeAttempt = { id: 'a1', current_state: 'provisioning' }
    await store.startWatching('a1')

    const updated = { id: 'a1', current_state: 'provisioned', assets: [] }
    capturedCallback({ action: 'update', record: updated })

    expect(store.activeAttempt).toEqual(updated)
  })

  it('updates both lastAttempt and activeAttempt when both match the event id', async () => {
    let capturedCallback
    subscribeToAttempt.mockImplementation(async (_id, cb) => { capturedCallback = cb; return vi.fn() })

    const store = useAttemptsStore()
    store.lastAttempt = { id: 'a1', current_state: 'provisioning' }
    store.activeAttempt = { id: 'a1', current_state: 'provisioning' }
    await store.startWatching('a1')

    const updated = { id: 'a1', current_state: 'provisioned', assets: [] }
    capturedCallback({ action: 'update', record: updated })

    expect(store.lastAttempt).toEqual(updated)
    expect(store.activeAttempt).toEqual(updated)
  })

  it('ignores events whose id does not match lastAttempt or activeAttempt', async () => {
    let capturedCallback
    subscribeToAttempt.mockImplementation(async (_id, cb) => { capturedCallback = cb; return vi.fn() })

    const store = useAttemptsStore()
    store.lastAttempt = { id: 'a1', current_state: 'provisioning' }
    await store.startWatching('a1')

    capturedCallback({ action: 'update', record: { id: 'other', current_state: 'provisioned' } })

    expect(store.lastAttempt.current_state).toBe('provisioning')
  })

  it('stops prior subscription before starting a new one', async () => {
    const firstUnsub = vi.fn()
    subscribeToAttempt
      .mockResolvedValueOnce(firstUnsub)
      .mockResolvedValueOnce(vi.fn())

    const store = useAttemptsStore()
    await store.startWatching('a1')
    await store.startWatching('a2')

    expect(firstUnsub).toHaveBeenCalled()
    expect(subscribeToAttempt).toHaveBeenCalledTimes(2)
  })

  it('stopWatching calls unsubscribe', async () => {
    const unsubFn = vi.fn()
    subscribeToAttempt.mockResolvedValue(unsubFn)

    const store = useAttemptsStore()
    await store.startWatching('a1')
    await store.stopWatching()

    expect(unsubFn).toHaveBeenCalled()
  })

  it('stopWatching is safe to call when not watching', async () => {
    const store = useAttemptsStore()
    await expect(store.stopWatching()).resolves.toBeUndefined()
  })

  it('stopWatching prevents further calls to unsubscribe on double-stop', async () => {
    const unsubFn = vi.fn()
    subscribeToAttempt.mockResolvedValue(unsubFn)

    const store = useAttemptsStore()
    await store.startWatching('a1')
    await store.stopWatching()
    await store.stopWatching()

    expect(unsubFn).toHaveBeenCalledTimes(1)
  })
})
