import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

vi.mock('next/navigation', () => ({
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  usePathname: () => '/secrets',
}))

const fetchSecretsMock = vi.fn()
const rotateSecretMock = vi.fn()
vi.mock('@/lib/api/dashboard', () => ({
  fetchSecrets: () => fetchSecretsMock(),
  rotateSecret: (name: string) => rotateSecretMock(name),
}))

const useWebSocketMock = vi.fn()
vi.mock('@/hooks/useWebSocket', () => ({
  useWebSocket: (...args: unknown[]) => useWebSocketMock(...args),
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

describe('SecretsContent rotate action', () => {
  beforeEach(() => {
    fetchSecretsMock.mockReset()
    rotateSecretMock.mockReset()
    useWebSocketMock.mockReset()
    useWebSocketMock.mockReturnValue({ events: [], connectionState: 'connected' })
    fetchSecretsMock.mockResolvedValue({ active_secrets: [dbPassword], total_count: 1 })
  })

  it('renders a Rotate button per secret', async () => {
    render(<SecretsContent />)
    expect(await screen.findByTestId('rotate-button-db-password')).toBeInTheDocument()
  })

  it('shows a confirmation dialog on click, naming the secret', async () => {
    const user = userEvent.setup()
    render(<SecretsContent />)
    await user.click(await screen.findByTestId('rotate-button-db-password'))

    const dialog = await screen.findByTestId('rotate-confirm-dialog')
    expect(dialog).toHaveTextContent('db-password')
    expect(rotateSecretMock).not.toHaveBeenCalled()
  })

  it('Cancel does not call the rotate API', async () => {
    const user = userEvent.setup()
    render(<SecretsContent />)
    await user.click(await screen.findByTestId('rotate-button-db-password'))
    await user.click(await screen.findByTestId('rotate-cancel'))

    expect(screen.queryByTestId('rotate-confirm-dialog')).not.toBeInTheDocument()
    expect(rotateSecretMock).not.toHaveBeenCalled()
  })

  it('confirming calls rotateSecret with the correct secret name', async () => {
    rotateSecretMock.mockResolvedValue({ status: 'accepted', secret: 'db-password' })
    const user = userEvent.setup()
    render(<SecretsContent />)
    await user.click(await screen.findByTestId('rotate-button-db-password'))
    await user.click(await screen.findByTestId('rotate-confirm'))

    await waitFor(() => expect(rotateSecretMock).toHaveBeenCalledWith('db-password'))
  })

  it('shows an in-progress state after the POST accepts, before a WS outcome arrives', async () => {
    let resolvePost: (v: unknown) => void = () => {}
    rotateSecretMock.mockReturnValue(new Promise((resolve) => (resolvePost = resolve)))
    const user = userEvent.setup()
    render(<SecretsContent />)
    await user.click(await screen.findByTestId('rotate-button-db-password'))
    await user.click(await screen.findByTestId('rotate-confirm'))

    resolvePost({ status: 'accepted', secret: 'db-password' })
    await waitFor(() =>
      expect(screen.getByTestId('rotate-status-db-password')).toHaveTextContent('Rotating')
    )
  })

  it('reflects success only from a real rotation_succeeded WebSocket event', async () => {
    useWebSocketMock.mockReturnValue({
      events: [{ timestamp: '2026-01-01T00:00:00Z', secret: 'db-password', event_type: 'rotation_succeeded', status: 'success' }],
      connectionState: 'connected',
    })
    render(<SecretsContent />)
    await waitFor(() =>
      expect(screen.getByTestId('rotate-status-db-password')).toHaveTextContent('Rotated')
    )
  })

  it('reflects failure only from a real rotation_failed WebSocket event', async () => {
    useWebSocketMock.mockReturnValue({
      events: [{ timestamp: '2026-01-01T00:00:00Z', secret: 'db-password', event_type: 'rotation_failed', status: 'failure' }],
      connectionState: 'connected',
    })
    render(<SecretsContent />)
    await waitFor(() =>
      expect(screen.getByTestId('rotate-status-db-password')).toHaveTextContent('Rotation failed')
    )
  })

  it('shows a failure state when the POST itself rejects', async () => {
    rotateSecretMock.mockRejectedValue(new Error('Unknown secret'))
    const user = userEvent.setup()
    render(<SecretsContent />)
    await user.click(await screen.findByTestId('rotate-button-db-password'))
    await user.click(await screen.findByTestId('rotate-confirm'))

    await waitFor(() =>
      expect(screen.getByTestId('rotate-status-db-password')).toHaveTextContent('Rotation failed')
    )
  })

  it('never renders a secret value anywhere on the page', async () => {
    render(<SecretsContent />)
    await screen.findByTestId('rotate-button-db-password')
    expect(document.body.textContent?.toLowerCase()).not.toContain('plaintext')
  })
})
