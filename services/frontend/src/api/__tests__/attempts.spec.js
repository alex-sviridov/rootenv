import { describe, it, expect, vi, beforeEach } from 'vitest'

const {
  mockGetFirstListItem,
  mockGetList,
  mockCreate,
  mockUpdate,
  mockCollection,
} = vi.hoisted(() => {
  const mockGetFirstListItem = vi.fn()
  const mockGetList = vi.fn()
  const mockCreate = vi.fn()
  const mockUpdate = vi.fn()
  const mockCollection = vi.fn(() => ({
    getFirstListItem: mockGetFirstListItem,
    getList: mockGetList,
    create: mockCreate,
    update: mockUpdate,
  }))
  return { mockGetFirstListItem, mockGetList, mockCreate, mockUpdate, mockCollection }
})

vi.mock('@/lib/pb', () => ({ pb: { collection: mockCollection, authStore: { record: { id: 'user-1' }, token: 'tok' } } }))

import {
  fetchLastAttempt,
  fetchAttempts,
  createAttempt,
  fetchActiveAttempt,
  decommissionAttempt,
  fetchAssetSecret,
} from '../attempts'

beforeEach(() => vi.clearAllMocks())

describe('fetchLastAttempt', () => {
  it('queries attempts collection with correct lab filter', async () => {
    const attempt = { id: 'a1', current_state: 'provisioned', lab: 'lab-1' }
    mockGetFirstListItem.mockResolvedValue(attempt)

    const result = await fetchLastAttempt('lab-1')

    expect(mockCollection).toHaveBeenCalledWith('attempts')
    expect(mockGetFirstListItem).toHaveBeenCalledWith('lab = "lab-1"', { sort: '-updated', requestKey: 'last-attempt-lab-1' })
    expect(result).toEqual(attempt)
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

  it('propagates errors', async () => {
    mockGetList.mockRejectedValue(new Error('not found'))
    await expect(fetchAttempts('lab-1', 1, 10)).rejects.toThrow('not found')
  })
})

describe('createAttempt', () => {
  it('creates a record in attempts collection with lab id, lab_name, and user', async () => {
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
  it('patches the attempt with desired_state=decommissioned', async () => {
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

describe('fetchAssetSecret', () => {
  it('queries keys_userview by asset and returns the secret field', async () => {
    mockGetFirstListItem.mockResolvedValue({ id: 'k1', secret: 's3cr3t', asset: 'srv1' })

    const result = await fetchAssetSecret('srv1')

    expect(mockCollection).toHaveBeenCalledWith('keys_userview')
    expect(mockGetFirstListItem).toHaveBeenCalledWith('asset = "srv1"', { requestKey: 'asset-secret-srv1' })
    expect(result).toBe('s3cr3t')
  })

  it('propagates errors', async () => {
    mockGetFirstListItem.mockRejectedValue(new Error('not found'))
    await expect(fetchAssetSecret('srv1')).rejects.toThrow('not found')
  })
})

describe('fetchActiveAttempt', () => {
  it('queries attempts collection for any non-decommissioned attempt', async () => {
    const attempt = { id: 'a1', current_state: 'provisioned', lab: 'lab-1', lab_name: 'My Lab' }
    mockGetFirstListItem.mockResolvedValue(attempt)

    const result = await fetchActiveAttempt()

    expect(mockCollection).toHaveBeenCalledWith('attempts')
    expect(mockGetFirstListItem).toHaveBeenCalledWith('current_state != "decommissioned"', { requestKey: 'active-attempt' })
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
