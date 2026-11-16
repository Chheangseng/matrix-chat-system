export const API_BASE = 'http://localhost:8080'

export interface ClientSession {
  accessToken: string
  userId: string
  roomId: string
  displayName?: string
}

export interface SupportInfo {
  support_user_id: string
  username: string
  display_name: string
  avatar_url: string
}

export async function fetchSupportInfo(): Promise<SupportInfo> {
  const res = await fetch(`${API_BASE}/api-server/support/info`)
  if (!res.ok) {
    throw new Error('Failed to load customer support information')
  }
  return res.json()
}

export async function loginDemoUser(): Promise<ClientSession> {
  // Uses mobile-login endpoint with demo token
  const res = await fetch(`${API_BASE}/api-server/auth/mobile-login`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Accept': 'application/json',
    },
    body: JSON.stringify({ token: 'mock_demo_mobile_token' }),
  })

  if (!res.ok) {
    const err = await res.json().catch(() => ({}))
    throw new Error(err.error || 'Demo login failed')
  }

  const data = await res.json()
  const session: ClientSession = {
    accessToken: data.matrix.access_token,
    userId: data.matrix.user_id,
    roomId: data.support_room_id || data.matrix.support_room_id,
    displayName: data.user?.name || data.matrix.user_id,
  }

  saveClientSession(session)
  return session
}

export function saveClientSession(session: ClientSession): void {
  localStorage.setItem('client_session', JSON.stringify(session))
}

export function getStoredClientSession(): ClientSession | null {
  const raw = localStorage.getItem('client_session')
  if (!raw) return null
  try {
    return JSON.parse(raw)
  } catch {
    return null
  }
}

export function clearClientSession(): void {
  localStorage.removeItem('client_session')
}
