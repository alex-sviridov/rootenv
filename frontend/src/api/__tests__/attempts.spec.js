import { describe, it, expect, vi, beforeEach } from 'vitest'

const {
  mockGetFirstListItem,
  mockGetList,
  mockCreate,
  mockSubscribe,
  mockCollection,
} = vi.hoisted(() => {
  const mockGetFirstListItem = vi.fn()
  const mockGetList = vi.fn()
  const mockCreate = vi.fn()
  const mockSubscribe = vi.fn()
  const mockCollection = vi.fn(() => ({
    getFirstListItem: mockGetFirstListItem,
    getList: mockGetList,
    create: mockCreate,
    subscribe: mockSubscribe,
  }))
  return { mockGetFirstListItem, mockGetList, mockCreate, mockSubscribe, mockCollection }
})

vi.mock('@/lib/pb', () => ({ pb: { collection: mockCollection, authStore: { record: { id: 'user-1' }, token: 'tok' } } }))

import {
  fetchLastAttempt,
  fetchAttempts,
  createAttempt,
  subscribeToAttempt,
  fetchActiveAttempt,
  decommissionAttempt,
  fetchAssetSecret,
} from '../attempts'

beforeEach(() => vi.clearAllMocks())

describe('fetchLastAttempt', () => {
  it('queries attempts_userview with correct lab filter', async () => {
    const attempt = { id: 'a1', status: 'running', lab: 'lab-1' }
    mockGetFirstListItem.mockResolvedValue(attempt)

    const result = await fetchLastAttempt('lab-1')

    expect(mockCollection).toHaveBeenCalledWith('attempts_userview')
    expect(mockGetFirstListItem).toHaveBeenCalledWith('lab = "lab-1"', { sort: '-updated' })
    expect(result).toEqual(attempt)
  })

  it('propagates errors', async () => {
    mockGetFirstListItem.mockRejectedValue(new Error('network error'))
    await expect(fetchLastAttempt('lab-1')).rejects.toThrow('network error')
  })
})

describe('fetchAttempts', () => {
  it('queries attempts_userview with page, perPage, filter and sort', async () => {
    const page = { items: [], page: 2, totalPages: 3 }
    mockGetList.mockResolvedValue(page)

    const result = await fetchAttempts('lab-1', 2, 5)

    expect(mockCollection).toHaveBeenCalledWith('attempts_userview')
    expect(mockGetList).toHaveBeenCalledWith(2, 5, {
      filter: 'lab = "lab-1"',
      sort: '-updated',
    })
    expect(result).toEqual(page)
  })

  it('propagates errors', async () => {
    mockGetList.mockRejectedValue(new Error('not found'))
    await expect(fetchAttempts('lab-1', 1, 10)).rejects.toThrow('not found')
  })
})

describe('createAttempt', () => {
  it('creates a record in attempts collection with lab id, lab_name, and user', async () => {
    const created = { id: 'a1', lab: 'lab-1', lab_name: 'My Lab', status: 'provisioning' }
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

describe('subscribeToAttempt', () => {
  it('subscribes to attempts collection and returns unsub fn', async () => {
    const unsubFn = vi.fn()
    mockSubscribe.mockResolvedValue(unsubFn)
    mockGetFirstListItem.mockResolvedValue({ id: 'a1', state: 'provisioned' })

    const result = await subscribeToAttempt('lab-1', vi.fn())

    expect(mockCollection).toHaveBeenCalledWith('attempts')
    expect(mockSubscribe).toHaveBeenCalledTimes(1)
    expect(result).toBe(unsubFn)
  })

  it('re-fetches view and calls callback when attempt event lab matches', async () => {
    let handler
    mockSubscribe.mockImplementation(async (_topic, fn) => { handler = fn; return vi.fn() })
    const fresh = { id: 'a1', state: 'provisioned' }
    mockGetFirstListItem.mockResolvedValue(fresh)
    const callback = vi.fn()

    await subscribeToAttempt('lab-1', callback)
    await handler({ record: { lab: 'lab-1' } })

    expect(callback).toHaveBeenCalledWith(fresh)
  })

  it('ignores attempt events for other labs', async () => {
    let handler
    mockSubscribe.mockImplementation(async (_topic, fn) => { handler = fn; return vi.fn() })
    mockGetFirstListItem.mockResolvedValue({ id: 'a1', state: 'provisioned' })
    const callback = vi.fn()

    await subscribeToAttempt('lab-1', callback)
    callback.mockClear()
    mockGetFirstListItem.mockClear()

    await handler({ record: { lab: 'lab-other' } })

    expect(mockGetFirstListItem).not.toHaveBeenCalled()
    expect(callback).not.toHaveBeenCalled()
  })

  it('propagates errors', async () => {
    mockSubscribe.mockRejectedValue(new Error('subscribe failed'))
    await expect(subscribeToAttempt('lab-1', vi.fn())).rejects.toThrow('subscribe failed')
  })
})

describe('decommissionAttempt', () => {
  it('creates a decommission command for each server id', async () => {
    mockCreate.mockResolvedValue({})

    await decommissionAttempt(['s1', 's2'])

    expect(mockCollection).toHaveBeenCalledWith('commands')
    expect(mockCreate).toHaveBeenCalledTimes(2)
    expect(mockCreate).toHaveBeenCalledWith({ asset: 's1', command: 'decommission', status: 'pending' }, { requestKey: 's1' })
    expect(mockCreate).toHaveBeenCalledWith({ asset: 's2', command: 'decommission', status: 'pending' }, { requestKey: 's2' })
  })

  it('propagates errors', async () => {
    mockCreate.mockRejectedValue(new Error('forbidden'))
    await expect(decommissionAttempt(['s1'])).rejects.toThrow('forbidden')
  })
})

describe('fetchAssetSecret', () => {
  it('queries keys_userview by asset and returns the secret field', async () => {
    mockGetFirstListItem.mockResolvedValue({ id: 'k1', secret: 's3cr3t', asset: 'srv1' })

    const result = await fetchAssetSecret('srv1')

    expect(mockCollection).toHaveBeenCalledWith('keys_userview')
    expect(mockGetFirstListItem).toHaveBeenCalledWith('asset = "srv1"')
    expect(result).toBe('s3cr3t')
  })

  it('propagates errors', async () => {
    mockGetFirstListItem.mockRejectedValue(new Error('not found'))
    await expect(fetchAssetSecret('srv1')).rejects.toThrow('not found')
  })
})

describe('fetchActiveAttempt', () => {
  it('queries attempts_userview for any non-decommissioned attempt', async () => {
    const attempt = { id: 'a1', state: 'provisioned', lab: 'lab-1', lab_name: 'My Lab' }
    mockGetFirstListItem.mockResolvedValue(attempt)

    const result = await fetchActiveAttempt()

    expect(mockCollection).toHaveBeenCalledWith('attempts_userview')
    expect(mockGetFirstListItem).toHaveBeenCalledWith('state != "decommissioned"')
    expect(result).toEqual(attempt)
  })

  it('returns null on 404', async () => {
    const notFound = Object.assign(new Error('Not found'), { status: 404 })
    mockGetFirstListItem.mockRejectedValue(notFound)

    const result = await fetchActiveAttempt()

    expect(result).toBeNull()
  })

  it('propagates non-404 errors', async () => {
    mockGetFirstListItem.mockRejectedValue(new Error('network error'))
    await expect(fetchActiveAttempt()).rejects.toThrow('network error')
  })
})
