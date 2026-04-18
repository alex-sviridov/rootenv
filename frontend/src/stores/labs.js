import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { fetchFolders, fetchLabsInFolder } from '@/api/labs'

export const useLabsStore = defineStore('labs', () => {
  const groups = ref([])
  const labsByGroup = ref({})
  const selectedGroupSlug = ref(null)
  const loading = ref(false)
  const error = ref(null)

  const selectedGroup = computed(() =>
    groups.value.find(g => g.id === selectedGroupSlug.value) ?? null
  )
  const currentLabs = computed(() =>
    selectedGroupSlug.value ? (labsByGroup.value[selectedGroupSlug.value] ?? []) : []
  )

  async function withLoading(fn) {
    loading.value = true
    error.value = null
    try {
      await fn()
    } catch (e) {
      error.value = e.message
    } finally {
      loading.value = false
    }
  }

  const loadGroups = () =>
    withLoading(async () => { groups.value = await fetchFolders() })

  async function selectGroup(id) {
    selectedGroupSlug.value = id
    if (!labsByGroup.value[id])
      await withLoading(async () => { labsByGroup.value[id] = await fetchLabsInFolder(id) })
  }

  const clearGroup = () => { selectedGroupSlug.value = null }

  return { groups, labsByGroup, selectedGroupSlug, selectedGroup, currentLabs, loading, error, loadGroups, selectGroup, clearGroup }
})
