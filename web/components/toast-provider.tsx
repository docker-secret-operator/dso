'use client'

import { ReactNode } from 'react'
import { ToastProvider as ToastContextProvider, useToastContext, ToastType } from './toast-context'
import { ToastContainer } from './toast-container'

// Wraps ToastContextProvider + ToastContainer in a single component
export function ToastSystemProvider({ children }: { children: ReactNode }) {
  return (
    <ToastContextProvider>
      {children}
      <ToastContainer />
    </ToastContextProvider>
  )
}

// Convenience hook with shorthand helpers
export function useToast() {
  const { addToast, removeToast, toasts } = useToastContext()

  return {
    toasts,
    toast: (type: ToastType, title: string, description?: string, duration?: number) =>
      addToast(type, title, description, duration),
    success: (title: string, description?: string) => addToast('success', title, description),
    error: (title: string, description?: string) => addToast('error', title, description),
    warning: (title: string, description?: string) => addToast('warning', title, description),
    info: (title: string, description?: string) => addToast('info', title, description),
    dismiss: removeToast,
  }
}
