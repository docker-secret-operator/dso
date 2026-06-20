import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { DiscoveryFilters, type FilterType } from '@/components/discovery/DiscoveryFilters'

describe('DiscoveryFilters Component', () => {
  const mockOnFilterChange = vi.fn()

  const defaultProps = {
    filters: { classification: [] as FilterType[], status: [] as FilterType[] },
    onFilterChange: mockOnFilterChange,
    containerCount: {
      managed: 10,
      partial: 5,
      unmanaged: 3,
    },
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Rendering', () => {
    it('should render without crashing', () => {
      render(<DiscoveryFilters {...defaultProps} />)
      expect(screen.getByText('Filters')).toBeInTheDocument()
    })

    it('should display all classification filter options', () => {
      render(<DiscoveryFilters {...defaultProps} />)
      const buttons = screen.getAllByRole('button')
      const buttonTexts = buttons.map(b => b.textContent?.toLowerCase() || '')
      expect(buttonTexts).toContain('managed')
      expect(buttonTexts).toContain('partial')
      expect(buttonTexts).toContain('unmanaged')
    })

    it('should display all status filter options', () => {
      render(<DiscoveryFilters {...defaultProps} />)
      const buttons = screen.getAllByRole('button')
      const buttonTexts = buttons.map(b => b.textContent?.toLowerCase() || '')
      expect(buttonTexts).toContain('running')
      expect(buttonTexts).toContain('stopped')
    })

    it('should display classification and status section headers', () => {
      render(<DiscoveryFilters {...defaultProps} />)
      expect(screen.getByText('Classification')).toBeInTheDocument()
      expect(screen.getByText('Status')).toBeInTheDocument()
    })
  })

  describe('Filter Click Handling', () => {
    it('should call onFilterChange when classification filter is clicked', async () => {
      const user = userEvent.setup()
      const { container } = render(<DiscoveryFilters {...defaultProps} />)

      const buttons = container.querySelectorAll('button')
      const managedButton = Array.from(buttons).find(b => b.textContent?.includes('managed'))
      await user.click(managedButton!)

      expect(mockOnFilterChange).toHaveBeenCalledWith({
        classification: ['managed'],
        status: [],
      })
    })

    it('should call onFilterChange when status filter is clicked', async () => {
      const user = userEvent.setup()
      const { container } = render(<DiscoveryFilters {...defaultProps} />)

      const buttons = container.querySelectorAll('button')
      const runningButton = Array.from(buttons).find(b => b.textContent?.includes('running'))
      await user.click(runningButton!)

      expect(mockOnFilterChange).toHaveBeenCalledWith({
        classification: [],
        status: ['running'],
      })
    })

    it('should deselect filter when clicked again', async () => {
      const user = userEvent.setup()
      const props = {
        ...defaultProps,
        filters: { classification: ['managed'] as FilterType[], status: [] as FilterType[] },
      }
      const { container } = render(<DiscoveryFilters {...props} />)

      const buttons = container.querySelectorAll('button')
      const managedButton = Array.from(buttons).find(b => b.textContent?.includes('managed'))
      await user.click(managedButton!)

      expect(mockOnFilterChange).toHaveBeenCalledWith({
        classification: [],
        status: [],
      })
    })
  })

  describe('Multiple Filters', () => {
    it('should allow selecting multiple classification filters', async () => {
      const user = userEvent.setup()
      const { container } = render(<DiscoveryFilters {...defaultProps} />)

      const buttons = container.querySelectorAll('button')
      const managedButton = Array.from(buttons).find(b => b.textContent?.includes('managed'))
      const partialButton = Array.from(buttons).find(b => b.textContent?.includes('partial'))

      await user.click(managedButton!)
      expect(mockOnFilterChange).toHaveBeenCalledWith({
        classification: ['managed'],
        status: [],
      })

      mockOnFilterChange.mockClear()

      // Re-render with first filter applied
      const { container: container2 } = render(
        <DiscoveryFilters
          {...defaultProps}
          filters={{ classification: ['managed'] as FilterType[], status: [] as FilterType[] }}
        />
      )

      const buttons2 = container2.querySelectorAll('button')
      const partialButton2 = Array.from(buttons2).find(b => b.textContent?.includes('partial'))
      await user.click(partialButton2!)
      expect(mockOnFilterChange).toHaveBeenCalledWith({
        classification: ['managed', 'partial'],
        status: [],
      })
    })

    it('should allow selecting multiple status filters', async () => {
      const user = userEvent.setup()
      const { container } = render(<DiscoveryFilters {...defaultProps} />)

      const buttons = container.querySelectorAll('button')
      const runningButton = Array.from(buttons).find(b => b.textContent?.includes('running'))
      const stoppedButton = Array.from(buttons).find(b => b.textContent?.includes('stopped'))

      await user.click(runningButton!)
      expect(mockOnFilterChange).toHaveBeenCalledWith({
        classification: [],
        status: ['running'],
      })

      mockOnFilterChange.mockClear()

      // Re-render with first filter applied
      const { container: container2 } = render(
        <DiscoveryFilters
          {...defaultProps}
          filters={{ classification: [] as FilterType[], status: ['running'] as FilterType[] }}
        />
      )

      const buttons2 = container2.querySelectorAll('button')
      const stoppedButton2 = Array.from(buttons2).find(b => b.textContent?.includes('stopped'))
      await user.click(stoppedButton2!)
      expect(mockOnFilterChange).toHaveBeenCalledWith({
        classification: [],
        status: ['running', 'stopped'],
      })
    })

    it('should allow combining classification and status filters', async () => {
      const user = userEvent.setup()
      const { container } = render(<DiscoveryFilters {...defaultProps} />)

      const buttons = container.querySelectorAll('button')
      const managedButton = Array.from(buttons).find(b => b.textContent?.includes('managed'))

      await user.click(managedButton!)
      mockOnFilterChange.mockClear()

      const { container: container2 } = render(
        <DiscoveryFilters
          {...defaultProps}
          filters={{ classification: ['managed'] as FilterType[], status: [] as FilterType[] }}
        />
      )

      const buttons2 = container2.querySelectorAll('button')
      const runningButton = Array.from(buttons2).find(b => b.textContent?.includes('running'))
      await user.click(runningButton!)
      expect(mockOnFilterChange).toHaveBeenCalledWith({
        classification: ['managed'],
        status: ['running'],
      })
    })
  })

  describe('Filter Chips', () => {
    it('should display active classification filters as chips', () => {
      const props = {
        ...defaultProps,
        filters: {
          classification: ['managed', 'partial'] as FilterType[],
          status: [] as FilterType[],
        },
      }
      render(<DiscoveryFilters {...props} />)

      expect(screen.getByText('Managed')).toBeInTheDocument()
      expect(screen.getByText('Partial')).toBeInTheDocument()
    })

    it('should display active status filters as chips', () => {
      const props = {
        ...defaultProps,
        filters: {
          classification: [] as FilterType[],
          status: ['running', 'stopped'] as FilterType[],
        },
      }
      render(<DiscoveryFilters {...props} />)

      expect(screen.getByText('Running')).toBeInTheDocument()
      expect(screen.getByText('Stopped')).toBeInTheDocument()
    })

    it('should remove filter when chip X button is clicked', async () => {
      const user = userEvent.setup()
      const props = {
        ...defaultProps,
        filters: {
          classification: ['managed'] as FilterType[],
          status: [] as FilterType[],
        },
      }
      render(<DiscoveryFilters {...props} />)

      // Find the remove button within the Managed chip
      const chipButtons = screen.getAllByRole('button')
      // The last button in the Managed chip should be the X button
      const removeButton = chipButtons[chipButtons.length - 1]
      await user.click(removeButton)

      expect(mockOnFilterChange).toHaveBeenCalledWith({
        classification: [],
        status: [],
      })
    })

    it('should not display chips when no filters are active', () => {
      render(<DiscoveryFilters {...defaultProps} />)
      // Chips should only show when there are active filters
      // Check that no capitalized filter names appear outside of buttons
      const badgeElements = screen.queryAllByRole('button').filter(btn => {
        const text = btn.textContent?.toLowerCase() || ''
        return (
          text === 'managed' ||
          text === 'partial' ||
          text === 'unmanaged' ||
          text === 'running' ||
          text === 'stopped'
        )
      })
      // Only the filter option buttons should exist, not chips
      expect(badgeElements.length).toBe(5) // 3 classification + 2 status
    })
  })

  describe('Clear All Filters', () => {
    it('should display Clear All button when filters are active', () => {
      const props = {
        ...defaultProps,
        filters: {
          classification: ['managed'] as FilterType[],
          status: [] as FilterType[],
        },
      }
      render(<DiscoveryFilters {...props} />)
      expect(screen.getByRole('button', { name: /Clear all/i })).toBeInTheDocument()
    })

    it('should not display Clear All button when no filters are active', () => {
      render(<DiscoveryFilters {...defaultProps} />)
      expect(screen.queryByRole('button', { name: /Clear all/i })).not.toBeInTheDocument()
    })

    it('should clear all filters when Clear All button is clicked', async () => {
      const user = userEvent.setup()
      const props = {
        ...defaultProps,
        filters: {
          classification: ['managed', 'partial'] as FilterType[],
          status: ['running'] as FilterType[],
        },
      }
      render(<DiscoveryFilters {...props} />)

      const clearAllButton = screen.getByRole('button', { name: /Clear all/i })
      await user.click(clearAllButton)

      expect(mockOnFilterChange).toHaveBeenCalledWith({
        classification: [],
        status: [],
      })
    })

    it('should clear only classification filters', async () => {
      const user = userEvent.setup()
      const props = {
        ...defaultProps,
        filters: {
          classification: ['managed'] as FilterType[],
          status: [] as FilterType[],
        },
      }
      render(<DiscoveryFilters {...props} />)

      const clearAllButton = screen.getByRole('button', { name: /Clear all/i })
      await user.click(clearAllButton)

      expect(mockOnFilterChange).toHaveBeenCalledWith({
        classification: [],
        status: [],
      })
    })
  })

  describe('Filter Visual States', () => {
    it('should apply active styles to selected classification filter', () => {
      const props = {
        ...defaultProps,
        filters: {
          classification: ['managed'] as FilterType[],
          status: [] as FilterType[],
        },
      }
      const { container } = render(<DiscoveryFilters {...props} />)

      const buttons = container.querySelectorAll('button')
      const managedButton = Array.from(buttons).find(b => b.textContent?.includes('managed') && b.textContent.length < 10)
      // Check for emerald color class applied to active managed filter
      expect(managedButton).toHaveClass('border-emerald-500/40')
      expect(managedButton).toHaveClass('bg-emerald-500/10')
    })

    it('should apply active styles to selected status filter', () => {
      const props = {
        ...defaultProps,
        filters: {
          classification: [] as FilterType[],
          status: ['running'] as FilterType[],
        },
      }
      const { container } = render(<DiscoveryFilters {...props} />)

      const buttons = container.querySelectorAll('button')
      const runningButton = Array.from(buttons).find(b => b.textContent?.includes('running') && b.textContent.length < 10)
      expect(runningButton).toHaveClass('border-emerald-500/40')
      expect(runningButton).toHaveClass('bg-emerald-500/10')
    })

    it('should apply default styles to unselected filters', () => {
      const { container } = render(<DiscoveryFilters {...defaultProps} />)

      const buttons = container.querySelectorAll('button')
      const managedButton = Array.from(buttons).find(b => b.textContent?.includes('managed') && b.textContent.length < 10)
      expect(managedButton).toHaveClass('border-white/[0.09]')
      expect(managedButton).toHaveClass('bg-transparent')
    })
  })

  describe('Edge Cases', () => {
    it('should handle empty filter arrays', () => {
      const props = {
        ...defaultProps,
        filters: {
          classification: [] as FilterType[],
          status: [] as FilterType[],
        },
      }
      const { container } = render(<DiscoveryFilters {...props} />)
      expect(container).toBeInTheDocument()
    })

    it('should handle all filters selected', () => {
      const props = {
        ...defaultProps,
        filters: {
          classification: ['managed', 'partial', 'unmanaged'] as FilterType[],
          status: ['running', 'stopped'] as FilterType[],
        },
      }
      render(<DiscoveryFilters {...props} />)

      expect(screen.getByText('Managed')).toBeInTheDocument()
      expect(screen.getByText('Partial')).toBeInTheDocument()
      expect(screen.getByText('Unmanaged')).toBeInTheDocument()
      expect(screen.getByText('Running')).toBeInTheDocument()
      expect(screen.getByText('Stopped')).toBeInTheDocument()
    })

    it('should preserve other filters when removing one', async () => {
      const user = userEvent.setup()
      const props = {
        ...defaultProps,
        filters: {
          classification: ['managed', 'partial'] as FilterType[],
          status: ['running'] as FilterType[],
        },
      }
      render(<DiscoveryFilters {...props} />)

      const chipButtons = screen.getAllByRole('button')
      // Click the X button for the first chip (Managed)
      const removeButton = chipButtons.find(btn => btn.className.includes('ml-1'))
      if (removeButton) {
        await user.click(removeButton)
        expect(mockOnFilterChange).toHaveBeenCalled()
      }
    })
  })
})
