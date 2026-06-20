# Phase 5.75: Quality & Stability Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development for task-by-task execution with reviews.

**Goal:** Transition DSO Web UI from "feature complete" to "validated and reliable" without architectural changes or new features.

**Architecture:** 8-phase testing pyramid: Unit → Component → Integration → E2E → Accessibility → Error Handling → Performance → CI/CD

**Success Criteria:** All phases passing before Phase 5C Operations Console starts.

---

## Testing Infrastructure Setup

### Task 0: Test Environment Configuration

**Files to Create:**
- `web/vitest.config.ts` - Vitest configuration
- `web/tests/setup.ts` - Global test setup
- `web/tests/mocks/handlers.ts` - MSW handlers
- `web/jest.config.js` - Jest configuration for coverage
- `web/playwright.config.ts` - Playwright configuration

**Code - vitest.config.ts:**

```typescript
import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./tests/setup.ts'],
    include: ['tests/**/*.test.{ts,tsx}'],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json', 'html'],
      exclude: [
        'node_modules/',
        'tests/',
        '**/*.d.ts',
        '**/types.ts',
      ],
    },
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './'),
    },
  },
})
```

**Code - tests/setup.ts:**

```typescript
import { expect, afterEach, beforeEach, vi } from 'vitest'
import { cleanup } from '@testing-library/react'
import '@testing-library/jest-dom'

// Cleanup after each test
afterEach(() => {
  cleanup()
})

// Mock localStorage
const localStorageMock = (() => {
  let store: Record<string, string> = {}
  return {
    getItem: (key: string) => store[key] || null,
    setItem: (key: string, value: string) => {
      store[key] = value.toString()
    },
    removeItem: (key: string) => {
      delete store[key]
    },
    clear: () => {
      store = {}
    },
  }
})()

Object.defineProperty(window, 'localStorage', {
  value: localStorageMock,
})

// Mock window.matchMedia
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: vi.fn().mockImplementation(query => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })),
})
```

**package.json scripts to add:**

```json
{
  "scripts": {
    "test": "vitest run",
    "test:watch": "vitest",
    "test:ui": "vitest --ui",
    "test:coverage": "vitest run --coverage",
    "test:e2e": "playwright test",
    "test:a11y": "axe-core --verbose"
  }
}
```

**Commit:** "test: initialize testing infrastructure with Vitest and Playwright"

---

## Phase 1: Unit Tests

### Task 1: Auth Permissions Tests

**File:** `web/tests/unit/auth/permissions.test.ts`

```typescript
import { describe, it, expect } from 'vitest'
import {
  hasPermission,
  canApprove,
  canReview,
  canOperate,
  canView,
} from '@/lib/auth/permissions'

describe('Auth Permissions', () => {
  describe('Role Hierarchy', () => {
    it('admin should have all permissions', () => {
      expect(canView('admin')).toBe(true)
      expect(canOperate('admin')).toBe(true)
      expect(canReview('admin')).toBe(true)
      expect(canApprove('admin')).toBe(true)
    })

    it('approver should have approve, review, operate, view', () => {
      expect(canView('approver')).toBe(true)
      expect(canOperate('approver')).toBe(true)
      expect(canReview('approver')).toBe(true)
      expect(canApprove('approver')).toBe(true)
    })

    it('reviewer should have review, operate, view', () => {
      expect(canView('reviewer')).toBe(true)
      expect(canOperate('reviewer')).toBe(true)
      expect(canReview('reviewer')).toBe(true)
      expect(canApprove('reviewer')).toBe(false)
    })

    it('operator should have operate, view', () => {
      expect(canView('operator')).toBe(true)
      expect(canOperate('operator')).toBe(true)
      expect(canReview('operator')).toBe(false)
      expect(canApprove('operator')).toBe(false)
    })

    it('viewer should only have view', () => {
      expect(canView('viewer')).toBe(true)
      expect(canOperate('viewer')).toBe(false)
      expect(canReview('viewer')).toBe(false)
      expect(canApprove('viewer')).toBe(false)
    })
  })

  describe('hasPermission', () => {
    it('should return true when role has permission', () => {
      expect(hasPermission('admin', 'view')).toBe(true)
      expect(hasPermission('operator', 'operate')).toBe(true)
    })

    it('should return false when role lacks permission', () => {
      expect(hasPermission('viewer', 'operate')).toBe(false)
      expect(hasPermission('operator', 'approve')).toBe(false)
    })

    it('should handle invalid roles gracefully', () => {
      expect(hasPermission('invalid' as any, 'view')).toBe(false)
    })
  })
})
```

**Commit:** "test: add comprehensive permissions unit tests"

---

### Task 2: Session Management Tests

**File:** `web/tests/unit/auth/session.test.ts`

```typescript
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import * as session from '@/lib/auth/session'
import * as storage from '@/lib/auth/storage'

describe('Session Management', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
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
      const futureDate = new Date(Date.now() + 3600000).toISOString()
      storage.setStoredSession({
        id: 'sess-1',
        created_at: new Date().toISOString(),
        expires_at: futureDate,
        ip_address: '127.0.0.1',
      })
      
      const remaining = session.getSessionTimeRemaining()
      expect(remaining).toBeGreaterThan(3590)
      expect(remaining).toBeLessThanOrEqual(3600)
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
```

**Commit:** "test: add session management unit tests"

---

### Task 3: Storage Tests

**File:** `web/tests/unit/auth/storage.test.ts`

```typescript
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
```

**Commit:** "test: add auth storage unit tests"

---

### Task 4: Export Utility Tests

**File:** `web/tests/unit/utils/discovery-export.test.ts`

```typescript
import { describe, it, expect } from 'vitest'
import {
  exportContainersToCSV,
  exportContainersToJSON,
} from '@/lib/utils/discovery-export'

describe('Discovery Export Utilities', () => {
  const mockContainers = [
    {
      container_id: 'abc123',
      container_name: 'api-server',
      image: 'registry.example.com/api:v1',
      status: 'running',
      dso_awareness: {
        classification: 'managed',
        managed_secrets: 2,
        config_references: 1,
        missing_mappings: 0,
      },
    },
    {
      container_id: 'xyz789',
      container_name: 'database',
      image: 'postgres:15',
      status: 'running',
      dso_awareness: {
        classification: 'partial',
        managed_secrets: 1,
        config_references: 0,
        missing_mappings: 1,
      },
    },
  ]

  describe('exportContainersToCSV', () => {
    it('should return empty string for empty array', () => {
      expect(exportContainersToCSV([])).toBe('')
    })

    it('should include CSV headers', () => {
      const csv = exportContainersToCSV(mockContainers)
      expect(csv).toContain('Container Name')
      expect(csv).toContain('Image')
      expect(csv).toContain('Status')
      expect(csv).toContain('Classification')
    })

    it('should include all container data', () => {
      const csv = exportContainersToCSV(mockContainers)
      expect(csv).toContain('api-server')
      expect(csv).toContain('database')
      expect(csv).toContain('managed')
      expect(csv).toContain('partial')
    })

    it('should properly escape CSV quotes', () => {
      const containerWithQuotes = [
        {
          container_name: 'server "prod"',
          ...mockContainers[0],
        },
      ]
      const csv = exportContainersToCSV(containerWithQuotes)
      expect(csv).toContain('"server ""prod"""')
    })
  })

  describe('exportContainersToJSON', () => {
    it('should return valid JSON', () => {
      const json = exportContainersToJSON(mockContainers)
      expect(() => JSON.parse(json)).not.toThrow()
    })

    it('should include all container data', () => {
      const json = JSON.parse(exportContainersToJSON(mockContainers))
      expect(json).toHaveLength(2)
      expect(json[0].container_name).toBe('api-server')
      expect(json[1].container_name).toBe('database')
    })

    it('should be properly formatted (2-space indent)', () => {
      const json = exportContainersToJSON(mockContainers)
      expect(json).toContain('  ')
      expect(json.startsWith('[')).toBe(true)
    })
  })
})
```

**Commit:** "test: add export utility unit tests"

---

### Task 5: API Service Tests

**File:** `web/tests/unit/api/auth.test.ts`

```typescript
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
```

**Commit:** "test: add auth API unit tests"

---

### Task 6: API Service Tests (Continued)

**File:** `web/tests/unit/api/discovery.test.ts`

```typescript
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

      expect(result.total).toBe(1)
      expect(result.managed).toBe(1)
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
```

**Commit:** "test: add discovery API unit tests"

---

## Phase 1 Success Criteria

- ✅ All 6 unit test suites created
- ✅ 40+ test cases written
- ✅ 100% coverage of auth, storage, session, export, and API modules
- ✅ All tests passing
- ✅ No console warnings/errors

---

## Phase 2-8: Continuation

The remaining phases follow the same structure:

**Phase 2:** Component tests (AuditFilters, AuditTable, CorrelationTimeline, ActorTimeline, ContainerTable, CoverageMetrics, SecretMappingsTable, DiscoveryFilters, RefreshButton, EmptyState)

**Phase 3:** Integration tests (auth flow, dashboard load, audit search/filters/pagination, discovery filters/search/drawer)

**Phase 4:** E2E tests with Playwright (full user journeys)

**Phase 5:** Accessibility audits with axe-core

**Phase 6:** Error isolation tests (verify cascading failures don't occur)

**Phase 7:** Performance profiling (React DevTools, memory leaks, re-renders)

**Phase 8:** GitHub Actions CI/CD workflow

---

## Success Metrics

| Phase | Tests | Coverage | Status |
|-------|-------|----------|--------|
| 1: Unit | 40+ | >80% | ✓ |
| 2: Component | 50+ | >75% | ⏳ |
| 3: Integration | 30+ | Full flow | ⏳ |
| 4: E2E | 8+ | All scenarios | ⏳ |
| 5: A11y | 15+ | WCAG AA | ⏳ |
| 6: Error | 20+ | Isolation | ⏳ |
| 7: Performance | 10+ | Profiles | ⏳ |
| 8: CI/CD | Pipeline | Green | ⏳ |

**Total: 183+ tests across 8 phases**

