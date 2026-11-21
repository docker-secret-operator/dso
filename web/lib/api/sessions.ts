import { apiFetch } from '../api-fetch'

/**
 * Wire shape for GET /api/sessions (see internal/server/rest.go
 * handleSessionsList / sessionView). DSO has a single operator identity --
 * each entry is a distinct logged-in browser/device for that one operator,
 * never a distinct user. The raw session token is never included.
 */
export interface SessionInfo {
  id: string
  username: string
  created_at: string
  last_seen_at: string
  current: boolean
}

export async function fetchSessions(): Promise<SessionInfo[]> {
  const res = await apiFetch('/api/sessions')
  if (!res.ok) {
    throw new Error(`Failed to load sessions (${res.status})`)
  }
  const data = await res.json().catch(() => [])
  return Array.isArray(data) ? (data as SessionInfo[]) : []
}

export async function revokeOtherSessions(): Promise<{ status: string; revoked: number }> {
  const res = await apiFetch('/api/sessions/revoke-others', { method: 'POST' })
  if (!res.ok) {
    throw new Error(`Failed to revoke other sessions (${res.status})`)
  }
  return res.json()
}

/** Sign out one other session by its opaque id (POST /api/sessions/revoke). */
export async function revokeSession(id: string): Promise<{ status: string }> {
  const res = await apiFetch('/api/sessions/revoke', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ id }),
  })
  if (res.status === 404) {
    throw new Error('That session no longer exists')
  }
  if (res.status === 409) {
    throw new Error('The current session cannot be revoked here; use sign out')
  }
  if (!res.ok) {
    throw new Error(`Failed to revoke session (${res.status})`)
  }
  return res.json()
}
