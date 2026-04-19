import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

vi.mock('@/api/servers', () => ({
  fetchServers: vi.fn(),
  subscribeToServers: vi.fn(),
}))

import { fetchServers, subscribeToServers } from '@/api/servers'
import { useServersStore } from '../servers'

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
})

describe('initial state', () => {
  it('starts with empty servers array and loading false', () => {
    const store = useServersStore()
    expect(store.servers).toEqual([])
    expect(store.loading).toBe(false)
  })
})

describe('loadServers', () => {
  it('fetches and sets servers on success', async () => {
    const servers = [{ id: 's1', name: 'web', state: 'provisioned', status: 'poweredon', attempt_id: 'a1' }]
    fetchServers.mockResolvedValue(servers)

    const store = useServersStore()
    await store.loadServers('a1')

    expect(fetchServers).toHaveBeenCalledWith('a1')
    expect(store.servers).toEqual(servers)
    expect(store.loading).toBe(false)
  })

  it('clears servers before loading', async () => {
    fetchServers.mockResolvedValue([{ id: 's2', name: 'db' }])

    const store = useServersStore()
    store.servers = [{ id: 's1', name: 'web' }]
    await store.loadServers('a1')

    expect(store.servers).toEqual([{ id: 's2', name: 'db' }])
  })

  it('sets loading true while in-flight, false after', async () => {
    let resolve
    fetchServers.mockReturnValue(new Promise(r => { resolve = r }))

    const store = useServersStore()
    const promise = store.loadServers('a1')
    expect(store.loading).toBe(true)
    resolve([])
    await promise
    expect(store.loading).toBe(false)
  })

  it('sets loading false and clears servers on error', async () => {
    fetchServers.mockRejectedValue(new Error('failed'))

    const store = useServersStore()
    store.servers = [{ id: 's1' }]
    await store.loadServers('a1')

    expect(store.loading).toBe(false)
    expect(store.servers).toEqual([])
  })
})

describe('startWatching', () => {
  it('subscribes and upserts new server records', async () => {
    let capturedCallback
    subscribeToServers.mockImplementation(async (_attemptId, cb) => {
      capturedCallback = cb
      return vi.fn()
    })

    const store = useServersStore()
    await store.startWatching('a1')

    const newServer = { id: 's1', name: 'web', state: 'provisioning', status: 'poweredoff' }
    capturedCallback({ action: 'create', record: newServer })

    expect(store.servers).toContainEqual(newServer)
  })

  it('updates existing server record in-place', async () => {
    let capturedCallback
    subscribeToServers.mockImplementation(async (_attemptId, cb) => {
      capturedCallback = cb
      return vi.fn()
    })

    const store = useServersStore()
    store.servers = [{ id: 's1', name: 'web', state: 'provisioning', status: 'poweredoff' }]
    await store.startWatching('a1')

    const updated = { id: 's1', name: 'web', state: 'provisioned', status: 'poweredon' }
    capturedCallback({ action: 'update', record: updated })

    expect(store.servers).toHaveLength(1)
    expect(store.servers[0]).toEqual(updated)
  })

  it('removes server on delete event', async () => {
    let capturedCallback
    subscribeToServers.mockImplementation(async (_attemptId, cb) => {
      capturedCallback = cb
      return vi.fn()
    })

    const store = useServersStore()
    store.servers = [{ id: 's1', name: 'web' }, { id: 's2', name: 'db' }]
    await store.startWatching('a1')

    capturedCallback({ action: 'delete', record: { id: 's1' } })

    expect(store.servers).toHaveLength(1)
    expect(store.servers[0].id).toBe('s2')
  })

  it('stops previous subscription before starting a new one', async () => {
    const firstUnsub = vi.fn()
    const secondUnsub = vi.fn()
    subscribeToServers
      .mockResolvedValueOnce(firstUnsub)
      .mockResolvedValueOnce(secondUnsub)

    const store = useServersStore()
    await store.startWatching('a1')
    await store.startWatching('a1')

    expect(firstUnsub).toHaveBeenCalled()
    expect(subscribeToServers).toHaveBeenCalledTimes(2)
  })
})

describe('stopWatching', () => {
  it('calls unsubscribe and clears servers', async () => {
    const unsubFn = vi.fn()
    subscribeToServers.mockResolvedValue(unsubFn)

    const store = useServersStore()
    store.servers = [{ id: 's1' }]
    await store.startWatching('a1')
    await store.stopWatching()

    expect(unsubFn).toHaveBeenCalled()
    expect(store.servers).toEqual([])
  })

  it('does nothing when not watching', async () => {
    const store = useServersStore()
    await expect(store.stopWatching()).resolves.toBeUndefined()
    expect(store.servers).toEqual([])
  })
})
