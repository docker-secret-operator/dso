/**
 * Modern UI Components
 * Premium SaaS components inspired by Linear, Vercel, Stripe
 */

import { cn } from '@/lib/utils'
import React from 'react'

// ============================================================================
// CARD COMPONENT
// ============================================================================

interface CardProps extends React.HTMLAttributes<HTMLDivElement> {
  variant?: 'default' | 'gradient' | 'bordered'
}

export const Card = React.forwardRef<HTMLDivElement, CardProps>(
  ({ className, variant = 'default', ...props }, ref) => (
    <div
      ref={ref}
      className={cn(
        'rounded-2xl transition-all duration-200',
        variant === 'default' && 'bg-white border border-slate-100 shadow-sm hover:shadow-md',
        variant === 'gradient' && 'bg-gradient-to-br from-white to-slate-50 border border-slate-100 shadow-sm hover:shadow-md',
        variant === 'bordered' && 'bg-white border-2 border-slate-200 shadow-none hover:shadow-sm',
        className
      )}
      {...props}
    />
  )
)
Card.displayName = 'Card'

// ============================================================================
// BUTTON COMPONENT
// ============================================================================

interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'danger' | 'ghost' | 'outline'
  size?: 'sm' | 'md' | 'lg'
  isLoading?: boolean
}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant = 'primary', size = 'md', isLoading, disabled, children, ...props }, ref) => (
    <button
      ref={ref}
      disabled={disabled || isLoading}
      className={cn(
        'inline-flex items-center justify-center font-semibold rounded-lg transition-all duration-200 focus:outline-none focus:ring-2 focus:ring-offset-2 disabled:opacity-50 disabled:cursor-not-allowed',

        // Sizes
        size === 'sm' && 'px-3 py-1.5 text-sm',
        size === 'md' && 'px-4 py-2.5 text-sm',
        size === 'lg' && 'px-6 py-3 text-base',

        // Variants
        variant === 'primary' &&
          'bg-gradient-to-r from-coral-600 to-coral-500 text-white shadow-md hover:shadow-lg hover:from-coral-700 hover:to-coral-600 focus:ring-coral-500',
        variant === 'secondary' &&
          'bg-white text-slate-900 border border-slate-200 shadow-sm hover:shadow-md hover:bg-slate-50 hover:border-slate-300 focus:ring-slate-500',
        variant === 'danger' &&
          'bg-red-600 text-white shadow-md hover:shadow-lg hover:bg-red-700 focus:ring-red-500',
        variant === 'ghost' &&
          'bg-transparent text-slate-700 hover:bg-slate-100 focus:ring-slate-500',
        variant === 'outline' &&
          'bg-white text-slate-700 border border-slate-300 hover:bg-slate-50 focus:ring-slate-500',

        className
      )}
      {...props}
    >
      {isLoading ? (
        <>
          <svg className="animate-spin -ml-1 mr-2 h-4 w-4" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z" />
          </svg>
          Loading...
        </>
      ) : (
        children
      )}
    </button>
  )
)
Button.displayName = 'Button'

// ============================================================================
// BADGE COMPONENT
// ============================================================================

interface BadgeProps extends React.HTMLAttributes<HTMLSpanElement> {
  variant?: 'success' | 'warning' | 'danger' | 'info' | 'default'
  size?: 'sm' | 'md'
}

export const Badge = React.forwardRef<HTMLSpanElement, BadgeProps>(
  ({ className, variant = 'default', size = 'md', ...props }, ref) => (
    <span
      ref={ref}
      className={cn(
        'inline-flex items-center rounded-full font-semibold',

        // Sizes
        size === 'sm' && 'px-2 py-1 text-xs',
        size === 'md' && 'px-3 py-1 text-sm',

        // Variants
        variant === 'success' && 'bg-green-100 text-green-800',
        variant === 'warning' && 'bg-yellow-100 text-yellow-800',
        variant === 'danger' && 'bg-red-100 text-red-800',
        variant === 'info' && 'bg-blue-100 text-blue-800',
        variant === 'default' && 'bg-slate-100 text-slate-800',

        className
      )}
      {...props}
    />
  )
)
Badge.displayName = 'Badge'

// ============================================================================
// INPUT COMPONENT
// ============================================================================

interface InputProps extends React.InputHTMLAttributes<HTMLInputElement> {
  label?: string
  error?: string
  helperText?: string
}

export const Input = React.forwardRef<HTMLInputElement, InputProps>(
  ({ className, label, error, helperText, ...props }, ref) => (
    <div className="w-full">
      {label && (
        <label className="block text-sm font-semibold text-slate-900 mb-2">
          {label}
        </label>
      )}
      <input
        ref={ref}
        className={cn(
          'w-full px-4 py-2.5 rounded-lg border-2 transition-all duration-200 font-medium',
          'bg-white text-slate-900 placeholder-slate-400',
          'border-slate-200 focus:border-coral-600 focus:ring-2 focus:ring-coral-500/20',
          'hover:border-slate-300',
          'disabled:bg-slate-50 disabled:text-slate-500 disabled:cursor-not-allowed',
          error && 'border-red-500 focus:border-red-600 focus:ring-red-500/20',
          className
        )}
        {...props}
      />
      {error && <p className="mt-1 text-sm font-medium text-red-600">{error}</p>}
      {helperText && !error && <p className="mt-1 text-sm text-slate-500">{helperText}</p>}
    </div>
  )
)
Input.displayName = 'Input'

// ============================================================================
// SELECT COMPONENT
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
        <label className="block text-sm font-semibold text-slate-900 mb-2">
          {label}
        </label>
      )}
      <select
        ref={ref}
        className={cn(
          'w-full px-4 py-2.5 rounded-lg border-2 transition-all duration-200 font-medium',
          'bg-white text-slate-900',
          'border-slate-200 focus:border-coral-600 focus:ring-2 focus:ring-coral-500/20',
          'hover:border-slate-300',
          'disabled:bg-slate-50 disabled:text-slate-500 disabled:cursor-not-allowed',
          error && 'border-red-500 focus:border-red-600 focus:ring-red-500/20',
          className
        )}
        {...props}
      >
        {options.map(option => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
      {error && <p className="mt-1 text-sm font-medium text-red-600">{error}</p>}
    </div>
  )
)
Select.displayName = 'Select'

// ============================================================================
// METRIC CARD COMPONENT
// ============================================================================

interface MetricCardProps {
  label: string
  value: string | number
  change?: number
  icon?: React.ReactNode
  trend?: 'up' | 'down' | 'neutral'
  gradient?: 'coral' | 'blue' | 'green'
}

export function MetricCard({
  label,
  value,
  change,
  icon,
  trend = 'neutral',
  gradient = 'coral',
}: MetricCardProps) {
  const gradientClass = {
    coral: 'from-coral-600/20 to-coral-500/10',
    blue: 'from-blue-600/20 to-blue-500/10',
    green: 'from-green-600/20 to-green-500/10',
  }[gradient]

  return (
    <Card className={cn('bg-gradient-to-br', gradientClass)}>
      <div className="p-6">
        <div className="flex items-start justify-between mb-4">
          <div>
            <p className="text-sm font-medium text-slate-600 mb-1">{label}</p>
            <p className="text-3xl font-bold text-slate-900">{value}</p>
          </div>
          {icon && <div className="text-slate-400">{icon}</div>}
        </div>

        {change !== undefined && (
          <div
            className={cn(
              'inline-flex items-center gap-1 text-sm font-semibold',
              trend === 'up' && 'text-green-600',
              trend === 'down' && 'text-red-600',
              trend === 'neutral' && 'text-slate-600'
            )}
          >
            <span>{change > 0 ? '+' : ''}{change}%</span>
            {trend === 'up' && <span>↑</span>}
            {trend === 'down' && <span>↓</span>}
          </div>
        )}
      </div>
    </Card>
  )
}

// ============================================================================
// STATUS INDICATOR
// ============================================================================

interface StatusIndicatorProps {
  status: 'healthy' | 'warning' | 'critical' | 'offline'
  label?: string
}

export function StatusIndicator({ status, label }: StatusIndicatorProps) {
  const colors = {
    healthy: 'bg-green-500',
    warning: 'bg-yellow-500',
    critical: 'bg-red-500',
    offline: 'bg-slate-400',
  }

  const labels = {
    healthy: 'Healthy',
    warning: 'Warning',
    critical: 'Critical',
    offline: 'Offline',
  }

  return (
    <div className="flex items-center gap-2">
      <div className={cn('w-3 h-3 rounded-full animate-pulse', colors[status])} />
      <span className="text-sm font-medium text-slate-700">{label || labels[status]}</span>
    </div>
  )
}

// ============================================================================
// STAT ROW
// ============================================================================

interface StatRowProps {
  label: string
  value: string | number
  icon?: React.ReactNode
}

export function StatRow({ label, value, icon }: StatRowProps) {
  return (
    <div className="flex items-center justify-between py-3 border-b border-slate-100 last:border-b-0">
      <div className="flex items-center gap-3">
        {icon && <div className="text-slate-400">{icon}</div>}
        <span className="text-sm text-slate-600">{label}</span>
      </div>
      <span className="text-sm font-semibold text-slate-900">{value}</span>
    </div>
  )
}
