import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import * as session from '@/lib/auth/session'
import * as storage from '@/lib/auth/storage'

describe('Session Management', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
  })

  afterEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    vi.useRealTimers()
  })

  describe('isSessionValid', () => {
    it('should return false when no token stored', () => {
      expect(session.isSessionValid()).toBe(false)
    })

    it('should return false when session is expired', () => {
      const pastDate = new Date(Date.now() - 3600000).toISOString()
      storage.setAccessToken('test-token')
      storage.setStoredSession({
        id: 'sess-1',
        created_at: pastDate,
        expires_at: pastDate,
        ip_address: '127.0.0.1',
      })

      expect(session.isSessionValid()).toBe(false)
    })

    it('should return true when session is valid and not expired', () => {
      const futureDate = new Date(Date.now() + 3600000).toISOString()
      storage.setAccessToken('test-token')
      storage.setStoredSession({
        id: 'sess-1',
        created_at: new Date().toISOString(),
        expires_at: futureDate,
        ip_address: '127.0.0.1',
      })
      storage.setStoredUser({
        id: 'user-1',
        username: 'test',
        display_name: 'Test User',
        role: 'viewer',
        must_change_password: false,
      })

      expect(session.isSessionValid()).toBe(true)
    })
  })

  describe('getSessionTimeRemaining', () => {
    it('should return 0 when no session', () => {
      expect(session.getSessionTimeRemaining()).toBe(0)
    })

    it('should return remaining seconds when session valid', () => {
      vi.useFakeTimers()
      const now = new Date('2026-06-19T12:00:00Z').getTime()
      vi.setSystemTime(now)

      const expiryTime = new Date(now + 3600000) // 1 hour from now
      storage.setStoredSession({
        id: 'sess-1',
        created_at: new Date(now).toISOString(),
        expires_at: expiryTime.toISOString(),
        ip_address: '127.0.0.1',
      })

      const remaining = session.getSessionTimeRemaining()
      expect(remaining).toBe(3600) // Exactly 3600 seconds with fake timers

      vi.useRealTimers()
    })
  })

  describe('isSessionExpiringSoon', () => {
    it('should return true when less than 5 minutes remaining', () => {
      const almostExpired = new Date(Date.now() + 240000).toISOString()
      storage.setStoredSession({
        id: 'sess-1',
        created_at: new Date().toISOString(),
        expires_at: almostExpired,
        ip_address: '127.0.0.1',
      })

      expect(session.isSessionExpiringSoon()).toBe(true)
    })

    it('should return true when just under 5 minutes remaining (boundary case)', () => {
      const justUnder = new Date(Date.now() + 299999).toISOString()
      storage.setStoredSession({
        id: 'sess-1',
        created_at: new Date().toISOString(),
        expires_at: justUnder,
        ip_address: '127.0.0.1',
      })

      expect(session.isSessionExpiringSoon()).toBe(true)
    })

    it('should return false when at exactly 5 minutes (boundary case)', () => {
      const exactBoundary = new Date(Date.now() + 300000).toISOString()
      storage.setStoredSession({
        id: 'sess-1',
        created_at: new Date().toISOString(),
        expires_at: exactBoundary,
        ip_address: '127.0.0.1',
      })

      expect(session.isSessionExpiringSoon()).toBe(false)
    })

    it('should return false when just over 5 minutes remaining (boundary case)', () => {
      const justOver = new Date(Date.now() + 300001).toISOString()
      storage.setStoredSession({
        id: 'sess-1',
        created_at: new Date().toISOString(),
        expires_at: justOver,
        ip_address: '127.0.0.1',
      })

      expect(session.isSessionExpiringSoon()).toBe(false)
    })

    it('should return false when more than 5 minutes remaining', () => {
      const futureDate = new Date(Date.now() + 3600000).toISOString()
      storage.setStoredSession({
        id: 'sess-1',
        created_at: new Date().toISOString(),
        expires_at: futureDate,
        ip_address: '127.0.0.1',
      })

      expect(session.isSessionExpiringSoon()).toBe(false)
    })
  })
})
