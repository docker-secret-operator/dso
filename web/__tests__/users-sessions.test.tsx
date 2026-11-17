import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

const fetchSessionsMock = vi.fn()
const revokeSessionMock = vi.fn()
const revokeOthersMock = vi.fn()
vi.mock('@/lib/api/sessions', () => ({
  fetchSessions: () => fetchSessionsMock(),
  revokeSession: (id: string) => revokeSessionMock(id),
  revokeOtherSessions: () => revokeOthersMock(),
}))
vi.mock('@/lib/api/auth', () => ({ fetchOperatorIdentity: async () => ({ username: 'operator' }) }))
vi.mock('@/components/toast-provider', () => ({
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}))

import { UsersContent } from '@/components/users-content'

const sessions = [
  { id: 'sess-aaaa', username: 'operator', created_at: '2026-01-01T00:00:00Z', last_seen_at: '2026-01-01T00:00:00Z', current: true },
  { id: 'sess-bbbb', username: 'operator', created_at: '2026-01-01T00:00:00Z', last_seen_at: '2026-01-01T00:00:00Z', current: false },
]

describe('UsersContent session revoke', () => {
  beforeEach(() => {
    fetchSessionsMock.mockReset().mockResolvedValue(sessions)
    revokeSessionMock.mockReset().mockResolvedValue({ status: 'ok' })
    revokeOthersMock.mockReset().mockResolvedValue({ status: 'ok', revoked: 1 })
  })

  it('offers sign-out only for other sessions, never the current one', async () => {
    render(<UsersContent />)
    expect(await screen.findByTestId('revoke-button-sess-bbbb')).toBeInTheDocument()
    expect(screen.queryByTestId('revoke-button-sess-aaaa')).toBeNull()
  })

  it('requires confirmation before revoking, and cancel revokes nothing', async () => {
    const user = userEvent.setup()
    render(<UsersContent />)
    await user.click(await screen.findByTestId('revoke-button-sess-bbbb'))
    expect(revokeSessionMock).not.toHaveBeenCalled()
    await user.click(screen.getByTestId('revoke-cancel'))
    expect(revokeSessionMock).not.toHaveBeenCalled()
    expect(screen.getByTestId('revoke-button-sess-bbbb')).toBeInTheDocument()
  })

  it('revokes the chosen session id after confirmation', async () => {
    const user = userEvent.setup()
    render(<UsersContent />)
    await user.click(await screen.findByTestId('revoke-button-sess-bbbb'))
    await user.click(screen.getByTestId('revoke-confirm-sess-bbbb'))
    await waitFor(() => expect(revokeSessionMock).toHaveBeenCalledWith('sess-bbbb'))
  })

  it('sign out other sessions is also two-step', async () => {
    const user = userEvent.setup()
    render(<UsersContent />)
    await user.click(await screen.findByTestId('revoke-others-button'))
    expect(revokeOthersMock).not.toHaveBeenCalled()
    await user.click(screen.getByTestId('revoke-others-confirm'))
    await waitFor(() => expect(revokeOthersMock).toHaveBeenCalledTimes(1))
  })
})
