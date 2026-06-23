import { describe, it, expect, beforeEach, vi } from 'vitest'
import * as discoveryApi from '@/lib/api/discovery'
import { apiClient } from '@/lib/api-client'

vi.mock('@/lib/api-client')

describe('Discovery API', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('getContainers', () => {
    it('should fetch containers with correct endpoint', async () => {
      const mockResponse = {
        data: {
          containers: [
            {
              container_id: 'abc123',
              container_name: 'api-server',
              status: 'running',
              dso_awareness: { classification: 'managed', managed_secrets: 1, config_references: 0, missing_mappings: 0 },
            },
          ],
          total: 1,
          managed: 1,
          partial: 0,
          unmanaged: 0,
          timestamp: new Date().toISOString(),
        },
      }

      vi.mocked(apiClient.client.get).mockResolvedValue(mockResponse)

      const result = await discoveryApi.getContainers()

      expect(result.total_count).toBe(1)
      expect(result.managed_count).toBe(1)
      expect(apiClient.client.get).toHaveBeenCalledWith('/api/discovery/containers')
    })
  })

  describe('getMappings', () => {
    it('should fetch secret mappings', async () => {
      const mockResponse = {
        data: {
          suggestions: [
            {
              env_var_name: 'DB_PASSWORD',
              suggested_secret_name: 'postgres-password',
              confidence: 'high',
              reason: 'Contains password keyword',
              is_configured: false,
            },
          ],
          count: 1,
          timestamp: new Date().toISOString(),
        },
      }

      vi.mocked(apiClient.client.get).mockResolvedValue(mockResponse)

      const result = await discoveryApi.getMappings()

      expect(result.count).toBe(1)
      expect(result.suggestions[0].confidence).toBe('high')
    })
  })

  describe('getDiscoveryMetrics', () => {
    it('should fetch cache metrics', async () => {
      const mockResponse = {
        data: {
          cache_hits: 1000,
          cache_misses: 50,
          refresh_count: 5,
          avg_latency_ms: 145,
          cache_age_seconds: 30,
        },
      }

      vi.mocked(apiClient.client.get).mockResolvedValue(mockResponse)

      const result = await discoveryApi.getDiscoveryMetrics()

      expect(result.cache_hits).toBe(1000)
      expect(result.avg_latency_ms).toBe(145)
    })
  })

  describe('refreshDiscovery', () => {
    it('should trigger async refresh', async () => {
      const mockResponse = {
        data: { status: 'refreshing', message: 'Discovery refresh initiated' },
      }

      vi.mocked(apiClient.client.get).mockResolvedValue(mockResponse)

      const result = await discoveryApi.refreshDiscovery()

      expect(result.status).toBe('refreshing')
    })
  })
})
