'use client'

import { QueryClientProvider } from '@tanstack/react-query'
import { getQueryClient } from '@/lib/query-client'
import { ToastProvider } from './toast-context'
import { ToastContainer } from './toast-container'
import { ReactNode } from 'react'

export function Providers({ children }: { children: ReactNode }) {
  const queryClient = getQueryClient()

  return (
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        {children}
        <ToastContainer />
      </ToastProvider>
    </QueryClientProvider>
  )
}
