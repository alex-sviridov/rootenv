<script setup>
import { ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { fetchFolders, fetchLabsInFolder, fetchLab } from '@/api/labs'
import { useBreadcrumbsStore } from '@/stores/breadcrumbs'
import GroupCard from '@/components/home/GroupCard.vue'
import LabCard from '@/components/home/LabCard.vue'

const route = useRoute()
const breadcrumbs = useBreadcrumbsStore()

const groups = ref([])
const labs = ref([])
const group = ref(null)
const loading = ref(true)
const error = ref(null)

async function load() {
  loading.value = true
  error.value = null
  try {
    if (route.params.group) {
      [group.value, labs.value] = await Promise.all([
        fetchLab(route.params.group),
        fetchLabsInFolder(route.params.group),
      ])
    } else {
      groups.value = await fetchFolders()
    }
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

watch(() => route.params.group, load, { immediate: true })

watch(group, () => {
  breadcrumbs.set(
    group.value
      ? [{ label: 'LinuxLab', to: '/' }, { label: group.value.title }]
      : [{ label: 'LinuxLab' }]
  )
}, { immediate: true })
</script>

<template>
  <div class="flex flex-col gap-6 p-8 max-w-7xl mx-auto">

    <p v-if="error" class="text-sm text-red-400">{{ error }}</p>

    <!-- Groups view -->
    <template v-if="!route.params.group">
      <div>
        <h1 class="text-xl font-semibold text-white mb-1">Lab Groups</h1>
        <p class="text-sm font-medium text-slate-400 mb-2">Choose a group to browse its labs.</p>
      </div>
      <p v-if="loading" class="text-sm text-slate-500">Loading…</p>
      <div v-else class="grid grid-cols-3 xl:grid-cols-4 gap-4">
        <GroupCard v-for="g in groups" :key="g.id" :group="g" />
      </div>
    </template>

    <!-- Labs view -->
    <template v-else>
      <div v-if="group">
        <h1 class="text-xl font-semibold text-white mb-1">{{ group.title }}</h1>
        <p class="text-sm font-medium text-slate-400 mb-2">{{ group.description }}</p>
      </div>
      <p v-if="loading" class="text-sm text-slate-500">Loading…</p>
      <div v-else class="grid grid-cols-3 xl:grid-cols-4 gap-4">
        <LabCard v-for="lab in labs" :key="lab.id" :lab="lab" />
      </div>
    </template>

  </div>
</template>
