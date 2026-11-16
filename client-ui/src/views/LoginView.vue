<script setup lang="ts">
import { ref } from 'vue'
import { MessageSquareText, Shield, Sparkles, ArrowRight, Loader2 } from 'lucide-vue-next'
import { API_BASE, loginDemoUser, type ClientSession } from '../lib/api'

const emit = defineEmits<{
  (e: 'login-success', session: ClientSession): void
}>()

const loadingDemo = ref(false)
const errorMessage = ref('')

function handleOIDCLogin() {
  window.location.href = `${API_BASE}/api-server/auth/login`
}

async function handleDemoLogin() {
  loadingDemo.value = true
  errorMessage.value = ''
  try {
    const session = await loginDemoUser()
    emit('login-success', session)
  } catch (err: any) {
    errorMessage.value = err.message || 'Demo login failed'
  } finally {
    loadingDemo.value = false
  }
}
</script>

<template>
  <div class="min-h-screen w-full flex items-center justify-center bg-slate-50 p-4">
    <div class="w-full max-w-md bg-white border border-slate-200 rounded-3xl shadow-xl p-8 space-y-6">
      <!-- Icon & Header -->
      <div class="text-center space-y-3">
        <div class="inline-flex p-4 rounded-3xl bg-blue-600 text-white shadow-lg shadow-blue-500/25">
          <MessageSquareText class="w-10 h-10" />
        </div>
        <h1 class="text-2xl font-bold tracking-tight text-slate-900">Customer Support</h1>
        <p class="text-sm text-slate-500 leading-relaxed">
          Need help? Connect directly with our support team in real-time.
        </p>
      </div>

      <!-- Error message -->
      <div
        v-if="errorMessage"
        class="p-3 text-sm text-red-700 bg-red-50 border border-red-200 rounded-xl"
      >
        {{ errorMessage }}
      </div>

      <!-- Action Buttons -->
      <div class="space-y-3 pt-2">
        <!-- OIDC Login Button -->
        <button
          type="button"
          @click="handleOIDCLogin"
          class="w-full flex items-center justify-center gap-2 py-3.5 px-4 bg-blue-600 hover:bg-blue-700 text-white font-medium text-sm rounded-2xl shadow-md shadow-blue-500/25 transition-all cursor-pointer"
        >
          <span>Sign In with HTP Account</span>
          <ArrowRight class="w-4 h-4" />
        </button>

        <!-- Demo Login Button -->
        <button
          type="button"
          :disabled="loadingDemo"
          @click="handleDemoLogin"
          class="w-full flex items-center justify-center gap-2 py-3 px-4 bg-slate-100 hover:bg-slate-200 text-slate-700 font-medium text-xs rounded-2xl transition-all cursor-pointer"
        >
          <Loader2 v-if="loadingDemo" class="w-4 h-4 animate-spin" />
          <Sparkles v-else class="w-4 h-4 text-amber-500" />
          <span>Quick Demo Customer Login</span>
        </button>
      </div>

      <!-- Guarantee Badges -->
      <div class="pt-4 border-t border-slate-100 grid grid-cols-2 gap-3 text-center text-xs text-slate-500">
        <div class="flex items-center justify-center gap-1.5 p-2 rounded-xl bg-slate-50">
          <Shield class="w-3.5 h-3.5 text-blue-600" />
          <span>Encrypted Chat</span>
        </div>
        <div class="flex items-center justify-center gap-1.5 p-2 rounded-xl bg-slate-50">
          <span class="w-2 h-2 rounded-full bg-emerald-500"></span>
          <span>Live Support</span>
        </div>
      </div>
    </div>
  </div>
</template>
