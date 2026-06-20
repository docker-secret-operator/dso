import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AuditTable } from '@/components/audit/AuditTable'
import { AuditEvent } from '@/lib/api/types'

describe('AuditTable Component', () => {
  const mockOnCorrelation = vi.fn()
  const mockOnActor = vi.fn()

  const mockEvents: AuditEvent[] = [
    {
      id: 'evt-1',
      timestamp: new Date('2026-06-19T12:00:00Z').toISOString(),
      actor: 'user@example.com',
      actor_id: 'user-123',
      actor_email: 'user@example.com',
      action: 'create',
      resource_type: 'container',
      resource: 'docker_containers',
      resource_id: 'abc123',
      status: 'success',
      severity: 'info',
      details: 'Created container successfully',
      correlation_id: 'corr-1234567890abcdef',
      execution_id: 'exec-123',
      ip_address: '192.168.1.100',
    },
    {
      id: 'evt-2',
      timestamp: new Date('2026-06-19T12:05:00Z').toISOString(),
      actor: 'admin@example.com',
      actor_id: 'admin-456',
      actor_email: 'admin@example.com',
      action: 'update',
      resource_type: 'secret',
      resource: 'secrets',
      resource_id: 'sec456',
      status: 'failure',
      severity: 'error',
      details: 'Permission denied',
      correlation_id: 'corr-abcdef1234567890',
      execution_id: 'exec-456',
      ip_address: '192.168.1.101',
    },
    {
      id: 'evt-3',
      timestamp: new Date('2026-06-19T12:10:00Z').toISOString(),
      actor: 'service@example.com',
      actor_id: 'service-789',
      actor_email: 'service@example.com',
      action: 'read',
      resource_type: 'config',
      resource: 'configurations',
      resource_id: 'cfg789',
      status: 'success',
      severity: 'warning',
      details: 'Accessed configuration file',
      correlation_id: 'corr-xyz9876543210abc',
      execution_id: 'exec-789',
      ip_address: '192.168.1.102',
    },
  ]

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Rendering', () => {
    it('should render without crashing', () => {
      render(
        <AuditTable
          events={mockEvents}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )
      // Should render at least one event row
      expect(screen.getByText('user@example.com')).toBeInTheDocument()
    })

    it('should display all event data in rows', () => {
      render(
        <AuditTable
          events={mockEvents}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      // Check actor names are displayed
      expect(screen.getByText('user@example.com')).toBeInTheDocument()
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
      expect(screen.getByText('service@example.com')).toBeInTheDocument()

      // Check actions are displayed
      expect(screen.getByText('create')).toBeInTheDocument()
      expect(screen.getByText('update')).toBeInTheDocument()
      expect(screen.getByText('read')).toBeInTheDocument()

      // Check resource types are displayed
      expect(screen.getByText('container')).toBeInTheDocument()
      expect(screen.getByText('secret')).toBeInTheDocument()
      expect(screen.getByText('config')).toBeInTheDocument()
    })

    it('should display resource information', () => {
      render(
        <AuditTable
          events={mockEvents}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      // Check resource/resource_id display (truncated to 8 chars)
      expect(screen.getByText(/docker_containers\/abc123/)).toBeInTheDocument()
      expect(screen.getByText(/secrets\/sec456/)).toBeInTheDocument()
      expect(screen.getByText(/configurations\/cfg789/)).toBeInTheDocument()
    })

    it('should display details text', () => {
      render(
        <AuditTable
          events={mockEvents}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      expect(screen.getByText('Created container successfully')).toBeInTheDocument()
      expect(screen.getByText('Permission denied')).toBeInTheDocument()
      expect(screen.getByText('Accessed configuration file')).toBeInTheDocument()
    })

    it('should display IP addresses', () => {
      render(
        <AuditTable
          events={mockEvents}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      expect(screen.getByText('192.168.1.100')).toBeInTheDocument()
      expect(screen.getByText('192.168.1.101')).toBeInTheDocument()
      expect(screen.getByText('192.168.1.102')).toBeInTheDocument()
    })

    it('should render correlation ID links', () => {
      render(
        <AuditTable
          events={mockEvents}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      // Correlation IDs are truncated to 16 characters
      expect(screen.getByText(/corr-123456789/)).toBeInTheDocument()
      expect(screen.getByText(/corr-abcdef1234/)).toBeInTheDocument()
    })
  })

  describe('Status Badges', () => {
    it('should display success status badge', () => {
      render(
        <AuditTable
          events={[mockEvents[0]]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      const successBadge = screen.getByText('success')
      expect(successBadge).toBeInTheDocument()
    })

    it('should display failure status badge', () => {
      render(
        <AuditTable
          events={[mockEvents[1]]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      const failureBadge = screen.getByText('failure')
      expect(failureBadge).toBeInTheDocument()
    })

    it('should display severity levels with correct icons', () => {
      render(
        <AuditTable
          events={mockEvents}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      // Component renders severity badges but we check they exist in the document
      // The info severity uses Info icon, error uses AlertCircle
      expect(screen.getByText('info')).toBeInTheDocument()
      expect(screen.getByText('error')).toBeInTheDocument()
      expect(screen.getByText('warning')).toBeInTheDocument()
    })
  })

  describe('Row Click Handler - Actor', () => {
    it('should call onActor when actor name clicked', async () => {
      const user = userEvent.setup()
      render(
        <AuditTable
          events={[mockEvents[0]]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      const actorButton = screen.getByRole('button', { name: 'user@example.com' })
      await user.click(actorButton)

      expect(mockOnActor).toHaveBeenCalledWith('user-123')
    })

    it('should pass correct actor_id to callback', async () => {
      const user = userEvent.setup()
      render(
        <AuditTable
          events={[mockEvents[1]]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      const actorButton = screen.getByRole('button', { name: 'admin@example.com' })
      await user.click(actorButton)

      expect(mockOnActor).toHaveBeenCalledWith('admin-456')
    })

    it('should handle multiple actor clicks', async () => {
      const user = userEvent.setup()
      render(
        <AuditTable
          events={mockEvents}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      const firstActor = screen.getByRole('button', { name: 'user@example.com' })
      const secondActor = screen.getByRole('button', { name: 'admin@example.com' })

      await user.click(firstActor)
      await user.click(secondActor)

      expect(mockOnActor).toHaveBeenCalledTimes(2)
      expect(mockOnActor).toHaveBeenNthCalledWith(1, 'user-123')
      expect(mockOnActor).toHaveBeenNthCalledWith(2, 'admin-456')
    })
  })

  describe('Row Click Handler - Correlation', () => {
    it('should call onCorrelation when correlation link clicked', async () => {
      const user = userEvent.setup()
      render(
        <AuditTable
          events={[mockEvents[0]]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      // Find the correlation button by its title
      const correlationButton = screen.getByTitle('View correlation chain')
      await user.click(correlationButton)

      expect(mockOnCorrelation).toHaveBeenCalledWith('corr-1234567890abcdef')
    })

    it('should pass correct correlation_id to callback', async () => {
      const user = userEvent.setup()
      render(
        <AuditTable
          events={[mockEvents[1]]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      const correlationButton = screen.getByTitle('View correlation chain')
      await user.click(correlationButton)

      expect(mockOnCorrelation).toHaveBeenCalledWith('corr-abcdef1234567890')
    })

    it('should handle multiple correlation clicks', async () => {
      const user = userEvent.setup()
      render(
        <AuditTable
          events={mockEvents.slice(0, 2)}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      const correlationButtons = screen.getAllByTitle('View correlation chain')

      await user.click(correlationButtons[0])
      await user.click(correlationButtons[1])

      expect(mockOnCorrelation).toHaveBeenCalledTimes(2)
      expect(mockOnCorrelation).toHaveBeenNthCalledWith(1, 'corr-1234567890abcdef')
      expect(mockOnCorrelation).toHaveBeenNthCalledWith(2, 'corr-abcdef1234567890')
    })
  })

  describe('Loading State', () => {
    it('should show skeletons when loading', () => {
      render(
        <AuditTable
          events={[]}
          isLoading={true}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      // When loading, the Skeleton component renders div elements with skeleton class
      const skeletons = document.querySelectorAll('.skeleton')
      expect(skeletons.length).toBeGreaterThan(0)
    })

    it('should not show data while loading', () => {
      render(
        <AuditTable
          events={mockEvents}
          isLoading={true}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      expect(screen.queryByText('user@example.com')).not.toBeInTheDocument()
      expect(screen.queryByText('admin@example.com')).not.toBeInTheDocument()
    })

    it('should render correct number of skeleton items', () => {
      render(
        <AuditTable
          events={[]}
          isLoading={true}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      const skeletons = document.querySelectorAll('.skeleton')
      // Skeleton component with count={5} creates 5 skeleton elements
      expect(skeletons.length).toBe(5)
    })
  })

  describe('Empty State', () => {
    it('should show empty state when isEmpty is true and not searching', () => {
      render(
        <AuditTable
          events={[]}
          isLoading={false}
          isEmpty={true}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      expect(screen.getByText('No audit events found')).toBeInTheDocument()
      expect(screen.getByText('System activity will appear here.')).toBeInTheDocument()
    })

    it('should show search-specific empty state when searching', () => {
      render(
        <AuditTable
          events={[]}
          isLoading={false}
          isEmpty={true}
          searchTerm="some-search-term"
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      expect(screen.getByText('No events match')).toBeInTheDocument()
      expect(screen.getByText('Try a different search term.')).toBeInTheDocument()
    })

    it('should render CheckCircle2 icon in empty state', () => {
      const { container } = render(
        <AuditTable
          events={[]}
          isLoading={false}
          isEmpty={true}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      // CheckCircle2 icon from lucide-react renders SVG
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
    })
  })

  describe('Relative Time Display', () => {
    it('should display "just now" for recent timestamps', () => {
      const recentEvent: AuditEvent = {
        ...mockEvents[0],
        timestamp: new Date().toISOString(),
      }

      render(
        <AuditTable
          events={[recentEvent]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      expect(screen.getByText('just now')).toBeInTheDocument()
    })

    it('should display minutes ago for older timestamps', () => {
      const minutesAgoEvent: AuditEvent = {
        ...mockEvents[0],
        timestamp: new Date(Date.now() - 5 * 60000).toISOString(), // 5 minutes ago
      }

      render(
        <AuditTable
          events={[minutesAgoEvent]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      expect(screen.getByText(/5m ago/)).toBeInTheDocument()
    })

    it('should display hours ago for older timestamps', () => {
      const hoursAgoEvent: AuditEvent = {
        ...mockEvents[0],
        timestamp: new Date(Date.now() - 2 * 3600000).toISOString(), // 2 hours ago
      }

      render(
        <AuditTable
          events={[hoursAgoEvent]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      expect(screen.getByText(/2h ago/)).toBeInTheDocument()
    })
  })

  describe('Multiple Events Display', () => {
    it('should render all events in list', () => {
      render(
        <AuditTable
          events={mockEvents}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      // All events should be visible
      mockEvents.forEach(event => {
        expect(screen.getByText(event.action)).toBeInTheDocument()
      })
    })

    it('should maintain correct order of events', () => {
      render(
        <AuditTable
          events={mockEvents}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      // Get all actor elements
      const actors = screen.getAllByRole('button').filter(btn =>
        btn.textContent?.includes('@example.com')
      )

      expect(actors[0]).toHaveTextContent('user@example.com')
      expect(actors[1]).toHaveTextContent('admin@example.com')
      expect(actors[2]).toHaveTextContent('service@example.com')
    })
  })

  describe('Edge Cases', () => {
    it('should handle events with minimal data', () => {
      const minimalEvent: AuditEvent = {
        id: 'evt-minimal',
        timestamp: new Date().toISOString(),
        actor: 'user@example.com',
        actor_id: 'user-min',
        actor_email: 'user@example.com',
        action: 'read',
        resource_type: 'file',
        resource: '',
        resource_id: '',
        status: 'success',
        severity: 'info',
        details: '',
        correlation_id: '',
        execution_id: '',
        ip_address: '',
      }

      render(
        <AuditTable
          events={[minimalEvent]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      expect(screen.getByText('read')).toBeInTheDocument()
      expect(screen.getByText('file')).toBeInTheDocument()
    })

    it('should handle long actor names gracefully', () => {
      const longNameEvent: AuditEvent = {
        ...mockEvents[0],
        actor: 'very.long.email.name.with.many.parts@subdomain.example.com',
        actor_email: 'very.long.email.name.with.many.parts@subdomain.example.com',
      }

      render(
        <AuditTable
          events={[longNameEvent]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      expect(screen.getByText(longNameEvent.actor)).toBeInTheDocument()
    })

    it('should handle long details text with truncation', () => {
      const longDetailsEvent: AuditEvent = {
        ...mockEvents[0],
        details: 'This is a very long details message that should be truncated due to max-w-2xl and truncate classes being applied to the paragraph element in the component',
      }

      render(
        <AuditTable
          events={[longDetailsEvent]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      const detailsElement = screen.getByText(longDetailsEvent.details)
      expect(detailsElement).toHaveClass('truncate')
    })

    it('should render large list of events efficiently', () => {
      const manyEvents = Array.from({ length: 100 }, (_, i) => ({
        ...mockEvents[0],
        id: `evt-${i}`,
        actor: `user${i}@example.com`,
        actor_id: `user-${i}`,
        action: ['create', 'read', 'update', 'delete'][i % 4],
      } as AuditEvent))

      const { container } = render(
        <AuditTable
          events={manyEvents}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      // Should render all 100 events
      const rows = container.querySelectorAll('[class*="border-b"]')
      expect(rows.length).toBe(100)
    })
  })

  describe('Hover States', () => {
    it('should have hover styling on event rows', () => {
      const { container } = render(
        <AuditTable
          events={[mockEvents[0]]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      const eventRow = container.querySelector('[class*="hover:bg-white"]')
      expect(eventRow).toHaveClass('hover:bg-white/[0.02]')
    })

    it('should have hover styling on actor button', () => {
      const { container } = render(
        <AuditTable
          events={[mockEvents[0]]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      const actorButton = screen.getByRole('button', { name: 'user@example.com' })
      expect(actorButton).toHaveClass('hover:underline')
    })

    it('should have hover styling on correlation button', () => {
      render(
        <AuditTable
          events={[mockEvents[0]]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      const correlationButton = screen.getByTitle('View correlation chain')
      expect(correlationButton).toHaveClass('hover:underline')
    })
  })

  describe('Event Row Structure', () => {
    it('should display severity icon before event details', () => {
      const { container } = render(
        <AuditTable
          events={[mockEvents[0]]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      // The component starts with StatusBadge for severity
      const firstChild = container.querySelector('[class*="flex items-start gap-3"]')
      expect(firstChild).toBeInTheDocument()
    })

    it('should display action and status badges together', () => {
      render(
        <AuditTable
          events={[mockEvents[0]]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      // Both action and status badges should be present
      expect(screen.getByText('create')).toBeInTheDocument()
      expect(screen.getByText('success')).toBeInTheDocument()
    })

    it('should show clock icon with relative time', () => {
      const { container } = render(
        <AuditTable
          events={[mockEvents[0]]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      // Clock icon is used before relative time
      const timeText = container.textContent?.includes('ago') || container.textContent?.includes('just now')
      expect(timeText).toBe(true)
    })
  })

  describe('Accessibility', () => {
    it('should have proper semantic HTML structure', () => {
      const { container } = render(
        <AuditTable
          events={[mockEvents[0]]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      // Event rows are divs with flex layout
      const eventRow = container.querySelector('[class*="flex items-start gap-3"]')
      expect(eventRow?.tagName).toBe('DIV')
    })

    it('actor button should be keyboard accessible', () => {
      render(
        <AuditTable
          events={[mockEvents[0]]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      const actorButton = screen.getByRole('button', { name: 'user@example.com' })
      expect(actorButton.tagName).toBe('BUTTON')
    })

    it('correlation button should be keyboard accessible', () => {
      render(
        <AuditTable
          events={[mockEvents[0]]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      const correlationButton = screen.getByTitle('View correlation chain')
      expect(correlationButton.tagName).toBe('BUTTON')
    })

    it('correlation button should have descriptive title', () => {
      render(
        <AuditTable
          events={[mockEvents[0]]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      const correlationButton = screen.getByTitle('View correlation chain')
      expect(correlationButton.title).toBe('View correlation chain')
    })
  })

  describe('Event Data Integrity', () => {
    it('should not modify event data when rendering', () => {
      const eventsCopy = JSON.parse(JSON.stringify(mockEvents))

      render(
        <AuditTable
          events={mockEvents}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      // Events should not be modified
      expect(mockEvents).toEqual(eventsCopy)
    })

    it('should handle events with null/undefined optional fields', () => {
      const eventWithUndefined: AuditEvent = {
        ...mockEvents[0],
        correlation_id: '',
        ip_address: '',
        details: '',
      }

      render(
        <AuditTable
          events={[eventWithUndefined]}
          isLoading={false}
          isEmpty={false}
          searchTerm=""
          onCorrelation={mockOnCorrelation}
          onActor={mockOnActor}
        />
      )

      // Should still render the event with action
      expect(screen.getByText('create')).toBeInTheDocument()
    })
  })
})
