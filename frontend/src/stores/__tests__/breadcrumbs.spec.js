import { describe, it, expect, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useBreadcrumbsStore } from '../breadcrumbs'

beforeEach(() => setActivePinia(createPinia()))

describe('initial state', () => {
  it('starts with empty crumbs', () => {
    const store = useBreadcrumbsStore()
    expect(store.crumbs).toEqual([])
  })
})

describe('set', () => {
  it('replaces crumbs with the provided array', () => {
    const store = useBreadcrumbsStore()
    store.set([{ label: 'Home', to: '/' }, { label: 'Account' }])
    expect(store.crumbs).toEqual([{ label: 'Home', to: '/' }, { label: 'Account' }])
  })

  it('overwrites previously set crumbs', () => {
    const store = useBreadcrumbsStore()
    store.set([{ label: 'First' }])
    store.set([{ label: 'Second' }, { label: 'Third' }])
    expect(store.crumbs).toHaveLength(2)
    expect(store.crumbs[0].label).toBe('Second')
  })

  it('accepts crumbs with action callbacks', () => {
    const store = useBreadcrumbsStore()
    const action = () => {}
    store.set([{ label: 'Home', action }, { label: 'Labs' }])
    expect(store.crumbs[0].action).toBe(action)
  })

  it('sets a single root crumb', () => {
    const store = useBreadcrumbsStore()
    store.set([{ label: 'LinuxLab' }])
    expect(store.crumbs).toHaveLength(1)
    expect(store.crumbs[0].label).toBe('LinuxLab')
  })
})
