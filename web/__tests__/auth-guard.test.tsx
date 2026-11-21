import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'

const replaceMock = vi.fn()
let pathname = '/dashboard'

vi.mock('next/navigation', () => ({
  useRouter: () => ({ replace: replaceMock }),
  usePathname: () => pathname,
}))

const checkSessionMock = vi.fn()
vi.mock('@/lib/api/auth', () => ({
  checkSession: () => checkSessionMock(),
  login: vi.fn(),
  logout: vi.fn(),
}))

import { AuthGuard } from '@/components/auth-guard'

describe('AuthGuard', () => {
  beforeEach(() => {
    replaceMock.mockReset()
    checkSessionMock.mockReset()
    pathname = '/dashboard'
  })

  it('redirects unauthenticated users to /login', async () => {
    checkSessionMock.mockResolvedValue(false)
    render(
      <AuthGuard>
        <div>protected content</div>
      </AuthGuard>
    )
    await waitFor(() => expect(replaceMock).toHaveBeenCalledWith('/login'))
  })

  it('renders protected content for authenticated users', async () => {
    checkSessionMock.mockResolvedValue(true)
    render(
      <AuthGuard>
        <div>protected content</div>
      </AuthGuard>
    )
    expect(await screen.findByText('protected content')).toBeInTheDocument()
    expect(replaceMock).not.toHaveBeenCalled()
  })

  it('redirects authenticated users away from /login to /dashboard', async () => {
    pathname = '/login'
    checkSessionMock.mockResolvedValue(true)
    render(
      <AuthGuard>
        <div>login form</div>
      </AuthGuard>
    )
    await waitFor(() => expect(replaceMock).toHaveBeenCalledWith('/dashboard'))
  })
})
