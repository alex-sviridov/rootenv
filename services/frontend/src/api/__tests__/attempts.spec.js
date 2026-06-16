import { describe, it, expect, vi, beforeEach } from 'vitest'

const {
  mockGetFirstListItem,
  mockGetList,
  mockCreate,
  mockUpdate,
  mockSubscribe,
  mockCollection,
} = vi.hoisted(() => {
  const mockGetFirstListItem = vi.fn()
  const mockGetList = vi.fn()
  const mockCreate = vi.fn()
  const mockUpdate = vi.fn()
  const mockSubscribe = vi.fn()
  const mockCollection = vi.fn(() => ({
    getFirstListItem: mockGetFirstListItem,
    getList: mockGetList,
    create: mockCreate,
    update: mockUpdate,
    subscribe: mockSubscribe,
  }))
  return { mockGetFirstListItem, mockGetList, mockCreate, mockUpdate, mockSubscribe, mockCollection }
})

vi.mock('@/lib/pb', () => ({ pb: { collection: mockCollection, authStore: { record: { id: 'user-1' } } } }))

import {
  fetchLastAttempt,
  fetchAttempts,
  createAttempt,
  fetchActiveAttempt,
  decommissionAttempt,
  subscribeToAttempt,
} from '../attempts'

beforeEach(() => vi.clearAllMocks())

describe('fetchLastAttempt', () => {
  it('queries attempts collection filtered by labId, sorted by -updated', async () => {
    const attempt = { id: 'a1', current_state: 'provisioned', lab: 'lab-1' }
    mockGetFirstListItem.mockResolvedValue(attempt)

    const result = await fetchLastAttempt('lab-1')

    expect(mockCollection).toHaveBeenCalledWith('attempts')
    expect(mockGetFirstListItem).toHaveBeenCalledWith('lab = "lab-1"', { sort: '-updated', requestKey: 'last-attempt-lab-1' })
    expect(result).toEqual(attempt)
  })

  it('uses a per-lab requestKey to allow concurrent lab queries', async () => {
    mockGetFirstListItem.mockResolvedValue({})
    await fetchLastAttempt('lab-2')
    expect(mockGetFirstListItem).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ requestKey: 'last-attempt-lab-2' }))
  })

  it('propagates errors', async () => {
    mockGetFirstListItem.mockRejectedValue(new Error('network error'))
    await expect(fetchLastAttempt('lab-1')).rejects.toThrow('network error')
  })
})

describe('fetchAttempts', () => {
  it('queries attempts collection with page, perPage, filter and sort', async () => {
    const page = { items: [], page: 2, totalPages: 3 }
    mockGetList.mockResolvedValue(page)

    const result = await fetchAttempts('lab-1', 2, 5)

    expect(mockCollection).toHaveBeenCalledWith('attempts')
    expect(mockGetList).toHaveBeenCalledWith(2, 5, {
      filter: 'lab = "lab-1"',
      sort: '-updated',
      requestKey: 'attempts-list-lab-1-2',
    })
    expect(result).toEqual(page)
  })

  it('includes page number in requestKey to allow concurrent page fetches', async () => {
    mockGetList.mockResolvedValue({ items: [], page: 3, totalPages: 5 })
    await fetchAttempts('lab-1', 3, 10)
    expect(mockGetList).toHaveBeenCalledWith(3, 10, expect.objectContaining({ requestKey: 'attempts-list-lab-1-3' }))
  })

  it('propagates errors', async () => {
    mockGetList.mockRejectedValue(new Error('not found'))
    await expect(fetchAttempts('lab-1', 1, 10)).rejects.toThrow('not found')
  })
})

describe('createAttempt', () => {
  it('creates a record with lab id, lab_name, and authenticated user id', async () => {
    const created = { id: 'a1', lab: 'lab-1', lab_name: 'My Lab', current_state: 'new' }
    mockCreate.mockResolvedValue(created)

    const result = await createAttempt('lab-1', 'My Lab')

    expect(mockCollection).toHaveBeenCalledWith('attempts')
    expect(mockCreate).toHaveBeenCalledWith({ lab: 'lab-1', lab_name: 'My Lab', user: 'user-1' })
    expect(result).toEqual(created)
  })

  it('propagates errors', async () => {
    mockCreate.mockRejectedValue(new Error('forbidden'))
    await expect(createAttempt('lab-1', 'My Lab')).rejects.toThrow('forbidden')
  })
})

describe('decommissionAttempt', () => {
  it('patches desired_state=decommissioned on the given attempt', async () => {
    mockUpdate.mockResolvedValue({})

    await decommissionAttempt('attempt-1')

    expect(mockCollection).toHaveBeenCalledWith('attempts')
    expect(mockUpdate).toHaveBeenCalledWith('attempt-1', { desired_state: 'decommissioned' })
  })

  it('propagates errors', async () => {
    mockUpdate.mockRejectedValue(new Error('forbidden'))
    await expect(decommissionAttempt('attempt-1')).rejects.toThrow('forbidden')
  })
})

describe('fetchActiveAttempt', () => {
  it('queries for any attempt that is not decommissioned', async () => {
    const attempt = { id: 'a1', current_state: 'provisioned', lab: 'lab-1' }
    mockGetFirstListItem.mockResolvedValue(attempt)

    const result = await fetchActiveAttempt()

    expect(mockCollection).toHaveBeenCalledWith('attempts')
    expect(mockGetFirstListItem).toHaveBeenCalledWith('current_state != "decommissioned"', { requestKey: 'active-attempt' })
    expect(result).toEqual(attempt)
  })

  it('returns null on 404 (no active attempt exists)', async () => {
    mockGetFirstListItem.mockRejectedValue(Object.assign(new Error('Not found'), { status: 404 }))
    expect(await fetchActiveAttempt()).toBeNull()
  })

  it('propagates non-404 errors', async () => {
    mockGetFirstListItem.mockRejectedValue(new Error('network error'))
    await expect(fetchActiveAttempt()).rejects.toThrow('network error')
  })
})

describe('subscribeToAttempt', () => {
  it('subscribes to the specific attempt record and returns unsubscribe fn', async () => {
    const unsubFn = vi.fn()
    mockSubscribe.mockResolvedValue(unsubFn)

    const result = await subscribeToAttempt('attempt-1', vi.fn())

    expect(mockCollection).toHaveBeenCalledWith('attempts')
    expect(mockSubscribe).toHaveBeenCalledWith('attempt-1', expect.any(Function))
    expect(result).toBe(unsubFn)
  })

  it('forwards realtime events to the callback', async () => {
    let capturedHandler
    mockSubscribe.mockImplementation(async (_topic, fn) => { capturedHandler = fn; return vi.fn() })
    const callback = vi.fn()

    await subscribeToAttempt('attempt-1', callback)
    const event = { action: 'update', record: { id: 'attempt-1', current_state: 'provisioned' } }
    capturedHandler(event)

    expect(callback).toHaveBeenCalledWith(event)
  })

  it('propagates subscribe errors', async () => {
    mockSubscribe.mockRejectedValue(new Error('subscribe failed'))
    await expect(subscribeToAttempt('attempt-1', vi.fn())).rejects.toThrow('subscribe failed')
  })
})
