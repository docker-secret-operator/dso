import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'

vi.mock('next/navigation', () => ({
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  usePathname: () => '/logs',
}))

const fetchLogsMock = vi.fn()
vi.mock('@/lib/api/logs', () => ({
  fetchLogs: (...args: unknown[]) => fetchLogsMock(...args),
}))

import { LogsContent } from '@/components/logs-content'

describe('LogsContent', () => {
  beforeEach(() => {
    fetchLogsMock.mockReset()
  })

  it('shows a loading state before GET /api/logs resolves', () => {
    fetchLogsMock.mockReturnValue(new Promise(() => {}))

    render(<LogsContent />)
    expect(screen.getByTestId('logs-loading')).toBeInTheDocument()
  })

  it('renders logs loaded from GET /api/logs', async () => {
    fetchLogsMock.mockResolvedValue([
      { timestamp: '2026-08-14T10:00:00Z', secret: 'db-password', event_type: 'rotate', status: 'success' },
    ])

    render(<LogsContent />)

    await waitFor(() => expect(screen.getByTestId('logs-list')).toBeInTheDocument())
    expect(screen.getByText(/db-password/)).toBeInTheDocument()
  })

  it('shows an honest empty state when there are no logs', async () => {
    fetchLogsMock.mockResolvedValue([])

    render(<LogsContent />)

    await waitFor(() => expect(screen.getByTestId('logs-empty')).toBeInTheDocument())
  })

  it('shows an error state when the request fails', async () => {
    fetchLogsMock.mockRejectedValue(new Error('Failed to load logs (500)'))

    render(<LogsContent />)

    await waitFor(() => expect(screen.getByTestId('logs-error')).toBeInTheDocument())
    expect(screen.getByText(/Failed to load logs \(500\)/)).toBeInTheDocument()
  })

  it('re-fetches with the selected severity filter', async () => {
    fetchLogsMock.mockResolvedValue([])

    render(<LogsContent />)

    await waitFor(() => expect(fetchLogsMock).toHaveBeenCalledWith(100, undefined))
  })
})
