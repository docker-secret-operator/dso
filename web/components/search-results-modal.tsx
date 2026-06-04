'use client'

import React from 'react'
import { useRouter } from 'next/navigation'
import { Command } from 'cmdk'
import { Dialog, DialogContent } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import { Container, Lock, AlertCircle } from 'lucide-react'
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
  }
  isEmpty: boolean
  hasResults: boolean
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

  const getCategoryIcon = (category: string) => {
    switch (category) {
      case 'container':
        return <Container className="w-4 h-4 text-blue-600" />
      case 'secret':
        return <Lock className="w-4 h-4 text-green-600" />
      case 'event':
        return <AlertCircle className="w-4 h-4 text-yellow-600" />
      default:
        return null
    }
  }

  const getCategoryLabel = (category: string) => {
    switch (category) {
      case 'container':
        return 'Containers'
      case 'secret':
        return 'Secrets'
      case 'event':
        return 'Events'
      default:
        return ''
    }
  }

  return (
    <Dialog open={isOpen} onOpenChange={onOpenChange}>
      <DialogContent className="overflow-hidden p-0 shadow-lg">
        <Command className="[&_[cmdk-input]]:h-12">
          <div className="flex items-center border-b px-3">
            <input
              placeholder="Search containers, secrets, events..."
              value={query}
              onChange={(e) => onQueryChange(e.target.value)}
              className="w-full bg-transparent py-3 text-sm outline-none placeholder:text-gray-500"
              autoFocus
            />
          </div>

          <div className="max-h-[300px] overflow-y-auto">
            {hasResults ? (
              <div className="overflow-hidden p-1">
                {groupedResults.container.length > 0 && (
                  <div>
                    <div className="px-2 py-1.5">
                      <p className="text-xs font-medium text-gray-600">
                        {getCategoryLabel('container')}
                      </p>
                    </div>
                    {groupedResults.container.map((result) => (
                      <div
                        key={result.id}
                        onClick={() => handleSelect(result.route)}
                        className="cursor-pointer rounded px-2 py-2 hover:bg-gray-100"
                      >
                        <div className="flex items-center gap-2">
                          {getCategoryIcon('container')}
                          <div className="flex-1 min-w-0">
                            <p className="text-sm font-medium text-gray-900 truncate">
                              {result.title}
                            </p>
                            {result.subtitle && (
                              <p className="text-xs text-gray-600 truncate">{result.subtitle}</p>
                            )}
                          </div>
                          <Badge className="bg-blue-100 text-blue-800 text-xs">
                            {result.metadata?.status}
                          </Badge>
                        </div>
                      </div>
                    ))}
                  </div>
                )}

                {groupedResults.secret.length > 0 && (
                  <div>
                    <div className="px-2 py-1.5 mt-2">
                      <p className="text-xs font-medium text-gray-600">{getCategoryLabel('secret')}</p>
                    </div>
                    {groupedResults.secret.map((result) => (
                      <div
                        key={result.id}
                        onClick={() => handleSelect(result.route)}
                        className="cursor-pointer rounded px-2 py-2 hover:bg-gray-100"
                      >
                        <div className="flex items-center gap-2">
                          {getCategoryIcon('secret')}
                          <div className="flex-1 min-w-0">
                            <p className="text-sm font-medium text-gray-900 truncate">
                              {result.title}
                            </p>
                            {result.subtitle && (
                              <p className="text-xs text-gray-600 truncate">{result.subtitle}</p>
                            )}
                          </div>
                          <Badge className="bg-green-100 text-green-800 text-xs">
                            {result.metadata?.status}
                          </Badge>
                        </div>
                      </div>
                    ))}
                  </div>
                )}

                {groupedResults.event.length > 0 && (
                  <div>
                    <div className="px-2 py-1.5 mt-2">
                      <p className="text-xs font-medium text-gray-600">{getCategoryLabel('event')}</p>
                    </div>
                    {groupedResults.event.map((result) => (
                      <div
                        key={result.id}
                        onClick={() => handleSelect(result.route)}
                        className="cursor-pointer rounded px-2 py-2 hover:bg-gray-100"
                      >
                        <div className="flex items-center gap-2">
                          {getCategoryIcon('event')}
                          <div className="flex-1 min-w-0">
                            <p className="text-sm font-medium text-gray-900 truncate">
                              {result.title}
                            </p>
                            {result.subtitle && (
                              <p className="text-xs text-gray-600 truncate">{result.subtitle}</p>
                            )}
                          </div>
                        </div>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            ) : isEmpty ? (
              <div className="py-6 text-center">
                <p className="text-sm text-gray-600">No results found for "{query}"</p>
              </div>
            ) : (
              <div className="py-6 text-center">
                <p className="text-sm text-gray-600">Type to search containers, secrets, and events</p>
              </div>
            )}
          </div>

          <div className="border-t px-2 py-2">
            <p className="text-xs text-gray-500">
              Press <kbd className="rounded bg-gray-100 px-2 py-1">Enter</kbd> to navigate,{' '}
              <kbd className="rounded bg-gray-100 px-2 py-1">Esc</kbd> to close
            </p>
          </div>
        </Command>
      </DialogContent>
    </Dialog>
  )
}
