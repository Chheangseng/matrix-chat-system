<script setup lang="ts">
import { ref, onMounted } from 'vue'
import LoginView from './views/LoginView.vue'
import ChatDashboard from './views/ChatDashboard.vue'
import {
  getStoredSupportSession,
  clearStoredSupportSession,
  type SupportSession,
} from './lib/api'

const session = ref<SupportSession | null>(null)

onMounted(() => {
  session.value = getStoredSupportSession()
})

function handleLoginSuccess(newSession: SupportSession) {
  session.value = newSession
}

function handleLogout() {
  clearStoredSupportSession()
  session.value = null
}
</script>

<template>
  <ChatDashboard
    v-if="session"
    :session="session"
    @logout="handleLogout"
  />
  <LoginView
    v-else
    @login-success="handleLoginSuccess"
  />
</template>
