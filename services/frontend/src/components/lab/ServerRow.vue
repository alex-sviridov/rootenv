<script setup>
import {
  serverStateConfig,
  serverStatusConfig,
  ClockIcon,
  BoltSlashIcon,
} from '@/config/labStates'

defineProps({
  server: { type: Object, required: true },
})
defineEmits(['open-tab'])

const visibleProtocols = (server) =>
  (server.protocols ?? []).filter(p => p !== 'none')
</script>

<template>
  <div>
    <div class="flex items-center gap-1.5 py-0.5">
      <component
        :is="serverStateConfig[server.state]?.icon ?? ClockIcon"
        class="w-3.5 h-3.5 shrink-0"
        :class="[
          serverStateConfig[server.state]?.iconCls ?? 'text-slate-500',
          serverStateConfig[server.state]?.spin ? 'animate-spin' : '',
        ]"
      />
      <span class="text-xs font-medium text-slate-200 truncate flex-1">{{ server.name }}</span>

      <template v-if="server.state === 'provisioned'">
        <button
            v-for="protocol in visibleProtocols(server)"
            :key="protocol"
            class="text-xs font-medium px-1.5 py-0.5 rounded transition-colors"
            :class="protocol === 'ssh'
              ? 'bg-yellow-500/15 text-yellow-400 hover:bg-yellow-500/25'
              : 'bg-slate-700 text-slate-300 hover:bg-slate-600'"
            @click="$emit('open-tab', { server, protocol })"
          >{{ protocol }}</button>
      </template>

      <span
        v-if="server.state === 'pending' || server.state === 'provisioning'"
        class="text-xs shrink-0"
        :class="serverStateConfig[server.state]?.labelCls ?? 'text-slate-500'"
      >
        {{ serverStateConfig[server.state]?.label ?? server.state }}
      </span>
      <div v-else class="flex items-center gap-1 shrink-0">
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
</template>
