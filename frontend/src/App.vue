<script setup>
import { RouterView } from 'vue-router'
import AppHeader from '@/components/ui/AppHeader.vue'
</script>

<template>
  <div class="flex flex-col min-h-screen bg-slate-950">
    <AppHeader />
    <main class="flex-1 overflow-y-auto">
      <RouterView />
    </main>
  </div>
</template>
