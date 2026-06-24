import { ref } from 'vue'

const MAX_TABS = 16

export function useTerminalTabs() {
  const tabs = ref([])
  const activeTabId = ref(null)
  const limitError = ref(null)
  let _seq = 0

  function _relabelGroup(serverId, type, serverName) {
    const matching = tabs.value.filter(t => t.serverId === serverId && t.type === type)
    if (matching.length === 1) {
      matching[0].label = serverName
    } else {
      matching.forEach((t, i) => { t.label = `${serverName} (${i + 1})` })
    }
  }

  function openTab(server, protocol) {
    if (tabs.value.length >= MAX_TABS) {
      limitError.value = `Maximum of ${MAX_TABS} terminal connections reached. Close a tab to open a new one.`
      return
    }
    limitError.value = null
    const serverId = server.name
    const tabId = `${serverId}-${protocol}-${++_seq}`
    tabs.value.push({ id: tabId, serverId, type: protocol, label: server.name })
    _relabelGroup(serverId, protocol, server.name)
    activeTabId.value = tabId
  }

  function closeTab(tabId) {
    const closing = tabs.value.find(t => t.id === tabId)
    tabs.value = tabs.value.filter(t => t.id !== tabId)
    limitError.value = null
    if (closing) _relabelGroup(closing.serverId, closing.type, closing.label.replace(/ \(\d+\)$/, ''))
    if (activeTabId.value === tabId) {
      activeTabId.value = tabs.value.at(-1)?.id ?? null
    }
  }

  function moveTab(fromId, toId) {
    if (fromId === toId) return
    const arr = tabs.value
    const from = arr.findIndex(t => t.id === fromId)
    const to = arr.findIndex(t => t.id === toId)
    if (from === -1 || to === -1) return
    const [item] = arr.splice(from, 1)
    arr.splice(to, 0, item)
  }

  function resetTabs() {
    tabs.value = []
    activeTabId.value = null
    limitError.value = null
  }

  function selectTab(tabId) {
    activeTabId.value = tabId
  }

  return { tabs, activeTabId, limitError, openTab, selectTab, closeTab, moveTab, resetTabs }
}
