'use client'

import { useRouter, useSearchParams, usePathname } from 'next/navigation'
import { useCallback } from 'react'
import { ChevronLeft, ChevronRight } from 'lucide-react'

interface PaginationProps {
  page: number
  pageSize: number
  total: number
  onPageChange?: (page: number) => void
  /** When true, page changes update the URL ?page= param. Default true. */
  urlState?: boolean
}

export function Pagination({ page, pageSize, total, onPageChange, urlState = true }: PaginationProps) {
  const router = useRouter()
  const pathname = usePathname()
  const searchParams = useSearchParams()

  const totalPages = Math.max(1, Math.ceil(total / pageSize))
  const hasPrev = page > 1
  const hasNext = page < totalPages
  const from = total === 0 ? 0 : (page - 1) * pageSize + 1
  const to = Math.min(page * pageSize, total)

  const go = useCallback(
    (next: number) => {
      if (urlState) {
        const params = new URLSearchParams(searchParams?.toString() ?? '')
        params.set('page', String(next))
        router.push(`${pathname}?${params.toString()}`)
      }
      onPageChange?.(next)
    },
    [urlState, searchParams, pathname, router, onPageChange]
  )

  if (total === 0) return null

  return (
    <div className="flex items-center justify-between px-1 py-3 text-sm text-slate-400">
      <span>
        {from}–{to} of {total}
      </span>
      <div className="flex items-center gap-1">
        <button
          onClick={() => go(page - 1)}
          disabled={!hasPrev}
          className="inline-flex items-center gap-1 rounded-md px-2.5 py-1.5 border border-white/[0.08] bg-white/[0.03] hover:bg-white/[0.07] disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
        >
          <ChevronLeft className="w-3.5 h-3.5" />
          Prev
        </button>
        <span className="px-2 text-slate-500">
          {page} / {totalPages}
        </span>
        <button
          onClick={() => go(page + 1)}
          disabled={!hasNext}
          className="inline-flex items-center gap-1 rounded-md px-2.5 py-1.5 border border-white/[0.08] bg-white/[0.03] hover:bg-white/[0.07] disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
        >
          Next
          <ChevronRight className="w-3.5 h-3.5" />
        </button>
      </div>
    </div>
  )
}
