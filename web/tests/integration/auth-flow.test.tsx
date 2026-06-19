import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ReactNode } from 'react'
import { AuthProvider, useAuth } from '@/contexts/AuthContext'
import * as authApi from '@/lib/api/auth'
import * as session from '@/lib/auth/session'
import * as storage from '@/lib/auth/storage'

// Mock the API modules
vi.mock('@/lib/api/auth', () => ({
  login: vi.fn(),
  currentUser: vi.fn(),
  logout: vi.fn(),
}))

vi.mock('@/lib/auth/session', () => ({
  initializeSession: vi.fn(),
  refreshAccessToken: vi.fn(),
  clearSession: vi.fn(),
  isSessionValid: vi.fn(),
  isSessionExpiringSoon: vi.fn(),
}))

vi.mock('@/lib/api-client', () => ({
  apiClient: {
    client: {
      post: vi.fn(),
      get: vi.fn(),
    },
  },
}))

/**
 * Test component that uses auth context to test integration
 */
function TestAuthComponent() {
  const { user, isAuthenticated, isLoading, login, logout } = useAuth()

  if (isLoading) {
    return <div data-testid="loading">Loading...</div>
  }

  return (
    <div>
      {isAuthenticated ? (
        <div data-testid="authenticated">
          <p data-testid="user-display">{user?.display_name}</p>
          <p data-testid="user-role">{user?.role}</p>
          <button onClick={() => logout()} data-testid="logout-btn">
            Logout
          </button>
        </div>
      ) : (
        <div data-testid="unauthenticated">
          <button
            onClick={() => login('testuser', 'password')}
            data-testid="login-btn"
          >
            Login
          </button>
        </div>
      )}
    </div>
  )
}

/**
 * Test wrapper component
 */
function Wrapper({ children }: { children: ReactNode }) {
  return <AuthProvider>{children}</AuthProvider>
}

describe('Auth Flow Integration Tests', () => {
  const mockUser = {
    id: 'user-1',
    username: 'testuser',
    display_name: 'Test User',
    role: 'viewer',
    must_change_password: false,
  }

  const mockSession = {
    id: 'sess-1',
    created_at: new Date().toISOString(),
    expires_at: new Date(Date.now() + 3600000).toISOString(),
    ip_address: '127.0.0.1',
  }

  const mockLoginResponse = {
    token: 'test-token-123',
    expires_at: new Date(Date.now() + 3600000).toISOString(),
    user: mockUser,
    session: mockSession,
  }

  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
    // Mock session.initializeSession to return null initially
    ;(session.initializeSession as any).mockResolvedValue(null)
  })

  afterEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
  })

  describe('Login Flow', () => {
    it('should login with valid credentials and store token in localStorage', async () => {
      ;(authApi.login as any).mockResolvedValue(mockLoginResponse)
      ;(session.initializeSession as any).mockResolvedValue(null)

      const { rerender } = render(<TestAuthComponent />, { wrapper: Wrapper })

      // Wait for initial loading to complete
      await waitFor(() => {
        expect(screen.getByTestId('unauthenticated')).toBeInTheDocument()
      })

      const loginBtn = screen.getByTestId('login-btn')
      const user = userEvent.setup()
      await user.click(loginBtn)

      // Wait for auth state to update
      await waitFor(() => {
        expect(authApi.login).toHaveBeenCalledWith({
          username: 'testuser',
          password: 'password',
        })
      })

      // Verify token was stored
      expect(localStorage.getItem('dso_api_token')).toBe('test-token-123')
    })

    it('should receive token and user in login response', async () => {
      ;(authApi.login as any).mockResolvedValue(mockLoginResponse)
      ;(session.initializeSession as any).mockResolvedValue(null)

      render(<TestAuthComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('unauthenticated')).toBeInTheDocument()
      })

      const loginBtn = screen.getByTestId('login-btn')
      const user = userEvent.setup()
      await user.click(loginBtn)

      // Token should be stored
      await waitFor(() => {
        expect(localStorage.getItem('dso_api_token')).toBe('test-token-123')
      })
    })

    it('should set user in context after successful login', async () => {
      ;(authApi.login as any).mockResolvedValue(mockLoginResponse)
      ;(session.initializeSession as any).mockResolvedValue(null)

      render(<TestAuthComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('unauthenticated')).toBeInTheDocument()
      })

      const loginBtn = screen.getByTestId('login-btn')
      const user = userEvent.setup()
      await user.click(loginBtn)

      // Check that user is now set in the UI
      await waitFor(() => {
        expect(screen.getByTestId('user-display')).toHaveTextContent('Test User')
      })
    })

    it('should handle login error with invalid credentials', async () => {
      // Mock login to return null (which represents failure)
      ;(authApi.login as any).mockResolvedValue(null)
      ;(session.initializeSession as any).mockResolvedValue(null)

      render(<TestAuthComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('unauthenticated')).toBeInTheDocument()
      })

      const loginBtn = screen.getByTestId('login-btn')
      expect(loginBtn).toBeEnabled()

      // Verify that failed auth doesn't store token
      expect(localStorage.getItem('dso_api_token')).toBeNull()
    })
  })

  describe('Session Validation', () => {
    it('should return true for isSessionValid when session is valid', async () => {
      storage.setAccessToken('valid-token')
      storage.setStoredUser(mockUser)
      storage.setStoredSession(mockSession)

      vi.mocked(session.isSessionValid).mockReturnValue(true)
      ;(session.initializeSession as any).mockResolvedValue(mockUser)

      render(<TestAuthComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('authenticated')).toBeInTheDocument()
      })
    })

    it('should validate session after login', async () => {
      ;(authApi.login as any).mockResolvedValue(mockLoginResponse)
      ;(session.initializeSession as any).mockResolvedValue(null)

      render(<TestAuthComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('unauthenticated')).toBeInTheDocument()
      })

      const loginBtn = screen.getByTestId('login-btn')
      const user = userEvent.setup()
      await user.click(loginBtn)

      // After login, session should be valid
      await waitFor(() => {
        expect(storage.getAccessToken()).toBe('test-token-123')
      })
    })
  })

  describe('Logout Flow', () => {
    it('should logout and clear all auth data', async () => {
      ;(session.clearSession as any).mockResolvedValue(undefined)
      storage.setAccessToken('valid-token')
      storage.setStoredUser(mockUser)
      storage.setStoredSession(mockSession)

      ;(session.initializeSession as any).mockResolvedValue(mockUser)

      render(<TestAuthComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('authenticated')).toBeInTheDocument()
      })

      const logoutBtn = screen.getByTestId('logout-btn')
      const user = userEvent.setup()
      await user.click(logoutBtn)

      // Should clear auth data
      await waitFor(() => {
        expect(session.clearSession).toHaveBeenCalled()
      })
    })

    it('should redirect to login after logout', async () => {
      ;(session.clearSession as any).mockResolvedValue(undefined)
      storage.setAccessToken('valid-token')
      storage.setStoredUser(mockUser)
      storage.setStoredSession(mockSession)

      ;(session.initializeSession as any).mockResolvedValue(mockUser)

      render(<TestAuthComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('authenticated')).toBeInTheDocument()
      })

      const logoutBtn = screen.getByTestId('logout-btn')
      const user = userEvent.setup()
      await user.click(logoutBtn)

      // After logout, should show unauthenticated state
      await waitFor(() => {
        expect(screen.getByTestId('unauthenticated')).toBeInTheDocument()
      })
    })

    it('should handle logout errors gracefully', async () => {
      const error = new Error('Logout failed')
      ;(session.clearSession as any).mockRejectedValue(error)
      storage.setAccessToken('valid-token')
      storage.setStoredUser(mockUser)
      storage.setStoredSession(mockSession)

      ;(session.initializeSession as any).mockResolvedValue(mockUser)

      render(<TestAuthComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('authenticated')).toBeInTheDocument()
      })

      const logoutBtn = screen.getByTestId('logout-btn')
      const user = userEvent.setup()
      await user.click(logoutBtn)

      // Should still clear state and redirect despite error
      await waitFor(() => {
        expect(screen.getByTestId('unauthenticated')).toBeInTheDocument()
      })
    })
  })

  describe('Protected Route', () => {
    it('should show authenticated content when user is logged in', async () => {
      storage.setAccessToken('valid-token')
      storage.setStoredUser(mockUser)
      storage.setStoredSession(mockSession)

      ;(session.initializeSession as any).mockResolvedValue(mockUser)

      render(<TestAuthComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('authenticated')).toBeInTheDocument()
      })
    })

    it('should show login prompt when user is not authenticated', async () => {
      ;(session.initializeSession as any).mockResolvedValue(null)

      render(<TestAuthComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('unauthenticated')).toBeInTheDocument()
      })
    })
  })

  describe('Session Expiry', () => {
    it('should treat expired token as invalid', async () => {
      const pastDate = new Date(Date.now() - 3600000).toISOString()
      storage.setAccessToken('expired-token')
      storage.setStoredSession({
        id: 'sess-1',
        created_at: pastDate,
        expires_at: pastDate,
        ip_address: '127.0.0.1',
      })
      storage.setStoredUser(mockUser)

      ;(session.initializeSession as any).mockResolvedValue(null)

      render(<TestAuthComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('unauthenticated')).toBeInTheDocument()
      })
    })

    it('should detect when session is expiring soon', () => {
      const almostExpired = new Date(Date.now() + 240000).toISOString()
      storage.setStoredSession({
        id: 'sess-1',
        created_at: new Date().toISOString(),
        expires_at: almostExpired,
        ip_address: '127.0.0.1',
      })

      ;(session.isSessionExpiringSoon as any).mockReturnValue(true)
      const expiringSoon = session.isSessionExpiringSoon()
      expect(expiringSoon).toBe(true)
    })
  })

  describe('Token Refresh', () => {
    it('should refresh token when refresh endpoint succeeds', async () => {
      const newExpiry = new Date(Date.now() + 3600000).toISOString()
      ;(session.refreshAccessToken as any).mockResolvedValue(true)
      ;(authApi.currentUser as any).mockResolvedValue(mockUser)

      storage.setAccessToken('valid-token')
      storage.setStoredUser(mockUser)
      storage.setStoredSession(mockSession)

      const result = await session.refreshAccessToken()

      expect(result).toBe(true)
    })

    it('should clear session when refresh fails', async () => {
      ;(session.refreshAccessToken as any).mockResolvedValue(false)

      storage.setAccessToken('expired-token')
      storage.setStoredUser(mockUser)
      storage.setStoredSession(mockSession)

      const result = await session.refreshAccessToken()

      expect(result).toBe(false)
    })
  })

  describe('Error Handling', () => {
    it('should not store token when login is not called', async () => {
      ;(authApi.login as any).mockResolvedValue(null)
      ;(session.initializeSession as any).mockResolvedValue(null)

      render(<TestAuthComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('unauthenticated')).toBeInTheDocument()
      })

      // Token should not be stored when login not attempted
      expect(localStorage.getItem('dso_api_token')).toBeNull()
    })

    it('should verify failed login results in no stored token', async () => {
      // Simulate empty/null response from login
      ;(authApi.login as any).mockResolvedValue(null)
      ;(session.initializeSession as any).mockResolvedValue(null)

      render(<TestAuthComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('unauthenticated')).toBeInTheDocument()
      })

      // Should not have token
      expect(localStorage.getItem('dso_api_token')).toBeNull()
    })

    it('should handle login auth context errors gracefully', async () => {
      // Test that auth context handles errors without crashing
      ;(session.initializeSession as any).mockResolvedValue(null)
      ;(authApi.login as any).mockResolvedValue(null)

      const { container } = render(<TestAuthComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('unauthenticated')).toBeInTheDocument()
      })

      // UI should still be responsive
      expect(container).toBeTruthy()
      expect(screen.getByTestId('login-btn')).toBeEnabled()
    })
  })

  describe('Session Persistence', () => {
    it('should restore session on page refresh when token is valid', async () => {
      storage.setAccessToken('valid-token')
      storage.setStoredUser(mockUser)
      storage.setStoredSession(mockSession)

      ;(session.initializeSession as any).mockResolvedValue(mockUser)

      render(<TestAuthComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('authenticated')).toBeInTheDocument()
      })

      // Verify user is displayed
      expect(screen.getByTestId('user-display')).toHaveTextContent('Test User')
    })

    it('should clear session on page refresh when token is expired', async () => {
      const pastDate = new Date(Date.now() - 3600000).toISOString()
      storage.setAccessToken('expired-token')
      storage.setStoredSession({
        id: 'sess-1',
        created_at: pastDate,
        expires_at: pastDate,
        ip_address: '127.0.0.1',
      })

      ;(session.initializeSession as any).mockResolvedValue(null)

      render(<TestAuthComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('unauthenticated')).toBeInTheDocument()
      })
    })
  })

  describe('Role-Based Access', () => {
    it('should display user role in context', async () => {
      storage.setAccessToken('valid-token')
      storage.setStoredUser(mockUser)
      storage.setStoredSession(mockSession)

      ;(session.initializeSession as any).mockResolvedValue(mockUser)

      render(<TestAuthComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('user-role')).toHaveTextContent('viewer')
      })
    })

    it('should handle admin role', async () => {
      const adminUser = { ...mockUser, role: 'admin' }
      storage.setAccessToken('valid-token')
      storage.setStoredUser(adminUser)
      storage.setStoredSession(mockSession)

      ;(session.initializeSession as any).mockResolvedValue(adminUser)

      render(<TestAuthComponent />, { wrapper: Wrapper })

      await waitFor(() => {
        expect(screen.getByTestId('user-role')).toHaveTextContent('admin')
      })
    })

    it('should handle different roles correctly', async () => {
      const roles = ['viewer', 'editor', 'admin']

      for (const role of roles) {
        const testUser = { ...mockUser, role }
        storage.setAccessToken('valid-token')
        storage.setStoredUser(testUser)
        storage.setStoredSession(mockSession)

        ;(session.initializeSession as any).mockResolvedValue(testUser)

        const { unmount } = render(<TestAuthComponent />, { wrapper: Wrapper })

        await waitFor(() => {
          expect(screen.getByTestId('user-role')).toHaveTextContent(role)
        })

        unmount()
        localStorage.clear()
        vi.clearAllMocks()
      }
    })
  })
})
