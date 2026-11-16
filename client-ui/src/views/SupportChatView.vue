<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import {
  Send,
  Paperclip,
  LogOut,
  Download,
  Loader2,
  Headset,
} from 'lucide-vue-next'
import {
  initClientMatrix,
  stopClientMatrix,
  sendChatMessage,
  sendAttachment,
  sendTypingStatus,
  getMediaHttpUrl,
  type ChatMessage,
} from '../lib/matrix'
import { fetchSupportInfo, type ClientSession, type SupportInfo } from '../lib/api'

const props = defineProps<{
  session: ClientSession
}>()

const emit = defineEmits<{
  (e: 'logout'): void
}>()

const supportInfo = ref<SupportInfo | null>(null)
const messages = ref<ChatMessage[]>([])
const messageText = ref('')
const isAgentTyping = ref(false)
const isSending = ref(false)
const activeRoomId = ref<string>(props.session.roomId)

const messagesContainer = ref<HTMLDivElement | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)

let typingTimer: any = null

function scrollToBottom() {
  nextTick(() => {
    if (messagesContainer.value) {
      messagesContainer.value.scrollTop = messagesContainer.value.scrollHeight
    }
  })
}

function formatTime(ts: number): string {
  if (!ts) return ''
  const d = new Date(ts)
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

function handleInput() {
  if (activeRoomId.value) {
    sendTypingStatus(activeRoomId.value, true)
    clearTimeout(typingTimer)
    typingTimer = setTimeout(() => {
      if (activeRoomId.value) {
        sendTypingStatus(activeRoomId.value, false)
      }
    }, 2000)
  }
}

async function handleSendMessage() {
  const text = messageText.value.trim()
  if (!text || !activeRoomId.value || isSending.value) return

  isSending.value = true
  const targetRoom = activeRoomId.value
  messageText.value = ''

  try {
    await sendChatMessage(targetRoom, text)
    sendTypingStatus(targetRoom, false)
    loadMessages()
  } catch (err) {
    console.error('Failed to send message:', err)
  } finally {
    isSending.value = false
  }
}

function triggerFileInput() {
  fileInput.value?.click()
}

async function handleFileUpload(e: Event) {
  const files = (e.target as HTMLInputElement).files
  if (!files || files.length === 0 || !activeRoomId.value) return

  const file = files[0]
  isSending.value = true

  try {
    await sendAttachment(activeRoomId.value, file)
    loadMessages()
  } catch (err) {
    console.error('Failed to upload file:', err)
  } finally {
    isSending.value = false
    if (fileInput.value) fileInput.value.value = ''
  }
}

function loadMessages() {
  const client = (window as any).__matrixClient
  if (!client || !activeRoomId.value) return

  const room = client.getRoom(activeRoomId.value)
  if (!room) return

  const timeline = room.getLiveTimeline().getEvents()
  const parsed: ChatMessage[] = []

  for (const event of timeline) {
    if (event.getType() !== 'm.room.message') continue

    const content = event.getContent()
    const sender = event.getSender()
    const isMe = sender === props.session.userId

    let mediaUrl: string | undefined
    if (content.url) {
      mediaUrl = getMediaHttpUrl(content.url)
    }

    parsed.push({
      id: event.getId() || `${event.getTs()}`,
      sender,
      senderName: isMe ? 'You' : (supportInfo.value?.display_name || 'Customer Support'),
      body: content.body || '',
      timestamp: event.getTs(),
      isMe,
      msgtype: content.msgtype || 'm.text',
      mediaUrl,
      fileName: content.body,
    })
  }

  messages.value = parsed
  scrollToBottom()
}

onMounted(async () => {
  try {
    supportInfo.value = await fetchSupportInfo()
  } catch (err) {
    console.warn('Failed to fetch support profile info:', err)
  }

  const client = initClientMatrix(props.session)
  ;(window as any).__matrixClient = client

  client.on('sync' as any, (state: string) => {
    if (state === 'PREPARED' || state === 'SYNCING') {
      // If roomId wasn't set, find the room
      if (!activeRoomId.value) {
        const rooms = client.getVisibleRooms()
        if (rooms.length > 0) {
          activeRoomId.value = rooms[0].roomId
        }
      }
      loadMessages()
    }
  })

  client.on('Room.timeline' as any, (event: any, room: any) => {
    if (event.getType() === 'm.room.message' && (!activeRoomId.value || room.roomId === activeRoomId.value)) {
      if (!activeRoomId.value) activeRoomId.value = room.roomId
      loadMessages()
    }
  })

  client.on('RoomMember.typing' as any, (_event: any, member: any) => {
    if (member.userId !== props.session.userId) {
      isAgentTyping.value = member.typing
    }
  })

  client.startClient({ initialSyncLimit: 30 })
})

onUnmounted(() => {
  stopClientMatrix()
})
</script>

<template>
  <div class="h-screen w-screen flex items-center justify-center bg-slate-100 p-0 sm:p-4 md:p-6 overflow-hidden">
    <!-- Main Dedicated Support Container -->
    <div class="w-full max-w-4xl h-full sm:h-[92vh] bg-white sm:rounded-3xl shadow-2xl border border-slate-200/80 flex flex-col overflow-hidden">
      <!-- Dedicated Header -->
      <header class="h-18 px-6 bg-white border-b border-slate-100 flex items-center justify-between shrink-0">
        <div class="flex items-center gap-3.5">
          <div class="relative">
            <div class="w-11 h-11 rounded-2xl bg-blue-600 text-white flex items-center justify-center shadow-md shadow-blue-500/20">
              <Headset class="w-6 h-6" />
            </div>
            <span class="absolute -bottom-0.5 -right-0.5 w-3.5 h-3.5 rounded-full bg-emerald-500 border-2 border-white"></span>
          </div>
          <div>
            <div class="flex items-center gap-2">
              <h2 class="text-sm font-bold text-slate-900 leading-none">
                {{ supportInfo?.display_name || 'Customer Support' }}
              </h2>
              <span class="px-2 py-0.5 rounded-full bg-blue-50 text-blue-700 text-[10px] font-semibold border border-blue-200">
                Official Staff
              </span>
            </div>
            <p v-if="isAgentTyping" class="text-xs text-blue-600 animate-pulse mt-1 font-medium">
              Agent is typing a response...
            </p>
            <p v-else class="text-xs text-slate-400 mt-1">
              Direct 1-on-1 Help Desk • Typically replies in seconds
            </p>
          </div>
        </div>

        <button
          @click="emit('logout')"
          class="flex items-center gap-1.5 px-3 py-1.5 rounded-xl text-xs font-medium text-slate-500 hover:text-red-600 hover:bg-red-50 transition-colors cursor-pointer"
          title="Sign Out"
        >
          <LogOut class="w-4 h-4" />
          <span class="hidden sm:inline">Sign Out</span>
        </button>
      </header>

      <!-- Message Timeline -->
      <div
        ref="messagesContainer"
        class="flex-1 overflow-y-auto p-6 space-y-4 bg-slate-50/60"
      >
        <!-- Welcome Banner -->
        <div class="max-w-md mx-auto text-center p-6 rounded-2xl bg-white border border-slate-200/60 shadow-xs mb-6 space-y-2">
          <div class="w-10 h-10 rounded-full bg-blue-50 text-blue-600 flex items-center justify-center mx-auto mb-2">
            <Headset class="w-5 h-5" />
          </div>
          <h3 class="text-xs font-bold text-slate-900 uppercase tracking-wider">Welcome to Support</h3>
          <p class="text-xs text-slate-500 leading-relaxed">
            You are chatting directly with Customer Support. Describe your question or issue, and an agent will assist you promptly.
          </p>
        </div>

        <!-- Messages -->
        <div
          v-for="msg in messages"
          :key="msg.id"
          :class="['flex flex-col', msg.isMe ? 'items-end' : 'items-start']"
        >
          <div class="text-[10px] text-slate-400 mb-1 px-1">
            {{ msg.senderName }} • {{ formatTime(msg.timestamp) }}
          </div>

          <!-- Bubble -->
          <div
            :class="[
              'max-w-[75%] rounded-2xl px-4 py-2.5 text-sm shadow-xs break-words leading-relaxed',
              msg.isMe
                ? 'bg-blue-600 text-white rounded-br-xs'
                : 'bg-white border border-slate-200 text-slate-900 rounded-bl-xs'
            ]"
          >
            <!-- Image Attachment Preview -->
            <div v-if="msg.msgtype === 'm.image' && msg.mediaUrl" class="mb-2">
              <img
                :src="msg.mediaUrl"
                :alt="msg.fileName"
                class="rounded-xl max-h-64 object-cover cursor-pointer hover:opacity-95"
              />
            </div>

            <!-- Generic File Download -->
            <div
              v-else-if="msg.msgtype === 'm.file' && msg.mediaUrl"
              class="flex items-center gap-2 mb-1 p-2 rounded-lg bg-black/5"
            >
              <Download class="w-4 h-4 shrink-0" />
              <a :href="msg.mediaUrl" target="_blank" download class="underline text-xs truncate">
                {{ msg.fileName || 'Download Attachment' }}
              </a>
            </div>

            <!-- Text Content -->
            <p class="whitespace-pre-wrap">{{ msg.body }}</p>
          </div>
        </div>
      </div>

      <!-- Composer Footer -->
      <footer class="p-4 bg-white border-t border-slate-100 shrink-0">
        <form @submit.prevent="handleSendMessage" class="flex items-center gap-2">
          <!-- Hidden File Input -->
          <input
            ref="fileInput"
            type="file"
            class="hidden"
            @change="handleFileUpload"
          />

          <!-- Attachment Button -->
          <button
            type="button"
            @click="triggerFileInput"
            class="p-2.5 text-slate-500 hover:text-blue-600 hover:bg-slate-100 rounded-2xl transition-colors cursor-pointer"
            title="Attach file or screenshot"
          >
            <Paperclip class="w-5 h-5" />
          </button>

          <!-- Text Field -->
          <input
            v-model="messageText"
            type="text"
            placeholder="Type your message to support..."
            @input="handleInput"
            @keydown.enter.exact.prevent="handleSendMessage"
            class="flex-1 py-3 px-4 bg-slate-50 border border-slate-200 rounded-2xl text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:bg-white transition-all"
          />

          <!-- Send Button -->
          <button
            type="submit"
            :disabled="!messageText.trim() || isSending"
            class="p-3 bg-blue-600 hover:bg-blue-700 text-white rounded-2xl shadow-md shadow-blue-500/20 disabled:opacity-40 transition-all cursor-pointer"
          >
            <Loader2 v-if="isSending" class="w-5 h-5 animate-spin" />
            <Send v-else class="w-5 h-5" />
          </button>
        </form>
      </footer>
    </div>
  </div>
</template>
