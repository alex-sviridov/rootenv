<script setup>
import { HomeIcon, ChevronRightIcon } from '@heroicons/vue/24/outline'
import { useLabsStore } from '@/stores/labs'

const user = { name: 'Alex S.', email: 'flashhaos@gmail.com' }
const labs = useLabsStore()
</script>

<template>
  <header class="flex items-center justify-between px-8 h-14 border-b border-slate-800 bg-slate-900 shrink-0">
    <nav class="flex items-center gap-2 text-sm">
      <div class="flex items-center gap-2">
        <HomeIcon class="w-4 h-4 text-indigo-400 shrink-0" />
        <button
          class="font-semibold transition-colors"
          :class="labs.selectedGroupSlug ? 'text-slate-400 hover:text-white' : 'text-white'"
          @click="labs.clearGroup()"
        >
          LinuxLab
        </button>
      </div>

      <template v-if="labs.selectedGroup">
        <ChevronRightIcon class="w-3.5 h-3.5 text-slate-600" />
        <span class="text-white font-medium">{{ labs.selectedGroup.title }}</span>
      </template>
    </nav>

    <div class="flex items-center gap-3">
      <div class="w-8 h-8 rounded-full bg-indigo-500 flex items-center justify-center text-white text-xs font-semibold select-none">
        {{ user.name.split(' ').map(n => n[0]).join('') }}
      </div>
      <span class="text-sm text-slate-300">{{ user.name }}</span>
    </div>
  </header>
</template>
