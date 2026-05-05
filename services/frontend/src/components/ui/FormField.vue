<script setup>
defineProps({
  label: { type: String, required: true },
  type: { type: String, default: 'text' },
  modelValue: { type: String, default: '' },
  autocomplete: { type: String, default: undefined },
  placeholder: { type: String, default: undefined },
  focusBorder: { type: String, default: 'focus:border-indigo-500' },
})
defineEmits(['update:modelValue'])
</script>

<template>
  <div class="flex flex-col gap-1.5">
    <label class="text-xs font-medium text-slate-400 uppercase tracking-wide">{{ label }}</label>
    <input
      :type="type"
      :value="modelValue"
      :autocomplete="autocomplete"
      :placeholder="placeholder"
      :class="focusBorder"
      class="bg-slate-900 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-white placeholder-slate-500 focus:outline-none transition-colors"
      @input="$emit('update:modelValue', $event.target.value)"
    />
  </div>
</template>
