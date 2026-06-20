import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { EmptyState, EmptyStateType } from '@/components/discovery/EmptyState'

describe('EmptyState Component', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  describe('Rendering', () => {
    it('should render without crashing for no-containers type', () => {
      const { container } = render(<EmptyState type="no-containers" />)
      expect(container).toBeInTheDocument()
    })

    it('should render without crashing for no-mappings type', () => {
      const { container } = render(<EmptyState type="no-mappings" />)
      expect(container).toBeInTheDocument()
    })

    it('should render without crashing for filter-mismatch type', () => {
      const { container } = render(<EmptyState type="filter-mismatch" />)
      expect(container).toBeInTheDocument()
    })
  })

  describe('Message Display', () => {
    it('should display correct title for no-containers', () => {
      render(<EmptyState type="no-containers" />)
      expect(screen.getByText('No containers discovered')).toBeInTheDocument()
    })

    it('should display correct title for no-mappings', () => {
      render(<EmptyState type="no-mappings" />)
      expect(screen.getByText('No secret mappings detected')).toBeInTheDocument()
    })

    it('should display correct title for filter-mismatch', () => {
      render(<EmptyState type="filter-mismatch" />)
      expect(screen.getByText('No containers match current filters')).toBeInTheDocument()
    })

    it('should display correct description for no-containers', () => {
      render(<EmptyState type="no-containers" />)
      expect(screen.getByText('Try refreshing or check your environment.')).toBeInTheDocument()
    })

    it('should display correct description for no-mappings', () => {
      render(<EmptyState type="no-mappings" />)
      expect(
        screen.getByText('This is perfectly valid — your containers may already be configured.')
      ).toBeInTheDocument()
    })

    it('should display correct description for filter-mismatch', () => {
      render(<EmptyState type="filter-mismatch" />)
      expect(screen.getByText('Try adjusting your search term or filters.')).toBeInTheDocument()
    })
  })

  describe('Icon Display', () => {
    it('should render icon for no-containers', () => {
      const { container } = render(<EmptyState type="no-containers" />)
      // Database icon should be rendered
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
      expect(svg).toHaveClass('w-12', 'h-12')
    })

    it('should render icon for no-mappings', () => {
      const { container } = render(<EmptyState type="no-mappings" />)
      // AlertCircle icon should be rendered
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
      expect(svg).toHaveClass('w-12', 'h-12')
    })

    it('should render icon for filter-mismatch', () => {
      const { container } = render(<EmptyState type="filter-mismatch" />)
      // Search icon should be rendered
      const svg = container.querySelector('svg')
      expect(svg).toBeInTheDocument()
      expect(svg).toHaveClass('w-12', 'h-12')
    })

    it('should apply slate-500 color to icon', () => {
      const { container } = render(<EmptyState type="no-containers" />)
      const svg = container.querySelector('svg')
      expect(svg).toHaveClass('text-slate-500')
    })

    it('should apply correct size classes to icon', () => {
      const { container } = render(<EmptyState type="no-containers" />)
      const svg = container.querySelector('svg')
      expect(svg).toHaveClass('w-12', 'h-12', 'mb-3')
    })
  })

  describe('OnRetry Callback', () => {
    it('should call onRetry when button is clicked', async () => {
      const user = userEvent.setup()
      const mockOnRetry = vi.fn()
      render(<EmptyState type="no-containers" onRetry={mockOnRetry} />)

      const button = screen.getByRole('button', { name: /try again/i })
      await user.click(button)

      expect(mockOnRetry).toHaveBeenCalledTimes(1)
    })

    it('should call onRetry multiple times on multiple clicks', async () => {
      const user = userEvent.setup()
      const mockOnRetry = vi.fn()
      render(<EmptyState type="no-containers" onRetry={mockOnRetry} />)

      const button = screen.getByRole('button', { name: /try again/i })
      await user.click(button)
      await user.click(button)
      await user.click(button)

      expect(mockOnRetry).toHaveBeenCalledTimes(3)
    })

    it('should invoke onRetry on button click', async () => {
      const user = userEvent.setup()
      const mockOnRetry = vi.fn()
      render(<EmptyState type="no-containers" onRetry={mockOnRetry} />)

      const button = screen.getByRole('button', { name: /try again/i })
      await user.click(button)

      expect(mockOnRetry).toHaveBeenCalled()
    })
  })

  describe('No Retry Button', () => {
    it('should not display retry button when onRetry is undefined', () => {
      render(<EmptyState type="no-containers" />)
      const button = screen.queryByRole('button', { name: /try again/i })
      expect(button).not.toBeInTheDocument()
    })

    it('should not display retry button for no-mappings without onRetry', () => {
      render(<EmptyState type="no-mappings" />)
      expect(screen.queryByRole('button')).not.toBeInTheDocument()
    })

    it('should not display retry button for filter-mismatch without onRetry', () => {
      render(<EmptyState type="filter-mismatch" />)
      expect(screen.queryByRole('button')).not.toBeInTheDocument()
    })

    it('should display retry button for no-containers with onRetry', () => {
      const mockOnRetry = vi.fn()
      render(<EmptyState type="no-containers" onRetry={mockOnRetry} />)
      const button = screen.getByRole('button', { name: /try again/i })
      expect(button).toBeInTheDocument()
    })

    it('should display retry button for no-mappings with onRetry', () => {
      const mockOnRetry = vi.fn()
      render(<EmptyState type="no-mappings" onRetry={mockOnRetry} />)
      const button = screen.getByRole('button', { name: /try again/i })
      expect(button).toBeInTheDocument()
    })

    it('should display retry button for filter-mismatch with onRetry', () => {
      const mockOnRetry = vi.fn()
      render(<EmptyState type="filter-mismatch" onRetry={mockOnRetry} />)
      const button = screen.getByRole('button', { name: /try again/i })
      expect(button).toBeInTheDocument()
    })
  })

  describe('Message Content Accuracy', () => {
    it('should have exact title for no-containers type', () => {
      render(<EmptyState type="no-containers" />)
      const heading = screen.getByRole('heading', { level: 3 })
      expect(heading).toHaveTextContent('No containers discovered')
    })

    it('should have exact description for no-containers type', () => {
      render(<EmptyState type="no-containers" />)
      expect(screen.getByText('Try refreshing or check your environment.')).toBeInTheDocument()
    })

    it('should have exact title for no-mappings type', () => {
      render(<EmptyState type="no-mappings" />)
      const heading = screen.getByRole('heading', { level: 3 })
      expect(heading).toHaveTextContent('No secret mappings detected')
    })

    it('should have exact description for no-mappings type', () => {
      render(<EmptyState type="no-mappings" />)
      expect(
        screen.getByText('This is perfectly valid — your containers may already be configured.')
      ).toBeInTheDocument()
    })

    it('should have exact title for filter-mismatch type', () => {
      render(<EmptyState type="filter-mismatch" />)
      const heading = screen.getByRole('heading', { level: 3 })
      expect(heading).toHaveTextContent('No containers match current filters')
    })

    it('should have exact description for filter-mismatch type', () => {
      render(<EmptyState type="filter-mismatch" />)
      expect(screen.getByText('Try adjusting your search term or filters.')).toBeInTheDocument()
    })
  })

  describe('Styling and Layout', () => {
    it('should apply flex container with centered layout', () => {
      const { container } = render(<EmptyState type="no-containers" />)
      const wrapper = container.querySelector('div')
      expect(wrapper).toHaveClass('flex', 'flex-col', 'items-center', 'justify-center')
    })

    it('should apply padding to container', () => {
      const { container } = render(<EmptyState type="no-containers" />)
      const wrapper = container.querySelector('div')
      expect(wrapper).toHaveClass('py-12', 'px-4')
    })

    it('should apply title styling classes', () => {
      render(<EmptyState type="no-containers" />)
      const heading = screen.getByRole('heading', { level: 3 })
      expect(heading).toHaveClass('text-lg', 'font-semibold', 'text-slate-200', 'mb-1')
    })

    it('should apply description styling classes', () => {
      const { container } = render(<EmptyState type="no-containers" />)
      const description = container.querySelector('p')
      expect(description).toHaveClass('text-sm', 'text-slate-400', 'mb-4', 'text-center', 'max-w-md')
    })

    it('should apply button styling classes', () => {
      const mockOnRetry = vi.fn()
      render(<EmptyState type="no-containers" onRetry={mockOnRetry} />)
      const button = screen.getByRole('button', { name: /try again/i })
      expect(button).toHaveClass('text-sm', 'text-indigo-400', 'underline')
    })

    it('should apply hover effect to button', () => {
      const mockOnRetry = vi.fn()
      render(<EmptyState type="no-containers" onRetry={mockOnRetry} />)
      const button = screen.getByRole('button', { name: /try again/i })
      expect(button).toHaveClass('hover:text-indigo-300')
    })
  })

  describe('Centered Layout', () => {
    it('should center content vertically', () => {
      const { container } = render(<EmptyState type="no-containers" />)
      const wrapper = container.querySelector('div')
      expect(wrapper).toHaveClass('justify-center')
    })

    it('should center content horizontally', () => {
      const { container } = render(<EmptyState type="no-containers" />)
      const wrapper = container.querySelector('div')
      expect(wrapper).toHaveClass('items-center')
    })

    it('should use flex layout for vertical stacking', () => {
      const { container } = render(<EmptyState type="no-containers" />)
      const wrapper = container.querySelector('div')
      expect(wrapper).toHaveClass('flex', 'flex-col')
    })

    it('should description be centered in text', () => {
      const { container } = render(<EmptyState type="no-containers" />)
      const description = container.querySelector('p')
      expect(description).toHaveClass('text-center')
    })
  })

  describe('Optional Retry Button Visibility', () => {
    it('should show button only when onRetry is provided', () => {
      const { rerender } = render(<EmptyState type="no-containers" />)
      expect(screen.queryByRole('button')).not.toBeInTheDocument()

      const mockOnRetry = vi.fn()
      rerender(<EmptyState type="no-containers" onRetry={mockOnRetry} />)
      expect(screen.getByRole('button')).toBeInTheDocument()
    })

    it('should hide button when onRetry is removed', () => {
      const mockOnRetry = vi.fn()
      const { rerender } = render(<EmptyState type="no-containers" onRetry={mockOnRetry} />)
      expect(screen.getByRole('button')).toBeInTheDocument()

      rerender(<EmptyState type="no-containers" />)
      expect(screen.queryByRole('button')).not.toBeInTheDocument()
    })

    it('should display correct button text when present', () => {
      const mockOnRetry = vi.fn()
      render(<EmptyState type="no-containers" onRetry={mockOnRetry} />)
      const button = screen.getByRole('button')
      expect(button).toHaveTextContent('Try again')
    })

    it('should render all three message elements regardless of retry', () => {
      render(<EmptyState type="no-containers" />)
      // Icon, title, description should all be present
      const icon = document.querySelector('svg')
      const title = screen.getByRole('heading', { level: 3 })
      const description = screen.getByText('Try refreshing or check your environment.')
      expect(icon).toBeInTheDocument()
      expect(title).toBeInTheDocument()
      expect(description).toBeInTheDocument()
    })
  })

  describe('Type Prop Variation', () => {
    const types: EmptyStateType[] = ['no-containers', 'no-mappings', 'filter-mismatch']

    types.forEach(type => {
      it(`should render correctly for type: ${type}`, () => {
        const { container } = render(<EmptyState type={type} />)
        expect(container.querySelector('svg')).toBeInTheDocument()
      })

      it(`should have icon with correct styling for type: ${type}`, () => {
        const { container } = render(<EmptyState type={type} />)
        const icon = container.querySelector('svg')
        expect(icon).toHaveClass('w-12', 'h-12', 'text-slate-500')
      })

      it(`should have title with correct styling for type: ${type}`, () => {
        render(<EmptyState type={type} />)
        const title = screen.getByRole('heading', { level: 3 })
        expect(title).toHaveClass('text-lg', 'font-semibold', 'text-slate-200')
      })

      it(`should have description with correct styling for type: ${type}`, () => {
        const { container } = render(<EmptyState type={type} />)
        const description = container.querySelector('p')
        expect(description).toHaveClass('text-sm', 'text-slate-400')
      })
    })
  })

  describe('Integration Tests', () => {
    it('should render complete empty state for no-containers without retry', () => {
      const { container } = render(<EmptyState type="no-containers" />)
      // Check all elements are present
      expect(container.querySelector('svg')).toBeInTheDocument()
      expect(screen.getByText('No containers discovered')).toBeInTheDocument()
      expect(screen.getByText('Try refreshing or check your environment.')).toBeInTheDocument()
      expect(screen.queryByRole('button')).not.toBeInTheDocument()
    })

    it('should render complete empty state for no-containers with retry', async () => {
      const user = userEvent.setup()
      const mockOnRetry = vi.fn()
      const { container } = render(<EmptyState type="no-containers" onRetry={mockOnRetry} />)

      // Check all elements are present
      expect(container.querySelector('svg')).toBeInTheDocument()
      expect(screen.getByText('No containers discovered')).toBeInTheDocument()
      expect(screen.getByText('Try refreshing or check your environment.')).toBeInTheDocument()

      // Check button works
      const button = screen.getByRole('button', { name: /try again/i })
      expect(button).toBeInTheDocument()
      await user.click(button)
      expect(mockOnRetry).toHaveBeenCalledTimes(1)
    })

    it('should render complete empty state for no-mappings with retry', async () => {
      const user = userEvent.setup()
      const mockOnRetry = vi.fn()
      const { container } = render(<EmptyState type="no-mappings" onRetry={mockOnRetry} />)

      expect(container.querySelector('svg')).toBeInTheDocument()
      expect(screen.getByText('No secret mappings detected')).toBeInTheDocument()
      expect(
        screen.getByText('This is perfectly valid — your containers may already be configured.')
      ).toBeInTheDocument()

      const button = screen.getByRole('button', { name: /try again/i })
      await user.click(button)
      expect(mockOnRetry).toHaveBeenCalled()
    })

    it('should render complete empty state for filter-mismatch with retry', async () => {
      const user = userEvent.setup()
      const mockOnRetry = vi.fn()
      const { container } = render(<EmptyState type="filter-mismatch" onRetry={mockOnRetry} />)

      expect(container.querySelector('svg')).toBeInTheDocument()
      expect(screen.getByText('No containers match current filters')).toBeInTheDocument()
      expect(screen.getByText('Try adjusting your search term or filters.')).toBeInTheDocument()

      const button = screen.getByRole('button', { name: /try again/i })
      await user.click(button)
      expect(mockOnRetry).toHaveBeenCalled()
    })
  })

  describe('Accessibility', () => {
    it('should have semantic heading element', () => {
      render(<EmptyState type="no-containers" />)
      const heading = screen.getByRole('heading', { level: 3 })
      expect(heading).toBeInTheDocument()
    })

    it('should have descriptive button text', () => {
      const mockOnRetry = vi.fn()
      render(<EmptyState type="no-containers" onRetry={mockOnRetry} />)
      const button = screen.getByRole('button')
      expect(button.textContent).toBe('Try again')
    })

    it('should have proper text contrast colors', () => {
      render(<EmptyState type="no-containers" />)
      const heading = screen.getByRole('heading', { level: 3 })
      // Verify text-slate-200 is applied (light text for dark background)
      expect(heading).toHaveClass('text-slate-200')
    })

    it('should have proper link-like button styling with underline', () => {
      const mockOnRetry = vi.fn()
      render(<EmptyState type="no-containers" onRetry={mockOnRetry} />)
      const button = screen.getByRole('button')
      expect(button).toHaveClass('underline')
    })
  })
})
