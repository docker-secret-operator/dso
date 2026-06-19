import { describe, it, expect, beforeEach } from 'vitest'
import * as storage from '@/lib/auth/storage'

describe('Auth Storage', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  describe('Token Storage', () => {
    it('should store and retrieve access token', () => {
      const token = 'test-access-token-123'
      storage.setAccessToken(token)
      expect(storage.getAccessToken()).toBe(token)
    })

    it('should store and retrieve refresh token', () => {
      const token = 'test-refresh-token-456'
      storage.setRefreshToken(token)
      expect(storage.getRefreshToken()).toBe(token)
    })

    it('should return null when token not set', () => {
      expect(storage.getAccessToken()).toBeNull()
      expect(storage.getRefreshToken()).toBeNull()
    })
  })

  describe('User Storage', () => {
    it('should store and retrieve user data', () => {
      const user = {
        id: 'user-1',
        username: 'testuser',
        display_name: 'Test User',
        role: 'admin',
        must_change_password: false,
      }
      storage.setStoredUser(user)
      expect(storage.getStoredUser()).toEqual(user)
    })

    it('should return null when user not stored', () => {
      expect(storage.getStoredUser()).toBeNull()
    })

    it('should handle corrupted JSON gracefully', () => {
      localStorage.setItem('dso_user', 'invalid-json{')
      expect(storage.getStoredUser()).toBeNull()
    })
  })

  describe('Session Storage', () => {
    it('should store and retrieve session', () => {
      const session = {
        id: 'sess-1',
        created_at: new Date().toISOString(),
        expires_at: new Date(Date.now() + 3600000).toISOString(),
        ip_address: '127.0.0.1',
      }
      storage.setStoredSession(session)
      expect(storage.getStoredSession()).toEqual(session)
    })
  })

  describe('clearAllAuthData', () => {
    it('should remove all auth data', () => {
      storage.setAccessToken('token')
      storage.setRefreshToken('refresh')
      storage.setStoredUser({
        id: 'user-1',
        username: 'test',
        display_name: 'Test',
        role: 'viewer',
        must_change_password: false,
      })
      storage.setStoredSession({
        id: 'sess-1',
        created_at: new Date().toISOString(),
        expires_at: new Date(Date.now() + 3600000).toISOString(),
        ip_address: '127.0.0.1',
      })

      storage.clearAllAuthData()

      expect(storage.getAccessToken()).toBeNull()
      expect(storage.getRefreshToken()).toBeNull()
      expect(storage.getStoredUser()).toBeNull()
      expect(storage.getStoredSession()).toBeNull()
    })
  })
})
