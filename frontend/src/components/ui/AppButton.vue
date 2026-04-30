<script setup>
defineProps({
  variant: { type: String, default: 'primary' },
  loading: { type: Boolean, default: false },
  disabled: { type: Boolean, default: false },
  type: { type: String, default: 'button' },
})
</script>

<template>
  <button
    :type="type"
    :disabled="disabled || loading"
    class="text-sm font-semibold rounded-lg px-4 py-2 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
    :class="{
      'bg-indigo-600 hover:bg-indigo-500 text-white': variant === 'primary',
      'bg-red-700 hover:bg-red-600 text-white': variant === 'danger',
      'text-slate-400 hover:text-white': variant === 'ghost',
    }"
  >
    <slot>{{ loading ? '…' : undefined }}</slot>
  </button>
</template>
