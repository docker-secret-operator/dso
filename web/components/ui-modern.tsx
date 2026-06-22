/**
 * DSO Modern UI Component Library
 * Dark-themed premium components — no coral, no white cards.
 */

import { cn } from '@/lib/utils'
import React from 'react'
import { TrendingUp, TrendingDown, Minus } from 'lucide-react'

// ============================================================================
// CARD
// ============================================================================

interface CardProps extends React.HTMLAttributes<HTMLDivElement> {
  variant?: 'default' | 'interactive' | 'elevated'
}

export const Card = React.forwardRef<HTMLDivElement, CardProps>(
  ({ className, variant = 'default', ...props }, ref) => {
    const variants = {
      default: cn(
        'bg-[#111827]',
        'border border-[rgba(255,255,255,0.08)]',
        'rounded-[12px]',
        'p-[20px]',
        'shadow-[0_0_0_1px_rgba(255,255,255,0.03),_0_8px_24px_rgba(0,0,0,0.3)]'
      ),
      interactive: cn(
        'bg-[#111827]',
        'border border-[rgba(255,255,255,0.08)]',
        'rounded-[12px]',
        'p-[20px]',
        'shadow-[0_0_0_1px_rgba(255,255,255,0.03),_0_8px_24px_rgba(0,0,0,0.3)]',
        'hover:border-[rgba(255,255,255,0.12)]',
        'hover:shadow-[0_0_0_1px_rgba(255,255,255,0.08),_0_4px_12px_rgba(0,0,0,0.2)]',
        'transition-all duration-200',
        'cursor-pointer'
      ),
      elevated: cn(
        'bg-[#1A2235]',
        'border border-[rgba(255,255,255,0.12)]',
        'rounded-[12px]',
        'p-[20px]',
        'shadow-[0_10px_15px_-3px_rgba(0,0,0,0.1),_0_4px_6px_-2px_rgba(0,0,0,0.05)]'
      ),
    }

    return (
      <div
        ref={ref}
        className={cn(variants[variant], className)}
        {...props}
      />
    )
  }
)
Card.displayName = 'Card'

// ============================================================================
// BUTTON
// ============================================================================

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'danger' | 'ghost' | 'outline'
  size?: 'sm' | 'md' | 'lg' | 'icon'
  isLoading?: boolean
}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant = 'primary', size = 'md', isLoading, disabled, children, ...props }, ref) => (
    <button
      ref={ref}
      disabled={disabled || isLoading}
      className={cn(
        'inline-flex items-center justify-center font-medium rounded-lg transition-all duration-100',
        'focus:outline-none focus:shadow-[0_0_0_3px_rgba(59,130,246,0.1),_0_0_0_1px_rgba(59,130,246,0.5)]',
        'disabled:opacity-50 disabled:cursor-not-allowed',

        // Sizes
        size === 'sm'   && 'px-3 py-1.5 text-xs gap-1.5',
        size === 'md'   && 'px-4 py-2 text-sm gap-2',
        size === 'lg'   && 'px-5 py-2.5 text-sm gap-2',
        size === 'icon' && 'p-2 text-sm',

        // Variants
        variant === 'primary'   && 'bg-indigo-600 text-white hover:bg-indigo-500 shadow-sm',
        variant === 'secondary' && 'bg-white/8 text-slate-200 border border-white/10 hover:bg-white/12 hover:border-white/15',
        variant === 'danger'    && 'bg-red-600/90 text-white hover:bg-red-500',
        variant === 'ghost'     && 'text-slate-400 hover:text-slate-200 hover:bg-white/6',
        variant === 'outline'   && 'border border-white/12 text-slate-300 hover:bg-white/6 hover:border-white/20',

        className
      )}
      {...props}
    >
      {isLoading ? (
        <>
          <svg className="animate-spin w-3.5 h-3.5 mr-1.5" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
          </svg>
          Loading…
        </>
      ) : children}
    </button>
  )
)
Button.displayName = 'Button'

// ============================================================================
// BADGE
// ============================================================================

interface BadgeProps extends React.HTMLAttributes<HTMLSpanElement> {
  variant?: 'success' | 'warning' | 'danger' | 'info' | 'default' | 'outline'
  size?: 'sm' | 'md'
  dot?: boolean
}

export const Badge = React.forwardRef<HTMLSpanElement, BadgeProps>(
  ({ className, variant = 'default', size = 'sm', dot = false, children, ...props }, ref) => (
    <span
      ref={ref}
      className={cn(
        'inline-flex items-center gap-1 rounded-full font-medium',
        size === 'sm' && 'px-2 py-0.5 text-[11px]',
        size === 'md' && 'px-2.5 py-1 text-xs',

        variant === 'success' && 'bg-emerald-500/15 text-emerald-400 ring-1 ring-emerald-500/20',
        variant === 'warning' && 'bg-amber-500/15   text-amber-400   ring-1 ring-amber-500/20',
        variant === 'danger'  && 'bg-red-500/15     text-red-400     ring-1 ring-red-500/20',
        variant === 'info'    && 'bg-blue-500/15    text-blue-400    ring-1 ring-blue-500/20',
        variant === 'default' && 'bg-slate-500/15   text-slate-400   ring-1 ring-slate-500/20',
        variant === 'outline' && 'border border-white/15 text-slate-400',

        className
      )}
      {...props}
    >
      {dot && (
        <span className={cn(
          'w-1.5 h-1.5 rounded-full flex-shrink-0',
          variant === 'success' && 'bg-emerald-400',
          variant === 'warning' && 'bg-amber-400',
          variant === 'danger'  && 'bg-red-400',
          variant === 'info'    && 'bg-blue-400',
          variant === 'default' && 'bg-slate-400',
        )} />
      )}
      {children}
    </span>
  )
)
Badge.displayName = 'Badge'

// ============================================================================
// STATUS BADGE — semantic shorthand
// ============================================================================

interface StatusBadgeProps { status: string; label?: string }

export function StatusBadge({ status, label }: StatusBadgeProps) {
  const s = status.toLowerCase()
  const variant =
    s === 'ok' || s === 'healthy' || s === 'active' || s === 'success' || s === 'operational' ? 'success' :
    s === 'pending' || s === 'warning' || s === 'acknowledged' ? 'warning' :
    s === 'error' || s === 'critical' || s === 'failed' || s === 'failure' ? 'danger' :
    s === 'offline' || s === 'disabled' || s === 'suppressed' ? 'default' :
    'info'

  return <Badge variant={variant} dot>{label ?? status}</Badge>
}

// ============================================================================
// INPUT
// ============================================================================

interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  label?: string
  error?: string
  helperText?: string
  startIcon?: React.ReactNode
}

export const Input = React.forwardRef<HTMLInputElement, InputProps>(
  ({ className, label, error, helperText, startIcon, ...props }, ref) => (
    <div className="w-full">
      {label && (
        <label className="block text-sm font-medium text-slate-300 mb-1.5">
          {label}
        </label>
      )}
      <div className="relative">
        {startIcon && (
          <span className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-500">
            {startIcon}
          </span>
        )}
        <input
          ref={ref}
          className={cn(
            'w-full rounded-lg border text-sm transition-all duration-100',
            'bg-[#1a1f2e] text-slate-200 placeholder:text-slate-600',
            'border-white/10 hover:border-white/16',
            'focus:outline-none focus:border-indigo-500/60 focus:shadow-[0_0_0_3px_rgba(59,130,246,0.1),_0_0_0_1px_rgba(59,130,246,0.5)]',
            'disabled:opacity-50 disabled:cursor-not-allowed',
            startIcon ? 'pl-9 pr-3 py-2' : 'px-3 py-2',
            error && 'border-red-500/60 focus:border-red-500 focus:shadow-[0_0_0_3px_rgba(239,68,68,0.1),_0_0_0_1px_rgba(239,68,68,0.5)]',
            className
          )}
          {...props}
        />
      </div>
      {error      && <p className="mt-1.5 text-xs text-red-400">{error}</p>}
      {helperText && !error && <p className="mt-1.5 text-xs text-slate-500">{helperText}</p>}
    </div>
  )
)
Input.displayName = 'Input'

// ============================================================================
// SELECT
// ============================================================================

interface SelectProps extends React.SelectHTMLAttributes<HTMLSelectElement> {
  label?: string
  options: Array<{ value: string; label: string }>
  error?: string
}

export const Select = React.forwardRef<HTMLSelectElement, SelectProps>(
  ({ className, label, options, error, ...props }, ref) => (
    <div className="w-full">
      {label && (
        <label className="block text-sm font-medium text-slate-300 mb-1.5">
          {label}
        </label>
      )}
      <select
        ref={ref}
        className={cn(
          'w-full rounded-lg border text-sm transition-all duration-100',
          'bg-[#1a1d24] text-slate-200',
          'border-white/10 hover:border-white/16',
          'focus:outline-none focus:border-indigo-500/60 focus:shadow-[0_0_0_3px_rgba(59,130,246,0.1),_0_0_0_1px_rgba(59,130,246,0.5)]',
          'disabled:opacity-50 disabled:cursor-not-allowed',
          'px-3 py-2',
          error && 'border-red-500/60',
          className
        )}
        {...props}
      >
        {options.map(o => (
          <option key={o.value} value={o.value} className="bg-[#1a1f2e]">{o.label}</option>
        ))}
      </select>
      {error && <p className="mt-1.5 text-xs text-red-400">{error}</p>}
    </div>
  )
)
Select.displayName = 'Select'

// ============================================================================
// METRIC CARD
// ============================================================================

interface MetricCardProps {
  label: string
  value: string | number
  change?: number
  trend?: 'up' | 'down' | 'neutral'
  icon?: React.ReactNode
  loading?: boolean
  onClick?: () => void
  sublabel?: string
  accentColor?: 'indigo' | 'emerald' | 'amber' | 'red' | 'blue' | 'slate'
}

const accentMap = {
  indigo:  { icon: 'text-indigo-400',  bar: 'bg-indigo-500' },
  emerald: { icon: 'text-emerald-400', bar: 'bg-emerald-500' },
  amber:   { icon: 'text-amber-400',   bar: 'bg-amber-500' },
  red:     { icon: 'text-red-400',     bar: 'bg-red-500' },
  blue:    { icon: 'text-blue-400',    bar: 'bg-blue-500' },
  slate:   { icon: 'text-slate-400',   bar: 'bg-slate-500' },
}

export function MetricCard({ label, value, change, trend = 'neutral', icon, loading, onClick, sublabel, accentColor = 'indigo' }: MetricCardProps) {
  const ac = accentMap[accentColor]

  if (loading) {
    return (
      <div className="rounded-xl border border-white/[0.07] bg-[#111827] p-5 space-y-3">
        <div className="skeleton h-3.5 w-20 rounded" />
        <div className="skeleton h-8 w-16 rounded" />
        <div className="skeleton h-3 w-24 rounded" />
      </div>
    )
  }

  return (
    <div
      className={cn(
        'rounded-xl border border-white/[0.07] bg-[#111827] p-5 transition-colors duration-150',
        onClick && 'cursor-pointer hover:border-white/[0.14]'
      )}
      onClick={onClick}
      role={onClick ? 'button' : undefined}
      tabIndex={onClick ? 0 : undefined}
    >
      <div className="flex items-start justify-between mb-3">
        <p className="text-xs font-medium text-slate-500 uppercase tracking-wider">{label}</p>
        {icon && <span className={cn('flex-shrink-0', ac.icon)}>{icon}</span>}
      </div>

      <p className="text-2xl font-semibold text-slate-100 tabular-nums">{value}</p>

      <div className="flex items-center justify-between mt-2">
        {sublabel && <p className="text-xs text-slate-600">{sublabel}</p>}
        {change !== undefined && (
          <div className={cn(
            'flex items-center gap-1 text-xs font-medium',
            trend === 'up'   && 'text-emerald-400',
            trend === 'down' && 'text-red-400',
            trend === 'neutral' && 'text-slate-500',
          )}>
            {trend === 'up'      && <TrendingUp className="w-3 h-3" />}
            {trend === 'down'    && <TrendingDown className="w-3 h-3" />}
            {trend === 'neutral' && <Minus className="w-3 h-3" />}
            <span>{change > 0 ? '+' : ''}{change}%</span>
          </div>
        )}
      </div>
    </div>
  )
}

// ============================================================================
// STATUS INDICATOR
// ============================================================================

interface StatusIndicatorProps {
  status: 'healthy' | 'warning' | 'critical' | 'offline' | 'info'
  label?: string
  pulse?: boolean
}

const statusStyles = {
  healthy:  'bg-emerald-400 shadow-[0_0_6px_rgba(52,211,153,0.6)]',
  warning:  'bg-amber-400   shadow-[0_0_6px_rgba(251,191,36,0.6)]',
  critical: 'bg-red-400     shadow-[0_0_6px_rgba(248,113,113,0.6)]',
  offline:  'bg-slate-500',
  info:     'bg-blue-400    shadow-[0_0_6px_rgba(96,165,250,0.6)]',
}

export function StatusIndicator({ status, label, pulse = true }: StatusIndicatorProps) {
  return (
    <div className="flex items-center gap-2">
      <span className={cn(
        'w-2 h-2 rounded-full flex-shrink-0',
        statusStyles[status],
        pulse && status !== 'offline' && 'animate-pulse'
      )} />
      {label && <span className="text-sm text-slate-400">{label}</span>}
    </div>
  )
}

// ============================================================================
// PAGE HEADER
// ============================================================================

interface PageHeaderProps {
  title: string
  description?: string
  actions?: React.ReactNode
  badge?: React.ReactNode
}

export function PageHeader({ title, description, actions, badge }: PageHeaderProps) {
  return (
    <div className="flex items-start justify-between gap-4 mb-6">
      <div className="min-w-0">
        <div className="flex items-center gap-2.5">
          <h1 className="text-xl font-semibold text-slate-100 truncate">{title}</h1>
          {badge}
        </div>
        {description && <p className="mt-1 text-sm text-slate-500">{description}</p>}
      </div>
      {actions && <div className="flex items-center gap-2 flex-shrink-0">{actions}</div>}
    </div>
  )
}

// ============================================================================
// SKELETON
// ============================================================================

interface SkeletonProps { className?: string; count?: number }

export function Skeleton({ className, count = 1 }: SkeletonProps) {
  return (
    <>
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className={cn('skeleton', className)} />
      ))}
    </>
  )
}

// ============================================================================
// EMPTY STATE
// ============================================================================

interface EmptyStateProps {
  icon?: React.ReactNode
  title: string
  description?: string
  action?: React.ReactNode
}

export function EmptyState({ icon, title, description, action }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center py-16 px-6 text-center">
      {icon && (
        <div className="w-12 h-12 rounded-xl bg-white/5 border border-white/10 flex items-center justify-center text-slate-500 mb-4">
          {icon}
        </div>
      )}
      <p className="text-sm font-medium text-slate-300">{title}</p>
      {description && <p className="mt-1 text-xs text-slate-600 max-w-xs">{description}</p>}
      {action && <div className="mt-4">{action}</div>}
    </div>
  )
}

// ============================================================================
// STAT ROW
// ============================================================================

export function StatRow({ label, value, icon }: { label: string; value: React.ReactNode; icon?: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between py-2.5 border-b border-white/[0.05] last:border-0">
      <div className="flex items-center gap-2.5">
        {icon && <span className="text-slate-600">{icon}</span>}
        <span className="text-sm text-slate-500">{label}</span>
      </div>
      <span className="text-sm font-medium text-slate-300">{value}</span>
    </div>
  )
}

// ============================================================================
// DIVIDER
// ============================================================================

export function Divider({ className }: { className?: string }) {
  return <hr className={cn('border-0 border-t border-white/[0.07]', className)} />
}
