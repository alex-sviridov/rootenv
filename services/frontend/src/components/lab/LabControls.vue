<script setup>
import { computed } from 'vue'
import { ArrowPathIcon } from '@heroicons/vue/24/outline'
import { ExclamationTriangleIcon } from '@/config/labStates'
import { useAttemptsStore } from '@/stores/attempts'
import { useLabTimer } from '@/composables/useLabTimer'
import LabSessionHeader from '@/components/lab/LabSessionHeader.vue'
import ServerRow from '@/components/lab/ServerRow.vue'

const props = defineProps({
  labId: { type: String, required: true },
  labName: { type: String, required: true },
})
const emit = defineEmits(['open-tab'])

const attempts = useAttemptsStore()

const attemptState = computed(() => attempts.lastAttempt?.current_state ?? null)

const runningAttempt = computed(() => {
  const a = attempts.lastAttempt
  return a && attemptState.value !== 'decommissioned' ? a : null
})

const anotherLabRunning = computed(() => {
  const a = attempts.activeAttempt
  const last = attempts.lastAttempt
  return a && a.lab !== last?.lab ? a : null
})

const canProvision = computed(() =>
  !attempts.lastAttempt || attemptState.value === 'decommissioned'
)

const canDecommission = computed(() =>
  runningAttempt.value &&
  attemptState.value !== 'decommissioning' &&
  !anotherLabRunning.value &&
  !attempts.loading
)

const { expiresIn } = useLabTimer(computed(() => runningAttempt.value?.expires_at ?? null))

async function refresh() {
  await Promise.all([
    attempts.loadLastAttempt(props.labId),
    attempts.loadActiveAttempt(),
  ])
}
</script>

<template>
  <div class="shrink-0 border-t border-slate-800">

    <LabSessionHeader
      :state="attemptState"
      :expires-in="expiresIn"
      :loading="attempts.loading"
      @refresh="refresh"
    />

    <div class="px-4 py-2 space-y-3">

      <!-- Another lab is running -->
      <div v-if="anotherLabRunning" class="rounded-lg bg-amber-500/10 border border-amber-500/25 p-3">
        <div class="flex items-center gap-2 mb-2">
          <ExclamationTriangleIcon class="w-3.5 h-3.5 text-amber-400 shrink-0" />
          <span class="text-xs font-medium text-amber-300">Another lab is active</span>
        </div>
        <a
          class="block w-full text-left text-xs text-slate-400 hover:text-indigo-300 transition-colors truncate"
          :href="`/labs/${anotherLabRunning.lab.replace('_', '/')}`"
        >→ {{ anotherLabRunning.lab_name }}</a>
      </div>

      <!-- Active servers list -->
      <div v-if="runningAttempt && attempts.servers.length" class="space-y-1 pb-1">
        <ServerRow
          v-for="server in attempts.servers"
          :key="server.name"
          :server="server"
          @open-tab="emit('open-tab', $event)"
        />
      </div>

      <div v-if="attempts.error" class="text-xs text-red-400 px-1">{{ attempts.error }}</div>

      <button
        v-if="canProvision"
        class="w-full py-2 px-3 rounded-lg text-xs font-semibold bg-indigo-600 hover:bg-indigo-500 text-white transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
        :disabled="attempts.loading || !!anotherLabRunning"
        @click="attempts.addAttempt(labId, labName)"
      >
        <span v-if="attempts.loading" class="flex items-center justify-center gap-1.5">
          <ArrowPathIcon class="w-3.5 h-3.5 animate-spin" />
          Working…
        </span>
        <span v-else>Provision Lab</span>
      </button>

      <button
        v-if="canDecommission"
        class="w-full py-2 px-3 rounded-lg text-xs font-semibold text-slate-400 hover:text-red-300 hover:bg-red-500/10 border border-slate-700 hover:border-red-500/30 transition-colors disabled:opacity-50"
        :disabled="attempts.loading"
        @click="attempts.removeAttempt()"
      >
        <span v-if="attempts.loading" class="flex items-center justify-center gap-1.5">
          <ArrowPathIcon class="w-3.5 h-3.5 animate-spin" />
          Working…
        </span>
        <span v-else>Decommission</span>
      </button>

    </div>
  </div>
</template>
