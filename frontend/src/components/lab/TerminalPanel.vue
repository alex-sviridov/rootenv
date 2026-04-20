<script setup>
import { ref, onMounted } from 'vue'
import { useRelayConnection } from '@/composables/useRelayConnection'

const props = defineProps({
  serverId: { type: String, required: true },
})

const termEl = ref(null)
const { terminal, fitAddon } = useRelayConnection(props.serverId)

onMounted(() => {
  if (termEl.value) {
    terminal.open(termEl.value)
    fitAddon.fit()

    const resizeObserver = new ResizeObserver(() => {
      fitAddon.fit()
    })
    resizeObserver.observe(termEl.value)

    const cleanup = () => resizeObserver.disconnect()
    termEl.value._resizeObserverCleanup = cleanup
  }
})
</script>

<template>
  <div ref="termEl" class="w-full h-full" />
</template>
