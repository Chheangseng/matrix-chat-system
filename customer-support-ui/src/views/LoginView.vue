<script setup lang="ts">
import { ref } from 'vue'
import { Headset, Lock, User, Loader2, ArrowRight } from 'lucide-vue-next'
import { loginSupportStaff, type SupportSession } from '../lib/api'

const emit = defineEmits<{
  (e: 'login-success', session: SupportSession): void
}>()

const username = ref('support')
const password = ref('support_password_123')
const loading = ref(false)
const errorMessage = ref('')

async function handleLogin() {
  if (!username.value.trim() || !password.value.trim()) {
    errorMessage.value = 'Please provide both username and password'
    return
  }

  loading.value = true
  errorMessage.value = ''

  try {
    const session = await loginSupportStaff(username.value.trim(), password.value)
    emit('login-success', session)
  } catch (err: any) {
    errorMessage.value = err.message || 'Failed to authenticate support credentials'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="min-h-screen w-full flex items-center justify-center bg-slate-50 p-4">
    <div class="w-full max-w-md bg-white border border-slate-200 rounded-2xl shadow-xl p-8 space-y-6">
      <!-- Header -->
      <div class="text-center space-y-2">
        <div class="inline-flex p-3 rounded-2xl bg-blue-600 text-white shadow-lg shadow-blue-500/30 mb-2">
          <Headset class="w-8 h-8" />
        </div>
        <h1 class="text-2xl font-bold tracking-tight text-slate-900">Support Desk</h1>
        <p class="text-sm text-slate-500">
          Sign in to your Customer Support agent account
        </p>
      </div>

      <!-- Error Alert -->
      <div
        v-if="errorMessage"
        class="p-3 text-sm text-red-700 bg-red-50 border border-red-200 rounded-xl"
      >
        {{ errorMessage }}
      </div>

      <!-- Form -->
      <form @submit.prevent="handleLogin" class="space-y-4">
        <div class="space-y-1.5">
          <label class="text-xs font-semibold uppercase tracking-wider text-slate-600">Username</label>
          <div class="relative flex items-center">
            <User class="w-5 h-5 absolute left-3 text-slate-400" />
            <input
              v-model="username"
              type="text"
              required
              autocomplete="username"
              placeholder="e.g. support"
              class="w-full pl-10 pr-4 py-2.5 bg-slate-50 border border-slate-200 rounded-xl text-slate-900 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:bg-white transition-all"
            />
          </div>
        </div>

        <div class="space-y-1.5">
          <label class="text-xs font-semibold uppercase tracking-wider text-slate-600">Password</label>
          <div class="relative flex items-center">
            <Lock class="w-5 h-5 absolute left-3 text-slate-400" />
            <input
              v-model="password"
              type="password"
              required
              autocomplete="current-password"
              placeholder="••••••••••••"
              class="w-full pl-10 pr-4 py-2.5 bg-slate-50 border border-slate-200 rounded-xl text-slate-900 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:bg-white transition-all"
            />
          </div>
        </div>

        <button
          type="submit"
          :disabled="loading"
          class="w-full flex items-center justify-center gap-2 py-3 px-4 bg-blue-600 hover:bg-blue-700 text-white font-medium text-sm rounded-xl shadow-md shadow-blue-500/20 disabled:opacity-50 transition-all cursor-pointer"
        >
          <Loader2 v-if="loading" class="w-4 h-4 animate-spin" />
          <span v-else>Sign In to Support Console</span>
          <ArrowRight v-if="!loading" class="w-4 h-4" />
        </button>
      </form>

      <!-- Footer Info -->
      <div class="pt-2 text-center text-xs text-slate-400 border-t border-slate-100">
        Connected securely via Chat API Gateway (Port 8080)
      </div>
    </div>
  </div>
</template>
