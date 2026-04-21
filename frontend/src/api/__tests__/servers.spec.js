import { describe, it, expect, vi, beforeEach } from 'vitest'

const {
  mockGetFullList,
  mockGetFirstListItem,
  mockSubscribe,
  mockCollection,
} = vi.hoisted(() => {
  const mockGetFullList = vi.fn()
  const mockGetFirstListItem = vi.fn()
  const mockSubscribe = vi.fn()
  const mockCollection = vi.fn(() => ({
    getFullList: mockGetFullList,
    getFirstListItem: mockGetFirstListItem,
    subscribe: mockSubscribe,
  }))
  return { mockGetFullList, mockGetFirstListItem, mockSubscribe, mockCollection }
})

vi.mock('@/lib/pb', () => ({ pb: { collection: mockCollection } }))

import { fetchServers, subscribeToServers } from '../servers'

beforeEach(() => vi.clearAllMocks())

describe('fetchServers', () => {
  it('queries assets_userview filtered by attemptId', async () => {
    const servers = [{ id: 's1', name: 'web', state: 'provisioned', status: 'poweredon' }]
    mockGetFullList.mockResolvedValue(servers)

    const result = await fetchServers('attempt-1')

    expect(mockCollection).toHaveBeenCalledWith('assets_userview')
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
  it('subscribes to assets collection and returns unsubscribe fn', async () => {
    const unsubFn = vi.fn()
    mockSubscribe.mockResolvedValue(unsubFn)

    const result = await subscribeToServers('attempt-1', vi.fn())

    expect(mockCollection).toHaveBeenCalledWith('assets')
    expect(mockSubscribe).toHaveBeenCalledWith('*', expect.any(Function))
    expect(result).toBe(unsubFn)
  })

  it('re-fetches from assets_userview and calls callback on update event', async () => {
    let innerHandler
    mockSubscribe.mockImplementation(async (_topic, fn) => { innerHandler = fn; return vi.fn() })
    const fresh = { id: 's1', name: 'web', state: 'provisioned', status: 'poweredon', attempt_id: 'attempt-1' }
    mockGetFirstListItem.mockResolvedValue(fresh)
    const callback = vi.fn()

    await subscribeToServers('attempt-1', callback)
    await innerHandler({ action: 'update', record: { id: 's1' } })

    expect(mockCollection).toHaveBeenCalledWith('assets_userview')
    expect(mockGetFirstListItem).toHaveBeenCalledWith('id = "s1" && attempt_id = "attempt-1"', { requestKey: null })
    expect(callback).toHaveBeenCalledWith({ action: 'update', record: fresh })
  })

  it('calls callback with delete action on delete event without re-fetching', async () => {
    let innerHandler
    mockSubscribe.mockImplementation(async (_topic, fn) => { innerHandler = fn; return vi.fn() })
    const callback = vi.fn()

    await subscribeToServers('attempt-1', callback)
    await innerHandler({ action: 'delete', record: { id: 's1' } })

    expect(mockGetFirstListItem).not.toHaveBeenCalled()
    expect(callback).toHaveBeenCalledWith({ action: 'delete', record: { id: 's1' } })
  })

  it('silently ignores re-fetch errors', async () => {
    let innerHandler
    mockSubscribe.mockImplementation(async (_topic, fn) => { innerHandler = fn; return vi.fn() })
    mockGetFirstListItem.mockRejectedValue(new Error('not found'))
    const callback = vi.fn()

    await subscribeToServers('attempt-1', callback)
    innerHandler({ action: 'update', record: { id: 's1' } })
    await Promise.resolve()
    expect(callback).not.toHaveBeenCalled()
  })

  it('propagates subscribe errors', async () => {
    mockSubscribe.mockRejectedValue(new Error('subscribe failed'))
    await expect(subscribeToServers('attempt-1', vi.fn())).rejects.toThrow('subscribe failed')
  })
})
