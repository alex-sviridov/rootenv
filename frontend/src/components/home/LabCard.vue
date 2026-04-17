<script setup>
import { DocumentTextIcon } from '@heroicons/vue/24/outline'

defineProps({
  lab: { type: Object, required: true },
})

const emit = defineEmits(['open'])
</script>

<template>
  <button
    class="text-left rounded-xl border border-slate-700/60 bg-slate-800/50 p-5 hover:border-indigo-500 hover:bg-slate-800 transition-all group cursor-pointer"
    @click="emit('open', lab.slug)"
  >
    <div class="mb-3">
      <div class="w-10 h-10 rounded-lg bg-slate-700/60 flex items-center justify-center shrink-0">
        <DocumentTextIcon class="w-5 h-5 text-slate-400" />
      </div>
    </div>
    <h3 class="text-sm font-semibold text-slate-100 mb-2 group-hover:text-indigo-400 transition-colors">
      {{ lab.title }}
    </h3>
    <p class="text-xs text-slate-400 leading-relaxed pt-2">
      {{ lab.description }}
    </p>
  </button>
</template>
