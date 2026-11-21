import { apiFetch } from '../api-fetch'
import type { Event } from '@/hooks/useWebSocket'

/**
 * GET /api/logs -- runtime event log, optionally filtered by severity (see
 * internal/server/rest.go handleLogs, which serves the same EventStore data
 * as /api/events via EventStore.GetLast). Returns `[]` when the in-memory
 * EventStore is empty, never fabricated data.
 */
export async function fetchLogs(limit = 100, severity?: string): Promise<Event[]> {
  const params = new URLSearchParams({ limit: String(limit) })
  if (severity) params.set('severity', severity)
  const res = await apiFetch(`/api/logs?${params.toString()}`)
  if (!res.ok) {
    throw new Error(`Failed to load logs (${res.status})`)
  }
  const data = await res.json().catch(() => [])
  return Array.isArray(data) ? (data as Event[]) : []
}
