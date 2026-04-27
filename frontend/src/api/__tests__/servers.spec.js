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
  it('queries assets collection filtered by attemptId', async () => {
    const servers = [{ id: 's1', name: 'web', state: 'provisioned', status: 'poweredon' }]
    mockGetFullList.mockResolvedValue(servers)

    const result = await fetchServers('attempt-1')

    expect(mockCollection).toHaveBeenCalledWith('assets')
    expect(mockGetFullList).toHaveBeenCalledWith({ filter: 'attempt = "attempt-1"' })
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

  it('calls callback with update action for matching attempt on update event', async () => {
    let innerHandler
    mockSubscribe.mockImplementation(async (_topic, fn) => { innerHandler = fn; return vi.fn() })
    const record = { id: 's1', name: 'web', state: 'provisioned', status: 'poweredon', attempt: 'attempt-1' }
    const callback = vi.fn()

    await subscribeToServers('attempt-1', callback)
    innerHandler({ action: 'update', record })

    expect(callback).toHaveBeenCalledWith({ action: 'update', record })
  })

  it('calls callback with delete action for matching attempt on delete event', async () => {
    let innerHandler
    mockSubscribe.mockImplementation(async (_topic, fn) => { innerHandler = fn; return vi.fn() })
    const callback = vi.fn()

    await subscribeToServers('attempt-1', callback)
    innerHandler({ action: 'delete', record: { id: 's1', attempt: 'attempt-1' } })

    expect(callback).toHaveBeenCalledWith({ action: 'delete', record: { id: 's1' } })
  })

  it('ignores events for a different attempt', async () => {
    let innerHandler
    mockSubscribe.mockImplementation(async (_topic, fn) => { innerHandler = fn; return vi.fn() })
    const callback = vi.fn()

    await subscribeToServers('attempt-1', callback)
    innerHandler({ action: 'update', record: { id: 's2', attempt: 'attempt-other' } })

    expect(callback).not.toHaveBeenCalled()
  })

  it('propagates subscribe errors', async () => {
    mockSubscribe.mockRejectedValue(new Error('subscribe failed'))
    await expect(subscribeToServers('attempt-1', vi.fn())).rejects.toThrow('subscribe failed')
  })
})
