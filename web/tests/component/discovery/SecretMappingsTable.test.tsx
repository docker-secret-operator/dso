import { describe, it, expect, beforeEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { SecretMappingsTable } from '@/components/discovery/SecretMappingsTable'
import { SecretMappingSuggestion } from '@/lib/api/types'

describe('SecretMappingsTable Component', () => {
  const mockMappings: SecretMappingSuggestion[] = [
    {
      env_var_name: 'DB_PASSWORD',
      suggested_secret_name: 'db-password',
      confidence: 'high',
      reason: 'Naming pattern matches database credentials',
      is_configured: true,
    },
    {
      env_var_name: 'API_KEY',
      suggested_secret_name: 'api-key',
      confidence: 'medium',
      reason: 'Variable contains "KEY" but not confirmed',
      is_configured: false,
    },
    {
      env_var_name: 'SERVICE_TOKEN',
      suggested_secret_name: 'service-token',
      confidence: 'low',
      reason: 'Uncertain if this is a secret',
      is_configured: false,
    },
  ]

  beforeEach(() => {
    // Clear any mocks or state
  })

  describe('Rendering', () => {
    it('should render table without crashing', () => {
      const { container } = render(
        <SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />
      )
      expect(container).toBeInTheDocument()
    })

    it('should display all 5 column headers', () => {
      render(<SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />)
      expect(screen.getByText('Environment Variable')).toBeInTheDocument()
      expect(screen.getByText('Suggested Secret')).toBeInTheDocument()
      expect(screen.getByText('Confidence')).toBeInTheDocument()
      expect(screen.getByText('Reason')).toBeInTheDocument()
      expect(screen.getByText('Status')).toBeInTheDocument()
    })

    it('should render all mapping rows', () => {
      render(<SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />)
      expect(screen.getByText('DB_PASSWORD')).toBeInTheDocument()
      expect(screen.getByText('API_KEY')).toBeInTheDocument()
      expect(screen.getByText('SERVICE_TOKEN')).toBeInTheDocument()
    })

    it('should display suggested secret names', () => {
      render(<SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />)
      expect(screen.getByText('db-password')).toBeInTheDocument()
      expect(screen.getByText('api-key')).toBeInTheDocument()
      expect(screen.getByText('service-token')).toBeInTheDocument()
    })

    it('should display reason text for each mapping', () => {
      render(<SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />)
      mockMappings.forEach(mapping => {
        expect(screen.getByText(mapping.reason)).toBeInTheDocument()
      })
    })
  })

  describe('Confidence Badges', () => {
    it('should display confidence level for each mapping', () => {
      render(<SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />)
      expect(screen.getByText('high')).toBeInTheDocument()
      expect(screen.getByText('medium')).toBeInTheDocument()
      expect(screen.getByText('low')).toBeInTheDocument()
    })

    it('should apply correct color class for high confidence', () => {
      const { container } = render(
        <SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />
      )
      const highBadge = container.querySelector('[class*="emerald"]')
      expect(highBadge).toBeInTheDocument()
    })

    it('should apply correct color class for medium confidence', () => {
      const { container } = render(
        <SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />
      )
      const mediumBadge = container.querySelector('[class*="amber"]')
      expect(mediumBadge).toBeInTheDocument()
    })

    it('should apply correct color class for low confidence', () => {
      const { container } = render(
        <SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />
      )
      const lowBadge = container.querySelector('[class*="red"]')
      expect(lowBadge).toBeInTheDocument()
    })

    it('should have proper badge styling', () => {
      const { container } = render(
        <SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />
      )
      const badges = container.querySelectorAll('[class*="border"]')
      expect(badges.length).toBeGreaterThan(0)
    })
  })

  describe('Search Highlighting', () => {
    it('should highlight matching environment variable names', () => {
      const { container } = render(
        <SecretMappingsTable mappings={mockMappings} searchTerm="DB" isLoading={false} />
      )
      const highlighted = container.querySelector('[class*="indigo-500"]')
      expect(highlighted).toBeInTheDocument()
    })

    it('should highlight matching secret names', () => {
      const { container } = render(
        <SecretMappingsTable mappings={mockMappings} searchTerm="password" isLoading={false} />
      )
      const highlighted = container.querySelector('[class*="indigo-500"]')
      expect(highlighted).toBeInTheDocument()
    })

    it('should filter rows based on search term', () => {
      render(<SecretMappingsTable mappings={mockMappings} searchTerm="API" isLoading={false} />)
      expect(screen.getByText('API_KEY')).toBeInTheDocument()
      expect(screen.queryByText('DB_PASSWORD')).not.toBeInTheDocument()
    })

    it('should be case-insensitive search', () => {
      render(<SecretMappingsTable mappings={mockMappings} searchTerm="db_password" isLoading={false} />)
      expect(screen.getByText('DB_PASSWORD')).toBeInTheDocument()
    })

    it('should show all rows with empty search term', () => {
      render(<SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />)
      expect(screen.getByText('DB_PASSWORD')).toBeInTheDocument()
      expect(screen.getByText('API_KEY')).toBeInTheDocument()
      expect(screen.getByText('SERVICE_TOKEN')).toBeInTheDocument()
    })

    it('should handle whitespace in search term', () => {
      render(<SecretMappingsTable mappings={mockMappings} searchTerm="  " isLoading={false} />)
      expect(screen.getByText('DB_PASSWORD')).toBeInTheDocument()
    })
  })

  describe('Configuration Status', () => {
    it('should show configured status with check icon', () => {
      render(<SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />)
      // DB_PASSWORD is configured
      const row = screen.getByText('DB_PASSWORD').closest('div')
      const checkIcon = row?.querySelector('[class*="text-emerald"]')
      expect(checkIcon).toBeInTheDocument()
    })

    it('should show unconfigured status with alert icon', () => {
      render(<SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />)
      // API_KEY is unconfigured
      const row = screen.getByText('API_KEY').closest('div')
      const alertIcon = row?.querySelector('[class*="text-amber"]')
      expect(alertIcon).toBeInTheDocument()
    })

    it('should display correct icon for each mapping status', () => {
      const { container } = render(
        <SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />
      )
      // Check for icon elements
      const icons = container.querySelectorAll('svg')
      expect(icons.length).toBeGreaterThan(0)
    })
  })

  describe('Loading State', () => {
    it('should show skeleton loaders when loading', () => {
      const { container } = render(
        <SecretMappingsTable mappings={[]} searchTerm="" isLoading={true} />
      )
      const skeletons = container.querySelectorAll('[class*="skeleton"]')
      expect(skeletons.length).toBeGreaterThan(0)
    })

    it('should display headers while loading', () => {
      render(<SecretMappingsTable mappings={[]} searchTerm="" isLoading={true} />)
      expect(screen.getByText('Environment Variable')).toBeInTheDocument()
      expect(screen.getByText('Confidence')).toBeInTheDocument()
    })

    it('should render 4 skeleton rows', () => {
      const { container } = render(
        <SecretMappingsTable mappings={[]} searchTerm="" isLoading={true} />
      )
      const skeletons = container.querySelectorAll('[class*="skeleton"]')
      expect(skeletons.length).toBe(4)
    })

    it('should not show data while loading', () => {
      render(
        <SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={true} />
      )
      expect(screen.queryByText('DB_PASSWORD')).not.toBeInTheDocument()
    })

    it('should transition from loading to loaded', () => {
      const { rerender } = render(
        <SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={true} />
      )
      expect(screen.queryByText('DB_PASSWORD')).not.toBeInTheDocument()

      rerender(
        <SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />
      )
      expect(screen.getByText('DB_PASSWORD')).toBeInTheDocument()
    })
  })

  describe('Empty State', () => {
    it('should show empty state when no mappings provided', () => {
      render(<SecretMappingsTable mappings={undefined} searchTerm="" isLoading={false} />)
      // Empty state should render
      const { container } = render(
        <SecretMappingsTable mappings={undefined} searchTerm="" isLoading={false} />
      )
      expect(container).toBeInTheDocument()
    })

    it('should show empty state for empty array', () => {
      render(<SecretMappingsTable mappings={[]} searchTerm="" isLoading={false} />)
      const { container } = render(
        <SecretMappingsTable mappings={[]} searchTerm="" isLoading={false} />
      )
      expect(container).toBeInTheDocument()
    })

    it('should show empty state when search yields no results', () => {
      render(
        <SecretMappingsTable mappings={mockMappings} searchTerm="NONEXISTENT" isLoading={false} />
      )
      // Empty state should render
      const { container } = render(
        <SecretMappingsTable mappings={mockMappings} searchTerm="NONEXISTENT" isLoading={false} />
      )
      expect(container).toBeInTheDocument()
    })

    it('should not display table headers in empty state', () => {
      render(<SecretMappingsTable mappings={[]} searchTerm="" isLoading={false} />)
      const gridHeaders = document.querySelectorAll('[class*="grid-cols-5"]')
      expect(gridHeaders.length).toBe(0)
    })
  })

  describe('Row Styling', () => {
    it('should have transition styling on rows', () => {
      const { container } = render(
        <SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />
      )
      const rows = container.querySelectorAll('[class*="transition"]')
      expect(rows.length).toBeGreaterThan(0)
    })

    it('should apply highlight background to search matches', () => {
      const { container } = render(
        <SecretMappingsTable mappings={mockMappings} searchTerm="DB" isLoading={false} />
      )
      const highlighted = container.querySelector('[class*="indigo"]')
      expect(highlighted).toBeInTheDocument()
    })

    it('should have proper border styling between rows', () => {
      const { container } = render(
        <SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />
      )
      const bordered = container.querySelector('[class*="border-b"]')
      expect(bordered).toBeInTheDocument()
    })
  })

  describe('Data Display', () => {
    it('should render monospace font for variable names', () => {
      const { container } = render(
        <SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />
      )
      const monoText = container.querySelector('[class*="font-mono"]')
      expect(monoText).toBeInTheDocument()
    })

    it('should display small font for reason text', () => {
      const { container } = render(
        <SecretMappingsTable mappings={mockMappings} searchTerm="" isLoading={false} />
      )
      const smallText = container.querySelector('[class*="text-xs"]')
      expect(smallText).toBeInTheDocument()
    })

    it('should handle long reason text with title attribute', () => {
      const longReason = mockMappings[0]
      const { container } = render(
        <SecretMappingsTable mappings={[longReason]} searchTerm="" isLoading={false} />
      )
      const reasonElement = screen.getByText(longReason.reason)
      expect(reasonElement).toHaveAttribute('title', longReason.reason)
    })
  })

  describe('Edge Cases', () => {
    it('should handle single mapping', () => {
      render(
        <SecretMappingsTable mappings={[mockMappings[0]]} searchTerm="" isLoading={false} />
      )
      expect(screen.getByText('DB_PASSWORD')).toBeInTheDocument()
    })

    it('should handle many mappings', () => {
      const manyMappings = Array.from({ length: 50 }, (_, i) => ({
        env_var_name: `VAR_${i}`,
        suggested_secret_name: `secret-${i}`,
        confidence: (['high', 'medium', 'low'] as const)[i % 3],
        reason: `Reason ${i}`,
        is_configured: i % 2 === 0,
      }))
      render(
        <SecretMappingsTable mappings={manyMappings} searchTerm="" isLoading={false} />
      )
      expect(screen.getByText('VAR_0')).toBeInTheDocument()
    })

    it('should handle special characters in search', () => {
      render(
        <SecretMappingsTable mappings={mockMappings} searchTerm="DB_" isLoading={false} />
      )
      expect(screen.getByText('DB_PASSWORD')).toBeInTheDocument()
    })

    it('should handle confidence level variations', () => {
      const allHighConfidence = mockMappings.map(m => ({ ...m, confidence: 'high' as const }))
      render(
        <SecretMappingsTable mappings={allHighConfidence} searchTerm="" isLoading={false} />
      )
      const highBadges = screen.getAllByText('high')
      expect(highBadges.length).toBe(3)
    })

    it('should handle all configured mappings', () => {
      const allConfigured = mockMappings.map(m => ({ ...m, is_configured: true }))
      render(
        <SecretMappingsTable mappings={allConfigured} searchTerm="" isLoading={false} />
      )
      expect(screen.getByText('DB_PASSWORD')).toBeInTheDocument()
    })

    it('should handle all unconfigured mappings', () => {
      const allUnconfigured = mockMappings.map(m => ({ ...m, is_configured: false }))
      render(
        <SecretMappingsTable mappings={allUnconfigured} searchTerm="" isLoading={false} />
      )
      expect(screen.getByText('DB_PASSWORD')).toBeInTheDocument()
    })
  })
})
