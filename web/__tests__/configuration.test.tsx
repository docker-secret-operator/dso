import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'

const replaceMock = vi.fn()
vi.mock('next/navigation', () => ({
  useRouter: () => ({ replace: replaceMock, push: vi.fn() }),
  usePathname: () => '/configuration',
}))

const fetchConfigRawMock = vi.fn()

const { ConfigFetchError } = vi.hoisted(() => {
  class ConfigFetchError extends Error {
    status: number
    constructor(status: number, message: string) {
      super(message)
      this.name = 'ConfigFetchError'
      this.status = status
    }
  }
  return { ConfigFetchError }
})

vi.mock('@/lib/api/config', () => ({
  fetchConfigRaw: () => fetchConfigRawMock(),
  ConfigFetchError,
}))

import { ConfigurationContent } from '@/components/configuration-content'

describe('ConfigurationContent', () => {
  let consoleLogSpy: any
  let consoleErrorSpy: any

  beforeEach(() => {
    fetchConfigRawMock.mockReset()
    replaceMock.mockReset()
    consoleLogSpy = vi.spyOn(console, 'log').mockImplementation(() => {})
    consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  it('shows a loading state before data resolves', () => {
    fetchConfigRawMock.mockReturnValue(new Promise(() => {}))

    render(<ConfigurationContent />)
    expect(screen.getByTestId('configuration-loading')).toBeInTheDocument()
  })

  it('renders real configuration data once loaded, without logging it', async () => {
    const configData = {
      secrets: [{ name: 'db-password', provider: 'vault' }],
      providers: ['vault', 'aws-secrets-manager'],
    }
    fetchConfigRawMock.mockResolvedValue(configData)

    render(<ConfigurationContent />)

    await waitFor(() => expect(screen.getByTestId('configuration-content')).toBeInTheDocument())
    expect(screen.getByText('db-password')).toBeInTheDocument()
    expect(screen.getAllByText('vault').length).toBeGreaterThan(0)
    expect(screen.getByText('aws-secrets-manager')).toBeInTheDocument()

    // Nothing about the config response should ever be logged.
    const loggedArgs = [...consoleLogSpy.mock.calls, ...consoleErrorSpy.mock.calls].flat()
    expect(loggedArgs.some((arg) => JSON.stringify(arg).includes('db-password'))).toBe(false)
  })

  it('redirects to /login on a 401 response rather than rendering data', async () => {
    fetchConfigRawMock.mockRejectedValue(new ConfigFetchError(401, 'Failed to load configuration (401)'))

    render(<ConfigurationContent />)

    await waitFor(() => expect(replaceMock).toHaveBeenCalledWith('/login'))
    expect(screen.queryByTestId('configuration-content')).not.toBeInTheDocument()
  })

  it('shows a clear not-authorized state on 403, not an empty table', async () => {
    fetchConfigRawMock.mockRejectedValue(new ConfigFetchError(403, 'Failed to load configuration (403)'))

    render(<ConfigurationContent />)

    await waitFor(() => expect(screen.getByTestId('configuration-unauthorized')).toBeInTheDocument())
    expect(screen.queryByTestId('configuration-content')).not.toBeInTheDocument()
  })

  it('shows a generic error state with retry for other failures', async () => {
    fetchConfigRawMock.mockRejectedValue(new Error('Network error'))

    render(<ConfigurationContent />)

    await waitFor(() => expect(screen.getByTestId('configuration-error')).toBeInTheDocument())
  })

  it('shows honest empty states when there is no data', async () => {
    fetchConfigRawMock.mockResolvedValue({ secrets: [], providers: [] })

    render(<ConfigurationContent />)

    await waitFor(() => expect(screen.getByTestId('configuration-providers-empty')).toBeInTheDocument())
    expect(screen.getByTestId('configuration-secrets-empty')).toBeInTheDocument()
  })
})
