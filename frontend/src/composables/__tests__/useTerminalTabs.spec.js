import { describe, it, expect } from 'vitest'
import { useTerminalTabs } from '../useTerminalTabs'

const server = (id, name = `server-${id}`) => ({ id, name })

describe('initial state', () => {
  it('starts with no tabs, no active tab, and no error', () => {
    const { tabs, activeTabId, limitError } = useTerminalTabs()
    expect(tabs.value).toEqual([])
    expect(activeTabId.value).toBeNull()
    expect(limitError.value).toBeNull()
  })
})

describe('openTerminal', () => {
  it('adds a tab and sets it active', () => {
    const { tabs, activeTabId, openTerminal } = useTerminalTabs()
    openTerminal(server('s1', 'web'))
    expect(tabs.value).toHaveLength(1)
    expect(tabs.value[0].serverId).toBe('s1')
    expect(tabs.value[0].label).toBe('web')
    expect(activeTabId.value).toBe(tabs.value[0].id)
  })

  it('allows multiple tabs for the same server with unique ids', () => {
    const { tabs, openTerminal } = useTerminalTabs()
    openTerminal(server('s1', 'web'))
    openTerminal(server('s1', 'web'))
    expect(tabs.value).toHaveLength(2)
    expect(tabs.value[0].serverId).toBe('s1')
    expect(tabs.value[1].serverId).toBe('s1')
    expect(tabs.value[0].id).not.toBe(tabs.value[1].id)
  })

  it('sets active to the most recently opened tab', () => {
    const { activeTabId, tabs, openTerminal } = useTerminalTabs()
    openTerminal(server('s1'))
    openTerminal(server('s1'))
    expect(activeTabId.value).toBe(tabs.value[1].id)
  })

  it('labels single-server tab without a number', () => {
    const { tabs, openTerminal } = useTerminalTabs()
    openTerminal(server('s1', 'web'))
    expect(tabs.value[0].label).toBe('web')
  })

  it('labels both tabs "(1)" and "(2)" when a second tab opens for the same server', () => {
    const { tabs, openTerminal } = useTerminalTabs()
    openTerminal(server('s1', 'web'))
    openTerminal(server('s1', 'web'))
    expect(tabs.value[0].label).toBe('web (1)')
    expect(tabs.value[1].label).toBe('web (2)')
  })

  it('does not affect labels of other servers when adding a duplicate', () => {
    const { tabs, openTerminal } = useTerminalTabs()
    openTerminal(server('s1', 'web'))
    openTerminal(server('s2', 'db'))
    openTerminal(server('s1', 'web'))
    expect(tabs.value[1].label).toBe('db')
  })

  it('adds multiple distinct server tabs', () => {
    const { tabs, openTerminal } = useTerminalTabs()
    openTerminal(server('s1', 'web'))
    openTerminal(server('s2', 'db'))
    expect(tabs.value).toHaveLength(2)
    expect(tabs.value[0].serverId).toBe('s1')
    expect(tabs.value[1].serverId).toBe('s2')
  })
})

describe('limit enforcement', () => {
  it('does not add a tab beyond 16 and sets limitError', () => {
    const { tabs, limitError, openTerminal } = useTerminalTabs()
    for (let i = 0; i < 16; i++) openTerminal(server('s1'))
    expect(tabs.value).toHaveLength(16)
    openTerminal(server('s1'))
    expect(tabs.value).toHaveLength(16)
    expect(limitError.value).toContain('16')
    expect(limitError.value).toContain('Close a tab')
  })

  it('clears limitError when a tab is closed', () => {
    const { tabs, limitError, openTerminal, closeTab } = useTerminalTabs()
    for (let i = 0; i < 16; i++) openTerminal(server('s1'))
    openTerminal(server('s1')) // triggers error
    expect(limitError.value).not.toBeNull()
    closeTab(tabs.value[0].id)
    expect(limitError.value).toBeNull()
  })

  it('allows opening again after closing when at limit', () => {
    const { tabs, openTerminal, closeTab } = useTerminalTabs()
    for (let i = 0; i < 16; i++) openTerminal(server('s1'))
    closeTab(tabs.value[0].id)
    openTerminal(server('s2'))
    expect(tabs.value).toHaveLength(16)
    expect(tabs.value.at(-1).serverId).toBe('s2')
  })
})

describe('closeTab', () => {
  it('removes the tab', () => {
    const { tabs, openTerminal, closeTab } = useTerminalTabs()
    openTerminal(server('s1'))
    const id = tabs.value[0].id
    closeTab(id)
    expect(tabs.value).toEqual([])
  })

  it('removes "(N)" label when closing one of two same-server tabs', () => {
    const { tabs, openTerminal, closeTab } = useTerminalTabs()
    openTerminal(server('s1', 'web'))
    openTerminal(server('s1', 'web'))
    closeTab(tabs.value[0].id)
    expect(tabs.value[0].label).toBe('web')
  })

  it('switches active to last remaining tab when closing the active tab', () => {
    const { activeTabId, tabs, openTerminal, closeTab } = useTerminalTabs()
    openTerminal(server('s1'))
    openTerminal(server('s2'))
    const lastId = tabs.value[0].id
    const activeId = tabs.value[1].id
    closeTab(activeId)
    expect(activeTabId.value).toBe(lastId)
  })

  it('sets activeTabId to null when closing the only tab', () => {
    const { activeTabId, tabs, openTerminal, closeTab } = useTerminalTabs()
    openTerminal(server('s1'))
    closeTab(tabs.value[0].id)
    expect(activeTabId.value).toBeNull()
  })

  it('does not change activeTabId when closing an inactive tab', () => {
    const { activeTabId, tabs, openTerminal, closeTab } = useTerminalTabs()
    openTerminal(server('s1'))
    openTerminal(server('s2'))
    const activeId = activeTabId.value
    const inactiveId = tabs.value[0].id
    closeTab(inactiveId)
    expect(activeTabId.value).toBe(activeId)
  })
})

describe('moveTab', () => {
  it('moves a tab to a new position', () => {
    const { tabs, openTerminal, moveTab } = useTerminalTabs()
    openTerminal(server('s1', 'a'))
    openTerminal(server('s2', 'b'))
    openTerminal(server('s3', 'c'))
    const [id0, id1, id2] = tabs.value.map(t => t.id)
    moveTab(id2, id0)
    expect(tabs.value.map(t => t.id)).toEqual([id2, id0, id1])
  })

  it('is a no-op when from === to', () => {
    const { tabs, openTerminal, moveTab } = useTerminalTabs()
    openTerminal(server('s1'))
    openTerminal(server('s2'))
    const before = tabs.value.map(t => t.id)
    moveTab(before[0], before[0])
    expect(tabs.value.map(t => t.id)).toEqual(before)
  })
})

describe('resetTabs', () => {
  it('clears all tabs, active tab, and error', () => {
    const { tabs, activeTabId, limitError, openTerminal, resetTabs } = useTerminalTabs()
    for (let i = 0; i < 16; i++) openTerminal(server('s1'))
    openTerminal(server('s1')) // set error
    resetTabs()
    expect(tabs.value).toEqual([])
    expect(activeTabId.value).toBeNull()
    expect(limitError.value).toBeNull()
  })

  it('is safe to call when already empty', () => {
    const { resetTabs, tabs, activeTabId, limitError } = useTerminalTabs()
    resetTabs()
    expect(tabs.value).toEqual([])
    expect(activeTabId.value).toBeNull()
    expect(limitError.value).toBeNull()
  })
})
