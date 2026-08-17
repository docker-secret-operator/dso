import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'

vi.mock('next/navigation', () => ({
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() }),
  usePathname: () => '/containers',
}))

const fetchContainersMock = vi.fn()
vi.mock('@/lib/api/containers', () => ({
  fetchContainers: () => fetchContainersMock(),
}))

import { ContainersContent } from '@/components/containers-content'

describe('ContainersContent', () => {
  beforeEach(() => {
    fetchContainersMock.mockReset()
  })

  it('shows a loading state before GET /api/containers resolves', () => {
    fetchContainersMock.mockReturnValue(new Promise(() => {}))

    render(<ContainersContent />)
    expect(screen.getByTestId('containers-loading')).toBeInTheDocument()
  })

  it('renders containers loaded from GET /api/containers', async () => {
    fetchContainersMock.mockResolvedValue({
      total_count: 1,
      containers: [
        {
          id: 'container-abc',
          strategy: 'restart',
          compose_path: '/opt/stack/docker-compose.yml',
          secrets: ['db-password', 'api-key'],
        },
      ],
    })

    render(<ContainersContent />)

    await waitFor(() => expect(screen.getByTestId('containers-list')).toBeInTheDocument())
    expect(screen.getByText('container-abc')).toBeInTheDocument()
    expect(screen.getByText('db-password')).toBeInTheDocument()
  })

  it('shows an honest empty state when there are no tracked containers', async () => {
    fetchContainersMock.mockResolvedValue({ total_count: 0, containers: [] })

    render(<ContainersContent />)

    await waitFor(() => expect(screen.getByTestId('containers-empty')).toBeInTheDocument())
  })

  it('shows an error state when the request fails', async () => {
    fetchContainersMock.mockRejectedValue(new Error('Failed to load containers (500)'))

    render(<ContainersContent />)

    await waitFor(() => expect(screen.getByTestId('containers-error')).toBeInTheDocument())
    expect(screen.getByText(/Failed to load containers \(500\)/)).toBeInTheDocument()
  })
})
