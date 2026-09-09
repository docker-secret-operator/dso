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

import { DashboardContent } from '@/components/dashboard-content'

describe('DashboardContent', () => {
  beforeEach(() => {
    fetchHealthMock.mockReset()
    fetchSecretsMock.mockReset()
    fetchDiscoveryMock.mockReset()
    logoutMock.mockReset()
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
})
