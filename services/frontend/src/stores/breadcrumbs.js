import { defineStore } from 'pinia'
import { ref } from 'vue'

// Crumb shape: { label: string, to?: string, action?: () => void }
// to     → rendered as RouterLink
// action → rendered as clickable button
// neither → plain text (current page)
export const useBreadcrumbsStore = defineStore('breadcrumbs', () => {
  const crumbs = ref([])
  const set = (newCrumbs) => { crumbs.value = newCrumbs }
  return { crumbs, set }
})
