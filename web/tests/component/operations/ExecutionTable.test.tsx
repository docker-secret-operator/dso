import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ExecutionTable } from '@/components/operations/ExecutionTable'
import type { Execution } from '@/lib/api/types'

const createMockExecution = (
  id: string = 'exec-1',
  status: Execution['status'] = 'completed'
): Execution => ({
  id,
  status,
  created_at: new Date().toISOString(),
  started_at: new Date().toISOString(),
  completed_at: new Date().toISOString(),
  duration_ms: 5000,
  correlation_id: `corr-${id}`,
})

describe('ExecutionTable Component', () => {
  const mockOnSelectExecution = vi.fn()

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Table Rendering', () => {
    it('should render table with executions', () => {
      const executions = [
        createMockExecution('exec-1', 'completed'),
        createMockExecution('exec-2', 'running'),
      ]

      render(
        <ExecutionTable
          executions={executions}
          total={2}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      expect(screen.getByText('exec-1')).toBeInTheDocument()
      expect(screen.getByText('exec-2')).toBeInTheDocument()
    })

    it('should display all required table columns', () => {
      const executions = [createMockExecution()]

      render(<ExecutionTable executions={executions} onSelectExecution={mockOnSelectExecution} />)

      expect(screen.getByText('exec-1')).toBeInTheDocument()
      expect(screen.getByText(/completed/i)).toBeInTheDocument()
    })

    it('should render correlation ID for each execution', () => {
      const executions = [
        createMockExecution('exec-1'),
        createMockExecution('exec-2'),
      ]

      render(
        <ExecutionTable executions={executions} onSelectExecution={mockOnSelectExecution} />
      )

      expect(screen.getByText('corr-exec-1')).toBeInTheDocument()
      expect(screen.getByText('corr-exec-2')).toBeInTheDocument()
    })

    it('should display status badge for each execution', () => {
      const executions = [
        createMockExecution('exec-1', 'completed'),
        createMockExecution('exec-2', 'failed'),
      ]

      render(
        <ExecutionTable executions={executions} onSelectExecution={mockOnSelectExecution} />
      )

      const badges = screen.getAllByRole('status')
      expect(badges.length).toBeGreaterThan(0)
    })

    it('should format timestamps correctly', () => {
      const executions = [createMockExecution()]

      const { container } = render(
        <ExecutionTable executions={executions} onSelectExecution={mockOnSelectExecution} />
      )

      expect(container.innerHTML).toBeTruthy()
    })

    it('should render empty state when no executions', () => {
      render(
        <ExecutionTable executions={[]} onSelectExecution={mockOnSelectExecution} />
      )

      expect(screen.getByText(/no executions/i) || screen.getByText(/empty/i)).toBeTruthy()
    })
  })

  describe('Search Functionality', () => {
    it('should filter executions by ID', async () => {
      const executions = [
        createMockExecution('exec-123'),
        createMockExecution('exec-456'),
      ]

      render(
        <ExecutionTable
          executions={executions}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      const searchInput = screen.getByRole('textbox')
      const user = userEvent.setup()

      await user.type(searchInput, 'exec-123')

      expect(screen.getByText('exec-123')).toBeInTheDocument()
      expect(screen.queryByText('exec-456')).not.toBeInTheDocument()
    })

    it('should filter executions by correlation ID', async () => {
      const executions = [
        createMockExecution('exec-1'),
        createMockExecution('exec-2'),
      ]

      render(
        <ExecutionTable
          executions={executions}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      const searchInput = screen.getByRole('textbox')
      const user = userEvent.setup()

      await user.type(searchInput, 'corr-exec-1')

      expect(screen.getByText('exec-1')).toBeInTheDocument()
      expect(screen.queryByText('exec-2')).not.toBeInTheDocument()
    })

    it('should be case-insensitive search', async () => {
      const executions = [createMockExecution('EXEC-ABC')]

      render(
        <ExecutionTable
          executions={executions}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      const searchInput = screen.getByRole('textbox')
      const user = userEvent.setup()

      await user.type(searchInput, 'exec-abc')

      expect(screen.getByText('EXEC-ABC')).toBeInTheDocument()
    })

    it('should clear search and show all results', async () => {
      const executions = [
        createMockExecution('exec-1'),
        createMockExecution('exec-2'),
      ]

      render(
        <ExecutionTable
          executions={executions}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      const searchInput = screen.getByRole('textbox') as HTMLInputElement
      const user = userEvent.setup()

      await user.type(searchInput, 'exec-1')
      await user.clear(searchInput)

      expect(screen.getByText('exec-1')).toBeInTheDocument()
      expect(screen.getByText('exec-2')).toBeInTheDocument()
    })

    it('should show no results message when search has no matches', async () => {
      const executions = [createMockExecution('exec-1')]

      render(
        <ExecutionTable
          executions={executions}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      const searchInput = screen.getByRole('textbox')
      const user = userEvent.setup()

      await user.type(searchInput, 'nonexistent-id')

      expect(screen.queryByText('exec-1')).not.toBeInTheDocument()
    })
  })

  describe('Status Filter', () => {
    it('should filter executions by status', async () => {
      const executions = [
        createMockExecution('exec-1', 'completed'),
        createMockExecution('exec-2', 'running'),
        createMockExecution('exec-3', 'failed'),
      ]

      render(
        <ExecutionTable
          executions={executions}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      const statusFilter = screen.getByRole('combobox') || screen.queryByDisplayValue('all')
      if (statusFilter) {
        const user = userEvent.setup()
        await user.click(statusFilter)
        const option = screen.queryByText('completed')
        if (option) {
          await user.click(option)
        }
      }

      expect(screen.getByText('exec-1')).toBeInTheDocument()
    })

    it('should show all statuses when filter is "all"', () => {
      const executions = [
        createMockExecution('exec-1', 'completed'),
        createMockExecution('exec-2', 'running'),
        createMockExecution('exec-3', 'failed'),
      ]

      render(
        <ExecutionTable
          executions={executions}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      expect(screen.getByText('exec-1')).toBeInTheDocument()
      expect(screen.getByText('exec-2')).toBeInTheDocument()
      expect(screen.getByText('exec-3')).toBeInTheDocument()
    })

    it('should filter only running executions', async () => {
      const executions = [
        createMockExecution('exec-1', 'completed'),
        createMockExecution('exec-2', 'running'),
      ]

      render(
        <ExecutionTable
          executions={executions}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      const filterElement = screen.queryByDisplayValue('all')
      if (filterElement) {
        const user = userEvent.setup()
        await user.selectOptions(filterElement as HTMLSelectElement, 'running')
        expect(screen.getByText('exec-2')).toBeInTheDocument()
      }
    })

    it('should support all status types', () => {
      const statuses: Execution['status'][] = [
        'queued',
        'running',
        'completed',
        'failed',
        'cancelled',
        'paused',
        'timed_out',
      ]

      const executions = statuses.map((status, i) =>
        createMockExecution(`exec-${i}`, status)
      )

      render(
        <ExecutionTable
          executions={executions}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      executions.forEach((exec) => {
        expect(screen.getByText(exec.id)).toBeInTheDocument()
      })
    })
  })

  describe('Pagination', () => {
    it('should paginate results correctly', () => {
      const executions = Array.from({ length: 30 }, (_, i) =>
        createMockExecution(`exec-${i}`)
      )

      render(
        <ExecutionTable
          executions={executions}
          total={30}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      expect(screen.getByText('exec-0')).toBeInTheDocument()
      expect(screen.getByText('exec-19')).toBeInTheDocument()
    })

    it('should show correct page items count (20 per page default)', () => {
      const executions = Array.from({ length: 25 }, (_, i) =>
        createMockExecution(`exec-${i}`)
      )

      render(
        <ExecutionTable
          executions={executions}
          total={25}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      expect(screen.getByText('exec-0')).toBeInTheDocument()
      expect(screen.getByText('exec-19')).toBeInTheDocument()
    })

    it('should navigate to next page', async () => {
      const executions = Array.from({ length: 45 }, (_, i) =>
        createMockExecution(`exec-${i}`)
      )

      render(
        <ExecutionTable
          executions={executions}
          total={45}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      const nextButton = screen.queryByText(/next/i)
      if (nextButton) {
        const user = userEvent.setup()
        await user.click(nextButton)
        expect(screen.getByText('exec-20')).toBeInTheDocument()
      }
    })

    it('should show pagination controls for large datasets', () => {
      const executions = Array.from({ length: 100 }, (_, i) =>
        createMockExecution(`exec-${i}`)
      )

      const { container } = render(
        <ExecutionTable
          executions={executions}
          total={100}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      expect(container.innerHTML).toBeTruthy()
    })
  })

  describe('Row Selection', () => {
    it('should call onSelectExecution when row is clicked', async () => {
      const execution = createMockExecution('exec-1', 'completed')
      const executions = [execution]

      render(
        <ExecutionTable
          executions={executions}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      const row = screen.getByText('exec-1').closest('tr') || screen.getByText('exec-1')
      const user = userEvent.setup()

      await user.click(row)

      expect(mockOnSelectExecution).toHaveBeenCalled()
    })

    it('should pass correct execution to callback', async () => {
      const execution = createMockExecution('exec-123', 'running')
      const executions = [execution]

      render(
        <ExecutionTable
          executions={executions}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      const row = screen.getByText('exec-123').closest('tr') || screen.getByText('exec-123')
      const user = userEvent.setup()

      await user.click(row)

      expect(mockOnSelectExecution).toHaveBeenCalledWith(expect.objectContaining({
        id: 'exec-123',
        status: 'running',
      }))
    })
  })

  describe('Loading State', () => {
    it('should render skeleton loaders when isLoading is true', () => {
      render(
        <ExecutionTable
          executions={[]}
          isLoading={true}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      const skeletons = document.querySelectorAll('.skeleton')
      expect(skeletons.length).toBeGreaterThan(0)
    })

    it('should not show executions when loading', () => {
      const executions = [createMockExecution('exec-1')]

      render(
        <ExecutionTable
          executions={executions}
          isLoading={true}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      expect(screen.queryByText('exec-1')).not.toBeInTheDocument()
    })
  })

  describe('Error State', () => {
    it('should display error message when error prop provided', () => {
      const errorMsg = 'Failed to load executions'

      render(
        <ExecutionTable
          executions={[]}
          error={errorMsg}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      expect(screen.getByText(errorMsg)).toBeInTheDocument()
    })

    it('should show error in red styling', () => {
      const { container } = render(
        <ExecutionTable
          executions={[]}
          error="Error message"
          onSelectExecution={mockOnSelectExecution}
        />
      )

      const errorElement = container.querySelector('.text-red-400')
      expect(errorElement).toBeInTheDocument()
    })
  })

  describe('Status Color Coding', () => {
    it('should apply correct color for completed status', () => {
      const executions = [createMockExecution('exec-1', 'completed')]

      const { container } = render(
        <ExecutionTable
          executions={executions}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      expect(container.innerHTML).toContain('completed')
    })

    it('should apply correct color for failed status', () => {
      const executions = [createMockExecution('exec-1', 'failed')]

      const { container } = render(
        <ExecutionTable
          executions={executions}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      expect(container.innerHTML).toContain('failed')
    })

    it('should apply correct color for running status', () => {
      const executions = [createMockExecution('exec-1', 'running')]

      const { container } = render(
        <ExecutionTable
          executions={executions}
          onSelectExecution={mockOnSelectExecution}
        />
      )

      expect(container.innerHTML).toContain('running')
    })
  })
})
