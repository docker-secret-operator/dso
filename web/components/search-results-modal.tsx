'use client'

import React from 'react'
import { useRouter } from 'next/navigation'
import { Container, Lock, AlertCircle, Link2, Play, User, Search } from 'lucide-react'
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
  container:   { icon: <Container  className="w-3.5 h-3.5 text-blue-400" />,   label: 'Containers' },
  secret:      { icon: <Lock       className="w-3.5 h-3.5 text-emerald-400" />, label: 'Secrets' },
  event:       { icon: <AlertCircle className="w-3.5 h-3.5 text-amber-400" />,  label: 'Events' },
  correlation: { icon: <Link2      className="w-3.5 h-3.5 text-indigo-400" />,  label: 'Correlation Chains' },
  execution:   { icon: <Play       className="w-3.5 h-3.5 text-cyan-400" />,    label: 'Executions' },
  actor:       { icon: <User       className="w-3.5 h-3.5 text-purple-400" />,  label: 'Actors' },
}

const ORDER: (keyof SearchResultsModalProps['groupedResults'])[] = [
  'correlation', 'execution', 'actor', 'secret', 'container', 'event',
]

function ResultGroup({ category, results, onSelect }: {
  category: string
  results: SearchResult[]
  onSelect: (route: string) => void
}) {
  if (results.length === 0) return null
  const meta = CATEGORY_META[category] ?? { icon: null, label: category }

  return (
    <div>
      <div className="px-3 pt-3 pb-1">
        <p className="text-[10px] font-semibold text-slate-600 uppercase tracking-widest">{meta.label}</p>
      </div>
      {results.map(result => (
        <button
          key={result.id}
          onClick={() => onSelect(result.route)}
          className="w-full text-left flex items-center gap-3 px-3 py-2.5 hover:bg-white/[0.05] transition-colors rounded-lg mx-1"
          style={{ width: 'calc(100% - 8px)' }}
        >
          <span className="flex-shrink-0">{meta.icon}</span>
          <div className="flex-1 min-w-0">
            <p className="text-sm text-slate-200 truncate font-mono">{result.title}</p>
            {result.subtitle && (
              <p className="text-xs text-slate-600 truncate">{result.subtitle}</p>
            )}
          </div>
        </button>
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

  if (!isOpen) return null

  return (
    <>
      {/* Backdrop */}
      <div
        className="fixed inset-0 bg-black/60 backdrop-blur-sm z-50 animate-fade-in"
        onClick={() => onOpenChange(false)}
      />

      {/* Modal */}
      <div className="fixed left-1/2 top-[20%] -translate-x-1/2 w-full max-w-xl z-50 animate-fade-in px-4">
        <div className="bg-[#111318] border border-white/[0.12] rounded-2xl shadow-2xl overflow-hidden">

          {/* Search input */}
          <div className="flex items-center gap-3 px-4 border-b border-white/[0.07]">
            <Search className="w-4 h-4 text-slate-600 flex-shrink-0" />
            <input
              className="flex-1 py-4 bg-transparent text-sm text-slate-200 placeholder:text-slate-600 focus:outline-none"
              placeholder="Search correlation IDs, secrets, actors…"
              value={query}
              onChange={e => onQueryChange(e.target.value)}
              autoFocus
            />
            <kbd className="hidden sm:block text-[10px] font-medium text-slate-600 bg-white/[0.06] border border-white/[0.08] rounded px-1.5 py-0.5 flex-shrink-0">
              ESC
            </kbd>
          </div>

          {/* Results */}
          <div className="max-h-[360px] overflow-y-auto p-1">
            {hasResults ? (
              ORDER.map(cat => (
                <ResultGroup
                  key={cat}
                  category={cat}
                  results={groupedResults[cat]}
                  onSelect={handleSelect}
                />
              ))
            ) : isEmpty ? (
              <div className="py-10 text-center">
                <p className="text-sm text-slate-500">No results for "{query}"</p>
              </div>
            ) : (
              <div className="py-10 text-center">
                <p className="text-sm text-slate-600">Search secrets, containers, correlation IDs, actors…</p>
              </div>
            )}
          </div>

          {/* Footer */}
          <div className="border-t border-white/[0.07] px-4 py-2.5 flex items-center gap-3">
            <span className="text-[11px] text-slate-700">
              <kbd className="bg-white/[0.06] border border-white/[0.08] rounded px-1.5 py-0.5 mr-1">↑↓</kbd>
              navigate
            </span>
            <span className="text-[11px] text-slate-700">
              <kbd className="bg-white/[0.06] border border-white/[0.08] rounded px-1.5 py-0.5 mr-1">↵</kbd>
              open
            </span>
            <span className="text-[11px] text-slate-700 ml-auto">
              <kbd className="bg-white/[0.06] border border-white/[0.08] rounded px-1.5 py-0.5 mr-1">⌘K</kbd>
              toggle
            </span>
          </div>
        </div>
      </div>
    </>
  )
}
