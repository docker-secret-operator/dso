'use client'

import { AuditFilters } from '@/lib/api/types'
import { Download } from 'lucide-react'
import * as auditApi from '@/lib/api/audit'

interface AuditExportButtonProps {
  filters: AuditFilters
  format: 'csv' | 'json'
}

export function AuditExportButton({ filters, format }: AuditExportButtonProps) {
  const handleExport = () => {
    const url = auditApi.getAuditExportURL(filters, format)
    const link = document.createElement('a')
    link.href = url
    link.download = `audit-export.${format}`
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
  }

  return (
    <button
      onClick={handleExport}
      className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs rounded-lg border border-white/10 text-slate-400 hover:text-slate-200 hover:bg-white/5 transition-colors"
    >
      <Download className="w-3.5 h-3.5" />
      {format.toUpperCase()}
    </button>
  )
}
