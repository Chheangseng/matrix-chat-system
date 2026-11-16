import * as matrixcs from 'matrix-js-sdk'
import { API_BASE, type ClientSession } from './api'

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

let matrixClient: matrixcs.MatrixClient | null = null

export function initClientMatrix(session: ClientSession): matrixcs.MatrixClient {
  if (matrixClient) {
    matrixClient.stopClient()
  }

  matrixClient = matrixcs.createClient({
    baseUrl: API_BASE,
    accessToken: session.accessToken,
    userId: session.userId,
  })

  return matrixClient
}

export function getClientMatrix(): matrixcs.MatrixClient | null {
  return matrixClient
}

export function stopClientMatrix(): void {
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
