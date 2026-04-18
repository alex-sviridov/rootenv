<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { HomeIcon, ChevronRightIcon } from '@heroicons/vue/24/outline'
import { RouterLink, useRouter } from 'vue-router'
import { useUserStore } from '@/stores/user'
import { useBreadcrumbsStore } from '@/stores/breadcrumbs'

const router = useRouter()
const user = useUserStore()
const breadcrumbs = useBreadcrumbsStore()

const menuOpen = ref(false)
const menuRef = ref(null)

function toggleMenu() { menuOpen.value = !menuOpen.value }
function closeMenu() { menuOpen.value = false }

function onClickOutside(e) {
  if (menuRef.value && !menuRef.value.contains(e.target)) closeMenu()
}

onMounted(() => document.addEventListener('click', onClickOutside))
onUnmounted(() => document.removeEventListener('click', onClickOutside))

function goAccount() { closeMenu(); router.push('/account') }
function signOut() { closeMenu(); user.signOut() }

const initials = (u) => u?.email?.[0]?.toUpperCase() ?? '?'
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
      <template v-if="user.isAuthenticated">
        <div ref="menuRef" class="relative">
          <button
            class="flex items-center gap-2 rounded-lg px-2 py-1 hover:bg-slate-800 transition-colors"
            @click.stop="toggleMenu"
          >
            <div class="w-7 h-7 rounded-full bg-indigo-500 flex items-center justify-center text-white text-xs font-semibold select-none">
              {{ initials(user.user) }}
            </div>
            <span class="text-sm text-slate-300">{{ user.user.email }}</span>
          </button>

          <div
            v-if="menuOpen"
            class="absolute right-0 top-full mt-1 w-44 rounded-lg border border-slate-700 bg-slate-900 shadow-xl py-1 z-50"
          >
            <button
              class="w-full text-left px-4 py-2 text-sm text-slate-300 hover:bg-slate-800 hover:text-white transition-colors"
              @click="goAccount"
            >Account</button>
            <div class="my-1 border-t border-slate-800" />
            <button
              class="w-full text-left px-4 py-2 text-sm text-slate-300 hover:bg-slate-800 hover:text-white transition-colors"
              @click="signOut"
            >Sign out</button>
          </div>
        </div>
      </template>
      <template v-else>
        <button
          class="text-sm font-medium text-indigo-400 hover:text-indigo-300 transition-colors"
          @click="router.push('/login')"
        >Sign in</button>
      </template>
    </div>
  </header>
</template>
