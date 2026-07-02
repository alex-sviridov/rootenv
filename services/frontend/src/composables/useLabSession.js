import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { useBreadcrumbsStore } from '@/stores/breadcrumbs'
import { useAttemptsStore } from '@/stores/attempts'
import { fetchLab } from '@/api/labs'
import { useTerminalTabs } from '@/composables/useTerminalTabs'

export function useLabSession() {
  const route = useRoute()
  const breadcrumbs = useBreadcrumbsStore()
  const attemptsStore = useAttemptsStore()

  const lab = ref(null)
  const selectedTask = ref(0)
  const error = ref(null)

  const { tabs, activeTabId, limitError, openTab, selectTab, closeTab, moveTab, resetTabs } = useTerminalTabs()

  const attemptId = computed(() => attemptsStore.lastAttempt?.id ?? null)

  const currentTask = computed(() => lab.value?.content?.[selectedTask.value] ?? null)

  watch(() => attemptsStore.lastAttempt?.id, async (id) => {
    await attemptsStore.stopWatching()
    resetTabs()
    const attempt = attemptsStore.lastAttempt
    if (id && attempt?.current_state !== 'decommissioned') {
      await attemptsStore.startWatching(id)
    }
  })

  async function initLab(group, slug) {
    lab.value = null
    error.value = null
    selectedTask.value = 0
    resetTabs()
    try {
      lab.value = await fetchLab(`${group}_${slug}`)

      breadcrumbs.set([
        { label: 'LinuxLab', to: '/' },
        lab.value.group_title ? { label: lab.value.group_title, to: `/labs/${group}` } : null,
        { label: lab.value.title },
      ].filter(Boolean))

      await attemptsStore.stopWatching()
      await Promise.all([
        attemptsStore.loadLastAttempt(lab.value.id),
        attemptsStore.loadActiveAttempt(),
      ])
    } catch {
      error.value = 'Lab not found.'
    }
  }

  onMounted(() => {
    const { group, slug } = route.params
    if (group && slug) initLab(group, slug)
  })

  onUnmounted(async () => {
    await attemptsStore.stopWatching()
  })

  return {
    lab, selectedTask, currentTask, error,
    tabs, activeTabId, limitError, openTab, selectTab, closeTab, moveTab,
    attemptId,
  }
}
