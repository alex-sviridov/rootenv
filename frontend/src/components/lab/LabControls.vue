<script setup>
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useAttemptsStore } from '@/stores/attempts'
import { useServersStore } from '@/stores/servers'
import { ArrowPathIcon } from '@heroicons/vue/24/outline'
import {
  attemptConfig,
  serverStateConfig,
  serverStatusConfig,
  ClockIcon,
  BoltSlashIcon,
  ExclamationTriangleIcon,
} from '@/config/labStates'

const props = defineProps({
  labId: { type: String, required: true },
  labName: { type: String, required: true },
})

const emit = defineEmits(['open-terminal'])

const router = useRouter()
const attempts = useAttemptsStore()
const serversStore = useServersStore()

const anotherLabRunning = computed(() => {
  const a = attempts.activeAttempt
  const last = attempts.lastAttempt
  return a && a.id !== last?.id ? a : null
})

const activeAttempt = computed(() => {
  const a = attempts.lastAttempt
  return a && a.state !== 'decommissioned' ? a : null
})

const canDecommission = computed(() =>
  activeAttempt.value &&
  activeAttempt.value.state !== 'decommissioning' &&
  !anotherLabRunning.value &&
  !attempts.loading
)

async function refresh() {
  await Promise.all([
    attempts.loadLastAttempt(props.labId),
    attempts.loadActiveAttempt(),
  ])
}

function provision() {
  attempts.addAttempt(props.labId, props.labName)
}

function decommission() {
  attempts.removeAttempt(serversStore.servers.map(s => s.id))
}

function goToLab(attempt) {
  router.push({ name: 'lab', params: { slug: attempt.lab } })
}
</script>

<template>
  <div class="shrink-0 border-t border-slate-800">

    <div class="px-4 py-3 border-b border-slate-800 flex items-center justify-between gap-2">
      <p class="text-xs font-semibold text-slate-500 uppercase tracking-widest shrink-0">Lab Session</p>
      <div v-if="activeAttempt" class="flex items-center gap-1.5 min-w-0">
        <span class="relative flex h-2 w-2 shrink-0">
          <span
            v-if="attemptConfig[activeAttempt.state]?.ping"
            class="absolute inline-flex h-full w-full rounded-full opacity-60 animate-ping"
            :class="attemptConfig[activeAttempt.state]?.dot"
          />
          <span class="relative inline-flex rounded-full h-2 w-2" :class="attemptConfig[activeAttempt.state]?.dot ?? 'bg-slate-400'" />
        </span>
        <span class="text-xs truncate" :class="attemptConfig[activeAttempt.state]?.text ?? 'text-slate-400'">
          {{ attemptConfig[activeAttempt.state]?.label ?? activeAttempt.state }}
        </span>
      </div>
      <button
        class="text-slate-600 hover:text-slate-300 transition-colors ml-auto shrink-0"
        :class="attempts.loading || serversStore.loading ? 'animate-spin pointer-events-none' : ''"
        @click="refresh"
      >
        <ArrowPathIcon class="w-3.5 h-3.5" />
      </button>
    </div>

    <div class="px-4 py-2 space-y-3">

    <!-- Another lab is running -->
    <div
      v-if="anotherLabRunning"
      class="rounded-lg bg-amber-500/10 border border-amber-500/25 p-3"
    >
      <div class="flex items-center gap-2 mb-2">
        <ExclamationTriangleIcon class="w-3.5 h-3.5 text-amber-400 shrink-0" />
        <span class="text-xs font-medium text-amber-300">Another lab is active</span>
      </div>
      <button
        class="w-full text-left text-xs text-slate-400 hover:text-indigo-300 transition-colors truncate"
        @click="goToLab(anotherLabRunning)"
      >
        → {{ anotherLabRunning.lab_name }}
      </button>
    </div>

    <!-- Active attempt panel -->
    <div v-if="activeAttempt" class="space-y-2">

      <!-- Servers list -->
      <div v-if="serversStore.servers.length" class="space-y-1 pb-1">
        <div
          v-for="server in serversStore.servers"
          :key="server.id"
          class="flex items-center gap-1.5 py-0.5"
        >
          <!-- Lifecycle state icon -->
          <component
            :is="serverStateConfig[server.state]?.icon ?? ClockIcon"
            class="w-3.5 h-3.5 shrink-0"
            :class="[
              serverStateConfig[server.state]?.iconCls ?? 'text-slate-500',
              serverStateConfig[server.state]?.spin ? 'animate-spin' : '',
            ]"
          />
          <!-- Name -->
          <button
            v-if="server.state === 'provisioned'"
            class="text-xs font-medium text-slate-200 truncate flex-1 text-left hover:text-indigo-300 transition-colors"
            @click="emit('open-terminal', server)"
          >{{ server.name }}</button>
          <span v-else class="text-xs font-medium text-slate-200 truncate flex-1">{{ server.name }}</span>
          <!-- Provision state label (pending / provisioning) -->
          <span
            v-if="server.state === 'pending' || server.state === 'provisioning'"
            class="text-xs shrink-0"
            :class="serverStateConfig[server.state]?.labelCls ?? 'text-slate-500'"
          >
            {{ serverStateConfig[server.state]?.label ?? server.state }}
          </span>
          <!-- Power status (provisioned or decommissioning) -->
          <div
            v-else
            class="flex items-center gap-1 shrink-0"
          >
            <component
              :is="serverStatusConfig[server.status]?.icon ?? BoltSlashIcon"
              class="w-3.5 h-3.5 shrink-0"
              :class="[
                serverStatusConfig[server.status]?.iconCls ?? 'text-slate-500',
                serverStatusConfig[server.status]?.spin ? 'animate-spin' : '',
              ]"
            />
            <span class="text-xs" :class="serverStatusConfig[server.status]?.labelCls ?? 'text-slate-500'">
              {{ serverStatusConfig[server.status]?.label ?? server.status }}
            </span>
          </div>
        </div>
      </div>

      <!-- Loading servers -->
      <div v-else-if="serversStore.loading" class="flex items-center gap-2 text-xs text-slate-500">
        <ArrowPathIcon class="w-3.5 h-3.5 animate-spin" />
        Loading servers…
      </div>

    </div>

    <!-- Error -->
    <div v-if="attempts.error" class="text-xs text-red-400 px-1">{{ attempts.error }}</div>

    <!-- Provision button -->
    <button
      v-if="attempts.canProvision"
      class="w-full py-2 px-3 rounded-lg text-xs font-semibold bg-indigo-600 hover:bg-indigo-500 text-white transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
      :disabled="attempts.loading || !!anotherLabRunning"
      @click="provision"
    >
      <span v-if="attempts.loading" class="flex items-center justify-center gap-1.5">
        <ArrowPathIcon class="w-3.5 h-3.5 animate-spin" />
        Working…
      </span>
      <span v-else>Provision Lab</span>
    </button>

    <!-- Decommission button -->
    <button
      v-if="canDecommission"
      class="w-full py-2 px-3 rounded-lg text-xs font-semibold text-slate-400 hover:text-red-300 hover:bg-red-500/10 border border-slate-700 hover:border-red-500/30 transition-colors disabled:opacity-50"
      :disabled="attempts.loading"
      @click="decommission"
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
