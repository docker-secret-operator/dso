import { apiFetch } from '../api-fetch'
import type { Event } from '@/hooks/useWebSocket'

/**
 * GET /api/audit -- the "Audit History" view of the same in-memory
 * EventStore that backs /api/events and /api/logs (see
 * internal/server/rest.go handleAudit). DSO has no separate persistent
 * audit-events table; this is a bounded, in-memory, non-persistent-across-
 * restart log, not a durable audit trail. Returns `[]` when the store is
 * empty, never fabricated data.
 */
export async function fetchAudit(limit = 200, severity?: string): Promise<Event[]> {
  const params = new URLSearchParams({ limit: String(limit) })
  if (severity) params.set('severity', severity)
  const res = await apiFetch(`/api/audit?${params.toString()}`)
  if (!res.ok) {
    throw new Error(`Failed to load audit history (${res.status})`)
  }
  const data = await res.json().catch(() => [])
  return Array.isArray(data) ? (data as Event[]) : []
}
