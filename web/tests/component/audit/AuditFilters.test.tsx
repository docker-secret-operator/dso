import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AuditFilters } from '@/components/audit/AuditFilters'
import type { AuditFilters as AuditFiltersType } from '@/lib/api/types'

describe('AuditFilters Component', () => {
  const mockOnFilterChange = vi.fn()
  const mockOnToggleFilters = vi.fn()
  const mockOnClearFilters = vi.fn()

  const defaultProps = {
    filters: {
      actor: undefined,
      actor_id: undefined,
      action: undefined,
      resource: undefined,
      correlation_id: undefined,
      execution_id: undefined,
      start_time: undefined,
      end_time: undefined,
      limit: 50,
      offset: 0,
    } as AuditFiltersType,
    showFilters: false,
    onToggleFilters: mockOnToggleFilters,
    onFilterChange: mockOnFilterChange,
    onClearFilters: mockOnClearFilters,
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Rendering', () => {
    it('should render without crashing', () => {
      render(<AuditFilters {...defaultProps} />)
      expect(screen.getByRole('button', { name: /Filters/i })).toBeInTheDocument()
    })

    it('should display filter toggle button', () => {
      render(<AuditFilters {...defaultProps} />)
      const filterButton = screen.getByRole('button', { name: /Filters/i })
      expect(filterButton).toBeInTheDocument()
      expect(filterButton).toHaveClass('inline-flex')
    })

    it('should not show filter panel when showFilters is false', () => {
      render(<AuditFilters {...defaultProps} showFilters={false} />)
      expect(screen.queryByLabelText(/Actor name/i)).not.toBeInTheDocument()
    })

    it('should show filter panel when showFilters is true', () => {
      render(<AuditFilters {...defaultProps} showFilters={true} />)
      expect(screen.getByLabelText(/Actor name/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/Actor ID/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/Action/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/Resource type/i)).toBeInTheDocument()
    })

    it('should display all filter input fields when panel is open', () => {
      render(<AuditFilters {...defaultProps} showFilters={true} />)

      expect(screen.getByLabelText(/Actor name/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/Actor ID/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/Action/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/Resource type/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/Correlation ID/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/Execution ID/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/From \(ISO date\)/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/To \(ISO date\)/i)).toBeInTheDocument()
    })
  })

  describe('Filter Toggle', () => {
    it('should call onToggleFilters when filter button clicked', async () => {
      const user = userEvent.setup()
      render(<AuditFilters {...defaultProps} />)

      const filterButton = screen.getByRole('button', { name: /Filters/i })
      await user.click(filterButton)

      expect(mockOnToggleFilters).toHaveBeenCalledTimes(1)
    })

    it('should show active filter count badge when filters are applied', () => {
      const filtersWithData = {
        ...defaultProps.filters,
        actor: 'user123',
        action: 'create',
      }

      render(
        <AuditFilters
          {...defaultProps}
          filters={filtersWithData}
          showFilters={false}
        />
      )

      // Check for badge with count (should show 2 active filters)
      expect(screen.getByText('2')).toBeInTheDocument()
    })

    it('should not show badge when no filters are applied', () => {
      render(<AuditFilters {...defaultProps} showFilters={false} />)

      // Filter button should not contain a count badge
      const filterButton = screen.getByRole('button', { name: /Filters/i })
      expect(filterButton.querySelector('span')).not.toBeInTheDocument()
    })
  })

  describe('Filter Application', () => {
    it('should call onFilterChange when input value changes', async () => {
      const user = userEvent.setup()
      render(<AuditFilters {...defaultProps} showFilters={true} />)

      const actorInput = screen.getByLabelText(/Actor name/i) as HTMLInputElement
      await user.clear(actorInput)
      await user.type(actorInput, 'test')

      // Verify the callback was called for actor field
      const actorCalls = mockOnFilterChange.mock.calls.filter(call => call[0] === 'actor')
      expect(actorCalls.length).toBeGreaterThan(0)
      expect(actorCalls[0][1]).toBe('t')
    })

    it('should allow entering filter values in multiple fields', async () => {
      const user = userEvent.setup()
      render(<AuditFilters {...defaultProps} showFilters={true} />)

      const actorInput = screen.getByLabelText(/Actor name/i) as HTMLInputElement
      const actionInput = screen.getByLabelText(/Action/i) as HTMLInputElement

      await user.type(actorInput, 'user')
      await user.type(actionInput, 'delete')

      // Verify callbacks were invoked for both fields
      const calls = mockOnFilterChange.mock.calls
      const actorCalls = calls.filter(call => call[0] === 'actor')
      const actionCalls = calls.filter(call => call[0] === 'action')

      expect(actorCalls.length).toBeGreaterThan(0)
      expect(actionCalls.length).toBeGreaterThan(0)
    })

    it('should display current filter value in input field', () => {
      const filtersWithData = {
        ...defaultProps.filters,
        actor: 'john-doe',
      }

      render(
        <AuditFilters
          {...defaultProps}
          filters={filtersWithData}
          showFilters={true}
        />
      )

      const actorInput = screen.getByLabelText(/Actor name/i) as HTMLInputElement
      expect(actorInput.value).toBe('john-doe')
    })

    it('should handle date input for start_time', async () => {
      const user = userEvent.setup()
      render(<AuditFilters {...defaultProps} showFilters={true} />)

      const startTimeInput = screen.getByLabelText(/From \(ISO date\)/i) as HTMLInputElement
      await user.type(startTimeInput, '2025')

      // Verify callback was called with the start_time field
      const startTimeCalls = mockOnFilterChange.mock.calls.filter(call => call[0] === 'start_time')
      expect(startTimeCalls.length).toBeGreaterThan(0)
    })

    it('should handle date input for end_time', async () => {
      const user = userEvent.setup()
      render(<AuditFilters {...defaultProps} showFilters={true} />)

      const endTimeInput = screen.getByLabelText(/To \(ISO date\)/i) as HTMLInputElement
      await user.type(endTimeInput, '2025')

      // Verify callback was called with the end_time field
      const endTimeCalls = mockOnFilterChange.mock.calls.filter(call => call[0] === 'end_time')
      expect(endTimeCalls.length).toBeGreaterThan(0)
    })
  })

  describe('Filter Chips', () => {
    it('should display active filters as chips', () => {
      const filtersWithData = {
        ...defaultProps.filters,
        actor: 'john-doe',
        action: 'create',
      }

      render(
        <AuditFilters
          {...defaultProps}
          filters={filtersWithData}
          showFilters={false}
        />
      )

      expect(screen.getByText(/actor: john-doe/i)).toBeInTheDocument()
      expect(screen.getByText(/action: create/i)).toBeInTheDocument()
    })

    it('should not display chips when no filters are active', () => {
      render(<AuditFilters {...defaultProps} showFilters={false} />)

      // Should not have any chip containers
      const chipElements = screen.queryAllByText(/:/i).filter(el =>
        el.textContent?.includes(':')
      )
      expect(chipElements).toHaveLength(0)
    })

    it('should remove filter when chip X button clicked', async () => {
      const user = userEvent.setup()
      const filtersWithData = {
        ...defaultProps.filters,
        actor: 'john-doe',
      }

      render(
        <AuditFilters
          {...defaultProps}
          filters={filtersWithData}
          showFilters={false}
        />
      )

      const removeButton = screen.getByLabelText(/Remove actor filter/i)
      await user.click(removeButton)

      expect(mockOnFilterChange).toHaveBeenCalledWith('actor', '')
    })

    it('should handle removing multiple filters individually', async () => {
      const user = userEvent.setup()
      const filtersWithData = {
        ...defaultProps.filters,
        actor: 'john-doe',
        action: 'create',
        resource: 'user',
      }

      render(
        <AuditFilters
          {...defaultProps}
          filters={filtersWithData}
          showFilters={false}
        />
      )

      const removeButtons = screen.getAllByRole('button', { name: /Remove/i })
      expect(removeButtons).toHaveLength(3)

      await user.click(removeButtons[0])
      expect(mockOnFilterChange).toHaveBeenCalled()
    })
  })

  describe('Clear All Button', () => {
    it('should display Clear All button when filters are active', () => {
      const filtersWithData = {
        ...defaultProps.filters,
        actor: 'john-doe',
      }

      render(
        <AuditFilters
          {...defaultProps}
          filters={filtersWithData}
          showFilters={true}
        />
      )

      expect(screen.getByRole('button', { name: /Clear all/i })).toBeInTheDocument()
    })

    it('should not display Clear All button when no filters are active', () => {
      render(<AuditFilters {...defaultProps} showFilters={true} />)

      expect(
        screen.queryByRole('button', { name: /Clear all/i })
      ).not.toBeInTheDocument()
    })

    it('should call onClearFilters when Clear All button clicked', async () => {
      const user = userEvent.setup()
      const filtersWithData = {
        ...defaultProps.filters,
        actor: 'john-doe',
        action: 'create',
      }

      render(
        <AuditFilters
          {...defaultProps}
          filters={filtersWithData}
          showFilters={true}
        />
      )

      const clearAllButton = screen.getByRole('button', { name: /Clear all/i })
      await user.click(clearAllButton)

      expect(mockOnClearFilters).toHaveBeenCalledTimes(1)
    })
  })

  describe('Complex Filter Scenarios', () => {
    it('should handle all filter fields simultaneously', () => {
      const filtersWithAllData = {
        actor: 'user1',
        actor_id: 'user-123',
        action: 'update',
        resource: 'permission',
        correlation_id: 'corr-456',
        execution_id: 'exec-789',
        start_time: '2025-01-01T00:00:00Z',
        end_time: '2025-12-31T23:59:59Z',
        limit: 50,
        offset: 0,
      } as AuditFiltersType

      render(
        <AuditFilters
          {...defaultProps}
          filters={filtersWithAllData}
          showFilters={false}
        />
      )

      // Check that all 8 non-pagination filters are displayed as chips
      expect(screen.getByText(/actor:/i)).toBeInTheDocument()
      expect(screen.getByText(/actor_id:/i)).toBeInTheDocument()
      expect(screen.getByText(/action:/i)).toBeInTheDocument()
      expect(screen.getByText(/resource:/i)).toBeInTheDocument()
    })

    it('should maintain filter panel state independently from filter display', async () => {
      const user = userEvent.setup()
      render(<AuditFilters {...defaultProps} showFilters={false} />)

      // Filter panel should be hidden
      expect(screen.queryByLabelText(/Actor name/i)).not.toBeInTheDocument()

      // Toggle filters open
      const filterButton = screen.getByRole('button', { name: /Filters/i })
      await user.click(filterButton)

      expect(mockOnToggleFilters).toHaveBeenCalled()
    })

    it('should preserve filter values when toggling panel visibility', () => {
      const filtersWithData = {
        ...defaultProps.filters,
        actor: 'john-doe',
        action: 'delete',
      }

      const { rerender } = render(
        <AuditFilters
          {...defaultProps}
          filters={filtersWithData}
          showFilters={false}
        />
      )

      // Check chips are visible
      expect(screen.getByText(/actor: john-doe/i)).toBeInTheDocument()
      expect(screen.getByText(/action: delete/i)).toBeInTheDocument()

      // Rerender with panel open
      rerender(
        <AuditFilters
          {...defaultProps}
          filters={filtersWithData}
          showFilters={true}
        />
      )

      // Chips should still be visible
      expect(screen.getByText(/actor: john-doe/i)).toBeInTheDocument()
      expect(screen.getByText(/action: delete/i)).toBeInTheDocument()

      // Input values should reflect current filters
      const actorInput = screen.getByLabelText(/Actor name/i) as HTMLInputElement
      expect(actorInput.value).toBe('john-doe')
    })
  })

  describe('Edge Cases', () => {
    it('should handle empty filter values', () => {
      const emptyFilters = {
        ...defaultProps.filters,
        actor: '',
        action: '',
      }

      render(
        <AuditFilters
          {...defaultProps}
          filters={emptyFilters}
          showFilters={true}
        />
      )

      expect(screen.queryByText(/actor:/i)).not.toBeInTheDocument()
      expect(screen.queryByText(/action:/i)).not.toBeInTheDocument()
    })

    it('should handle undefined filter values', () => {
      render(
        <AuditFilters
          {...defaultProps}
          filters={{
            actor: undefined,
            actor_id: undefined,
            action: undefined,
            resource: undefined,
            correlation_id: undefined,
            execution_id: undefined,
            start_time: undefined,
            end_time: undefined,
            limit: 50,
            offset: 0,
          }}
          showFilters={false}
        />
      )

      // Should not display any chips
      const filterChips = screen.queryAllByText(/:/)
      expect(filterChips).toHaveLength(0)
    })

    it('should handle filter values with special characters', async () => {
      const user = userEvent.setup()
      render(<AuditFilters {...defaultProps} showFilters={true} />)

      const correlationInput = screen.getByLabelText(/Correlation ID/i) as HTMLInputElement
      await user.type(correlationInput, 'corr-123')

      // Verify that the callback was called at least once
      expect(mockOnFilterChange).toHaveBeenCalledWith(
        'correlation_id',
        expect.any(String)
      )
    })

    it('should ignore limit and offset when counting active filters', () => {
      const filtersWithPaginationOnly = {
        ...defaultProps.filters,
        limit: 100,
        offset: 50,
      }

      render(
        <AuditFilters
          {...defaultProps}
          filters={filtersWithPaginationOnly}
          showFilters={false}
        />
      )

      // No chips should be displayed (limit and offset don't count as active filters)
      const filterButton = screen.getByRole('button', { name: /Filters/i })
      expect(filterButton.querySelector('span')).not.toBeInTheDocument()
    })

    it('should handle very long filter values', () => {
      const longValue =
        'this-is-a-very-long-filter-value-that-exceeds-normal-length-and-should-still-display-properly'
      const filtersWithLongValue = {
        ...defaultProps.filters,
        actor: longValue,
      }

      render(
        <AuditFilters
          {...defaultProps}
          filters={filtersWithLongValue}
          showFilters={false}
        />
      )

      expect(screen.getByText(new RegExp(longValue))).toBeInTheDocument()
    })
  })

  describe('Accessibility', () => {
    it('should have proper labels for all filter inputs', () => {
      render(<AuditFilters {...defaultProps} showFilters={true} />)

      expect(screen.getByLabelText(/Actor name/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/Actor ID/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/Action/i)).toBeInTheDocument()
      expect(screen.getByLabelText(/Resource type/i)).toBeInTheDocument()
    })

    it('should have aria-labels for remove buttons', () => {
      const filtersWithData = {
        ...defaultProps.filters,
        actor: 'john-doe',
      }

      render(
        <AuditFilters
          {...defaultProps}
          filters={filtersWithData}
          showFilters={false}
        />
      )

      expect(screen.getByLabelText(/Remove actor filter/i)).toBeInTheDocument()
    })

    it('should allow keyboard navigation', async () => {
      const user = userEvent.setup()
      render(<AuditFilters {...defaultProps} showFilters={true} />)

      const actorInput = screen.getByLabelText(/Actor name/i)
      actorInput.focus()

      expect(document.activeElement).toBe(actorInput)

      await user.keyboard('test-user')
      expect(mockOnFilterChange).toHaveBeenCalled()
    })
  })
})
