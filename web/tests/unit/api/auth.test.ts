import { describe, it, expect, beforeEach, vi } from 'vitest'
import * as authApi from '@/lib/api/auth'
import { apiClient } from '@/lib/api-client'

vi.mock('@/lib/api-client', () => ({
  apiClient: {
    client: {
      post: vi.fn(),
      get: vi.fn(),
    },
  },
}))

describe('Auth API', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
  })

  describe('login', () => {
    it('should send credentials and return token', async () => {
      const mockResponse = {
        data: {
          token: 'test-token',
          user: {
            id: 'user-1',
            username: 'testuser',
            display_name: 'Test User',
            role: 'viewer',
            must_change_password: false,
          },
        },
      }

      vi.mocked(apiClient.client.post).mockResolvedValue(mockResponse)

      const result = await authApi.login({ username: 'test', password: 'pass' })

      expect(result.token).toBe('test-token')
      expect(apiClient.client.post).toHaveBeenCalledWith('/api/auth/login', {
        username: 'test',
        password: 'pass',
      })
    })

    it('should throw UnauthorizedError on 401', async () => {
      const error = new Error('Unauthorized')
      ;(error as any).response = { status: 401 }

      vi.mocked(apiClient.client.post).mockRejectedValue(error)

      await expect(
        authApi.login({ username: 'test', password: 'wrong' })
      ).rejects.toThrow()
    })
  })

  describe('currentUser', () => {
    it('should fetch current user', async () => {
      const mockUser = {
        data: {
          id: 'user-1',
          username: 'testuser',
          display_name: 'Test User',
          role: 'admin',
          must_change_password: false,
        },
      }

      vi.mocked(apiClient.client.get).mockResolvedValue(mockUser)

      const result = await authApi.currentUser()

      expect(result.username).toBe('testuser')
      expect(apiClient.client.get).toHaveBeenCalledWith('/api/auth/me')
    })
  })
})
