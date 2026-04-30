<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useUserStore } from '@/stores/user'
import FormField from '@/components/ui/FormField.vue'
import AppButton from '@/components/ui/AppButton.vue'

const router = useRouter()
const user = useUserStore()

const oldPassword = ref('')
const password = ref('')
const passwordConfirm = ref('')

async function submit() {
  await user.updatePassword(oldPassword.value, password.value, passwordConfirm.value)
  if (!user.error) router.push('/login')
}
</script>

<template>
  <section class="rounded-xl border border-slate-700/60 bg-slate-800/50 p-6">
    <h2 class="text-sm font-semibold text-slate-300 mb-5">Change password</h2>

    <form class="flex flex-col gap-4" @submit.prevent="submit">
      <FormField v-model="oldPassword" label="Current password" type="password" autocomplete="current-password" />
      <FormField v-model="password" label="New password" type="password" autocomplete="new-password" />
      <FormField v-model="passwordConfirm" label="Confirm new password" type="password" autocomplete="new-password" />

      <p v-if="user.error" class="text-sm text-red-400">{{ user.error }}</p>

      <div class="flex items-center gap-3 pt-1">
        <AppButton type="submit" variant="primary" :loading="user.loading">
          {{ user.loading ? '…' : 'Update password' }}
        </AppButton>
        <AppButton type="button" variant="ghost" @click="router.back()">Cancel</AppButton>
      </div>
    </form>
  </section>
</template>
