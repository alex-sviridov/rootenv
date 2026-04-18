import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

vi.mock('@/api/labs', () => ({
  fetchFolders: vi.fn(),
  fetchLabsInFolder: vi.fn(),
}))

import { fetchFolders, fetchLabsInFolder } from '@/api/labs'
import { useLabsStore } from '../labs'

beforeEach(() => {
  setActivePinia(createPinia())
  vi.clearAllMocks()
})

describe('initial state', () => {
  it('starts empty with no loading or error', () => {
    const store = useLabsStore()
    expect(store.groups).toEqual([])
    expect(store.labsByGroup).toEqual({})
    expect(store.selectedGroupSlug).toBeNull()
    expect(store.loading).toBe(false)
    expect(store.error).toBeNull()
  })
})

describe('selectedGroup', () => {
  it('returns null when nothing is selected', () => {
    expect(useLabsStore().selectedGroup).toBeNull()
  })

  it('returns the matching group when selected', () => {
    const store = useLabsStore()
    store.groups = [{ id: 'g1', title: 'RHCSA' }]
    store.selectedGroupSlug = 'g1'
    expect(store.selectedGroup).toEqual({ id: 'g1', title: 'RHCSA' })
  })

  it('returns null when selected id has no match', () => {
    const store = useLabsStore()
    store.groups = [{ id: 'g1', title: 'RHCSA' }]
    store.selectedGroupSlug = 'missing'
    expect(store.selectedGroup).toBeNull()
  })
})

describe('currentLabs', () => {
  it('returns empty array when no group is selected', () => {
    expect(useLabsStore().currentLabs).toEqual([])
  })

  it('returns labs for the selected group', () => {
    const store = useLabsStore()
    const labs = [{ id: 'l1', title: 'Lab 1' }]
    store.labsByGroup = { g1: labs }
    store.selectedGroupSlug = 'g1'
    expect(store.currentLabs).toEqual(labs)
  })

  it('returns empty array when selected group has no fetched labs', () => {
    const store = useLabsStore()
    store.selectedGroupSlug = 'g1'
    expect(store.currentLabs).toEqual([])
  })
})

describe('loadGroups', () => {
  it('fetches folders and stores them', async () => {
    const groups = [{ id: 'g1', title: 'RHCSA' }]
    fetchFolders.mockResolvedValue(groups)

    const store = useLabsStore()
    await store.loadGroups()

    expect(store.groups).toEqual(groups)
    expect(store.loading).toBe(false)
    expect(store.error).toBeNull()
  })

  it('sets loading true while in-flight', async () => {
    let resolve
    fetchFolders.mockReturnValue(new Promise(r => { resolve = r }))

    const store = useLabsStore()
    const promise = store.loadGroups()
    expect(store.loading).toBe(true)
    resolve([])
    await promise
    expect(store.loading).toBe(false)
  })

  it('captures error message on failure', async () => {
    fetchFolders.mockRejectedValue(new Error('fetch failed'))

    const store = useLabsStore()
    await store.loadGroups()

    expect(store.groups).toEqual([])
    expect(store.error).toBe('fetch failed')
    expect(store.loading).toBe(false)
  })
})

describe('selectGroup', () => {
  it('sets selectedGroupSlug', async () => {
    fetchLabsInFolder.mockResolvedValue([])
    const store = useLabsStore()
    await store.selectGroup('g1')
    expect(store.selectedGroupSlug).toBe('g1')
  })

  it('fetches and caches labs for the group', async () => {
    const labs = [{ id: 'l1', title: 'Lab 1' }]
    fetchLabsInFolder.mockResolvedValue(labs)

    const store = useLabsStore()
    await store.selectGroup('g1')

    expect(fetchLabsInFolder).toHaveBeenCalledWith('g1')
    expect(store.labsByGroup['g1']).toEqual(labs)
  })

  it('does not re-fetch when labs are already cached', async () => {
    const store = useLabsStore()
    store.labsByGroup = { g1: [{ id: 'l1' }] }
    await store.selectGroup('g1')

    expect(fetchLabsInFolder).not.toHaveBeenCalled()
  })

  it('captures error message on failure', async () => {
    fetchLabsInFolder.mockRejectedValue(new Error('group load failed'))

    const store = useLabsStore()
    await store.selectGroup('g1')

    expect(store.error).toBe('group load failed')
    expect(store.loading).toBe(false)
  })
})

describe('clearGroup', () => {
  it('resets selectedGroupSlug to null', () => {
    const store = useLabsStore()
    store.selectedGroupSlug = 'g1'
    store.clearGroup()
    expect(store.selectedGroupSlug).toBeNull()
  })
})
