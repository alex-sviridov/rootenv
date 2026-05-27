<script setup>
import { ref } from 'vue'
import { useRouter, RouterLink } from 'vue-router'
import { useUserStore } from '@/stores/user'
import { fetchActiveAttempt } from '@/api/attempts'
import FormField from '@/components/ui/FormField.vue'
import AppButton from '@/components/ui/AppButton.vue'

const router = useRouter()
const user = useUserStore()

const deletePassword = ref('')
const confirmingDelete = ref(false)
const activeAttempt = ref(null)

async function requestDelete() {
  activeAttempt.value = await fetchActiveAttempt()
  if (activeAttempt.value) return
  confirmingDelete.value = true
}

function cancelDelete() {
  confirmingDelete.value = false
  deletePassword.value = ''
  activeAttempt.value = null
}

async function confirmDelete() {
  await user.deleteAccount(deletePassword.value)
  if (!user.error) router.push('/login')
}
</script>

<template>
  <section class="rounded-xl border border-red-900/40 bg-slate-800/50 p-6">
    <h2 class="text-sm font-semibold text-red-400 mb-1">Delete account</h2>
    <p class="text-xs text-slate-400 mb-5">Permanently removes your account and all associated data. This cannot be undone.</p>

    <template v-if="!confirmingDelete">
      <div v-if="activeAttempt" class="mb-4 rounded-lg bg-amber-500/10 border border-amber-500/25 p-3 text-xs text-amber-300">
        You have an active lab session (<RouterLink :to="{ name: 'lab', params: { slug: activeAttempt.lab } }" class="underline hover:text-amber-100">{{ activeAttempt.lab_name }}</RouterLink>). Decommission it before deleting your account.
      </div>
      <div class="flex flex-col gap-4">
        <FormField
          v-model="deletePassword"
          label="Password"
          type="password"
          autocomplete="current-password"
          placeholder="Enter your password to continue"
          focus-border="focus:border-red-500"
        />
        <AppButton variant="danger" :disabled="!deletePassword" @click="requestDelete">
          Delete account
        </AppButton>
      </div>
    </template>

    <template v-else>
      <p class="text-sm text-red-300 mb-4">Are you sure? Your account will be permanently deleted and you will be signed out immediately.</p>
      <p v-if="user.error" class="text-sm text-red-400 mb-3">{{ user.error }}</p>
      <div class="flex items-center gap-3">
        <AppButton variant="danger" :loading="user.loading" @click="confirmDelete">
          {{ user.loading ? '…' : 'Yes, delete my account' }}
        </AppButton>
        <AppButton variant="ghost" @click="cancelDelete">Cancel</AppButton>
      </div>
    </template>
  </section>
</template>
