import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { CoverageMetrics } from '@/components/discovery/CoverageMetrics'
import { ContainerMetadata, DSOAwarenessInfo } from '@/lib/api/types'

describe('CoverageMetrics Component', () => {
  const createMockContainer = (
    id: string,
    name: string,
    status: 'managed' | 'unmanaged' | 'partial'
  ): ContainerMetadata => ({
    container_id: id,
    container_name: name,
    image: 'test:v1',
    status: 'running',
    networks: { ip: '172.17.0.2', gateway: '172.17.0.1', networks: [] },
    env_vars: {},
    labels: {},
    restart_policy: { name: 'no' },
    dso_awareness: {
      status,
      managed_secrets: status === 'managed' ? ['secret1'] : status === 'partial' ? ['secret1'] : [],
      config_refs: status === 'managed' ? ['config1'] : status === 'partial' ? ['config1'] : [],
      missing_mappings: status === 'partial' ? ['mapping1'] : [],
    },
  })

  const mockContainers: ContainerMetadata[] = [
    createMockContainer('c1', 'api-1', 'managed'),
    createMockContainer('c2', 'api-2', 'managed'),
    createMockContainer('c3', 'db-1', 'partial'),
    createMockContainer('c4', 'cache-1', 'unmanaged'),
  ]

  beforeEach(() => {
    // Clear any mocks or state before each test
  })

  describe('Rendering', () => {
    it('should render without crashing', () => {
      const { container } = render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      expect(container).toBeInTheDocument()
    })

    it('should display all 4 metric cards', () => {
      render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      expect(screen.getByText('Total')).toBeInTheDocument()
      expect(screen.getByText('Managed')).toBeInTheDocument()
      expect(screen.getByText('Partial')).toBeInTheDocument()
      expect(screen.getByText('Unmanaged')).toBeInTheDocument()
    })

    it('should have correct number of cards in grid', () => {
      const { container } = render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      const cardElements = container.querySelectorAll('[class*="rounded-xl"]')
      // Should have 4 metric cards rendered
      expect(cardElements.length).toBe(4)
    })
  })

  describe('Metric Calculations', () => {
    it('should display correct total container count', () => {
      render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      const totalLabel = screen.getByText('Total').closest('div')
      // Total value should contain 4
      expect(totalLabel?.textContent).toContain('4')
    })

    it('should display correct managed count and percentage', () => {
      render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      // 2 managed out of 4 = 50%
      const managedLabel = screen.getByText('Managed').closest('div')
      expect(managedLabel?.textContent).toContain('2')
      expect(managedLabel?.textContent).toContain('50%')
    })

    it('should display correct partial count and percentage', () => {
      render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      // 1 partial out of 4 = 25%
      const partialLabel = screen.getByText('Partial').closest('div')
      expect(partialLabel?.textContent).toContain('1')
      expect(partialLabel?.textContent).toContain('25%')
    })

    it('should display correct unmanaged count and percentage', () => {
      render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      // 1 unmanaged out of 4 = 25%
      const unmanagedLabel = screen.getByText('Unmanaged').closest('div')
      expect(unmanagedLabel?.textContent).toContain('1')
      expect(unmanagedLabel?.textContent).toContain('25%')
    })

    it('should handle 100% managed case', () => {
      const allManaged = mockContainers.map(c => createMockContainer(c.container_id, c.container_name, 'managed'))
      render(<CoverageMetrics containers={allManaged} isLoading={false} />)
      // Should show 100% managed, 0% partial/unmanaged
      const managedLabel = screen.getByText('Managed').closest('div')
      expect(managedLabel?.textContent).toContain('100%')
      const partialLabel = screen.getByText('Partial').closest('div')
      expect(partialLabel?.textContent).toContain('0%')
    })

    it('should handle 0% managed case', () => {
      const allUnmanaged = mockContainers.map(c => createMockContainer(c.container_id, c.container_name, 'unmanaged'))
      render(<CoverageMetrics containers={allUnmanaged} isLoading={false} />)
      const managedLabel = screen.getByText('Managed').closest('div')
      expect(managedLabel?.textContent).toContain('0%')
      const unmanagedLabel = screen.getByText('Unmanaged').closest('div')
      expect(unmanagedLabel?.textContent).toContain('100%')
    })

    it('should calculate percentages correctly with different ratios', () => {
      const mixed = [
        createMockContainer('c1', 'a1', 'managed'),
        createMockContainer('c2', 'a2', 'managed'),
        createMockContainer('c3', 'a3', 'managed'),
        createMockContainer('c4', 'a4', 'partial'),
        createMockContainer('c5', 'a5', 'unmanaged'),
      ]
      render(<CoverageMetrics containers={mixed} isLoading={false} />)
      // 3/5 = 60%, 1/5 = 20%, 1/5 = 20%
      const managedLabel = screen.getByText('Managed').closest('div')
      expect(managedLabel?.textContent).toContain('3')
      expect(managedLabel?.textContent).toContain('60%')
    })
  })

  describe('Data Display', () => {
    it('should show count and percentage on same card', () => {
      render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      const managedCard = screen.getByText('Managed').closest('[class*="space-y"]')
      expect(managedCard?.textContent).toMatch(/\d+/)
      expect(managedCard?.textContent).toMatch(/%/)
    })

    it('should display metrics with proper formatting', () => {
      render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      // Check that numbers are displayed
      const texts = screen.getAllByText('2')
      expect(texts.length).toBeGreaterThan(0)
      const texts1 = screen.getAllByText('1')
      expect(texts1.length).toBeGreaterThan(0)
      const texts4 = screen.getAllByText('4')
      expect(texts4.length).toBeGreaterThan(0)
    })

    it('should format large numbers correctly', () => {
      const manyContainers = Array.from({ length: 1500 }, (_, i) =>
        createMockContainer(`c${i}`, `container-${i}`, 'managed')
      )
      render(<CoverageMetrics containers={manyContainers} isLoading={false} />)
      // Total should show 1500
      const totalLabel = screen.getByText('Total').closest('div')
      expect(totalLabel?.textContent).toContain('1500')
    })

    it('should show percentage with % symbol', () => {
      render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      // Check that cards display percentages
      const managedLabel = screen.getByText('Managed').closest('div')
      const partialLabel = screen.getByText('Partial').closest('div')
      const unmanagedLabel = screen.getByText('Unmanaged').closest('div')
      expect(managedLabel?.textContent).toMatch(/%/)
      expect(partialLabel?.textContent).toMatch(/%/)
      expect(unmanagedLabel?.textContent).toMatch(/%/)
    })
  })

  describe('Loading State', () => {
    it('should show skeleton loaders when loading', () => {
      const { container } = render(<CoverageMetrics containers={undefined} isLoading={true} />)
      const skeletons = container.querySelectorAll('[class*="skeleton"]')
      expect(skeletons.length).toBeGreaterThanOrEqual(4)
    })

    it('should not show data while loading', () => {
      render(<CoverageMetrics containers={mockContainers} isLoading={true} />)
      expect(screen.queryByText('Managed')).not.toBeInTheDocument()
      expect(screen.queryByText('Total')).not.toBeInTheDocument()
    })

    it('should render correct number of skeleton items', () => {
      const { container } = render(<CoverageMetrics containers={[]} isLoading={true} />)
      const skeletons = container.querySelectorAll('[class*="skeleton"]')
      // Should have 4 skeleton loaders for 4 metrics
      expect(skeletons.length).toBe(4)
    })

    it('should transition from loading to loaded state', () => {
      const { rerender, container } = render(<CoverageMetrics containers={mockContainers} isLoading={true} />)
      // While loading, no metric cards
      expect(screen.queryByText('Managed')).not.toBeInTheDocument()

      // After loading
      rerender(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      expect(screen.getByText('Managed')).toBeInTheDocument()
    })
  })

  describe('Empty State', () => {
    it('should handle undefined containers gracefully', () => {
      const { container } = render(<CoverageMetrics containers={undefined} isLoading={false} />)
      expect(container).toBeInTheDocument()
    })

    it('should handle empty array', () => {
      render(<CoverageMetrics containers={[]} isLoading={false} />)
      expect(screen.getByText('Total')).toBeInTheDocument()
    })

    it('should show 0 for all metrics with empty containers', () => {
      render(<CoverageMetrics containers={[]} isLoading={false} />)
      const totalLabel = screen.getByText('Total').closest('div')
      expect(totalLabel?.textContent).toContain('0')
      const managedLabel = screen.getByText('Managed').closest('div')
      expect(managedLabel?.textContent).toContain('0')
      const partialLabel = screen.getByText('Partial').closest('div')
      expect(partialLabel?.textContent).toContain('0')
      const unmanagedLabel = screen.getByText('Unmanaged').closest('div')
      expect(unmanagedLabel?.textContent).toContain('0')
    })

    it('should show 0% percentages for empty state', () => {
      render(<CoverageMetrics containers={[]} isLoading={false} />)
      // All classification cards should show 0%
      const cards = [
        screen.getByText('Managed').closest('div'),
        screen.getByText('Partial').closest('div'),
        screen.getByText('Unmanaged').closest('div'),
      ]
      cards.forEach(card => {
        expect(card?.textContent).toContain('0%')
      })
    })

    it('should handle single container', () => {
      const single = [createMockContainer('c1', 'test', 'managed')]
      render(<CoverageMetrics containers={single} isLoading={false} />)
      const totalLabel = screen.getByText('Total').closest('[class*="space-y"]')
      expect(totalLabel?.textContent).toContain('1')
    })
  })

  describe('Card Styling and Visual Indicators', () => {
    it('should apply color classes to metric values', () => {
      const { container } = render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      // Total should be blue
      const totalCard = screen.getByText('Total').closest('[class*="space-y"]')
      const totalValue = totalCard?.querySelector('[class*="text-blue"]')
      expect(totalValue).toBeInTheDocument()
    })

    it('should have different colors for different classifications', () => {
      const { container } = render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      // Get the color classes for each metric
      const managedCard = screen.getByText('Managed').closest('[class*="space-y"]')
      const unmanagedCard = screen.getByText('Unmanaged').closest('[class*="space-y"]')

      // They should have different text color classes
      const managedText = managedCard?.querySelector('[class*="text-2xl"]')
      const unmanagedText = unmanagedCard?.querySelector('[class*="text-2xl"]')

      expect(managedText?.className).not.toBe(unmanagedText?.className)
    })

    it('should apply emerald color to managed metrics', () => {
      const { container } = render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      const managedCard = screen.getByText('Managed').closest('[class*="space-y"]')
      const coloredText = managedCard?.querySelector('[class*="emerald"]')
      expect(coloredText).toBeInTheDocument()
    })

    it('should apply amber color to partial metrics', () => {
      const { container } = render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      const partialCard = screen.getByText('Partial').closest('[class*="space-y"]')
      const coloredText = partialCard?.querySelector('[class*="amber"]')
      expect(coloredText).toBeInTheDocument()
    })

    it('should apply slate color to unmanaged metrics', () => {
      const { container } = render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      const unmanagedCard = screen.getByText('Unmanaged').closest('[class*="space-y"]')
      const coloredText = unmanagedCard?.querySelector('[class*="slate"]')
      expect(coloredText).toBeInTheDocument()
    })

    it('cards should have proper spacing', () => {
      const { container } = render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      const gridContainer = container.querySelector('[class*="grid-cols"]')
      expect(gridContainer).toHaveClass('gap-4')
    })
  })

  describe('Edge Cases', () => {
    it('should handle containers with missing dso_awareness', () => {
      const malformed = [
        {
          container_id: 'c1',
          container_name: 'test',
          image: 'test:v1',
          status: 'running',
          networks: { ip: '', gateway: '', networks: [] },
          env_vars: {},
          labels: {},
          restart_policy: { name: 'no' },
        } as unknown as ContainerMetadata,
      ]
      render(<CoverageMetrics containers={malformed} isLoading={false} />)
      expect(screen.getByText('Total')).toBeInTheDocument()
    })

    it('should handle containers without status field in dso_awareness', () => {
      const malformedAwareness = [
        {
          container_id: 'c1',
          container_name: 'test',
          image: 'test:v1',
          status: 'running',
          networks: { ip: '', gateway: '', networks: [] },
          env_vars: {},
          labels: {},
          restart_policy: { name: 'no' },
          dso_awareness: {} as DSOAwarenessInfo,
        },
      ]
      render(<CoverageMetrics containers={malformedAwareness} isLoading={false} />)
      expect(screen.getByText('Total')).toBeInTheDocument()
    })

    it('should handle very large container count', () => {
      const largeArray = Array.from({ length: 10000 }, (_, i) =>
        createMockContainer(`c${i}`, `container-${i}`, i % 3 === 0 ? 'managed' : i % 3 === 1 ? 'partial' : 'unmanaged')
      )
      render(<CoverageMetrics containers={largeArray} isLoading={false} />)
      expect(screen.getByText('Total')).toBeInTheDocument()
      const totalLabel = screen.getByText('Total').closest('div')
      expect(totalLabel?.textContent).toContain('10000')
    })

    it('should handle all containers being partial', () => {
      const allPartial = mockContainers.map(c => createMockContainer(c.container_id, c.container_name, 'partial'))
      render(<CoverageMetrics containers={allPartial} isLoading={false} />)
      const partialLabel = screen.getByText('Partial').closest('div')
      expect(partialLabel?.textContent).toContain('4')
      expect(partialLabel?.textContent).toContain('100%')
    })
  })

  describe('Percentage Calculation Accuracy', () => {
    it('percentages should add up correctly for common ratios', () => {
      render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      // 2 managed + 1 partial + 1 unmanaged = 4 total
      // 50% + 25% + 25% = 100%
      const managedLabel = screen.getByText('Managed').closest('[class*="space-y"]')
      const partialLabel = screen.getByText('Partial').closest('[class*="space-y"]')
      const unmanagedLabel = screen.getByText('Unmanaged').closest('[class*="space-y"]')

      // Extract percentage from the content more carefully
      const managedPercent = managedLabel?.querySelector('p:last-child')?.textContent?.trim()
      const partialPercent = partialLabel?.querySelector('p:last-child')?.textContent?.trim()
      const unmanagedPercent = unmanagedLabel?.querySelector('p:last-child')?.textContent?.trim()

      const managed = managedPercent ? parseInt(managedPercent) : 0
      const partial = partialPercent ? parseInt(partialPercent) : 0
      const unmanaged = unmanagedPercent ? parseInt(unmanagedPercent) : 0

      expect(managed + partial + unmanaged).toBe(100)
    })

    it('should round percentages correctly', () => {
      // Test with 1/3 ratio: 1/3 = 33.33... should round to 33%
      const threeContainers = [
        createMockContainer('c1', 'a1', 'managed'),
        createMockContainer('c2', 'a2', 'partial'),
        createMockContainer('c3', 'a3', 'unmanaged'),
      ]
      render(<CoverageMetrics containers={threeContainers} isLoading={false} />)
      const managedLabel = screen.getByText('Managed').closest('div')
      expect(managedLabel?.textContent).toContain('33%')
    })

    it('should handle rounding errors with 1% tolerance', () => {
      // Test edge cases where rounding might cause issues
      const sevenContainers = Array.from({ length: 7 }, (_, i) =>
        createMockContainer(`c${i}`, `a${i}`, i === 0 ? 'managed' : i < 4 ? 'partial' : 'unmanaged')
      )
      render(<CoverageMetrics containers={sevenContainers} isLoading={false} />)

      const managedLabel = screen.getByText('Managed').closest('[class*="space-y"]')
      const partialLabel = screen.getByText('Partial').closest('[class*="space-y"]')
      const unmanagedLabel = screen.getByText('Unmanaged').closest('[class*="space-y"]')

      // Extract percentage from the last p tag in each card
      const managedPercent = managedLabel?.querySelector('p:last-child')?.textContent?.trim()
      const partialPercent = partialLabel?.querySelector('p:last-child')?.textContent?.trim()
      const unmanagedPercent = unmanagedLabel?.querySelector('p:last-child')?.textContent?.trim()

      const managed = managedPercent ? parseInt(managedPercent) : 0
      const partial = partialPercent ? parseInt(partialPercent) : 0
      const unmanaged = unmanagedPercent ? parseInt(unmanagedPercent) : 0

      // Sum should be 100 or within 1% due to rounding
      const sum = managed + partial + unmanaged
      expect(sum).toBeGreaterThanOrEqual(99)
      expect(sum).toBeLessThanOrEqual(101)
    })
  })

  describe('Responsive Layout', () => {
    it('should have responsive grid layout', () => {
      const { container } = render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      const grid = container.querySelector('[class*="grid"]')
      expect(grid).toHaveClass('grid-cols-2')
      expect(grid).toHaveClass('md:grid-cols-4')
    })

    it('should render in 2-column layout on mobile', () => {
      const { container } = render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      const grid = container.querySelector('[class*="grid"]')
      // Primary grid-cols-2 for mobile
      expect(grid?.className).toMatch(/grid-cols-2/)
    })

    it('should render in 4-column layout on desktop', () => {
      const { container } = render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      const grid = container.querySelector('[class*="grid"]')
      // md:grid-cols-4 for desktop
      expect(grid?.className).toMatch(/md:grid-cols-4/)
    })
  })

  describe('Card Content Structure', () => {
    it('should display metric label as text', () => {
      render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      // Verify labels are visible
      const labels = screen.getAllByText(/Total|Managed|Partial|Unmanaged/i)
      expect(labels.length).toBeGreaterThanOrEqual(4)
    })

    it('should display metric values in larger font', () => {
      const { container } = render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      const largeText = container.querySelectorAll('[class*="text-2xl"]')
      // Should have 4 large text elements for the metric values
      expect(largeText.length).toBeGreaterThanOrEqual(4)
    })

    it('should display percentage in smaller font', () => {
      const { container } = render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      const smallText = container.querySelectorAll('[class*="text-xs"]')
      // Should have multiple xs text elements (labels + percentages)
      expect(smallText.length).toBeGreaterThanOrEqual(8)
    })
  })

  describe('Data Integrity', () => {
    it('should not modify container data during render', () => {
      const containersCopy = JSON.parse(JSON.stringify(mockContainers))
      render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      expect(mockContainers).toEqual(containersCopy)
    })

    it('should handle null/undefined values safely', () => {
      const safeContainers = [
        {
          container_id: 'c1',
          container_name: 'test',
          image: 'test:v1',
          status: 'running',
          networks: { ip: '', gateway: '', networks: [] },
          env_vars: {},
          labels: {},
          restart_policy: { name: 'no' },
          dso_awareness: {
            status: 'managed' as const,
            managed_secrets: [],
            config_refs: [],
            missing_mappings: [],
          },
        },
      ]
      render(<CoverageMetrics containers={safeContainers} isLoading={false} />)
      expect(screen.getByText('Total')).toBeInTheDocument()
    })
  })

  describe('Integration Tests', () => {
    it('should render complete metrics dashboard', () => {
      const { container } = render(<CoverageMetrics containers={mockContainers} isLoading={false} />)
      // All elements should be present
      expect(screen.getByText('Total')).toBeInTheDocument()
      expect(screen.getByText('Managed')).toBeInTheDocument()
      expect(screen.getByText('Partial')).toBeInTheDocument()
      expect(screen.getByText('Unmanaged')).toBeInTheDocument()

      // All values should be displayed
      const values = container.querySelectorAll('[class*="text-2xl"]')
      expect(values.length).toBeGreaterThanOrEqual(4)
    })

    it('should handle transition from loading to data state', () => {
      const { rerender, container: initialContainer } = render(
        <CoverageMetrics containers={mockContainers} isLoading={true} />
      )

      // Check loading state
      let skeletons = initialContainer.querySelectorAll('[class*="skeleton"]')
      expect(skeletons.length).toBe(4)

      // Transition to loaded state
      rerender(<CoverageMetrics containers={mockContainers} isLoading={false} />)

      // Check data is now visible
      expect(screen.getByText('Managed')).toBeInTheDocument()
      skeletons = initialContainer.querySelectorAll('[class*="skeleton"]')
      expect(skeletons.length).toBe(0)
    })

    it('should maintain consistency across multiple renders', () => {
      const { rerender } = render(<CoverageMetrics containers={mockContainers} isLoading={false} />)

      const firstRender = {
        totalCard: screen.getByText('Total').closest('div')?.textContent,
        managedCard: screen.getByText('Managed').closest('div')?.textContent,
      }

      // Re-render with same props
      rerender(<CoverageMetrics containers={mockContainers} isLoading={false} />)

      const secondRender = {
        totalCard: screen.getByText('Total').closest('div')?.textContent,
        managedCard: screen.getByText('Managed').closest('div')?.textContent,
      }

      expect(firstRender.totalCard).toBe(secondRender.totalCard)
      expect(firstRender.managedCard).toBe(secondRender.managedCard)
    })
  })
})
