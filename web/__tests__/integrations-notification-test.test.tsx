import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

const fetchIntegrationsMock = vi.fn()
const testNotificationsMock = vi.fn()
vi.mock('@/lib/api/integrations', () => ({
  fetchIntegrations: () => fetchIntegrationsMock(),
  testNotifications: () => testNotificationsMock(),
}))

import { IntegrationsContent } from '@/components/integrations-content'

describe('IntegrationsContent test notification action', () => {
  beforeEach(() => {
    fetchIntegrationsMock.mockReset()
    testNotificationsMock.mockReset()
    fetchIntegrationsMock.mockResolvedValue({
      integrations: [{ name: 'secret-update-webhook', type: 'webhook', enabled: true, auth_configured: true }],
      total_count: 1,
    })
  })

  it('renders the Test Notification button', async () => {
    render(<IntegrationsContent />)
    expect(await screen.findByTestId('test-notification-button')).toBeInTheDocument()
  })

  it('shows Testing... while the request is in flight', async () => {
    let resolvePost: (v: unknown) => void = () => {}
    testNotificationsMock.mockReturnValue(new Promise((resolve) => (resolvePost = resolve)))
    const user = userEvent.setup()
    render(<IntegrationsContent />)
    await user.click(await screen.findByTestId('test-notification-button'))

    expect(screen.getByTestId('test-notification-button')).toHaveTextContent('Testing...')
    resolvePost({ status: 'success', results: [], total_count: 1, failed: 0 })
  })

  it('click calls the correct endpoint function', async () => {
    testNotificationsMock.mockResolvedValue({ status: 'success', results: [], total_count: 1, failed: 0 })
    const user = userEvent.setup()
    render(<IntegrationsContent />)
    await user.click(await screen.findByTestId('test-notification-button'))

    await waitFor(() => expect(testNotificationsMock).toHaveBeenCalledTimes(1))
  })

  it('shows a success state when every destination succeeds', async () => {
    testNotificationsMock.mockResolvedValue({ status: 'success', results: [{ destination: 'https://x', success: true }], total_count: 1, failed: 0 })
    const user = userEvent.setup()
    render(<IntegrationsContent />)
    await user.click(await screen.findByTestId('test-notification-button'))

    expect(await screen.findByTestId('test-notification-success')).toHaveTextContent('Test notification sent')
  })

  it('shows a failure state when a destination fails', async () => {
    testNotificationsMock.mockResolvedValue({
      status: 'failure',
      results: [{ destination: 'https://x', success: false, error: 'connection refused' }],
      total_count: 1,
      failed: 1,
    })
    const user = userEvent.setup()
    render(<IntegrationsContent />)
    await user.click(await screen.findByTestId('test-notification-button'))

    expect(await screen.findByTestId('test-notification-failure')).toHaveTextContent('Test notification failed')
  })

  it('shows a failure state on network/API error without exposing internals', async () => {
    testNotificationsMock.mockRejectedValue(new Error('Notification test failed (500)'))
    const user = userEvent.setup()
    render(<IntegrationsContent />)
    await user.click(await screen.findByTestId('test-notification-button'))

    const failure = await screen.findByTestId('test-notification-failure')
    expect(failure.textContent?.toLowerCase()).not.toContain('token')
    expect(failure.textContent?.toLowerCase()).not.toContain('webhook secret')
  })

  it('never exposes credentials, tokens, or webhook secrets on the page', async () => {
    render(<IntegrationsContent />)
    await screen.findByTestId('test-notification-button')
    const text = document.body.textContent?.toLowerCase() ?? ''
    expect(text).not.toContain('auth_token')
    expect(text).not.toContain('bearer ')
  })
})
