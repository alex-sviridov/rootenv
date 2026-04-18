<script setup>
defineProps({
  lab: { type: Object, required: true },
  selectedTask: { type: Number, required: true },
})

const emit = defineEmits(['selectTask'])
</script>

<template>
  <aside class="w-64 shrink-0 border-r border-slate-800 flex flex-col overflow-hidden">
    <div class="flex-1 overflow-y-auto">
      <div class="p-3 border-b border-slate-800">
        <p class="text-xs font-semibold text-slate-500 uppercase tracking-wider">Tasks</p>
      </div>
      <div>
        <button
          v-for="(task, i) in lab.content"
          :key="i"
          class="w-full text-left px-4 py-2.5 text-sm border-l-2 transition-colors"
          :class="i === selectedTask
            ? 'border-indigo-500 bg-slate-800 text-white'
            : 'border-transparent text-slate-400 hover:text-white hover:bg-slate-800/50'"
          @click="emit('selectTask', i)"
        >
          {{ task.title }}
        </button>
      </div>
    </div>

    <div class="shrink-0 border-t border-slate-800 p-4 text-xs text-slate-600">
      Lab controls
    </div>
  </aside>
</template>
