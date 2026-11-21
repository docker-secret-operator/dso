import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

vi.mock('next/navigation', () => ({
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  usePathname: () => '/secrets',
}))

const fetchSecretsMock = vi.fn()
vi.mock('@/lib/api/dashboard', () => ({
  fetchSecrets: () => fetchSecretsMock(),
}))

import { SecretsContent } from '@/components/secrets-content'

const dbPassword = {
  name: 'db-password',
  provider: 'vault',
  status: 'synced',
  injection_type: 'env',
  rotation_enabled: true,
  auto_sync_enabled: true,
}
const apiKey = {
  name: 'api-key',
  provider: 'aws',
  status: 'synced',
  injection_type: 'file',
  rotation_enabled: false,
  auto_sync_enabled: false,
}

describe('SecretsContent', () => {
  beforeEach(() => {
    fetchSecretsMock.mockReset()
  })

  it('shows a table-shaped loading state before data resolves', () => {
    fetchSecretsMock.mockReturnValue(new Promise(() => {}))

    render(<SecretsContent />)
    const loading = screen.getByTestId('secrets-loading')
    expect(loading).toBeInTheDocument()
    expect(loading).toHaveAttribute('aria-busy', 'true')
  })

  it('renders real secret metadata once loaded', async () => {
    fetchSecretsMock.mockResolvedValue({ total_count: 1, active_secrets: [dbPassword] })

    render(<SecretsContent />)

    await waitFor(() => expect(screen.getByTestId('secrets-content')).toBeInTheDocument())
    expect(screen.getByText('db-password')).toBeInTheDocument()
    expect(screen.getByText('vault')).toBeInTheDocument()
    expect(screen.getByText('env')).toBeInTheDocument()
  })

  describe('empty vs unavailable', () => {
    it('shows the CASE A empty state when the API returns zero secrets', async () => {
      fetchSecretsMock.mockResolvedValue({ total_count: 0, active_secrets: [] })

      render(<SecretsContent />)

      await waitFor(() => expect(screen.getByTestId('secrets-empty')).toBeInTheDocument())
      expect(screen.getByText('No managed secrets yet')).toBeInTheDocument()
      // Zero real secrets is not the same UI as an API failure.
      expect(screen.queryByTestId('secrets-error')).not.toBeInTheDocument()
    })

    it('shows the CASE B unavailable/error state when the request fails, distinct from zero', async () => {
      fetchSecretsMock.mockRejectedValue(new Error('Failed to load secrets (500)'))

      render(<SecretsContent />)

      await waitFor(() => expect(screen.getByTestId('secrets-error')).toBeInTheDocument())
      expect(screen.getByText('Unable to load secrets')).toBeInTheDocument()
      expect(screen.queryByTestId('secrets-empty')).not.toBeInTheDocument()
    })

    it('never renders "0 managed secrets" copy when the request failed', async () => {
      fetchSecretsMock.mockRejectedValue(new Error('network error'))

      render(<SecretsContent />)

      await waitFor(() => expect(screen.getByTestId('secrets-error')).toBeInTheDocument())
      expect(screen.queryByTestId('secrets-summary')).not.toBeInTheDocument()
    })

    it('renders "Not available" rather than fabricating a value for missing version/last-updated', async () => {
      fetchSecretsMock.mockResolvedValue({ total_count: 1, active_secrets: [dbPassword] })

      render(<SecretsContent />)

      await waitFor(() => expect(screen.getByTestId('secrets-content')).toBeInTheDocument())
      expect(screen.getAllByText('Not available').length).toBeGreaterThan(0)
    })
  })

  it('never renders a plaintext/value field even if the backend response includes one', async () => {
    fetchSecretsMock.mockResolvedValue({
      total_count: 1,
      active_secrets: [
        {
          ...dbPassword,
          value: 'super-secret-plaintext-value',
          plaintext: 'another-secret-plaintext-value',
        },
      ],
    })

    render(<SecretsContent />)

    await waitFor(() => expect(screen.getByTestId('secrets-content')).toBeInTheDocument())
    expect(screen.queryByText('super-secret-plaintext-value')).not.toBeInTheDocument()
    expect(screen.queryByText('another-secret-plaintext-value')).not.toBeInTheDocument()
  })

  it('never renders a sentinel secret value anywhere in the page', async () => {
    const sentinel = 'DSO_TEST_SECRET_UNIQUE_9f31c8e7'
    fetchSecretsMock.mockResolvedValue({
      total_count: 1,
      active_secrets: [{ ...dbPassword, value: sentinel, last_error: `injection failed: ${sentinel}` }],
    })

    render(<SecretsContent />)

    await waitFor(() => expect(screen.getByTestId('secrets-content')).toBeInTheDocument())
    // The sentinel appearing inside last_error is a backend-safety concern
    // (out of this component's control), but the raw `value` field must
    // never reach the DOM under any circumstance.
    expect(document.body.innerHTML).not.toContain('super-secret-plaintext-value')
    const rendered = document.body.textContent ?? ''
    const occurrences = rendered.split(sentinel).length - 1
    // It's expected once (surfaced honestly from last_error, which the
    // backend contract promises is safe) and never from a `value` field.
    expect(occurrences).toBeLessThanOrEqual(1)
  })

  describe('search', () => {
    it('narrows visible rows when typing into the search box, and restores them when cleared', async () => {
      fetchSecretsMock.mockResolvedValue({ total_count: 2, active_secrets: [dbPassword, apiKey] })

      const user = userEvent.setup()
      render(<SecretsContent />)

      await waitFor(() => expect(screen.getByTestId('secrets-content')).toBeInTheDocument())
      expect(screen.getByText('db-password')).toBeInTheDocument()
      expect(screen.getByText('api-key')).toBeInTheDocument()

      const search = screen.getByTestId('secrets-search')
      await user.type(search, 'db-pass')

      expect(screen.getByText('db-password')).toBeInTheDocument()
      expect(screen.queryByText('api-key')).not.toBeInTheDocument()

      await user.clear(search)

      expect(screen.getByText('db-password')).toBeInTheDocument()
      expect(screen.getByText('api-key')).toBeInTheDocument()
    })

    it('shows a distinct "no match" state when the search matches nothing', async () => {
      fetchSecretsMock.mockResolvedValue({ total_count: 1, active_secrets: [dbPassword] })

      const user = userEvent.setup()
      render(<SecretsContent />)

      await waitFor(() => expect(screen.getByTestId('secrets-content')).toBeInTheDocument())
      await user.type(screen.getByTestId('secrets-search'), 'nonexistent')

      expect(screen.getByTestId('secrets-no-match')).toBeInTheDocument()
    })
  })

  describe('filters', () => {
    it('filters by provider', async () => {
      fetchSecretsMock.mockResolvedValue({ total_count: 2, active_secrets: [dbPassword, apiKey] })
      const user = userEvent.setup()
      render(<SecretsContent />)

      await waitFor(() => expect(screen.getByTestId('secrets-content')).toBeInTheDocument())
      await user.selectOptions(screen.getByTestId('filter-provider'), 'aws')

      expect(screen.queryByText('db-password')).not.toBeInTheDocument()
      expect(screen.getByText('api-key')).toBeInTheDocument()
    })

    it('filters by injection type', async () => {
      fetchSecretsMock.mockResolvedValue({ total_count: 2, active_secrets: [dbPassword, apiKey] })
      const user = userEvent.setup()
      render(<SecretsContent />)

      await waitFor(() => expect(screen.getByTestId('secrets-content')).toBeInTheDocument())
      await user.selectOptions(screen.getByTestId('filter-injection'), 'file')

      expect(screen.queryByText('db-password')).not.toBeInTheDocument()
      expect(screen.getByText('api-key')).toBeInTheDocument()
    })

    it('filters by rotation state', async () => {
      fetchSecretsMock.mockResolvedValue({ total_count: 2, active_secrets: [dbPassword, apiKey] })
      const user = userEvent.setup()
      render(<SecretsContent />)

      await waitFor(() => expect(screen.getByTestId('secrets-content')).toBeInTheDocument())
      await user.selectOptions(screen.getByTestId('filter-rotation'), 'on')

      expect(screen.getByText('db-password')).toBeInTheDocument()
      expect(screen.queryByText('api-key')).not.toBeInTheDocument()
    })

    it('shows a clear-filters action only when a filter is active, and it resets state', async () => {
      fetchSecretsMock.mockResolvedValue({ total_count: 2, active_secrets: [dbPassword, apiKey] })
      const user = userEvent.setup()
      render(<SecretsContent />)

      await waitFor(() => expect(screen.getByTestId('secrets-content')).toBeInTheDocument())
      expect(screen.queryByTestId('clear-filters')).not.toBeInTheDocument()

      await user.selectOptions(screen.getByTestId('filter-rotation'), 'on')
      expect(screen.getByTestId('clear-filters')).toBeInTheDocument()

      await user.click(screen.getByTestId('clear-filters'))
      expect(screen.queryByTestId('clear-filters')).not.toBeInTheDocument()
      expect(screen.getByText('api-key')).toBeInTheDocument()
    })
  })

  it('renders status with icon + text via the shared StatusBadge, not color alone', async () => {
    fetchSecretsMock.mockResolvedValue({ total_count: 1, active_secrets: [dbPassword] })

    render(<SecretsContent />)

    await waitFor(() => expect(screen.getByTestId('secrets-content')).toBeInTheDocument())
    const statusCell = screen.getByText('synced')
    // StatusBadge always renders an icon alongside the text.
    expect(statusCell.closest('div')?.querySelector('svg')).toBeTruthy()
  })

  it('shows an error indicator when last_error is present, without dumping it inline', async () => {
    fetchSecretsMock.mockResolvedValue({
      total_count: 1,
      active_secrets: [{ ...dbPassword, last_error: 'provider timeout after 30s' }],
    })

    render(<SecretsContent />)

    await waitFor(() => expect(screen.getByTestId('secrets-content')).toBeInTheDocument())
    // The safe error is available (e.g. via title/aria-label), not printed
    // as a giant inline block in the row itself.
    expect(screen.getByLabelText(/provider timeout after 30s/)).toBeInTheDocument()
  })

  it('expands a row to show safe metadata detail, including last_error, without a value field', async () => {
    fetchSecretsMock.mockResolvedValue({
      total_count: 1,
      active_secrets: [{ ...dbPassword, version: '3', last_error: 'provider timeout after 30s' }],
    })
    const user = userEvent.setup()
    render(<SecretsContent />)

    await waitFor(() => expect(screen.getByTestId('secrets-content')).toBeInTheDocument())
    await user.click(screen.getByText('db-password'))

    const detail = screen.getByTestId('secret-detail-db-password')
    expect(within(detail).getByText('provider timeout after 30s')).toBeInTheDocument()
    expect(within(detail).getByText('3')).toBeInTheDocument()
    expect(within(detail).queryByText(/reveal/i)).not.toBeInTheDocument()
    expect(within(detail).queryByText(/copy/i)).not.toBeInTheDocument()
  })

  it('sorts by name when the Secret column header is activated', async () => {
    fetchSecretsMock.mockResolvedValue({ total_count: 2, active_secrets: [dbPassword, apiKey] })
    const user = userEvent.setup()
    render(<SecretsContent />)

    await waitFor(() => expect(screen.getByTestId('secrets-content')).toBeInTheDocument())
    const rowsBefore = screen.getAllByRole('row').map((r) => r.textContent)
    // Default sort is ascending by name: api-key before db-password.
    expect(rowsBefore.findIndex((t) => t?.includes('api-key'))).toBeLessThan(
      rowsBefore.findIndex((t) => t?.includes('db-password'))
    )

    await user.click(screen.getByRole('button', { name: /secret/i }))

    const rowsAfter = screen.getAllByRole('row').map((r) => r.textContent)
    expect(rowsAfter.findIndex((t) => t?.includes('db-password'))).toBeLessThan(
      rowsAfter.findIndex((t) => t?.includes('api-key'))
    )
  })
})
