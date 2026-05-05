<script setup>
import { ArrowPathIcon } from '@heroicons/vue/24/outline'
import { attemptConfig } from '@/config/labStates'
import StatusDot from '@/components/ui/StatusDot.vue'

defineProps({
  state: { type: String, default: null },
  expiresIn: { type: String, default: null },
  loading: { type: Boolean, default: false },
})
defineEmits(['refresh'])
</script>

<template>
  <div class="px-4 py-3 border-b border-slate-800 flex items-center justify-between gap-2">
    <div class="shrink-0">
      <p class="text-xs font-semibold text-slate-500 uppercase tracking-widest">Lab Session</p>
      <p v-if="expiresIn" class="text-[10px] text-slate-600 mt-0.5">expires in {{ expiresIn }}</p>
    </div>
    <StatusDot
      v-if="state && attemptConfig[state]"
      :dot-class="attemptConfig[state].dot"
      :ping="attemptConfig[state].ping"
      :label="attemptConfig[state].label"
      :label-class="attemptConfig[state].text"
    />
    <button
      class="text-slate-600 hover:text-slate-300 transition-colors ml-auto shrink-0"
      :class="loading ? 'animate-spin pointer-events-none' : ''"
      @click="$emit('refresh')"
    >
      <ArrowPathIcon class="w-3.5 h-3.5" />
    </button>
  </div>
</template>
