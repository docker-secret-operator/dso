/**
 * DSO Premium Component Library
 * Reusable components for enterprise operations platform
 */

import React from 'react'
import { cn } from '@/lib/utils'
import {
  AlertCircle,
  CheckCircle2,
  AlertTriangle,
  Loader2,
  TrendingUp,
  TrendingDown,
  Plus,
} from 'lucide-react'

// ============================================================================
// PAGE HEADER
// ============================================================================

interface PageHeaderProps {
  title: string
  description?: string
  action?: {
    label: string
    onClick: () => void
    variant?: 'primary' | 'secondary'
  }
  breadcrumbs?: { label: string; href?: string }[]
}

export function PageHeader({ title, description, action, breadcrumbs }: PageHeaderProps) {
  return (
    <div className="mb-8 space-y-6">
      {breadcrumbs && breadcrumbs.length > 0 && (
        <div className="flex items-center gap-2 text-sm text-slate-600">
          {breadcrumbs.map((crumb, idx) => (
            <React.Fragment key={idx}>
              {idx > 0 && <span className="text-slate-400">/</span>}
              {crumb.href ? (
                <a href={crumb.href} className="text-slate-600 hover:text-slate-900">
                  {crumb.label}
                </a>
              ) : (
                <span className="text-slate-900 font-medium">{crumb.label}</span>
              )}
            </React.Fragment>
          ))}
        </div>
      )}

      <div className="flex items-start justify-between gap-6">
        <div>
          <h1 className="text-5xl font-bold tracking-tight text-slate-900">{title}</h1>
          {description && <p className="text-lg text-slate-600 mt-2">{description}</p>}
        </div>

        {action && (
          <button
            onClick={action.onClick}
            className={`flex items-center gap-2 px-4 py-2.5 rounded-lg font-medium transition-all flex-shrink-0 ${
              action.variant === 'secondary'
                ? 'bg-white border border-slate-300 text-slate-900 hover:bg-slate-50'
                : 'bg-gradient-to-r from-indigo-600 to-indigo-700 text-white shadow-md hover:shadow-lg'
            }`}
          >
            <Plus className="w-5 h-5" />
            {action.label}
          </button>
        )}
      </div>
    </div>
  )
}

// ============================================================================
// METRIC CARD
// ============================================================================

interface MetricCardProps {
  label: string
  value: string | number
  change?: number
  trend?: 'up' | 'down' | 'neutral'
  icon?: React.ReactNode
  subsystem?: 'incidents' | 'recommendations' | 'forecasts' | 'drift' | 'autonomy' | 'security' | 'alerts'
  size?: 'sm' | 'md' | 'lg'
}

export function MetricCard({ label, value, change, trend, icon, subsystem = 'security', size = 'md' }: MetricCardProps) {
  const subsystemColors = {
    incidents: 'text-orange-600 from-orange-50 to-orange-50/50',
    recommendations: 'text-purple-600 from-purple-50 to-purple-50/50',
    forecasts: 'text-cyan-600 from-cyan-50 to-cyan-50/50',
    drift: 'text-amber-600 from-amber-50 to-amber-50/50',
    autonomy: 'text-emerald-600 from-emerald-50 to-emerald-50/50',
    security: 'text-blue-600 from-blue-50 to-blue-50/50',
    alerts: 'text-red-600 from-red-50 to-red-50/50',
  }

  const [colorClass, bgClass] = subsystemColors[subsystem].split(' ')

  return (
    <div className={`rounded-2xl border border-slate-200 bg-gradient-to-br ${bgClass} shadow-sm hover:shadow-md transition-shadow p-${size === 'sm' ? '4' : size === 'lg' ? '8' : '6'}`}>
      <div className="flex items-start justify-between mb-4">
        {icon && <div className={`text-slate-400 ${colorClass}`}>{icon}</div>}
      </div>

      <p className="text-sm text-slate-600 font-medium">{label}</p>

      <div className="flex items-end gap-3 mt-3">
        <p className={`text-${size === 'lg' ? '4xl' : size === 'sm' ? '2xl' : '3xl'} font-bold text-slate-900`}>{value}</p>

        {change !== undefined && (
          <div className={`flex items-center gap-1 text-sm font-semibold mb-1 ${trend === 'up' ? 'text-emerald-600' : trend === 'down' ? 'text-red-600' : 'text-slate-600'}`}>
            {trend === 'up' ? <TrendingUp className="w-4 h-4" /> : trend === 'down' ? <TrendingDown className="w-4 h-4" /> : null}
            <span>{change > 0 ? '+' : ''}{change}%</span>
          </div>
        )}
      </div>
    </div>
  )
}

// ============================================================================
// PANEL CARD
// ============================================================================

interface PanelCardProps {
  title: string
  children: React.ReactNode
  action?: {
    label: string
    onClick: () => void
  }
  subsystem?: string
  loading?: boolean
}

export function PanelCard({ title, children, action, subsystem, loading }: PanelCardProps) {
  return (
    <div className="rounded-2xl border border-slate-200 bg-white shadow-sm hover:shadow-md transition-shadow p-8">
      <div className="flex items-center justify-between mb-6">
        <h3 className="text-xl font-bold text-slate-900">{title}</h3>
        {action && (
          <button
            onClick={action.onClick}
            className="text-sm text-indigo-600 hover:text-indigo-700 font-semibold transition-colors"
          >
            {action.label} →
          </button>
        )}
      </div>

      {loading ? (
        <div className="flex items-center justify-center py-12">
          <Loader2 className="w-6 h-6 text-slate-400 animate-spin" />
        </div>
      ) : (
        children
      )}
    </div>
  )
}

// ============================================================================
// STATUS BADGE
// ============================================================================

interface StatusBadgeProps {
  status: 'success' | 'warning' | 'error' | 'info' | 'neutral'
  label?: string
  size?: 'sm' | 'md'
}

export function StatusBadge({ status, label, size = 'md' }: StatusBadgeProps) {
  const statusStyles = {
    success: 'bg-emerald-100 text-emerald-800',
    warning: 'bg-amber-100 text-amber-800',
    error: 'bg-red-100 text-red-800',
    info: 'bg-blue-100 text-blue-800',
    neutral: 'bg-slate-100 text-slate-800',
  }

  const statusLabels = {
    success: 'Healthy',
    warning: 'Warning',
    error: 'Failed',
    info: 'Info',
    neutral: 'Neutral',
  }

  return (
    <span className={`rounded-full px-3 py-1 text-${size === 'sm' ? 'xs' : 'sm'} font-medium inline-flex items-center gap-1.5 ${statusStyles[status]}`}>
      {status === 'success' && <CheckCircle2 className="w-3.5 h-3.5" />}
      {status === 'warning' && <AlertTriangle className="w-3.5 h-3.5" />}
      {status === 'error' && <AlertCircle className="w-3.5 h-3.5" />}
      {label || statusLabels[status]}
    </span>
  )
}

// ============================================================================
// SECTION HEADER
// ============================================================================

export function SectionHeader({ title, description }: { title: string; description?: string }) {
  return (
    <div className="mb-6">
      <h2 className="text-2xl font-bold text-slate-900">{title}</h2>
      {description && <p className="text-sm text-slate-600 mt-2">{description}</p>}
    </div>
  )
}

// ============================================================================
// INFO CARD
// ============================================================================

export function InfoCard({
  icon,
  title,
  description,
  subsystem = 'security',
}: {
  icon?: React.ReactNode
  title: string
  description: string
  subsystem?: string
}) {
  return (
    <div className="p-6 rounded-2xl border border-slate-200 bg-white">
      {icon && <div className="mb-3">{icon}</div>}
      <h4 className="font-semibold text-slate-900">{title}</h4>
      <p className="text-sm text-slate-600 mt-2">{description}</p>
    </div>
  )
}

// ============================================================================
// TABLE CARD
// ============================================================================

interface TableColumn {
  key: string
  label: string
  render?: (value: any) => React.ReactNode
  width?: string
}

interface TableCardProps {
  columns: TableColumn[]
  data: Record<string, any>[]
  loading?: boolean
  empty?: {
    icon?: React.ReactNode
    title: string
    description: string
  }
}

export function TableCard({ columns, data, loading, empty }: TableCardProps) {
  return (
    <div className="rounded-2xl border border-slate-200 bg-white shadow-sm overflow-hidden">
      <div className="overflow-x-auto">
        <table className="w-full">
          {/* Header */}
          <thead className="bg-slate-50 border-b border-slate-200">
            <tr>
              {columns.map(col => (
                <th
                  key={col.key}
                  className={`px-6 py-3 text-left text-sm font-semibold text-slate-900 ${col.width || 'flex-1'}`}
                >
                  {col.label}
                </th>
              ))}
            </tr>
          </thead>

          {/* Body */}
          <tbody>
            {loading ? (
              <tr>
                <td colSpan={columns.length} className="px-6 py-8 text-center">
                  <Loader2 className="w-6 h-6 text-slate-400 animate-spin mx-auto" />
                </td>
              </tr>
            ) : data.length === 0 ? (
              <tr>
                <td colSpan={columns.length} className="px-6 py-12 text-center">
                  <div className="text-slate-400 mb-2">{empty?.icon}</div>
                  <p className="font-medium text-slate-900">{empty?.title}</p>
                  <p className="text-sm text-slate-600">{empty?.description}</p>
                </td>
              </tr>
            ) : (
              data.map((row, idx) => (
                <tr key={idx} className="border-b border-slate-200 hover:bg-slate-50 transition-colors">
                  {columns.map(col => (
                    <td key={col.key} className="px-6 py-4 text-sm text-slate-900">
                      {col.render ? col.render(row[col.key]) : row[col.key]}
                    </td>
                  ))}
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// ============================================================================
// CHART CARD
// ============================================================================

export function ChartCard({
  title,
  description,
  children,
  loading,
}: {
  title: string
  description?: string
  children?: React.ReactNode
  loading?: boolean
}) {
  return (
    <div className="rounded-2xl border border-slate-200 bg-white shadow-sm p-8">
      <div className="mb-6">
        <h3 className="text-lg font-bold text-slate-900">{title}</h3>
        {description && <p className="text-sm text-slate-600 mt-1">{description}</p>}
      </div>

      {loading ? (
        <div className="flex items-center justify-center h-64">
          <Loader2 className="w-6 h-6 text-slate-400 animate-spin" />
        </div>
      ) : (
        <div className="h-64">{children}</div>
      )}
    </div>
  )
}

// ============================================================================
// EMPTY STATE
// ============================================================================

export function EmptyState({
  icon,
  title,
  description,
  action,
}: {
  icon?: React.ReactNode
  title: string
  description: string
  action?: { label: string; onClick: () => void }
}) {
  return (
    <div className="rounded-2xl border border-slate-200 border-dashed bg-slate-50 p-12 text-center">
      {icon && <div className="mb-4 flex justify-center text-slate-400">{icon}</div>}
      <h3 className="text-lg font-semibold text-slate-900">{title}</h3>
      <p className="text-sm text-slate-600 mt-2 max-w-sm mx-auto">{description}</p>
      {action && (
        <button
          onClick={action.onClick}
          className="mt-6 px-4 py-2 rounded-lg bg-indigo-600 text-white text-sm font-medium hover:bg-indigo-700 transition-colors"
        >
          {action.label}
        </button>
      )}
    </div>
  )
}

// ============================================================================
// LOADING STATE
// ============================================================================

export function LoadingState({ count = 3 }: { count?: number }) {
  return (
    <div className="space-y-4">
      {Array.from({ length: count }).map((_, idx) => (
        <div key={idx} className="rounded-2xl border border-slate-200 bg-white p-6 animate-pulse">
          <div className="h-4 bg-slate-200 rounded w-3/4 mb-3" />
          <div className="h-8 bg-slate-200 rounded w-1/2" />
        </div>
      ))}
    </div>
  )
}

// ============================================================================
// ERROR STATE
// ============================================================================

export function ErrorState({
  title = 'Something went wrong',
  description = 'An error occurred while loading this content.',
  action,
}: {
  title?: string
  description?: string
  action?: { label: string; onClick: () => void }
}) {
  return (
    <div className="rounded-2xl border border-red-200 bg-red-50 p-8">
      <div className="flex items-start gap-4">
        <AlertCircle className="w-6 h-6 text-red-600 flex-shrink-0 mt-0.5" />
        <div className="flex-1">
          <h3 className="font-semibold text-red-900">{title}</h3>
          <p className="text-sm text-red-700 mt-1">{description}</p>
          {action && (
            <button
              onClick={action.onClick}
              className="mt-4 text-sm text-red-600 hover:text-red-700 font-medium underline"
            >
              {action.label}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

// ============================================================================
// ACTIVITY CARD
// ============================================================================

export function ActivityCard({
  icon,
  title,
  description,
  timestamp,
  badge,
  onClick,
}: {
  icon?: React.ReactNode
  title: string
  description?: string
  timestamp?: string
  badge?: React.ReactNode
  onClick?: () => void
}) {
  return (
    <button
      onClick={onClick}
      className="w-full text-left p-4 rounded-xl border border-slate-200 bg-white hover:bg-slate-50 hover:shadow-md transition-all"
    >
      <div className="flex items-start gap-3">
        {icon && <div className="text-slate-400 flex-shrink-0 mt-0.5">{icon}</div>}
        <div className="flex-1 min-w-0">
          <p className="font-medium text-slate-900 truncate">{title}</p>
          {description && <p className="text-sm text-slate-600 mt-1 truncate">{description}</p>}
          {timestamp && <p className="text-xs text-slate-500 mt-2">{timestamp}</p>}
        </div>
        {badge && <div className="flex-shrink-0">{badge}</div>}
      </div>
    </button>
  )
}

// ============================================================================
// INSIGHT CARD (For recommendations, forecasts, etc)
// ============================================================================

export function InsightCard({
  icon,
  title,
  priority,
  confidence,
  tags,
  action,
}: {
  icon?: React.ReactNode
  title: string
  priority?: 'high' | 'medium' | 'low'
  confidence?: number
  tags?: string[]
  action?: { label: string; onClick: () => void }
}) {
  return (
    <div className="p-4 rounded-xl border border-slate-200 bg-white hover:shadow-md transition-shadow">
      <div className="flex items-start gap-3 mb-3">
        {icon && <div className="text-slate-400 flex-shrink-0">{icon}</div>}
        <div className="flex-1 min-w-0">
          <p className="font-medium text-slate-900">{title}</p>
        </div>
      </div>

      <div className="flex items-center gap-2 flex-wrap mb-3">
        {priority && (
          <span
            className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${
              priority === 'high'
                ? 'bg-red-100 text-red-800'
                : priority === 'medium'
                  ? 'bg-amber-100 text-amber-800'
                  : 'bg-green-100 text-green-800'
            }`}
          >
            {priority} Priority
          </span>
        )}

        {confidence !== undefined && (
          <span className="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-blue-100 text-blue-800">
            {confidence}% Confidence
          </span>
        )}
      </div>

      {tags && tags.length > 0 && (
        <div className="flex items-center gap-1 flex-wrap mb-3">
          {tags.map((tag, idx) => (
            <span key={idx} className="inline-flex items-center px-2 py-1 rounded-full text-xs bg-slate-100 text-slate-700">
              {tag}
            </span>
          ))}
        </div>
      )}

      {action && (
        <button
          onClick={action.onClick}
          className="text-xs text-indigo-600 hover:text-indigo-700 font-semibold transition-colors"
        >
          {action.label} →
        </button>
      )}
    </div>
  )
}
