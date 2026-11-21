'use client'

import { createContext, useCallback, useContext, useState, type ReactNode } from 'react'

export type ToastType = 'success' | 'error' | 'info' | 'warning'

export interface Toast {
  id: string
  type: ToastType
  title: string
  description?: string
  duration?: number
}

const DEFAULT_DURATION = 5000

interface ToastContextValue {
  toasts: Toast[]
  addToast: (toast: Omit<Toast, 'id'>) => string
  dismissToast: (id: string) => void
}

const ToastContext = createContext<ToastContextValue | null>(null)

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([])

  const dismissToast = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id))
  }, [])

  const addToast = useCallback(
    (toast: Omit<Toast, 'id'>) => {
      const id = `toast-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`
      const duration = toast.duration ?? DEFAULT_DURATION
      setToasts((prev) => [...prev, { ...toast, id }])
      if (duration > 0) {
        setTimeout(() => dismissToast(id), duration)
      }
      return id
    },
    [dismissToast]
  )

  return (
    <ToastContext.Provider value={{ toasts, addToast, dismissToast }}>{children}</ToastContext.Provider>
  )
}

export function useToastContext(): ToastContextValue {
  const ctx = useContext(ToastContext)
  if (!ctx) {
    throw new Error('useToastContext must be used within a ToastProvider')
  }
  return ctx
}
