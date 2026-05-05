<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { useRouter, RouterLink } from 'vue-router'
import { useUserStore } from '@/stores/user'
import { useAttemptsStore } from '@/stores/attempts'

const router = useRouter()
const user = useUserStore()
const attempts = useAttemptsStore()

const menuOpen = ref(false)
const menuRef = ref(null)

function toggleMenu() { menuOpen.value = !menuOpen.value }
function closeMenu() { menuOpen.value = false }

function onClickOutside(e) {
  if (menuRef.value && !menuRef.value.contains(e.target)) closeMenu()
}

onMounted(() => document.addEventListener('click', onClickOutside))
onUnmounted(() => document.removeEventListener('click', onClickOutside))

const initials = (u) => u?.email?.[0]?.toUpperCase() ?? '?'
</script>

<template>
  <div ref="menuRef" class="relative">
    <button
      class="flex items-center gap-2 rounded-lg px-2 py-1 hover:bg-slate-800 transition-colors"
      @click.stop="toggleMenu"
    >
      <div class="relative w-7 h-7">
        <div class="w-7 h-7 rounded-full bg-indigo-500 flex items-center justify-center text-white text-xs font-semibold select-none">
          {{ initials(user.user) }}
        </div>
        <template v-if="attempts.activeAttempt">
          <span class="absolute -top-0.5 -right-0.5 inline-flex rounded-full h-2.5 w-2.5 bg-green-400 opacity-75 animate-ping" />
          <span class="absolute -top-0.5 -right-0.5 inline-flex rounded-full h-2.5 w-2.5 bg-green-400" />
        </template>
      </div>
      <span class="text-sm text-slate-300">{{ user.user.email }}</span>
    </button>

    <div
      v-if="menuOpen"
      class="absolute right-0 top-full mt-1 w-52 rounded-lg border border-slate-700 bg-slate-900 shadow-xl py-1 z-50"
    >
      <template v-if="attempts.activeAttempt">
        <RouterLink
          :to="{ name: 'lab', params: { slug: attempts.activeAttempt.lab } }"
          class="flex items-center gap-2 px-4 py-2 text-sm text-green-400 hover:bg-slate-800 hover:text-green-300 transition-colors"
          @click="closeMenu"
        >
          <span class="inline-flex rounded-full h-2 w-2 bg-green-400 shrink-0" />
          {{ attempts.activeAttempt.lab_name }}
        </RouterLink>
        <div class="my-1 border-t border-slate-800" />
      </template>
      <button
        class="w-full text-left px-4 py-2 text-sm text-slate-300 hover:bg-slate-800 hover:text-white transition-colors"
        @click="closeMenu(); router.push('/account')"
      >Account</button>
      <div class="my-1 border-t border-slate-800" />
      <button
        class="w-full text-left px-4 py-2 text-sm text-slate-300 hover:bg-slate-800 hover:text-white transition-colors"
        @click="closeMenu(); user.signOut()"
      >Sign out</button>
    </div>
  </div>
</template>
