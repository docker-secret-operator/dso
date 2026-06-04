'use client'

import React, { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { apiClient } from '@/lib/api-client'
import { SearchResultsModal } from './search-results-modal'
import { useGlobalSearch } from '@/hooks/useGlobalSearch'
import { Search } from 'lucide-react'

export function GlobalSearch() {
  // Fetch all searchable data
  const { data: containers = [] } = useQuery({
    queryKey: ['containers-search'],
    queryFn: async () => {
      try {
        const response = await fetch('/api/discovery/docker')
        if (!response.ok) return []
        const data = await response.json()
        return data.containers || []
      } catch {
        return []
      }
    },
    refetchInterval: 60000, // Refetch every minute
  })

  const { data: secrets = [] } = useQuery({
    queryKey: ['secrets-search'],
    queryFn: () => apiClient.getSecrets(),
    refetchInterval: 60000,
  })

  const { data: events = [] } = useQuery({
    queryKey: ['events-search'],
    queryFn: () => apiClient.getEvents(100),
    refetchInterval: 30000,
  })

  // Use global search hook
  const { query, isOpen, results, groupedResults, isEmpty, hasResults, handleQueryChange, handleOpenChange } =
    useGlobalSearch({
      containers,
      secrets,
      events,
    })

  // Handle keyboard shortcuts
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      // Ctrl+K or Cmd+K
      if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
        e.preventDefault()
        handleOpenChange(!isOpen)
      }

      // Escape to close
      if (e.key === 'Escape') {
        handleOpenChange(false)
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, handleOpenChange])

  return (
    <>
      {/* Search Trigger Button */}
      <button
        onClick={() => handleOpenChange(true)}
        className="relative inline-flex items-center gap-2 rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm text-gray-600 hover:bg-gray-50"
      >
        <Search className="w-4 h-4" />
        <span className="hidden sm:inline">Search...</span>
        <kbd className="hidden sm:inline-block ml-auto text-xs font-semibold text-gray-400">
          ⌘K
        </kbd>
      </button>

      {/* Search Results Modal */}
      <SearchResultsModal
        isOpen={isOpen}
        onOpenChange={handleOpenChange}
        query={query}
        onQueryChange={handleQueryChange}
        groupedResults={groupedResults}
        isEmpty={isEmpty}
        hasResults={hasResults}
      />
    </>
  )
}
