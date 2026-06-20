import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { CorrelationTimeline } from '@/components/audit/CorrelationTimeline'
import { CorrelationChainResponse, AuditEvent } from '@/lib/api/types'

describe('CorrelationTimeline Component', () => {
  const mockOnClose = vi.fn()

  const mockAuditEvents: AuditEvent[] = [
    {
      id: 'evt-1',
      timestamp: new Date('2026-06-19T12:00:00Z').toISOString(),
      actor: 'user@example.com',
      actor_id: 'user-123',
      actor_email: 'user@example.com',
      action: 'create_secret',
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
      timestamp: new Date('2026-06-19T12:00:05Z').toISOString(),
      actor: 'user@example.com',
      actor_id: 'user-123',
      actor_email: 'user@example.com',
      action: 'update_deployment',
      resource_type: 'deployment',
      resource: 'deployments',
      resource_id: 'deploy-456',
      status: 'success',
      severity: 'info',
      details: 'Deployment updated with new secret',
      correlation_id: 'corr-1234567890abcdef',
      execution_id: 'exec-123',
      ip_address: '192.168.1.100',
    },
    {
      id: 'evt-3',
      timestamp: new Date('2026-06-19T12:00:10Z').toISOString(),
      actor: 'system',
      actor_id: 'sys-789',
      actor_email: 'system@internal',
      action: 'restart_container',
      resource_type: 'container',
      resource: 'docker_containers',
      resource_id: 'cont-789',
      status: 'success',
      severity: 'info',
      details: 'Container restarted',
      correlation_id: 'corr-1234567890abcdef',
      execution_id: 'exec-123',
      ip_address: '192.168.1.100',
    },
  ]

  const mockCorrelationData: CorrelationChainResponse = {
    correlation_id: 'corr-1234567890abcdef',
    count: mockAuditEvents.length,
    events: mockAuditEvents,
    timestamp: new Date('2026-06-19T12:00:10Z').toISOString(),
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Modal Visibility', () => {
    it('should render modal when data is provided', () => {
      render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('Correlation Chain')).toBeInTheDocument()
    })

    it('should not render when data is null', () => {
      const { container } = render(
        <CorrelationTimeline
          data={null}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      // Component should return null when data is null, so container should be empty
      expect(container.innerHTML).toBe('')
    })

    it('should render modal with overlay', () => {
      const { container } = render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      // Check for fixed positioned overlay with black/50 background
      const overlay = container.querySelector('.fixed.inset-0.bg-black\\/50')
      expect(overlay).toBeInTheDocument()
    })
  })

  describe('Modal Content', () => {
    it('should display correlation chain title', () => {
      render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('Correlation Chain')).toBeInTheDocument()
    })

    it('should display event count', () => {
      render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('3 events')).toBeInTheDocument()
    })

    it('should display correlation ID in footer', () => {
      render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('corr-1234567890abcdef')).toBeInTheDocument()
    })

    it('should display all events in timeline', () => {
      render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('create_secret')).toBeInTheDocument()
      expect(screen.getByText('update_deployment')).toBeInTheDocument()
      expect(screen.getByText('restart_container')).toBeInTheDocument()
    })

    it('should display event details', () => {
      render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('Secret created successfully')).toBeInTheDocument()
      expect(screen.getByText('Deployment updated with new secret')).toBeInTheDocument()
      expect(screen.getByText('Container restarted')).toBeInTheDocument()
    })

    it('should display actor names', () => {
      render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      // Multiple instances of user@example.com can exist, so check with getAllByText
      const userActors = screen.getAllByText('user@example.com')
      expect(userActors.length).toBeGreaterThan(0)
      expect(screen.getByText('system')).toBeInTheDocument()
    })

    it('should display resource type badges', () => {
      render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('secret')).toBeInTheDocument()
      expect(screen.getByText('deployment')).toBeInTheDocument()
      expect(screen.getByText('container')).toBeInTheDocument()
    })
  })

  describe('Timeline Structure', () => {
    it('should display timeline dots for each event', () => {
      const { container } = render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      // Timeline dots are rendered as divs with rounded-full class
      const timelineDots = container.querySelectorAll('.rounded-full.border-2')
      expect(timelineDots.length).toBe(mockAuditEvents.length)
    })

    it('should render timeline connecting lines between events', () => {
      const { container } = render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      // Timeline lines are rendered as divs with bg-white/[0.1]
      const timelineLines = container.querySelectorAll('.bg-white\\/\\[0\\.1\\]')
      // Should have N-1 connecting lines for N events
      expect(timelineLines.length).toBeGreaterThanOrEqual(mockAuditEvents.length - 1)
    })

    it('should display timeline with correct node count', () => {
      render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      // Each event should display its action
      const actions = [
        'create_secret',
        'update_deployment',
        'restart_container',
      ]

      actions.forEach((action) => {
        expect(screen.getByText(action)).toBeInTheDocument()
      })
    })

    it('should display success status colors for all events', () => {
      const { container } = render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      // All events have success status, so dots should be green
      const successDots = container.querySelectorAll(
        '.bg-emerald-500.border-emerald-600'
      )
      expect(successDots.length).toBeGreaterThan(0)
    })
  })

  describe('Event Status Display', () => {
    it('should display success status with emerald color', () => {
      const { container } = render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      const successDots = container.querySelectorAll(
        '.bg-emerald-500.border-emerald-600'
      )
      expect(successDots.length).toBeGreaterThan(0)
    })

    it('should display failed status with red color', () => {
      const failedEvent: CorrelationChainResponse = {
        correlation_id: 'corr-test',
        count: 1,
        events: [
          {
            ...mockAuditEvents[0],
            status: 'failed',
          },
        ],
        timestamp: new Date().toISOString(),
      }

      const { container } = render(
        <CorrelationTimeline
          data={failedEvent}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      const failedDots = container.querySelectorAll(
        '.bg-red-500.border-red-600'
      )
      expect(failedDots.length).toBeGreaterThan(0)
    })

    it('should display default status with slate color', () => {
      const unknownStatusEvent: CorrelationChainResponse = {
        correlation_id: 'corr-test',
        count: 1,
        events: [
          {
            ...mockAuditEvents[0],
            status: 'unknown',
          },
        ],
        timestamp: new Date().toISOString(),
      }

      const { container } = render(
        <CorrelationTimeline
          data={unknownStatusEvent}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      const unknownDots = container.querySelectorAll(
        '.bg-slate-500.border-slate-600'
      )
      expect(unknownDots.length).toBeGreaterThan(0)
    })
  })

  describe('Modal Controls', () => {
    it('should have close button with X icon', () => {
      render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      const closeButton = screen.getByRole('button')
      expect(closeButton).toBeInTheDocument()
    })

    it('should call onClose when close button clicked', async () => {
      const user = userEvent.setup()
      render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      const closeButton = screen.getByRole('button')
      await user.click(closeButton)

      expect(mockOnClose).toHaveBeenCalledTimes(1)
    })

    it('should have scrollable content area', () => {
      const { container } = render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      const scrollContainer = container.querySelector('.overflow-y-auto')
      expect(scrollContainer).toBeInTheDocument()
    })
  })

  describe('Loading State', () => {
    it('should show loading state when isLoading=true', () => {
      render(
        <CorrelationTimeline
          data={undefined}
          isLoading={true}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('Correlation Chain')).toBeInTheDocument()
    })

    it('should display skeleton loaders during loading', () => {
      const { container } = render(
        <CorrelationTimeline
          data={undefined}
          isLoading={true}
          onClose={mockOnClose}
        />
      )

      // Skeleton components render with specific classes
      const skeletons = container.querySelectorAll('[class*="skeleton"]')
      expect(skeletons.length).toBeGreaterThan(0)
    })

    it('should have close button available during loading', async () => {
      const user = userEvent.setup()
      render(
        <CorrelationTimeline
          data={undefined}
          isLoading={true}
          onClose={mockOnClose}
        />
      )

      const closeButton = screen.getByRole('button')
      await user.click(closeButton)

      expect(mockOnClose).toHaveBeenCalled()
    })

    it('should not display event content while loading', () => {
      render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={true}
          onClose={mockOnClose}
        />
      )

      // Events should not be visible during loading
      expect(screen.queryByText('create_secret')).not.toBeInTheDocument()
      expect(screen.queryByText('update_deployment')).not.toBeInTheDocument()
    })
  })

  describe('Edge Cases', () => {
    it('should handle single event in correlation chain', () => {
      const singleEventData: CorrelationChainResponse = {
        correlation_id: 'corr-single',
        count: 1,
        events: [mockAuditEvents[0]],
        timestamp: new Date().toISOString(),
      }

      render(
        <CorrelationTimeline
          data={singleEventData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('1 events')).toBeInTheDocument()
      expect(screen.getByText('create_secret')).toBeInTheDocument()
    })

    it('should handle many events (10+) in correlation chain', () => {
      const manyEvents = Array.from({ length: 12 }, (_, i) => ({
        ...mockAuditEvents[0],
        id: `evt-${i}`,
        action: `action_${i}`,
        timestamp: new Date(
          Date.parse(mockAuditEvents[0].timestamp) + i * 5000
        ).toISOString(),
      }))

      const manyEventsData: CorrelationChainResponse = {
        correlation_id: 'corr-many',
        count: manyEvents.length,
        events: manyEvents,
        timestamp: new Date().toISOString(),
      }

      render(
        <CorrelationTimeline
          data={manyEventsData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('12 events')).toBeInTheDocument()
      expect(screen.getByText('action_0')).toBeInTheDocument()
      expect(screen.getByText('action_11')).toBeInTheDocument()
    })

    it('should handle events with long details text', () => {
      const longDetailsEvent: CorrelationChainResponse = {
        correlation_id: 'corr-long',
        count: 1,
        events: [
          {
            ...mockAuditEvents[0],
            details:
              'This is a very long details message that contains multiple lines and lots of information about what happened during this audit event',
          },
        ],
        timestamp: new Date().toISOString(),
      }

      render(
        <CorrelationTimeline
          data={longDetailsEvent}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      expect(
        screen.getByText(
          /This is a very long details message that contains multiple lines/
        )
      ).toBeInTheDocument()
    })

    it('should handle events with special characters in details', () => {
      const specialCharsEvent: CorrelationChainResponse = {
        correlation_id: 'corr-special',
        count: 1,
        events: [
          {
            ...mockAuditEvents[0],
            details: 'Special chars: <>&"\' and symbols: @#$%^&*()',
          },
        ],
        timestamp: new Date().toISOString(),
      }

      render(
        <CorrelationTimeline
          data={specialCharsEvent}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      expect(
        screen.getByText(/Special chars: <>&"' and symbols: @#\$%\^\&\*\(\)/)
      ).toBeInTheDocument()
    })

    it('should handle empty details text', () => {
      const noDetailsEvent: CorrelationChainResponse = {
        correlation_id: 'corr-empty-details',
        count: 1,
        events: [
          {
            ...mockAuditEvents[0],
            details: '',
          },
        ],
        timestamp: new Date().toISOString(),
      }

      render(
        <CorrelationTimeline
          data={noDetailsEvent}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('create_secret')).toBeInTheDocument()
    })
  })

  describe('Timestamp Display', () => {
    it('should display recent timestamps as relative time', () => {
      const recentEvent: CorrelationChainResponse = {
        correlation_id: 'corr-recent',
        count: 1,
        events: [
          {
            ...mockAuditEvents[0],
            timestamp: new Date(Date.now() - 30000).toISOString(), // 30 seconds ago
          },
        ],
        timestamp: new Date().toISOString(),
      }

      render(
        <CorrelationTimeline
          data={recentEvent}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      // Should show "just now" or relative time like "Xm ago"
      expect(
        screen.getByText(/just now|0m ago/)
      ).toBeInTheDocument()
    })

    it('should display Clock icon with timestamps', () => {
      const { container } = render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      // Clock icon should be present
      const clockIcons = container.querySelectorAll('svg')
      expect(clockIcons.length).toBeGreaterThan(0)
    })
  })

  describe('Modal Structure', () => {
    it('should have proper card styling', () => {
      const { container } = render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      const card = container.querySelector('.max-w-2xl')
      expect(card).toBeInTheDocument()
    })

    it('should have header with border', () => {
      const { container } = render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      const header = container.querySelector('.border-b')
      expect(header).toBeInTheDocument()
    })

    it('should have footer with border', () => {
      const { container } = render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      const footers = container.querySelectorAll('.border-t')
      expect(footers.length).toBeGreaterThan(0)
    })

    it('should have proper responsive padding', () => {
      const { container } = render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      // Check for padding classes
      const paddedElements = container.querySelectorAll('[class*="p-4"]')
      expect(paddedElements.length).toBeGreaterThan(0)
    })
  })

  describe('Accessibility', () => {
    it('should have proper heading structure', () => {
      render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      const heading = screen.getByText('Correlation Chain')
      expect(heading).toBeInTheDocument()
    })

    it('should have keyboard accessible close button', async () => {
      const user = userEvent.setup()
      render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      const closeButton = screen.getByRole('button')
      await user.tab()
      expect(closeButton).toHaveFocus()
    })

    it('should display correlation ID in semantic footer', () => {
      const { container } = render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      // Footer should contain correlation ID
      const correlationIdText = screen.getByText('corr-1234567890abcdef')
      expect(correlationIdText.closest('.border-t')).toBeInTheDocument()
    })
  })

  describe('Multiple Opens/Closes', () => {
    it('should handle rapid close calls', async () => {
      const user = userEvent.setup()
      const { rerender } = render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      const closeButton = screen.getByRole('button')
      await user.click(closeButton)

      // Rerender with new data should work
      rerender(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      expect(mockOnClose).toHaveBeenCalledTimes(1)
    })

    it('should clear previous state when reopening', () => {
      const { rerender } = render(
        <CorrelationTimeline
          data={mockCorrelationData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('create_secret')).toBeInTheDocument()

      // Close modal
      rerender(
        <CorrelationTimeline
          data={null}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      expect(screen.queryByText('create_secret')).not.toBeInTheDocument()

      // Reopen with different data
      const newData: CorrelationChainResponse = {
        correlation_id: 'corr-new',
        count: 1,
        events: [
          {
            ...mockAuditEvents[1],
            id: 'evt-new',
          },
        ],
        timestamp: new Date().toISOString(),
      }

      rerender(
        <CorrelationTimeline
          data={newData}
          isLoading={false}
          onClose={mockOnClose}
        />
      )

      expect(screen.getByText('update_deployment')).toBeInTheDocument()
    })
  })
})
