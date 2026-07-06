<script setup>
import { parseExerciseBlocks } from '@/lib/exercises'

const props = defineProps({
  tasks: { type: Array, required: true },
  selectedTask: { type: Number, required: true },
  grades: { type: Object, default: () => ({}) },
})

const emit = defineEmits(['selectTask'])

function exerciseSummary(task) {
  const ids = parseExerciseBlocks(task.content ?? '').map((b) => b.id)
  if (ids.length === 0) return null
  const passed = ids.filter((id) => props.grades[id] === true).length
  return { passed, total: ids.length }
}

const CIRCLE_RADIUS = 7
const CIRCLE_CIRCUMFERENCE = 2 * Math.PI * CIRCLE_RADIUS

function progressOffset(summary) {
  const ratio = summary.total === 0 ? 0 : summary.passed / summary.total
  return CIRCLE_CIRCUMFERENCE * (1 - ratio)
}
</script>

<template>
  <div class="flex-1 overflow-y-auto">
    <div class="px-4 py-3 border-b border-slate-800">
      <p class="text-xs font-semibold text-slate-500 uppercase tracking-widest">Tasks</p>
    </div>
    <div>
      <button
        v-for="(task, i) in tasks"
        :key="i"
        class="w-full flex items-center gap-2 text-left px-4 py-2.5 text-sm border-l-2 transition-colors"
        :class="i === selectedTask
          ? 'border-indigo-500 bg-slate-800 text-white'
          : 'border-transparent text-slate-400 hover:text-white hover:bg-slate-800/50'"
        @click="emit('selectTask', i)"
      >
        <span class="flex-1 truncate">{{ task.title }}</span>
        <span
          v-if="exerciseSummary(task)"
          class="shrink-0"
          :title="`${exerciseSummary(task).passed}/${exerciseSummary(task).total} exercises passed`"
        >
          <svg width="18" height="18" viewBox="0 0 18 18" class="-rotate-90">
            <circle
              cx="9"
              cy="9"
              :r="CIRCLE_RADIUS"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              class="text-slate-700"
            />
            <circle
              cx="9"
              cy="9"
              :r="CIRCLE_RADIUS"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              :stroke-dasharray="CIRCLE_CIRCUMFERENCE"
              :stroke-dashoffset="progressOffset(exerciseSummary(task))"
              :class="exerciseSummary(task).passed === exerciseSummary(task).total
                ? 'text-green-400'
                : 'text-indigo-400'"
            />
          </svg>
        </span>
      </button>
    </div>
  </div>
</template>
