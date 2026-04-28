<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { useSshRelayConnection } from '@/composables/useSshRelayConnection'

const props = defineProps({
  serverId: { type: String, required: true },
  secret: { type: String, required: true },
})

const termEl = ref(null)
const { terminal, fitAddon } = useSshRelayConnection(props.serverId, props.secret)
const showAltWHint = ref(false)
let resizeObserver = null
let terminalFocused = false
let altWHintTimer = null

// Ctrl+key → terminal escape sequence. Keys the browser would otherwise swallow.
// Ctrl+W is uncatchable (browser closes tab); use Alt+W instead — shown in beforeunload message.
const CTRL_KEY_MAP = {
  t: '\x14', r: '\x12', n: '\x0e',
  a: '\x01', e: '\x05', k: '\x0b', u: '\x15',
  l: '\x0c', c: '\x03', z: '\x1a', d: '\x04',
  f: '\x06', b: '\x02', p: '\x10', q: '\x11',
}

function onDocumentKeydown(e) {
  if (!terminalFocused) return

  // Alt+W → send Ctrl+W sequence (\x17) to terminal
  if (e.altKey && !e.ctrlKey && !e.metaKey && e.key.toLowerCase() === 'w') {
    e.preventDefault()
    e.stopPropagation()
    terminal.input('\x17')
    return
  }

  if (!e.ctrlKey || e.altKey || e.metaKey) return
  const seq = CTRL_KEY_MAP[e.key.toLowerCase()]
  if (!seq) return
  e.preventDefault()
  e.stopPropagation()
  terminal.input(seq)
}

function onBeforeUnload(e) {
  if (!terminalFocused) return
  e.preventDefault()
  e.returnValue = ''
  // If the user clicks "Stay", the timeout fires and we show the in-page hint.
  altWHintTimer = setTimeout(() => {
    showAltWHint.value = true
    altWHintTimer = setTimeout(() => { showAltWHint.value = false }, 5000)
  }, 500)
}

function onContextMenu(e) {
  e.preventDefault()
  navigator.clipboard.readText().then((text) => {
    if (text) terminal.input(text)
  }).catch(() => {})
}

onMounted(() => {
  if (!termEl.value) return

  terminal.open(termEl.value)
  fitAddon.fit()

  resizeObserver = new ResizeObserver(() => fitAddon.fit())
  resizeObserver.observe(termEl.value)

  terminal.textarea?.addEventListener('focus', () => { terminalFocused = true })
  terminal.textarea?.addEventListener('blur', () => { terminalFocused = false })

  // Copy on select
  terminal.onSelectionChange(() => {
    const sel = terminal.getSelection()
    if (sel) navigator.clipboard.writeText(sel).catch(() => {})
  })

  termEl.value.addEventListener('contextmenu', onContextMenu)
  document.addEventListener('keydown', onDocumentKeydown, true)
  window.addEventListener('beforeunload', onBeforeUnload)
})

onUnmounted(() => {
  resizeObserver?.disconnect()
  clearTimeout(altWHintTimer)
  document.removeEventListener('keydown', onDocumentKeydown, true)
  window.removeEventListener('beforeunload', onBeforeUnload)
  termEl.value?.removeEventListener('contextmenu', onContextMenu)
})
</script>

<template>
  <div class="relative w-full h-full">
    <div ref="termEl" class="w-full h-full" />
    <div
      v-if="showAltWHint"
      class="absolute bottom-4 right-4 flex items-center gap-3 bg-slate-700 text-slate-100 text-sm px-4 py-2 rounded shadow-lg z-50"
    >
      <span>Tip: use <kbd class="bg-slate-600 px-1 rounded">Alt+W</kbd> to send Ctrl+W to the terminal.</span>
      <button @click="showAltWHint = false" class="text-slate-400 hover:text-white">✕</button>
    </div>
  </div>
</template>
