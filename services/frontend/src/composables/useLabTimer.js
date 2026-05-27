import { ref, computed, onMounted, onUnmounted } from 'vue'

export function useLabTimer(expiresAtRef) {
  const now = ref(Date.now())
  let ticker = null

  onMounted(() => { ticker = setInterval(() => { now.value = Date.now() }, 1000) })
  onUnmounted(() => clearInterval(ticker))

  const expiresIn = computed(() => {
    const exp = expiresAtRef.value
    if (!exp) return null
    const secs = Math.max(0, Math.floor((new Date(exp).getTime() - now.value) / 1000))
    const h = Math.floor(secs / 3600)
    const m = Math.floor((secs % 3600) / 60)
    const s = secs % 60
    if (h > 0) return `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
    return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
  })

  return { expiresIn }
}
