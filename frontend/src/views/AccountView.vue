<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useUserStore } from '@/stores/user'
import { useBreadcrumbsStore } from '@/stores/breadcrumbs'
import { fetchActiveAttempt } from '@/api/attempts'

const router = useRouter()
const user = useUserStore()
const breadcrumbs = useBreadcrumbsStore()

onMounted(() => breadcrumbs.set([{ label: 'LinuxLab', to: '/' }, { label: 'Account' }]))

const oldPassword = ref('')
const password = ref('')
const passwordConfirm = ref('')

async function submit() {
  await user.updatePassword(oldPassword.value, password.value, passwordConfirm.value)
  if (!user.error) router.push('/login')
}

const deletePassword = ref('')
const confirmingDelete = ref(false)
const activeAttemptForDelete = ref(null)

async function requestDelete() {
  activeAttemptForDelete.value = await fetchActiveAttempt()
  if (activeAttemptForDelete.value) return
  confirmingDelete.value = true
}

function cancelDelete() {
  confirmingDelete.value = false
  deletePassword.value = ''
  activeAttemptForDelete.value = null
}

async function confirmDelete() {
  await user.deleteAccount(deletePassword.value)
  if (!user.error) router.push('/login')
}
</script>

<template>
  <div class="flex items-center justify-center min-h-full py-16 px-4">
    <div class="w-full max-w-md flex flex-col gap-6">
      <section class="rounded-xl border border-slate-700/60 bg-slate-800/50 p-6">
        <h2 class="text-sm font-semibold text-slate-300 mb-5">Change password</h2>

        <form class="flex flex-col gap-4" @submit.prevent="submit">
          <div class="flex flex-col gap-1.5">
            <label class="text-xs font-medium text-slate-400 uppercase tracking-wide">Current password</label>
            <input
              v-model="oldPassword"
              type="password"
              autocomplete="current-password"
              class="bg-slate-900 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-white placeholder-slate-500 focus:outline-none focus:border-indigo-500 transition-colors"
            />
          </div>

          <div class="flex flex-col gap-1.5">
            <label class="text-xs font-medium text-slate-400 uppercase tracking-wide">New password</label>
            <input
              v-model="password"
              type="password"
              autocomplete="new-password"
              class="bg-slate-900 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-white placeholder-slate-500 focus:outline-none focus:border-indigo-500 transition-colors"
            />
          </div>

          <div class="flex flex-col gap-1.5">
            <label class="text-xs font-medium text-slate-400 uppercase tracking-wide">Confirm new password</label>
            <input
              v-model="passwordConfirm"
              type="password"
              autocomplete="new-password"
              class="bg-slate-900 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-white placeholder-slate-500 focus:outline-none focus:border-indigo-500 transition-colors"
            />
          </div>

          <p v-if="user.error" class="text-sm text-red-400">{{ user.error }}</p>

          <div class="flex items-center gap-3 pt-1">
            <button
              type="submit"
              :disabled="user.loading"
              class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 disabled:cursor-not-allowed text-white text-sm font-semibold px-4 py-2 rounded-lg transition-colors"
            >
              {{ user.loading ? '…' : 'Update password' }}
            </button>
            <button
              type="button"
              class="text-sm text-slate-400 hover:text-white transition-colors"
              @click="router.back()"
            >Cancel</button>
          </div>
        </form>
      </section>

      <section class="rounded-xl border border-red-900/40 bg-slate-800/50 p-6">
        <h2 class="text-sm font-semibold text-red-400 mb-1">Delete account</h2>
        <p class="text-xs text-slate-400 mb-5">Permanently removes your account and all associated data. This cannot be undone.</p>

        <template v-if="!confirmingDelete">
          <div v-if="activeAttemptForDelete" class="mb-4 rounded-lg bg-amber-500/10 border border-amber-500/25 p-3 text-xs text-amber-300">
            You have an active lab session (<RouterLink :to="{ name: 'lab', params: { slug: activeAttemptForDelete.lab } }" class="underline hover:text-amber-100">{{ activeAttemptForDelete.lab_name }}</RouterLink>). Decommission it before deleting your account.
          </div>
          <div class="flex flex-col gap-1.5 mb-4">
            <label class="text-xs font-medium text-slate-400 uppercase tracking-wide">Password</label>
            <input
              v-model="deletePassword"
              type="password"
              autocomplete="current-password"
              placeholder="Enter your password to continue"
              class="bg-slate-900 border border-slate-700 rounded-lg px-3 py-2.5 text-sm text-white placeholder-slate-500 focus:outline-none focus:border-red-500 transition-colors"
            />
          </div>
          <button
            type="button"
            :disabled="!deletePassword"
            class="bg-red-700 hover:bg-red-600 disabled:opacity-40 disabled:cursor-not-allowed text-white text-sm font-semibold px-4 py-2 rounded-lg transition-colors"
            @click="requestDelete"
          >Delete account</button>
        </template>

        <template v-else>
          <p class="text-sm text-red-300 mb-4">Are you sure? Your account will be permanently deleted and you will be signed out immediately.</p>
          <p v-if="user.error" class="text-sm text-red-400 mb-3">{{ user.error }}</p>
          <div class="flex items-center gap-3">
            <button
              type="button"
              :disabled="user.loading"
              class="bg-red-700 hover:bg-red-600 disabled:opacity-50 disabled:cursor-not-allowed text-white text-sm font-semibold px-4 py-2 rounded-lg transition-colors"
              @click="confirmDelete"
            >{{ user.loading ? '…' : 'Yes, delete my account' }}</button>
            <button
              type="button"
              class="text-sm text-slate-400 hover:text-white transition-colors"
              @click="cancelDelete"
            >Cancel</button>
          </div>
        </template>
      </section>
    </div>
  </div>
</template>
