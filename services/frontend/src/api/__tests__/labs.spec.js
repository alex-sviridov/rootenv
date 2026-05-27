import { describe, it, expect, vi, beforeEach } from 'vitest'

const { mockGetFullList, mockGetOne, mockCollection } = vi.hoisted(() => {
  const mockGetFullList = vi.fn()
  const mockGetOne = vi.fn()
  const mockCollection = vi.fn(() => ({ getFullList: mockGetFullList, getOne: mockGetOne }))
  return { mockGetFullList, mockGetOne, mockCollection }
})

vi.mock('@/lib/pb', () => ({
  pb: { collection: mockCollection },
}))

import { fetchFolders, fetchLabsInFolder, fetchLab } from '../labs'

beforeEach(() => vi.clearAllMocks())

describe('fetchFolders', () => {
  it('queries labs_userview with correct params', async () => {
    const folders = [{ id: 'g1', title: 'RHCSA', description: '' }]
    mockGetFullList.mockResolvedValue(folders)

    const result = await fetchFolders()

    expect(mockCollection).toHaveBeenCalledWith('labs_userview')
    expect(mockGetFullList).toHaveBeenCalledWith({
      filter: 'type = "folder" && parent = ""',
      sort: 'title',
      fields: 'id,title,description',
    })
    expect(result).toEqual(folders)
  })

  it('propagates errors', async () => {
    mockGetFullList.mockRejectedValue(new Error('network error'))
    await expect(fetchFolders()).rejects.toThrow('network error')
  })
})

describe('fetchLabsInFolder', () => {
  it('queries labs_userview filtering by parent folder', async () => {
    const labs = [{ id: 'l1', title: 'Lab 1', description: '' }]
    mockGetFullList.mockResolvedValue(labs)

    const result = await fetchLabsInFolder('folder-id-1')

    expect(mockCollection).toHaveBeenCalledWith('labs_userview')
    expect(mockGetFullList).toHaveBeenCalledWith({
      filter: 'type = "lab" && parent = "folder-id-1"',
      sort: 'title',
      fields: 'id,title,description',
    })
    expect(result).toEqual(labs)
  })

  it('propagates errors', async () => {
    mockGetFullList.mockRejectedValue(new Error('not found'))
    await expect(fetchLabsInFolder('x')).rejects.toThrow('not found')
  })
})

describe('fetchLab', () => {
  it('fetches a single lab by id from labs_userview', async () => {
    const lab = { id: 'rhcsa-lab1', title: 'Lab 1', content: [] }
    mockGetOne.mockResolvedValue(lab)

    const result = await fetchLab('rhcsa-lab1')

    expect(mockCollection).toHaveBeenCalledWith('labs_userview')
    expect(mockGetOne).toHaveBeenCalledWith('rhcsa-lab1')
    expect(result).toEqual(lab)
  })

  it('propagates errors', async () => {
    mockGetOne.mockRejectedValue(new Error('not found'))
    await expect(fetchLab('missing')).rejects.toThrow('not found')
  })
})
