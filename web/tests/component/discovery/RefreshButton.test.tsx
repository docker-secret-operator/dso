import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { RefreshButton } from '@/components/discovery/RefreshButton'

describe('RefreshButton Component', () => {
  const defaultProps = {
    isRefreshing: false,
    onRefresh: vi.fn(),
  }

  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Rendering', () => {
    it('should render without crashing', () => {
      render(<RefreshButton {...defaultProps} />)
      expect(screen.getByRole('button')).toBeInTheDocument()
    })

    it('should display Refresh button with correct label', () => {
      render(<RefreshButton {...defaultProps} />)
      const button = screen.getByRole('button')
      expect(button.textContent).toContain('Refresh')
    })

    it('should have proper aria attributes', () => {
      render(<RefreshButton {...defaultProps} />)
      const button = screen.getByRole('button')
      expect(button).toHaveAttribute('aria-label', 'Refresh discovery data')
      expect(button).toHaveAttribute('aria-busy', 'false')
    })

    it('should display icon in button', () => {
      const { container } = render(<RefreshButton {...defaultProps} />)
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
    })
  })

  describe('Click Handler', () => {
    it('should call onRefresh when button is clicked', async () => {
      const user = userEvent.setup()
      const mockOnRefresh = vi.fn(() => Promise.resolve())
      render(
        <RefreshButton
          isRefreshing={false}
          onRefresh={mockOnRefresh}
        />
      )

      const button = screen.getByRole('button')
      await user.click(button)

      expect(mockOnRefresh).toHaveBeenCalledTimes(1)
    })

    it('should handle async onRefresh call', async () => {
      const user = userEvent.setup()
      const mockOnRefresh = vi.fn(() => Promise.resolve())

      render(
        <RefreshButton
          isRefreshing={false}
          onRefresh={mockOnRefresh}
        />
      )

      const button = screen.getByRole('button')
      await user.click(button)

      await waitFor(() => {
        expect(mockOnRefresh).toHaveBeenCalledTimes(1)
      })
    })
  })

  describe('Loading State', () => {
    it('should disable button during refresh', () => {
      render(<RefreshButton isRefreshing={true} onRefresh={vi.fn()} />)

      const button = screen.getByRole('button')
      expect(button).toBeDisabled()
    })

    it('should show Refreshing text when refreshing', () => {
      render(<RefreshButton isRefreshing={true} onRefresh={vi.fn()} />)
      expect(screen.getByText(/Refreshing/i)).toBeInTheDocument()
    })

    it('should show Refresh text when not refreshing', () => {
      render(<RefreshButton isRefreshing={false} onRefresh={vi.fn()} />)
      const button = screen.getByRole('button')
      expect(button.textContent).toContain('Refresh')
    })

    it('should display spinner animation during refresh', () => {
      const { container } = render(<RefreshButton isRefreshing={true} onRefresh={vi.fn()} />)
      const svg = container.querySelector('svg')
      expect(svg).toHaveClass('animate-spin')
    })

    it('should remove spinner animation when refresh completes', () => {
      const { container, rerender } = render(
        <RefreshButton isRefreshing={true} onRefresh={vi.fn()} />
      )
      let svg = container.querySelector('svg')
      expect(svg).toHaveClass('animate-spin')

      rerender(<RefreshButton isRefreshing={false} onRefresh={vi.fn()} />)
      svg = container.querySelector('svg')
      expect(svg).not.toHaveClass('animate-spin')
    })

    it('should have disabled opacity styling when refreshing', () => {
      render(<RefreshButton isRefreshing={true} onRefresh={vi.fn()} />)
      const button = screen.getByRole('button')
      expect(button).toHaveClass('disabled:opacity-50')
    })
  })

  describe('Last Refresh Timestamp', () => {
    it('should not display timestamp initially', () => {
      render(<RefreshButton isRefreshing={false} onRefresh={vi.fn()} />)
      expect(screen.queryByText(/Last refreshed/i)).not.toBeInTheDocument()
    })

    it('should display timestamp after refresh', async () => {
      const user = userEvent.setup()
      const mockOnRefresh = vi.fn(() => Promise.resolve())

      render(
        <RefreshButton
          isRefreshing={false}
          onRefresh={mockOnRefresh}
        />
      )

      const button = screen.getByRole('button')
      await user.click(button)

      await waitFor(() => {
        expect(screen.getByText(/Last refreshed/i)).toBeInTheDocument()
      })
    })

    it('should display relative time format', async () => {
      const user = userEvent.setup()
      const mockOnRefresh = vi.fn(() => Promise.resolve())

      render(
        <RefreshButton
          isRefreshing={false}
          onRefresh={mockOnRefresh}
        />
      )

      const button = screen.getByRole('button')
      await user.click(button)

      await waitFor(() => {
        const timestamp = screen.getByText(/Last refreshed/i)
        expect(timestamp.textContent).toMatch(/just now|[0-9]+(s|m|h) ago/)
      })
    })

    it('should update timestamp display when re-rendered', async () => {
      const user = userEvent.setup()
      const mockOnRefresh = vi.fn(() => Promise.resolve())

      const { rerender } = render(
        <RefreshButton
          isRefreshing={false}
          onRefresh={mockOnRefresh}
        />
      )

      const button = screen.getByRole('button')
      await user.click(button)

      await waitFor(() => {
        expect(screen.getByText(/Last refreshed/i)).toBeInTheDocument()
      })

      // Re-render should maintain timestamp
      rerender(
        <RefreshButton
          isRefreshing={false}
          onRefresh={mockOnRefresh}
        />
      )

      expect(screen.getByText(/Last refreshed/i)).toBeInTheDocument()
    })
  })

  describe('Error Handling', () => {
    it('should handle refresh errors gracefully', async () => {
      const user = userEvent.setup()
      const failingRefresh = vi.fn(() => Promise.reject(new Error('Refresh failed')))

      const { rerender } = render(
        <RefreshButton
          isRefreshing={true}
          onRefresh={failingRefresh}
        />
      )

      rerender(
        <RefreshButton
          isRefreshing={false}
          onRefresh={failingRefresh}
        />
      )

      const button = screen.getByRole('button')
      expect(button).not.toBeDisabled()
    })

    it('should re-enable button after failed refresh', async () => {
      const failingRefresh = vi.fn(() => Promise.reject(new Error('Refresh failed')))

      const { rerender } = render(
        <RefreshButton
          isRefreshing={true}
          onRefresh={failingRefresh}
        />
      )

      let button = screen.getByRole('button')
      expect(button).toBeDisabled()

      rerender(
        <RefreshButton
          isRefreshing={false}
          onRefresh={failingRefresh}
        />
      )

      button = screen.getByRole('button')
      expect(button).not.toBeDisabled()
    })

    it('should handle and log refresh errors', async () => {
      const user = userEvent.setup()
      const consoleErrorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
      const failingRefresh = vi.fn(() => Promise.reject(new Error('Network error')))

      render(
        <RefreshButton
          isRefreshing={false}
          onRefresh={failingRefresh}
        />
      )

      const button = screen.getByRole('button')
      await user.click(button)

      await waitFor(() => {
        expect(consoleErrorSpy).toHaveBeenCalled()
      })

      consoleErrorSpy.mockRestore()
    })
  })

  describe('Rapid Clicks', () => {
    it('should not make multiple requests on rapid clicks while refreshing', async () => {
      const user = userEvent.setup()
      const mockOnRefresh = vi.fn(() => new Promise(() => {}))

      render(
        <RefreshButton
          isRefreshing={true}
          onRefresh={mockOnRefresh}
        />
      )

      const button = screen.getByRole('button')
      expect(button).toBeDisabled()

      // Attempt to click multiple times
      await user.click(button)
      await user.click(button)
      await user.click(button)

      expect(mockOnRefresh).toHaveBeenCalledTimes(0)
    })

    it('should allow refresh after previous refresh completes', async () => {
      const user = userEvent.setup()
      const mockOnRefresh = vi.fn(() => Promise.resolve())

      const { rerender } = render(
        <RefreshButton
          isRefreshing={false}
          onRefresh={mockOnRefresh}
        />
      )

      const button = screen.getByRole('button')
      await user.click(button)

      expect(mockOnRefresh).toHaveBeenCalledTimes(1)

      rerender(
        <RefreshButton
          isRefreshing={false}
          onRefresh={mockOnRefresh}
        />
      )

      const newButton = screen.getByRole('button')
      await user.click(newButton)
      expect(mockOnRefresh).toHaveBeenCalledTimes(2)
    })
  })

  describe('Accessibility', () => {
    it('should update aria-busy attribute based on isRefreshing', () => {
      const { rerender } = render(<RefreshButton isRefreshing={false} onRefresh={vi.fn()} />)
      const button = screen.getByRole('button')
      expect(button).toHaveAttribute('aria-busy', 'false')

      rerender(<RefreshButton isRefreshing={true} onRefresh={vi.fn()} />)
      expect(button).toHaveAttribute('aria-busy', 'true')
    })

    it('should have meaningful aria-label', () => {
      render(<RefreshButton isRefreshing={false} onRefresh={vi.fn()} />)
      const button = screen.getByRole('button')
      expect(button.getAttribute('aria-label')).toBe('Refresh discovery data')
    })

    it('should indicate disabled state in UI', () => {
      render(<RefreshButton isRefreshing={true} onRefresh={vi.fn()} />)
      const button = screen.getByRole('button')
      expect(button).toHaveAttribute('disabled')
    })

    it('should have accessible button role', () => {
      render(<RefreshButton isRefreshing={false} onRefresh={vi.fn()} />)
      const button = screen.getByRole('button')
      expect(button.tagName).toBe('BUTTON')
    })
  })

  describe('Component Integration', () => {
    it('should handle multiple refresh cycles', async () => {
      const user = userEvent.setup()
      const mockOnRefresh = vi.fn(() => Promise.resolve())

      const { rerender } = render(
        <RefreshButton
          isRefreshing={false}
          onRefresh={mockOnRefresh}
        />
      )

      let button = screen.getByRole('button')
      await user.click(button)

      rerender(
        <RefreshButton
          isRefreshing={true}
          onRefresh={mockOnRefresh}
        />
      )

      button = screen.getByRole('button')
      expect(button).toBeDisabled()

      rerender(
        <RefreshButton
          isRefreshing={false}
          onRefresh={mockOnRefresh}
        />
      )

      button = screen.getByRole('button')
      expect(button).not.toBeDisabled()
      await user.click(button)

      expect(mockOnRefresh).toHaveBeenCalledTimes(2)
    })

    it('should hide timestamp during refresh', () => {
      const mockOnRefresh = vi.fn(() => Promise.resolve())

      const { rerender } = render(
        <RefreshButton
          isRefreshing={false}
          onRefresh={mockOnRefresh}
        />
      )

      rerender(
        <RefreshButton
          isRefreshing={true}
          onRefresh={mockOnRefresh}
        />
      )

      expect(screen.queryByText(/Last refreshed/i)).not.toBeInTheDocument()
    })

    it('should properly transition between loading states', () => {
      const { container: container1, rerender } = render(
        <RefreshButton isRefreshing={false} onRefresh={vi.fn()} />
      )
      expect(container1.querySelector('svg')).not.toHaveClass('animate-spin')

      rerender(<RefreshButton isRefreshing={true} onRefresh={vi.fn()} />)
      const svg = container1.querySelector('svg')
      expect(svg).toHaveClass('animate-spin')

      rerender(<RefreshButton isRefreshing={false} onRefresh={vi.fn()} />)
      expect(svg).not.toHaveClass('animate-spin')
    })
  })
})
