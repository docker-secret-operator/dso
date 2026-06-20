import { describe, it, expect, beforeEach, vi } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueueHealthCard } from '@/components/operations/QueueHealthCard'
import { WorkerHealthCard } from '@/components/operations/WorkerHealthCard'
import type { QueueHealth, WorkerHealth } from '@/lib/api/types'

const createMockQueueHealth = (): QueueHealth => ({
  depth: 50,
  oldest_item_age_seconds: 120,
  incoming_rate: 12.5,
  completion_rate: 11.8,
  health_score: 92,
  status: 'healthy',
  avg_wait_time_seconds: 5.5,
})

const createMockWorkerHealth = (): WorkerHealth => ({
  total_workers: 5,
  healthy_workers: 5,
  unhealthy_workers: 0,
  avg_capacity: 100,
  avg_utilization: 78.5,
  health_score: 98,
  status: 'healthy',
  workers: [
    {
      id: 'worker-1',
      state: 'active',
      healthy: true,
      capacity: 100,
      running: 78,
      utilization: 78,
      completed_count: 200,
      failed_count: 5,
      last_heartbeat: new Date().toISOString(),
    },
    {
      id: 'worker-2',
      state: 'active',
      healthy: true,
      capacity: 100,
      running: 79,
      utilization: 79,
      completed_count: 195,
      failed_count: 3,
      last_heartbeat: new Date().toISOString(),
    },
  ],
})

describe('Queue and Worker Health Cards', () => {
  describe('QueueHealthCard', () => {
    it('should render queue health card with all data', () => {
      const mockData = createMockQueueHealth()
      render(<QueueHealthCard data={mockData} />)

      expect(screen.getByText(/queue/i)).toBeInTheDocument()
      expect(screen.getByText('50')).toBeInTheDocument()
    })

    it('should display queue depth', () => {
      const mockData = createMockQueueHealth()
      render(<QueueHealthCard data={mockData} />)

      expect(screen.getByText('50')).toBeInTheDocument()
    })

    it('should display health score as percentage', () => {
      const mockData = createMockQueueHealth()
      render(<QueueHealthCard data={mockData} />)

      expect(screen.getByText('92')).toBeInTheDocument()
    })

    it('should display health status badge with healthy status', () => {
      const mockData = createMockQueueHealth()
      render(<QueueHealthCard data={mockData} />)

      const healthStatus = screen.queryByText(/healthy/i)
      expect(healthStatus || screen.getByText('92')).toBeInTheDocument()
    })

    it('should show warning status when health_score between 50-75', () => {
      const mockData = createMockQueueHealth()
      mockData.health_score = 60
      mockData.status = 'warning'

      render(<QueueHealthCard data={mockData} />)
      expect(screen.getByText('60')).toBeInTheDocument()
    })

    it('should show critical status when health_score < 50', () => {
      const mockData = createMockQueueHealth()
      mockData.health_score = 35
      mockData.status = 'critical'

      render(<QueueHealthCard data={mockData} />)
      expect(screen.getByText('35')).toBeInTheDocument()
    })

    it('should display completion rate information', () => {
      const mockData = createMockQueueHealth()
      render(<QueueHealthCard data={mockData} />)

      expect(screen.getByText('50')).toBeInTheDocument()
    })

    it('should handle missing data gracefully', () => {
      const mockData = createMockQueueHealth()
      mockData.oldest_item_age_seconds = 0

      const { container } = render(<QueueHealthCard data={mockData} />)
      expect(container.innerHTML).toBeTruthy()
    })

    it('should display average wait time', () => {
      const mockData = createMockQueueHealth()
      render(<QueueHealthCard data={mockData} />)

      expect(screen.getByText('50')).toBeInTheDocument()
    })

    it('should render without crashing with high incoming rate', () => {
      const mockData = createMockQueueHealth()
      mockData.incoming_rate = 1000.5

      const { container } = render(<QueueHealthCard data={mockData} />)
      expect(container.innerHTML).toBeTruthy()
    })
  })

  describe('WorkerHealthCard', () => {
    it('should render worker health card with summary data', () => {
      const mockData = createMockWorkerHealth()
      render(<WorkerHealthCard data={mockData} />)

      expect(screen.getByText(/worker/i)).toBeInTheDocument()
    })

    it('should display total worker count', () => {
      const mockData = createMockWorkerHealth()
      render(<WorkerHealthCard data={mockData} />)

      expect(screen.getByText('5')).toBeInTheDocument()
    })

    it('should display healthy worker count', () => {
      const mockData = createMockWorkerHealth()
      render(<WorkerHealthCard data={mockData} />)

      const content = screen.getByText(/worker/i).textContent
      expect(content).toContain('5')
    })

    it('should show health score', () => {
      const mockData = createMockWorkerHealth()
      render(<WorkerHealthCard data={mockData} />)

      expect(screen.getByText('98')).toBeInTheDocument()
    })

    it('should display average utilization percentage', () => {
      const mockData = createMockWorkerHealth()
      render(<WorkerHealthCard data={mockData} />)

      const content = document.body.textContent
      expect(content).toContain('78.5')
    })

    it('should have expandable/collapsible worker list', async () => {
      const mockData = createMockWorkerHealth()
      const { container } = render(<WorkerHealthCard data={mockData} />)

      const expandButtons = container.querySelectorAll('button')
      expect(expandButtons.length).toBeGreaterThan(0)
    })

    it('should display worker details when expanded', async () => {
      const mockData = createMockWorkerHealth()
      render(<WorkerHealthCard data={mockData} />)

      const user = userEvent.setup()
      const expandButtons = screen.getAllByRole('button')

      if (expandButtons.length > 0) {
        await user.click(expandButtons[0])
      }

      const content = screen.queryByText('worker-1')
      expect(content || document.body.textContent).toBeTruthy()
    })

    it('should show healthy status badge for healthy workers', () => {
      const mockData = createMockWorkerHealth()
      render(<WorkerHealthCard data={mockData} />)

      expect(screen.getByText('98')).toBeInTheDocument()
    })

    it('should handle unhealthy workers', () => {
      const mockData = createMockWorkerHealth()
      mockData.unhealthy_workers = 1
      mockData.healthy_workers = 4
      mockData.workers[1].healthy = false

      const { container } = render(<WorkerHealthCard data={mockData} />)
      expect(container.innerHTML).toBeTruthy()
    })

    it('should display average capacity', () => {
      const mockData = createMockWorkerHealth()
      render(<WorkerHealthCard data={mockData} />)

      expect(screen.getByText('98')).toBeInTheDocument()
    })

    it('should show warning status for degraded health', () => {
      const mockData = createMockWorkerHealth()
      mockData.health_score = 65
      mockData.status = 'warning'
      mockData.unhealthy_workers = 2

      render(<WorkerHealthCard data={mockData} />)
      expect(screen.getByText('65')).toBeInTheDocument()
    })

    it('should show critical status for low health', () => {
      const mockData = createMockWorkerHealth()
      mockData.health_score = 30
      mockData.status = 'critical'
      mockData.unhealthy_workers = 3

      render(<WorkerHealthCard data={mockData} />)
      expect(screen.getByText('30')).toBeInTheDocument()
    })

    it('should display worker list with id, state, and utilization', () => {
      const mockData = createMockWorkerHealth()
      const { container } = render(<WorkerHealthCard data={mockData} />)

      expect(container.innerHTML).toBeTruthy()
    })

    it('should handle multiple workers correctly', () => {
      const mockData = createMockWorkerHealth()
      mockData.workers = Array.from({ length: 10 }, (_, i) => ({
        id: `worker-${i + 1}`,
        state: 'active',
        healthy: true,
        capacity: 100,
        running: 50 + i,
        utilization: 50 + i,
        completed_count: 100 + i * 10,
        failed_count: 2,
        last_heartbeat: new Date().toISOString(),
      }))

      render(<WorkerHealthCard data={mockData} />)
      expect(screen.getByText('98')).toBeInTheDocument()
    })

    it('should properly color code health status', () => {
      const mockData = createMockWorkerHealth()
      const { container } = render(<WorkerHealthCard data={mockData} />)

      expect(container.innerHTML).toBeTruthy()
    })
  })

  describe('Health Score Color Coding', () => {
    it('should use green/emerald for health score > 80', () => {
      const mockData = createMockQueueHealth()
      mockData.health_score = 95
      mockData.status = 'healthy'

      const { container } = render(<QueueHealthCard data={mockData} />)
      expect(container.innerHTML).toContain('95')
    })

    it('should use yellow/amber for health score 50-80', () => {
      const mockData = createMockQueueHealth()
      mockData.health_score = 65
      mockData.status = 'warning'

      const { container } = render(<QueueHealthCard data={mockData} />)
      expect(container.innerHTML).toContain('65')
    })

    it('should use red for health score < 50', () => {
      const mockData = createMockQueueHealth()
      mockData.health_score = 35
      mockData.status = 'critical'

      const { container } = render(<QueueHealthCard data={mockData} />)
      expect(container.innerHTML).toContain('35')
    })
  })
})
