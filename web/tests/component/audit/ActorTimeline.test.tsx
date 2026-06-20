import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ActorTimeline } from '@/components/audit/ActorTimeline'
import { ActorTimelineResponse, AuditEvent } from '@/lib/api/types'

describe('ActorTimeline Component', () => {
  const mockOnClose = vi.fn()
  const mockOnPeriodChange = vi.fn()

  const mockAuditEvents: AuditEvent[] = [
    {
      id: 'evt-1',
      timestamp: new Date('2026-06-19T14:00:00Z').toISOString(),
      action: 'create',
      actor: 'user@example.com',
      actor_id: 'user-123',
      actor_email: 'user@example.com',
      resource_type: 'secret',
      resource: 'secrets',
      resource_id: 'sec-123',
      status: 'success',
      severity: 'info',
      details: 'Secret created successfully',
      correlation_id: 'corr-1234567890abcdef',
      execution_id: 'exec-123',
      ip_address: '192.168.1.100',
    },
    {
      id: 'evt-2',
      timestamp: new Date('2026-06-19T14:30:00Z').toISOString(),
      action: 'update',
      actor: 'user@example.com',
      actor_id: 'user-123',
      actor_email: 'user@example.com',
      resource_type: 'deployment',
      resource: 'deployments',
      resource_id: 'deploy-456',
      status: 'success',
      severity: 'info',
      details: 'Deployment updated with new configuration',
      correlation_id: 'corr-0987654321fedcba',
      execution_id: 'exec-456',
      ip_address: '192.168.1.100',
    },
    {
      id: 'evt-3',
      timestamp: new Date('2026-06-19T15:00:00Z').toISOString(),
      action: 'delete',
      actor: 'user@example.com',
      actor_id: 'user-123',
      actor_email: 'user@example.com',
      resource_type: 'container',
      resource: 'docker_containers',
      resource_id: 'cont-789',
      status: 'failed',
      severity: 'error',
      details: 'Container deletion failed due to running processes',
      correlation_id: 'corr-xyz9876543210abc',
      execution_id: 'exec-789',
      ip_address: '192.168.1.100',
    },
  ]

  const mockActorTimelineData: ActorTimelineResponse = {
    actor_id: 'user-123',
    actor_name: 'user@example.com',
    period: '24h',
    count: mockAuditEvents.length,
    events: mockAuditEvents,
    timestamp: new Date('2026-06-19T15:00:00Z').toISOString(),
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Modal Visibility', () => {
    it('should render modal when data is provided', () => {
      const { container } = render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const modal = container.querySelector('.fixed.inset-0')
      expect(modal).toBeInTheDocument()
    })

    it('should not render when data is null', () => {
      const { container } = render(
        <ActorTimeline
          data={null}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(container.innerHTML).toBe('')
    })

    it('should render modal with dark overlay', () => {
      const { container } = render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const overlay = container.querySelector('.fixed.inset-0.bg-black\\/50')
      expect(overlay).toBeInTheDocument()
    })

    it('should render card with proper styling', () => {
      const { container } = render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const card = container.querySelector('.w-full.max-w-2xl')
      expect(card).toBeInTheDocument()
    })
  })

  describe('Modal Title and Header', () => {
    it('should display actor name in title', () => {
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('user@example.com')).toBeInTheDocument()
    })

    it('should display event count summary', () => {
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('3 events in period')).toBeInTheDocument()
    })

    it('should display event count for different periods', () => {
      const dataWith5Events: ActorTimelineResponse = {
        ...mockActorTimelineData,
        count: 5,
      }

      render(
        <ActorTimeline
          data={dataWith5Events}
          isLoading={false}
          period="7d"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('5 events in period')).toBeInTheDocument()
    })

    it('should display correct actor name from data', () => {
      const customActorData: ActorTimelineResponse = {
        ...mockActorTimelineData,
        actor_name: 'admin@example.com',
      }

      render(
        <ActorTimeline
          data={customActorData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })
  })

  describe('Period Selection', () => {
    it('should display all 3 period options', () => {
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByRole('button', { name: '24h' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: '7d' })).toBeInTheDocument()
      expect(screen.getByRole('button', { name: '30d' })).toBeInTheDocument()
    })

    it('should highlight selected period (24h)', () => {
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const period24h = screen.getByRole('button', { name: '24h' })
      expect(period24h).toHaveClass('bg-indigo-600')
    })

    it('should highlight selected period (7d)', () => {
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="7d"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const period7d = screen.getByRole('button', { name: '7d' })
      expect(period7d).toHaveClass('bg-indigo-600')
    })

    it('should highlight selected period (30d)', () => {
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="30d"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const period30d = screen.getByRole('button', { name: '30d' })
      expect(period30d).toHaveClass('bg-indigo-600')
    })

    it('should call onPeriodChange when 24h period clicked', async () => {
      const user = userEvent.setup()
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="7d"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const period24h = screen.getByRole('button', { name: '24h' })
      await user.click(period24h)

      expect(mockOnPeriodChange).toHaveBeenCalledWith('24h')
    })

    it('should call onPeriodChange when 7d period clicked', async () => {
      const user = userEvent.setup()
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const period7d = screen.getByRole('button', { name: '7d' })
      await user.click(period7d)

      expect(mockOnPeriodChange).toHaveBeenCalledWith('7d')
    })

    it('should call onPeriodChange when 30d period clicked', async () => {
      const user = userEvent.setup()
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const period30d = screen.getByRole('button', { name: '30d' })
      await user.click(period30d)

      expect(mockOnPeriodChange).toHaveBeenCalledWith('30d')
    })

    it('should handle multiple period changes', async () => {
      const user = userEvent.setup()
      const { rerender } = render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const period7d = screen.getByRole('button', { name: '7d' })
      await user.click(period7d)
      expect(mockOnPeriodChange).toHaveBeenCalledWith('7d')

      // Re-render with new period
      rerender(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="7d"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const period30d = screen.getByRole('button', { name: '30d' })
      await user.click(period30d)
      expect(mockOnPeriodChange).toHaveBeenCalledWith('30d')

      expect(mockOnPeriodChange).toHaveBeenCalledTimes(2)
    })
  })

  describe('Timeline Display', () => {
    it('should display all events', () => {
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('create')).toBeInTheDocument()
      expect(screen.getByText('update')).toBeInTheDocument()
      expect(screen.getByText('delete')).toBeInTheDocument()
    })

    it('should display event count in all events', () => {
      const dataWith5Events: ActorTimelineResponse = {
        ...mockActorTimelineData,
        count: 5,
        events: Array.from({ length: 5 }, (_, i) => ({
          ...mockAuditEvents[0],
          id: `evt-${i}`,
          timestamp: new Date(Date.now() - i * 60000).toISOString(),
        })),
      }

      render(
        <ActorTimeline
          data={dataWith5Events}
          isLoading={false}
          period="7d"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('5 events in period')).toBeInTheDocument()
    })

    it('should display events in chronological order', () => {
      const { container } = render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      // Get all event containers in order
      const eventContainers = container.querySelectorAll('.relative.pl-6.pb-4')
      expect(eventContainers.length).toBe(3)

      // Check order by getting the action text from each container
      const firstEventText = eventContainers[0].textContent
      const secondEventText = eventContainers[1].textContent
      const thirdEventText = eventContainers[2].textContent

      expect(firstEventText).toContain('create')
      expect(secondEventText).toContain('update')
      expect(thirdEventText).toContain('delete')
    })

    it('should display event action types', () => {
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('create')).toBeInTheDocument()
      expect(screen.getByText('update')).toBeInTheDocument()
      expect(screen.getByText('delete')).toBeInTheDocument()
    })

    it('should display resource type badges', () => {
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('secret')).toBeInTheDocument()
      expect(screen.getByText('deployment')).toBeInTheDocument()
      expect(screen.getByText('container')).toBeInTheDocument()
    })

    it('should display event details', () => {
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('Secret created successfully')).toBeInTheDocument()
      expect(screen.getByText('Deployment updated with new configuration')).toBeInTheDocument()
      expect(screen.getByText('Container deletion failed due to running processes')).toBeInTheDocument()
    })

    it('should display resource information', () => {
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      // Resource info should be displayed as "resource/resource_id"
      expect(screen.getByText(/secrets\/sec-/)).toBeInTheDocument()
      expect(screen.getByText(/deployments\/deploy-/)).toBeInTheDocument()
      expect(screen.getByText(/docker_containers\/cont-/)).toBeInTheDocument()
    })
  })

  describe('Status Badges', () => {
    it('should display success status', () => {
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const successIndicators = screen.getAllByText(/success/)
      expect(successIndicators.length).toBeGreaterThan(0)
    })

    it('should display failed status', () => {
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const failureIndicators = screen.queryAllByText(/failed/)
      expect(failureIndicators.length).toBeGreaterThan(0)
    })

    it('should show timeline dots with different colors for different statuses', () => {
      const { container } = render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      // Check for timeline dots with different bg colors
      const timelineDots = container.querySelectorAll('[class*="rounded-full"][class*="border-2"]')
      expect(timelineDots.length).toBeGreaterThanOrEqual(mockAuditEvents.length)
    })

    it('should render success timeline dot with emerald color', () => {
      const { container } = render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const emeraldDot = container.querySelector('.bg-emerald-500')
      expect(emeraldDot).toBeInTheDocument()
    })

    it('should render failed timeline dot with red color', () => {
      const { container } = render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const redDot = container.querySelector('.bg-red-500')
      expect(redDot).toBeInTheDocument()
    })
  })

  describe('Modal Controls', () => {
    it('should render close button', () => {
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const closeButtons = screen.getAllByRole('button')
      const closeButton = closeButtons.find(btn => btn.classList.toString().includes('hover:text-slate-300'))
      expect(closeButton).toBeInTheDocument()
    })

    it('should close modal when close button clicked', async () => {
      const user = userEvent.setup()
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      // Get the close button (X icon button in header)
      const closeButtons = screen.getAllByRole('button')
      const closeButton = closeButtons.find(btn => btn.classList.toString().includes('hover:text-slate-300'))

      if (closeButton) {
        await user.click(closeButton)
        expect(mockOnClose).toHaveBeenCalled()
      }
    })

    it('should have proper close button styling', () => {
      const { container } = render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const closeButton = container.querySelector('button.text-slate-500')
      expect(closeButton).toBeInTheDocument()
    })
  })

  describe('Loading State', () => {
    it('should show loading modal when isLoading is true', () => {
      render(
        <ActorTimeline
          data={null}
          isLoading={true}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('Actor Timeline')).toBeInTheDocument()
    })

    it('should show skeletons when loading', () => {
      const { container } = render(
        <ActorTimeline
          data={null}
          isLoading={true}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      // Skeleton component renders elements with specific styles
      const skeletons = container.querySelectorAll('.skeleton')
      expect(skeletons.length).toBeGreaterThan(0)
    })

    it('should display correct number of skeleton items', () => {
      const { container } = render(
        <ActorTimeline
          data={null}
          isLoading={true}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const skeletons = container.querySelectorAll('.skeleton')
      // Skeleton with count={4}
      expect(skeletons.length).toBe(4)
    })

    it('should not show event data while loading', () => {
      render(
        <ActorTimeline
          data={null}
          isLoading={true}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.queryByText('create')).not.toBeInTheDocument()
      expect(screen.queryByText('update')).not.toBeInTheDocument()
      expect(screen.queryByText('delete')).not.toBeInTheDocument()
    })

    it('should still show close button while loading', () => {
      render(
        <ActorTimeline
          data={null}
          isLoading={true}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const closeButtons = screen.getAllByRole('button')
      expect(closeButtons.length).toBeGreaterThan(0)
    })

    it('should allow closing modal while loading', async () => {
      const user = userEvent.setup()
      render(
        <ActorTimeline
          data={null}
          isLoading={true}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const closeButtons = screen.getAllByRole('button')
      const closeButton = closeButtons.find(btn => btn.classList.toString().includes('hover:text-slate-300'))

      if (closeButton) {
        await user.click(closeButton)
        expect(mockOnClose).toHaveBeenCalled()
      }
    })
  })

  describe('Empty State', () => {
    it('should handle data with no events', () => {
      const emptyData: ActorTimelineResponse = {
        ...mockActorTimelineData,
        count: 0,
        events: [],
      }

      render(
        <ActorTimeline
          data={emptyData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      // Should display actor name and 0 events
      expect(screen.getByText('user@example.com')).toBeInTheDocument()
      expect(screen.getByText('0 events in period')).toBeInTheDocument()
    })

    it('should render empty event list without crashing', () => {
      const emptyData: ActorTimelineResponse = {
        ...mockActorTimelineData,
        count: 0,
        events: [],
      }

      const { container } = render(
        <ActorTimeline
          data={emptyData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(container).toBeInTheDocument()
    })

    it('should allow period change even with no events', async () => {
      const user = userEvent.setup()
      const emptyData: ActorTimelineResponse = {
        ...mockActorTimelineData,
        count: 0,
        events: [],
      }

      render(
        <ActorTimeline
          data={emptyData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const period7d = screen.getByRole('button', { name: '7d' })
      await user.click(period7d)

      expect(mockOnPeriodChange).toHaveBeenCalledWith('7d')
    })
  })

  describe('Timeline Structure', () => {
    it('should render timeline lines between events', () => {
      const { container } = render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      // Timeline lines are divs with specific class
      const timelineLines = container.querySelectorAll('[class*="bottom-0"][class*="w-0.5"][class*="bg-white"]')
      expect(timelineLines.length).toBeGreaterThan(0)
    })

    it('should render timeline dots for each event', () => {
      const { container } = render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      // Timeline dots should be present
      const dots = container.querySelectorAll('[class*="rounded-full"]')
      expect(dots.length).toBeGreaterThanOrEqual(mockAuditEvents.length)
    })

    it('should have proper spacing between events', () => {
      const { container } = render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      // Check for pb-4 class on event containers
      const eventContainers = container.querySelectorAll('[class*="pb-4"]')
      expect(eventContainers.length).toBeGreaterThan(0)
    })
  })

  describe('Relative Time Display', () => {
    it('should display "just now" for very recent timestamps', () => {
      const recentEvent: AuditEvent = {
        ...mockAuditEvents[0],
        timestamp: new Date().toISOString(),
      }

      const recentData: ActorTimelineResponse = {
        ...mockActorTimelineData,
        events: [recentEvent],
        count: 1,
      }

      render(
        <ActorTimeline
          data={recentData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('just now')).toBeInTheDocument()
    })

    it('should display minutes ago for older timestamps', () => {
      const minutesAgoEvent: AuditEvent = {
        ...mockAuditEvents[0],
        timestamp: new Date(Date.now() - 5 * 60000).toISOString(), // 5 minutes ago
      }

      const minutesAgoData: ActorTimelineResponse = {
        ...mockActorTimelineData,
        events: [minutesAgoEvent],
        count: 1,
      }

      render(
        <ActorTimeline
          data={minutesAgoData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText(/5m ago/)).toBeInTheDocument()
    })

    it('should display hours ago for older timestamps', () => {
      const hoursAgoEvent: AuditEvent = {
        ...mockAuditEvents[0],
        timestamp: new Date(Date.now() - 2 * 3600000).toISOString(), // 2 hours ago
      }

      const hoursAgoData: ActorTimelineResponse = {
        ...mockActorTimelineData,
        events: [hoursAgoEvent],
        count: 1,
      }

      render(
        <ActorTimeline
          data={hoursAgoData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText(/2h ago/)).toBeInTheDocument()
    })
  })

  describe('Edge Cases', () => {
    it('should handle single event', () => {
      const singleEventData: ActorTimelineResponse = {
        ...mockActorTimelineData,
        events: [mockAuditEvents[0]],
        count: 1,
      }

      render(
        <ActorTimeline
          data={singleEventData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('1 events in period')).toBeInTheDocument()
      expect(screen.getByText('create')).toBeInTheDocument()
    })

    it('should handle large number of events', () => {
      const manyEventsData: ActorTimelineResponse = {
        ...mockActorTimelineData,
        count: 50,
        events: Array.from({ length: 50 }, (_, i) => ({
          ...mockAuditEvents[0],
          id: `evt-${i}`,
          timestamp: new Date(Date.now() - i * 60000).toISOString(),
          action: ['create', 'read', 'update', 'delete'][i % 4],
        })),
      }

      render(
        <ActorTimeline
          data={manyEventsData}
          isLoading={false}
          period="30d"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('50 events in period')).toBeInTheDocument()
    })

    it('should handle events with empty details', () => {
      const eventWithoutDetails: AuditEvent = {
        ...mockAuditEvents[0],
        details: '',
      }

      const dataWithoutDetails: ActorTimelineResponse = {
        ...mockActorTimelineData,
        events: [eventWithoutDetails],
        count: 1,
      }

      render(
        <ActorTimeline
          data={dataWithoutDetails}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('create')).toBeInTheDocument()
    })

    it('should handle events with very long details', () => {
      const eventWithLongDetails: AuditEvent = {
        ...mockAuditEvents[0],
        details: 'A'.repeat(500),
      }

      const dataWithLongDetails: ActorTimelineResponse = {
        ...mockActorTimelineData,
        events: [eventWithLongDetails],
        count: 1,
      }

      render(
        <ActorTimeline
          data={dataWithLongDetails}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText(/A+/)).toBeInTheDocument()
    })

    it('should handle actor names with special characters', () => {
      const specialCharData: ActorTimelineResponse = {
        ...mockActorTimelineData,
        actor_name: 'user+tag@example.co.uk',
      }

      render(
        <ActorTimeline
          data={specialCharData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('user+tag@example.co.uk')).toBeInTheDocument()
    })

    it('should handle events with unknown status', () => {
      const eventWithUnknownStatus: AuditEvent = {
        ...mockAuditEvents[0],
        status: 'unknown',
      }

      const dataWithUnknownStatus: ActorTimelineResponse = {
        ...mockActorTimelineData,
        events: [eventWithUnknownStatus],
        count: 1,
      }

      const { container } = render(
        <ActorTimeline
          data={dataWithUnknownStatus}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      // Should render with default status color (slate)
      const slateGrayDot = container.querySelector('.bg-slate-500')
      expect(slateGrayDot).toBeInTheDocument()
    })
  })

  describe('Modal Scrolling', () => {
    it('should have scrollable content area', () => {
      const { container } = render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const scrollArea = container.querySelector('[class*="overflow-y-auto"]')
      expect(scrollArea).toBeInTheDocument()
    })

    it('should have max height constraint on modal', () => {
      const { container } = render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const card = container.querySelector('[class*="max-h-[80vh]"]')
      expect(card).toBeInTheDocument()
    })
  })

  describe('Accessibility', () => {
    it('should render with proper button roles for period selection', () => {
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      const period24h = screen.getByRole('button', { name: '24h' })
      const period7d = screen.getByRole('button', { name: '7d' })
      const period30d = screen.getByRole('button', { name: '30d' })

      expect(period24h.tagName).toBe('BUTTON')
      expect(period7d.tagName).toBe('BUTTON')
      expect(period30d.tagName).toBe('BUTTON')
    })

    it('should have clock icon with time information', () => {
      const { container } = render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      // Clock icon should be present in the component
      const timeElements = screen.getAllByText(/ago|just now/)
      expect(timeElements.length).toBeGreaterThan(0)
    })
  })

  describe('Data Integrity', () => {
    it('should not modify data during render', () => {
      const dataCopy = JSON.parse(JSON.stringify(mockActorTimelineData))

      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(mockActorTimelineData).toEqual(dataCopy)
    })

    it('should display all event properties correctly', () => {
      render(
        <ActorTimeline
          data={mockActorTimelineData}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      // Verify first event properties are rendered
      expect(screen.getByText('create')).toBeInTheDocument()
      expect(screen.getByText('secret')).toBeInTheDocument()
      expect(screen.getByText('Secret created successfully')).toBeInTheDocument()
    })
  })

  describe('Period Change with Data Updates', () => {
    it('should update events when period changes', () => {
      const data24h: ActorTimelineResponse = {
        ...mockActorTimelineData,
        period: '24h',
        count: 3,
        events: mockAuditEvents,
      }

      const data7d: ActorTimelineResponse = {
        ...mockActorTimelineData,
        period: '7d',
        count: 10,
        events: [
          ...mockAuditEvents,
          ...Array.from({ length: 7 }, (_, i) => ({
            ...mockAuditEvents[0],
            id: `evt-extra-${i}`,
            timestamp: new Date(Date.now() - (i + 1) * 24 * 3600000).toISOString(),
          })),
        ],
      }

      const { rerender } = render(
        <ActorTimeline
          data={data24h}
          isLoading={false}
          period="24h"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('3 events in period')).toBeInTheDocument()

      // Re-render with 7d data
      rerender(
        <ActorTimeline
          data={data7d}
          isLoading={false}
          period="7d"
          onPeriodChange={mockOnPeriodChange}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('10 events in period')).toBeInTheDocument()
    })
  })
})
