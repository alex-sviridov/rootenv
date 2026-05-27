<script setup>
import { onMounted } from 'vue'
import { useBreadcrumbsStore } from '@/stores/breadcrumbs'
import ChangePasswordForm from '@/components/account/ChangePasswordForm.vue'
import DeleteAccountSection from '@/components/account/DeleteAccountSection.vue'

const breadcrumbs = useBreadcrumbsStore()
onMounted(() => breadcrumbs.set([{ label: 'LinuxLab', to: '/' }, { label: 'Account' }]))
</script>

<template>
  <div class="flex items-center justify-center min-h-full py-16 px-4">
    <div class="w-full max-w-md flex flex-col gap-6">
      <ChangePasswordForm />
      <DeleteAccountSection />
    </div>
  </div>
</template>
