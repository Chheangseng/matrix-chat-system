import * as matrixcs from 'matrix-js-sdk'
import { API_BASE, type SupportSession } from './api'

export interface ChatMessage {
  id: string
  sender: string
  senderName: string
  body: string
  timestamp: number
  isMe: boolean
  msgtype: string
  mediaUrl?: string
  fileName?: string
}

export interface CustomerConversation {
  roomId: string
  customerUserId: string
  customerName: string
  customerAvatar?: string
  lastMessage: string
  lastTimestamp: number
  unreadCount: number
  isTyping: boolean
}

let matrixClient: matrixcs.MatrixClient | null = null

export function initMatrixClient(session: SupportSession): matrixcs.MatrixClient {
  if (matrixClient) {
    matrixClient.stopClient()
  }

  matrixClient = matrixcs.createClient({
    baseUrl: API_BASE,
    accessToken: session.accessToken,
    userId: session.userId,
    deviceId: session.deviceId,
  })

  return matrixClient
}

export function getMatrixClient(): matrixcs.MatrixClient | null {
  return matrixClient
}

export function stopMatrixClient(): void {
  if (matrixClient) {
    matrixClient.stopClient()
    matrixClient = null
  }
}

export function getMediaHttpUrl(mxcUrl?: string): string | undefined {
  if (!mxcUrl || !matrixClient) return undefined
  return matrixClient.mxcUrlToHttp(mxcUrl, 800, 800, 'scale', true) || undefined
}

export async function sendChatMessage(roomId: string, text: string): Promise<void> {
  if (!matrixClient) throw new Error('Matrix client not initialized')
  await matrixClient.sendTextMessage(roomId, text)
}

export async function sendAttachment(roomId: string, file: File): Promise<void> {
  if (!matrixClient) throw new Error('Matrix client not initialized')
  
  const uploadRes = await matrixClient.uploadContent(file, {
    name: file.name,
    type: file.type,
  })

  const mxcUrl = uploadRes.content_uri
  const isImage = file.type.startsWith('image/')

  await matrixClient.sendMessage(roomId, {
    msgtype: isImage ? matrixcs.MsgType.Image : matrixcs.MsgType.File,
    body: file.name,
    url: mxcUrl,
    info: {
      mimetype: file.type,
      size: file.size,
    },
  } as any)
}

export async function sendTypingStatus(roomId: string, isTyping: boolean): Promise<void> {
  if (!matrixClient) return
  await matrixClient.sendTyping(roomId, isTyping, isTyping ? 3000 : 0)
}
