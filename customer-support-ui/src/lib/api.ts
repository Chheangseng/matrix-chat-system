export const API_BASE = 'http://localhost:8080'

export interface SupportSession {
  accessToken: string
  userId: string
  deviceId: string
  homeServer: string
  displayName?: string
  avatarUrl?: string
}

export interface SupportLoginResponse {
  matrix: {
    access_token: string
    device_id: string
    home_server: string
    user_id: string
  }
  user: {
    username: string
    user_id: string
    display_name: string
    avatar_url: string
    role: string
  }
}

export async function loginSupportStaff(username: string, password: string): Promise<SupportSession> {
  const response = await fetch(`${API_BASE}/api-server/auth/support-login`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Accept': 'application/json',
    },
    body: JSON.stringify({ username, password }),
  })

  if (!response.ok) {
    const errData = await response.json().catch(() => ({}))
    throw new Error(errData.error || `Login failed with status ${response.status}`)
  }

  const data: SupportLoginResponse = await response.json()
  const session: SupportSession = {
    accessToken: data.matrix.access_token,
    userId: data.matrix.user_id,
    deviceId: data.matrix.device_id,
    homeServer: data.matrix.home_server,
    displayName: data.user.display_name,
    avatarUrl: data.user.avatar_url,
  }

  localStorage.setItem('support_session', JSON.stringify(session))
  return session
}

export function getStoredSupportSession(): SupportSession | null {
  const raw = localStorage.getItem('support_session')
  if (!raw) return null
  try {
    return JSON.parse(raw)
  } catch {
    return null
  }
}

export function clearStoredSupportSession(): void {
  localStorage.removeItem('support_session')
}
