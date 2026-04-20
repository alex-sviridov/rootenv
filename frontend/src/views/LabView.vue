<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useBreadcrumbsStore } from '@/stores/breadcrumbs'
import { useLabsStore } from '@/stores/labs'
import { useAttemptsStore } from '@/stores/attempts'
import { useServersStore } from '@/stores/servers'
import { fetchLab } from '@/api/labs'
import { useTerminalTabs } from '@/composables/useTerminalTabs'
import LabNavigation from '@/components/lab/LabNavigation.vue'
import LabControls from '@/components/lab/LabControls.vue'
import LabContent from '@/components/lab/LabContent.vue'
import LabConsole from '@/components/lab/LabConsole.vue'

const route = useRoute()
const router = useRouter()
const breadcrumbs = useBreadcrumbsStore()
const labsStore = useLabsStore()
const attemptsStore = useAttemptsStore()
const serversStore = useServersStore()

const lab = ref(null)
const selectedTask = ref(0)
const error = ref(null)

const { tabs, activeTabId, limitError, openTerminal, closeTab, moveTab, resetTabs } = useTerminalTabs()

const currentTask = computed(() => lab.value?.content?.[selectedTask.value] ?? null)

watch(() => attemptsStore.lastAttempt, (attempt, prev) => {
  if (attempt?.state === 'decommissioned' || !attempt) {
    serversStore.stopWatching()
    resetTabs()
  } else if (attempt?.id !== prev?.id) {
    serversStore.loadServers(attempt.id)
    serversStore.startWatching(attempt.id)
    resetTabs()
  }
})

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
    await attemptsStore.startWatching(lab.value.id)
    const attempt = attemptsStore.lastAttempt
    if (attempt && attempt.state !== 'decommissioned') {
      serversStore.loadServers(attempt.id)
      serversStore.startWatching(attempt.id)
    }
  } catch {
    error.value = 'Lab not found.'
  }
}

watch(() => route.params.slug, (slug) => { if (slug) initLab(slug) })

onMounted(() => initLab(route.params.slug))

onUnmounted(() => {
  attemptsStore.stopWatching()
  serversStore.stopWatching()
})
</script>

<template>
  <div v-if="error" class="p-8 text-sm text-red-400">{{ error }}</div>
  <div v-else-if="!lab" class="p-8 text-sm text-slate-500">Loading…</div>
  <div v-else class="flex h-full overflow-hidden">
    <aside class="w-64 shrink-0 border-r border-slate-800 flex flex-col overflow-hidden">
      <LabNavigation :lab="lab" :selected-task="selectedTask" @select-task="selectedTask = $event" />
      <LabControls :lab-id="lab.id" :lab-name="lab.title" @open-terminal="openTerminal" />
    </aside>
    <LabContent :task="currentTask" />
    <LabConsole
      :tabs="tabs"
      :active-tab-id="activeTabId"
      :limit-error="limitError"
      @select-tab="activeTabId = $event"
      @close-tab="closeTab"
      @move-tab="moveTab($event.from, $event.to)"
    />
  </div>
</template>
