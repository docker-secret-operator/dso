import { describe, it, expect, beforeEach, vi } from 'vitest'
import * as operationsApi from '@/lib/api/operations'
import { apiClient } from '@/lib/api-client'

vi.mock('@/lib/api-client', () => ({
  apiClient: {
    client: {
      get: vi.fn(),
      post: vi.fn(),
    },
  },
}))

describe('Operations API', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('getOperationsDashboard fetches dashboard', async () => {
    const mockData = { data: { success_rate: 95.5, failure_rate: 4.5, throughput_per_sec: 125.3, worker_utilization: 72, total_executions: 5000, timestamp: '2026-06-20T10:00:00Z' } }
    vi.mocked(apiClient.client.get).mockResolvedValue(mockData)
    const result = await operationsApi.getOperationsDashboard()
    expect(result).toBeDefined()
  })

  it('getAlerts returns alerts array', async () => {
    const mockData = { data: { alerts: [{ id: 'alert-1', severity: 'critical', message: 'Error rate high', threshold: '5%', current_value: '12%', timestamp: '2026-06-20T10:00:00Z' }] } }
    vi.mocked(apiClient.client.get).mockResolvedValue(mockData)
    const result = await operationsApi.getAlerts()
    expect(result).toHaveLength(1)
    expect(result[0].severity).toBe('critical')
  })

  it('getRecoveryEvents returns events', async () => {
    const mockData = { data: { events: [{ id: 'evt-1', type: 'worker_failure', worker_id: 'w1', timestamp: '2026-06-20T10:00:00Z', details: 'Crashed' }] } }
    vi.mocked(apiClient.client.get).mockResolvedValue(mockData)
    const result = await operationsApi.getRecoveryEvents()
    expect(result).toHaveLength(1)
  })

  it('getMetricsHistory returns metrics', async () => {
    const mockData = { data: { timestamp: '2026-06-20T10:00:00Z', throughput_history: [{ timestamp: '2026-06-20T09:00:00Z', value: 100 }], queue_depth_history: [{ timestamp: '2026-06-20T09:00:00Z', value: 50 }], worker_utilization_history: [{ timestamp: '2026-06-20T09:00:00Z', value: 75 }], success_rate_history: [{ timestamp: '2026-06-20T09:00:00Z', value: 95 }] } }
    vi.mocked(apiClient.client.get).mockResolvedValue(mockData)
    const result = await operationsApi.getMetricsHistory()
    expect(result).toBeDefined()
  })

  it('getExecutions returns list', async () => {
    const mockData = { data: { executions: [{ id: 'exec-1', status: 'completed', created_at: '2026-06-20T10:00:00Z', readiness_score: 85, correlation_id: 'corr-123' }], total: 100, offset: 0, limit: 20 } }
    vi.mocked(apiClient.client.get).mockResolvedValue(mockData)
    const result = await operationsApi.getExecutions({ limit: 20, offset: 0 })
    expect(result).toBeDefined()
  })

  it('getExecution fetches single execution', async () => {
    const mockData = { data: { id: 'exec-1', status: 'running', created_at: '2026-06-20T10:00:00Z', readiness_score: 90, correlation_id: 'corr-123' } }
    vi.mocked(apiClient.client.get).mockResolvedValue(mockData)
    const result = await operationsApi.getExecution('exec-1')
    expect(result.id).toBe('exec-1')
  })

  it('getExecutionPlan returns plan', async () => {
    const mockData = { data: { id: 'exec-1', steps: [{ id: 'step-1', name: 'Init', type: 'setup', depends_on: [], status: 'completed' }], estimated_duration_seconds: 300 } }
    vi.mocked(apiClient.client.get).mockResolvedValue(mockData)
    const result = await operationsApi.getExecutionPlan('exec-1')
    expect(result.estimated_duration_seconds).toBe(300)
  })

  it('getExecutionValidation returns validation', async () => {
    const mockData = { data: { id: 'exec-1', is_valid: true, warnings: [], errors: [] } }
    vi.mocked(apiClient.client.get).mockResolvedValue(mockData)
    const result = await operationsApi.getExecutionValidation('exec-1')
    expect(result).toBeDefined()
  })

  it('getExecutionTrace returns trace', async () => {
    const mockData = { data: { id: 'exec-1', events: [{ timestamp: '2026-06-20T10:00:00Z', level: 'info', message: 'Started', context: {} }] } }
    vi.mocked(apiClient.client.get).mockResolvedValue(mockData)
    const result = await operationsApi.getExecutionTrace('exec-1')
    expect(result.events).toHaveLength(1)
  })

  it('getExecutionJourney returns journey', async () => {
    const mockData = { data: { id: 'exec-1', events: [{ timestamp: '2026-06-20T10:00:00Z', event_type: 'started', description: 'Started', status: 'success' }], total_duration_seconds: 150 } }
    vi.mocked(apiClient.client.get).mockResolvedValue(mockData)
    const result = await operationsApi.getExecutionJourney('exec-1')
    expect(result).toBeDefined()
  })
})
