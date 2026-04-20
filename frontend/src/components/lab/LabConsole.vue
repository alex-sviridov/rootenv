<script setup>
import TerminalPanel from '@/components/lab/TerminalPanel.vue'

defineProps({
  tabs: { type: Array, required: true },
  activeTabId: { type: String, default: null },
})

const emit = defineEmits(['select-tab', 'close-tab'])
</script>

<template>
  <div class="w-2/5 shrink-0 border-l border-slate-800 bg-slate-950 flex flex-col overflow-hidden">

    <!-- Tab bar -->
    <div v-if="tabs.length" class="flex items-center border-b border-slate-800 overflow-x-auto shrink-0">
      <button
        v-for="tab in tabs"
        :key="tab.id"
        class="flex items-center gap-1.5 px-3 py-2 text-xs font-medium border-r border-slate-800 shrink-0 transition-colors"
        :class="tab.id === activeTabId
          ? 'bg-slate-900 text-slate-200'
          : 'text-slate-500 hover:text-slate-300 hover:bg-slate-900/50'"
        @click="emit('select-tab', tab.id)"
      >
        <span class="truncate max-w-24">{{ tab.label }}</span>
        <span
          class="text-slate-600 hover:text-slate-300 transition-colors leading-none"
          @click.stop="emit('close-tab', tab.id)"
        >×</span>
      </button>
    </div>

    <!-- Terminal panels (v-show keeps WS alive when switching) -->
    <div class="flex-1 overflow-hidden relative">
      <template v-if="tabs.length">
        <TerminalPanel
          v-for="tab in tabs"
          v-show="tab.id === activeTabId"
          :key="tab.id"
          :server-id="tab.id"
        />
      </template>
      <div v-else class="flex items-center justify-center h-full">
        <span class="text-xs text-slate-600">No terminals open — click a provisioned server to connect.</span>
      </div>
    </div>

  </div>
</template>
