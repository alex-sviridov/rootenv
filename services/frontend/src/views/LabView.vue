<script setup>
import { ref, computed, watch, onUnmounted } from 'vue'
import { ChevronLeftIcon, ChevronRightIcon } from '@heroicons/vue/24/outline'
import { useUserStore } from '@/stores/user'
import { useLabSession } from '@/composables/useLabSession'
import LabNavigation from '@/components/lab/LabNavigation.vue'
import LabControls from '@/components/lab/LabControls.vue'
import LabContent from '@/components/lab/LabContent.vue'
import LabConsole from '@/components/lab/LabConsole.vue'

const {
  lab, selectedTask, currentTask, error,
  tabs, activeTabId, limitError, openTab, selectTab, closeTab, moveTab,
  attemptId, grades,
} = useLabSession()

const userStore = useUserStore()

const labId = computed(() => lab.value?.id)
const labName = computed(() => lab.value?.title)
const labTasks = computed(() => lab.value?.content ?? [])

// ─── layout constants ────────────────────────────────────────────────────────
const MIN_W     = 200
const SIDEBAR_W = 256
const DIVIDER_W = 5

// ─── panel visibility ────────────────────────────────────────────────────────
const contentVisible = ref(true)
const consoleVisible = ref(true)

function toggleContent() { contentVisible.value = !contentVisible.value }
function toggleConsole() { consoleVisible.value = !consoleVisible.value }

// ─── resize ──────────────────────────────────────────────────────────────────
const flexContainer    = ref(null)
const containerWidth   = ref(0)
const consoleWidth     = ref(0)
const isDragging       = ref(false)
const isDividerHovered = ref(false)

const availableWidth = computed(() => containerWidth.value - SIDEBAR_W - DIVIDER_W)

const contentStyle = computed(() => {
  if (!consoleVisible.value) return { flex: '1 1 0%' }
  return { width: availableWidth.value - consoleWidth.value + 'px' }
})

const consoleStyle = computed(() => {
  if (!contentVisible.value) return { flex: '1 1 0%' }
  return { width: consoleWidth.value + 'px' }
})

let _saveTimer = null
function scheduleSave(width) {
  clearTimeout(_saveTimer)
  _saveTimer = setTimeout(() => userStore.saveSetting('dividerConsoleWidth', width), 800)
}

const ro = new ResizeObserver(([entry]) => {
  const w = entry.contentRect.width
  const avail = w - SIDEBAR_W - DIVIDER_W
  if (containerWidth.value === 0) {
    const saved = userStore.settings.dividerConsoleWidth
    consoleWidth.value = saved ?? Math.round(avail * 0.4)
  } else {
    const prevAvail = containerWidth.value - SIDEBAR_W - DIVIDER_W
    consoleWidth.value = Math.round(consoleWidth.value * (avail / prevAvail))
  }
  containerWidth.value = w
})

watch(flexContainer, (el) => { ro.disconnect(); if (el) ro.observe(el) })
onUnmounted(() => { ro.disconnect(); clearTimeout(_saveTimer) })

let _moveHandler = null
let _upHandler   = null

function onDividerMousedown(e) {
  if (e.button !== 0) return
  isDragging.value = true
  e.preventDefault()
  const startX = e.clientX
  const startW = consoleWidth.value

  _moveHandler = (e) => {
    consoleWidth.value = Math.min(
      Math.max(startW + (startX - e.clientX), MIN_W),
      availableWidth.value - MIN_W,
    )
  }
  _upHandler = () => {
    isDragging.value = false
    scheduleSave(consoleWidth.value)
    document.removeEventListener('mousemove', _moveHandler)
    document.removeEventListener('mouseup', _upHandler)
    _moveHandler = null
    _upHandler   = null
  }
  document.addEventListener('mousemove', _moveHandler)
  document.addEventListener('mouseup', _upHandler)
}

onUnmounted(() => {
  if (_moveHandler) document.removeEventListener('mousemove', _moveHandler)
  if (_upHandler)   document.removeEventListener('mouseup', _upHandler)
})
</script>

<template>
  <div v-if="error" class="p-8 text-sm text-red-400">{{ error }}</div>
  <div v-else-if="!lab" class="p-8 text-sm text-slate-500">Loading…</div>
  <div v-else ref="flexContainer" class="flex h-full overflow-hidden">

    <aside class="w-64 shrink-0 border-r border-slate-800 flex flex-col overflow-hidden">
      <LabNavigation :tasks="labTasks" :selected-task="selectedTask" :grades="grades" @select-task="selectedTask = $event" />
      <LabControls :lab-id="labId" :lab-name="labName" @open-tab="({ server, protocol }) => openTab(server, protocol)" />
    </aside>

    <div v-show="contentVisible" class="overflow-hidden" :style="contentStyle">
      <LabContent :task="currentTask" :grades="grades" />
    </div>

    <!-- divider: outer handles hover; inner bar handles drag -->
    <div
      class="relative w-[5px] shrink-0 z-10 flex items-center justify-center"
      @mouseenter="isDividerHovered = true"
      @mouseleave="isDividerHovered = false"
    >
      <!-- drag bar -->
      <div
        class="absolute inset-0 cursor-col-resize transition-colors"
        :class="isDividerHovered || isDragging ? 'bg-slate-600' : 'bg-slate-800'"
        @mousedown="onDividerMousedown"
      />


      <!-- restore buttons: always visible when a panel is hidden -->
      <button
        v-if="!contentVisible"
        class="absolute top-1/2 -translate-y-1/2 left-1.5 z-20 w-4 h-8 flex items-center justify-center bg-slate-600 hover:bg-slate-500 text-white rounded-r transition-colors"
        title="Show content"
        @click="toggleContent"
      >
        <ChevronRightIcon class="w-3 h-3" />
      </button>
      <button
        v-if="!consoleVisible"
        class="absolute top-1/2 -translate-y-1/2 -left-4 z-20 w-4 h-8 flex items-center justify-center bg-slate-600 hover:bg-slate-500 text-white rounded-l transition-colors"
        title="Show console"
        @click="toggleConsole"
      >
        <ChevronLeftIcon class="w-3 h-3" />
      </button>

      <!-- hide buttons: on hover only, when both panels are visible -->
      <template v-if="isDividerHovered && !isDragging && contentVisible && consoleVisible">
        <button
          class="absolute top-1/2 -translate-y-1/2 -left-4 z-20 w-4 h-8 flex items-center justify-center bg-slate-700 hover:bg-slate-600 text-slate-300 hover:text-white rounded-l transition-colors"
          title="Hide content"
          @click="toggleContent"
        >
          <ChevronLeftIcon class="w-3 h-3" />
        </button>

        <button
          class="absolute top-1/2 -translate-y-1/2 -right-4 z-20 w-4 h-8 flex items-center justify-center bg-slate-700 hover:bg-slate-600 text-slate-300 hover:text-white rounded-r transition-colors"
          title="Hide console"
          @click="toggleConsole"
        >
          <ChevronRightIcon class="w-3 h-3" />
        </button>
      </template>
    </div>

    <div v-show="consoleVisible" class="overflow-hidden" :style="consoleStyle">
      <LabConsole
        :tabs="tabs"
        :active-tab-id="activeTabId"
        :limit-error="limitError"
        :attempt-id="attemptId"
        @select-tab="selectTab"
        @close-tab="closeTab"
        @move-tab="moveTab($event.from, $event.to)"
      />
    </div>

  </div>
</template>
