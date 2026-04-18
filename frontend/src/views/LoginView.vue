<script setup>
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useUserStore } from '@/stores/user'

const router = useRouter()
const route = useRoute()
const user = useUserStore()

const mode = ref('login')

const email = ref('')
const password = ref('')
const passwordConfirm = ref('')

async function submit() {
  if (mode.value === 'login') {
    await user.signIn(email.value, password.value)
  } else {
    await user.signUp(email.value, password.value, passwordConfirm.value)
  }
  if (user.isAuthenticated) {
    router.push(route.query.redirect || '/')
  }
}

function switchMode(m) {
  mode.value = m
  user.error = null
}
</script>

<template>
  <div class="flex items-center justify-center min-h-full py-16 px-4">
    <div class="w-full max-w-sm">
      <h1 class="text-2xl font-bold text-white mb-8 text-center">LinuxLab</h1>

      <!-- Tabs -->
      <div class="flex rounded-lg bg-slate-800 p-1 mb-6">
        <button
          class="flex-1 text-sm font-medium py-1.5 rounded-md transition-colors"
          :class="mode === 'login' ? 'bg-slate-700 text-white' : 'text-slate-400 hover:text-white'"
          @click="switchMode('login')"
        >Sign in</button>
        <button
          class="flex-1 text-sm font-medium py-1.5 rounded-md transition-colors"
          :class="mode === 'register' ? 'bg-slate-700 text-white' : 'text-slate-400 hover:text-white'"
          @click="switchMode('register')"
        >Register</button>
      </div>

      <form class="flex flex-col gap-4" @submit.prevent="submit">
        <div class="flex flex-col gap-1.5">
          <label class="text-xs font-medium text-slate-400 uppercase tracking-wide">Email</label>
          <input
            v-model="email"
            type="email"
            autocomplete="email"
            class="bg-slate-800 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-white placeholder-slate-500 focus:outline-none focus:border-indigo-500 transition-colors"
            placeholder="you@example.com"
          />
        </div>

        <div class="flex flex-col gap-1.5">
          <label class="text-xs font-medium text-slate-400 uppercase tracking-wide">Password</label>
          <input
            v-model="password"
            type="password"
            autocomplete="current-password"
            class="bg-slate-800 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-white placeholder-slate-500 focus:outline-none focus:border-indigo-500 transition-colors"
            placeholder="••••••••"
          />
        </div>

        <div v-if="mode === 'register'" class="flex flex-col gap-1.5">
          <label class="text-xs font-medium text-slate-400 uppercase tracking-wide">Confirm password</label>
          <input
            v-model="passwordConfirm"
            type="password"
            autocomplete="new-password"
            class="bg-slate-800 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-white placeholder-slate-500 focus:outline-none focus:border-indigo-500 transition-colors"
            placeholder="••••••••"
          />
        </div>

        <p v-if="user.error" class="text-sm text-red-400">{{ user.error }}</p>

        <button
          type="submit"
          :disabled="user.loading"
          class="mt-1 bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 disabled:cursor-not-allowed text-white text-sm font-semibold py-2.5 rounded-lg transition-colors"
        >
          {{ user.loading ? '…' : mode === 'login' ? 'Sign in' : 'Create account' }}
        </button>
      </form>
    </div>
  </div>
</template>
