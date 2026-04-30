<script setup>
defineProps({
  dotClass: { type: String, default: 'bg-slate-400' },
  ping: { type: Boolean, default: false },
  label: { type: String, default: null },
  labelClass: { type: String, default: 'text-slate-400' },
})
</script>

<template>
  <div class="flex items-center gap-1.5 min-w-0">
    <span class="relative flex h-2 w-2 shrink-0">
      <span
        v-if="ping"
        class="absolute inline-flex h-full w-full rounded-full opacity-60 animate-ping"
        :class="dotClass"
      />
      <span class="relative inline-flex rounded-full h-2 w-2" :class="dotClass" />
    </span>
    <span v-if="label" class="text-xs truncate" :class="labelClass">{{ label }}</span>
  </div>
</template>
