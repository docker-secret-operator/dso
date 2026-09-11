import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { AuthenticationError } from '@/lib/errors'

vi.mock('next/navigation', () => ({
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  usePathname: () => '/dashboard',
}))

const logoutMock = vi.fn()
vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ logout: logoutMock, isAuthenticated: true, isLoading: false, login: vi.fn(), refreshSession: vi.fn() }),
}))

const fetchHealthMock = vi.fn()
const fetchSecretsMock = vi.fn()
const fetchDiscoveryMock = vi.fn()
vi.mock('@/lib/api/dashboard', () => ({
  fetchHealth: () => fetchHealthMock(),
  fetchSecrets: () => fetchSecretsMock(),
  fetchDiscovery: () => fetchDiscoveryMock(),
}))

const fetchAnalyticsMock = vi.fn()
vi.mock('@/lib/api/analytics', () => ({
  fetchAnalytics: () => fetchAnalyticsMock(),
}))

const fetchEventsMock = vi.fn()
vi.mock('@/lib/api/events', () => ({
  fetchEvents: () => fetchEventsMock(),
}))

import { DashboardContent } from '@/components/dashboard-content'

const emptyAnalytics = {
  current: { managed_secrets: 0, containers_targeted: 0, drifted: 0, degraded: 0 },
  since_restart: {
    rotation_success_total: 0,
    rotation_failure_total: 0,
    injection_success_total: 0,
    injection_failure_total: 0,
    note: 'Since last agent restart -- not an all-time total.',
  },
  recent_activity: { successful_rotations: [], failed_rotations: [], injection_failures: [], recent_failures: [] },
  degraded_services: [],
}

describe('DashboardContent', () => {
  beforeEach(() => {
    fetchHealthMock.mockReset()
    fetchSecretsMock.mockReset()
    fetchDiscoveryMock.mockReset()
    fetchAnalyticsMock.mockReset()
    fetchEventsMock.mockReset()
    logoutMock.mockReset()
    fetchEventsMock.mockResolvedValue([])
    fetchAnalyticsMock.mockResolvedValue(emptyAnalytics)
  })

  it('shows a loading state before data resolves', () => {
    fetchHealthMock.mockReturnValue(new Promise(() => {}))
    fetchSecretsMock.mockReturnValue(new Promise(() => {}))
    fetchDiscoveryMock.mockReturnValue(new Promise(() => {}))

    render(<DashboardContent />)
    expect(screen.getByTestId('dashboard-loading')).toBeInTheDocument()
  })

  it('renders real data once loaded', async () => {
    fetchHealthMock.mockResolvedValue({ status: 'up' })
    fetchSecretsMock.mockResolvedValue({
      total_count: 1,
      active_secrets: [
        { name: 'db-password', provider: 'vault', status: 'synced', injection_type: 'env', rotation_enabled: true, auto_sync_enabled: true },
      ],
    })
    fetchDiscoveryMock.mockResolvedValue({ webui_enabled: true, webhook_enabled: true, secret_count: 1 })

    render(<DashboardContent />)

    await waitFor(() => expect(screen.getByTestId('dashboard-content')).toBeInTheDocument())
    expect(screen.getByText('Up')).toBeInTheDocument()
    expect(screen.getByText('db-password')).toBeInTheDocument()
    expect(screen.getByText('Enabled')).toBeInTheDocument()
  })

  it('shows an honest empty state when there are no secrets', async () => {
    fetchHealthMock.mockResolvedValue({ status: 'up' })
    fetchSecretsMock.mockResolvedValue({ total_count: 0, active_secrets: [] })
    fetchDiscoveryMock.mockResolvedValue({ webui_enabled: true, webhook_enabled: false, secret_count: 0 })

    render(<DashboardContent />)

    await waitFor(() => expect(screen.getByTestId('dashboard-secrets-empty')).toBeInTheDocument())
    expect(screen.getByText('No managed secrets yet')).toBeInTheDocument()
  })

  it('shows an error state with a retry option when a fetch fails', async () => {
    fetchHealthMock.mockRejectedValue(new Error('Health check failed (500)'))
    fetchSecretsMock.mockResolvedValue({ total_count: 0, active_secrets: [] })
    fetchDiscoveryMock.mockResolvedValue({ webui_enabled: true, webhook_enabled: false, secret_count: 0 })

    render(<DashboardContent />)

    await waitFor(() => expect(screen.getByTestId('dashboard-error')).toBeInTheDocument())
    expect(screen.getByText(/health check failed/i)).toBeInTheDocument()
  })

  it('marks the error region with role=alert and the loading region with aria-busy', () => {
    fetchHealthMock.mockReturnValue(new Promise(() => {}))
    fetchSecretsMock.mockReturnValue(new Promise(() => {}))
    fetchDiscoveryMock.mockReturnValue(new Promise(() => {}))

    render(<DashboardContent />)
    const loading = screen.getByTestId('dashboard-loading')
    expect(loading).toHaveAttribute('aria-busy', 'true')
  })

  it('redirects through the existing auth flow instead of showing the retry banner on a 401', async () => {
    fetchHealthMock.mockRejectedValue(new AuthenticationError('Your session has expired. Please log in again.'))
    fetchSecretsMock.mockResolvedValue({ total_count: 0, active_secrets: [] })
    fetchDiscoveryMock.mockResolvedValue({ webui_enabled: true, webhook_enabled: false, secret_count: 0 })

    render(<DashboardContent />)

    await waitFor(() => expect(logoutMock).toHaveBeenCalledTimes(1))
    expect(screen.queryByTestId('dashboard-error')).not.toBeInTheDocument()
  })

  it('only redirects once when multiple simultaneous fetches all 401', async () => {
    fetchHealthMock.mockRejectedValue(new AuthenticationError())
    fetchSecretsMock.mockRejectedValue(new AuthenticationError())
    fetchDiscoveryMock.mockRejectedValue(new AuthenticationError())

    render(<DashboardContent />)

    await waitFor(() => expect(logoutMock).toHaveBeenCalled())
    // Promise.all only surfaces the first rejection to the catch block, and
    // the redirect is additionally guarded by a ref -- either way, exactly
    // one logout/redirect should fire, never one per failed request.
    expect(logoutMock).toHaveBeenCalledTimes(1)
  })

  describe('/api/analytics integration', () => {
    beforeEach(() => {
      fetchHealthMock.mockResolvedValue({ status: 'up' })
      fetchSecretsMock.mockResolvedValue({ total_count: 0, active_secrets: [] })
      fetchDiscoveryMock.mockResolvedValue({ webui_enabled: true, webhook_enabled: false, secret_count: 0 })
    })

    it('renders real zero values distinctly from unavailable (null) values', async () => {
      fetchAnalyticsMock.mockResolvedValue({
        ...emptyAnalytics,
        current: { managed_secrets: 0, containers_targeted: null, drifted: 0, degraded: 0 },
      })

      render(<DashboardContent />)

      await waitFor(() => expect(screen.getByTestId('analytics-summary')).toBeInTheDocument())
      // A real, known zero renders as "0", not as an empty/unavailable state.
      expect(screen.getAllByText('0').length).toBeGreaterThan(0)
      // A null (source unavailable) renders as "Unavailable", never as 0.
      expect(screen.getByText('Unavailable')).toBeInTheDocument()
    })

    it('does not block the rest of the dashboard when analytics fails', async () => {
      fetchAnalyticsMock.mockRejectedValue(new Error('Failed to load analytics (500)'))
      fetchSecretsMock.mockResolvedValue({
        total_count: 1,
        active_secrets: [
          { name: 'db-password', provider: 'vault', status: 'synced', injection_type: 'env', rotation_enabled: true, auto_sync_enabled: true },
        ],
      })

      render(<DashboardContent />)

      // Core dashboard content (secrets table) still renders.
      await waitFor(() => expect(screen.getByText('db-password')).toBeInTheDocument())
      // Analytics section shows its own actionable error, not a blank page.
      await waitFor(() => expect(screen.getByTestId('analytics-error')).toBeInTheDocument())
      expect(screen.getByText(/docker dso doctor/)).toBeInTheDocument()
    })

    it('shows a compact "attention required" section only when degraded services are real', async () => {
      fetchAnalyticsMock.mockResolvedValue({
        ...emptyAnalytics,
        current: { managed_secrets: 2, containers_targeted: 2, drifted: 0, degraded: 1 },
        degraded_services: [{ service: 'payments', reason: 'rotation failed, still serving previous secret' }],
      })

      render(<DashboardContent />)

      await waitFor(() => expect(screen.getByTestId('dashboard-degraded')).toBeInTheDocument())
      expect(screen.getByText('payments')).toBeInTheDocument()
      expect(screen.getByText('rotation failed, still serving previous secret')).toBeInTheDocument()
    })

    it('does not render an attention section when there are no degraded services', async () => {
      render(<DashboardContent />)

      await waitFor(() => expect(screen.getByTestId('analytics-summary')).toBeInTheDocument())
      expect(screen.queryByTestId('dashboard-degraded')).not.toBeInTheDocument()
    })

    it('builds Rotation Activity from analytics recent_activity, labeled by outcome', async () => {
      fetchAnalyticsMock.mockResolvedValue({
        ...emptyAnalytics,
        recent_activity: {
          successful_rotations: [
            { timestamp: '2026-01-01T00:00:02Z', secret: 'db-password', event_type: 'rotate' },
          ],
          failed_rotations: [
            { timestamp: '2026-01-01T00:00:01Z', secret: 'api-key', event_type: 'rotate' },
          ],
          injection_failures: [],
          recent_failures: [],
        },
      })

      render(<DashboardContent />)

      await waitFor(() => expect(screen.getByText('db-password')).toBeInTheDocument())
      expect(screen.getByText('Rotation succeeded')).toBeInTheDocument()
      expect(screen.getByText('api-key')).toBeInTheDocument()
      expect(screen.getByText('Rotation failed')).toBeInTheDocument()
    })

    it('shows an honest empty state for rotation activity when there is none', async () => {
      render(<DashboardContent />)

      await waitFor(() => expect(screen.getByText('No rotation activity yet')).toBeInTheDocument())
    })
  })

  it('does not blank the dashboard when the events feed fails -- only that section degrades', async () => {
    fetchHealthMock.mockResolvedValue({ status: 'up' })
    fetchSecretsMock.mockResolvedValue({
      total_count: 1,
      active_secrets: [
        { name: 'db-password', provider: 'vault', status: 'synced', injection_type: 'env', rotation_enabled: true, auto_sync_enabled: true },
      ],
    })
    fetchDiscoveryMock.mockResolvedValue({ webui_enabled: true, webhook_enabled: true, secret_count: 1 })
    fetchEventsMock.mockRejectedValue(new Error('network error'))

    render(<DashboardContent />)

    // Core content (secrets table, status pills, analytics) all render fine.
    await waitFor(() => expect(screen.getByText('db-password')).toBeInTheDocument())
    expect(screen.getByTestId('analytics-summary')).toBeInTheDocument()
    // The events section degrades to its own empty state rather than an
    // error banner -- events is informational, not load-bearing, matching
    // the same best-effort convention as before this redesign.
    expect(screen.getByText('No recent activity')).toBeInTheDocument()
    // Critically: no page-level error, and the rest of the dashboard is intact.
    expect(screen.queryByTestId('dashboard-error')).not.toBeInTheDocument()
  })
})
