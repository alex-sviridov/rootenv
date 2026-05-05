<script setup>
defineProps({
  tasks: { type: Array, required: true },
  selectedTask: { type: Number, required: true },
})

const emit = defineEmits(['selectTask'])
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
</template>
