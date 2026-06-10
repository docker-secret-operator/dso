import axios, { AxiosInstance, AxiosError } from 'axios'

// API Base URL - use relative path to proxy through dashboard server
// The dashboard server (default :8472) proxies /api/* to the REST API (:8471)
const API_BASE_URL = typeof window !== 'undefined'
  ? window.location.origin  // Use same origin for proxied API calls
  : 'http://127.0.0.1:8472' // Default to dashboard server for SSR

// ============================================================================
// Type Definitions
// ============================================================================

export interface Health {
  status: 'up' | 'down'
  version?: string
  uptime?: number
  timestamp?: string
}

export interface Secret {
  name: string
  provider: string
  last_rotated?: string
  next_rotation?: string
  status: 'ok' | 'pending' | 'error'
  version?: string
  container_count?: number
  size?: number
  age?: number
  rotation_strategy?: 'rolling' | 'restart' | 'signal' | 'none'
}

export interface Event {
  timestamp: string
  action: string
  secret_name?: string
  provider?: string
  container_id?: string
  container_name?: string
  status?: 'success' | 'failure' | 'pending'
  duration_ms?: number
  error?: string
  severity: 'info' | 'warning' | 'error'
  message: string
}

export interface User {
  id: string
  username: string
  display_name: string
  role: string
  disabled: boolean
  locked: boolean
  locked_until?: string
  must_change_password: boolean
  created_at: string
  updated_at: string
}

export interface Session {
  id: string
  user_id: string
  ip_address: string
  user_agent: string
  created_at: string
  expires_at: string
  last_activity: string
  is_current?: boolean
}

export interface LogEntry {
  timestamp: string
  level: 'debug' | 'info' | 'warning' | 'error'
  message: string
  context?: Record<string, unknown>
  secret_name?: string
  provider?: string
}

export interface APIError extends AxiosError {
  message: string
  status?: number
}

// ── Audit explorer types ──────────────────────────────────────────────────────

export interface AuditEvent {
  id: string
  correlation_id: string
  execution_id: string
  action: string
  actor: string
  actor_id: string
  actor_email: string
  resource: string
  resource_id: string
  resource_type: string
  status: string
  severity: 'info' | 'warning' | 'error' | 'critical'
  details: string
  ip_address: string
  timestamp: string
}

export interface AuditListResponse {
  total: number
  count: number
  offset: number
  limit: number
  events: AuditEvent[]
  timestamp: string
}

export interface CorrelationChainResponse {
  correlation_id: string
  count: number
  events: AuditEvent[]
  timestamp: string
}

export interface ActorTimelineResponse {
  actor_id: string
  actor_name: string
  period: string
  count: number
  events: AuditEvent[]
  timestamp: string
}

export interface JourneyStep {
  step: string
  action: string
  status: string
  actor: string
  actor_id: string
  correlation_id: string
  details: string
  timestamp: string
}

export interface ExecutionJourneyResponse {
  execution_id: string
  correlation_id: string
  total_steps: number
  duration_ms: number
  steps: JourneyStep[]
  timestamp: string
}

export interface AuditFilters {
  correlation_id?: string
  execution_id?: string
  action?: string
  actor?: string
  actor_id?: string
  resource?: string
  start_time?: string
  end_time?: string
  limit?: number
  offset?: number
}

export interface MetricsPoint {
  ts: number   // Unix seconds
  sr: number   // success_rate
  fr: number   // failure_rate
  tp: number   // throughput
  qd: number   // queue_depth
  wu: number   // worker_utilization
  ae: number   // active_executions
  mm: number   // memory_mb
  gr: number   // goroutines
}

export interface TrendInfo {
  direction: 'improving' | 'stable' | 'degrading'
  arrow: string
  slope: number
  moving_avg: number
}

export interface TrendReport {
  success_rate: TrendInfo
  failure_rate: TrendInfo
  queue_depth: TrendInfo
  worker_utilization: TrendInfo
  memory_mb: TrendInfo
}

export interface ForecastResult {
  queue_saturation_hours: number
  worker_exhaustion_hours: number
  queue_status: 'healthy' | 'warning' | 'critical'
  worker_status: 'healthy' | 'warning' | 'critical'
}

export interface AnomalyInfo {
  field: string
  timestamp: string
  value: number
  baseline: number
  deviation: number
  message: string
}

export interface MetricsHistoryResponse {
  period: string
  granularity: string
  count: number
  data: MetricsPoint[]
  trends: TrendReport
  forecast: ForecastResult
  anomalies: AnomalyInfo[]
}

// ============================================================================
// API Client Class
// ============================================================================

class APIClient {
  private client: AxiosInstance

  constructor(baseURL: string = API_BASE_URL) {
    this.client = axios.create({
      baseURL,
      timeout: 10000,
      headers: {
        'Content-Type': 'application/json',
      },
    })

    // Add token to requests if available
    this.client.interceptors.request.use((config) => {
      if (typeof window !== 'undefined') {
        const token = localStorage.getItem('dso_api_token')
        if (token) {
          config.headers.Authorization = `Bearer ${token}`
        }
      }
      return config
    })

    // Handle errors
    this.client.interceptors.response.use(
      (response) => response,
      (error: AxiosError) => {
        if (error.response?.status === 401) {
          if (typeof window !== 'undefined' && window.location.pathname !== '/login') {
            sessionStorage.setItem('session_expired', '1')
            localStorage.removeItem('dso_api_token')
            window.location.href = '/login'
          }
        }
        return Promise.reject(error)
      }
    )
  }

  // Health & Status
  async getHealth(): Promise<Health> {
    const response = await this.client.get<Health>('/health')
    return response.data
  }

  // Secrets
  async getSecrets(provider?: string): Promise<Secret[]> {
    const params = provider ? { provider } : {}
    const response = await this.client.get<{ active_secrets: Secret[]; total_count: number } | Secret[]>(
      '/api/secrets',
      { params }
    )
    // Backend wraps secrets in { active_secrets: [...], total_count: N }
    const data = response.data as { active_secrets?: Secret[] }
    if (Array.isArray(data?.active_secrets)) {
      return data.active_secrets
    }
    return Array.isArray(response.data) ? (response.data as Secret[]) : []
  }

  async getSecret(name: string): Promise<Secret | null> {
    try {
      const response = await this.client.get<Secret>(`/api/secrets/${name}`)
      return response.data
    } catch (error) {
      if (axios.isAxiosError(error) && error.response?.status === 404) {
        return null
      }
      throw error
    }
  }

  async rotateSecret(name: string, strategy?: string): Promise<{ status: string }> {
    const params = strategy ? { strategy } : {}
    const response = await this.client.post<{ status: string }>(
      `/api/secrets/${name}/rotate`,
      {},
      { params }
    )
    return response.data
  }

  // Events
  async getEvents(
    limit: number = 50,
    severity?: 'info' | 'warning' | 'error'
  ): Promise<Event[]> {
    const params: Record<string, unknown> = { limit }
    if (severity) params.severity = severity

    const response = await this.client.get<Event[]>('/api/events', { params })
    return Array.isArray(response.data) ? response.data : []
  }

  // Logs
  async getLogs(
    level?: 'debug' | 'info' | 'warning' | 'error',
    limit: number = 100,
    component?: string,
    since?: string
  ): Promise<LogEntry[]> {
    const params: Record<string, unknown> = { limit }
    if (level) params.level = level
    if (component) params.component = component
    if (since) params.since = since

    const response = await this.client.get<{ entries: LogEntry[]; count: number } | LogEntry[]>(
      '/api/logs',
      { params }
    )
    const data = response.data as { entries?: LogEntry[] }
    if (Array.isArray(data?.entries)) {
      return data.entries
    }
    return Array.isArray(response.data) ? (response.data as LogEntry[]) : []
  }

  // ── Users ──────────────────────────────────────────────────────────────────

  async listUsers(params?: { search?: string; role?: string; page?: number; page_size?: number }): Promise<{ users: User[]; count: number; page: number }> {
    const response = await this.client.get<{ users: User[]; count: number; page: number }>('/api/users', { params })
    return response.data
  }

  async getUser(id: string): Promise<User> {
    const response = await this.client.get<User>(`/api/users/${id}`)
    return response.data
  }

  async createUser(data: { username: string; password: string; display_name?: string; role: string }): Promise<User> {
    const response = await this.client.post<User>('/api/users', data)
    return response.data
  }

  async updateUser(id: string, data: { display_name?: string; role?: string; disabled?: boolean; unlock?: boolean; force_password_reset?: boolean }): Promise<User> {
    const response = await this.client.put<User>(`/api/users/${id}`, data)
    return response.data
  }

  async deleteUser(id: string): Promise<void> {
    await this.client.delete(`/api/users/${id}`)
  }

  // ── Sessions ───────────────────────────────────────────────────────────────

  async listSessions(): Promise<{ sessions: Session[]; count: number }> {
    const response = await this.client.get<{ sessions: Session[]; count: number }>('/api/sessions')
    return response.data
  }

  async revokeSession(id: string): Promise<void> {
    await this.client.delete(`/api/sessions/${id}`)
  }

  async revokeAllSessions(): Promise<void> {
    await this.client.post('/api/sessions/revoke-all', {})
  }

  // ── Password management ────────────────────────────────────────────────────

  async changePassword(currentPassword: string, newPassword: string): Promise<void> {
    await this.client.post('/api/auth/change-password', { current_password: currentPassword, new_password: newPassword })
  }

  async resetPassword(userId: string, newPassword: string): Promise<void> {
    await this.client.post('/api/auth/reset-password', { user_id: userId, new_password: newPassword })
  }

  // ── Auth me / session ─────────────────────────────────────────────────────

  async getMe(): Promise<{ id: string; username: string; display_name: string; role: string; must_change_password: boolean; password_expires_at?: string }> {
    const response = await this.client.get('/api/auth/me')
    return response.data
  }

  async getSessionInfo(): Promise<{ id: string; created_at: string; expires_at: string; ip_address: string }> {
    const response = await this.client.get('/api/auth/session')
    return response.data
  }

  async refreshSession(): Promise<{ expires_at: string }> {
    const response = await this.client.post('/api/auth/refresh', {})
    return response.data
  }

  // ── Admin: all sessions ────────────────────────────────────────────────────

  async listAdminSessions(): Promise<{ sessions: (Session & { username?: string })[]; count: number }> {
    const response = await this.client.get<{ sessions: (Session & { username?: string })[]; count: number }>('/api/admin/sessions')
    return response.data
  }

  async adminRevokeSession(id: string): Promise<void> {
    await this.client.delete(`/api/admin/sessions/${id}`)
  }

  // ── Audit explorer ────────────────────────────────────────────────────────

  async getAuditEvents(filters?: AuditFilters): Promise<AuditListResponse> {
    const response = await this.client.get<AuditListResponse>('/api/audit', { params: filters })
    return response.data
  }

  async getCorrelationChain(id: string): Promise<CorrelationChainResponse> {
    const response = await this.client.get<CorrelationChainResponse>(`/api/audit/correlation/${encodeURIComponent(id)}`)
    return response.data
  }

  async getActorTimeline(id: string, period?: '24h' | '7d' | '30d'): Promise<ActorTimelineResponse> {
    const response = await this.client.get<ActorTimelineResponse>(`/api/audit/actors/${encodeURIComponent(id)}`, { params: period ? { period } : {} })
    return response.data
  }

  async getExecutionJourney(id: string): Promise<ExecutionJourneyResponse> {
    const response = await this.client.get<ExecutionJourneyResponse>(`/api/executions/${encodeURIComponent(id)}/journey`)
    return response.data
  }

  getAuditExportURL(filters?: AuditFilters, format: 'json' | 'csv' = 'csv'): string {
    const params = new URLSearchParams({ format, ...Object.fromEntries(Object.entries(filters ?? {}).filter(([, v]) => v !== undefined).map(([k, v]) => [k, String(v)])) })
    return `/api/audit/export?${params}`
  }

  // ── Metrics analytics ─────────────────────────────────────────────────────

  async getMetricsHistory(params?: {
    period?: '1h' | '24h' | '7d' | '30d'
    granularity?: '1m' | '5m' | '1h'
  }): Promise<MetricsHistoryResponse> {
    const response = await this.client.get<MetricsHistoryResponse>('/api/metrics/history', { params })
    return response.data
  }

  getMetricsExportURL(period: string, format: 'json' | 'csv'): string {
    return `/api/metrics/export?period=${period}&format=${format}`
  }

  // WebSocket connection
  // Use same origin as dashboard server (which proxies to REST API)
  getWebSocketURL(path: string = '/api/events/ws'): string {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const host = window.location.host // Includes port from dashboard
    return `${protocol}//${host}${path}`
  }
}

// ============================================================================
// Singleton Instance
// ============================================================================

export const apiClient = new APIClient(API_BASE_URL)

export default apiClient
