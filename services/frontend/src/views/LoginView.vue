<script setup>
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useUserStore } from '@/stores/user'
import FormField from '@/components/ui/FormField.vue'
import AppButton from '@/components/ui/AppButton.vue'

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
        <FormField
          v-model="email"
          label="Email"
          type="email"
          autocomplete="email"
          placeholder="you@example.com"
          focus-border="focus:border-indigo-500"
        />
        <FormField
          v-model="password"
          label="Password"
          type="password"
          autocomplete="current-password"
          placeholder="••••••••"
          focus-border="focus:border-indigo-500"
        />
        <FormField
          v-if="mode === 'register'"
          v-model="passwordConfirm"
          label="Confirm password"
          type="password"
          autocomplete="new-password"
          placeholder="••••••••"
          focus-border="focus:border-indigo-500"
        />

        <p v-if="user.error" class="text-sm text-red-400">{{ user.error }}</p>

        <AppButton type="submit" variant="primary" :loading="user.loading" class="mt-1 w-full py-2.5">
          {{ user.loading ? '…' : mode === 'login' ? 'Sign in' : 'Create account' }}
        </AppButton>
      </form>
    </div>
  </div>
</template>
