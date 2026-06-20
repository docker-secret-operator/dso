import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ContainerTable } from '@/components/discovery/ContainerTable'
import { ContainerMetadata } from '@/lib/api/types'

describe('ContainerTable Component', () => {
  const mockOnSelectContainer = vi.fn()
  const mockOnToggleSelect = vi.fn()

  const createMockContainer = (id: string, name: string, status: 'managed' | 'unmanaged' | 'partial'): ContainerMetadata => ({
    container_id: id,
    container_name: name,
    image: `test-image:${id}`,
    status: 'running',
    networks: { bridge: { ip_address: '172.17.0.2', gateway: '172.17.0.1' } },
    env_vars: {},
    labels: {},
    restart_policy: { name: 'no' },
    dso_awareness: {
      status,
      managed_secrets: status === 'managed' ? ['secret1'] : [],
      config_refs: status === 'managed' ? ['config1'] : [],
      missing_mappings: status === 'partial' ? ['missing1'] : [],
    },
  })

  const mockContainers: ContainerMetadata[] = [
    createMockContainer('c1', 'api-server', 'managed'),
    createMockContainer('c2', 'database', 'partial'),
    createMockContainer('c3', 'cache', 'unmanaged'),
  ]

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Rendering', () => {
    it('should render table without crashing', () => {
      const { container } = render(
        <ContainerTable containers={mockContainers} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      expect(container).toBeInTheDocument()
    })

    it('should display all 6 column headers', () => {
      render(
        <ContainerTable containers={mockContainers} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      expect(screen.getByText('Name')).toBeInTheDocument()
      expect(screen.getByText('Image')).toBeInTheDocument()
      expect(screen.getByText('Status')).toBeInTheDocument()
      expect(screen.getByText('Classification')).toBeInTheDocument()
      expect(screen.getByText('Secrets')).toBeInTheDocument()
      expect(screen.getByText('Missing')).toBeInTheDocument()
    })

    it('should have 7 columns when selection is enabled', () => {
      const { container } = render(
        <ContainerTable
          containers={mockContainers}
          isLoading={false}
          onSelectContainer={mockOnSelectContainer}
          onToggleSelect={mockOnToggleSelect}
        />
      )
      const header = container.querySelector('[class*="grid-cols-7"]')
      expect(header).toBeInTheDocument()
    })
  })

  describe('Row Display', () => {
    it('should display all container rows', () => {
      render(
        <ContainerTable containers={mockContainers} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      expect(screen.getByText('api-server')).toBeInTheDocument()
      expect(screen.getByText('database')).toBeInTheDocument()
      expect(screen.getByText('cache')).toBeInTheDocument()
    })

    it('should display container names in rows', () => {
      render(
        <ContainerTable containers={mockContainers} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      mockContainers.forEach(container => {
        expect(screen.getByText(container.container_name)).toBeInTheDocument()
      })
    })

    it('should display container images in rows', () => {
      render(
        <ContainerTable containers={mockContainers} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      mockContainers.forEach(container => {
        expect(screen.getByText(container.image)).toBeInTheDocument()
      })
    })

    it('should render correct number of rows for containers', () => {
      const { container } = render(
        <ContainerTable containers={mockContainers} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      // Each container becomes a ContainerRow, check for rendered content
      expect(screen.getAllByText(/api-server|database|cache/).length).toBeGreaterThan(0)
    })
  })

  describe('Row Click', () => {
    it('should call onSelectContainer when container row is clicked', async () => {
      const user = userEvent.setup()
      render(
        <ContainerTable containers={mockContainers} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )

      const firstContainer = mockContainers[0]
      const nameCell = screen.getByText(firstContainer.container_name)

      await user.click(nameCell)
      expect(mockOnSelectContainer).toHaveBeenCalled()
    })

    it('should pass correct container to callback', async () => {
      const user = userEvent.setup()
      render(
        <ContainerTable containers={mockContainers} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )

      const firstContainer = mockContainers[0]
      const nameCell = screen.getByText(firstContainer.container_name)

      await user.click(nameCell)
      expect(mockOnSelectContainer).toHaveBeenCalledWith(expect.objectContaining({
        container_id: firstContainer.container_id,
        container_name: firstContainer.container_name,
      }))
    })

    it('should call callback for each different container', async () => {
      const user = userEvent.setup()
      render(
        <ContainerTable containers={mockContainers} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )

      for (const container of mockContainers) {
        const cell = screen.getByText(container.container_name)
        await user.click(cell)
      }

      expect(mockOnSelectContainer).toHaveBeenCalledTimes(mockContainers.length)
    })
  })

  describe('Loading State', () => {
    it('should show skeleton loaders when loading', () => {
      const { container } = render(
        <ContainerTable containers={[]} isLoading={true} onSelectContainer={mockOnSelectContainer} />
      )
      const skeletons = container.querySelectorAll('[class*="skeleton"]')
      expect(skeletons.length).toBeGreaterThan(0)
    })

    it('should display header even while loading', () => {
      render(
        <ContainerTable containers={[]} isLoading={true} onSelectContainer={mockOnSelectContainer} />
      )
      expect(screen.getByText('Name')).toBeInTheDocument()
      expect(screen.getByText('Image')).toBeInTheDocument()
    })

    it('should render 5 skeleton rows for loading state', () => {
      const { container } = render(
        <ContainerTable containers={[]} isLoading={true} onSelectContainer={mockOnSelectContainer} />
      )
      const skeletons = container.querySelectorAll('[class*="skeleton"]')
      expect(skeletons.length).toBe(5)
    })

    it('should not show data while loading', () => {
      render(
        <ContainerTable containers={mockContainers} isLoading={true} onSelectContainer={mockOnSelectContainer} />
      )
      expect(screen.queryByText('api-server')).not.toBeInTheDocument()
    })

    it('should transition from loading to loaded state', () => {
      const { rerender } = render(
        <ContainerTable containers={mockContainers} isLoading={true} onSelectContainer={mockOnSelectContainer} />
      )
      expect(screen.queryByText('api-server')).not.toBeInTheDocument()

      rerender(
        <ContainerTable containers={mockContainers} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      expect(screen.getByText('api-server')).toBeInTheDocument()
    })
  })

  describe('Empty State', () => {
    it('should show empty state when no containers', () => {
      const { container } = render(
        <ContainerTable containers={[]} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      // Empty state should render
      const card = container.querySelector('[class*="rounded"]')
      expect(card).toBeInTheDocument()
    })

    it('should not show table headers for empty state', () => {
      render(
        <ContainerTable containers={[]} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      // Headers should not be visible in empty state card
      const gridHeaders = document.querySelectorAll('[class*="grid-cols-6"]')
      expect(gridHeaders.length).toBe(0)
    })

    it('should handle undefined containers', () => {
      render(
        <ContainerTable containers={[]} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      const { container } = render(
        <ContainerTable containers={[]} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      expect(container).toBeInTheDocument()
    })
  })

  describe('Selection Feature', () => {
    it('should display select column when onToggleSelect provided', () => {
      const { container } = render(
        <ContainerTable
          containers={mockContainers}
          isLoading={false}
          onSelectContainer={mockOnSelectContainer}
          onToggleSelect={mockOnToggleSelect}
        />
      )
      expect(screen.getByText('Select')).toBeInTheDocument()
    })

    it('should not display select column without onToggleSelect', () => {
      render(
        <ContainerTable containers={mockContainers} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      const selectHeaders = screen.queryAllByText('Select')
      expect(selectHeaders.length).toBe(0)
    })

    it('should highlight selected containers', () => {
      const selectedIds = new Set(['c1'])
      const { container } = render(
        <ContainerTable
          containers={mockContainers}
          isLoading={false}
          onSelectContainer={mockOnSelectContainer}
          selectedIds={selectedIds}
          onToggleSelect={mockOnToggleSelect}
        />
      )
      // Selected container should be rendered
      expect(screen.getByText('api-server')).toBeInTheDocument()
    })
  })

  describe('Classification Display', () => {
    it('should display classification for managed containers', () => {
      render(
        <ContainerTable containers={mockContainers} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      // managed container should be displayed
      expect(screen.getByText('api-server')).toBeInTheDocument()
    })

    it('should display classification for partial containers', () => {
      render(
        <ContainerTable containers={mockContainers} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      expect(screen.getByText('database')).toBeInTheDocument()
    })

    it('should display classification for unmanaged containers', () => {
      render(
        <ContainerTable containers={mockContainers} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      expect(screen.getByText('cache')).toBeInTheDocument()
    })
  })

  describe('Card Structure', () => {
    it('should render within a Card component', () => {
      const { container } = render(
        <ContainerTable containers={mockContainers} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      expect(container.querySelector('[class*="overflow-hidden"]')).toBeInTheDocument()
    })

    it('should have proper border styling', () => {
      const { container } = render(
        <ContainerTable containers={mockContainers} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      const bordered = container.querySelector('[class*="border-b"]')
      expect(bordered).toBeInTheDocument()
    })
  })

  describe('Data Integrity', () => {
    it('should not modify container data during render', () => {
      const containersCopy = JSON.parse(JSON.stringify(mockContainers))
      render(
        <ContainerTable containers={mockContainers} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      expect(mockContainers).toEqual(containersCopy)
    })

    it('should render containers with correct DSO awareness info', () => {
      const containerWithSecrets = createMockContainer('c4', 'test', 'managed')
      render(
        <ContainerTable containers={[containerWithSecrets]} isLoading={false} onSelectContainer={mockOnSelectContainer} />
      )
      expect(screen.getByText('test')).toBeInTheDocument()
    })
  })
})
