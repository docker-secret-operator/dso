'use client'

import React, { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { apiClient } from '@/lib/api-client'
import { SearchResultsModal } from './search-results-modal'
import { useGlobalSearch } from '@/hooks/useGlobalSearch'
import { Search } from 'lucide-react'

export function GlobalSearch() {
  const [isOpen, setIsOpen] = useState(false)

  // All queries are gated on isOpen — no fetches until the user opens search
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
    enabled: isOpen,
    refetchInterval: isOpen ? 60000 : false,
  })

  const { data: secretsPage } = useQuery({
    queryKey: ['secrets-search'],
    queryFn: () => apiClient.getSecretsPage({ pageSize: 50 }),
    enabled: isOpen,
    refetchInterval: isOpen ? 60000 : false,
  })
  const secrets = secretsPage?.items ?? []

  const { data: events = [] } = useQuery({
    queryKey: ['events-search'],
    queryFn: () => apiClient.getEvents(100),
    enabled: isOpen,
    refetchInterval: isOpen ? 30000 : false,
  })

  const { data: auditData } = useQuery({
    queryKey: ['audit-search'],
    queryFn: () => apiClient.getAuditEvents({ limit: 200 }),
    enabled: isOpen,
    refetchInterval: isOpen ? 60000 : false,
  })
  const auditEvents = auditData?.events ?? []

  // Use global search hook
  const { query, results, groupedResults, isEmpty, hasResults, handleQueryChange, handleOpenChange } =
    useGlobalSearch({
      isOpen,
      onOpenChange: setIsOpen,
      containers,
      secrets,
      events,
      auditEvents,
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
        className="inline-flex items-center gap-2 rounded-lg border border-white/[0.09] bg-white/[0.04] px-2.5 py-1.5 text-sm text-slate-500 hover:text-slate-300 hover:bg-white/[0.07] transition-colors"
        aria-label="Search (⌘K)"
      >
        <Search className="w-3.5 h-3.5" />
        <span className="hidden sm:inline text-xs">Search…</span>
        <kbd className="hidden sm:inline-block ml-1 text-[10px] font-medium text-slate-700 bg-white/[0.06] border border-white/[0.08] rounded px-1.5 py-0.5">
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
