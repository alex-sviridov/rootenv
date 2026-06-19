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

describe('openTab', () => {
  it('adds a tab and sets it active', () => {
    const { tabs, activeTabId, openTab } = useTerminalTabs()
    openTab(server('s1', 'web'), 'exec')
    expect(tabs.value).toHaveLength(1)
    expect(tabs.value[0].serverId).toBe('s1')
    expect(tabs.value[0].type).toBe('exec')
    expect(tabs.value[0].label).toBe('web')
    expect(activeTabId.value).toBe(tabs.value[0].id)
  })

  it('allows multiple tabs for the same server with unique ids', () => {
    const { tabs, openTab } = useTerminalTabs()
    openTab(server('s1', 'web'), 'exec')
    openTab(server('s1', 'web'), 'exec')
    expect(tabs.value).toHaveLength(2)
    expect(tabs.value[0].serverId).toBe('s1')
    expect(tabs.value[1].serverId).toBe('s1')
    expect(tabs.value[0].id).not.toBe(tabs.value[1].id)
  })

  it('sets active to the most recently opened tab', () => {
    const { activeTabId, tabs, openTab } = useTerminalTabs()
    openTab(server('s1'), 'exec')
    openTab(server('s1'), 'exec')
    expect(activeTabId.value).toBe(tabs.value[1].id)
  })

  it('labels single-server tab without a number', () => {
    const { tabs, openTab } = useTerminalTabs()
    openTab(server('s1', 'web'), 'exec')
    expect(tabs.value[0].label).toBe('web')
  })

  it('labels both tabs "(1)" and "(2)" when a second tab opens for the same server and type', () => {
    const { tabs, openTab } = useTerminalTabs()
    openTab(server('s1', 'web'), 'exec')
    openTab(server('s1', 'web'), 'exec')
    expect(tabs.value[0].label).toBe('web (1)')
    expect(tabs.value[1].label).toBe('web (2)')
  })

  it('does not affect labels of other servers when adding a duplicate', () => {
    const { tabs, openTab } = useTerminalTabs()
    openTab(server('s1', 'web'), 'exec')
    openTab(server('s2', 'db'), 'exec')
    openTab(server('s1', 'web'), 'exec')
    expect(tabs.value[1].label).toBe('db')
  })

  it('adds multiple distinct server tabs', () => {
    const { tabs, openTab } = useTerminalTabs()
    openTab(server('s1', 'web'), 'exec')
    openTab(server('s2', 'db'), 'exec')
    expect(tabs.value).toHaveLength(2)
    expect(tabs.value[0].serverId).toBe('s1')
    expect(tabs.value[1].serverId).toBe('s2')
  })
})

describe('openTab — type-scoped relabeling', () => {
  it('two different protocol tabs for same server are independently unnumbered', () => {
    const { tabs, openTab } = useTerminalTabs()
    openTab(server('s1', 'web'), 'exec')
    openTab(server('s1', 'web'), 'http')
    expect(tabs.value[0].label).toBe('web')
    expect(tabs.value[1].label).toBe('web')
  })

  it('second ssh tab for same server gets numbered; rdp tab for same server stays unnumbered', () => {
    const { tabs, openTab } = useTerminalTabs()
    openTab(server('s1', 'web'), 'exec')
    openTab(server('s1', 'web'), 'exec')
    openTab(server('s1', 'web'), 'http')
    expect(tabs.value[0].label).toBe('web (1)')
    expect(tabs.value[1].label).toBe('web (2)')
    expect(tabs.value[2].label).toBe('web')
  })

  it('closing one ssh tab removes numbering only within ssh type', () => {
    const { tabs, openTab, closeTab } = useTerminalTabs()
    openTab(server('s1', 'web'), 'exec')
    openTab(server('s1', 'web'), 'exec')
    openTab(server('s1', 'web'), 'http')
    closeTab(tabs.value[0].id)
    // remaining ssh tab should lose its number
    expect(tabs.value.find(t => t.type === 'exec').label).toBe('web')
    // rdp tab unaffected
    expect(tabs.value.find(t => t.type === 'http').label).toBe('web')
  })
})

describe('limit enforcement', () => {
  it('does not add a tab beyond 16 and sets limitError', () => {
    const { tabs, limitError, openTab } = useTerminalTabs()
    for (let i = 0; i < 16; i++) openTab(server('s1'), 'exec')
    expect(tabs.value).toHaveLength(16)
    openTab(server('s1'), 'exec')
    expect(tabs.value).toHaveLength(16)
    expect(limitError.value).toContain('16')
    expect(limitError.value).toContain('Close a tab')
  })

  it('clears limitError when a tab is closed', () => {
    const { tabs, limitError, openTab, closeTab } = useTerminalTabs()
    for (let i = 0; i < 16; i++) openTab(server('s1'), 'exec')
    openTab(server('s1'), 'exec') // triggers error
    expect(limitError.value).not.toBeNull()
    closeTab(tabs.value[0].id)
    expect(limitError.value).toBeNull()
  })

  it('allows opening again after closing when at limit', () => {
    const { tabs, openTab, closeTab } = useTerminalTabs()
    for (let i = 0; i < 16; i++) openTab(server('s1'), 'exec')
    closeTab(tabs.value[0].id)
    openTab(server('s2'), 'exec')
    expect(tabs.value).toHaveLength(16)
    expect(tabs.value.at(-1).serverId).toBe('s2')
  })
})

describe('closeTab', () => {
  it('removes the tab', () => {
    const { tabs, openTab, closeTab } = useTerminalTabs()
    openTab(server('s1'), 'exec')
    const id = tabs.value[0].id
    closeTab(id)
    expect(tabs.value).toEqual([])
  })

  it('removes "(N)" label when closing one of two same-server same-type tabs', () => {
    const { tabs, openTab, closeTab } = useTerminalTabs()
    openTab(server('s1', 'web'), 'exec')
    openTab(server('s1', 'web'), 'exec')
    closeTab(tabs.value[0].id)
    expect(tabs.value[0].label).toBe('web')
  })

  it('switches active to last remaining tab when closing the active tab', () => {
    const { activeTabId, tabs, openTab, closeTab } = useTerminalTabs()
    openTab(server('s1'), 'exec')
    openTab(server('s2'), 'exec')
    const lastId = tabs.value[0].id
    const activeId = tabs.value[1].id
    closeTab(activeId)
    expect(activeTabId.value).toBe(lastId)
  })

  it('sets activeTabId to null when closing the only tab', () => {
    const { activeTabId, tabs, openTab, closeTab } = useTerminalTabs()
    openTab(server('s1'), 'exec')
    closeTab(tabs.value[0].id)
    expect(activeTabId.value).toBeNull()
  })

  it('does not change activeTabId when closing an inactive tab', () => {
    const { activeTabId, tabs, openTab, closeTab } = useTerminalTabs()
    openTab(server('s1'), 'exec')
    openTab(server('s2'), 'exec')
    const activeId = activeTabId.value
    const inactiveId = tabs.value[0].id
    closeTab(inactiveId)
    expect(activeTabId.value).toBe(activeId)
  })
})

describe('moveTab', () => {
  it('moves a tab to a new position', () => {
    const { tabs, openTab, moveTab } = useTerminalTabs()
    openTab(server('s1', 'a'), 'exec')
    openTab(server('s2', 'b'), 'exec')
    openTab(server('s3', 'c'), 'exec')
    const [id0, id1, id2] = tabs.value.map(t => t.id)
    moveTab(id2, id0)
    expect(tabs.value.map(t => t.id)).toEqual([id2, id0, id1])
  })

  it('is a no-op when from === to', () => {
    const { tabs, openTab, moveTab } = useTerminalTabs()
    openTab(server('s1'), 'exec')
    openTab(server('s2'), 'exec')
    const before = tabs.value.map(t => t.id)
    moveTab(before[0], before[0])
    expect(tabs.value.map(t => t.id)).toEqual(before)
  })
})

describe('resetTabs', () => {
  it('clears all tabs, active tab, and error', () => {
    const { tabs, activeTabId, limitError, openTab, resetTabs } = useTerminalTabs()
    for (let i = 0; i < 16; i++) openTab(server('s1'), 'exec')
    openTab(server('s1'), 'exec') // set error
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
