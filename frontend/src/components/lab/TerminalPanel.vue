<script setup>
import { useRelayConnection } from '@/composables/useRelayConnection'

const props = defineProps({
  serverId: { type: String, required: true },
})

const { status } = useRelayConnection(props.serverId)
</script>

<template>
  <div class="w-full h-full p-4 font-mono text-xs text-slate-400 overflow-auto">
    <pre>{{ status }}</pre>
  </div>
</template>
