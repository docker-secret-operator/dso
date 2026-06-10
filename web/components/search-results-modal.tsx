'use client'

import React from 'react'
import { useRouter } from 'next/navigation'
import { Command } from 'cmdk'
import { Dialog, DialogContent } from '@/components/ui/dialog'
import { Container, Lock, AlertCircle, Link2, Play, User } from 'lucide-react'
import type { SearchResult } from '@/hooks/useGlobalSearch'

export interface SearchResultsModalProps {
  isOpen: boolean
  onOpenChange: (open: boolean) => void
  query: string
  onQueryChange: (query: string) => void
  groupedResults: {
    container: SearchResult[]
    secret: SearchResult[]
    event: SearchResult[]
    correlation: SearchResult[]
    execution: SearchResult[]
    actor: SearchResult[]
  }
  isEmpty: boolean
  hasResults: boolean
}

const CATEGORY_META: Record<string, { icon: React.ReactNode; label: string }> = {
  container:   { icon: <Container className="w-4 h-4 text-blue-600" />,   label: 'Containers' },
  secret:      { icon: <Lock className="w-4 h-4 text-green-600" />,        label: 'Secrets' },
  event:       { icon: <AlertCircle className="w-4 h-4 text-yellow-600" />,label: 'Events' },
  correlation: { icon: <Link2 className="w-4 h-4 text-indigo-600" />,      label: 'Correlation Chains' },
  execution:   { icon: <Play className="w-4 h-4 text-teal-600" />,         label: 'Execution Journeys' },
  actor:       { icon: <User className="w-4 h-4 text-purple-600" />,       label: 'Actors' },
}

function ResultGroup({ category, results, onSelect }: {
  category: string
  results: SearchResult[]
  onSelect: (route: string) => void
}) {
  if (results.length === 0) return null
  const meta = CATEGORY_META[category] ?? { icon: null, label: category }
  return (
    <div>
      <div className="px-2 py-1.5 mt-2 first:mt-0">
        <p className="text-xs font-medium text-gray-500 uppercase tracking-wide">{meta.label}</p>
      </div>
      {results.map(result => (
        <div
          key={result.id}
          onClick={() => onSelect(result.route)}
          className="cursor-pointer rounded px-2 py-2 hover:bg-gray-100"
        >
          <div className="flex items-center gap-2">
            {meta.icon}
            <div className="flex-1 min-w-0">
              <p className="text-sm font-medium text-gray-900 truncate font-mono">{result.title}</p>
              {result.subtitle && (
                <p className="text-xs text-gray-500 truncate">{result.subtitle}</p>
              )}
            </div>
          </div>
        </div>
      ))}
    </div>
  )
}

export function SearchResultsModal({
  isOpen,
  onOpenChange,
  query,
  onQueryChange,
  groupedResults,
  isEmpty,
  hasResults,
}: SearchResultsModalProps) {
  const router = useRouter()

  const handleSelect = (route: string) => {
    onOpenChange(false)
    router.push(route)
  }

  const ORDER: (keyof typeof groupedResults)[] = ['correlation', 'execution', 'actor', 'secret', 'container', 'event']

  return (
    <Dialog open={isOpen} onOpenChange={onOpenChange}>
      <DialogContent className="overflow-hidden p-0 shadow-lg">
        <Command className="[&_[cmdk-input]]:h-12">
          <div className="flex items-center border-b px-3">
            <input
              placeholder="Search correlation IDs, executions, actors, secrets…"
              value={query}
              onChange={(e) => onQueryChange(e.target.value)}
              className="w-full bg-transparent py-3 text-sm outline-none placeholder:text-gray-400"
              autoFocus
            />
          </div>

          <div className="max-h-[380px] overflow-y-auto">
            {hasResults ? (
              <div className="overflow-hidden p-1">
                {ORDER.map(cat => (
                  <ResultGroup
                    key={cat}
                    category={cat}
                    results={groupedResults[cat]}
                    onSelect={handleSelect}
                  />
                ))}
              </div>
            ) : isEmpty ? (
              <div className="py-6 text-center">
                <p className="text-sm text-gray-500">No results found for "{query}"</p>
              </div>
            ) : (
              <div className="py-6 text-center">
                <p className="text-sm text-gray-500">Search correlation IDs, executions, actors, secrets…</p>
              </div>
            )}
          </div>

          <div className="border-t px-2 py-2">
            <p className="text-xs text-gray-400">
              <kbd className="rounded bg-gray-100 px-1.5 py-0.5">⌘K</kbd> to open ·{' '}
              <kbd className="rounded bg-gray-100 px-1.5 py-0.5">Esc</kbd> to close
            </p>
          </div>
        </Command>
      </DialogContent>
    </Dialog>
  )
}
