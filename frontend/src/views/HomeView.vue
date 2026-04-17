<script setup>
import { onMounted } from 'vue'
import { useLabsStore } from '@/stores/labs'
import GroupCard from '@/components/home/GroupCard.vue'
import LabCard from '@/components/home/LabCard.vue'

const labs = useLabsStore()

onMounted(() => labs.loadGroups())
</script>

<template>
  <div class="flex flex-col gap-6 p-8 max-w-7xl mx-auto">

    <p v-if="labs.error" class="text-sm text-red-400">{{ labs.error }}</p>

    <!-- Groups view -->
    <template v-if="!labs.selectedGroupSlug">
      <div>
        <h1 class="text-xl font-semibold text-white mb-1">Lab Groups</h1>
        <p class="text-sm font-medium text-slate-400 mb-2">Choose a group to browse its labs.</p>
      </div>
      <p v-if="labs.loading" class="text-sm text-slate-500">Loading…</p>
      <div v-else class="grid grid-cols-3 xl:grid-cols-4 gap-4">
        <GroupCard
          v-for="group in labs.groups"
          :key="group.id"
          :group="group"
          @select="labs.selectGroup"
        />
      </div>
    </template>

    <!-- Labs view -->
    <template v-else>
      <div>
        <h1 class="text-xl font-semibold text-white mb-1">{{ labs.selectedGroup?.title }}</h1>
        <p class="text-sm font-medium text-slate-400 mb-2">{{ labs.selectedGroup?.description }}</p>
      </div>
      <p v-if="labs.loading" class="text-sm text-slate-500">Loading…</p>
      <div v-else class="grid grid-cols-3 xl:grid-cols-4 gap-4">
        <LabCard
          v-for="lab in labs.currentLabs"
          :key="lab.id"
          :lab="lab"
          @open="id => console.log('open lab', id)"
        />
      </div>
    </template>

  </div>
</template>
