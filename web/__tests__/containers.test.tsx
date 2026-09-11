import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

vi.mock('next/navigation', () => ({
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  usePathname: () => '/containers',
}))

const fetchContainersMock = vi.fn()
vi.mock('@/lib/api/containers', () => ({
  fetchContainers: () => fetchContainersMock(),
}))

import { ContainersContent } from '@/components/containers-content'

const paymentsApi = {
  id: 'a84f2c91e73bcontainerid0001',
  name: 'payments-api',
  image: 'myapp/backend:2026.09',
  status: 'running',
  health: 'healthy',
  ports: ['8080->80/tcp'],
  strategy: 'rolling',
  secrets: ['db-password', 'api-key'],
  degraded: false,
}
const oldWorker = {
  id: 'b93e1d82f64acontainerid0002',
  name: 'old-worker',
  image: 'myapp/worker:1.0',
  status: 'exited',
  health: '',
  ports: [],
  strategy: 'restart',
  secrets: [],
  degraded: true,
  degraded_reason: 'rotation failed, still serving previous secret',
}

describe('ContainersContent', () => {
  beforeEach(() => {
    fetchContainersMock.mockReset()
  })

  it('shows a table-shaped loading state before GET /api/containers resolves', () => {
    fetchContainersMock.mockReturnValue(new Promise(() => {}))

    render(<ContainersContent />)
    const loading = screen.getByTestId('containers-loading')
    expect(loading).toBeInTheDocument()
    expect(loading).toHaveAttribute('aria-busy', 'true')
  })

  it('renders real container metadata once loaded', async () => {
    fetchContainersMock.mockResolvedValue({ total_count: 1, containers: [paymentsApi] })

    render(<ContainersContent />)

    await waitFor(() => expect(screen.getByTestId('containers-content')).toBeInTheDocument())
    expect(screen.getByText('payments-api')).toBeInTheDocument()
    expect(screen.getByText('a84f2c91e73b')).toBeInTheDocument() // short ID, not the full 64-char ID
    expect(screen.getByText('myapp/backend:2026.09')).toBeInTheDocument()
    expect(screen.getByText('rolling')).toBeInTheDocument()
  })

  it('never renders the full container ID as the primary identity', async () => {
    fetchContainersMock.mockResolvedValue({ total_count: 1, containers: [paymentsApi] })

    render(<ContainersContent />)

    await waitFor(() => expect(screen.getByTestId('containers-content')).toBeInTheDocument())
    expect(screen.queryByText(paymentsApi.id)).not.toBeInTheDocument()
  })

  describe('empty vs unavailable', () => {
    it('shows the honest empty state when the API returns zero containers', async () => {
      fetchContainersMock.mockResolvedValue({ total_count: 0, containers: [] })

      render(<ContainersContent />)

      await waitFor(() => expect(screen.getByTestId('containers-empty')).toBeInTheDocument())
      expect(screen.getByText('No managed containers')).toBeInTheDocument()
      expect(screen.queryByTestId('containers-error')).not.toBeInTheDocument()
    })

    it('shows the unavailable/error state when the request fails, distinct from zero', async () => {
      fetchContainersMock.mockRejectedValue(new Error('Failed to load containers (500)'))

      render(<ContainersContent />)

      await waitFor(() => expect(screen.getByTestId('containers-error')).toBeInTheDocument())
      expect(screen.getByText('Unable to load containers')).toBeInTheDocument()
      expect(screen.queryByTestId('containers-empty')).not.toBeInTheDocument()
    })

    it('never renders the managed-containers summary when the request failed', async () => {
      fetchContainersMock.mockRejectedValue(new Error('network error'))

      render(<ContainersContent />)

      await waitFor(() => expect(screen.getByTestId('containers-error')).toBeInTheDocument())
      expect(screen.queryByTestId('containers-summary')).not.toBeInTheDocument()
    })

    it('renders "Not available" rather than fabricating missing Docker metadata', async () => {
      fetchContainersMock.mockResolvedValue({
        total_count: 1,
        containers: [{ id: 'stale-container-id', strategy: 'restart', secrets: [], degraded: false }],
      })

      render(<ContainersContent />)

      await waitFor(() => expect(screen.getByTestId('containers-content')).toBeInTheDocument())
      expect(screen.getAllByText('Not available').length).toBeGreaterThan(0)
    })
  })

  it('never renders a plaintext/env field even if the backend response includes one', async () => {
    fetchContainersMock.mockResolvedValue({
      total_count: 1,
      containers: [
        {
          ...paymentsApi,
          env: ['DB_PASSWORD=super-secret-plaintext-value'],
          config: { env: ['TOKEN=another-secret-plaintext-value'] },
        },
      ],
    })

    render(<ContainersContent />)

    await waitFor(() => expect(screen.getByTestId('containers-content')).toBeInTheDocument())
    expect(screen.queryByText(/super-secret-plaintext-value/)).not.toBeInTheDocument()
    expect(screen.queryByText(/another-secret-plaintext-value/)).not.toBeInTheDocument()
  })

  it('never renders a sentinel value anywhere in the page', async () => {
    const sentinel = 'DSO_CONTAINER_SECRET_SENTINEL_4a71e2'
    fetchContainersMock.mockResolvedValue({
      total_count: 1,
      containers: [{ ...paymentsApi, env: [`TOKEN=${sentinel}`], degraded_reason: undefined }],
    })

    render(<ContainersContent />)

    await waitFor(() => expect(screen.getByTestId('containers-content')).toBeInTheDocument())
    expect(document.body.textContent ?? '').not.toContain(sentinel)
  })

  describe('search', () => {
    it('narrows visible rows by name, image, or ID, and restores them when cleared', async () => {
      fetchContainersMock.mockResolvedValue({ total_count: 2, containers: [paymentsApi, oldWorker] })

      const user = userEvent.setup()
      render(<ContainersContent />)

      await waitFor(() => expect(screen.getByTestId('containers-content')).toBeInTheDocument())
      expect(screen.getByText('payments-api')).toBeInTheDocument()
      expect(screen.getByText('old-worker')).toBeInTheDocument()

      const search = screen.getByTestId('containers-search')
      await user.type(search, 'payments')

      expect(screen.getByText('payments-api')).toBeInTheDocument()
      expect(screen.queryByText('old-worker')).not.toBeInTheDocument()

      await user.clear(search)

      expect(screen.getByText('payments-api')).toBeInTheDocument()
      expect(screen.getByText('old-worker')).toBeInTheDocument()
    })

    it('shows a distinct "no match" state when the search matches nothing', async () => {
      fetchContainersMock.mockResolvedValue({ total_count: 1, containers: [paymentsApi] })

      const user = userEvent.setup()
      render(<ContainersContent />)

      await waitFor(() => expect(screen.getByTestId('containers-content')).toBeInTheDocument())
      await user.type(screen.getByTestId('containers-search'), 'nonexistent')

      expect(screen.getByTestId('containers-no-match')).toBeInTheDocument()
    })
  })

  describe('filters', () => {
    it('filters by status', async () => {
      fetchContainersMock.mockResolvedValue({ total_count: 2, containers: [paymentsApi, oldWorker] })
      const user = userEvent.setup()
      render(<ContainersContent />)

      await waitFor(() => expect(screen.getByTestId('containers-content')).toBeInTheDocument())
      await user.selectOptions(screen.getByTestId('filter-status'), 'exited')

      expect(screen.queryByText('payments-api')).not.toBeInTheDocument()
      expect(screen.getByText('old-worker')).toBeInTheDocument()
    })

    it('filters by health', async () => {
      fetchContainersMock.mockResolvedValue({ total_count: 2, containers: [paymentsApi, oldWorker] })
      const user = userEvent.setup()
      render(<ContainersContent />)

      await waitFor(() => expect(screen.getByTestId('containers-content')).toBeInTheDocument())
      await user.selectOptions(screen.getByTestId('filter-health'), 'healthy')

      expect(screen.getByText('payments-api')).toBeInTheDocument()
      expect(screen.queryByText('old-worker')).not.toBeInTheDocument()
    })

    it('filters by DSO degraded state', async () => {
      fetchContainersMock.mockResolvedValue({ total_count: 2, containers: [paymentsApi, oldWorker] })
      const user = userEvent.setup()
      render(<ContainersContent />)

      await waitFor(() => expect(screen.getByTestId('containers-content')).toBeInTheDocument())
      await user.selectOptions(screen.getByTestId('filter-dso-state'), 'degraded')

      expect(screen.queryByText('payments-api')).not.toBeInTheDocument()
      expect(screen.getByText('old-worker')).toBeInTheDocument()
    })

    it('shows a clear-filters action only when a filter is active, and it resets state', async () => {
      fetchContainersMock.mockResolvedValue({ total_count: 2, containers: [paymentsApi, oldWorker] })
      const user = userEvent.setup()
      render(<ContainersContent />)

      await waitFor(() => expect(screen.getByTestId('containers-content')).toBeInTheDocument())
      expect(screen.queryByTestId('clear-filters')).not.toBeInTheDocument()

      await user.selectOptions(screen.getByTestId('filter-dso-state'), 'degraded')
      expect(screen.getByTestId('clear-filters')).toBeInTheDocument()

      await user.click(screen.getByTestId('clear-filters'))
      expect(screen.queryByTestId('clear-filters')).not.toBeInTheDocument()
      expect(screen.getByText('payments-api')).toBeInTheDocument()
    })
  })

  it('renders status and health with icon + text via the shared StatusBadge, not color alone', async () => {
    fetchContainersMock.mockResolvedValue({ total_count: 1, containers: [paymentsApi] })

    render(<ContainersContent />)

    await waitFor(() => expect(screen.getByTestId('containers-content')).toBeInTheDocument())
    const table = screen.getByRole('table')
    const statusCell = within(table).getByText('running')
    expect(statusCell.closest('div')?.querySelector('svg')).toBeTruthy()
    const healthCell = within(table).getByText('healthy')
    expect(healthCell.closest('div')?.querySelector('svg')).toBeTruthy()
  })

  it('shows "No healthcheck" rather than fabricating "healthy" for a running container with no healthcheck', async () => {
    fetchContainersMock.mockResolvedValue({
      total_count: 1,
      containers: [{ ...paymentsApi, health: '' }],
    })

    render(<ContainersContent />)

    await waitFor(() => expect(screen.getByTestId('containers-content')).toBeInTheDocument())
    expect(screen.getByText('No healthcheck')).toBeInTheDocument()
  })

  it('shows a prominent DSO-degraded indicator, separate from Docker status/health', async () => {
    fetchContainersMock.mockResolvedValue({ total_count: 1, containers: [oldWorker] })

    render(<ContainersContent />)

    await waitFor(() => expect(screen.getByTestId('containers-content')).toBeInTheDocument())
    expect(screen.getByText('DSO degraded')).toBeInTheDocument()
    expect(screen.getByText(/rotation failed, still serving previous secret/)).toBeInTheDocument()
  })

  it('shows the secret count, not secret values', async () => {
    fetchContainersMock.mockResolvedValue({ total_count: 1, containers: [paymentsApi] })

    render(<ContainersContent />)

    await waitFor(() => expect(screen.getByTestId('containers-content')).toBeInTheDocument())
    expect(screen.getByText('2 secrets')).toBeInTheDocument()
  })

  it('shows published ports formatted as host->container/proto', async () => {
    fetchContainersMock.mockResolvedValue({ total_count: 1, containers: [paymentsApi] })

    render(<ContainersContent />)

    await waitFor(() => expect(screen.getByTestId('containers-content')).toBeInTheDocument())
    expect(screen.getByText('8080->80/tcp')).toBeInTheDocument()
  })

  it('expands a row to show safe metadata detail, including the full ID and degraded reason', async () => {
    const user = userEvent.setup()
    fetchContainersMock.mockResolvedValue({ total_count: 1, containers: [oldWorker] })
    render(<ContainersContent />)

    await waitFor(() => expect(screen.getByTestId('containers-content')).toBeInTheDocument())
    await user.click(screen.getByText('old-worker'))

    const detail = screen.getByTestId(`container-detail-${oldWorker.id}`)
    expect(within(detail).getByText(oldWorker.id)).toBeInTheDocument()
    expect(within(detail).getByText('rotation failed, still serving previous secret')).toBeInTheDocument()
    expect(within(detail).queryByText(/env/i)).not.toBeInTheDocument()
  })

  it('sorts by container name when the Container column header is activated', async () => {
    fetchContainersMock.mockResolvedValue({ total_count: 2, containers: [paymentsApi, oldWorker] })
    const user = userEvent.setup()
    render(<ContainersContent />)

    await waitFor(() => expect(screen.getByTestId('containers-content')).toBeInTheDocument())
    const rowsBefore = screen.getAllByRole('row').map((r) => r.textContent)
    // Default sort is ascending by name: old-worker before payments-api.
    expect(rowsBefore.findIndex((t) => t?.includes('old-worker'))).toBeLessThan(
      rowsBefore.findIndex((t) => t?.includes('payments-api'))
    )

    await user.click(screen.getByRole('button', { name: /container/i }))

    const rowsAfter = screen.getAllByRole('row').map((r) => r.textContent)
    expect(rowsAfter.findIndex((t) => t?.includes('payments-api'))).toBeLessThan(
      rowsAfter.findIndex((t) => t?.includes('old-worker'))
    )
  })
})
