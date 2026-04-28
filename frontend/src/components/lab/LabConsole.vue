<script setup>
import { ref } from 'vue'
import TerminalPanel from '@/components/lab/TerminalPanel.vue'

const tabComponents = { ssh: TerminalPanel }

defineProps({
  tabs: { type: Array, required: true },
  activeTabId: { type: String, default: null },
  limitError: { type: String, default: null },
  secrets: { type: Object, default: () => ({}) },
})

const emit = defineEmits(['select-tab', 'close-tab', 'move-tab'])

const dragFrom = ref(null)

function onDragStart(e, tabId) {
  dragFrom.value = tabId
  e.dataTransfer.effectAllowed = 'move'
}

function onDragOver(e, tabId) {
  if (dragFrom.value && dragFrom.value !== tabId) {
    e.preventDefault()
    e.dataTransfer.dropEffect = 'move'
  }
}

function onDrop(e, tabId) {
  e.preventDefault()
  if (dragFrom.value && dragFrom.value !== tabId) {
    emit('move-tab', { from: dragFrom.value, to: tabId })
  }
  dragFrom.value = null
}

function onDragEnd() {
  dragFrom.value = null
}
</script>

<template>
  <div class="w-2/5 shrink-0 border-l border-slate-800 bg-slate-950 flex flex-col overflow-hidden">

    <!-- Tab bar -->
    <div v-if="tabs.length" class="flex flex-wrap items-center border-b border-slate-800 shrink-0">
      <button
        v-for="tab in tabs"
        :key="tab.id"
        draggable="true"
        class="flex items-center gap-1.5 px-3 py-2 text-xs font-medium border-r border-slate-800 shrink-0 transition-colors cursor-grab active:cursor-grabbing select-none"
        :class="[
          tab.id === activeTabId
            ? 'bg-slate-900 text-slate-200'
            : 'text-slate-500 hover:text-slate-300 hover:bg-slate-900/50',
          dragFrom === tab.id ? 'opacity-40' : '',
        ]"
        @click="emit('select-tab', tab.id)"
        @mousedown.middle.prevent="emit('close-tab', tab.id)"
        @dragstart="onDragStart($event, tab.id)"
        @dragover="onDragOver($event, tab.id)"
        @drop="onDrop($event, tab.id)"
        @dragend="onDragEnd"
      >
        <span class="truncate max-w-24">{{ tab.label }}</span>
        <span
          class="text-slate-600 hover:text-slate-300 transition-colors leading-none cursor-pointer"
          @click.stop="emit('close-tab', tab.id)"
        >×</span>
      </button>
    </div>

    <!-- Terminal panels (v-show keeps WS alive when switching) -->
    <div class="flex-1 overflow-hidden relative">
      <template v-if="tabs.length">
        <template v-for="tab in tabs" :key="tab.id">
          <component
            :is="tabComponents[tab.type]"
            v-if="tabComponents[tab.type] && secrets[tab.serverId]"
            v-show="tab.id === activeTabId"
            :server-id="tab.serverId"
            :secret="secrets[tab.serverId]"
          />
        </template>
      </template>
      <div v-else class="flex items-center justify-center h-full px-6 text-center">
        <span v-if="limitError" class="text-xs text-amber-400">{{ limitError }}</span>
        <span v-else class="text-xl text-slate-600">No active terminal connection — click a protocol badge on a provisioned server to connect.</span>
      </div>
    </div>

  </div>
</template>
