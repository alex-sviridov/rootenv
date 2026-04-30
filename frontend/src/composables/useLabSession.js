import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useBreadcrumbsStore } from '@/stores/breadcrumbs'
import { useLabsStore } from '@/stores/labs'
import { useAttemptsStore } from '@/stores/attempts'
import { useServersStore } from '@/stores/servers'
import { fetchLab } from '@/api/labs'
import { fetchAssetSecret } from '@/api/attempts'
import { useTerminalTabs } from '@/composables/useTerminalTabs'

export function useLabSession() {
  const route = useRoute()
  const router = useRouter()
  const breadcrumbs = useBreadcrumbsStore()
  const labsStore = useLabsStore()
  const attemptsStore = useAttemptsStore()
  const serversStore = useServersStore()

  const lab = ref(null)
  const selectedTask = ref(0)
  const error = ref(null)
  const secrets = ref({})

  const { tabs, activeTabId, limitError, openTab, closeTab, moveTab, resetTabs } = useTerminalTabs()

  const currentTask = computed(() => lab.value?.content?.[selectedTask.value] ?? null)

  watch(() => attemptsStore.lastAttempt?.id, async (id) => {
    await attemptsStore.stopWatching()
    await serversStore.stopWatching()
    secrets.value = {}
    resetTabs()
    const attempt = attemptsStore.lastAttempt
    if (id && attempt?.current_state !== 'decommissioned') {
      await attemptsStore.startWatching(id)
      await serversStore.startWatching(id)
      await serversStore.loadServers(id)
    }
  })

  watch(() => serversStore.servers, (servers) => {
    for (const server of servers) {
      if (server.state === 'provisioned' && !secrets.value[server.id]) {
        fetchAssetSecret(server.id)
          .then(s => { secrets.value = { ...secrets.value, [server.id]: s } })
          .catch(() => {})
      }
    }
  }, { deep: true })

  async function initLab(slug) {
    lab.value = null
    error.value = null
    selectedTask.value = 0
    resetTabs()
    try {
      lab.value = await fetchLab(slug)

      const group = labsStore.groups.find(g => g.id === lab.value.parent)
      breadcrumbs.set([
        { label: 'LinuxLab', action: () => { labsStore.clearGroup(); router.push('/') } },
        group
          ? { label: group.title, action: () => { labsStore.selectGroup(group.id); router.push('/') } }
          : null,
        { label: lab.value.title },
      ].filter(Boolean))

      await attemptsStore.stopWatching()
      await serversStore.stopWatching()
      await Promise.all([
        attemptsStore.loadLastAttempt(lab.value.id),
        attemptsStore.loadActiveAttempt(),
      ])
    } catch {
      error.value = 'Lab not found.'
    }
  }

  watch(() => route.params.slug, (slug) => { if (slug) initLab(slug) })

  onMounted(() => initLab(route.params.slug))

  onUnmounted(async () => {
    await serversStore.stopWatching()
    await attemptsStore.stopWatching()
  })

  return {
    lab, selectedTask, currentTask, error, secrets,
    tabs, activeTabId, limitError, openTab, closeTab, moveTab,
  }
}
