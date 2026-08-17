import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'

vi.mock('next/navigation', () => ({
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  usePathname: () => '/secrets',
}))

const fetchSecretsMock = vi.fn()
vi.mock('@/lib/api/dashboard', () => ({
  fetchSecrets: () => fetchSecretsMock(),
}))

import { SecretsContent } from '@/components/secrets-content'

describe('SecretsContent', () => {
  beforeEach(() => {
    fetchSecretsMock.mockReset()
  })

  it('shows a loading state before data resolves', () => {
    fetchSecretsMock.mockReturnValue(new Promise(() => {}))

    render(<SecretsContent />)
    expect(screen.getByTestId('secrets-loading')).toBeInTheDocument()
  })

  it('renders real secret metadata once loaded', async () => {
    fetchSecretsMock.mockResolvedValue({
      total_count: 1,
      active_secrets: [
        { name: 'db-password', provider: 'vault', status: 'synced', injection_type: 'env', rotation_enabled: true, auto_sync_enabled: true },
      ],
    })

    render(<SecretsContent />)

    await waitFor(() => expect(screen.getByTestId('secrets-content')).toBeInTheDocument())
    expect(screen.getByText('db-password')).toBeInTheDocument()
    expect(screen.getByText('vault')).toBeInTheDocument()
    expect(screen.getByText('synced')).toBeInTheDocument()
  })

  it('shows an honest empty state when there are no secrets', async () => {
    fetchSecretsMock.mockResolvedValue({ total_count: 0, active_secrets: [] })

    render(<SecretsContent />)

    await waitFor(() => expect(screen.getByTestId('secrets-empty')).toBeInTheDocument())
  })

  it('shows an error state with a retry option when the fetch fails', async () => {
    fetchSecretsMock.mockRejectedValue(new Error('Failed to load secrets (500)'))

    render(<SecretsContent />)

    await waitFor(() => expect(screen.getByTestId('secrets-error')).toBeInTheDocument())
    expect(screen.getByText(/failed to load secrets/i)).toBeInTheDocument()
  })

  it('never renders a plaintext/value field even if the backend response includes one', async () => {
    // Simulates a hypothetical malicious/misbehaving backend response that
    // includes a plaintext-looking field. SecretsContent only destructures
    // the declared SecretSummary fields (name/provider/status/injection_type/
    // rotation_enabled) -- it has no code path that would render `value` or
    // `plaintext`, so this asserts that guarantee holds even under a
    // response shape the real backend should never produce.
    fetchSecretsMock.mockResolvedValue({
      total_count: 1,
      active_secrets: [
        {
          name: 'db-password',
          provider: 'vault',
          status: 'synced',
          injection_type: 'env',
          rotation_enabled: true,
          auto_sync_enabled: true,
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
})
