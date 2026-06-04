'use client'

import { useCallback } from 'react'
import { useToastContext } from '@/components/toast-context'

export function useToast() {
  const { addToast, removeToast } = useToastContext()

  const success = useCallback((title: string, description?: string) => {
    return addToast('success', title, description)
  }, [addToast])

  const error = useCallback((title: string, description?: string) => {
    return addToast('error', title, description)
  }, [addToast])

  const info = useCallback((title: string, description?: string) => {
    return addToast('info', title, description)
  }, [addToast])

  const warning = useCallback((title: string, description?: string) => {
    return addToast('warning', title, description)
  }, [addToast])

  return {
    toast: { success, error, info, warning },
    addToast,
    removeToast,
  }
}
