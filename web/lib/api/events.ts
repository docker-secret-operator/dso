import { apiFetch } from '../api-fetch'
import type { Event } from '@/hooks/useWebSocket'

/**
 * GET /api/events -- initial event history load (see
 * internal/server/rest.go handleEvents). Returns `[]` when the in-memory
 * EventStore is empty (fresh startup), never fabricated data.
 */
export async function fetchEvents(limit = 50): Promise<Event[]> {
  const res = await apiFetch(`/api/events?limit=${encodeURIComponent(String(limit))}`)
  if (!res.ok) {
    throw new Error(`Failed to load events (${res.status})`)
  }
  const data = await res.json().catch(() => [])
  return Array.isArray(data) ? (data as Event[]) : []
}
