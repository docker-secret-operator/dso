'use client'

import { useMemo, useState } from 'react'
import { Card, Badge, Input, Select, Skeleton, EmptyState } from '@/components/ui-modern'
import { ChevronRight, Search } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { Execution } from '@/lib/api/types'

interface ExecutionTableProps {
  executions: Execution[]
  total?: number
  isLoading?: boolean
  error?: string | null
  onSelectExecution?: (execution: Execution) => void
}

const ITEMS_PER_PAGE = 20

/**
 * Execution table with search, status filter, and pagination
 * Columns: ID | Status | Created | Readiness Score | Correlation ID
 */
export function ExecutionTable({
  executions,
  total = 0,
  isLoading,
  error,
  onSelectExecution,
}: ExecutionTableProps) {
  const [searchTerm, setSearchTerm] = useState('')
  const [statusFilter, setStatusFilter] = useState('all')
  const [currentPage, setCurrentPage] = useState(1)

  // Client-side filtering
  const filtered = useMemo(() => {
    let result = executions

    // Filter by search term (ID or correlation ID)
    if (searchTerm.trim()) {
      const term = searchTerm.toLowerCase()
      result = result.filter(e =>
        e.id.toLowerCase().includes(term) ||
        e.correlation_id.toLowerCase().includes(term)
      )
    }

    // Filter by status
    if (statusFilter !== 'all') {
      result = result.filter(e => e.status === statusFilter)
    }

    return result
  }, [executions, searchTerm, statusFilter])

  // Pagination
  const totalPages = Math.ceil(filtered.length / ITEMS_PER_PAGE)
  const start = (currentPage - 1) * ITEMS_PER_PAGE
  const paged = filtered.slice(start, start + ITEMS_PER_PAGE)

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'completed':
        return 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20'
      case 'failed':
        return 'bg-red-500/10 text-red-400 border-red-500/20'
      case 'running':
        return 'bg-blue-500/10 text-blue-400 border-blue-500/20'
      case 'queued':
        return 'bg-slate-500/10 text-slate-400 border-slate-500/20'
      case 'cancelled':
        return 'bg-orange-500/10 text-orange-400 border-orange-500/20'
      case 'paused':
        return 'bg-amber-500/10 text-amber-400 border-amber-500/20'
      case 'timed_out':
        return 'bg-red-500/10 text-red-400 border-red-500/20'
      default:
        return 'bg-slate-500/10 text-slate-400 border-slate-500/20'
    }
  }

  const formatDate = (dateStr: string) => {
    try {
      return new Date(dateStr).toLocaleTimeString([], {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
      })
    } catch {
      return dateStr
    }
  }

  if (isLoading) {
    return (
      <Card className="p-6">
        <h3 className="text-sm font-semibold text-slate-300 mb-4">Recent Executions</h3>
        <div className="space-y-3">
          <Skeleton className="h-10 w-full rounded" count={5} />
        </div>
      </Card>
    )
  }

  if (error) {
    return (
      <Card className="p-6">
        <h3 className="text-sm font-semibold text-slate-300 mb-4">Recent Executions</h3>
        <p className="text-red-400 text-sm">{error}</p>
      </Card>
    )
  }

  return (
    <Card className="p-6">
      <div className="mb-4">
        <h3 className="text-sm font-semibold text-slate-300 mb-4">Recent Executions</h3>

        {/* Search and Filter Controls */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mb-4">
          <Input
            placeholder="Search by ID or correlation ID..."
            value={searchTerm}
            onChange={(e) => {
              setSearchTerm(e.target.value)
              setCurrentPage(1)
            }}
            startIcon={<Search className="w-4 h-4" />}
          />
          <Select
            options={[
              { value: 'all', label: 'All Statuses' },
              { value: 'queued', label: 'Queued' },
              { value: 'running', label: 'Running' },
              { value: 'completed', label: 'Completed' },
              { value: 'failed', label: 'Failed' },
              { value: 'cancelled', label: 'Cancelled' },
              { value: 'paused', label: 'Paused' },
              { value: 'timed_out', label: 'Timed Out' },
            ]}
            value={statusFilter}
            onChange={(e) => {
              setStatusFilter(e.target.value)
              setCurrentPage(1)
            }}
          />
        </div>
      </div>

      {/* Table or Empty State */}
      {paged.length === 0 ? (
        <EmptyState
          icon={<Search className="w-5 h-5" />}
          title="No executions found"
          description={searchTerm || statusFilter !== 'all' ? 'Try adjusting your search or filters' : 'No execution data available'}
        />
      ) : (
        <>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-white/10">
                  <th className="text-left py-3 px-3 font-medium text-xs text-slate-400 uppercase">ID</th>
                  <th className="text-left py-3 px-3 font-medium text-xs text-slate-400 uppercase">Status</th>
                  <th className="text-left py-3 px-3 font-medium text-xs text-slate-400 uppercase">Created</th>
                  <th className="text-left py-3 px-3 font-medium text-xs text-slate-400 uppercase">Readiness</th>
                  <th className="text-left py-3 px-3 font-medium text-xs text-slate-400 uppercase">Correlation ID</th>
                  <th className="text-center py-3 px-3 font-medium text-xs text-slate-400 uppercase">Action</th>
                </tr>
              </thead>
              <tbody>
                {paged.map((execution) => (
                  <tr
                    key={execution.id}
                    className="border-b border-white/5 hover:bg-white/[0.02] transition-colors cursor-pointer"
                    onClick={() => onSelectExecution?.(execution)}
                  >
                    <td className="py-3 px-3">
                      <code className="text-xs text-slate-300 bg-black/40 px-2 py-1 rounded">
                        {execution.id.substring(0, 8)}…
                      </code>
                    </td>
                    <td className="py-3 px-3">
                      <Badge className={getStatusColor(execution.status)}>
                        {execution.status}
                      </Badge>
                    </td>
                    <td className="py-3 px-3 text-slate-400 text-xs">
                      {formatDate(execution.created_at)}
                    </td>
                    <td className="py-3 px-3">
                      <div className="text-xs text-slate-400">—</div>
                    </td>
                    <td className="py-3 px-3">
                      <code className="text-xs text-slate-500">
                        {execution.correlation_id.substring(0, 12)}…
                      </code>
                    </td>
                    <td className="py-3 px-3 text-center">
                      <button
                        className="inline-flex items-center justify-center w-6 h-6 rounded hover:bg-white/10 transition-colors"
                        onClick={(e) => {
                          e.stopPropagation()
                          onSelectExecution?.(execution)
                        }}
                      >
                        <ChevronRight className="w-4 h-4 text-slate-400" />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Pagination */}
          {totalPages > 1 && (
            <div className="flex items-center justify-between mt-4 text-xs text-slate-500">
              <span>
                Page {currentPage} of {totalPages} • {filtered.length} results
              </span>
              <div className="flex gap-2">
                <button
                  disabled={currentPage === 1}
                  onClick={() => setCurrentPage(p => Math.max(1, p - 1))}
                  className="px-2 py-1 rounded border border-white/10 hover:bg-white/5 disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  Previous
                </button>
                <button
                  disabled={currentPage === totalPages}
                  onClick={() => setCurrentPage(p => Math.min(totalPages, p + 1))}
                  className="px-2 py-1 rounded border border-white/10 hover:bg-white/5 disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  Next
                </button>
              </div>
            </div>
          )}
        </>
      )}
    </Card>
  )
}
