'use client'

import { CheckCircle2, XCircle, Info, AlertTriangle, X } from 'lucide-react'

import { cn } from '@/lib/utils'
import { useToastContext, type Toast, type ToastType } from './toast-context'

const ICONS: Record<ToastType, typeof CheckCircle2> = {
  success: CheckCircle2,
  error: XCircle,
  info: Info,
  warning: AlertTriangle,
}

const STYLES: Record<ToastType, string> = {
  success: 'border-emerald-500/25 bg-emerald-500/10 text-emerald-400',
  error: 'border-red-500/25 bg-red-500/10 text-red-400',
  info: 'border-border bg-card text-foreground',
  warning: 'border-amber-500/25 bg-amber-500/10 text-amber-400',
}

function ToastItem({ toast, onDismiss }: { toast: Toast; onDismiss: (id: string) => void }) {
  const Icon = ICONS[toast.type]
  return (
    <div
      role="status"
      aria-live="polite"
      data-testid={`toast-${toast.type}`}
      className={cn(
        'flex w-80 items-start gap-3 rounded-lg border px-4 py-3 shadow-lg backdrop-blur-sm',
        STYLES[toast.type]
      )}
    >
      <Icon className="mt-0.5 h-4 w-4 flex-shrink-0" />
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium">{toast.title}</p>
        {toast.description && <p className="mt-0.5 text-xs opacity-80">{toast.description}</p>}
      </div>
      <button
        onClick={() => onDismiss(toast.id)}
        aria-label="Dismiss notification"
        className="flex-shrink-0 opacity-60 transition-opacity hover:opacity-100"
      >
        <X className="h-3.5 w-3.5" />
      </button>
    </div>
  )
}

export function ToastContainer() {
  const { toasts, dismissToast } = useToastContext()

  if (toasts.length === 0) return null

  return (
    <div className="pointer-events-none fixed bottom-4 right-4 z-50 flex flex-col gap-2">
      {toasts.map((toast) => (
        <div key={toast.id} className="pointer-events-auto">
          <ToastItem toast={toast} onDismiss={dismissToast} />
        </div>
      ))}
    </div>
  )
}
