<script setup>
import { HomeIcon, ChevronRightIcon } from '@heroicons/vue/24/outline'
import { RouterLink, useRouter } from 'vue-router'
import { useUserStore } from '@/stores/user'
import { useBreadcrumbsStore } from '@/stores/breadcrumbs'
import UserMenu from '@/components/ui/UserMenu.vue'

const router = useRouter()
const user = useUserStore()
const breadcrumbs = useBreadcrumbsStore()
</script>

<template>
  <header class="flex items-center justify-between px-8 h-14 border-b border-slate-800 bg-slate-900 shrink-0">
    <nav class="flex items-center gap-2 text-sm">
      <HomeIcon class="w-4 h-4 text-indigo-400 shrink-0" />

      <template v-for="(crumb, i) in breadcrumbs.crumbs" :key="i">
        <ChevronRightIcon v-if="i > 0" class="w-3.5 h-3.5 text-slate-600" />
        <RouterLink
          v-if="crumb.to"
          :to="crumb.to"
          class="font-semibold text-slate-400 hover:text-white transition-colors"
        >{{ crumb.label }}</RouterLink>
        <button
          v-else-if="crumb.action"
          class="font-semibold text-slate-400 hover:text-white transition-colors"
          @click="crumb.action()"
        >{{ crumb.label }}</button>
        <span v-else class="font-semibold text-white">{{ crumb.label }}</span>
      </template>
    </nav>

    <div class="flex items-center gap-3">
      <UserMenu v-if="user.isAuthenticated" />
      <button
        v-else
        class="text-sm font-medium text-indigo-400 hover:text-indigo-300 transition-colors"
        @click="router.push('/login')"
      >Sign in</button>
    </div>
  </header>
</template>
