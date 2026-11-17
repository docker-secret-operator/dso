import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

vi.mock('next/navigation', () => ({
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  usePathname: () => '/audit',
}))

const fetchAuditMock = vi.fn()
const fetchAuditStatusMock = vi.fn()
vi.mock('@/lib/api/audit', () => ({
  fetchAudit: () => fetchAuditMock(),
  fetchAuditStatus: () => fetchAuditStatusMock(),
}))

import { AuditContent } from '@/components/audit-content'

const secretFetch = {
  timestamp: '2026-08-14T10:00:00Z',
  secret: 'db-password',
  event_type: 'secret_fetch',
  status: 'success',
  user: 'alice',
}
const rotationFailed = {
  timestamp: '2026-08-14T09:00:00Z',
  secret: 'api-key',
  container: 'a84f2c91e73b4c1a9e2f6d8b0c3a5f7e9d1b3c5a7f9e1d3b5c7a9f1e3d5b7c9a',
  event_type: 'rotation_failed',
  status: 'failure',
  error: 'provider timeout after 30s',
  // No "user" field -- rotation events never carry an actor today.
}

describe('AuditContent', () => {
  beforeEach(() => {
    fetchAuditMock.mockReset()
    fetchAuditStatusMock.mockReset()
    fetchAuditStatusMock.mockResolvedValue(null)
  })

  it('shows a table-shaped loading state before the request resolves', () => {
    fetchAuditMock.mockReturnValue(new Promise(() => {}))

    render(<AuditContent />)
    const loading = screen.getByTestId('audit-loading')
    expect(loading).toBeInTheDocument()
    expect(loading).toHaveAttribute('aria-busy', 'true')
  })

  it('renders real audit data loaded from GET /api/audit', async () => {
    fetchAuditMock.mockResolvedValue([secretFetch])

    render(<AuditContent />)

    await waitFor(() => expect(screen.getByTestId('audit-content')).toBeInTheDocument())
    expect(screen.getByText('db-password')).toBeInTheDocument()
    expect(screen.getByText('secret fetch')).toBeInTheDocument()
  })

  it('shows the honest empty state when the API returns zero records', async () => {
    fetchAuditMock.mockResolvedValue([])

    render(<AuditContent />)

    await waitFor(() => expect(screen.getByTestId('audit-empty')).toBeInTheDocument())
    expect(screen.getByText('No audit history yet')).toBeInTheDocument()
    expect(screen.queryByTestId('audit-error')).not.toBeInTheDocument()
  })

  it('shows the unavailable/error state when the request fails, distinct from empty', async () => {
    fetchAuditMock.mockRejectedValue(new Error('Failed to load audit history (500)'))

    render(<AuditContent />)

    await waitFor(() => expect(screen.getByTestId('audit-error')).toBeInTheDocument())
    expect(screen.getByText('Unable to load audit history')).toBeInTheDocument()
    expect(screen.queryByTestId('audit-empty')).not.toBeInTheDocument()
  })

  it('renders result with icon + text via the shared StatusBadge, not color alone', async () => {
    fetchAuditMock.mockResolvedValue([secretFetch])

    render(<AuditContent />)

    await waitFor(() => expect(screen.getByTestId('audit-content')).toBeInTheDocument())
    const table = screen.getByRole('table')
    const resultCell = within(table).getByText('success')
    expect(resultCell.closest('div')?.querySelector('svg')).toBeTruthy()
  })

  it('shows a real actor when the backend provides one', async () => {
    fetchAuditMock.mockResolvedValue([secretFetch])

    render(<AuditContent />)

    await waitFor(() => expect(screen.getByTestId('audit-content')).toBeInTheDocument())
    expect(screen.getByText('alice')).toBeInTheDocument()
  })

  it('never fabricates "System" or any other actor when the backend record has none', async () => {
    fetchAuditMock.mockResolvedValue([rotationFailed])

    render(<AuditContent />)

    await waitFor(() => expect(screen.getByTestId('audit-content')).toBeInTheDocument())
    expect(screen.queryByText('System')).not.toBeInTheDocument()
    const table = screen.getByRole('table')
    expect(within(table).getAllByText('Not available').length).toBeGreaterThan(0)
  })

  it('renders an exact timestamp (not a fabricated relative one is required, but the real time must show)', async () => {
    fetchAuditMock.mockResolvedValue([secretFetch])

    render(<AuditContent />)

    await waitFor(() => expect(screen.getByTestId('audit-content')).toBeInTheDocument())
    expect(screen.getByText(new Date(secretFetch.timestamp).toLocaleString())).toBeInTheDocument()
  })

  describe('search and filters', () => {
    it('narrows visible rows via search', async () => {
      fetchAuditMock.mockResolvedValue([secretFetch, rotationFailed])
      const user = userEvent.setup()
      render(<AuditContent />)

      await waitFor(() => expect(screen.getByTestId('audit-content')).toBeInTheDocument())
      await user.type(screen.getByTestId('audit-search'), 'alice')

      expect(screen.getByText('db-password')).toBeInTheDocument()
      expect(screen.queryByText('api-key')).not.toBeInTheDocument()
    })

    it('shows a distinct "no match" state when the search matches nothing', async () => {
      fetchAuditMock.mockResolvedValue([secretFetch])
      const user = userEvent.setup()
      render(<AuditContent />)

      await waitFor(() => expect(screen.getByTestId('audit-content')).toBeInTheDocument())
      await user.type(screen.getByTestId('audit-search'), 'nonexistent')

      expect(screen.getByTestId('audit-no-match')).toBeInTheDocument()
    })

    it('filters by result', async () => {
      fetchAuditMock.mockResolvedValue([secretFetch, rotationFailed])
      const user = userEvent.setup()
      render(<AuditContent />)

      await waitFor(() => expect(screen.getByTestId('audit-content')).toBeInTheDocument())
      await user.selectOptions(screen.getByTestId('filter-status'), 'failure')

      expect(screen.queryByText('db-password')).not.toBeInTheDocument()
      expect(screen.getByText('api-key')).toBeInTheDocument()
    })

    it('filters by action', async () => {
      fetchAuditMock.mockResolvedValue([secretFetch, rotationFailed])
      const user = userEvent.setup()
      render(<AuditContent />)

      await waitFor(() => expect(screen.getByTestId('audit-content')).toBeInTheDocument())
      await user.selectOptions(screen.getByTestId('filter-action'), 'secret_fetch')

      expect(screen.getByText('db-password')).toBeInTheDocument()
      expect(screen.queryByText('api-key')).not.toBeInTheDocument()
    })
  })

  it('expands a row to show safe metadata detail, including actor and message', async () => {
    fetchAuditMock.mockResolvedValue([rotationFailed])
    const user = userEvent.setup()
    render(<AuditContent />)

    await waitFor(() => expect(screen.getByTestId('audit-content')).toBeInTheDocument())
    await user.click(screen.getByText('rotation failed'))

    const detail = screen.getByTestId(/audit-detail-/)
    expect(within(detail).getByText('provider timeout after 30s')).toBeInTheDocument()
    expect(within(detail).queryByText(/reveal/i)).not.toBeInTheDocument()
    expect(within(detail).queryByText(/copy/i)).not.toBeInTheDocument()
  })

  it('never renders a plaintext/value field even if the backend response includes one', async () => {
    fetchAuditMock.mockResolvedValue([{ ...secretFetch, value: 'super-secret-plaintext-value', plaintext: 'another-secret-plaintext-value' }])

    render(<AuditContent />)

    await waitFor(() => expect(screen.getByTestId('audit-content')).toBeInTheDocument())
    expect(screen.queryByText(/super-secret-plaintext-value/)).not.toBeInTheDocument()
    expect(screen.queryByText(/another-secret-plaintext-value/)).not.toBeInTheDocument()
  })

  it('never renders the audit-secret sentinel anywhere except an explicitly safe field', async () => {
    const sentinel = 'DSO_AUDIT_SECRET_SENTINEL_8b43c1'
    fetchAuditMock.mockResolvedValue([{ ...secretFetch, value: sentinel }])

    render(<AuditContent />)

    await waitFor(() => expect(screen.getByTestId('audit-content')).toBeInTheDocument())
    expect(document.body.textContent ?? '').not.toContain(sentinel)
  })

  it('does not claim durability when status is unknown or disabled', async () => {
    fetchAuditMock.mockResolvedValue([secretFetch])
    fetchAuditStatusMock.mockResolvedValue({ durable_enabled: false, write_failures: 0 })
    render(<AuditContent />)
    const note = await screen.findByTestId('audit-durable-status')
    expect(note).toHaveTextContent(/no durable audit file/i)
    expect(screen.queryByTestId('audit-export-link')).not.toBeInTheDocument()
  })

  it('shows the durable file, its limits and an export link when active', async () => {
    fetchAuditMock.mockResolvedValue([secretFetch])
    fetchAuditStatusMock.mockResolvedValue({ durable_enabled: true, write_failures: 0 })
    render(<AuditContent />)
    const note = await screen.findByText(/separate durable audit file is active/i)
    expect(note).toHaveTextContent(/best effort, not tamper-proof/i)
    expect(screen.getByTestId('audit-export-link')).toHaveAttribute('href', '/api/audit/export')
    expect(screen.queryByTestId('audit-durable-failures')).not.toBeInTheDocument()
  })

  it('surfaces persistence failures', async () => {
    fetchAuditMock.mockResolvedValue([secretFetch])
    fetchAuditStatusMock.mockResolvedValue({ durable_enabled: true, write_failures: 3 })
    render(<AuditContent />)
    expect(await screen.findByTestId('audit-durable-failures')).toHaveTextContent('3 event(s) could not be persisted')
  })
})
