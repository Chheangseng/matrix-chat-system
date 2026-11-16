<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import {
  MessageSquare,
  Search,
  Send,
  Paperclip,
  LogOut,
  User,
  ShieldCheck,
  Download,
  Info,
} from 'lucide-vue-next'
import {
  initMatrixClient,
  stopMatrixClient,
  sendChatMessage,
  sendAttachment,
  sendTypingStatus,
  getMediaHttpUrl,
  type ChatMessage,
  type CustomerConversation,
} from '../lib/matrix'
import type { SupportSession } from '../lib/api'

const props = defineProps<{
  session: SupportSession
}>()

const emit = defineEmits<{
  (e: 'logout'): void
}>()

const conversations = ref<CustomerConversation[]>([])
const selectedRoomId = ref<string | null>(null)
const messages = ref<ChatMessage[]>([])
const messageText = ref('')
const searchQuery = ref('')
const isCustomerTyping = ref(false)
const isSending = ref(false)
const showInfoPanel = ref(true)

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

const activeConversation = computed(() => {
  return conversations.value.find((c) => c.roomId === selectedRoomId.value) || null
})

const filteredConversations = computed(() => {
  const q = searchQuery.value.toLowerCase().trim()
  if (!q) return conversations.value
  return conversations.value.filter(
    (c) =>
      c.customerName.toLowerCase().includes(q) ||
      c.customerUserId.toLowerCase().includes(q) ||
      c.lastMessage.toLowerCase().includes(q)
  )
})

function formatTime(ts: number): string {
  if (!ts) return ''
  const d = new Date(ts)
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

function handleInput() {
  if (selectedRoomId.value) {
    sendTypingStatus(selectedRoomId.value, true)
    clearTimeout(typingTimer)
    typingTimer = setTimeout(() => {
      if (selectedRoomId.value) {
        sendTypingStatus(selectedRoomId.value, false)
      }
    }, 2000)
  }
}

async function handleSendMessage() {
  const text = messageText.value.trim()
  if (!text || !selectedRoomId.value || isSending.value) return

  isSending.value = true
  const targetRoom = selectedRoomId.value
  messageText.value = ''

  try {
    await sendChatMessage(targetRoom, text)
    sendTypingStatus(targetRoom, false)
    loadRoomMessages(targetRoom)
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
  if (!files || files.length === 0 || !selectedRoomId.value) return

  const file = files[0]
  isSending.value = true

  try {
    await sendAttachment(selectedRoomId.value, file)
    loadRoomMessages(selectedRoomId.value)
  } catch (err) {
    console.error('Failed to upload file:', err)
  } finally {
    isSending.value = false
    if (fileInput.value) fileInput.value.value = ''
  }
}

function selectConversation(roomId: string) {
  selectedRoomId.value = roomId
  loadRoomMessages(roomId)
  scrollToBottom()
}

function loadRoomMessages(roomId: string) {
  const client = (window as any).__matrixClient
  if (!client) return

  const room = client.getRoom(roomId)
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
      senderName: room.getMember(sender)?.name || sender,
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

function updateConversationsList() {
  const client = (window as any).__matrixClient
  if (!client) return

  const rooms = client.getVisibleRooms()
  const list: CustomerConversation[] = []

  for (const room of rooms) {
    // Find the customer in this room (the member that is NOT current support agent)
    const members = room.getJoinedMembers()
    const otherMember = members.find((m: any) => m.userId !== props.session.userId)
    const customerUserId = otherMember ? otherMember.userId : room.roomId
    const customerName = otherMember ? otherMember.name : 'Customer'

    const timeline = room.getLiveTimeline().getEvents()
    let lastMsg = 'No messages yet'
    let lastTs = room.getLastActiveTimestamp() || 0

    for (let i = timeline.length - 1; i >= 0; i--) {
      const evt = timeline[i]
      if (evt.getType() === 'm.room.message') {
        lastMsg = evt.getContent().body || '[Attachment]'
        lastTs = evt.getTs()
        break
      }
    }

    list.push({
      roomId: room.roomId,
      customerUserId,
      customerName,
      lastMessage: lastMsg,
      lastTimestamp: lastTs,
      unreadCount: room.getUnreadNotificationCount('total') || 0,
      isTyping: false,
    })
  }

  // Sort by last active timestamp descending
  list.sort((a, b) => b.lastTimestamp - a.lastTimestamp)
  conversations.value = list

  // Default select first conversation if none selected
  if (!selectedRoomId.value && list.length > 0) {
    selectConversation(list[0].roomId)
  }
}

onMounted(() => {
  const client = initMatrixClient(props.session)
  ;(window as any).__matrixClient = client

  client.on('sync' as any, (state: string) => {
    if (state === 'PREPARED' || state === 'SYNCING') {
      updateConversationsList()
      if (selectedRoomId.value) {
        loadRoomMessages(selectedRoomId.value)
      }
    }
  })

  client.on('Room.timeline' as any, (event: any, room: any) => {
    if (event.getType() === 'm.room.message') {
      updateConversationsList()
      if (selectedRoomId.value === room.roomId) {
        loadRoomMessages(room.roomId)
      }
    }
  })

  client.on('RoomMember.typing' as any, (_event: any, member: any) => {
    if (activeConversation.value && member.userId === activeConversation.value.customerUserId) {
      isCustomerTyping.value = member.typing
    }
  })

  client.startClient({ initialSyncLimit: 30 })
})

onUnmounted(() => {
  stopMatrixClient()
})
</script>

<template>
  <div class="h-screen w-screen flex bg-slate-100 overflow-hidden text-slate-800">
    <!-- Left Sidebar: Conversations Inbox -->
    <aside class="w-80 bg-white border-r border-slate-200 flex flex-col shrink-0">
      <!-- Agent Brand Header -->
      <div class="p-4 border-b border-slate-200 flex items-center justify-between">
        <div class="flex items-center gap-3">
          <div class="w-10 h-10 rounded-xl bg-blue-600 flex items-center justify-center text-white shadow-sm font-semibold">
            <ShieldCheck class="w-5 h-5" />
          </div>
          <div>
            <h2 class="text-sm font-bold text-slate-900 leading-none">Support Desk</h2>
            <div class="flex items-center gap-1.5 mt-1">
              <span class="w-2 h-2 rounded-full bg-emerald-500 animate-pulse"></span>
              <span class="text-xs text-slate-500">{{ session.displayName || session.userId }}</span>
            </div>
          </div>
        </div>
      </div>

      <!-- Search Input -->
      <div class="p-3 border-b border-slate-100">
        <div class="relative flex items-center">
          <Search class="w-4 h-4 absolute left-3 text-slate-400" />
          <input
            v-model="searchQuery"
            type="text"
            placeholder="Search conversations..."
            class="w-full pl-9 pr-3 py-1.5 bg-slate-50 border border-slate-200 rounded-lg text-xs focus:outline-none focus:ring-1 focus:ring-blue-500"
          />
        </div>
      </div>

      <!-- Conversation List -->
      <div class="flex-1 overflow-y-auto divide-y divide-slate-100">
        <div
          v-if="filteredConversations.length === 0"
          class="p-8 text-center text-xs text-slate-400 space-y-2"
        >
          <MessageSquare class="w-8 h-8 mx-auto opacity-30" />
          <p>No customer chats active yet</p>
        </div>

        <button
          v-for="conv in filteredConversations"
          :key="conv.roomId"
          @click="selectConversation(conv.roomId)"
          :class="[
            'w-full p-3.5 text-left flex items-start gap-3 transition-colors cursor-pointer',
            selectedRoomId === conv.roomId ? 'bg-blue-50/70 border-l-4 border-blue-600' : 'hover:bg-slate-50'
          ]"
        >
          <!-- Customer Avatar -->
          <div class="w-10 h-10 rounded-full bg-slate-200 flex items-center justify-center text-slate-700 font-semibold shrink-0 text-sm">
            {{ conv.customerName.charAt(0).toUpperCase() }}
          </div>

          <div class="flex-1 min-w-0">
            <div class="flex items-center justify-between mb-1">
              <span class="text-xs font-semibold text-slate-900 truncate">{{ conv.customerName }}</span>
              <span class="text-[10px] text-slate-400 shrink-0">{{ formatTime(conv.lastTimestamp) }}</span>
            </div>
            <p class="text-xs text-slate-500 truncate">{{ conv.lastMessage }}</p>
          </div>

          <span
            v-if="conv.unreadCount > 0"
            class="px-1.5 py-0.5 rounded-full bg-blue-600 text-white text-[10px] font-bold"
          >
            {{ conv.unreadCount }}
          </span>
        </button>
      </div>

      <!-- Sidebar Footer (Agent Info & Logout) -->
      <div class="p-3 border-t border-slate-200 bg-slate-50/50 flex items-center justify-between">
        <div class="flex items-center gap-2 text-xs text-slate-600 truncate">
          <User class="w-3.5 h-3.5 text-slate-400 shrink-0" />
          <span class="truncate font-mono text-[11px]">{{ session.userId }}</span>
        </div>
        <button
          @click="emit('logout')"
          title="Sign Out"
          class="p-1.5 text-slate-500 hover:text-red-600 hover:bg-red-50 rounded-lg transition-colors cursor-pointer"
        >
          <LogOut class="w-4 h-4" />
        </button>
      </div>
    </aside>

    <!-- Center Main Chat Window -->
    <main class="flex-1 flex flex-col bg-white overflow-hidden">
      <!-- Active Chat Header -->
      <header
        v-if="activeConversation"
        class="h-16 px-6 border-b border-slate-200 flex items-center justify-between shrink-0 bg-white"
      >
        <div class="flex items-center gap-3">
          <div class="w-10 h-10 rounded-full bg-blue-100 text-blue-700 flex items-center justify-center font-bold text-sm">
            {{ activeConversation.customerName.charAt(0).toUpperCase() }}
          </div>
          <div>
            <h3 class="text-sm font-semibold text-slate-900 leading-none">{{ activeConversation.customerName }}</h3>
            <p v-if="isCustomerTyping" class="text-xs text-blue-600 animate-pulse mt-1">Customer is typing...</p>
            <p v-else class="text-xs text-slate-400 font-mono mt-1">{{ activeConversation.customerUserId }}</p>
          </div>
        </div>

        <button
          @click="showInfoPanel = !showInfoPanel"
          :class="['p-2 rounded-lg transition-colors cursor-pointer', showInfoPanel ? 'bg-blue-50 text-blue-600' : 'text-slate-400 hover:bg-slate-50']"
          title="Toggle Details"
        >
          <Info class="w-5 h-5" />
        </button>
      </header>

      <!-- Empty State when no conversation selected -->
      <div
        v-if="!activeConversation"
        class="flex-1 flex flex-col items-center justify-center p-8 text-center text-slate-400"
      >
        <MessageSquare class="w-12 h-12 opacity-20 mb-3" />
        <h4 class="text-sm font-semibold text-slate-700">No active conversation</h4>
        <p class="text-xs mt-1">Select a customer from the inbox to start messaging</p>
      </div>

      <!-- Messages Timeline -->
      <div
        v-else
        ref="messagesContainer"
        class="flex-1 overflow-y-auto p-6 space-y-4 bg-slate-50/50"
      >
        <div
          v-for="msg in messages"
          :key="msg.id"
          :class="['flex flex-col', msg.isMe ? 'items-end' : 'items-start']"
        >
          <div class="text-[10px] text-slate-400 mb-1 px-1">
            {{ msg.isMe ? 'You (Support)' : msg.senderName }} • {{ formatTime(msg.timestamp) }}
          </div>

          <!-- Message Bubble -->
          <div
            :class="[
              'max-w-[70%] rounded-2xl px-4 py-2.5 text-sm shadow-sm break-words',
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
                class="rounded-xl max-h-60 object-cover cursor-pointer hover:opacity-95"
              />
            </div>

            <!-- Generic File Download -->
            <div
              v-else-if="msg.msgtype === 'm.file' && msg.mediaUrl"
              class="flex items-center gap-2 mb-1 p-2 rounded-lg bg-black/5"
            >
              <Download class="w-4 h-4 shrink-0" />
              <a :href="msg.mediaUrl" target="_blank" download class="underline text-xs truncate">
                {{ msg.fileName || 'Download File' }}
              </a>
            </div>

            <!-- Text Body -->
            <p class="whitespace-pre-wrap leading-relaxed">{{ msg.body }}</p>
          </div>
        </div>
      </div>

      <!-- Message Composer Footer -->
      <footer
        v-if="activeConversation"
        class="p-4 bg-white border-t border-slate-200 shrink-0"
      >
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
            class="p-2.5 text-slate-500 hover:text-blue-600 hover:bg-slate-100 rounded-xl transition-colors cursor-pointer"
            title="Attach file or photo"
          >
            <Paperclip class="w-5 h-5" />
          </button>

          <!-- Input Field -->
          <input
            v-model="messageText"
            type="text"
            placeholder="Type your response to customer..."
            @input="handleInput"
            @keydown.enter.exact.prevent="handleSendMessage"
            class="flex-1 py-2.5 px-4 bg-slate-50 border border-slate-200 rounded-xl text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:bg-white transition-all"
          />

          <!-- Send Button -->
          <button
            type="submit"
            :disabled="!messageText.trim() || isSending"
            class="p-2.5 bg-blue-600 hover:bg-blue-700 text-white rounded-xl shadow-md shadow-blue-500/20 disabled:opacity-40 transition-all cursor-pointer"
          >
            <Send class="w-5 h-5" />
          </button>
        </form>
      </footer>
    </main>

    <!-- Right Drawer: Customer Profile Info -->
    <aside
      v-if="activeConversation && showInfoPanel"
      class="w-72 bg-white border-l border-slate-200 p-6 flex flex-col shrink-0 overflow-y-auto"
    >
      <div class="text-center pb-6 border-b border-slate-100">
        <div class="w-16 h-16 rounded-full bg-blue-100 text-blue-700 flex items-center justify-center font-bold text-xl mx-auto mb-3">
          {{ activeConversation.customerName.charAt(0).toUpperCase() }}
        </div>
        <h3 class="font-semibold text-slate-900 text-sm">{{ activeConversation.customerName }}</h3>
        <span class="inline-block px-2 py-0.5 mt-1 rounded-full bg-emerald-50 text-emerald-700 text-[10px] font-semibold border border-emerald-200">
          Direct Chat
        </span>
      </div>

      <div class="py-4 space-y-4 text-xs">
        <div>
          <span class="text-slate-400 uppercase font-semibold text-[10px] tracking-wider">User ID</span>
          <p class="font-mono text-slate-700 break-all mt-0.5">{{ activeConversation.customerUserId }}</p>
        </div>

        <div>
          <span class="text-slate-400 uppercase font-semibold text-[10px] tracking-wider">Room ID</span>
          <p class="font-mono text-slate-700 break-all mt-0.5">{{ activeConversation.roomId }}</p>
        </div>

        <div>
          <span class="text-slate-400 uppercase font-semibold text-[10px] tracking-wider">Gateway Proxy</span>
          <p class="text-slate-600 mt-0.5">http://localhost:8080</p>
        </div>
      </div>
    </aside>
  </div>
</template>
