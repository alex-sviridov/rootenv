import { describe, it, expect, vi, beforeEach } from 'vitest'

const {
  mockGetFullList,
  mockGetOne,
  mockSubscribe,
  mockUnsubscribe,
  mockCollection,
} = vi.hoisted(() => {
  const mockGetFullList = vi.fn()
  const mockGetOne = vi.fn()
  const mockSubscribe = vi.fn()
  const mockUnsubscribe = vi.fn()
  const mockCollection = vi.fn(() => ({
    getFullList: mockGetFullList,
    getOne: mockGetOne,
    subscribe: mockSubscribe,
    unsubscribe: mockUnsubscribe,
  }))
  return { mockGetFullList, mockGetOne, mockSubscribe, mockUnsubscribe, mockCollection }
})

vi.mock('@/lib/pb', () => ({ pb: { collection: mockCollection } }))

import { fetchServers, subscribeToServers, unsubscribeFromServers } from '../servers'

beforeEach(() => vi.clearAllMocks())

describe('fetchServers', () => {
  it('queries servers_userview filtered by attemptId', async () => {
    const servers = [{ id: 's1', name: 'web', state: 'provisioned', status: 'poweredon' }]
    mockGetFullList.mockResolvedValue(servers)

    const result = await fetchServers('attempt-1')

    expect(mockCollection).toHaveBeenCalledWith('servers_userview')
    expect(mockGetFullList).toHaveBeenCalledWith({ filter: 'attempt_id = "attempt-1"' })
    expect(result).toEqual(servers)
  })

  it('returns empty array when no servers exist', async () => {
    mockGetFullList.mockResolvedValue([])
    const result = await fetchServers('attempt-1')
    expect(result).toEqual([])
  })

  it('propagates errors', async () => {
    mockGetFullList.mockRejectedValue(new Error('network error'))
    await expect(fetchServers('attempt-1')).rejects.toThrow('network error')
  })
})

describe('subscribeToServers', () => {
  it('subscribes to base servers collection and returns unsubscribe fn', async () => {
    const unsubFn = vi.fn()
    mockSubscribe.mockResolvedValue(unsubFn)
    mockGetOne.mockResolvedValue({ id: 's1', name: 'web', state: 'provisioned', status: 'poweredon' })

    const result = await subscribeToServers('attempt-1', vi.fn())

    expect(mockCollection).toHaveBeenCalledWith('servers')
    expect(mockSubscribe).toHaveBeenCalledWith('*', expect.any(Function))
    expect(result).toBe(unsubFn)
  })

  it('re-fetches from view and forwards to callback when attempt matches', async () => {
    let innerHandler
    mockSubscribe.mockImplementation(async (_topic, fn) => {
      innerHandler = fn
      return vi.fn()
    })
    const fresh = { id: 's1', name: 'web', state: 'provisioned', status: 'poweredon', attempt_id: 'attempt-1' }
    mockGetOne.mockResolvedValue(fresh)
    const callback = vi.fn()

    await subscribeToServers('attempt-1', callback)
    await innerHandler({ action: 'update', record: { id: 's1', attempt: 'attempt-1' } })

    expect(mockGetOne).toHaveBeenCalledWith('s1')
    expect(callback).toHaveBeenCalledWith({ action: 'update', record: fresh })
  })

  it('forwards delete event without re-fetching', async () => {
    let innerHandler
    mockSubscribe.mockImplementation(async (_topic, fn) => {
      innerHandler = fn
      return vi.fn()
    })
    const callback = vi.fn()

    await subscribeToServers('attempt-1', callback)
    await innerHandler({ action: 'delete', record: { id: 's1', attempt: 'attempt-1' } })

    expect(mockGetOne).not.toHaveBeenCalled()
    expect(callback).toHaveBeenCalledWith({ action: 'delete', record: { id: 's1' } })
  })

  it('ignores events for other attempts', async () => {
    let innerHandler
    mockSubscribe.mockImplementation(async (_topic, fn) => {
      innerHandler = fn
      return vi.fn()
    })
    const callback = vi.fn()

    await subscribeToServers('attempt-1', callback)
    await innerHandler({ action: 'update', record: { id: 's2', attempt: 'attempt-other' } })

    expect(mockGetOne).not.toHaveBeenCalled()
    expect(callback).not.toHaveBeenCalled()
  })

  it('propagates errors', async () => {
    mockSubscribe.mockRejectedValue(new Error('subscribe failed'))
    await expect(subscribeToServers('attempt-1', vi.fn())).rejects.toThrow('subscribe failed')
  })
})

describe('unsubscribeFromServers', () => {
  it('calls unsubscribe("*") on base servers collection', () => {
    mockUnsubscribe.mockReturnValue(undefined)

    unsubscribeFromServers()

    expect(mockCollection).toHaveBeenCalledWith('servers')
    expect(mockUnsubscribe).toHaveBeenCalledWith('*')
  })
})
