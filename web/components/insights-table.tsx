'use client'

import { useState, useMemo, useCallback } from 'react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { EmptyState } from './empty-state'
import { Search, ChevronUp, ChevronDown } from 'lucide-react'

export interface InsightsTableColumn<T> {
  key: keyof T
  label: string
  width?: string
  sortable?: boolean
  render?: (value: unknown, row: T) => React.ReactNode
}

interface InsightsTableProps<T extends { id: string }> {
  title: string
  description?: string
  columns: InsightsTableColumn<T>[]
  data: T[]
  searchableFields?: (keyof T)[]
  sortByDefault?: keyof T
  isLoading?: boolean
}

type SortDirection = 'asc' | 'desc'

export function InsightsTable<T extends { id: string }>({
  title,
  description,
  columns,
  data,
  searchableFields = [],
  sortByDefault,
  isLoading,
}: InsightsTableProps<T>) {
  const [searchQuery, setSearchQuery] = useState('')
  const [sortKey, setSortKey] = useState<keyof T | null>(sortByDefault || null)
  const [sortDirection, setSortDirection] = useState<SortDirection>('asc')

  // Filter by search
  const filteredData = useMemo(() => {
    if (!searchQuery.trim() || searchableFields.length === 0) return data

    const query = searchQuery.toLowerCase()
    return data.filter((row) =>
      searchableFields.some((field) => {
        const value = row[field]
        return String(value).toLowerCase().includes(query)
      })
    )
  }, [data, searchQuery, searchableFields])

  // Sort
  const sortedData = useMemo(() => {
    if (!sortKey) return filteredData

    const sorted = [...filteredData].sort((a, b) => {
      const aVal = a[sortKey]
      const bVal = b[sortKey]

      if (typeof aVal === 'string' && typeof bVal === 'string') {
        return sortDirection === 'asc' ? aVal.localeCompare(bVal) : bVal.localeCompare(aVal)
      }

      if (typeof aVal === 'number' && typeof bVal === 'number') {
        return sortDirection === 'asc' ? aVal - bVal : bVal - aVal
      }

      return 0
    })

    return sorted
  }, [filteredData, sortKey, sortDirection])

  const handleSort = useCallback(
    (key: keyof T) => {
      if (sortKey === key) {
        setSortDirection(sortDirection === 'asc' ? 'desc' : 'asc')
      } else {
        setSortKey(key)
        setSortDirection('asc')
      }
    },
    [sortKey, sortDirection]
  )

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
          {description && <CardDescription>{description}</CardDescription>}
        </CardHeader>
        <CardContent className="py-8">
          <div className="flex items-center justify-center">
            <div className="text-center">
              <div className="w-8 h-8 border-2 border-gray-300 border-t-blue-600 rounded-full animate-spin mx-auto mb-2" />
              <p className="text-sm text-gray-600">Loading...</p>
            </div>
          </div>
        </CardContent>
      </Card>
    )
  }

  if (data.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
          {description && <CardDescription>{description}</CardDescription>}
        </CardHeader>
        <CardContent className="py-8">
          <EmptyState
            type="empty"
            title="No data available"
            description={description || 'No entries to display'}
          />
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <div className="space-y-4">
          <div>
            <CardTitle>{title}</CardTitle>
            {description && <CardDescription>{description}</CardDescription>}
          </div>

          {searchableFields.length > 0 && (
            <div className="relative">
              <Search className="absolute left-3 top-3 w-4 h-4 text-gray-400" />
              <input
                type="text"
                placeholder="Search..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="w-full pl-9 pr-4 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
          )}
        </div>
      </CardHeader>

      <CardContent>
        {filteredData.length === 0 ? (
          <EmptyState
            type="no-results"
            title="No results found"
            description={`No entries match "${searchQuery}"`}
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b">
                  {columns.map((column) => (
                    <th
                      key={String(column.key)}
                      className={`px-4 py-3 text-left font-semibold text-gray-700 bg-gray-50 ${
                        column.width || ''
                      }`}
                    >
                      {column.sortable ? (
                        <button
                          onClick={() => handleSort(column.key)}
                          className="flex items-center gap-1 hover:text-gray-900"
                        >
                          {column.label}
                          {sortKey === column.key && (
                            <>
                              {sortDirection === 'asc' ? (
                                <ChevronUp className="w-4 h-4" />
                              ) : (
                                <ChevronDown className="w-4 h-4" />
                              )}
                            </>
                          )}
                        </button>
                      ) : (
                        column.label
                      )}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {sortedData.map((row, idx) => (
                  <tr key={row.id} className={idx % 2 === 0 ? 'bg-white' : 'bg-gray-50'}>
                    {columns.map((column) => (
                      <td key={String(column.key)} className="px-4 py-3 border-b">
                        {column.render ? column.render(row[column.key], row) : String(row[column.key] || '-')}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
