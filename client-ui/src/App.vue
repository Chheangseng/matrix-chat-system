<script setup lang="ts">
import { ref, onMounted } from 'vue'
import LoginView from './views/LoginView.vue'
import SupportChatView from './views/SupportChatView.vue'
import {
  getStoredClientSession,
  saveClientSession,
  clearClientSession,
  type ClientSession,
} from './lib/api'

const session = ref<ClientSession | null>(null)

onMounted(() => {
  // 1. Check if redirected from OIDC flow with auth_token in URL query parameters
  const params = new URLSearchParams(window.location.search)
  const authToken = params.get('auth_token')
  const roomId = params.get('room_id') || ''
  const userId = params.get('user_id') || ''

  if (authToken) {
    const newSession: ClientSession = {
      accessToken: authToken,
      roomId: roomId,
      userId: userId,
    }
    saveClientSession(newSession)
    session.value = newSession

    // Clean URL query parameters
    window.history.replaceState({}, document.title, window.location.pathname)
    return
  }

  // 2. Otherwise load existing session from localStorage
  session.value = getStoredClientSession()
})

function handleLoginSuccess(newSession: ClientSession) {
  session.value = newSession
}

function handleLogout() {
  clearClientSession()
  session.value = null
}
</script>

<template>
  <SupportChatView
    v-if="session"
    :session="session"
    @logout="handleLogout"
  />
  <LoginView
    v-else
    @login-success="handleLoginSuccess"
  />
</template>
