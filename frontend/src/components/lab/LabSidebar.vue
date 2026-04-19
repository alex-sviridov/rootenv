<script setup>
import LabNavigation from '@/components/lab/LabNavigation.vue'
import LabControls from '@/components/lab/LabControls.vue'

defineProps({
  lab: { type: Object, required: true },
  selectedTask: { type: Number, required: true },
})

const emit = defineEmits(['selectTask'])
</script>

<template>
  <aside class="w-64 shrink-0 border-r border-slate-800 flex flex-col overflow-hidden">
    <LabNavigation :lab="lab" :selected-task="selectedTask" @select-task="emit('selectTask', $event)" />
    <LabControls :lab-id="lab.id" :lab-name="lab.id" />
  </aside>
</template>
