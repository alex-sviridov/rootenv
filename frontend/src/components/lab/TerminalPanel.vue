<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { useRelayConnection } from '@/composables/useRelayConnection'

const props = defineProps({
  serverId: { type: String, required: true },
})

const termEl = ref(null)
const { terminal, fitAddon } = useRelayConnection(props.serverId)
let resizeObserver = null

onMounted(() => {
  if (termEl.value) {
    terminal.open(termEl.value)
    fitAddon.fit()

    resizeObserver = new ResizeObserver(() => {
      fitAddon.fit()
    })
    resizeObserver.observe(termEl.value)
  }
})

onUnmounted(() => {
  if (resizeObserver) {
    resizeObserver.disconnect()
  }
})
</script>

<template>
  <div ref="termEl" class="w-full h-full" />
</template>
