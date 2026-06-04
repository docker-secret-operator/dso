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
          if (typeof window !== 'undefined') {
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
    const response = await this.client.get<Secret[]>('/api/secrets', { params })
    return Array.isArray(response.data) ? response.data : []
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
    limit: number = 100
  ): Promise<LogEntry[]> {
    const params: Record<string, unknown> = { limit }
    if (level) params.level = level

    const response = await this.client.get<LogEntry[]>('/api/logs', { params })
    return Array.isArray(response.data) ? response.data : []
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
