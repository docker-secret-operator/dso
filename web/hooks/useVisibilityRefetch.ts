'use client'

import { useState, useEffect } from 'react'

// Returns false when the tab is hidden, true when visible.
// Pass the result as the `enabled` guard or multiply the refetchInterval:
//   refetchInterval: visible ? 5000 : false
export function usePageVisible(): boolean {
  const [visible, setVisible] = useState(
    typeof document !== 'undefined' ? !document.hidden : true
  )

  useEffect(() => {
    function handleVisibilityChange() {
      setVisible(!document.hidden)
    }

    document.addEventListener('visibilitychange', handleVisibilityChange)
    return () => document.removeEventListener('visibilitychange', handleVisibilityChange)
  }, [])

  return visible
}

// Convenience: returns a refetchInterval value — the given interval when visible, false when hidden
export function useVisibleInterval(intervalMs: number): number | false {
  const visible = usePageVisible()
  return visible ? intervalMs : false
}
