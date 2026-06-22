'use client'

import { cn } from '@/lib/utils'
import { ChevronRight } from 'lucide-react'
import Link from 'next/link'

interface SectionProps {
  title: string
  /** Optional muted context shown next to the title. */
  meta?: React.ReactNode
  /** Optional "view all" link target. */
  href?: string
  className?: string
  children: React.ReactNode
}

/**
 * Consistent panel + header for a dashboard section.
 * Flat surface, hairline border — no glow, no glass.
 */
export function Section({ title, meta, href, className, children }: SectionProps) {
  return (
    <section className={cn('rounded-xl border border-white/[0.07] bg-[#111827]', className)}>
      <header className="flex items-center justify-between gap-3 px-5 h-11 border-b border-white/[0.06]">
        <div className="flex items-baseline gap-2 min-w-0">
          <h2 className="text-[13px] font-semibold text-slate-200 truncate">{title}</h2>
          {meta && <span className="text-xs text-slate-400 truncate">{meta}</span>}
        </div>
        {href && (
          <Link
            href={href}
            className="flex items-center gap-0.5 h-full px-2 -mr-2 text-xs text-slate-400 hover:text-slate-200 transition-colors flex-shrink-0"
          >
            View all
            <ChevronRight className="w-3.5 h-3.5" />
          </Link>
        )}
      </header>
      <div className="p-5">{children}</div>
    </section>
  )
}
