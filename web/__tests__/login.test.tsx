import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

const replaceMock = vi.fn()

vi.mock('next/navigation', () => ({
  useRouter: () => ({ replace: replaceMock, push: vi.fn() }),
}))

import LoginPage from '@/app/login/page'

describe('LoginPage', () => {
  beforeEach(() => {
    replaceMock.mockReset()
    vi.stubGlobal('fetch', vi.fn())
    sessionStorage.clear()
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('redirects to /dashboard on successful login', async () => {
    ;(fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      json: async () => ({ status: 'ok' }),
    })

    render(<LoginPage />)
    const user = userEvent.setup()

    await user.type(screen.getByLabelText(/username/i), 'admin')
    await user.type(screen.getByLabelText('Password'), 'supersecret1')
    await user.click(screen.getByRole('button', { name: /sign in/i }))

    await waitFor(() => expect(replaceMock).toHaveBeenCalledWith('/dashboard'))
    expect(fetch).toHaveBeenCalledWith(
      '/api/auth/login',
      expect.objectContaining({
        method: 'POST',
        credentials: 'include',
        body: JSON.stringify({ username: 'admin', password: 'supersecret1' }),
      })
    )
  })

  it('shows an error message on failed login and does not redirect', async () => {
    ;(fetch as unknown as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false,
      json: async () => ({ error: 'Invalid credentials. Please try again.' }),
    })

    render(<LoginPage />)
    const user = userEvent.setup()

    await user.type(screen.getByLabelText(/username/i), 'admin')
    await user.type(screen.getByLabelText('Password'), 'wrongpassword')
    await user.click(screen.getByRole('button', { name: /sign in/i }))

    expect(await screen.findByText(/invalid credentials/i)).toBeInTheDocument()
    expect(replaceMock).not.toHaveBeenCalled()
  })

  it('shows a session-expired notice when sessionStorage flag is set', () => {
    sessionStorage.setItem('session_expired', '1')
    render(<LoginPage />)
    expect(screen.getByText(/session expired/i)).toBeInTheDocument()
  })
})
