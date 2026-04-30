<script setup>
import { useLabSession } from '@/composables/useLabSession'
import LabNavigation from '@/components/lab/LabNavigation.vue'
import LabControls from '@/components/lab/LabControls.vue'
import LabContent from '@/components/lab/LabContent.vue'
import LabConsole from '@/components/lab/LabConsole.vue'

const {
  lab, selectedTask, currentTask, error, secrets,
  tabs, activeTabId, limitError, openTab, closeTab, moveTab,
} = useLabSession()
</script>

<template>
  <div v-if="error" class="p-8 text-sm text-red-400">{{ error }}</div>
  <div v-else-if="!lab" class="p-8 text-sm text-slate-500">Loading…</div>
  <div v-else class="flex h-full overflow-hidden">
    <aside class="w-64 shrink-0 border-r border-slate-800 flex flex-col overflow-hidden">
      <LabNavigation :lab="lab" :selected-task="selectedTask" @select-task="selectedTask = $event" />
      <LabControls :lab-id="lab.id" :lab-name="lab.title" @open-tab="({ server, protocol }) => openTab(server, protocol)" />
    </aside>
    <LabContent :task="currentTask" />
    <LabConsole
      :tabs="tabs"
      :active-tab-id="activeTabId"
      :limit-error="limitError"
      :secrets="secrets"
      @select-tab="activeTabId = $event"
      @close-tab="closeTab"
      @move-tab="moveTab($event.from, $event.to)"
    />
  </div>
</template>
