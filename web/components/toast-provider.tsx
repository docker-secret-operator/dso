'use client'

import type { ReactNode } from 'react'

import { ToastProvider as ToastContextProvider, useToastContext, type ToastType } from './toast-context'
import { ToastContainer } from './toast-container'

export function ToastSystemProvider({ children }: { children: ReactNode }) {
  return (
    <ToastContextProvider>
      {children}
      <ToastContainer />
    </ToastContextProvider>
  )
}

/**
 * Convenience hook for firing toasts from any page. Replaces
 * browser-native alert()/confirm() feedback across the Phase 1 pages.
 */
export function useToast() {
  const { addToast, dismissToast } = useToastContext()

  return {
    toast: (type: ToastType, title: string, description?: string) => addToast({ type, title, description }),
    success: (title: string, description?: string) => addToast({ type: 'success', title, description }),
    error: (title: string, description?: string) => addToast({ type: 'error', title, description }),
    warning: (title: string, description?: string) => addToast({ type: 'warning', title, description }),
    info: (title: string, description?: string) => addToast({ type: 'info', title, description }),
    dismiss: dismissToast,
  }
}
