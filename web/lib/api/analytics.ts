import { apiFetch } from '../api-fetch'
import type { Event } from '@/hooks/useWebSocket'

/**
 * Wire shape for GET /api/analytics (see internal/server/rest.go
 * handleAnalytics). Every "current" field is nullable: null means the
 * underlying source (Cache/Reloader) is unavailable, NOT zero -- never
 * treat null and 0 as the same thing. since_restart counters are
 * process-lifetime-only (pkg/observability/counters.go) and reset on
 * every agent restart -- always render them with the accompanying `note`,
 * never as an all-time total.
 */
export interface AnalyticsCurrentState {
  managed_secrets: number | null
  containers_targeted: number | null
  drifted: number | null
  degraded: number | null
}

export interface AnalyticsCounters {
  rotation_success_total: number
  rotation_failure_total: number
  injection_success_total: number
  injection_failure_total: number
  note: string
}

export interface AnalyticsRecentActivity {
  successful_rotations: Event[]
  failed_rotations: Event[]
  injection_failures: Event[]
  recent_failures: Event[]
}

export interface DegradedServiceEntry {
  service: string
  reason: string
}

export interface AnalyticsResponse {
  current: AnalyticsCurrentState
  since_restart: AnalyticsCounters
  recent_activity: AnalyticsRecentActivity
  degraded_services: DegradedServiceEntry[]
}

export async function fetchAnalytics(): Promise<AnalyticsResponse> {
  const res = await apiFetch('/api/analytics')
  if (!res.ok) {
    throw new Error(`Failed to load analytics (${res.status})`)
  }
  return res.json()
}
